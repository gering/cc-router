package agents

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
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

// recordRender feeds one render, stamped unless renderedAt is zero.
func recordRender(t *testing.T, app *App, home, model string, renderedAt int64) {
	t.Helper()
	in := strings.NewReader(`{"session_id":"11111111-1111-4111-8111-111111111111","cwd":"/w","model":{"id":"` + model + `"}}`)
	args := []string{"--rendered-at", strconv.FormatInt(renderedAt, 10)}
	if renderedAt == 0 {
		args = nil
	}
	if err := app.recordSession(home, args, in); err != nil {
		t.Error(err)
	}
}

// storedRecord is the session's record as a resume reads it.
func storedRecord(t *testing.T, home string) sessionRecord {
	t.Helper()
	dir := filepath.Join(home, ".cache", "cc-router", "sessions")
	rec, ok := sessionLookup{dir: dir, stderr: io.Discard, now: time.Now()}.byID("11111111-1111-4111-8111-111111111111")
	if !ok {
		t.Fatal("no record")
	}
	return rec
}

func TestRecordSessionKeepsTheNewerRender(t *testing.T) {
	home := t.TempDir()
	app := &App{Env: NewEnv(nil), Now: time.Now}
	// A slow writer of the render before /model arrives last.
	recordRender(t, app, home, "gpt-5.6-terra", 2000)
	recordRender(t, app, home, "gpt-6.1-sol", 1000)
	if rec := storedRecord(t, home); rec.Model != "gpt-5.6-terra" || rec.RenderedAt != 2000 {
		t.Fatalf("record = %+v", rec)
	}
	// A later render replaces it. Render time is the wall clock, so a resumed
	// session — whose own counters may restart — still renders later.
	recordRender(t, app, home, "gpt-6.1-sol", 3000)
	if rec := storedRecord(t, home); rec.Model != "gpt-6.1-sol" {
		t.Fatalf("record = %+v", rec)
	}
}

func TestRecordSessionConcurrentWritersKeepTheNewestRender(t *testing.T) {
	home := t.TempDir()
	app := &App{Env: NewEnv(nil), Now: time.Now}
	const n = 32
	var wg sync.WaitGroup
	// Started newest first, so arrival order runs against render order.
	for i := n; i >= 1; i-- {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			model := "grok-4.6"
			if i == n {
				model = "gpt-5.6-terra"
			}
			recordRender(t, app, home, model, int64(i))
		}(i)
	}
	wg.Wait()
	if rec := storedRecord(t, home); rec.Model != "gpt-5.6-terra" || rec.RenderedAt != n {
		t.Fatalf("record = %+v", rec)
	}
}

func TestRecordSessionReplacesARecordThatPredatesStamping(t *testing.T) {
	home := t.TempDir()
	app := &App{Env: NewEnv(nil), Now: time.Now}
	recordRender(t, app, home, "gpt-6.1-sol", 0)
	if rec := storedRecord(t, home); rec.RenderedAt != 0 {
		t.Fatalf("an unstamped render was stamped: %+v", rec)
	}
	recordRender(t, app, home, "gpt-5.6-terra", 1000)
	// An unstamped writer (a statusline that predates stamping) still writes,
	// but keeps the stored order: an older stamped render still loses.
	recordRender(t, app, home, "grok-4.6", 0)
	recordRender(t, app, home, "gpt-6.1-sol", 500)
	if rec := storedRecord(t, home); rec.Model != "grok-4.6" || rec.RenderedAt != 1000 {
		t.Fatalf("record = %+v", rec)
	}
}

func TestRecordSessionIgnoresAStampFromTheFuture(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	app := &App{Env: NewEnv(nil), Now: func() time.Time { return now }}
	// Stored before the clock stepped back five minutes.
	recordRender(t, app, home, "gpt-6.1-sol", now.Add(5*time.Minute).UnixMilli())
	recordRender(t, app, home, "gpt-5.6-terra", now.UnixMilli())
	if rec := storedRecord(t, home); rec.Model != "gpt-5.6-terra" {
		t.Fatalf("a stamp from the future froze the record: %+v", rec)
	}
}

func TestRecordSessionReplacesWhatAResumeWouldNotRead(t *testing.T) {
	for name, plant := range map[string]func(path string) error{
		"symlink": func(path string) error {
			target := filepath.Join(filepath.Dir(path), "elsewhere")
			writeFile(t, target, `{"rendered_at":9000}`)
			os.Chmod(target, 0o600)
			return os.Symlink(target, path)
		},
		"fifo": func(path string) error { return syscall.Mkfifo(path, 0o600) },
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			app := &App{Env: NewEnv(nil), Now: time.Now}
			dir := filepath.Join(home, ".cache", "cc-router", "sessions")
			os.MkdirAll(dir, 0o700)
			if err := plant(filepath.Join(dir, "11111111-1111-4111-8111-111111111111.json")); err != nil {
				t.Fatal(err)
			}
			recordRender(t, app, home, "gpt-5.6-terra", 1000)
			if rec := storedRecord(t, home); rec.Model != "gpt-5.6-terra" {
				t.Fatalf("record = %+v", rec)
			}
		})
	}
}

func TestRecordSessionWaitsForTheLock(t *testing.T) {
	home := t.TempDir()
	app := &App{Env: NewEnv(nil), Now: time.Now}
	recordRender(t, app, home, "gpt-6.1-sol", 1000)
	unlock, err := lockDir(filepath.Join(home, ".cache", "cc-router", "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { recordRender(t, app, home, "gpt-5.6-terra", 2000); close(done) }()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("wrote while the lock was held")
	default:
	}
	unlock()
	<-done
	if rec := storedRecord(t, home); rec.Model != "gpt-5.6-terra" {
		t.Fatalf("the waiting render was dropped: %+v", rec)
	}
}

func TestRecordSessionReadsTheClockUnderTheLock(t *testing.T) {
	home := t.TempDir()
	app := &App{Env: NewEnv(nil), Now: time.Now}
	older := time.Now().UnixMilli()
	recordRender(t, app, home, "gpt-6.1-sol", older-10)
	unlock, err := lockDir(filepath.Join(home, ".cache", "cc-router", "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	// An older render's writer starts and waits on the lock …
	done := make(chan struct{})
	go func() { recordRender(t, app, home, "gpt-6.1-sol", older); close(done) }()
	time.Sleep(50 * time.Millisecond)
	// … while a newer render, stamped after the waiter started, is written.
	writeRecordDirect(t, home, "gpt-5.6-terra", time.Now().UnixMilli())
	unlock()
	<-done
	if rec := storedRecord(t, home); rec.Model != "gpt-5.6-terra" {
		t.Fatalf("the newer render was replaced: %+v", rec)
	}
}

// writeRecordDirect writes a stamped record as a writer already holding the
// lock would, bypassing lockDir.
func writeRecordDirect(t *testing.T, home, model string, renderedAt int64) {
	t.Helper()
	dir := filepath.Join(home, ".cache", "cc-router", "sessions")
	rec := sessionRecord{SessionID: "11111111-1111-4111-8111-111111111111", Cwd: "/w", Model: model,
		UpdatedAt: time.Now().Unix(), RenderedAt: renderedAt}
	out, _ := json.Marshal(rec)
	if err := writeAtomic(dir, filepath.Join(dir, rec.SessionID+".json"), out); err != nil {
		t.Fatal(err)
	}
}

func TestRecordSessionTakesAnEmptyStampAsUnstamped(t *testing.T) {
	home := t.TempDir()
	app := &App{Env: NewEnv(nil), Now: time.Now}
	in := strings.NewReader(`{"session_id":"11111111-1111-4111-8111-111111111111","cwd":"/w","model":{"id":"grok-4.6"}}`)
	if err := app.recordSession(home, []string{"--rendered-at", ""}, in); err != nil {
		t.Fatal(err)
	}
	if rec := storedRecord(t, home); rec.Model != "grok-4.6" || rec.RenderedAt != 0 {
		t.Fatalf("record = %+v", rec)
	}
}

func TestRecordSessionRejectsAMalformedStamp(t *testing.T) {
	app := &App{Env: NewEnv(nil), Now: time.Now}
	for _, args := range [][]string{{"--rendered-at"}, {"--rendered-at", "0"}, {"--rendered-at", "-5"}, {"--rendered-at", "1.5"}, {"1000"}} {
		in := strings.NewReader(`{"session_id":"11111111-1111-4111-8111-111111111111","cwd":"/w","model":{"id":"grok-4.6"}}`)
		if err := app.recordSession(t.TempDir(), args, in); err == nil {
			t.Errorf("%q accepted", args)
		}
	}
}
