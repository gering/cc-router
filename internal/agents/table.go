package agents

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Row is one foreign agent: the nine columns of models.tsv, in file order.
// A NEW agent is a new row, never a new code path.
type Row struct {
	Name     string // agent id; exposed as cc-harness:<name>
	Model    string // ANTHROPIC_MODEL (last-known when the agent discovers)
	Opus     string // ANTHROPIC_DEFAULT_OPUS_MODEL
	Sonnet   string // ANTHROPIC_DEFAULT_SONNET_MODEL
	Haiku    string // ANTHROPIC_DEFAULT_HAIKU_MODEL
	MaxCtx   int    // CLAUDE_CODE_MAX_CONTEXT_TOKENS: the primary's REAL window
	Cred     string // credential file prefix in the proxy dir (<cred>-*.json)
	Login    string // cliproxyapi login flag suggested when credentials are missing
	Provider string // human-readable provider label
}

const (
	agentNamespace = "cc-harness"
	tableColumns   = 9
	tableMaxRows   = 50 // work-system admits at most 50 agent rows
	tableMaxLine   = 1024
	maxContext     = 10000000
)

var (
	agentNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	// The same shape cc-harness-resume accepts from a transcript, so every id
	// the table exports can also be restored.
	modelIDRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
	credRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	loginRe    = regexp.MustCompile(`^[A-Za-z0-9._=-]{1,64}$`)
	providerRe = regexp.MustCompile(`^[ -~]{1,64}$`)
)

// Table is the effective agent table of one invocation.
type Table struct {
	Rows []Row
}

// userTablePath is the per-user override; when present it REPLACES the
// packaged table completely.
func userTablePath(home string) string {
	return filepath.Join(configDir(home), "models.tsv")
}

// packagedTablePath locates share/models.tsv next to the REAL installed
// executable, so a PATH symlink or the dotfiles sibling link resolves to the
// checkout rather than to the directory the link lives in.
func packagedTablePath(executable string) (string, error) {
	real, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(filepath.Dir(real)), "share", "models.tsv"), nil
}

// LoadTable reads the effective table, per invocation. An existing but
// invalid user table fails closed: it never falls back to the packaged one.
func LoadTable(home, executable string) (*Table, error) {
	path := userTablePath(home)
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		if path, err = packagedTablePath(executable); err != nil {
			return nil, fail(exitCapability, "cannot locate the packaged model table: %v", err)
		}
	}
	data, err := readRegularFile(path)
	if err != nil {
		return nil, fail(exitCapability, "model table not readable: %s", oneLine(path))
	}
	rows, err := ParseTable(data)
	if err != nil {
		return nil, fail(exitCapability, "invalid model table %s: %v", oneLine(path), err)
	}
	return &Table{Rows: rows}, nil
}

// boundedInt parses a plain positive decimal with no leading zero and an
// inclusive ceiling — the one rule both the table's context column and the
// config's port value are held to.
func boundedInt(v string, max int) (int, bool) {
	if !isDigits(v) || v[0] == '0' {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n > max {
		return 0, false
	}
	return n, true
}

// maxFileBytes caps every file this helper reads. Tables, configs, markers,
// keys and credentials are all kilobytes at most.
const maxFileBytes = 1 << 20

// readRegularFile is the one reader for the files this helper consumes. It
// refuses directories, devices and FIFOs, which os.ReadFile would otherwise
// read (or block on forever), and caps the size so a huge or growing file
// cannot exhaust memory.
func readRegularFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, maxFileBytes)
	}
	return data, nil
}

// ParseTable validates the whole table before any of it is used. Diagnostics
// name the line and the rule, never the content.
func ParseTable(data []byte) ([]Row, error) {
	var rows []Row
	seen := map[string]bool{}
	credRoute := map[string]string{}
	for i, text := range splitLines(string(data)) {
		line := i + 1
		at := func(format string, args ...any) error {
			return fmt.Errorf("at line %d: %s", line, fmt.Sprintf(format, args...))
		}
		if len(text) > tableMaxLine {
			return nil, at("line longer than %d bytes", tableMaxLine)
		}
		if strings.ContainsRune(text, '\r') {
			return nil, at("carriage return (the table must use LF line endings)")
		}
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		f := strings.Split(text, "\t")
		if len(f) != tableColumns {
			return nil, at("expected %d tab-separated fields, found %d", tableColumns, len(f))
		}
		for i, v := range f {
			if v == "" {
				return nil, at("field %d is empty", i+1)
			}
		}
		r := Row{Name: f[0], Model: f[1], Opus: f[2], Sonnet: f[3], Haiku: f[4], Cred: f[6], Login: f[7], Provider: f[8]}
		if !agentNameRe.MatchString(r.Name) {
			return nil, at("invalid agent name")
		}
		for _, m := range []string{r.Model, r.Opus, r.Sonnet, r.Haiku} {
			if !modelIDRe.MatchString(m) {
				return nil, at("invalid model id")
			}
		}
		ctx, ok := boundedInt(f[5], maxContext)
		if !ok {
			return nil, at("max_ctx must be an integer between 1 and %d", maxContext)
		}
		r.MaxCtx = ctx
		if !credRe.MatchString(r.Cred) {
			return nil, at("invalid credential prefix")
		}
		if !loginRe.MatchString(r.Login) {
			return nil, at("invalid login flag")
		}
		if !providerRe.MatchString(r.Provider) {
			return nil, at("invalid provider label")
		}
		// Uniqueness on everything a lookup keys on: the name (and the
		// override variable derived from it) and the primary model, which
		// resolve-model treats as unambiguous.
		for _, key := range []string{"name:" + r.Name, "var:" + overrideVar(r.Name), "model:" + r.Model} {
			if seen[key] {
				return nil, at("duplicate %s", strings.SplitN(key, ":", 2)[0])
			}
			seen[key] = true
		}
		// The local listing probes a credential once and reuses the verdict,
		// so one prefix must always mean one login and one provider.
		route := r.Login + "\t" + r.Provider
		if prev, ok := credRoute[r.Cred]; ok && prev != route {
			return nil, at("credential prefix %s is used with different login/provider values", r.Cred)
		}
		credRoute[r.Cred] = route
		rows = append(rows, r)
		if len(rows) > tableMaxRows {
			return nil, at("more than %d agent rows", tableMaxRows)
		}
	}
	if len(rows) == 0 {
		return nil, errors.New("contains no agent rows")
	}
	return rows, nil
}

// Find returns the row for a bare or namespaced agent name.
func (t *Table) Find(name string) (Row, bool) {
	name = strings.TrimPrefix(name, agentNamespace+":")
	for _, r := range t.Rows {
		if r.Name == name {
			return r, true
		}
	}
	return Row{}, false
}

// logins joins the table's distinct login flags for the setup hint, so a new
// provider row never leaves the hint advertising only the old ones.
func (t *Table) logins() string {
	var out []string
	seen := map[string]bool{}
	for _, r := range t.Rows {
		if !seen[r.Login] {
			seen[r.Login] = true
			out = append(out, r.Login)
		}
	}
	return strings.Join(out, " / ")
}

// ResolveModel maps a recorded model id onto the agent that serves it. It is
// the table's only machine-readable reader for other tools (cc-harness-resume
// once regex-scraped the bash source instead): pure lookup, no probe, no
// network, no credentials. Matching order: exact primary, exact tier (only
// while every claimant routes identically), then a vendor-prefix family
// claimed by exactly one agent.
func (t *Table) ResolveModel(wanted string) (agent, model string, err error) {
	original := wanted
	// xAI answers name build ids while the proxy exposes the canonical alias.
	if strings.HasPrefix(wanted, "grok-") && strings.HasSuffix(wanted, "-build") && len(wanted) >= len("grok--build") {
		wanted = strings.TrimSuffix(wanted, "-build")
	}
	for _, r := range t.Rows {
		if r.Model == wanted {
			return r.Name, wanted, nil
		}
	}
	hit, hitRoute := "", ""
	for _, r := range t.Rows {
		if wanted != r.Opus && wanted != r.Sonnet && wanted != r.Haiku {
			continue
		}
		route := r.Provider + "|" + r.Cred + "|" + r.Login
		if hit == "" {
			hit, hitRoute = r.Name, route
		} else if route != hitRoute {
			return "", "", fail(exitUnavailable, "model %s is served by several profiles that route differently", oneLine(original))
		}
	}
	if hit != "" {
		return hit, wanted, nil
	}
	// The family fallback is the only branch that echoes its INPUT rather than
	// a table value, and the result is a TSV field cc-harness-resume parses.
	if !modelIDRe.MatchString(wanted) {
		return "", "", fail(exitUnavailable, "no routing profile for model %s", oneLine(original))
	}
	prefix, _, _ := strings.Cut(wanted, "-")
	count := 0
	for _, r := range t.Rows {
		if p, _, _ := strings.Cut(r.Model, "-"); p == prefix {
			count++
			hit = r.Name
		}
	}
	if count == 1 {
		return hit, wanted, nil
	}
	return "", "", fail(exitUnavailable, "no routing profile for model %s", oneLine(original))
}
