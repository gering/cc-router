package agents

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Model-selection policy data. Discovery patterns and context knowledge are
// POLICY, not routing rows: models.tsv replaces the rows, these stay here.
//
// Precedence of an agent's effective model, highest first:
//  1. CC_HARNESS_MODEL_<AGENT> — an explicit override, on both routes.
//  2. Catalog discovery (remote route, rows of a discovering family): the
//     NEWEST CURRENTLY OFFERED canonical model per class. The last-known model is not a
//     floor — a catalog that stopped offering it has said so.
//  3. The table's last-known model, only when there is no valid catalog, and
//     labelled as such in the note.
//
// Discovery works per release CLASS — an id with its release masked: grok-4.6
// and grok-4.8 are grok-*, gpt-6-sol and gpt-6.1-sol are gpt-*-sol. Every class
// a row's ladder uses resolves on its own: the table's newest id in it moves to
// the newest offered candidate, the next one to the newest candidate BELOW that
// can hold the session's ceiling. Grok's two-rung ladder (primary + previous)
// and Codex's four independent roles are the same rule.
type family struct {
	// The pattern accepts ONLY canonical ids, and its major is an allow-list:
	// a new generation is exactly where a provider changes what an id means,
	// so extending it is a deliberate edit.
	pattern *regexp.Regexp
	// catalogCap is the most a catalog-ADVERTISED window may claim: the
	// largest window this route has been measured to serve for the family.
	// 0 = the family's windows never come from the catalog or a predecessor.
	catalogCap int
	// majorContext is the verified window every release of a major inherits.
	// Robert's policy for Codex: a minor is not expected to shrink its major's
	// window. It never crosses majors, so a major enters the pattern only
	// together with its measured window.
	majorContext map[string]int
}

var (
	// Keyed by credential prefix: one route family, one catalog shape.
	discovery = map[string]family{
		"xai": {pattern: regexp.MustCompile(`^grok-(4|5)\.[0-9]+$`), catalogCap: 500000},
		"codex": {
			pattern:      regexp.MustCompile(`^gpt-6(\.[0-9]+)?-(sol|terra|luna|astra)$`),
			majorContext: map[string]int{"6": 372000},
		},
	}

	// Windows measured (or provider-documented) on this route. It is also what
	// gives a PINNED tier its real ceiling: the row's max_ctx belongs to the
	// primary only. Ids with an undocumented window (kimi-k2.7-code, gpt-5.5)
	// are deliberately absent. gpt-6-sol/luna carry their FLOOR-TESTED session
	// budget, not a measured maximum; the superseded gpt-5.6-sol/luna stay so a
	// retained resume pin keeps its window. Ordered: ties in the predecessor
	// search resolve to the earlier entry.
	verifiedContext = []verifiedWindow{
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
		{"gpt-6-sol", 372000},
		{"gpt-6-luna", 372000},
		{"gpt-6-astra", 900000},
	}

	// Ids that left the table while the route still serves them and that no
	// discovery pattern covers, mapped to their former row. Without this
	// resolve-model would refuse them and a session recorded on gpt-5.6-sol
	// could not resume on its own model. A route that stops serving one
	// reports the row unavailable, never a silent remap; drop the entry then,
	// and resume refuses it like any retired id.
	retainedModels = map[string]string{
		"gpt-5.6-sol":  "sol",
		"gpt-5.6-luna": "luna",
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

type verifiedWindow struct {
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
	for _, m := range r.models() {
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

// splitRelease locates an id's release: the first '-'-separated segment that
// starts with a digit (grok-4.6, gpt-6.1-sol, grok-composer-2.5-fast).
func splitRelease(id string) (head, version, tail string, ok bool) {
	for i := 0; i+1 < len(id); i++ {
		if id[i] != '-' || id[i+1] < '0' || id[i+1] > '9' {
			continue
		}
		head, rest := id[:i+1], id[i+1:]
		if j := strings.IndexByte(rest, '-'); j >= 0 {
			return head, rest[:j], rest[j:], true
		}
		return head, rest, "", true
	}
	return id, "", "", false
}

// release splits the version at its first '.'; dotted reports whether there
// was one.
func release(id string) (major, minor string, dotted bool) {
	_, version, _, _ := splitRelease(id)
	return strings.Cut(version, ".")
}

// class masks the release: ids that differ only in it are one role in one
// family. An id without a release is its own class.
func class(id string) string {
	head, _, tail, ok := splitRelease(id)
	if !ok {
		return id
	}
	return head + "*" + tail
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

// Selection is one row's effective primary, its ceiling, the table rungs it
// moves (table id → effective id) and its note ("" = nothing to say).
type Selection struct {
	Model  string
	MaxCtx int
	Note   string
	moves  map[string]string
}

// modelContext returns a model's window from the first source that knows:
// VERIFIED, then its major's inherited window, then catalog metadata (clamped
// to the family cap), then the newest verified PREDECESSOR (an assumption).
// The last three apply to candidates of the row's family only. note names a
// window that is not a measured one, so the source and its label come from
// one place.
func (s *selector) modelContext(cred, id string) (ctx int, note string, ok bool) {
	if ctx, ok := lookupVerified(id); ok {
		return ctx, "", true
	}
	fam, ok := discovery[cred]
	if !ok || !isCandidate(fam.pattern, id) {
		return 0, "", false
	}
	major, _, _ := release(id)
	if ctx, ok := fam.majorContext[major]; ok {
		return ctx, "", true
	}
	if fam.catalogCap == 0 {
		return 0, "", false
	}
	// A catalog value ENDS the search: the gateway said something plausible,
	// so nothing is assumed over it — a smaller window included.
	if ctx, ok := s.cat.Context[id]; ok {
		ctx = min(ctx, fam.catalogCap)
		return ctx, fmt.Sprintf("; context window %d from catalog metadata, not measured", ctx), true
	}
	from, ok := assumedFrom(fam.pattern, id)
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
func (s *selector) ceiling(cred, id string) (int, string) {
	if ctx, note, ok := s.modelContext(cred, id); ok {
		return ctx, note
	}
	return unverifiedMaxCtx, fmt.Sprintf("; context window unknown, conservative ceiling %d", unverifiedMaxCtx)
}

// rungHolds: the exported ceiling is set once per session, so every rung
// reachable with /model has to hold it.
func (s *selector) rungHolds(cred, id string, ceiling int) bool {
	if ctx, _, ok := s.modelContext(cred, id); ok {
		return ctx >= ceiling
	}
	return ceiling <= unverifiedMaxCtx
}

// highest is the newest candidate of one class in the catalog, optionally
// strictly below `below` and able to hold `ceiling` (0 = no constraint).
func (s *selector) highest(cred, cls, below string, ceiling int) string {
	best := ""
	for _, id := range s.cat.IDs {
		if !isCandidate(discovery[cred].pattern, id) || class(id) != cls {
			continue
		}
		if below != "" && !versionNewer(below, id) {
			continue
		}
		if ceiling > 0 && !s.rungHolds(cred, id, ceiling) {
			continue
		}
		if best == "" || versionNewer(id, best) {
			best = id
		}
	}
	return best
}

// candidates lists every candidate of one class the catalog offers, newest
// first, capped.
func (s *selector) candidates(cred, cls string) string {
	var c []string
	for _, id := range s.cat.IDs {
		if isCandidate(discovery[cred].pattern, id) && class(id) == cls {
			c = append(c, id)
		}
	}
	sort.SliceStable(c, func(i, j int) bool { return versionNewer(c[i], c[j]) })
	if len(c) > candidateNoteMax {
		return strings.Join(c[:candidateNoteMax], " ") + " …"
	}
	return strings.Join(c, " ")
}

// classes groups the row's distinct models by class, the primary's class
// first and each group newest first: the ladder discovery walks.
func classes(r Row) [][]string {
	var out [][]string
	at := map[string]int{}
	for _, m := range r.models() {
		c := class(m)
		i, ok := at[c]
		if !ok {
			i, at[c] = len(out), len(out)
			out = append(out, nil)
		}
		if !slices.Contains(out[i], m) {
			out[i] = append(out[i], m)
		}
	}
	for _, ids := range out {
		sort.SliceStable(ids, func(i, j int) bool { return versionNewer(ids[i], ids[j]) })
	}
	return out
}

// Resolve computes one row's effective model. It runs once per invocation,
// before exec, and the result is exported as a literal id.
func (s *selector) Resolve(r Row) Selection {
	sel := Selection{Model: r.Model, MaxCtx: r.MaxCtx}
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
			// One reproducible model: every rung the table put in the
			// primary's class collapses onto it (grok's previous rung follows,
			// codex's other roles stay), with the ceiling of THAT model —
			// sharing a row is not sharing a window. A retained or discovered
			// id so resumes the ladder it ran under. The ceiling is never
			// RAISED above the row's: the rungs that stay are reachable with
			// /model, and the row ceiling is what they were sized against.
			ctx, note := s.ceiling(r.Cred, override)
			if known, _, ok := s.modelContext(r.Cred, override); ok && known > r.MaxCtx {
				ctx = r.MaxCtx
				note = fmt.Sprintf("; context window capped at the row ceiling %d (its own window %d exceeds a reachable rung)", r.MaxCtx, known)
			}
			sel = Selection{Model: override, MaxCtx: ctx, moves: map[string]string{},
				Note: fmt.Sprintf("pinned to %s via %s%s", override, variable, note)}
			for _, m := range r.models() {
				if class(m) == class(r.Model) {
					sel.moves[m] = override
				}
			}
		}
		return sel
	}

	fam, ok := discovery[r.Cred]
	if !ok {
		return sel
	}
	// Only a primary the family could have discovered is "last-known"; a
	// row standing on an id outside the pattern (gpt-5.6-terra) is the table's.
	discoverable := isCandidate(fam.pattern, r.Model)
	lastKnown := r.Model + " is the last-known model, not current discovery"
	switch {
	case s.cat.State == catalogStale && discoverable:
		sel.Note = lastKnown + " (catalog unavailable)"
	case s.cat.State == catalogNone && discoverable:
		sel.Note = lastKnown + " (this route has no catalog)"
	}
	if s.cat.State != catalogValid {
		return sel
	}

	sel.moves = map[string]string{}
	var notes []string
	ceiling := 0 // the session's: the smallest window among the placed rungs
	for _, ids := range classes(r) {
		cls, below, bound := class(ids[0]), "", 0
		for i, id := range ids {
			// A class's newest rung is unconstrained and may LOWER the
			// ceiling; a rung below it must hold the ceiling as it stands.
			pick := s.highest(r.Cred, cls, below, bound)
			if pick == "" && i == 0 {
				// A valid catalog without a candidate is an answer: nothing
				// is substituted, and the availability check reports the
				// table id missing.
				if id == r.Model && discoverable {
					notes = append(notes, fmt.Sprintf("catalog offers no canonical %s model — %s", r.Name, lastKnown))
				}
				break
			}
			if pick == "" {
				pick = below // nothing lower holds the ceiling: share the rung above
			}
			sel.moves[id] = pick
			ctx, note := s.ceiling(r.Cred, pick)
			if ceiling == 0 || ctx < ceiling {
				ceiling = ctx
			}
			if i == 0 && (pick != id || note != "") {
				notes = append(notes, fmt.Sprintf("auto-selected %s (candidates: %s; last-known: %s)%s", pick, s.candidates(r.Cred, cls), id, note))
			}
			below, bound = pick, ceiling
		}
	}
	if m, ok := sel.moves[r.Model]; ok {
		sel.Model = m
	}
	if ceiling > 0 {
		sel.MaxCtx = ceiling
	}
	sel.Note = strings.Join(notes, "; ")
	return sel
}

// Apply pushes a selection into the row. A tier moves only where the TABLE
// pointed it at an id the selection moved; anything else stays put.
func (sel Selection) Apply(r Row) Row {
	for _, tier := range []*string{&r.Fable, &r.Opus, &r.Sonnet, &r.Haiku} {
		if m, ok := sel.moves[*tier]; ok {
			*tier = m
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
