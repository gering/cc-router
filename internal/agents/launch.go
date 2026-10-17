package agents

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// launch is the executable path of `claude`: the interactive wrapper hands it
// Claude's argv, and it replaces itself with the native
// launcher — routed when a foreign-model flag or a resumed session asks for
// it, untouched otherwise. Parsing, conflicts and the resume restore live
// here exactly once, so a caller that never sources a shell function gets the
// same routing as a terminal.
//
// Exact arg match only: prompt text is never rewritten.

// Value-taking Claude options, split by how many tokens each consumes.
// Derived from `claude --help` (`<v>` required, `<v...>` variadic, `[v]`
// optional); refresh it when the CLI grows a flag whose value could be
// spelled like a resume selector. A flag missing here is not fatal — its
// value is simply re-parsed as an option, which is how `--resume` used to be
// forged out of someone else's argument.
var (
	requiredValue = setOf("--agent", "--agents", "--append-system-prompt", "--autocompact", "--debug-file",
		"--effort", "--environment", "--fallback-model", "--input-format", "--json-schema", "--max-budget-usd",
		"--name", "--output-format", "--permission-mode", "--permission-prompts", "--plugin-dir", "--plugin-url",
		"--remote-control-session-name-prefix", "--session-id", "--setting-sources", "--settings",
		"--system-prompt", "--system-prompt-snapshot", "-n")
	variadicValue = setOf("--add-dir", "--allowedTools", "--allowed-tools", "--betas", "--disallowedTools",
		"--disallowed-tools", "--file", "--mcp-config", "--tools")
	optionalValue = setOf("--cloud", "--debug", "--from-pr", "--prompt-suggestions", "--remote-control",
		"--teleport", "--worktree", "-d", "-w")
	// The NATIVE counterpart of the foreign-model flags: `--opus` IS `--model
	// opus`, no more. They exist for resume — the restore stands down only for
	// an explicit model choice, so without them `-c` always re-enters the
	// session's foreign agent and there is no short way back to Claude.
	// Aliases, not pinned ids: the CLI resolves them to the current release.
	nativeShorthands = setOf("--fable", "--opus", "--sonnet", "--haiku")
)

func setOf(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}

// launchPlan is a parsed command line: Claude's argv with this layer's own
// flags consumed or expanded, plus what they selected.
type launchPlan struct {
	args        []string
	yolo        bool
	local       bool
	agent       string // foreign agent, from a flag or a restore
	agentFlag   string // the flag the CALLER gave, for conflicts and replay
	nativeModel bool   // --model or a native shorthand
	nativeFlag  string
	// The resume selector: "continue", "picker", or --resume's value; with the
	// indices of its option and value in args (-1 when absent).
	resume        string
	resumeAt      int
	resumeValueAt int
	resumes       int
}

// conflict is a refused command line; launch reports it as `claude: …`.
type conflict string

func (c conflict) Error() string { return string(c) }

// parseLaunch splits argv into Claude's argv and this layer's selections.
// isAgent answers whether `--<name>` is a row of the table.
func parseLaunch(argv []string, isAgent func(string) bool) (*launchPlan, error) {
	p := &launchPlan{resumeAt: -1, resumeValueAt: -1}
	passthrough, valuePending, optvalPending, optvalIsResume, variadicPending := false, false, false, false, false
	for _, arg := range argv {
		// After a bare `--` every token is verbatim, so a literal `--sol` reaches
		// the CLI instead of being consumed here.
		if passthrough {
			p.args = append(p.args, arg)
			continue
		}
		// A REQUIRED value is data even when it looks like a flag:
		// `--append-system-prompt --continue` passes the literal text.
		if valuePending {
			p.args = append(p.args, arg)
			valuePending = false
			continue
		}
		// An OPTIONAL or VARIADIC value ends at the next option token.
		if (optvalPending || variadicPending) && !strings.HasPrefix(arg, "-") {
			p.args = append(p.args, arg)
			if optvalIsResume {
				p.resume = arg
				p.resumeValueAt = len(p.args) - 1
				optvalIsResume = false
			}
			optvalPending = false
			continue
		}
		optvalPending, variadicPending, optvalIsResume = false, false, false
		switch {
		case arg == "-c" || arg == "--continue":
			p.args = append(p.args, arg)
			p.markResume("continue")
		case arg == "-r" || arg == "--resume":
			p.args = append(p.args, arg)
			p.markResume("picker")
			optvalPending, optvalIsResume = true, true
		case strings.HasPrefix(arg, "--resume="):
			p.args = append(p.args, arg)
			p.markResume(strings.TrimPrefix(arg, "--resume="))
		case arg == "--model" || strings.HasPrefix(arg, "--model="):
			if p.nativeFlag != "" {
				return nil, conflict(p.nativeFlag + " conflicts with --model — pick one")
			}
			p.nativeModel = true
			valuePending = arg == "--model"
			p.args = append(p.args, arg)
		case nativeShorthands[arg]:
			switch {
			case p.nativeFlag != "" && p.nativeFlag != arg:
				return nil, conflict("conflicting model flags: " + p.nativeFlag + " and " + arg)
			case p.nativeFlag == "" && p.nativeModel:
				return nil, conflict(arg + " conflicts with --model — pick one")
			case p.nativeFlag == "":
				p.nativeFlag, p.nativeModel = arg, true
				p.args = append(p.args, "--model", strings.TrimPrefix(arg, "--"))
			}
		case requiredValue[arg]:
			valuePending = true
			p.args = append(p.args, arg)
		case variadicValue[arg]:
			variadicPending = true
			p.args = append(p.args, arg)
		case optionalValue[arg]:
			optvalPending = true
			p.args = append(p.args, arg)
		case arg == "--":
			passthrough = true
			p.args = append(p.args, arg)
		case arg == "--yolo":
			p.yolo = true
			p.args = append(p.args, "--dangerously-skip-permissions")
		case arg == "--dangerously-skip-permissions":
			p.yolo = true
			p.args = append(p.args, arg)
		case arg == "--local":
			p.local = true
		case strings.HasPrefix(arg, "--") && len(arg) > 2 && isAgent(arg[2:]):
			// Refuse conflicting model flags instead of last-wins: a model's
			// self-report is unreliable, so a silently wrong backend would not
			// be noticeable in-session.
			if p.agentFlag != "" && p.agentFlag != arg {
				return nil, conflict("conflicting model flags: " + p.agentFlag + " and " + arg)
			}
			p.agentFlag, p.agent = arg, arg[2:]
		default:
			// Unknown options reach Claude untouched, which rejects what it
			// does not know — never a silent run on the default model.
			p.args = append(p.args, arg)
		}
	}
	return p, nil
}

func (p *launchPlan) markResume(selector string) {
	p.resume = selector
	p.resumeAt = len(p.args) - 1
	p.resumeValueAt = -1
	p.resumes++
}

// pin points the resume selector at one session, so routing and Claude cannot
// pick different conversations: a bare -c lets Claude take the newest
// transcript in the folder, which need not be the one routing was chosen for.
func (p *launchPlan) pin(id string) {
	if p.resumeValueAt >= 0 {
		p.args[p.resumeAt], p.args[p.resumeValueAt] = "--resume", id
	} else {
		p.args[p.resumeAt] = "--resume=" + id
	}
}

// replay is what the resume-history hint must repeat: only flags the CALLER
// gave. A restore-derived agent flag would skip the restore on replay and come
// back on the agent's primary instead of the model the session ran on.
func (p *launchPlan) replay() string {
	var out []string
	if p.yolo {
		out = append(out, "--yolo")
	}
	if p.local {
		out = append(out, "--local")
	}
	if p.agentFlag != "" {
		out = append(out, p.agentFlag)
	}
	return strings.Join(out, " ")
}

func (a *App) launch(home string, args []string) error {
	replayTo := ""
	if len(args) >= 2 && args[0] == "--replay-to" {
		replayTo, args = args[1], args[2:]
	}
	if len(args) == 0 || args[0] != "--" {
		return usageError()
	}
	args = args[1:]
	native, err := a.nativeClaude(home)
	if err != nil {
		return err
	}
	// The table is read at most once, and only when an option could be a
	// foreign-model flag or a resume needs it: a plain `claude` never loads it.
	// An unreadable table makes no option a flag — Claude rejects them.
	var table *Table
	loadTable := func() *Table {
		if table == nil {
			if t, err := LoadTable(home, a.Executable); err == nil {
				table = t
			} else {
				table = &Table{}
			}
		}
		return table
	}
	p, err := parseLaunch(args, func(name string) bool { return loadTable().has(name) })
	if err != nil {
		return a.refuse(err)
	}
	// Inside a routed session (`cc-harness-agents exec <id> -- claude -c`, or
	// a claude started from one) the route is already chosen and inherited: a
	// restore would re-route it behind the caller's back. An explicit flag
	// still re-routes, like any explicit choice.
	_, routed := a.Env.Lookup(keyAgent)
	if p.resume != "" && p.agent == "" && !p.nativeModel && !routed && a.Env.Get("CC_ROUTER_RESTORE") != "0" {
		// Refuse a second selector rather than rewriting the last one's slot:
		// a trailing bare `--resume` would open the picker and land the user on
		// a different conversation than the routing was resolved from. Only
		// here, where a rewrite would happen; otherwise Claude's own rule holds.
		if p.resumes > 1 {
			return a.refuse(conflict("more than one resume selector (-c / --resume) — pick one"))
		}
		if err := a.restore(home, p); err != nil {
			return err
		}
	}
	if p.local && p.agent == "" {
		return a.refuse(conflict("--local requires one foreign-model flag"))
	}
	// A native --model outranks ANTHROPIC_MODEL, so it would quietly win over
	// the agent's tier — the same silent-wrong-tier trap.
	if p.agent != "" && p.nativeModel {
		flag := p.nativeFlag
		if flag == "" {
			flag = "--model"
		}
		return a.refuse(conflict("--" + p.agent + " conflicts with " + flag + " — pick one"))
	}
	if replayTo != "" {
		// Best effort: the hint is a convenience, the launch is not.
		os.WriteFile(replayTo, []byte(p.replay()), 0o600)
	}
	argv := append([]string{native}, p.args...)
	if p.agent == "" {
		return a.replace(argv, a.Env)
	}
	return a.routed(home, p.agent, p.local, argv)
}

func (a *App) refuse(err error) error {
	fmt.Fprintf(a.Stderr, "claude: %s\n", err)
	return &exitError{code: exitUnavailable}
}

// nativeClaude is the launcher this layer execs: CC_ROUTER_CLAUDE, or the
// native installer's ~/.local/bin/claude. A configured path and never a PATH
// search: a PATH entry must not decide which binary receives the routing
// secrets.
func (a *App) nativeClaude(home string) (string, error) {
	path := a.Env.Get("CC_ROUTER_CLAUDE")
	if path == "" {
		path = filepath.Join(home, ".local", "bin", "claude")
	} else if !filepath.IsAbs(path) {
		return "", fail(exitCapability, "CC_ROUTER_CLAUDE must be an absolute path")
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fail(exitNotFound, "no Claude Code launcher at %s — install Claude Code or set CC_ROUTER_CLAUDE", oneLine(path))
	}
	if self, err := filepath.EvalSymlinks(a.Executable); err == nil {
		if real == self {
			return "", fail(exitCapability, "%s is cc-router itself, not Claude Code — point CC_ROUTER_CLAUDE at the native launcher", oneLine(path))
		}
	}
	return path, nil
}

// restore re-enters the routing a resumed session last ran on. Explicit
// caller choices never reach here; this decides only what the session itself
// recorded, and on doubt changes nothing or falls back to the Anthropic
// default — it never guesses a foreign route.
func (a *App) restore(home string, p *launchPlan) error {
	r := a.resolveResume(p.resume)
	if r.status == resumeResolved && r.agent != "native" {
		// The table maps the model, but the gateway may have dropped it since.
		// That verdict alone falls back to the Anthropic default; every other
		// failure is the route's and stays an error naming its layer. The
		// local route has no catalog to ask.
		if !p.local {
			if err := a.servesModel(home, r.model); err != nil {
				var ee *exitError
				if !errors.As(err, &ee) || ee.code != exitNotServed {
					return err
				}
				fmt.Fprintf(a.Stderr, "%s: %s\n", progName, ee.msg)
				r.status = resumeUnroutable
			}
		}
	}
	if r.id != "" && r.agent != "native" && r.status != resumeUnknown {
		p.pin(r.id)
	}
	switch {
	case r.status == resumeResolved && r.agent != "native":
		p.agent = r.agent
		// The override path pins the exact model: exec exports it instead of
		// the row's primary, and it is inherited so a nested exec keeps it.
		a.Env.Set(overrideVar(r.agent), r.model)
		// Overrides any model Claude stored with the session as well.
		p.args = append([]string{"--model", r.model}, p.args...)
		fmt.Fprintf(a.Stderr, "claude: restoring %s via %s:%s\n", r.model, agentNamespace, r.agent)
	case r.status == resumeUnroutable:
		// The session DID run on a foreign model nothing routes any more.
		// Claude re-applies a session's stored model on resume, so leaving it
		// alone would send a foreign id to Anthropic. Why is already on stderr.
		p.args = append([]string{"--model", "default"}, p.args...)
		fmt.Fprintln(a.Stderr, "claude: could not restore this session's foreign model; continuing with normal Anthropic defaults")
	}
	// resumeUnknown: cannot tell (no session, the picker, nothing recorded) —
	// change NOTHING, or a plain resume would outrank the user's own model.
	return nil
}

var sessionIDRe = regexp.MustCompile(`^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`)

// Resume verdicts, numbered as cc-harness-resume's exit statuses.
const (
	resumeResolved   = 0
	resumeUnknown    = 1
	resumeUnroutable = 3
)

type resumeRoute struct {
	status           int
	id, agent, model string
}

// resolveResume answers which session a selector means and what it ran on.
func (a *App) resolveResume(selector string) resumeRoute {
	if selector == "picker" {
		// The conversation is unknown until after the pick.
		return resumeRoute{status: resumeUnknown}
	}
	return a.transcriptResume(selector)
}

// transcriptResume asks the resume bridge, which reads the session's last
// model out of Claude's own transcript. Its exit status is the verdict; its
// stderr is the user's and passes straight through. Missing, or an answer
// that does not hold its shape, is "cannot tell".
func (a *App) transcriptResume(selector string) resumeRoute {
	self, err := filepath.EvalSymlinks(a.Executable)
	if err != nil {
		return resumeRoute{status: resumeUnknown}
	}
	cmd := exec.Command(filepath.Join(filepath.Dir(self), "cc-harness-resume"), selector)
	cmd.Env = a.Env.Entries()
	cmd.Stderr = a.Stderr
	out, err := cmd.Output()
	var xe *exec.ExitError
	switch {
	case errors.As(err, &xe) && xe.ExitCode() == resumeUnroutable:
		return resumeRoute{status: resumeUnroutable}
	case err != nil:
		return resumeRoute{status: resumeUnknown}
	}
	f := strings.Split(strings.TrimSuffix(string(out), "\n"), "\t")
	if len(f) != 3 || !sessionIDRe.MatchString(f[0]) || !modelIDRe.MatchString(f[2]) ||
		(f[1] != "native" && !agentNameRe.MatchString(f[1])) {
		return resumeRoute{status: resumeUnknown}
	}
	return resumeRoute{status: resumeResolved, id: f[0], agent: f[1], model: f[2]}
}
