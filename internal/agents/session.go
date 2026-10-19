package agents

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// A session sidecar records which agent, route and model a Claude Code
// session last ran on, so a resume can re-enter exactly that routing. The
// statusline writes it (record-session) from the JSON Claude Code hands it;
// launch reads it. It holds no credentials, headers, prompts or transcript
// text — only the names below.
type sessionRecord struct {
	SessionID      string `json:"session_id"`
	Cwd            string `json:"cwd"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	Agent          string `json:"agent,omitempty"` // empty: not launched through exec
	Route          string `json:"route,omitempty"` // remote | local, empty when native
	Model          string `json:"model"`
	UpdatedAt      int64  `json:"updated_at"`
	// RenderedAt orders the writers of one session (unix ms). The statusline
	// stamps each render before it detaches record-session, so a slow writer
	// of an older render cannot replace a newer record. Zero: not stamped.
	RenderedAt int64 `json:"rendered_at,omitempty"`
}

const (
	// sessionTTL bounds the cache: older sidecars are pruned whenever a new
	// session is recorded, and a session idle that long resumes like one that
	// predates the sidecar.
	sessionTTL = 30 * 24 * time.Hour
	// Generous for Claude Code's statusline JSON, small enough that a runaway
	// producer cannot make the statusline buffer without bound.
	maxStatuslineInput = 1 << 20
	maxRecordSize      = 64 << 10
	// The lock is held for one small read and a rename; a writer that cannot
	// take it this long gives up rather than pile up behind a hung one.
	lockWait = time.Second
)

var sessionIDRe = regexp.MustCompile(`^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`)

// sessionDir is ${XDG_CACHE_HOME:-~/.cache}/cc-router/sessions.
func sessionDir(env *Env, home string) string {
	cache := env.Get("XDG_CACHE_HOME")
	if !filepath.IsAbs(cache) {
		cache = filepath.Join(home, ".cache")
	}
	return filepath.Join(cache, "cc-router", "sessions")
}

// validate rejects a record no resume may act on. Every field becomes argv or
// an environment value of the resumed session, so each is checked against the
// shape its consumer accepts — the same shapes the table itself enforces.
func (r *sessionRecord) validate() error {
	switch {
	case !sessionIDRe.MatchString(r.SessionID):
		return errors.New("session id is not a UUID")
	case !filepath.IsAbs(r.Cwd):
		return errors.New("cwd is not an absolute path")
	case !modelIDRe.MatchString(r.Model):
		return errors.New("model is not a valid id")
	case r.Agent != "" && !agentNameRe.MatchString(r.Agent):
		return errors.New("agent is not a valid name")
	case r.Route != "" && r.Route != "remote" && r.Route != "local":
		return errors.New("route is neither remote nor local")
	}
	return nil
}

// recordSession reads Claude Code's statusline JSON on stdin and writes the
// session's sidecar. Local, bounded and atomic, and never the network: the
// statusline calls it on every render.
func (a *App) recordSession(home string, args []string, stdin io.Reader) error {
	var renderedAt int64
	switch {
	case len(args) == 0:
	case len(args) == 2 && args[0] == "--rendered-at":
		// No clock at the caller: an empty stamp is an unstamped render, so
		// one command line works whether or not the stamp could be taken.
		if args[1] == "" {
			break
		}
		n, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil || n <= 0 {
			return usageError()
		}
		renderedAt = n
	default:
		return usageError()
	}
	var in struct {
		SessionID      string `json:"session_id"`
		Cwd            string `json:"cwd"`
		TranscriptPath string `json:"transcript_path"`
		Workspace      struct {
			ProjectDir string `json:"project_dir"`
		} `json:"workspace"`
		Model struct {
			ID string `json:"id"`
		} `json:"model"`
	}
	data, err := io.ReadAll(io.LimitReader(stdin, maxStatuslineInput+1))
	if err != nil || len(data) > maxStatuslineInput {
		return fail(exitUsage, "record-session: unreadable statusline input")
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return fail(exitUsage, "record-session: statusline input is not JSON")
	}
	// -c is matched against the directory Claude was started in, which is
	// the project dir; cwd follows a `cd` inside the session.
	cwd := in.Workspace.ProjectDir
	if cwd == "" {
		cwd = in.Cwd
	}
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	rec := sessionRecord{
		SessionID:  in.SessionID,
		Cwd:        cwd,
		Model:      in.Model.ID,
		Agent:      a.Env.Get(keyAgent),
		Route:      a.Env.Get("CLIPROXY_ROUTE"),
		UpdatedAt:  a.Now().Unix(),
		RenderedAt: renderedAt,
	}
	// Metadata only, never opened: kept for a human reading the cache.
	if filepath.IsAbs(in.TranscriptPath) && !hasControl(in.TranscriptPath) {
		rec.TranscriptPath = in.TranscriptPath
	}
	if err := rec.validate(); err != nil {
		return fail(exitUsage, "record-session: %v", err)
	}
	dir := sessionDir(a.Env, home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail(exitCapability, "record-session: cannot create %s", dir)
	}
	if err := checkPrivate(dir, true); err != nil {
		return fail(exitCapability, "record-session: %v", err)
	}
	// Lower-case on disk, as the lookup spells it: a filesystem that keeps
	// case would otherwise miss a record Claude reported in upper case.
	path := filepath.Join(dir, strings.ToLower(rec.SessionID)+".json")
	created, err := writeRecord(dir, path, rec, a.Now)
	if err != nil {
		return err
	}
	// A new session is rare next to statusline renders, so pruning there
	// keeps the cache bounded without a cost on every render. Outside the
	// lock: a slow scan must not make a newer render's writer give up.
	if created {
		pruneSessions(dir, a.Now())
	}
	return nil
}

// writeRecord replaces the session's record under the directory lock, unless
// a newer render already wrote it. created reports a session recorded for the
// first time.
func writeRecord(dir, path string, rec sessionRecord, now func() time.Time) (created bool, err error) {
	unlock, err := lockDir(dir)
	if err != nil {
		return false, fail(exitCapability, "record-session: cannot lock %s", dir)
	}
	defer unlock()
	_, statErr := os.Lstat(path)
	// The clock is read under the lock: read before it, a render written
	// while this writer waited would look like one from the future.
	stored := storedRenderedAt(dir, filepath.Base(path), now())
	switch {
	case rec.RenderedAt == 0:
		// Unstamped: write, but keep the stored order rather than reset it.
		rec.RenderedAt = stored
	case stored > rec.RenderedAt:
		return false, nil // a newer render already wrote this session
	}
	out, _ := json.Marshal(rec)
	if err := writeAtomic(dir, path, append(out, '\n')); err != nil {
		return false, fail(exitCapability, "record-session: cannot write %s", path)
	}
	return errors.Is(statErr, fs.ErrNotExist), nil
}

// lockDir takes an exclusive flock on the sessions directory itself, so there
// is no lock file to create or prune. It polls instead of blocking, bounded by
// lockWait; closing the descriptor releases the lock.
func lockDir(dir string) (unlock func(), err error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(lockWait)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			f.Close()
			return nil, err
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// storedRenderedAt is the stamp of the record name in dir; zero — replaced by
// any render — when there is none, it predates stamping, or a resume would
// ignore it. A stamp ahead of now orders nothing: the clock stepped back
// since, and honouring it would drop every render until the clock caught up.
func storedRenderedAt(dir, name string, now time.Time) int64 {
	rec, ok := sessionLookup{dir: dir, stderr: io.Discard, now: now}.read(name)
	if !ok || rec.RenderedAt > now.UnixMilli() {
		return 0
	}
	return rec.RenderedAt
}

// writeAtomic replaces path with data through a 0600 temp file and a rename,
// so a reader never sees a torn record.
func writeAtomic(dir, path string, data []byte) error {
	tmp, err := os.CreateTemp(dir, ".record-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr == nil && cerr == nil {
		if err = os.Rename(tmp.Name(), path); err == nil {
			return nil
		}
	}
	os.Remove(tmp.Name())
	return errors.Join(werr, cerr, err)
}

func pruneSessions(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && info.Mode().IsRegular() && now.Sub(info.ModTime()) > sessionTTL {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// checkPrivate refuses anything another user could have planted or read: not
// owned by us, group/other bits set, or not the expected file type (a
// symlink included).
func checkPrivate(path string, dir bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	switch {
	case dir && !info.IsDir(), !dir && !info.Mode().IsRegular():
		return fmt.Errorf("%s is not a plain %s", path, map[bool]string{true: "directory", false: "file"}[dir])
	case info.Mode().Perm()&0o077 != 0:
		return fmt.Errorf("%s is accessible to other users (mode %04o)", path, info.Mode().Perm())
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s is owned by another user", path)
	}
	return nil
}

// sessionLookup reads sidecars for a resume. A record that fails any check is
// reported on stderr and ignored — never acted on, never fatal: the caller
// then falls back as if none existed.
type sessionLookup struct {
	dir    string
	stderr io.Writer
	now    time.Time
}

func (l sessionLookup) warn(format string, args ...any) {
	fmt.Fprintf(l.stderr, "claude: ignoring session record — "+format+"\n", args...)
}

// read loads one sidecar; ok is false when there is none or it was ignored.
func (l sessionLookup) read(name string) (sessionRecord, bool) {
	path := filepath.Join(l.dir, name)
	if err := checkPrivate(path, false); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			l.warn("%s", oneLine(err.Error()))
		}
		return sessionRecord{}, false
	}
	data, err := readBounded(path, maxRecordSize)
	var rec sessionRecord
	switch {
	case err != nil:
	case len(data) > maxRecordSize:
		err = errors.New("too large")
	default:
		// Strict on what is consumed: this file is ours, so an unknown field
		// means it is not.
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err = dec.Decode(&rec); err == nil {
			err = rec.validate()
		}
	}
	if err == nil && !strings.EqualFold(rec.SessionID+".json", name) {
		err = errors.New("its name does not match its session id")
	}
	if err != nil {
		l.warn("%s: %s", path, oneLine(err.Error()))
		return sessionRecord{}, false
	}
	// Expired, even if no new session has pruned it yet: idle that long, the
	// session resumes like one that predates the record.
	if l.now.Sub(time.Unix(rec.UpdatedAt, 0)) > sessionTTL {
		return sessionRecord{}, false
	}
	return rec, true
}

// readBounded reads at most limit+1 bytes, so an oversized file is detected
// without being read whole.
func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit+1))
}

// usable reports whether the cache directory may be read at all.
func (l sessionLookup) usable() bool {
	err := checkPrivate(l.dir, true)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		l.warn("%s", oneLine(err.Error()))
	}
	return err == nil
}

// byID is the sidecar of one session.
func (l sessionLookup) byID(id string) (sessionRecord, bool) {
	if !l.usable() {
		return sessionRecord{}, false
	}
	return l.read(strings.ToLower(id) + ".json")
}

// transcriptGone: the record names a transcript that no longer exists, so a
// resume pinned to it could only fail. Checked by existence alone; the
// transcript is never opened.
func (r sessionRecord) transcriptGone() bool {
	if r.TranscriptPath == "" {
		return false
	}
	_, err := os.Stat(r.TranscriptPath)
	return errors.Is(err, fs.ErrNotExist)
}

// latestIn is the uniquely newest sidecar recorded for exactly cwd. Worktrees
// of one repository are different directories, so they never cross-select;
// a parent directory never matches either. A tie is no answer.
func (l sessionLookup) latestIn(cwd string) (sessionRecord, bool) {
	if !l.usable() {
		return sessionRecord{}, false
	}
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return sessionRecord{}, false
	}
	var best sessionRecord
	found, tie := false, false
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		rec, ok := l.read(e.Name())
		if !ok || rec.Cwd != cwd || rec.transcriptGone() {
			continue
		}
		switch {
		case !found || rec.UpdatedAt > best.UpdatedAt:
			best, found, tie = rec, true, false
		case rec.UpdatedAt == best.UpdatedAt:
			tie = true
		}
	}
	if tie {
		fmt.Fprintln(l.stderr, "claude: several sessions here were last active at the same time; not choosing one by its record")
		return sessionRecord{}, false
	}
	return best, found
}
