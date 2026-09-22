package agents

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// localRoute is the machine-local gateway. It is only a reachability check
// over plaintext loopback: whoever holds the port receives the token, which is
// why the host is restricted to loopback and the route is explicit and gated.
type localRoute struct {
	host, port string
	proxyDir   string
	marker     string
	hint       string
}

func loadLocal(cfg *Config) (*localRoute, error) {
	l := &localRoute{}
	for _, s := range []struct {
		key string
		dst *string
	}{
		{keyLocalHost, &l.host}, {keyLocalPort, &l.port}, {keyProxyDir, &l.proxyDir},
		{keyLocalMarker, &l.marker}, {keyPrepareHint, &l.hint},
	} {
		v, err := cfg.Must(s.key)
		if err != nil {
			return nil, err
		}
		*s.dst = v
	}
	return l, nil
}

func (l *localRoute) addr() string      { return net.JoinHostPort(l.host, l.port) }
func (l *localRoute) baseURL() string   { return "http://" + l.addr() }
func (l *localRoute) tokenFile() string { return filepath.Join(l.proxyDir, "client.key") }

var markerStampRe = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$`)

// blocked reports why the local fallback is NOT sanctioned ("" = open). The
// host that keeps the provider refresh tokens must be stopped while a local
// proxy runs on a copy of them — otherwise the two race the refresh and can
// invalidate the tokens for every machine. cliproxy-auth's marker proves that
// window is open, so this gate runs BEFORE the token, gateway and OAuth
// probes: a stale copy looks perfectly healthy to all three.
func (l *localRoute) blocked(now time.Time) string {
	data, err := readRegularFile(l.marker)
	if err != nil {
		return "the local route is not prepared — " + l.hint
	}
	var m struct {
		Version   any `json:"version"`
		ExpiresAt any `json:"expires_at"`
		Files     any `json:"files"`
	}
	err = decodeSingle(data, &m)
	stamp, isString := m.ExpiresAt.(string)
	_, filesOK := m.Files.([]any)
	// Numeric, like the jq `.version == 1` it replaces: 1.0 is version 1.
	version, isNumber := m.Version.(json.Number)
	v, numErr := version.Float64()
	if err != nil || !isNumber || numErr != nil || v != 1 || !isString || !markerStampRe.MatchString(stamp) || !filesOK {
		return "the local fallback marker is unusable — " + l.hint
	}
	expires, err := time.Parse("2006-01-02T15:04:05Z", stamp)
	if err != nil || !expires.After(now) {
		return "the local fallback expired — " + l.hint
	}
	return ""
}

// decodeSingle decodes exactly one JSON value; trailing data is an error, so a
// document with a second value appended is refused rather than half-read.
// Numbers stay json.Number: this helper reads credential and catalog metadata
// where float64 would round an id or an epoch.
func decodeSingle(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}

// token reads the gateway key. Content, not readability, is checked: an
// interrupted login leaves a 0-byte key, and whitespace is stripped from the
// value itself so the check and the export see the same string.
func (l *localRoute) token() string {
	data, err := readRegularFile(l.tokenFile())
	if err != nil {
		return ""
	}
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(" \t\n\v\f\r", r) {
			return -1
		}
		return r
	}, string(data))
}

// live is a TCP reachability check only: over plain loopback every other
// signal is forgeable by another local process. Dialed directly, never via a
// proxy.
func (l *localRoute) live(timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", l.addr(), timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (l *localRoute) downNote() string {
	return fmt.Sprintf("nothing listening on %s — start the proxy: brew services start cliproxyapi", l.addr())
}

// credentials lists <prefix>-*.json newest mtime first (name order on ties).
// Newest-ONLY is wrong: a failed login writes a fresh broken file that would
// hide the older working credential.
func (l *localRoute) credentials(prefix string) []string {
	entries, err := os.ReadDir(l.proxyDir)
	if err != nil {
		return nil
	}
	type cred struct {
		path  string
		mtime time.Time
	}
	var creds []cred
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix+"-") || !strings.HasSuffix(name, ".json") || len(name) < len(prefix)+len("-.json") {
			continue
		}
		path := filepath.Join(l.proxyDir, name)
		info, err := os.Stat(path)
		if err != nil {
			info, err = os.Lstat(path)
		}
		if err == nil {
			creds = append(creds, cred{path, info.ModTime()})
		}
	}
	sort.SliceStable(creds, func(i, j int) bool {
		if !creds[i].mtime.Equal(creds[j].mtime) {
			return creds[i].mtime.After(creds[j].mtime)
		}
		return creds[i].path < creds[j].path
	})
	out := make([]string, len(creds))
	for i, c := range creds {
		out[i] = c.path
	}
	return out
}

type verdict string

const (
	verdictOK         verdict = "ok"
	verdictDisabled   verdict = "disabled"
	verdictExpired    verdict = "expired"
	verdictEmpty      verdict = "empty"
	verdictUnreadable verdict = "unreadable"
)

// credVerdict judges one credential file. Access-token expiry is TTL, not
// revocation: CLIProxyAPI refreshes on first use when a refresh_token exists,
// so only disabled, or expired WITHOUT a refresh token, blocks. A file that
// cannot be read or parsed fails closed; an unrecognized expiry shape means
// "no expiry known", not a broken file.
func credVerdict(path string, now time.Time) verdict {
	data, err := readRegularFile(path)
	if err != nil {
		return verdictUnreadable
	}
	var v any
	if err := decodeSingle(data, &v); err != nil {
		return verdictUnreadable
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return verdictEmpty
	}
	if token, ok := obj["access_token"].(string); !ok || token == "" {
		return verdictEmpty
	}
	if disabled, ok := obj["disabled"].(bool); ok && disabled {
		return verdictDisabled
	}
	if refresh, ok := obj["refresh_token"].(string); ok && refresh != "" {
		return verdictOK
	}
	if expiry, ok := credExpiry(obj["expired"]); ok && expiry < float64(now.Unix()) {
		return verdictExpired
	}
	return verdictOK
}

var (
	isoPrefixRe = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T`)
	fractionRe  = regexp.MustCompile(`\.[0-9]+`)
	offsetRe    = regexp.MustCompile(`^(.*?)([+-])([0-9]{2}):?([0-9]{2})$`)
)

// credExpiry reads `expired` as epoch seconds: a number directly, an ISO
// timestamp with Z or a ±HH:MM / ±HHMM offset (what codex writes) parsed,
// anything else as unknown.
func credExpiry(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		if !isoPrefixRe.MatchString(x) {
			return 0, false
		}
		s := x
		if loc := fractionRe.FindStringIndex(s); loc != nil {
			s = s[:loc[0]] + s[loc[1]:]
		}
		offset := 0
		if strings.HasSuffix(s, "Z") {
			s = strings.TrimSuffix(s, "Z")
		} else if m := offsetRe.FindStringSubmatch(s); m != nil {
			h, m2 := atoi2(m[3]), atoi2(m[4])
			offset = h*3600 + m2*60
			if m[2] == "-" {
				offset = -offset
			}
			s = m[1]
		}
		t, err := time.Parse("2006-01-02T15:04:05", s)
		if err != nil {
			return 0, false
		}
		return float64(t.Unix() - int64(offset)), true
	}
	return 0, false
}

func atoi2(s string) int { return int(s[0]-'0')*10 + int(s[1]-'0') }

// checkCreds returns whether the provider has a usable credential and, when
// not, the note naming the fix. The first usable credential wins; a broken
// newer one is reported only if NONE works. Notes carry the basename only.
func (l *localRoute) checkCreds(r Row, now time.Time) (bool, string) {
	relogin := "re-login: cliproxyapi " + r.Login
	first := ""
	for _, path := range l.credentials(r.Cred) {
		v := credVerdict(path, now)
		if v == verdictOK {
			return true, ""
		}
		if first != "" {
			continue
		}
		base := filepath.Base(path)
		switch v {
		case verdictDisabled:
			first = fmt.Sprintf("%s credentials disabled — %s", r.Provider, relogin)
		case verdictExpired:
			first = fmt.Sprintf("%s credentials expired (no refresh token) — %s", r.Provider, relogin)
		case verdictEmpty:
			first = fmt.Sprintf("%s credentials incomplete (no access_token in %s) — %s", r.Provider, base, relogin)
		default:
			first = fmt.Sprintf("%s credentials unreadable (%s) — %s", r.Provider, base, relogin)
		}
	}
	if first == "" {
		first = "run: cliproxyapi " + r.Login
	}
	return false, first
}
