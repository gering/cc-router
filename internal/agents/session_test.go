package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordSessionPrunesOnlyStaleRecordsOnANewSession(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".cache", "cc-router", "sessions")
	os.MkdirAll(dir, 0o700)
	// Real time: the record just written carries the real clock as mtime.
	now := time.Now()
	stale, fresh := filepath.Join(dir, "stale.json"), filepath.Join(dir, "fresh.json")
	for _, p := range []string{stale, fresh} {
		writeFile(t, p, "{}")
	}
	os.Chtimes(stale, now.Add(-sessionTTL-time.Hour), now.Add(-sessionTTL-time.Hour))
	os.Chtimes(fresh, now.Add(-time.Hour), now.Add(-time.Hour))
	app := &App{Env: NewEnv(nil), Now: func() time.Time { return now }}
	in := func(id string) *strings.Reader {
		return strings.NewReader(`{"session_id":"` + id + `","cwd":"/w","model":{"id":"claude-opus-5"}}`)
	}
	id := "11111111-1111-4111-8111-111111111111"
	if err := app.recordSession(home, nil, in(id)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("a stale record survived a new session")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("a fresh record was pruned")
	}
	// A render of a known session does not scan the cache.
	writeFile(t, stale, "{}")
	os.Chtimes(stale, now.Add(-sessionTTL-time.Hour), now.Add(-sessionTTL-time.Hour))
	if err := app.recordSession(home, nil, in(id)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Fatal("a re-render pruned the cache")
	}
}

func TestSessionRecordIsNeverReadThroughASymlink(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0o700)
	target := filepath.Join(t.TempDir(), "elsewhere.json")
	writeFile(t, target, `{"session_id":"11111111-1111-4111-8111-111111111111","cwd":"/w","model":"grok-4.6","updated_at":1}`)
	os.Chmod(target, 0o600)
	os.Symlink(target, filepath.Join(dir, "11111111-1111-4111-8111-111111111111.json"))
	var stderr strings.Builder
	if _, ok := (sessionLookup{dir: dir, stderr: &stderr}).byID("11111111-1111-4111-8111-111111111111"); ok {
		t.Fatal("a symlinked record was acted on")
	}
	if !strings.Contains(stderr.String(), "not a plain file") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
