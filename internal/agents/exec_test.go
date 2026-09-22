package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoutedEnv(t *testing.T) {
	base := NewEnv([]string{"PATH=/bin", "CLAUDE_CODE_USE_BEDROCK=1", "ANTHROPIC_MODEL=old"})
	eff := packagedRows(t)[5]
	env := routedEnv(base, routing{route: "local", baseURL: "http://127.0.0.1:8317", token: "tok", headers: "X-A: 1", noProxy: "127.0.0.1,localhost"}, eff)
	want := map[string]string{
		"PATH": "/bin", "ANTHROPIC_BASE_URL": "http://127.0.0.1:8317", "ANTHROPIC_AUTH_TOKEN": "tok",
		"ANTHROPIC_CUSTOM_HEADERS": "X-A: 1", "CLIPROXY_ROUTE": "local", "NO_PROXY": "127.0.0.1,localhost",
		"no_proxy": "127.0.0.1,localhost", "ANTHROPIC_MODEL": "gpt-6-astra", "CLAUDE_CODE_SUBAGENT_MODEL": "gpt-6-astra",
		"ANTHROPIC_DEFAULT_OPUS_MODEL": "gpt-6-astra", "ANTHROPIC_DEFAULT_SONNET_MODEL": "gpt-6-astra",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL": "gpt-5.6-luna", "CLAUDE_CODE_MAX_CONTEXT_TOKENS": "900000",
	}
	for _, s := range providerSelectors {
		want[s] = ""
	}
	for k, v := range want {
		if got, ok := env.Lookup(k); !ok || got != v {
			t.Errorf("%s = %q (set %v), want %q", k, got, ok, v)
		}
	}
	if base.Get("ANTHROPIC_MODEL") != "old" {
		t.Fatal("routedEnv mutated the inherited environment")
	}
	// The remote recipe leaves both proxy-bypass variables alone.
	remote := routedEnv(NewEnv([]string{"NO_PROXY=corp", "no_proxy=lower"}), routing{route: "remote"}, eff)
	if remote.Get("NO_PROXY") != "corp" || remote.Get("no_proxy") != "lower" {
		t.Fatal("remote route rewrote NO_PROXY")
	}
}

func TestLocalNoProxy(t *testing.T) {
	for _, c := range []struct {
		env  []string
		want string
	}{
		{nil, "127.0.0.1,localhost"},
		{[]string{"NO_PROXY=a", "no_proxy=b"}, "127.0.0.1,localhost,a,b"},
		{[]string{"NO_PROXY=a", "no_proxy=a"}, "127.0.0.1,localhost,a"},
		{[]string{"no_proxy=b"}, "127.0.0.1,localhost,b"},
	} {
		if got := localNoProxy(NewEnv(c.env), "127.0.0.1"); got != c.want {
			t.Errorf("%v: %q", c.env, got)
		}
	}
}

func TestLookPath(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "tool")
	writeFile(t, exe, "#!/bin/sh\n")
	os.Chmod(exe, 0o755)
	writeFile(t, filepath.Join(dir, "plain"), "")
	if got, err := lookPath("tool", "/nonexistent:"+dir); err != nil || got != exe {
		t.Fatalf("lookPath = %q %v", got, err)
	}
	if _, err := lookPath("plain", dir); err == nil {
		t.Fatal("a non-executable file was found")
	}
	if got, _ := lookPath("./x/y", ""); got != "./x/y" {
		t.Fatal("a path with a slash is not taken as is")
	}
	if !strings.HasSuffix(exe, "tool") {
		t.Fatal()
	}
}
