package agents

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Non-secret configuration keys. Each is resolved as: environment (when set,
// even to "") > ~/.config/cc-router/config.env > documented default. Secrets
// are never configuration: they stay a "these variables must be in the
// environment" contract.
const (
	keyRemoteURL   = "CC_ROUTER_REMOTE_URL"
	keyProfile     = "CLIPROXY_PROFILE"
	keyLocalHost   = "CC_ROUTER_LOCAL_HOST"
	keyLocalPort   = "CC_ROUTER_LOCAL_PORT"
	keyProxyDir    = "CC_ROUTER_PROXY_DIR"
	keyLocalMarker = "CC_ROUTER_LOCAL_MARKER"
	keyPrepareHint = "CC_ROUTER_LOCAL_PREPARE_HINT"
)

// setting describes one key: its default (derived from HOME; "" = none) and
// how a value is validated. Values are validated only when consumed, so a
// local run is not blocked by a remote key it never reads.
type setting struct {
	def      func(home string) string
	validate func(string) error
}

var settings = map[string]setting{
	keyRemoteURL: {validate: validateRemoteURL},
	keyProfile:   {validate: validateProfile},
	keyLocalHost: {
		def: func(string) string { return "127.0.0.1" },
		validate: func(v string) error {
			// Loopback LITERALS only: the local route sends its token in
			// plaintext, and a resolvable name like "localhost" would send it
			// wherever the host's name resolution points.
			switch v {
			case "127.0.0.1", "::1":
				return nil
			}
			return errors.New("must be 127.0.0.1 or ::1")
		},
	},
	keyLocalPort: {
		def: func(string) string { return "8317" },
		validate: func(v string) error {
			if !validPort(v) {
				return errors.New("must be a port number between 1 and 65535")
			}
			return nil
		},
	},
	keyProxyDir: {
		def:      func(home string) string { return filepath.Join(home, ".cli-proxy-api") },
		validate: validateAbsPath,
	},
	// Written by cliproxy-auth; the path is the contract between the two.
	keyLocalMarker: {
		def:      func(home string) string { return filepath.Join(home, ".cache", "cliproxy-auth", "local-ready.json") },
		validate: validateAbsPath,
	},
	keyPrepareHint: {
		def: func(string) string { return "run: cliproxy-auth prepare-local" },
		validate: func(v string) error {
			if v == "" || hasControl(v) || len(v) > 200 {
				return errors.New("must be a single line of at most 200 characters")
			}
			return nil
		},
	},
}

func configDir(home string) string { return filepath.Join(home, ".config", "cc-router") }

// legacyProfilePath is the pre-cc-router profile file the quota helpers still
// read; it stays a fallback for the profile until they migrate too.
func legacyProfilePath(home string) string {
	return filepath.Join(home, ".config", "cliproxy", "client.env")
}

// Config resolves settings for one invocation.
type Config struct {
	env  *Env
	home string
	path string
	file map[string]string
}

// LoadConfig parses config.env strictly as data: KEY=VALUE lines, blank lines
// and # comments; never sourced, no quoting, no substitution. Unknown or
// duplicate keys are errors, and a secret-shaped key is refused outright.
func LoadConfig(env *Env, home string) (*Config, error) {
	c := &Config{env: env, home: home, path: filepath.Join(configDir(home), "config.env"), file: map[string]string{}}
	if _, err := os.Lstat(c.path); errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	data, err := readRegularFile(c.path)
	if err != nil {
		return nil, fail(exitCapability, "config file not readable: %s", c.path)
	}
	for i, text := range splitLines(string(data)) {
		line := i + 1
		bad := func(reason string) error {
			return fail(exitCapability, "invalid config %s at line %d: %s", c.path, line, reason)
		}
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		if hasControl(text) {
			return nil, bad("control character")
		}
		key, value, ok := strings.Cut(text, "=")
		switch {
		case !ok:
			return nil, bad("expected KEY=VALUE")
		case strings.HasPrefix(key, "CLIPROXY_API_KEY") || strings.HasPrefix(key, "CLIPROXY_CF_ACCESS"):
			return nil, bad(key + " is a secret; secrets must stay in the environment")
		}
		if _, known := settings[key]; !known {
			return nil, bad("unknown key " + key)
		}
		if _, dup := c.file[key]; dup {
			return nil, bad("duplicate key " + key)
		}
		c.file[key] = value
	}
	return c, nil
}

// Value returns a validated setting. ok is false when the key has no value
// anywhere and no default.
func (c *Config) Value(key string) (value string, ok bool, err error) {
	source := "environment"
	value, ok = c.env.Lookup(key)
	if !ok {
		source = c.path
		value, ok = c.file[key]
	}
	if !ok {
		if s := settings[key]; s.def != nil {
			return s.def(c.home), true, nil
		}
		return "", false, nil
	}
	if err := settings[key].validate(value); err != nil {
		return "", false, fail(exitCapability, "invalid %s (from %s): %v", key, source, err)
	}
	return value, true, nil
}

// Must returns a setting that has a default, turning a validation error into
// the caller's error.
func (c *Config) Must(key string) (string, error) {
	v, _, err := c.Value(key)
	return v, err
}

// Profile resolves CLIPROXY_PROFILE from the environment, config.env, or the
// legacy client.env (exactly one assignment, never sourced).
func (c *Config) Profile() (string, error) {
	if v, ok, err := c.Value(keyProfile); ok || err != nil {
		return v, err
	}
	path := legacyProfilePath(c.home)
	data, err := readRegularFile(path)
	if err != nil {
		return "", fail(exitCapability, "remote profile file not readable: %s", path)
	}
	candidate, found := "", false
	for _, line := range splitLines(string(data)) {
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, keyProfile+"="):
			if found {
				return "", fail(exitCapability, "remote profile file contains multiple %s assignments", keyProfile)
			}
			candidate, found = strings.TrimPrefix(line, keyProfile+"="), true
		default:
			return "", fail(exitCapability, "remote profile file contains an unsupported line")
		}
	}
	if err := validateProfile(candidate); err != nil {
		return "", fail(exitCapability, "%s %v", keyProfile, err)
	}
	return candidate, nil
}

// splitLines splits on LF; a final line without a newline still counts, a
// trailing newline does not add an empty one.
func splitLines(s string) []string {
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func validPort(v string) bool {
	_, ok := boundedInt(v, 65535)
	return ok
}

var profileRe = regexp.MustCompile(`^[A-Z0-9_]+$`)

func validateProfile(v string) error {
	if !profileRe.MatchString(v) {
		return errors.New("must match [A-Z0-9_]+")
	}
	return nil
}

// validateRemoteURL: TLS is what authenticates the gateway, so HTTPS only; no
// credentials in the URL, and nothing after the path that /v1/models could
// not be appended to.
func validateRemoteURL(v string) error {
	u, err := url.Parse(v)
	switch {
	case err != nil || hasControl(v) || strings.ContainsAny(v, " ?#"):
		return errors.New("must be a plain https:// URL")
	case u.Scheme != "https":
		return errors.New("must use https://")
	case u.Host == "" || u.Hostname() == "":
		return errors.New("must name a host")
	case u.User != nil:
		return errors.New("must not contain credentials")
	case u.Port() != "" && !validPort(u.Port()):
		return errors.New("must use a port between 1 and 65535")
	case strings.HasSuffix(v, "/"):
		return errors.New("must not end with a slash")
	}
	return nil
}

func validateAbsPath(v string) error {
	if !filepath.IsAbs(v) || hasControl(v) {
		return fmt.Errorf("must be an absolute path")
	}
	return nil
}
