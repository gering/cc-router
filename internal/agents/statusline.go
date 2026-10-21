package agents

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The statusline segment: the route marker of a routed session plus its
// provider quota, which Claude Code leaves out of .rate_limits for anything
// it did not get from Anthropic. Rendering reads the cache only, never the
// network; a stale cache starts a detached refresh, which is the only part
// that talks to a usage API.
const (
	// A cache younger than this is fresh: no refresh is started.
	quotaTTL = 300 * time.Second
	// The render gives up after this and prints nothing: a statusline that
	// hangs on a stuck file system is worse than one without a quota.
	renderTimeout = 500 * time.Millisecond
	maxQuotaFile  = 64 << 10
)

// routeMarks are the markers for CLIPROXY_ROUTE; any other value is native.
var routeMarks = map[string]string{"remote": "⇄", "local": "⌂"}

// quotaFiles are one provider's cache on one route: the snapshot, the failure
// marker (its content the retry time) and the refresh lock.
type quotaFiles struct{ dir, cache, failed, lock string }

func newQuotaFiles(env *Env, home, provider, route string) quotaFiles {
	dir := filepath.Join(cacheRoot(env, home), "quota")
	base := filepath.Join(dir, provider+"-"+route)
	return quotaFiles{dir: dir, cache: base + ".json", failed: base + ".failed", lock: base + ".lock"}
}

// readPrivate reads a file of ours: private, plain, bounded.
func readPrivate(path string) ([]byte, os.FileInfo, error) {
	if err := checkPrivate(path, false); err != nil {
		return nil, nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	data, err := readBounded(path, maxQuotaFile)
	if err == nil && len(data) > maxQuotaFile {
		err = errors.New("too large")
	}
	return data, info, err
}

// read returns the cached quota when it is ours, well-formed and its source
// recent enough to show; fresh reports whether it is also within the TTL.
func (f quotaFiles) read(now time.Time) (q quota, fresh bool) {
	if checkPrivate(f.dir, true) != nil {
		return nil, false
	}
	data, info, err := readPrivate(f.cache)
	if err != nil {
		return nil, false
	}
	var snap quotaSnapshot
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(&snap) != nil || !snap.valid() || !usableAge(time.Unix(snap.SourceEpoch, 0), now) {
		return nil, false
	}
	return snap.Quota, now.Sub(info.ModTime()) < quotaTTL
}

// valid holds the cache to what a refresh writes.
func (s quotaSnapshot) valid() bool {
	for key, w := range s.Quota {
		if !slices.Contains(quotaWindows, key) || w.UsedPercentage < 0 || w.UsedPercentage > 100 {
			return false
		}
	}
	return s.Quota != nil
}

// backingOff reports a recent failed refresh. The marker holds its retry
// time; an unreadable one falls back to its mtime plus the short backoff,
// and no retry time is believed beyond the long one.
func (f quotaFiles) backingOff(now time.Time) bool {
	data, info, err := readPrivate(f.failed)
	if info == nil {
		return false
	}
	retry := info.ModTime().Add(quotaFailureBackoff)
	if n, perr := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); err == nil && perr == nil {
		retry = time.Unix(n, 0)
	}
	if limit := info.ModTime().Add(quotaLongBackoff); retry.After(limit) {
		retry = limit
	}
	return now.Before(retry)
}

// mayRefresh reports whether a refresh could do any good now: the directory
// is absent (the refresh creates it) or ours, no failure is backing off and
// none is in flight. A directory another user could touch would fail every
// refresh before it can leave a marker, so it starts none.
func (f quotaFiles) mayRefresh(now time.Time) bool {
	if err := checkPrivate(f.dir, true); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false
	}
	return !f.backingOff(now) && !f.refreshing()
}

// refreshing reports a refresh in flight: its lock is held. Probed without
// creating anything, and released at once.
func (f quotaFiles) refreshing() bool {
	lock, err := os.OpenFile(f.lock, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return false
	}
	defer lock.Close()
	return syscall.Flock(int(lock.Fd()), syscall.LOCK_SH|syscall.LOCK_NB) != nil
}

// statusline dispatches the three forms: the segment, its values as JSON,
// and the refresh a render detaches.
func (a *App) statusline(home string, args []string) error {
	switch {
	case len(args) == 0:
		return a.renderStatusline(home, false)
	case len(args) == 1 && args[0] == "--json":
		return a.renderStatusline(home, true)
	case len(args) == 3 && args[0] == "--refresh":
		return a.refreshQuota(home, args[1], args[2])
	}
	return usageError()
}

// statuslineValues is the --json shape; everything is optional, a native
// session is {}.
type statuslineValues struct {
	Route    string       `json:"route,omitempty"`
	FiveHour *quotaWindow `json:"five_hour,omitempty"`
	SevenDay *quotaWindow `json:"seven_day,omitempty"`
}

// renderStatusline prints the segment, or nothing (--json: {}) when it cannot
// finish within renderTimeout. Exit 0 either way: a statusline has no use
// for a failure status.
func (a *App) renderStatusline(home string, asJSON bool) error {
	done := make(chan statuslineValues, 1)
	go func() { done <- a.statuslineValues(home) }()
	var v statuslineValues
	select {
	case v = <-done:
	case <-time.After(renderTimeout):
	}
	if asJSON {
		out, _ := json.Marshal(v)
		fmt.Fprintf(a.Stdout, "%s\n", out)
		return nil
	}
	if v.Route == "" {
		return nil
	}
	parts := []string{routeMarks[v.Route]}
	for _, w := range []struct {
		label string
		win   *quotaWindow
	}{{"5h", v.FiveHour}, {"7d", v.SevenDay}} {
		if w.win != nil {
			parts = append(parts, fmt.Sprintf("%s %d%%", w.label, int(math.Round(w.win.UsedPercentage))))
		}
	}
	fmt.Fprintln(a.Stdout, strings.Join(parts, " "))
	return nil
}

// statuslineValues resolves the session's route and, for a model a table row
// serves, that row's cached quota. Native sessions and unknown models get no
// quota; any doubt about the input does the same.
func (a *App) statuslineValues(home string) statuslineValues {
	route := a.Env.Get("CLIPROXY_ROUTE")
	if _, ok := routeMarks[route]; !ok {
		return statuslineValues{}
	}
	v := statuslineValues{Route: route}
	var in struct {
		Model struct {
			ID string `json:"id"`
		} `json:"model"`
	}
	data, err := io.ReadAll(io.LimitReader(a.Stdin, maxStatuslineInput+1))
	if err != nil || len(data) > maxStatuslineInput || json.Unmarshal(data, &in) != nil {
		return v
	}
	table, err := LoadTable(home, a.Executable)
	if err != nil {
		return v
	}
	agent, _, err := table.ResolveModel(in.Model.ID)
	if err != nil {
		return v
	}
	row, _ := table.Find(agent)
	files := newQuotaFiles(a.Env, home, row.Cred, route)
	now := a.Now()
	q, fresh := files.read(now)
	if !fresh && files.mayRefresh(now) && a.Spawn != nil {
		// Best effort: a refresh that cannot start leaves the cache as it is.
		_ = a.Spawn(a.Executable, []string{progName, "statusline", "--refresh", route, row.Cred}, a.Env.Entries())
	}
	if w, ok := q["five_hour"]; ok {
		v.FiveHour = &w
	}
	if w, ok := q["seven_day"]; ok {
		v.SevenDay = &w
	}
	return v
}

// spawnDetached starts argv in its own session with no inherited streams, so
// the statusline's command substitution never waits for it, and does not
// wait for it either.
func spawnDetached(path string, argv, env []string) error {
	// nosemgrep: go.lang.security.audit.dangerous-exec-cmd.dangerous-exec-cmd -- path is this binary (a.Executable) and argv its own refresh subcommand with validated route and provider tokens; no shell is involved
	cmd := &exec.Cmd{Path: path, Args: argv, Env: env, SysProcAttr: &syscall.SysProcAttr{Setsid: true}}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// refreshQuota fetches one provider's quota on one route and publishes it.
// One refresh at a time: the lock is an flock the kernel drops with its
// holder, so a crashed refresh never leaves a stale lock behind, and a
// refresh that finds it taken leaves quietly. Diagnostics go to stderr only.
func (a *App) refreshQuota(home, route, provider string) error {
	if _, ok := routeMarks[route]; !ok || !credRe.MatchString(provider) {
		return usageError()
	}
	table, err := LoadTable(home, a.Executable)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(table.Rows, func(r Row) bool { return r.Cred == provider }) {
		return fail(exitUsage, "no table row uses provider %s", provider)
	}
	f := newQuotaFiles(a.Env, home, provider, route)
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		return fail(exitCapability, "quota refresh: cannot create %s", f.dir)
	}
	if err := checkPrivate(f.dir, true); err != nil {
		return fail(exitCapability, "quota refresh: %v", err)
	}
	lock, err := os.OpenFile(f.lock, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fail(exitCapability, "quota refresh: cannot open %s", f.lock)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil // another refresh holds it
	}
	// Re-checked under the lock: a refresh that finished just before this one
	// took it may already have published, or failed into a backoff.
	if _, fresh := f.read(a.Now()); fresh {
		return nil
	}
	if f.backingOff(a.Now()) {
		fmt.Fprintf(a.Stderr, "%s: quota refresh: backing off after a failed refresh — delete %s to retry now\n", progName, f.failed)
		return nil
	}
	fetch := a.fetchRemoteUsage
	if route == "local" {
		fetch = a.fetchLocalUsage
	}
	snap, backoff, err := fetch(home, provider)
	if err != nil {
		retry := strconv.FormatInt(a.Now().Add(backoff).Unix(), 10) + "\n"
		if werr := writeAtomic(f.dir, f.failed, []byte(retry)); werr != nil {
			fmt.Fprintf(a.Stderr, "%s: quota refresh: cannot write %s\n", progName, f.failed)
		}
		return fail(exitUnavailable, "quota refresh (%s, %s): %s", route, provider, oneLine(err.Error()))
	}
	out, _ := json.Marshal(snap)
	if err := writeAtomic(f.dir, f.cache, append(out, '\n')); err != nil {
		return fail(exitCapability, "quota refresh: cannot write %s", f.cache)
	}
	os.Remove(f.failed)
	return nil
}
