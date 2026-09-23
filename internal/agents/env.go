package agents

import "strings"

// Env is an ordered process environment. It is the single view every
// component reads, so tests inject one instead of mutating the real process
// environment, and exec hands exactly this (plus the routing overrides) on.
type Env struct {
	keys []string
	vals map[string]string
}

// NewEnv parses KEY=VALUE entries as returned by os.Environ. A later duplicate
// wins, matching how a shell resolves repeated assignments.
func NewEnv(entries []string) *Env {
	e := &Env{vals: map[string]string{}}
	for _, kv := range entries {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			continue
		}
		e.Set(k, v)
	}
	return e
}

// Lookup distinguishes an unset variable from one set to the empty string;
// several contracts (CLIPROXY_PROFILE, the config precedence) depend on it.
func (e *Env) Lookup(key string) (string, bool) {
	v, ok := e.vals[key]
	return v, ok
}

// Get returns the value, or "" when unset.
func (e *Env) Get(key string) string { return e.vals[key] }

// Set adds or replaces a variable, keeping the original position of an
// existing one.
func (e *Env) Set(key, value string) {
	if _, ok := e.vals[key]; !ok {
		e.keys = append(e.keys, key)
	}
	e.vals[key] = value
}

// Clone returns an independent copy.
func (e *Env) Clone() *Env {
	c := &Env{keys: append([]string(nil), e.keys...), vals: make(map[string]string, len(e.vals))}
	for k, v := range e.vals {
		c.vals[k] = v
	}
	return c
}

// Entries renders the environment for execve.
func (e *Env) Entries() []string {
	out := make([]string, 0, len(e.keys))
	for _, k := range e.keys {
		out = append(out, k+"="+e.vals[k])
	}
	return out
}
