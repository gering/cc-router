package agents

import (
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
	}
	for _, c := range cases {
		if got := versionNewer(c.a, c.b); got != c.want {
			t.Errorf("versionNewer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

func TestCandidates(t *testing.T) {
	grok := discovery["grok"]
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
	if got := s.candidates(grok); got != "grok-4.8 grok-4.7 grok-4.6 grok-4.5 grok-4.4 grok-4.3 …" {
		t.Fatalf("candidates = %q", got)
	}
}

func TestResolveOverrideAndLadder(t *testing.T) {
	rows := packagedRows(t)
	grok, astra := rows[0], rows[5]
	catalog := &Catalog{State: catalogValid, IDs: strings.Fields("grok-4.5 grok-4.6 grok-4.8 grok-composer-2.5-fast"), Context: map[string]int{}}

	sel := (&selector{env: NewEnv(nil), cat: catalog}).Resolve(grok)
	eff := sel.Apply(grok)
	if eff.Model != "grok-4.8" || eff.Opus != "grok-4.8" || eff.Sonnet != "grok-4.6" || eff.Haiku != "grok-composer-2.5-fast" || eff.MaxCtx != 500000 {
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
