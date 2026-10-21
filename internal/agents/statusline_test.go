package agents

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/gering/cc-router/internal/testpki"
)

// Spec: dotfiles scripts/test-cliproxy-quota.sh (see tests/migration-coverage.md).

const (
	quotaKey    = "remote-api-secret"
	quotaID     = "access-id-secret"
	quotaSecret = "access-client-secret"
)

// quotaHome is a HOME whose table is the packaged one plus a kimi row: the
// remote path must serve a provider that returns as a row, with no code.
func quotaHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	data, err := os.ReadFile("../../share/models.tsv")
	if err != nil {
		t.Fatal(err)
	}
	kimi := "kimi\tkimi-k3\tkimi-k3\tkimi-k3\tkimi-k3\tkimi-k3\t256000\tkimi\t-kimi-login\tKimi\n"
	writeFile(t, filepath.Join(home, ".config", "cc-router", "models.tsv"), string(data)+kimi)
	return home
}

type spawned struct {
	mu    sync.Mutex
	calls [][]string
}

func (s *spawned) spawn(path string, argv, env []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, argv[1:])
	return nil
}

func (s *spawned) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

type quotaRun struct {
	app            *App
	stdout, stderr strings.Builder
	spawn          spawned
}

// quotaApp is an invocation for home with the remote profile TEST set and
// trust in ca only. Extra entries override.
func quotaApp(home string, ca *testpki.CA, env ...string) *quotaRun {
	base := []string{
		"HOME=" + home, "CLIPROXY_PROFILE=TEST",
		"CLIPROXY_API_KEY_TEST=" + quotaKey,
		"CLIPROXY_CF_ACCESS_TEST_CLIENT_ID=" + quotaID,
		"CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=" + quotaSecret,
		"CC_ROUTER_REMOTE_URL=https://127.0.0.1:1",
	}
	r := &quotaRun{}
	r.app = &App{
		Env:            NewEnv(append(base, env...)),
		Stdin:          strings.NewReader(""),
		Stdout:         &r.stdout,
		Stderr:         &r.stderr,
		Now:            time.Now,
		ConnectTimeout: time.Second,
		TotalTimeout:   2 * time.Second,
		Spawn:          r.spawn.spawn,
	}
	if ca != nil {
		r.app.RootCAs = ca.Pool()
	}
	return r
}

func (r *quotaRun) render(t *testing.T, model string, args ...string) (string, int) {
	t.Helper()
	r.app.Stdin = strings.NewReader(`{"model":{"id":"` + model + `"}}`)
	r.stdout.Reset()
	code := r.app.Run(append([]string{"statusline"}, args...))
	return r.stdout.String(), code
}

func (r *quotaRun) refresh(route, provider string) int {
	return r.app.Run([]string{"statusline", "--refresh", route, provider})
}

func cacheFile(home, provider, route string) string {
	return filepath.Join(home, ".cache", "cc-router", "quota", provider+"-"+route+".json")
}

// writeQuotaCache publishes a snapshot as a refresh would, aged by age.
func writeQuotaCache(t *testing.T, home, provider, route, q string, source time.Time, age time.Duration) {
	t.Helper()
	path := cacheFile(home, provider, route)
	os.MkdirAll(filepath.Dir(path), 0o700)
	writeFile(t, path, fmt.Sprintf(`{"quota":%s,"source_epoch":%d}`, q, source.Unix()))
	os.Chmod(path, 0o600)
	mtime := time.Now().Add(-age)
	os.Chtimes(path, mtime, mtime)
}

func cachedQuota(t *testing.T, home, provider, route string) string {
	t.Helper()
	data, err := os.ReadFile(cacheFile(home, provider, route))
	if err != nil {
		return ""
	}
	var snap quotaSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("cache: %v", err)
	}
	out, _ := json.Marshal(snap.Quota)
	return string(out)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func assertSecretSafe(t *testing.T, label string, texts ...string) {
	t.Helper()
	for _, s := range []string{quotaKey, quotaID, quotaSecret} {
		for _, text := range texts {
			if strings.Contains(text, s) {
				t.Errorf("%s: a secret leaked", label)
			}
		}
	}
}

func TestStatuslineFreshCacheIsShownWithoutRefresh(t *testing.T) {
	home := quotaHome(t)
	writeQuotaCache(t, home, "codex", "remote", `{"five_hour":{"used_percentage":11.4,"reset_at":101},"seven_day":{"used_percentage":80}}`, time.Now(), 0)
	r := quotaApp(home, nil, "CLIPROXY_ROUTE=remote")
	if out, code := r.render(t, "gpt-6-sol"); out != "⇄ 5h 11% 7d 80%\n" || code != 0 {
		t.Fatalf("segment = %q (%d)", out, code)
	}
	if out, _ := r.render(t, "gpt-6-sol", "--json"); out != `{"route":"remote","five_hour":{"used_percentage":11.4,"reset_at":101},"seven_day":{"used_percentage":80}}`+"\n" {
		t.Fatalf("json = %q", out)
	}
	if r.spawn.count() != 0 {
		t.Fatal("a fresh cache started a refresh")
	}
}

func TestStatuslineStaleCacheIsShownAndRefreshed(t *testing.T) {
	home := quotaHome(t)
	writeQuotaCache(t, home, "xai", "local", `{"seven_day":{"used_percentage":12}}`, time.Now().Add(-10*time.Minute), 10*time.Minute)
	r := quotaApp(home, nil, "CLIPROXY_ROUTE=local")
	if out, _ := r.render(t, "grok-4.6"); out != "⌂ 7d 12%\n" {
		t.Fatalf("segment = %q", out)
	}
	if r.spawn.count() != 1 || strings.Join(r.spawn.calls[0], " ") != "statusline --refresh local xai" {
		t.Fatalf("spawned %v", r.spawn.calls)
	}
}

// Data whose source is older than 30 minutes, or a cache that is not one, is
// hidden while a refresh is attempted.
func TestStatuslineHidesWhatItCannotTrust(t *testing.T) {
	cases := map[string]func(t *testing.T, home string){
		"none": func(*testing.T, string) {},
		"source too old": func(t *testing.T, home string) {
			writeQuotaCache(t, home, "codex", "remote", `{"five_hour":{"used_percentage":20}}`, time.Now().Add(-31*time.Minute), 0)
		},
		"source from the future": func(t *testing.T, home string) {
			writeQuotaCache(t, home, "codex", "remote", `{"five_hour":{"used_percentage":20}}`, time.Now().Add(time.Hour), 0)
		},
		"unknown field": func(t *testing.T, home string) {
			writeQuotaCache(t, home, "codex", "remote", `{"five_hour":{"used_percentage":20}},"extra":1`, time.Now(), 0)
		},
		"unknown window": func(t *testing.T, home string) {
			writeQuotaCache(t, home, "codex", "remote", `{"monthly":{"used_percentage":20}}`, time.Now(), 0)
		},
		"percentage out of range": func(t *testing.T, home string) {
			writeQuotaCache(t, home, "codex", "remote", `{"five_hour":{"used_percentage":120}}`, time.Now(), 0)
		},
		"readable by others": func(t *testing.T, home string) {
			writeQuotaCache(t, home, "codex", "remote", `{"five_hour":{"used_percentage":20}}`, time.Now(), 0)
			os.Chmod(cacheFile(home, "codex", "remote"), 0o644)
		},
		"symlink": func(t *testing.T, home string) {
			writeQuotaCache(t, home, "codex", "local", `{"five_hour":{"used_percentage":20}}`, time.Now(), 0)
			os.Symlink(cacheFile(home, "codex", "local"), cacheFile(home, "codex", "remote"))
		},
		// A fresh local cache is never the remote one.
		"other route": func(t *testing.T, home string) {
			writeQuotaCache(t, home, "codex", "local", `{"five_hour":{"used_percentage":99}}`, time.Now(), 0)
		},
	}
	for name, setup := range cases {
		home := quotaHome(t)
		setup(t, home)
		r := quotaApp(home, nil, "CLIPROXY_ROUTE=remote")
		if out, _ := r.render(t, "gpt-6-luna"); out != "⇄\n" {
			t.Errorf("%s: segment = %q", name, out)
		}
		if r.spawn.count() != 1 {
			t.Errorf("%s: %d refreshes started", name, r.spawn.count())
		}
	}
}

func TestStatuslineNativeAndUnknownModels(t *testing.T) {
	home := quotaHome(t)
	for _, route := range []string{"", "bogus"} {
		r := quotaApp(home, nil, "CLIPROXY_ROUTE="+route)
		if out, code := r.render(t, "gpt-6-sol"); out != "" || code != 0 {
			t.Errorf("route %q: segment = %q", route, out)
		}
		if out, _ := r.render(t, "gpt-6-sol", "--json"); out != "{}\n" {
			t.Errorf("route %q: json = %q", route, out)
		}
		if r.spawn.count() != 0 {
			t.Errorf("route %q: a native session started a refresh", route)
		}
	}
	// Routed, but no row serves the model, or the input is not JSON: marker only.
	r := quotaApp(home, nil, "CLIPROXY_ROUTE=remote")
	for _, model := range []string{"mystery-1", `"x`} {
		if out, _ := r.render(t, model); out != "⇄\n" {
			t.Errorf("%s: segment = %q", model, out)
		}
	}
	if r.spawn.count() != 0 {
		t.Error("an unknown model started a refresh")
	}
}

func TestStatuslineFailureMarkerBacksOff(t *testing.T) {
	home := quotaHome(t)
	failed := strings.TrimSuffix(cacheFile(home, "codex", "remote"), ".json") + ".failed"
	mark := func(content string, age time.Duration) {
		os.MkdirAll(filepath.Dir(failed), 0o700)
		writeFile(t, failed, content)
		os.Chmod(failed, 0o600)
		os.Chtimes(failed, time.Now().Add(-age), time.Now().Add(-age))
	}
	cases := []struct {
		name    string
		content string
		age     time.Duration
		backoff bool
	}{
		{"retry ahead", strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10), 0, true},
		{"retry passed", strconv.FormatInt(time.Now().Add(-time.Second).Unix(), 10), 0, false},
		{"unreadable, recent", "junk", 30 * time.Second, true},
		{"unreadable, old", "junk", 2 * time.Minute, false},
		{"retry beyond the long backoff", strconv.FormatInt(time.Now().Add(24*time.Hour).Unix(), 10), 31 * time.Minute, false},
	}
	for _, c := range cases {
		mark(c.content, c.age)
		r := quotaApp(home, nil, "CLIPROXY_ROUTE=remote")
		r.render(t, "gpt-6-sol")
		if got := r.spawn.count() == 0; got != c.backoff {
			t.Errorf("%s: backing off = %v", c.name, got)
		}
	}
}

// The render path never dials: neither the usage API nor a proxy sees a
// connection, even for a stale cache, and a stuck input yields nothing.
func TestStatuslineRenderIsNetworkFree(t *testing.T) {
	ca := newCA(t)
	srv := gateway(t, ca, []string{"127.0.0.1"}, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the render path reached the usage API")
	})
	l, err := testpki.Loopback()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	var dials atomic.Int32
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			dials.Add(1)
			c.Close()
		}
	}()
	home := quotaHome(t)
	writeQuotaCache(t, home, "codex", "remote", `{"five_hour":{"used_percentage":5}}`, time.Now().Add(-10*time.Minute), 10*time.Minute)
	proxy := fmt.Sprintf("http://127.0.0.1:%d", testpki.Port(l))
	r := quotaApp(home, ca, "CLIPROXY_ROUTE=remote", "CC_ROUTER_REMOTE_URL="+srv.URL, "https_proxy="+proxy, "ALL_PROXY="+proxy)
	r.app.Spawn = nil
	for _, args := range [][]string{nil, {"--json"}} {
		if out, _ := r.render(t, "gpt-6-sol", args...); !strings.Contains(out, "5") {
			t.Errorf("%v: %q", args, out)
		}
	}
	if dials.Load() != 0 {
		t.Fatal("the render path dialed")
	}

	stuck, w := io.Pipe()
	defer w.Close()
	r.app.Stdin = stuck
	r.stdout.Reset()
	start := time.Now()
	r.app.Run([]string{"statusline"})
	if r.stdout.String() != "" || time.Since(start) > 2*renderTimeout {
		t.Fatalf("stuck input: %q after %v", r.stdout.String(), time.Since(start))
	}
}

// accountBody is one ok account of the companion's answer.
func accountBody(provider, limits string, fetched time.Time) string {
	return fmt.Sprintf(`{"accounts":[{"id":"abcdefghijklmnopqrstuv","provider":%q,"status":"ok","fetched_at":%q,"stale":false,"rate_limits":%s}]}`,
		provider, fetched.UTC().Format(time.RFC3339), limits)
}

func TestQuotaRemoteRefresh(t *testing.T) {
	ca := newCA(t)
	// The remote path is generic: kimi is only a table row here.
	for _, c := range []struct{ provider, model, window string }{
		{"codex", "gpt-6-sol", "five_hour"}, {"xai", "grok-4.6", "seven_day"}, {"kimi", "kimi-k3", "five_hour"},
	} {
		var got http.Header
		var path string
		var requests atomic.Int32
		srv := gateway(t, ca, []string{"127.0.0.1"}, func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			got, path = r.Header.Clone(), r.URL.Path
			io.WriteString(w, accountBody(c.provider, `{"`+c.window+`":{"used_percentage":31,"reset_at":3131},"monthly":{"x":1}}`, time.Now()))
		})
		home := quotaHome(t)
		r := quotaApp(home, ca, "CC_ROUTER_USAGE_URL="+srv.URL+"/base")
		if code := r.refresh("remote", c.provider); code != 0 || requests.Load() != 1 {
			t.Fatalf("%s: exit %d, %d requests: %s", c.provider, code, requests.Load(), r.stderr.String())
		}
		if path != "/base/usage/v1/"+c.provider {
			t.Errorf("%s: path %s", c.provider, path)
		}
		for k, v := range map[string]string{
			"Authorization": "Bearer " + quotaKey, "Cf-Access-Client-Id": quotaID,
			"Cf-Access-Client-Secret": quotaSecret, "Accept": "application/json",
		} {
			if got.Get(k) != v {
				t.Errorf("%s: header %s wrong", c.provider, k)
			}
		}
		if q := cachedQuota(t, home, c.provider, "remote"); q != `{"`+c.window+`":{"used_percentage":31,"reset_at":3131}}` {
			t.Errorf("%s: cache %s", c.provider, q)
		}
		data, _ := os.ReadFile(cacheFile(home, c.provider, "remote"))
		info, _ := os.Stat(cacheFile(home, c.provider, "remote"))
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s: cache mode %v", c.provider, info.Mode())
		}
		assertSecretSafe(t, c.provider, string(data), r.stdout.String(), r.stderr.String())
		// The published cache is what the next render shows.
		r.app.Env.Set("CLIPROXY_ROUTE", "remote")
		if out, _ := r.render(t, c.model); !strings.Contains(out, "31%") || r.spawn.count() != 0 {
			t.Errorf("%s: render %q", c.provider, out)
		}
	}
}

// The usage API defaults to the gateway itself.
func TestQuotaUsageURLDefaultsToTheGateway(t *testing.T) {
	ca := newCA(t)
	var path string
	srv := gateway(t, ca, []string{"127.0.0.1"}, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		io.WriteString(w, accountBody("codex", `{}`, time.Now()))
	})
	r := quotaApp(quotaHome(t), ca, "CC_ROUTER_REMOTE_URL="+srv.URL)
	if code := r.refresh("remote", "codex"); code != 0 || path != "/usage/v1/codex" {
		t.Fatalf("exit %d, path %q: %s", code, path, r.stderr.String())
	}
}

func TestQuotaRemoteFailuresAreSafe(t *testing.T) {
	ca, other := newCA(t), newCA(t)
	respond := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if status == http.StatusFound {
				w.Header().Set("Location", "/elsewhere")
			}
			w.WriteHeader(status)
			io.WriteString(w, body)
		}
	}
	long, short := quotaLongBackoff, quotaFailureBackoff
	cases := []struct {
		name    string
		h       http.HandlerFunc
		ca      *testpki.CA
		want    string
		backoff time.Duration
	}{
		{"TLS", respond(200, "{}"), other, "remote TLS validation failed", short},
		{"redirect", respond(302, ""), ca, "redirects are disabled", short},
		{"401", respond(401, ""), ca, "remote proxy API key rejected (HTTP 401)", long},
		{"403", respond(403, ""), ca, "Cloudflare Access rejected the client credentials (HTTP 403)", long},
		{"404", respond(404, ""), ca, "remote usage request failed (HTTP 404)", long},
		{"502", respond(502, ""), ca, "remote proxy origin unavailable (HTTP 502)", short},
		{"oversize", respond(200, strings.Repeat(" ", remoteMaxBodyBytes+1)), ca, "remote usage request response exceeds 1048576 bytes", short},
		{"JSON", respond(200, "not-json"), ca, "unexpected shape", short},
		{"schema", respond(200, `{"accounts":"bad"}`), ca, "unexpected shape", short},
		{"wrong provider", respond(200, accountBody("xai", `{}`, time.Now())), ca, "unexpected shape", short},
		{"old data", respond(200, accountBody("codex", `{"five_hour":{"used_percentage":55}}`, time.Now().Add(-31*time.Minute))), ca, "too old", short},
		{"future data", respond(200, accountBody("codex", `{"five_hour":{"used_percentage":56}}`, time.Now().Add(time.Hour))), ca, "from the future", short},
	}
	for _, c := range cases {
		srv := gateway(t, c.ca, []string{"127.0.0.1"}, c.h)
		home := quotaHome(t)
		r := quotaApp(home, ca, "CC_ROUTER_REMOTE_URL="+srv.URL)
		before := time.Now()
		if code := r.refresh("remote", "codex"); code != exitUnavailable || !strings.Contains(r.stderr.String(), c.want) {
			t.Errorf("%s: exit %d, stderr %q", c.name, code, r.stderr.String())
		}
		if exists(cacheFile(home, "codex", "remote")) {
			t.Errorf("%s: a cache was written", c.name)
		}
		data, _ := os.ReadFile(strings.TrimSuffix(cacheFile(home, "codex", "remote"), ".json") + ".failed")
		retry, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if err != nil || retry < before.Add(c.backoff).Unix() || retry > time.Now().Add(c.backoff).Unix() {
			t.Errorf("%s: failure marker %q, want retry after %v", c.name, data, c.backoff)
		}
		assertSecretSafe(t, c.name, r.stdout.String(), r.stderr.String())
	}
	// A transport failure names the network.
	port, _ := testpki.ClosedPort()
	r := quotaApp(quotaHome(t), ca, fmt.Sprintf("CC_ROUTER_REMOTE_URL=https://127.0.0.1:%d", port))
	if r.refresh("remote", "codex"); !strings.Contains(r.stderr.String(), "remote network request failed") {
		t.Errorf("network: %q", r.stderr.String())
	}
}

// Missing or malformed credentials stop the refresh before any request: a
// keyless probe counts against the gateway's failed-auth ban.
func TestQuotaNeverSendsAKeylessRequest(t *testing.T) {
	ca := newCA(t)
	var requests atomic.Int32
	srv := gateway(t, ca, []string{"127.0.0.1"}, func(w http.ResponseWriter, r *http.Request) { requests.Add(1) })
	for _, c := range []struct{ env, want string }{
		{"CLIPROXY_API_KEY_TEST=", "CLIPROXY_API_KEY_TEST is empty"},
		{"CLIPROXY_API_KEY_TEST=" + quotaKey + "\nsecond-line", "CLIPROXY_API_KEY_TEST contains a control character"},
		{"CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET=", "CLIPROXY_CF_ACCESS_TEST_CLIENT_SECRET is empty"},
		{"CLIPROXY_PROFILE=bad profile", "CLIPROXY_PROFILE"},
		{"CC_ROUTER_USAGE_URL=http://plain.example", "CC_ROUTER_USAGE_URL"},
	} {
		home := quotaHome(t)
		r := quotaApp(home, ca, "CC_ROUTER_REMOTE_URL="+srv.URL, c.env)
		if code := r.refresh("remote", "codex"); code == 0 || !strings.Contains(r.stderr.String(), c.want) {
			t.Errorf("%q: exit %d, stderr %q", c.env, code, r.stderr.String())
		}
		if exists(cacheFile(home, "codex", "remote")) {
			t.Errorf("%q: a cache was written", c.env)
		}
		assertSecretSafe(t, c.env, r.stdout.String(), r.stderr.String())
	}
	if requests.Load() != 0 {
		t.Fatalf("%d requests without a usable credential", requests.Load())
	}
}

// Several accounts aggregate conservatively: each window takes the highest
// valid usage; error accounts and invalid windows are ignored.
func TestParseRemoteUsage(t *testing.T) {
	now := time.Now()
	at := now.UTC().Format(time.RFC3339)
	body := `{"accounts":[
		{"id":"a","provider":"codex","status":"ok","fetched_at":"` + at + `","stale":false,"rate_limits":{"five_hour":{"used_percentage":40,"reset_at":400},"seven_day":{"used_percentage":80,"reset_at":800}}},
		{"id":"b","provider":"codex","status":"ok","fetched_at":"` + at + `","stale":false,"rate_limits":{"five_hour":{"used_percentage":70,"reset_at":"invalid"},"seven_day":{"used_percentage":110,"reset_at":1100}}},
		{"id":"c","provider":"codex","status":"error","fetched_at":"` + at + `","stale":false,"error":{"category":"upstream_unavailable"}},
		{"id":"d","provider":"codex","status":"ok","fetched_at":"` + at + `","stale":true,"rate_limits":{"seven_day":{"used_percentage":90,"reset_at":900}},"error":{"category":"upstream_rejected"}}
	],"future_field":1}`
	snap, err := parseRemoteUsage([]byte(body), "codex", now)
	out, _ := json.Marshal(snap.Quota)
	if err != nil || string(out) != `{"five_hour":{"used_percentage":70},"seven_day":{"used_percentage":90,"reset_at":900}}` {
		t.Fatalf("quota = %s, %v", out, err)
	}
	// Missing windows stay absent; the source time is the oldest one shown.
	old := now.Add(-10 * time.Minute)
	snap, err = parseRemoteUsage([]byte(accountBody("xai", `{"seven_day":{"used_percentage":23,"reset_at":2300}}`, old)), "xai", now)
	out, _ = json.Marshal(snap.Quota)
	if err != nil || string(out) != `{"seven_day":{"used_percentage":23,"reset_at":2300}}` || snap.SourceEpoch != old.Unix() {
		t.Fatalf("quota = %s @%d, %v", out, snap.SourceEpoch, err)
	}
	for name, body := range map[string]string{
		"status":       `{"accounts":[{"provider":"codex","status":"weird","fetched_at":"` + at + `"}]}`,
		"fetched_at":   `{"accounts":[{"provider":"codex","status":"ok","fetched_at":"yesterday","rate_limits":{}}]}`,
		"rate_limits":  `{"accounts":[{"provider":"codex","status":"ok","fetched_at":"` + at + `"}]}`,
		"not an array": `{"accounts":{}}`,
		"trailing":     `{"accounts":[]} {}`,
	} {
		if _, err := parseRemoteUsage([]byte(body), "codex", now); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestQuotaRefreshLock(t *testing.T) {
	ca := newCA(t)
	var requests atomic.Int32
	srv := gateway(t, ca, []string{"127.0.0.1"}, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		time.Sleep(300 * time.Millisecond)
		io.WriteString(w, accountBody("codex", `{"five_hour":{"used_percentage":61}}`, time.Now()))
	})
	home := quotaHome(t)
	lockPath := strings.TrimSuffix(cacheFile(home, "codex", "remote"), ".json") + ".lock"
	os.MkdirAll(filepath.Dir(lockPath), 0o700)

	// A held lock suppresses the duplicate request.
	held, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	syscall.Flock(int(held.Fd()), syscall.LOCK_EX)
	r := quotaApp(home, ca, "CC_ROUTER_REMOTE_URL="+srv.URL)
	if code := r.refresh("remote", "codex"); code != 0 || requests.Load() != 0 {
		t.Fatalf("held lock: exit %d, %d requests", code, requests.Load())
	}
	// Its holder gone, the lock file left behind is no obstacle.
	held.Close()
	if code := r.refresh("remote", "codex"); code != 0 || requests.Load() != 1 {
		t.Fatalf("released lock: exit %d, %d requests", code, requests.Load())
	}

	// Two parallel refreshers perform exactly one fetch.
	os.Remove(cacheFile(home, "codex", "remote"))
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			quotaApp(home, ca, "CC_ROUTER_REMOTE_URL="+srv.URL).refresh("remote", "codex")
		}()
	}
	wg.Wait()
	if requests.Load() != 2 || cachedQuota(t, home, "codex", "remote") != `{"five_hour":{"used_percentage":61}}` {
		t.Fatalf("parallel: %d requests, cache %s", requests.Load(), cachedQuota(t, home, "codex", "remote"))
	}
}

// A successful refresh clears the failure marker.
func TestQuotaSuccessClearsTheFailureMarker(t *testing.T) {
	ca := newCA(t)
	var fail atomic.Bool
	fail.Store(true)
	srv := gateway(t, ca, []string{"127.0.0.1"}, func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(500)
			return
		}
		io.WriteString(w, accountBody("codex", `{"five_hour":{"used_percentage":7}}`, time.Now()))
	})
	home := quotaHome(t)
	failed := strings.TrimSuffix(cacheFile(home, "codex", "remote"), ".json") + ".failed"
	r := quotaApp(home, ca, "CC_ROUTER_REMOTE_URL="+srv.URL)
	r.refresh("remote", "codex")
	if !exists(failed) {
		t.Fatal("no failure marker")
	}
	fail.Store(false)
	if code := r.refresh("remote", "codex"); code != 0 || exists(failed) || !exists(cacheFile(home, "codex", "remote")) {
		t.Fatalf("exit %d, marker %v", code, exists(failed))
	}
}

func TestQuotaRefreshUsage(t *testing.T) {
	r := quotaApp(quotaHome(t), nil)
	for _, args := range [][]string{
		{"--refresh", "native", "codex"}, {"--refresh", "remote", "../x"}, {"--refresh", "remote"}, {"--bogus"},
	} {
		if code := r.app.Run(append([]string{"statusline"}, args...)); code != exitUsage {
			t.Errorf("%v: exit %d", args, code)
		}
	}
	if code := r.refresh("remote", "nobody"); code != exitUsage || !strings.Contains(r.stderr.String(), "no table row uses provider nobody") {
		t.Errorf("unknown provider: exit %d, %q", code, r.stderr.String())
	}
}

// useLocalSource points one provider's local usage source at a test server.
func useLocalSource(t *testing.T, provider, url string) {
	t.Helper()
	orig := localUsage[provider]
	src := orig
	src.url = url
	localUsage[provider] = src
	t.Cleanup(func() { localUsage[provider] = orig })
}

func TestQuotaLocalCodex(t *testing.T) {
	ca := newCA(t)
	var got http.Header
	srv := gateway(t, ca, []string{"127.0.0.1"}, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		io.WriteString(w, `{"rate_limit":{"primary_window":{"limit_window_seconds":18000,"used_percent":17,"reset_at":1700},"secondary_window":{"limit_window_seconds":604800,"used_percent":45,"reset_at":4500}}}`)
	})
	useLocalSource(t, "codex", srv.URL+"/wham/usage")
	home := quotaHome(t)
	// The newest proxy file has no token: the codex CLI's own login is next.
	writeFile(t, filepath.Join(home, ".cli-proxy-api", "codex-newest.json"), `{"token_data":{}}`)
	writeFile(t, filepath.Join(home, ".codex", "auth.json"), `{"tokens":{"access_token":"codex-native-token","account_id":"codex-account"}}`)
	r := quotaApp(home, ca)
	if code := r.refresh("local", "codex"); code != 0 {
		t.Fatalf("exit %d: %s", code, r.stderr.String())
	}
	if q := cachedQuota(t, home, "codex", "local"); q != `{"five_hour":{"used_percentage":17,"reset_at":1700},"seven_day":{"used_percentage":45,"reset_at":4500}}` {
		t.Fatalf("cache %s", q)
	}
	if got.Get("Authorization") != "Bearer codex-native-token" || got.Get("ChatGPT-Account-ID") != "codex-account" {
		t.Fatal("wrong local credentials sent")
	}
	if exists(cacheFile(home, "codex", "remote")) {
		t.Fatal("the local refresh wrote the remote cache")
	}
}

func TestQuotaLocalGrok(t *testing.T) {
	ca := newCA(t)
	var auth string
	srv := gateway(t, ca, []string{"127.0.0.1"}, func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		io.WriteString(w, `{"config":{"creditUsagePercent":12,"billingPeriodEnd":"2026-08-07T00:00:00Z"}}`)
	})
	useLocalSource(t, "xai", srv.URL+"/v1/billing")
	home := quotaHome(t)
	writeFile(t, filepath.Join(home, ".cli-proxy-api", "xai-newest.json"), `{"disabled":true,"access_token":"disabled-token"}`)
	writeFile(t, filepath.Join(home, ".grok", "auth.json"), `{"issuer::client":{"key":"grok-native-token"}}`)
	r := quotaApp(home, ca)
	if code := r.refresh("local", "xai"); code != 0 {
		t.Fatalf("exit %d: %s", code, r.stderr.String())
	}
	if q := cachedQuota(t, home, "xai", "local"); q != `{"seven_day":{"used_percentage":12,"reset_at":1786060800}}` {
		t.Fatalf("cache %s", q)
	}
	if auth != "Bearer grok-native-token" {
		t.Fatal("the disabled proxy credential was used")
	}
}

// A provider without a local usage source, or without credentials, sends
// nothing.
func TestQuotaLocalWithoutASource(t *testing.T) {
	ca := newCA(t)
	var requests atomic.Int32
	srv := gateway(t, ca, []string{"127.0.0.1"}, func(w http.ResponseWriter, r *http.Request) { requests.Add(1) })
	useLocalSource(t, "codex", srv.URL)
	home := quotaHome(t)
	r := quotaApp(home, ca)
	if code := r.refresh("local", "claude"); code != exitUnavailable || !strings.Contains(r.stderr.String(), "no local usage source for provider claude") {
		t.Errorf("claude: exit %d, %q", code, r.stderr.String())
	}
	if code := r.refresh("local", "codex"); code != exitUnavailable || !strings.Contains(r.stderr.String(), "no usable codex credentials") {
		t.Errorf("codex: exit %d, %q", code, r.stderr.String())
	}
	if requests.Load() != 0 {
		t.Fatal("a request without a credential")
	}
}

func TestParseLocalUsage(t *testing.T) {
	for _, c := range []struct {
		name  string
		parse func([]byte) (quota, error)
		body  string
		want  string
	}{
		{"codex between windows", parseCodexUsage, `{"rate_limit":{"primary_window":{"limit_window_seconds":86400,"used_percent":5}}}`, `{}`},
		{"codex bad percent", parseCodexUsage, `{"rate_limit":{"primary_window":{"limit_window_seconds":18000,"used_percent":"5"},"secondary_window":{"limit_window_seconds":604800,"used_percent":9,"reset_at":1.5}}}`, `{"seven_day":{"used_percentage":9}}`},
		{"codex no rate_limit", parseCodexUsage, `{}`, ``},
		{"grok current period, offset", parseGrokUsage, `{"config":{"creditUsagePercent":3.5,"currentPeriod":{"end":"2026-08-07T02:00:00.123+0200"}}}`, `{"seven_day":{"used_percentage":3.5,"reset_at":1786060800}}`},
		{"grok no usage", parseGrokUsage, `{"config":{}}`, ``},
	} {
		q, err := c.parse([]byte(c.body))
		got := ""
		if err == nil {
			out, _ := json.Marshal(q)
			got = string(out)
		}
		if got != c.want {
			t.Errorf("%s: %s (%v)", c.name, got, err)
		}
	}
}

// The detached refresh never holds up its caller.
func TestSpawnDetachedDoesNotWait(t *testing.T) {
	start := time.Now()
	if err := spawnDetached("/bin/sleep", []string{"sleep", "3"}, nil); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("spawn waited for the child")
	}
}

// An expired snapshot stays expired: a fresher temp file a crashed refresh
// left behind is never read, and inside the backoff nothing is retried.
func TestStatuslineIgnoresAnOrphanTempFile(t *testing.T) {
	home := quotaHome(t)
	writeQuotaCache(t, home, "codex", "remote", `{"five_hour":{"used_percentage":21}}`, time.Now().Add(-40*time.Minute), 40*time.Minute)
	dir := filepath.Dir(cacheFile(home, "codex", "remote"))
	writeFile(t, filepath.Join(dir, ".record-interrupted"), fmt.Sprintf(`{"quota":{"five_hour":{"used_percentage":88}},"source_epoch":%d}`, time.Now().Unix()))
	writeFile(t, filepath.Join(dir, "codex-remote.failed"), "")
	r := quotaApp(home, nil, "CLIPROXY_ROUTE=remote")
	if out, _ := r.render(t, "gpt-6-sol"); out != "⇄\n" || r.spawn.count() != 0 {
		t.Fatalf("segment %q, %d refreshes", out, r.spawn.count())
	}
	if q := cachedQuota(t, home, "codex", "remote"); q != `{"five_hour":{"used_percentage":21}}` {
		t.Fatalf("cache %s", q)
	}
	// The remote backoff is the remote route's alone.
	r = quotaApp(home, nil, "CLIPROXY_ROUTE=local")
	if r.render(t, "gpt-6-sol"); r.spawn.count() != 1 {
		t.Fatal("a remote failure held back the local refresh")
	}
}

// The local sources are the providers' own endpoints, never the gateway.
func TestLocalUsageEndpoints(t *testing.T) {
	for provider, want := range map[string]string{
		"codex": "https://chatgpt.com/backend-api/wham/usage",
		"xai":   "https://cli-chat-proxy.grok.com/v1/billing?format=credits",
	} {
		if got := localUsage[provider].url; got != want {
			t.Errorf("%s: %s", provider, got)
		}
	}
}

// A refresh in flight is not started twice.
func TestStatuslineDoesNotStackRefreshes(t *testing.T) {
	home := quotaHome(t)
	lockPath := strings.TrimSuffix(cacheFile(home, "codex", "remote"), ".json") + ".lock"
	writeFile(t, lockPath, "")
	held, err := os.OpenFile(lockPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	syscall.Flock(int(held.Fd()), syscall.LOCK_EX)
	r := quotaApp(home, nil, "CLIPROXY_ROUTE=remote")
	if r.render(t, "gpt-6-sol"); r.spawn.count() != 0 {
		t.Fatal("a second refresh was started")
	}
	held.Close()
	if r.render(t, "gpt-6-sol"); r.spawn.count() != 1 {
		t.Fatal("no refresh once the lock was free")
	}
}
