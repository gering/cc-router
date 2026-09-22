package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)

// Upstream never exercised the credential verdicts; these pin them.
func TestCredVerdict(t *testing.T) {
	cases := map[string]struct {
		content string
		want    verdict
	}{
		"refreshable":           {`{"access_token":"a","refresh_token":"r","expired":"2020-01-01T00:00:00Z"}`, verdictOK},
		"disabled":              {`{"access_token":"a","refresh_token":"r","disabled":true}`, verdictDisabled},
		"disabled string":       {`{"access_token":"a","disabled":"true"}`, verdictOK},
		"expired Z":             {`{"access_token":"a","expired":"2026-09-22T11:59:59Z"}`, verdictExpired},
		"valid Z":               {`{"access_token":"a","expired":"2026-09-22T12:00:01Z"}`, verdictOK},
		"expired fraction":      {`{"access_token":"a","expired":"2026-09-22T11:59:59.123Z"}`, verdictExpired},
		"offset colon":          {`{"access_token":"a","expired":"2026-09-22T13:59:59+02:00"}`, verdictExpired},
		"offset compact future": {`{"access_token":"a","expired":"2026-09-22T14:00:01+0200"}`, verdictOK},
		"negative offset":       {`{"access_token":"a","expired":"2026-09-22T06:59:59-05:00"}`, verdictExpired},
		"no zone":               {`{"access_token":"a","expired":"2026-09-22T11:00:00"}`, verdictExpired},
		"epoch past":            {`{"access_token":"a","expired":1000}`, verdictExpired},
		"epoch future":          {`{"access_token":"a","expired":99999999999}`, verdictOK},
		"unknown shape":         {`{"access_token":"a","expired":"soon"}`, verdictOK},
		"unparsable date":       {`{"access_token":"a","expired":"2026-13-45T99:00:00Z"}`, verdictOK},
		"expired false":         {`{"access_token":"a","expired":false}`, verdictOK},
		"no token":              {`{"refresh_token":"r"}`, verdictEmpty},
		"empty token":           {`{"access_token":""}`, verdictEmpty},
		"token not string":      {`{"access_token":1}`, verdictEmpty},
		"null":                  {`null`, verdictEmpty},
		"array":                 {`[]`, verdictEmpty},
		"truncated":             {`{"access_token":"a"`, verdictUnreadable},
		"trailing data":         {`{"access_token":"a"} {}`, verdictUnreadable},
		"empty file":            {``, verdictUnreadable},
	}
	dir := t.TempDir()
	for name, c := range cases {
		path := filepath.Join(dir, "c.json")
		writeFile(t, path, c.content)
		if got := credVerdict(path, testNow); got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
	if got := credVerdict(dir, testNow); got != verdictUnreadable {
		t.Errorf("directory: %s", got)
	}
}

func TestCheckCredsNewestUsableWins(t *testing.T) {
	dir := t.TempDir()
	l := &localRoute{proxyDir: dir}
	row := Row{Cred: "codex", Login: "-codex-login", Provider: "Codex"}
	touch := func(name, content string, age time.Duration) {
		path := filepath.Join(dir, name)
		writeFile(t, path, content)
		if err := os.Chtimes(path, testNow.Add(-age), testNow.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	if ok, note := l.checkCreds(row, testNow); ok || note != "run: cliproxyapi -codex-login" {
		t.Fatalf("no credentials: %v %q", ok, note)
	}
	// A failed login leaves a fresh broken file; the older working one wins.
	touch("codex-old.json", `{"access_token":"a","refresh_token":"r"}`, time.Hour)
	touch("codex-new.json", `{"access_token":"a"`, time.Minute)
	touch("codexother-x.json", `{}`, 0) // a different prefix is not ours
	if ok, note := l.checkCreds(row, testNow); !ok || note != "" {
		t.Fatalf("older usable credential: %v %q", ok, note)
	}
	// With none usable, the NEWEST file's problem is reported, by basename.
	touch("codex-old.json", `{"access_token":"a","disabled":true}`, time.Hour)
	ok, note := l.checkCreds(row, testNow)
	if ok || note != "Codex credentials unreadable (codex-new.json) — re-login: cliproxyapi -codex-login" {
		t.Fatalf("all broken: %v %q", ok, note)
	}
}

func TestLocalMarker(t *testing.T) {
	dir := t.TempDir()
	l := &localRoute{marker: filepath.Join(dir, "local-ready.json"), hint: "run: prepare"}
	check := func(content, want string) {
		t.Helper()
		if content != "" {
			writeFile(t, l.marker, content)
		}
		got := l.blocked(testNow)
		// want "" means the route is OPEN, and HasPrefix(got, "") is always
		// true — so an open verdict is asserted as exactly empty.
		if (want == "" && got != "") || (want != "" && (!strings.HasPrefix(got, want) || !strings.HasSuffix(got, "run: prepare"))) {
			t.Errorf("marker %q: %q, want %q…", content, got, want)
		}
	}
	check("", "the local route is not prepared")
	check(`{"version":1,"expires_at":"2026-09-22T13:00:00Z","files":[]}`, "")
	check(`{"version":1.0,"expires_at":"2026-09-22T13:00:00Z","files":[],"extra":1}`, "")
	check(`{"version":1,"expires_at":"2026-09-22T11:00:00Z","files":[]}`, "the local fallback expired")
	check(`{"version":1,"expires_at":"2026-13-45T11:00:00Z","files":[]}`, "the local fallback expired")
	check(`{"version":2,"expires_at":"2026-09-22T13:00:00Z","files":[]}`, "the local fallback marker is unusable")
	check(`{"version":1,"expires_at":"2026-09-22T13:00:00+00:00","files":[]}`, "the local fallback marker is unusable")
	check(`{"version":1,"expires_at":"2026-09-22T13:00:00Z","files":{}}`, "the local fallback marker is unusable")
	check(`not json`, "the local fallback marker is unusable")
	check(`null`, "the local fallback marker is unusable")
	os.Remove(l.marker)
	if err := os.Mkdir(l.marker, 0o700); err != nil {
		t.Fatal(err)
	}
	check("", "the local route is not prepared")
}

func TestLocalToken(t *testing.T) {
	dir := t.TempDir()
	l := &localRoute{proxyDir: dir}
	if l.token() != "" {
		t.Fatal("missing token file")
	}
	writeFile(t, l.tokenFile(), " \t\r\n")
	if l.token() != "" {
		t.Fatal("whitespace-only token")
	}
	writeFile(t, l.tokenFile(), "tok\r\nen\n")
	if got := l.token(); got != "token" {
		t.Fatalf("token = %q", got)
	}
}
