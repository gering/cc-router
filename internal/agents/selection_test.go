package agents

import (
	"fmt"
	"strings"
	"testing"
)

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"grok-4.20", "grok-4.6", true}, // component-wise, not decimal
		{"grok-5.0", "grok-4.20", true},
		{"grok-4.08", "grok-4.7", true}, // base 10, not octal
		{"grok-4.6", "grok-4.6", false},
		{"grok-4.5", "grok-4.6", false},
		{"grok-4.99999", "grok-4.6", false}, // overlong component refuses
		{"grok-4.x", "grok-4.6", false},
		{"grok-.9", "grok-4.6", false},
		{"gpt-6.1-sol", "gpt-6-sol", true}, // a bare major is its .0
		{"gpt-6.10-luna", "gpt-6.9-luna", true},
		{"gpt-6-sol", "gpt-6.0-sol", false},
	}
	for _, c := range cases {
		if got := versionNewer(c.a, c.b); got != c.want {
			t.Errorf("versionNewer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

func TestCandidates(t *testing.T) {
	grok := discovery["xai"].pattern
	for id, want := range map[string]bool{
		"grok-4.6": true, "grok-5.0": true, "grok-4.9999": true,
		"grok-4.99999": false, "grok-6.0": false, "grok-5": false, "grok-4.6.1": false,
		"grok-4.9-fast": false, "Grok-4.9": false, " grok-4.9": false, "grok-4.9 ": false,
	} {
		if got := isCandidate(grok, id); got != want {
			t.Errorf("isCandidate(%q) = %v", id, got)
		}
	}
	s := &selector{env: NewEnv(nil), cat: &Catalog{IDs: strings.Fields("grok-4.1 grok-4.2 grok-4.3 grok-4.4 grok-4.5 grok-4.6 grok-4.7 grok-4.8")}}
	if got := s.candidates("xai", "grok-*"); got != "grok-4.8 grok-4.7 grok-4.6 grok-4.5 grok-4.4 grok-4.3 …" {
		t.Fatalf("candidates = %q", got)
	}
}

func TestClass(t *testing.T) {
	for id, want := range map[string]string{
		"grok-4.6": "grok-*", "grok-composer-2.5-fast": "grok-composer-*-fast",
		"gpt-6-sol": "gpt-*-sol", "gpt-6.1-sol": "gpt-*-sol", "gpt-5.6-terra": "gpt-*-terra",
		"kimi-k3": "kimi-k3", "grok-.9": "grok-.9",
	} {
		if got := class(id); got != want {
			t.Errorf("class(%s) = %s, want %s", id, got, want)
		}
	}
}

// codexLadder resolves one codex row against a catalog of the table ids plus
// extra, and returns the exported ladder as "model fable opus sonnet haiku ctx".
func codexLadder(t *testing.T, agent string, extra ...string) (string, string) {
	t.Helper()
	var row Row
	for _, r := range packagedRows(t) {
		if r.Name == agent {
			row = r
		}
	}
	ids := append(strings.Fields("gpt-6-astra gpt-6-sol gpt-5.6-terra gpt-6-luna gpt-5.6-sol gpt-5.6-luna gpt-5.5"), extra...)
	sel := (&selector{env: NewEnv(nil), cat: &Catalog{State: catalogValid, IDs: ids, Context: map[string]int{}}}).Resolve(row)
	e := sel.Apply(row)
	return fmt.Sprintf("%s %s %s %s %s %d", e.Model, e.Fable, e.Opus, e.Sonnet, e.Haiku, e.MaxCtx), sel.Note
}

func TestCodexDiscovery(t *testing.T) {
	cases := []struct {
		name, agent string
		extra       []string
		want        string
	}{
		{"6.0 only", "sol", nil, "gpt-6-sol gpt-6-astra gpt-6-sol gpt-5.6-terra gpt-6-luna 372000"},
		{"6.0 only, astra keeps the shared ceiling", "astra", nil, "gpt-6-astra gpt-6-astra gpt-6-sol gpt-5.6-terra gpt-6-luna 372000"},
		// Rungs move independently: only the sol role moved.
		{"6.1-sol only", "sol", []string{"gpt-6.1-sol"}, "gpt-6.1-sol gpt-6-astra gpt-6.1-sol gpt-5.6-terra gpt-6-luna 372000"},
		{"6.1-sol only, other row", "luna", []string{"gpt-6.1-sol"}, "gpt-6-luna gpt-6-astra gpt-6.1-sol gpt-5.6-terra gpt-6-luna 372000"},
		{"mixed minors per role", "terra", []string{"gpt-6.1-sol", "gpt-6.2-sol", "gpt-6.1-luna", "gpt-6.3-astra", "gpt-6-terra"},
			"gpt-6-terra gpt-6.3-astra gpt-6.2-sol gpt-6-terra gpt-6.1-luna 372000"},
		{"7.x and variants ignored", "astra",
			[]string{"gpt-7-sol", "gpt-7.1-astra", "gpt-6.1-sol-preview", "gpt-6-sol-fast", "gpt-6.1-nova", "GPT-6.1-luna", "gpt-6.99999-sol", "gpt-6.1"},
			"gpt-6-astra gpt-6-astra gpt-6-sol gpt-5.6-terra gpt-6-luna 372000"},
	}
	for _, c := range cases {
		got, note := codexLadder(t, c.agent, c.extra...)
		if got != c.want {
			t.Errorf("%s: ladder = %s, want %s", c.name, got, c.want)
		}
		if c.extra == nil || strings.HasPrefix(c.name, "7.x") {
			if note != "" {
				t.Errorf("%s: note = %q, want none", c.name, note)
			}
		} else if !strings.Contains(note, "auto-selected gpt-6") {
			t.Errorf("%s: note = %q", c.name, note)
		}
	}
	if _, note := codexLadder(t, "sol", "gpt-6.1-sol"); note != "auto-selected gpt-6.1-sol (candidates: gpt-6.1-sol gpt-6-sol; last-known: gpt-6-sol)" {
		t.Errorf("note = %q", note)
	}
}

func TestCodexResume(t *testing.T) {
	rows := packagedRows(t)
	sol := rows[2]
	cat := &Catalog{State: catalogValid, IDs: strings.Fields("gpt-6-astra gpt-6-sol gpt-6.1-sol gpt-6.2-sol gpt-5.6-terra gpt-6-luna"), Context: map[string]int{}}
	// An old gpt-6-sol session pins its own primary: a no-op, no discovery.
	old := (&selector{env: NewEnv([]string{"CC_HARNESS_MODEL_SOL=gpt-6-sol"}), cat: cat}).Resolve(sol)
	if got := old.Apply(sol); got != sol || old.Note != "" {
		t.Fatalf("gpt-6-sol resume = %+v %q", got, old.Note)
	}
	// A pinned discovered minor outranks a newer one, keeps the inherited
	// window and moves only its own class: Terra is another role.
	pin := (&selector{env: NewEnv([]string{"CC_HARNESS_MODEL_SOL=gpt-6.1-sol"}), cat: cat}).Resolve(sol)
	got := pin.Apply(sol)
	if got.Model != "gpt-6.1-sol" || got.Opus != "gpt-6.1-sol" || got.Sonnet != "gpt-5.6-terra" || got.Haiku != "gpt-6-luna" || got.MaxCtx != 372000 {
		t.Fatalf("gpt-6.1-sol resume = %+v", got)
	}
	if pin.Note != "pinned to gpt-6.1-sol via CC_HARNESS_MODEL_SOL" {
		t.Fatalf("note = %q", pin.Note)
	}
}

func TestResolveOverrideAndLadder(t *testing.T) {
	rows := packagedRows(t)
	grok, sol, astra := rows[0], rows[2], rows[5]
	catalog := &Catalog{State: catalogValid, IDs: strings.Fields("grok-4.5 grok-4.6 grok-4.8 grok-composer-2.5-fast"), Context: map[string]int{}}

	sel := (&selector{env: NewEnv(nil), cat: catalog}).Resolve(grok)
	eff := sel.Apply(grok)
	if eff.Model != "grok-4.8" || eff.Fable != "grok-4.8" || eff.Opus != "grok-4.8" || eff.Sonnet != "grok-4.6" || eff.Haiku != "grok-composer-2.5-fast" || eff.MaxCtx != 500000 {
		t.Fatalf("ladder = %+v", eff)
	}
	if !strings.Contains(sel.Note, "ASSUMED from predecessor grok-4.7") {
		t.Fatalf("note = %q", sel.Note)
	}

	// Pinning a row's own primary is a complete no-op.
	pinned := (&selector{env: NewEnv([]string{"CC_HARNESS_MODEL_ASTRA=gpt-6-astra"}), cat: &Catalog{}}).Resolve(astra)
	if got := pinned.Apply(astra); got != astra || pinned.Note != "" {
		t.Fatalf("primary pin = %+v %q", got, pinned.Note)
	}

	// A different model collapses the tracking tiers and uses its own ceiling.
	other := (&selector{env: NewEnv([]string{"CC_HARNESS_MODEL_GROK=grok-composer-2.5-fast"}), cat: &Catalog{}}).Resolve(grok)
	if got := other.Apply(grok); got.Model != "grok-composer-2.5-fast" || got.Sonnet != "grok-composer-2.5-fast" || got.MaxCtx != 200000 {
		t.Fatalf("override = %+v", got)
	}

	// An override never RAISES the ceiling: astra's own 900000 over sol's
	// untouched Luna rung would overflow upstream one /model away.
	capped := (&selector{env: NewEnv([]string{"CC_HARNESS_MODEL_SOL=gpt-6-astra"}), cat: &Catalog{}}).Resolve(sol)
	if got := capped.Apply(sol); got.Model != "gpt-6-astra" || got.Haiku != "gpt-6-luna" || got.MaxCtx != 372000 ||
		!strings.Contains(capped.Note, "capped at the row ceiling 372000 (its own window 900000") {
		t.Fatalf("capped override = %+v %q", got, capped.Note)
	}

	// A RETAINED id on its former row swaps only the primary and the rungs
	// equal to it; the tracking sonnet rung keeps the table's value.
	retained := (&selector{env: NewEnv([]string{"CC_HARNESS_MODEL_SOL=gpt-5.6-sol"}), cat: &Catalog{}}).Resolve(sol)
	if got := retained.Apply(sol); got.Model != "gpt-5.6-sol" || got.Opus != "gpt-5.6-sol" || got.Fable != "gpt-6-astra" ||
		got.Sonnet != "gpt-5.6-terra" || got.Haiku != "gpt-6-luna" || got.MaxCtx != 372000 {
		t.Fatalf("retained pin = %+v", got)
	}

	// An override is exported as a literal model id, so it is held to the same
	// shape as every id the table carries: whitespace, control bytes, an id
	// longer than the table would accept, or characters no model id uses.
	for _, bad := range []string{"grok 4.6", "grok-4.6\x1b[2J", "grok-4.6$(id)", strings.Repeat("grok-4.6", 20)} {
		malformed := (&selector{env: NewEnv([]string{"CC_HARNESS_MODEL_GROK=" + bad}), cat: &Catalog{}}).Resolve(grok)
		if malformed.Model != "grok-4.6" || !strings.Contains(malformed.Note, "is malformed") {
			t.Errorf("override %q = %+v", bad, malformed)
		}
	}
}

func TestMergeNotes(t *testing.T) {
	for _, c := range [][3]string{
		{"", "", "-"}, {"route", "", "route"}, {"-", "sel", "sel"}, {"", "sel", "sel"}, {"route", "sel", "route; sel"},
	} {
		if got := mergeNotes(c[0], c[1]); got != c[2] {
			t.Errorf("mergeNotes(%q, %q) = %q", c[0], c[1], got)
		}
	}
}
