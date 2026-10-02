package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile creates parent directories and writes content.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestConfigPrecedence(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(configDir(home), "config.env"),
		"# comment\n\nCC_ROUTER_REMOTE_URL=https://file.example\nCC_ROUTER_LOCAL_PORT=9000\n")

	cfg, err := LoadConfig(NewEnv([]string{"CC_ROUTER_LOCAL_PORT=9100"}), home)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		keyRemoteURL:   "https://file.example",                          // file over default
		keyLocalPort:   "9100",                                          // environment over file
		keyLocalHost:   "127.0.0.1",                                     // default
		keyLocalMarker: home + "/.cache/cliproxy-auth/local-ready.json", // default from HOME
		keyPrepareHint: "run: cliproxy-auth prepare-local",              // generic default
		keyProxyDir:    filepath.Join(home, ".cli-proxy-api"),
	} {
		if got, err := cfg.Must(key); err != nil || got != want {
			t.Errorf("%s = %q, %v; want %q", key, got, err, want)
		}
	}

	// An explicitly empty environment value wins — and then fails validation,
	// rather than silently falling back to the file.
	cfg, _ = LoadConfig(NewEnv([]string{"CC_ROUTER_REMOTE_URL="}), home)
	if _, _, err := cfg.Value(keyRemoteURL); err == nil || !strings.Contains(err.Error(), "from environment") {
		t.Fatalf("empty env value: %v", err)
	}
}

// A default derived from HOME meets the same validator as an explicit value:
// a relative HOME must not yield a relative credential dir.
func TestConfigValidatesDefaults(t *testing.T) {
	cfg, err := LoadConfig(NewEnv(nil), "relative-home")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Must(keyProxyDir); err == nil || !strings.Contains(err.Error(), "the default") {
		t.Fatalf("relative default accepted: %v", err)
	}
}

func TestConfigRejects(t *testing.T) {
	for name, content := range map[string]string{
		"unknown key": "CC_ROUTER_NOPE=1\n",
		"duplicate":   "CC_ROUTER_LOCAL_PORT=1\nCC_ROUTER_LOCAL_PORT=2\n",
		"no equals":   "CC_ROUTER_LOCAL_PORT\n",
		"secret":      "CLIPROXY_API_KEY_TEST=value-secret\n",
		"crlf":        "CC_ROUTER_LOCAL_PORT=1\r\n",
	} {
		home := t.TempDir()
		writeFile(t, filepath.Join(configDir(home), "config.env"), content)
		_, err := LoadConfig(NewEnv(nil), home)
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), "value-secret") {
			t.Errorf("%s: diagnostic leaks a value: %v", name, err)
		}
	}
}

func TestSettingValidation(t *testing.T) {
	cases := map[string]map[string]bool{
		keyRemoteURL: {
			"https://gw.example": true, "https://gw.example:8443/prefix": true,
			"http://gw.example": false, "https://user:pw@gw.example": false, "https://gw.example/": false,
			"https://gw.example?x=1": false, "https://gw.example#f": false, "https://": false, "gw.example": false,
			"https://gw.example:70000": false, "https://gw.example:0": false,
		},
		keyLocalHost: {"127.0.0.1": true, "::1": true, "localhost": false, "0.0.0.0": false, "gw.example": false},
		keyLocalPort: {"8317": true, "65535": true, "0": false, "65536": false, "08317": false, "x": false},
		keyProxyDir:  {"/abs": true, "relative": false},
		keyProfile:   {"MACHINE_1": true, "": false, "bad-name": false, "lower": false},
	}
	for key, values := range cases {
		for v, ok := range values {
			if err := settings[key].validate(v); (err == nil) != ok {
				t.Errorf("%s=%q: err %v", key, v, err)
			}
		}
	}
}

func TestLegacyProfile(t *testing.T) {
	home := t.TempDir()
	path := legacyProfilePath(home)
	cfg := func(env ...string) *Config {
		c, err := LoadConfig(NewEnv(env), home)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	if _, err := cfg().Profile(); err == nil || !strings.Contains(err.Error(), "not readable") {
		t.Fatalf("missing legacy file: %v", err)
	}
	writeFile(t, path, "# deployment\nCLIPROXY_PROFILE=LAPTOP")
	if p, err := cfg().Profile(); err != nil || p != "LAPTOP" {
		t.Fatalf("legacy profile: %q %v", p, err)
	}
	// The environment wins without the file being trusted at all.
	writeFile(t, path, "garbage\n")
	if p, err := cfg("CLIPROXY_PROFILE=OVERRIDE").Profile(); err != nil || p != "OVERRIDE" {
		t.Fatalf("env override: %q %v", p, err)
	}
	for name, content := range map[string]string{
		"unsupported": "garbage\n",
		"multiple":    "CLIPROXY_PROFILE=A\nCLIPROXY_PROFILE=B\n",
		"invalid":     "CLIPROXY_PROFILE=bad-name\n",
		"crlf":        "CLIPROXY_PROFILE=A\r\n",
	} {
		writeFile(t, path, content)
		if _, err := cfg().Profile(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
