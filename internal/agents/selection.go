package agents

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Model-selection policy data. Discovery patterns and context knowledge are
// POLICY, not routing rows: models.tsv replaces the rows, these stay here.
//
// Precedence of an agent's effective model, highest first:
//  1. CC_HARNESS_MODEL_<AGENT> — an explicit override, on both routes.
//  2. Catalog discovery (remote route, agents with a discovery pattern): the
//     NEWEST CURRENTLY OFFERED canonical model. The last-known model is not a
//     floor — a catalog that stopped offering it has said so.
//  3. The table's last-known model, only when there is no valid catalog, and
//     labelled as such in the note.
//
// Discovery resolves a two-rung ladder: the newest candidate becomes the
// primary (and every tier the table pointed at the primary); the newest one
// BELOW it that can hold the session's ceiling becomes the tier the table
// pointed at the previous rung. Other tiers never move.
var (
	// The pattern accepts ONLY bare major.minor ids. The major is an
	// allow-list (4 and 5): a new generation is exactly where a provider
	// changes what a bare id means, so extending it is a deliberate edit.
	discovery = map[string]*regexp.Regexp{
		"grok": regexp.MustCompile(`^grok-(4|5)\.[0-9]+$`),
	}

	// The most a catalog-ADVERTISED window may claim for a discovering agent:
	// the largest window this route has been measured to serve for the family.
	discoveryContextCap = map[string]int{"grok": 500000}

	// Windows measured (or provider-documented) on this route. It is also what
	// gives a PINNED tier its real ceiling: the row's max_ctx belongs to the
	// primary only. Tiers with an undocumented window (gpt-5.3-codex-spark,
	// kimi-k2.7-code) are deliberately absent. Ordered: ties in the predecessor
	// search resolve to the earlier entry.
	verifiedContext = []modelContext{
		{"grok-4.3", 500000},
		{"grok-4.5", 500000},
		{"grok-4.6", 500000},
		{"grok-4.7", 500000},
		{"grok-composer-2.5-fast", 200000},
		{"kimi-k3", 262144},
		{"kimi-k3-256k", 262144},
		{"gpt-5.6-sol", 372000},
		{"gpt-5.6-terra", 372000},
		{"gpt-5.6-luna", 372000},
		{"gpt-6-astra", 900000},
	}
)

const (
	// What Claude Code assumes for an unrecognized slug: exporting it behaves
	// exactly like exporting nothing, and the note always says so.
	unverifiedMaxCtx = 200000
	// A release component longer than this is not a version number.
	versionMaxDigits = 4
	// The candidate list lands in a one-line TSV field.
	candidateNoteMax = 6
	// A catalog context_length outside this range is a typo, a unit mix-up or
	// an attack; the window then stays unknown.
	catalogContextMin = 32000
	catalogContextMax = maxContext
)

type modelContext struct {
	model string
	ctx   int
}

func lookupVerified(model string) (int, bool) {
	for _, v := range verifiedContext {
		if v.model == model {
			return v.ctx, true
		}
	}
	return 0, false
}

type catalogState int

const (
	catalogNone  catalogState = iota // the route has no catalog (--local)
	catalogStale                     // the route has one, but it could not be fetched
	catalogValid
)

// Catalog is the selected route's model list from ONE probe.
type Catalog struct {
	State   catalogState
	IDs     []string       // in response order
	Context map[string]int // validated per-id context_length metadata
}

func (c *Catalog) has(model string) bool {
	for _, id := range c.IDs {
		if id == model {
			return true
		}
	}
	return false
}

// missing lists the row's exported models the catalog lacks, deduplicated and
// in export order.
func (c *Catalog) missing(r Row) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range []string{r.Model, r.Opus, r.Sonnet, r.Haiku} {
		if seen[m] {
			continue
		}
		seen[m] = true
		if !c.has(m) {
			out = append(out, m)
		}
	}
	return out
}

// overrideVar is CC_HARNESS_MODEL_<AGENT>.
func overrideVar(agent string) string {
	return "CC_HARNESS_MODEL_" + strings.ToUpper(strings.ReplaceAll(agent, "-", "_"))
}

// release splits the version after the last '-' at its first '.'; dotted
// reports whether there was one.
func release(id string) (major, minor string, dotted bool) {
	return strings.Cut(id[strings.LastIndex(id, "-")+1:], ".")
}

// versionNewer compares component-wise and numerically (grok-4.20 is newer
// than grok-4.6). Anything that is not a short digit run refuses the
// comparison, which callers read as "not newer".
func versionNewer(a, b string) bool {
	am, an, aDot := release(a)
	bm, bn, bDot := release(b)
	if !aDot {
		an = "0"
	}
	if !bDot {
		bn = "0"
	}
	var n [4]int
	for i, s := range []string{am, an, bm, bn} {
		if !isDigits(s) || len(s) > versionMaxDigits {
			return false
		}
		n[i], _ = strconv.Atoi(s) // base 10: 08 is the eighth release
	}
	if n[0] != n[2] {
		return n[0] > n[2]
	}
	return n[1] > n[3]
}

// isCandidate: the pattern decides the SHAPE; the digit bound keeps a hostile
// id out of the comparison, where it could win by merely being seen first.
func isCandidate(pattern *regexp.Regexp, id string) bool {
	if !pattern.MatchString(id) {
		return false
	}
	major, minor, _ := release(id)
	return len(major) <= versionMaxDigits && len(minor) <= versionMaxDigits
}

// selector resolves models against one catalog.
type selector struct {
	env *Env
	cat *Catalog
}

// Selection is one row's effective model and its note ("" = nothing to say).
type Selection struct {
	Model    string
	Previous string
	MaxCtx   int
	Note     string
}

// modelContext returns a model's window from the first source that knows:
// VERIFIED, then catalog metadata (auto-selectable ids only, clamped to the
// family cap), then the newest verified PREDECESSOR (an assumption). note
// names a window that is not a measured one, so the source and its label
// come from one place.
func (s *selector) modelContext(agent, id string) (ctx int, note string, ok bool) {
	if ctx, ok := lookupVerified(id); ok {
		return ctx, "", true
	}
	limit, ok := discoveryContextCap[agent]
	if !ok {
		return 0, "", false
	}
	pattern, ok := discovery[agent]
	if !ok || !isCandidate(pattern, id) {
		return 0, "", false
	}
	// A catalog value ENDS the search: the gateway said something plausible,
	// so nothing is assumed over it — a smaller window included.
	if ctx, ok := s.cat.Context[id]; ok {
		ctx = min(ctx, limit)
		return ctx, fmt.Sprintf("; context window %d from catalog metadata, not measured", ctx), true
	}
	from, ok := assumedFrom(pattern, id)
	if !ok {
		return 0, "", false
	}
	ctx, ok = lookupVerified(from)
	return ctx, fmt.Sprintf("; context window %d ASSUMED from predecessor %s, not verified", ctx, from), ok
}

// assumedFrom is the newest VERIFIED candidate strictly older than id, across
// majors. It chains only from a verified row, never from another assumption.
func assumedFrom(pattern *regexp.Regexp, id string) (string, bool) {
	best := ""
	for _, v := range verifiedContext {
		if !isCandidate(pattern, v.model) || !versionNewer(id, v.model) {
			continue
		}
		if best == "" || versionNewer(v.model, best) {
			best = v.model
		}
	}
	return best, best != ""
}

// ceiling is a model's window or the conservative one, plus its note.
func (s *selector) ceiling(agent, id string) (int, string) {
	if ctx, note, ok := s.modelContext(agent, id); ok {
		return ctx, note
	}
	return unverifiedMaxCtx, fmt.Sprintf("; context window unknown, conservative ceiling %d", unverifiedMaxCtx)
}

// rungHolds: the exported ceiling is the PRIMARY's and set once per session,
// so every rung reachable with /model has to hold it.
func (s *selector) rungHolds(agent, id string, ceiling int) bool {
	if ctx, _, ok := s.modelContext(agent, id); ok {
		return ctx >= ceiling
	}
	return ceiling <= unverifiedMaxCtx
}

// highest is the newest candidate in the catalog, optionally strictly below
// `below` and able to hold `ceiling` (0 = no constraint).
func (s *selector) highest(agent string, pattern *regexp.Regexp, below string, ceiling int) string {
	best := ""
	for _, id := range s.cat.IDs {
		if !isCandidate(pattern, id) {
			continue
		}
		if below != "" && !versionNewer(below, id) {
			continue
		}
		if ceiling > 0 && !s.rungHolds(agent, id, ceiling) {
			continue
		}
		if best == "" || versionNewer(id, best) {
			best = id
		}
	}
	return best
}

// candidates lists every candidate the catalog offers, newest first, capped.
func (s *selector) candidates(pattern *regexp.Regexp) string {
	var c []string
	for _, id := range s.cat.IDs {
		if isCandidate(pattern, id) {
			c = append(c, id)
		}
	}
	sort.SliceStable(c, func(i, j int) bool { return versionNewer(c[i], c[j]) })
	if len(c) > candidateNoteMax {
		return strings.Join(c[:candidateNoteMax], " ") + " …"
	}
	return strings.Join(c, " ")
}

// Resolve computes one row's effective model. It runs once per invocation,
// before exec, and the result is exported as a literal id.
func (s *selector) Resolve(r Row) Selection {
	sel := Selection{Model: r.Model, Previous: r.Sonnet, MaxCtx: r.MaxCtx}
	variable := overrideVar(r.Name)
	if override := s.env.Get(variable); override != "" {
		switch {
		case strings.ContainsAny(override, " \t\n\v\f\r") || hasControl(override) || !modelIDRe.MatchString(override):
			// Exported, it would 404 the whole session as a nonsense model id.
			sel.Note = fmt.Sprintf("%s is malformed — ignored, using %s", variable, r.Model)
		case override == r.Model:
			// Pinning the row's OWN primary is a no-op, not a ladder move:
			// the resume path pins the recorded primary, and collapsing the
			// tiers there silently changed a resumed session's sonnet rung.
		default:
			// One reproducible model: every ladder-tracking tier collapses
			// onto it, with the ceiling of THAT model — sharing a row is not
			// sharing a window.
			ctx, note := s.ceiling(r.Name, override)
			sel = Selection{Model: override, Previous: override, MaxCtx: ctx,
				Note: fmt.Sprintf("pinned to %s via %s%s", override, variable, note)}
		}
		return sel
	}

	pattern, ok := discovery[r.Name]
	if !ok {
		return sel
	}
	lastKnown := r.Model + " is the last-known model, not current discovery"
	switch s.cat.State {
	case catalogStale:
		sel.Note = lastKnown + " (catalog unavailable)"
		return sel
	case catalogNone:
		sel.Note = lastKnown + " (this route has no catalog)"
		return sel
	}

	top := s.highest(r.Name, pattern, "", 0)
	if top == "" {
		// A valid catalog without a candidate is an answer: nothing is
		// substituted, and the availability check reports the row missing.
		sel.Note = fmt.Sprintf("catalog offers no canonical %s model — %s", r.Name, lastKnown)
		return sel
	}
	ctx, note := s.ceiling(r.Name, top)
	sel.Model, sel.MaxCtx = top, ctx
	if top != r.Model || note != "" {
		sel.Note = fmt.Sprintf("auto-selected %s (candidates: %s; last-known: %s)%s", top, s.candidates(pattern), r.Model, note)
	}
	sel.Previous = s.highest(r.Name, pattern, top, ctx)
	if sel.Previous == "" {
		sel.Previous = top
	}
	return sel
}

// Apply pushes a selection into the row. A tier follows the ladder only where
// the TABLE pointed it at one of the two rungs; anything else stays put.
func (sel Selection) Apply(r Row) Row {
	pinned, pinnedPrev := r.Model, r.Sonnet
	for _, tier := range []*string{&r.Opus, &r.Sonnet, &r.Haiku} {
		switch *tier {
		case pinned:
			*tier = sel.Model
		case pinnedPrev:
			*tier = sel.Previous
		}
	}
	r.Model, r.MaxCtx = sel.Model, sel.MaxCtx
	return r
}

// mergeNotes joins the route note and the selection note; "-" is the TSV's
// "nothing to say" placeholder and never survives as a prefix.
func mergeNotes(base, selection string) string {
	switch {
	case selection == "" && base == "":
		return "-"
	case selection == "":
		return base
	case base == "" || base == "-":
		return selection
	}
	return base + "; " + selection
}
