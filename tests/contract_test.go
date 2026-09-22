// Package tests holds black-box contract tests that run the PRODUCTION binary
// (built exactly as `make build` builds it) — no test hooks, system trust
// store. They pin the external contracts the shell suite cannot reach:
// process replacement, exit and signal propagation, exact argv, relocation
// through symlinks, the model table override and the configuration file.
package tests

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gering/cc-router/internal/testpki"
)

var pkg string // temp package layout: bin/cc-harness-agents + share -> ../share

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cc-router-contract")
	if err != nil {
		panic(err)
	}
	pkg = dir
	root, _ := filepath.Abs("..")
	build := exec.Command("go", "build", "-o", filepath.Join(pkg, "bin", "cc-harness-agents"), "./cmd/cc-harness-agents")
	build.Dir, build.Stderr = root, os.Stderr
	if err := build.Run(); err != nil {
		panic(err)
	}
	if err := os.Symlink(filepath.Join(root, "share"), filepath.Join(pkg, "share")); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(pkg)
	os.Exit(code)
}

func helper() string { return filepath.Join(pkg, "bin", "cc-harness-agents") }

type result struct {
	code        int
	signal      syscall.Signal
	stdout, err string
	pid         int
}

// run executes bin with a clean environment (only env) in dir.
func run(t *testing.T, bin, dir string, env []string, args ...string) result {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env, cmd.Dir = env, dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	r := result{stdout: out.String(), err: errb.String()}
	if cmd.Process != nil {
		r.pid = cmd.Process.Pid
	}
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		ws := ee.Sys().(syscall.WaitStatus)
		r.code, r.signal = ws.ExitStatus(), ws.Signal()
	case err != nil:
		t.Fatal(err)
	}
	return r
}

// localHome prepares a HOME with a sanctioned local route: token, one usable
// credential per provider and a fresh fallback marker. It returns HOME and
// the port of a live loopback "gateway".
func localHome(t *testing.T) (string, int) {
	t.Helper()
	home := t.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(".cli-proxy-api/client.key", "local-token\n")
	for _, cred := range []string{"xai", "kimi", "codex"} {
		write(".cli-proxy-api/"+cred+"-test.json", `{"access_token":"a","refresh_token":"r"}`)
	}
	expires := time.Now().Add(time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	write(".cache/cliproxy-auth/local-ready.json", `{"version":1,"expires_at":"`+expires+`","files":[]}`)
	l, err := testpki.Loopback()
	if err != nil {
		t.Fatal(err)
	}
	go testpki.AcceptAndClose(l)
	t.Cleanup(func() { l.Close() })
	return home, testpki.Port(l)
}

func localEnv(home string, port int, extra ...string) []string {
	return append([]string{"HOME=" + home, "PATH=/usr/bin:/bin", "CC_ROUTER_LOCAL_PORT=" + strconv.Itoa(port)}, extra...)
}

// The work-system contract: a captured `list` is exactly four TSV columns,
// one row per agent, in table order, no header.
func TestListIsFourColumnTSV(t *testing.T) {
	home, port := localHome(t)
	r := run(t, helper(), home, localEnv(home, port), "list", "--local")
	if r.code != 0 {
		t.Fatalf("list: %d %s", r.code, r.err)
	}
	lines := strings.Split(strings.TrimSuffix(r.stdout, "\n"), "\n")
	var names []string
	for _, line := range lines {
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("row has %d fields: %q", len(f), line)
		}
		if f[2] != "yes" && f[2] != "no" {
			t.Fatalf("available = %q", f[2])
		}
		names = append(names, f[0])
	}
	if got := strings.Join(names, " "); got != "cc-harness:grok cc-harness:kimi cc-harness:sol cc-harness:terra cc-harness:luna cc-harness:astra" {
		t.Fatalf("rows = %s", got)
	}
}

// exec REPLACES the process: the target runs under the helper's PID, sees its
// argv byte for byte, and its exit status and signal come back unchanged.
func TestExecReplacesTheProcess(t *testing.T) {
	home, port := localHome(t)
	env := localEnv(home, port)
	r := run(t, helper(), home, env, "exec", "--local", "sol", "--", "/bin/sh", "-c", `echo $$; exit 7`)
	if r.code != 7 || strings.TrimSpace(r.stdout) != strconv.Itoa(r.pid) {
		t.Fatalf("pid/exit: code %d, target pid %q, helper pid %d, stderr %s", r.code, r.stdout, r.pid, r.err)
	}
	r = run(t, helper(), home, env, "exec", "--local", "sol", "--", "sh", "-c", `kill -TERM $$`)
	if r.signal != syscall.SIGTERM {
		t.Fatalf("signal not propagated: %+v", r)
	}
	argv := []string{"", "two words", "-c", "*", "--", "ünï\tcode"}
	for _, name := range []string{"sol", "cc-harness:sol"} {
		// Without the `--` separator too: the rest is the command line either way.
		args := append([]string{"exec", "--local", name, "/bin/sh", "-c", `for a; do printf '<%s>' "$a"; done`, "sh"}, argv...)
		r = run(t, helper(), home, env, args...)
		want := ""
		for _, a := range argv {
			want += "<" + a + ">"
		}
		if r.code != 0 || r.stdout != want {
			t.Fatalf("%s argv: %q (%d %s), want %q", name, r.stdout, r.code, r.err, want)
		}
	}
	if r = run(t, helper(), home, env, "exec", "--local", "sol", "--", "no-such-command-xyz"); r.code != 127 {
		t.Fatalf("missing target: %+v", r)
	}
	if r = run(t, helper(), home, env, "exec", "--local", "nope", "--", "true"); r.code != 2 || !strings.Contains(r.err, "unknown agent 'nope'") {
		t.Fatalf("unknown agent: %+v", r)
	}
}

// The packaged table is found next to the REAL binary, through symlink chains
// (absolute and relative) and from any working directory.
func TestRelocationThroughSymlinks(t *testing.T) {
	dir := t.TempDir()
	os.Symlink(helper(), filepath.Join(dir, "abs"))
	os.Symlink("abs", filepath.Join(dir, "rel"))
	env := []string{"HOME=" + dir, "PATH=/usr/bin:/bin"}
	for _, link := range []string{"abs", "rel"} {
		r := run(t, filepath.Join(dir, link), "/", env, "resolve-model", "gpt-6-astra")
		if r.code != 0 || r.stdout != "astra\tgpt-6-astra\n" {
			t.Fatalf("%s: %+v", link, r)
		}
	}
	// A binary without its share/ fails closed, naming the table.
	lonely := filepath.Join(t.TempDir(), "bin", "cc-harness-agents")
	os.MkdirAll(filepath.Dir(lonely), 0o755)
	data, _ := os.ReadFile(helper())
	os.WriteFile(lonely, data, 0o755)
	if r := run(t, lonely, "/", env, "resolve-model", "gpt-6-astra"); r.code != 3 || !strings.Contains(r.err, "model table") {
		t.Fatalf("missing share: %+v", r)
	}
}

// ~/.config/cc-router/models.tsv REPLACES the packaged table for all three
// commands, is re-read per invocation, and fails closed when invalid.
func TestUserTableOverride(t *testing.T) {
	home, port := localHome(t)
	env := localEnv(home, port)
	table := filepath.Join(home, ".config", "cc-router", "models.tsv")
	os.MkdirAll(filepath.Dir(table), 0o700)
	os.WriteFile(table, []byte("zeta\tz-1\tz-1\tz-1\tz-1\t100000\tcodex\t-codex-login\tCodex\n"), 0o600)

	if r := run(t, helper(), home, env, "list", "--local"); r.stdout != "cc-harness:zeta\tz-1\tyes\t-\n" {
		t.Fatalf("list: %+v", r)
	}
	if r := run(t, helper(), home, env, "resolve-model", "gpt-6-astra"); r.code != 1 {
		t.Fatalf("a replaced table still knows astra: %+v", r)
	}
	if r := run(t, helper(), home, env, "exec", "--local", "zeta", "--", "sh", "-c", `echo "$ANTHROPIC_MODEL $CLAUDE_CODE_MAX_CONTEXT_TOKENS"`); r.stdout != "z-1 100000\n" {
		t.Fatalf("exec: %+v", r)
	}
	os.WriteFile(table, []byte("zeta\tz-2\tz-2\tz-2\tz-2\t100000\tcodex\t-codex-login\tCodex\n"), 0o600)
	if r := run(t, helper(), home, env, "resolve-model", "z-2"); r.stdout != "zeta\tz-2\n" {
		t.Fatalf("not re-read per invocation: %+v", r)
	}
	os.WriteFile(table, []byte("broken row\n"), 0o600)
	for _, args := range [][]string{{"list", "--local"}, {"resolve-model", "gpt-6-astra"}, {"exec", "--local", "sol", "--", "true"}} {
		if r := run(t, helper(), home, env, args...); r.code != 3 || !strings.Contains(r.err, "invalid model table") || r.stdout != "" {
			t.Fatalf("%v with an invalid override: %+v", args, r)
		}
	}
}

// config.env supplies settings; the environment overrides it.
func TestConfigFile(t *testing.T) {
	home, port := localHome(t)
	closed, _ := testpki.ClosedPort()
	cfg := filepath.Join(home, ".config", "cc-router", "config.env")
	os.MkdirAll(filepath.Dir(cfg), 0o700)
	os.WriteFile(cfg, []byte(fmt.Sprintf("# local gateway\nCC_ROUTER_LOCAL_PORT=%d\nCC_ROUTER_LOCAL_PREPARE_HINT=run: my-prepare\n", port)), 0o600)
	env := []string{"HOME=" + home, "PATH=/usr/bin:/bin"}

	if r := run(t, helper(), home, env, "list", "--local"); r.code != 0 || !strings.Contains(r.stdout, "\tyes\t") {
		t.Fatalf("file port: %+v", r)
	}
	r := run(t, helper(), home, append(env, "CC_ROUTER_LOCAL_PORT="+strconv.Itoa(closed)), "list", "--local")
	if !strings.Contains(r.stdout, fmt.Sprintf("nothing listening on 127.0.0.1:%d", closed)) {
		t.Fatalf("environment does not override the file: %+v", r)
	}
	os.Remove(filepath.Join(home, ".cache", "cliproxy-auth", "local-ready.json"))
	if r := run(t, helper(), home, env, "exec", "--local", "sol", "--", "true"); r.code != 3 || !strings.Contains(r.err, "run: my-prepare") {
		t.Fatalf("configured hint: %+v", r)
	}
	os.WriteFile(cfg, []byte("CLIPROXY_API_KEY_TEST=leak-me\n"), 0o600)
	if r := run(t, helper(), home, env, "list", "--local"); r.code != 3 || strings.Contains(r.err, "leak-me") {
		t.Fatalf("a secret in the config file: %+v", r)
	}
}

// The production binary trusts only the system store: a gateway whose
// certificate chains to a private CA fails TLS before any request — the
// secrets never reach it. Missing configuration is "capability absent".
func TestProductionRemoteRoute(t *testing.T) {
	ca, err := testpki.NewCA("not in the system store")
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++ }))
	srv.TLS, _ = ca.ServerConfig("127.0.0.1")
	srv.StartTLS()
	defer srv.Close()
	home := t.TempDir()
	secrets := []string{"CLIPROXY_PROFILE=TEST", "CLIPROXY_API_KEY_TEST=api-secret",
		"CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=id-secret", "CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=access-secret"}
	env := append([]string{"HOME=" + home, "PATH=/usr/bin:/bin", "CC_ROUTER_REMOTE_URL=" + srv.URL}, secrets...)

	r := run(t, helper(), home, env, "exec", "sol", "--", "true")
	if r.code != 1 || !strings.Contains(r.err, "remote TLS validation failed") || requests != 0 {
		t.Fatalf("untrusted gateway: %+v, requests %d", r, requests)
	}
	for _, s := range []string{"api-secret", "id-secret", "access-secret"} {
		if strings.Contains(r.err+r.stdout, s) {
			t.Fatalf("diagnostic leaks %s", s)
		}
	}
	r = run(t, helper(), home, append([]string{"HOME=" + home, "PATH=/usr/bin:/bin"}, secrets...), "list")
	if r.code != 3 || !strings.Contains(r.err, "CC_ROUTER_REMOTE_URL") {
		t.Fatalf("unconfigured remote: %+v", r)
	}
}

func TestUsageAndEnvironment(t *testing.T) {
	home := t.TempDir()
	env := []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
	if r := run(t, helper(), home, env, "--help"); r.code != 0 || !strings.HasPrefix(r.stdout, "usage:") || r.err != "" {
		t.Fatalf("--help: %+v", r)
	}
	for _, args := range [][]string{{}, {"bogus"}, {"list", "--bogus"}, {"exec", "sol"}, {"exec"}, {"resolve-model"}} {
		if r := run(t, helper(), home, env, args...); r.code != 2 || r.stdout != "" || !strings.Contains(r.err, "usage:") {
			t.Fatalf("%v: %+v", args, r)
		}
	}
	if r := run(t, helper(), home, []string{"PATH=/usr/bin:/bin"}, "resolve-model", "gpt-6-astra"); r.code != 3 || !strings.Contains(r.err, "HOME is not set") {
		t.Fatalf("no HOME: %+v", r)
	}
}
