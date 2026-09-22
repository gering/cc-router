package agents

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func packagedRows(t *testing.T) []Row {
	t.Helper()
	data, err := os.ReadFile("../../share/models.tsv")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ParseTable(data)
	if err != nil {
		t.Fatalf("packaged table: %v", err)
	}
	return rows
}

func TestPackagedTable(t *testing.T) {
	rows := packagedRows(t)
	var names []string
	for _, r := range rows {
		names = append(names, r.Name)
	}
	// A literal expectation, not derived from the file it checks.
	if got := strings.Join(names, " "); got != "grok kimi sol terra luna astra" {
		t.Fatalf("rows = %s", got)
	}
	astra := rows[5]
	if astra.Model != "gpt-6-astra" || astra.Sonnet != "gpt-6-astra" || astra.Haiku != "gpt-5.6-luna" || astra.MaxCtx != 900000 {
		t.Fatalf("astra row = %+v", astra)
	}
}

func TestParseTableRejects(t *testing.T) {
	good := "a\tm1\tm1\tm1\tm1\t1000\tc\t-c-login\tC"
	cases := map[string]string{
		"crlf":            good + "\r\n",
		"columns":         "a\tm1\tm1\tm1\tm1\t1000\tc\t-c-login",
		"empty field":     "a\tm1\t\tm1\tm1\t1000\tc\t-c-login\tC",
		"name":            "-a\tm1\tm1\tm1\tm1\t1000\tc\t-c-login\tC",
		"model slash":     "a\tx/m1\tm1\tm1\tm1\t1000\tc\t-c-login\tC",
		"ctx zero":        "a\tm1\tm1\tm1\tm1\t0\tc\t-c-login\tC",
		"ctx leading 0":   "a\tm1\tm1\tm1\tm1\t0100\tc\t-c-login\tC",
		"ctx huge":        "a\tm1\tm1\tm1\tm1\t10000001\tc\t-c-login\tC",
		"ctx text":        "a\tm1\tm1\tm1\tm1\t3e5\tc\t-c-login\tC",
		"cred glob":       "a\tm1\tm1\tm1\tm1\t1000\tc*\t-c-login\tC",
		"login space":     "a\tm1\tm1\tm1\tm1\t1000\tc\t-c login\tC",
		"provider ctl":    "a\tm1\tm1\tm1\tm1\t1000\tc\t-c-login\tC\x01",
		"duplicate name":  good + "\n" + strings.Replace(good, "m1\tm1", "m2\tm1", 1),
		"duplicate var":   good + "\n" + "A\tm2\tm2\tm2\tm2\t1000\tc\t-c-login\tC",
		"duplicate model": good + "\n" + "b\tm1\tm1\tm1\tm1\t1000\tc\t-c-login\tC",
		"cred route":      good + "\n" + "b\tm2\tm2\tm2\tm2\t1000\tc\t-other\tC",
		"no rows":         "# only a comment\n",
		"long line":       strings.Repeat("x", 2000),
		"too many":        manyRows(51),
	}
	for name, input := range cases {
		_, err := ParseTable([]byte(input))
		switch {
		case err == nil:
			t.Errorf("%s: accepted", name)
		case strings.Contains(err.Error(), "m1"):
			t.Errorf("%s: diagnostic echoes table content: %v", name, err)
		}
	}
	if rows, err := ParseTable([]byte("# comment\n\n" + good + "\n")); err != nil || len(rows) != 1 {
		t.Fatalf("comments and blank lines: %v %v", rows, err)
	}
}

func manyRows(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "a%d\tx%d\tx\tx\tx\t1000\tc\t-c-login\tC\n", i, i)
	}
	return b.String()
}

func TestResolveModel(t *testing.T) {
	table := &Table{Rows: packagedRows(t)}
	cases := []struct{ in, agent, model string }{
		{"gpt-5.6-terra", "terra", "gpt-5.6-terra"},
		{"kimi-k3-256k", "kimi", "kimi-k3-256k"},
		{"gpt-6-astra", "astra", "gpt-6-astra"},
		{"gpt-5.6-luna", "luna", "gpt-5.6-luna"}, // primary outranks astra's borrowed tier
		{"gpt-5.3-codex-spark", "sol", "gpt-5.3-codex-spark"},
		{"grok-4.6-build", "grok", "grok-4.6"},
		{"grok-5.0-build", "grok", "grok-5.0"},
		{"grok-9.9-experimental", "grok", "grok-9.9-experimental"},
		{"grok", "grok", "grok"},             // no dash: the whole id is the family prefix
		{"grok-build", "grok", "grok-build"}, // too short for the build alias: family fallback
	}
	for _, c := range cases {
		agent, model, err := table.ResolveModel(c.in)
		if err != nil || agent != c.agent || model != c.model {
			t.Errorf("%s -> %s %s %v, want %s %s", c.in, agent, model, err, c.agent, c.model)
		}
	}
	for _, in := range []string{"gpt-9.9-unknown", "claude-opus-5"} {
		if _, _, err := table.ResolveModel(in); err == nil {
			t.Errorf("%s resolved", in)
		}
	}
	// A tier shared by rows that route differently is refused, never picked.
	split := &Table{Rows: []Row{
		{Name: "a", Model: "a1", Opus: "a1", Sonnet: "shared", Haiku: "a1", Cred: "x", Login: "-x", Provider: "X"},
		{Name: "b", Model: "b1", Opus: "b1", Sonnet: "shared", Haiku: "b1", Cred: "y", Login: "-y", Provider: "Y"},
	}}
	if _, _, err := split.ResolveModel("shared"); err == nil || !strings.Contains(err.Error(), "route differently") {
		t.Fatalf("differently routed shared tier: %v", err)
	}
}
