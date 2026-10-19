package agents

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestForeignDefault(t *testing.T) {
	cases := []struct {
		name                string
		user, shared, local string // "" = no such file
		layout              string // "" (no git), "subdir" or "worktree" of a repository
		configDir           bool   // the user file sits in CLAUDE_CONFIG_DIR
		process             string // one KEY=VALUE of the process environment
		fifo                bool   // the shared settings are a FIFO
		wantFile, wantModel string
		wantTable           bool
	}{
		{name: "no settings"},
		{name: "no model key", user: `{"theme":"dark"}`},
		{name: "empty model", user: `{"model":""}`},
		{name: "anthropic id", user: `{"model":"claude-sonnet-5-5"}`},
		{name: "anthropic id of a row", user: `{"model":"claude-opus-5-5"}`},
		{name: "foreign id", user: `{"model":"gpt-5.6-terra","theme":"dark"}`, wantFile: "user", wantModel: "gpt-5.6-terra", wantTable: true},
		{name: "unknown id", user: `{"model":"mystery-1"}`, wantTable: true},
		{name: "alias", user: `{"model":"opus"}`, wantTable: true},
		{name: "invalid json", user: `{"model":`},
		{name: "model not a string", user: `{"model":7}`},
		{name: "the key is exact", user: `{"Model":"gpt-6-sol"}`},
		{name: "local outranks user", user: `{"model":"gpt-6-sol"}`, local: `{"model":"claude-sonnet-5-5"}`},
		{name: "shared outranks user", user: `{"model":"claude-sonnet-5-5"}`, shared: `{"model":"grok-4.6"}`, wantFile: "shared", wantModel: "grok-4.6", wantTable: true},
		{name: "a file without model defers", user: `{"model":"gpt-6-sol"}`, local: `{}`, wantFile: "user", wantModel: "gpt-6-sol", wantTable: true},
		{name: "an invalid higher file is skipped", user: `{"model":"gpt-6-sol"}`, shared: `not json`, wantFile: "user", wantModel: "gpt-6-sol", wantTable: true},
		{name: "env ANTHROPIC_MODEL outranks model", user: `{"model":"gpt-6-sol"}`, local: `{"env":{"ANTHROPIC_MODEL":"claude-haiku-4-5"}}`},
		{name: "an empty env ANTHROPIC_MODEL is unset", user: `{"model":"gpt-6-sol","env":{"ANTHROPIC_MODEL":""}}`, wantFile: "user", wantModel: "gpt-6-sol", wantTable: true},
		{name: "a higher empty env value shadows a lower one", user: `{"model":"gpt-6-sol","env":{"ANTHROPIC_MODEL":"claude-sonnet-5-5"}}`, local: `{"env":{"ANTHROPIC_MODEL":""}}`, wantFile: "user", wantModel: "gpt-6-sol", wantTable: true},
		{name: "env ANTHROPIC_BASE_URL is not Anthropic", user: `{"model":"gpt-6-sol","env":{"ANTHROPIC_BASE_URL":"https://gw.example"}}`},
		{name: "process ANTHROPIC_BASE_URL is not Anthropic", user: `{"model":"gpt-6-sol"}`, process: "ANTHROPIC_BASE_URL=https://gw.example"},
		{name: "process ANTHROPIC_MODEL outranks", user: `{"model":"gpt-6-sol"}`, process: "ANTHROPIC_MODEL=claude-sonnet-5-5"},
		{name: "an empty process ANTHROPIC_MODEL is unset", user: `{"model":"gpt-6-sol"}`, process: "ANTHROPIC_MODEL=", wantFile: "user", wantModel: "gpt-6-sol", wantTable: true},
		{name: "a FIFO is skipped", user: `{"model":"gpt-6-sol"}`, fifo: true, wantFile: "user", wantModel: "gpt-6-sol", wantTable: true},
		{name: "CLAUDE_CONFIG_DIR", user: `{"model":"gpt-6-sol"}`, configDir: true, wantFile: "user", wantModel: "gpt-6-sol", wantTable: true},
		{name: "local settings from the repository root", layout: "subdir", local: `{"model":"gpt-6-sol"}`, wantFile: "local", wantModel: "gpt-6-sol", wantTable: true},
		{name: "local settings from the main checkout", layout: "worktree", local: `{"model":"gpt-6-sol"}`, wantFile: "local", wantModel: "gpt-6-sol", wantTable: true},
	}
	rows := packagedRows(t) // before any Chdir: the path is relative
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home, base := t.TempDir(), t.TempDir()
			base, _ = filepath.EvalSymlinks(base)
			cwd, root := base, base
			switch c.layout {
			case "subdir":
				cwd = filepath.Join(root, "sub")
				writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
				// A subdirectory's own local file is not Claude's.
				writeFile(t, filepath.Join(cwd, ".claude", "settings.local.json"), `{"model":"claude-sonnet-5-5"}`)
			case "worktree":
				root, cwd = filepath.Join(base, "main"), filepath.Join(base, "wt")
				gitdir := filepath.Join(root, ".git", "worktrees", "wt")
				writeFile(t, filepath.Join(gitdir, "commondir"), "../..\n")
				writeFile(t, filepath.Join(cwd, ".git"), "gitdir: "+gitdir+"\n")
				writeFile(t, filepath.Join(cwd, ".claude", "settings.local.json"), `{"model":"claude-sonnet-5-5"}`)
			}
			if err := os.MkdirAll(cwd, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Chdir(cwd)
			env := NewEnv(nil)
			if c.process != "" {
				env = NewEnv([]string{c.process})
			}
			if c.fifo {
				if err := os.MkdirAll(filepath.Join(cwd, ".claude"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := syscall.Mkfifo(filepath.Join(cwd, ".claude", "settings.json"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			user := filepath.Join(home, ".claude", "settings.json")
			if c.configDir {
				dir := t.TempDir()
				env.Set("CLAUDE_CONFIG_DIR", dir)
				user = filepath.Join(dir, "settings.json")
				// The home file no longer counts.
				writeFile(t, filepath.Join(home, ".claude", "settings.json"), `{"model":"claude-sonnet-5-5"}`)
			}
			files := map[string]string{
				"user":   user,
				"shared": filepath.Join(cwd, ".claude", "settings.json"),
				"local":  filepath.Join(root, ".claude", "settings.local.json"),
			}
			for kind, body := range map[string]string{"user": c.user, "shared": c.shared, "local": c.local} {
				if body != "" {
					writeFile(t, files[kind], body)
				}
			}
			loaded := false
			table := func() *Table {
				loaded = true
				return &Table{Rows: rows}
			}
			file, model := (&App{}).foreignDefault(home, env, table)
			if file != files[c.wantFile] || model != c.wantModel {
				t.Errorf("got (%q, %q), want (%q, %q)", file, model, files[c.wantFile], c.wantModel)
			}
			if loaded != c.wantTable {
				t.Errorf("table loaded = %v, want %v", loaded, c.wantTable)
			}
		})
	}
}
