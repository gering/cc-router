package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// launch is the executable path of `claude`: the shim and the interactive
// wrapper both hand it Claude's argv, and it replaces itself with the native
// launcher — routed when --proxy, --local or a resumed session asks for
// it, untouched otherwise. Parsing, conflicts and the resume restore live
// here exactly once, so a caller that never sources a shell function gets the
// same routing as a terminal.

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
	// The pre-`--proxy` native shorthands: `--opus` IS `--model opus`, no
	// more. Deprecated, but they keep that meaning for the transition, so a
	// habit never silently moves a session onto the gateway — even where a
	// row (opus) shares the name.
	nativeShorthands = setOf("--fable", "--opus", "--sonnet", "--haiku")
	// Options that settle the model another way: an agent definition, the
	// caller's own settings sources, or a session they resume.
	modelSources = setOf("--agent", "--settings", "--setting-sources", "--from-pr", "--teleport")
)

func setOf(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}

// launchPlan is a parsed command line: Claude's argv with this layer's own
// flags consumed or expanded, plus what they selected. Matching is by exact
// argument only: prompt text is never rewritten.
type launchPlan struct {
	args []string
	yolo bool
	// The caller's route choice: `--proxy <want>` or `--local <want>`, where
	// want is an agent or a model id; routeFlag is how the caller spelled it,
	// for conflicts. `--noproxy` is the explicit native choice.
	want      string
	routeFlag string
	proxy     bool
	local     bool
	noproxy   bool
	// Spellings from before --proxy (`--sol`, a bare `--local`): still
	// honoured, and named in a deprecation notice.
	deprecated []string
	// A recorded local route: routed like --local, never replayed as one.
	restoredLocal bool
	agent         string // the resolved agent, from the caller's choice or a restore
	nativeModel   bool   // --model or a deprecated native shorthand
	nativeFlag    string // the shorthand, for conflicts and its notice
	ownModel      bool   // a modelSources option
	// Start on the account default (`--model default`): what Claude would
	// otherwise pick is a foreign id it would send to Anthropic.
	accountDefault bool
	// The resume selector — its kind, --resume's value, and the indices of
	// its option and value in args (-1 when absent). A kind, not a sentinel
	// string: `--resume continue` names a session, it is not -c.
	resumeKind    selectorKind
	resume        string
	resumeAt      int
	resumeValueAt int
	resumes       int
}

type selectorKind int

const (
	noSelector selectorKind = iota
	continueSelector
	pickerSelector
	valueSelector
)

// conflict is a refused command line; launch reports it as `claude: …`.
type conflict string

func (c conflict) Error() string { return string(c) }

// parseLaunch splits argv into Claude's argv and this layer's selections.
// isAgent answers whether `--<name>` is a row of the table; routes whether a
// word names an agent or a model the table resolves.
func parseLaunch(argv []string, isAgent, routes func(string) bool) (*launchPlan, error) {
	p := &launchPlan{resumeAt: -1, resumeValueAt: -1}
	passthrough, valuePending, optvalPending, optvalIsResume, variadicPending := false, false, false, false, false
	routePending := "" // --proxy or --local, waiting for its value
	for _, arg := range argv {
		// After a bare `--` every token is verbatim, so a literal `--proxy`
		// reaches the CLI instead of being consumed here.
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
		// A route's value is never option-shaped: an agent or a model id. The
		// word after a --local is its value only when it names one: otherwise
		// it is the deprecated bare modifier, and `--sol --local "fix it"`
		// keeps its prompt.
		if routePending != "" {
			flag := routePending
			routePending = ""
			if !strings.HasPrefix(arg, "-") && (flag == "--proxy" || routes(arg)) {
				if err := p.choose(flag, arg, flag+" "+arg); err != nil {
					return nil, err
				}
				continue
			}
			if err := p.bareRoute(flag); err != nil {
				return nil, err
			}
		}
		// An OPTIONAL or VARIADIC value ends at the next option token.
		if (optvalPending || variadicPending) && !strings.HasPrefix(arg, "-") {
			p.args = append(p.args, arg)
			if optvalIsResume {
				p.resumeKind, p.resume = valueSelector, arg
				p.resumeValueAt = len(p.args) - 1
				optvalIsResume = false
			}
			optvalPending = false
			continue
		}
		optvalPending, variadicPending, optvalIsResume = false, false, false
		if name, _, _ := strings.Cut(arg, "="); modelSources[name] {
			p.ownModel = true
		}
		switch {
		case arg == "-c" || arg == "--continue":
			p.args = append(p.args, arg)
			p.markResume(continueSelector, "")
		case arg == "-r" || arg == "--resume":
			p.args = append(p.args, arg)
			p.markResume(pickerSelector, "")
			optvalPending, optvalIsResume = true, true
		case strings.HasPrefix(arg, "--resume="):
			p.args = append(p.args, arg)
			p.markResume(valueSelector, strings.TrimPrefix(arg, "--resume="))
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
		case arg == "--proxy" || arg == "--local":
			routePending = arg
		case strings.HasPrefix(arg, "--proxy=") || strings.HasPrefix(arg, "--local="):
			flag, want, _ := strings.Cut(arg, "=")
			if err := p.choose(flag, want, flag+" "+want); err != nil {
				return nil, err
			}
		case arg == "--noproxy":
			p.noproxy = true
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
		case strings.HasPrefix(arg, "--") && len(arg) > 2 && isAgent(arg[2:]):
			// The pre-`--proxy` spelling of an agent: honoured for the
			// transition, routed by whichever route the line names.
			p.deprecated = append(p.deprecated, arg)
			if err := p.choose("", arg[2:], arg); err != nil {
				return nil, err
			}
		default:
			// Unknown options reach Claude untouched, which rejects what it
			// does not know — never a silent run on the default model.
			p.args = append(p.args, arg)
		}
	}
	if routePending != "" {
		if err := p.bareRoute(routePending); err != nil {
			return nil, err
		}
	}
	switch {
	case p.proxy && p.local:
		return nil, conflict("--proxy conflicts with --local — pick one route")
	case p.noproxy && (p.want != "" || p.local):
		flag := p.routeFlag
		if flag == "" {
			flag = "--local"
		}
		return nil, conflict("--noproxy conflicts with " + flag + " — pick one")
	}
	return p, nil
}

// choose records the caller's agent or model. flag is the route it came
// with ("" for a deprecated `--<agent>`, which takes the line's route).
// Conflicting choices are refused instead of last-wins: a model's
// self-report is unreliable, so a silently wrong backend would not be
// noticeable in-session.
func (p *launchPlan) choose(flag, want, spelled string) error {
	if want == "" {
		return conflict(flag + " needs an agent or model id")
	}
	// The value is argv: every message that names it gets it on one line,
	// without control bytes for the caller's terminal.
	spelled = oneLine(spelled)
	if p.want != "" && p.want != want {
		return conflict("conflicting model flags: " + p.routeFlag + " and " + spelled)
	}
	p.want, p.routeFlag = want, spelled
	p.proxy = p.proxy || flag == "--proxy"
	p.local = p.local || flag == "--local"
	return nil
}

// bareRoute is --proxy or --local without a value. A bare --local is the
// pre-`--proxy` modifier (`--local --sol`), honoured for the transition.
func (p *launchPlan) bareRoute(flag string) error {
	if flag == "--proxy" {
		return conflict("--proxy needs an agent or model id")
	}
	p.local = true
	p.deprecated = append(p.deprecated, flag)
	return nil
}

func (p *launchPlan) markResume(kind selectorKind, value string) {
	p.resumeKind, p.resume = kind, value
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
	// One word per flag and in today's spelling, whatever the caller typed:
	// the hint keeps only option-shaped words.
	switch {
	case p.want != "" && p.local:
		out = append(out, "--local="+p.want)
	case p.want != "":
		out = append(out, "--proxy="+p.want)
	case p.noproxy:
		out = append(out, "--noproxy")
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
	// The table is read at most once, and only when a route value or an
	// option could name an agent, a resume needs it, or a settings file names
	// a default model: a plain `claude` without one never loads it. An unreadable table knows no agent — a deprecated `--<agent>`
	// reaches Claude, which rejects it, and a route value is refused.
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
	p, err := parseLaunch(args, func(name string) bool { return loadTable().has(name) }, func(want string) bool {
		_, agent := loadTable().Find(want)
		_, _, err := loadTable().ResolveModel(want)
		return agent || err == nil
	})
	if err != nil {
		return a.refuse(err)
	}
	if len(p.deprecated) > 0 {
		use := "--local <agent>"
		switch {
		case p.want != "" && p.local:
			use = "--local " + p.want
		case p.want != "":
			use = "--proxy " + p.want
		}
		fmt.Fprintf(a.Stderr, "claude: %s is deprecated — use %s\n", strings.Join(p.deprecated, " "), oneLine(use))
	}
	if p.nativeFlag != "" {
		alias := strings.TrimPrefix(p.nativeFlag, "--")
		gateway := ""
		if loadTable().has(alias) {
			gateway = ", or --proxy " + alias + " for the gateway"
		}
		fmt.Fprintf(a.Stderr, "claude: %s is deprecated — use --model %s%s\n", p.nativeFlag, alias, gateway)
	}
	// Inside a routed session (`cc-harness-agents exec <id> -- claude -c`, or
	// a claude started from one) the route is already chosen and inherited: a
	// restore would re-route it behind the caller's back. An explicit choice
	// still re-routes, like any explicit choice.
	_, routed := a.Env.Lookup(keyAgent)
	if p.resumeKind != noSelector && p.want == "" && !p.noproxy && !p.nativeModel && !routed && a.Env.Get("CC_ROUTER_RESTORE") != "0" {
		// Refuse a second selector rather than rewriting the last one's slot:
		// a trailing bare `--resume` would open the picker and land the user on
		// a different conversation than the routing was resolved from. Only
		// here, where a rewrite would happen; otherwise Claude's own rule holds.
		if p.resumes > 1 {
			return a.refuse(conflict("more than one resume selector (-c / --resume) — pick one"))
		}
		if err := a.restore(home, p, loadTable); err != nil {
			return err
		}
	}
	if p.local && p.want == "" && p.agent == "" {
		return a.refuse(conflict("--local needs an agent or model id: --local <agent|model>"))
	}
	// A native --model outranks ANTHROPIC_MODEL, so it would quietly win over
	// the agent's tier — the same silent-wrong-tier trap.
	if p.want != "" && p.nativeModel {
		flag := p.nativeFlag
		if flag == "" {
			flag = "--model"
		}
		return a.refuse(conflict(p.routeFlag + " conflicts with " + flag + " — pick one"))
	}
	if p.want != "" {
		// An agent first; otherwise the model id names the agent that serves
		// it and is pinned, exactly as a restore pins a recorded model.
		t := loadTable()
		if _, ok := t.Find(p.want); ok {
			p.agent = p.want
		} else if agent, model, err := t.ResolveModel(p.want); err == nil {
			p.agent = agent
			a.pinModel(p, agent, model)
		} else {
			return a.refuse(conflict(fmt.Sprintf("%s: no such agent, and %v — see: %s list", p.routeFlag, err, progName)))
		}
	}
	env := a.Env
	if p.noproxy {
		// A session that ran routed stored its foreign model, and Claude
		// re-applies it on resume: it would reach Anthropic as a foreign id.
		p.accountDefault = p.resumeKind != noSelector && !p.nativeModel
		// Inside a routed session the recipe is inherited; native is without it.
		if routed {
			env = unrouted(a.Env)
		}
	}
	// Claude saves an in-session `/model` as the default for new sessions,
	// so one switch inside a routed session leaves a foreign id behind that
	// a native start would send to Anthropic. A resume restores its own
	// model, and `--noproxy` pins the default for it; only a start that
	// would use the setting is guarded. Inside a routed session the route
	// stays chosen unless --noproxy dropped it.
	if p.agent == "" && !p.nativeModel && !p.ownModel && (!routed || p.noproxy) &&
		(p.resumeKind == noSelector || p.noproxy) {
		if file, model := a.foreignDefault(home, env, loadTable); file != "" {
			p.accountDefault = true
			fmt.Fprintf(a.Stderr, "claude: \"model\" in %s is %s, which only the gateway serves — starting on the Anthropic default (remove it there, or use --proxy %[2]s)\n",
				oneLine(file), oneLine(model))
		}
	}
	if p.accountDefault {
		p.args = append([]string{"--model", "default"}, p.args...)
	}
	if replayTo != "" {
		// Best effort: the hint is a convenience, the launch is not.
		os.WriteFile(replayTo, []byte(p.replay()), 0o600)
	}
	argv := append([]string{native}, p.args...)
	if p.agent == "" {
		return a.replace(argv, env)
	}
	return a.routed(home, p.agent, p.local || p.restoredLocal, argv)
}

// foreignDefault names the settings file whose "model" Claude would start
// on, and that model, when the start goes to Anthropic and the table routes
// the id: one only the gateway serves. The files and their order are
// Claude's (2.1.304, observed where the docs are silent), highest first: the
// project's local settings — in a git checkout only the main checkout's root
// holds them, also for linked worktrees and subdirectories — then the
// current directory's shared settings, then the user's (CLAUDE_CONFIG_DIR or
// ~/.claude). Claude skips a file it cannot read or parse, and so does this.
// ANTHROPIC_MODEL outranks every "model" key, and ANTHROPIC_BASE_URL points
// the start away from Anthropic: either one, non-empty in the environment or
// in the settings' merged "env", means no guard. Keys are matched exactly,
// as Claude matches them; the settings are the user's and never written here.
func (a *App) foreignDefault(home string, env *Env, table func() *Table) (file, model string) {
	user := filepath.Join(home, ".claude")
	if dir := env.Get("CLAUDE_CONFIG_DIR"); dir != "" {
		user = dir
	}
	files := []string{filepath.Join(user, "settings.json")}
	if cwd, err := os.Getwd(); err == nil {
		if real, err := filepath.EvalSymlinks(cwd); err == nil {
			cwd = real
		}
		files = append([]string{
			filepath.Join(localSettingsRoot(cwd), ".claude", "settings.local.json"),
			filepath.Join(cwd, ".claude", "settings.json"),
		}, files...)
	}
	overrides := map[string]string{}
	for _, key := range []string{"ANTHROPIC_MODEL", "ANTHROPIC_BASE_URL"} {
		if v := env.Get(key); v != "" {
			overrides[key] = v
		}
	}
	for _, f := range files {
		var keys, fileEnv map[string]json.RawMessage
		if data, err := readSmall(f); err != nil || json.Unmarshal(data, &keys) != nil {
			continue
		}
		json.Unmarshal(keys["env"], &fileEnv)
		// A higher file's value wins, an empty one included.
		for _, key := range []string{"ANTHROPIC_MODEL", "ANTHROPIC_BASE_URL"} {
			var v string
			if _, set := overrides[key]; !set && json.Unmarshal(fileEnv[key], &v) == nil {
				overrides[key] = v
			}
		}
		var m string
		if file == "" && json.Unmarshal(keys["model"], &m) == nil && m != "" {
			file, model = f, m
		}
	}
	if file == "" || overrides["ANTHROPIC_MODEL"] != "" || overrides["ANTHROPIC_BASE_URL"] != "" || nativeID(model) {
		return "", ""
	}
	if _, _, err := table().ResolveModel(model); err != nil {
		return "", ""
	}
	return file, model
}

// localSettingsRoot is where Claude looks for .claude/settings.local.json:
// the main checkout of the git repository dir is in — found through a
// linked worktree's .git file and its commondir — or dir itself outside one.
func localSettingsRoot(dir string) string {
	for d := dir; ; d = filepath.Dir(d) {
		dotgit := filepath.Join(d, ".git")
		if fi, err := os.Stat(dotgit); err == nil && fi.IsDir() {
			return d
		}
		if data, err := readSmall(dotgit); err == nil {
			gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: ")
			if !ok {
				return d
			}
			if !filepath.IsAbs(gitdir) {
				gitdir = filepath.Join(d, gitdir)
			}
			common, err := readSmall(filepath.Join(gitdir, "commondir"))
			if err != nil {
				return d // a submodule: its own checkout
			}
			c := strings.TrimSpace(string(common))
			if !filepath.IsAbs(c) {
				c = filepath.Join(gitdir, c)
			}
			return filepath.Dir(c)
		}
		if filepath.Dir(d) == d {
			return dir
		}
	}
}

// readSmall reads a regular file of at most 1 MiB. A checkout can plant a
// FIFO or a device where settings belong; opening one could block the launch
// or read without end.
func readSmall(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	const limit = 1 << 20
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && len(data) > limit {
		err = fmt.Errorf("%s: larger than %d bytes", path, limit)
	}
	return data, err
}

// pinModel routes the exact model, not the agent's primary: exec exports the
// override instead, and it is inherited so a nested exec keeps it. The
// `--model` also outranks any model Claude stored with a resumed session.
func (a *App) pinModel(p *launchPlan, agent, model string) {
	a.Env.Set(overrideVar(agent), model)
	p.args = append([]string{"--model", model}, p.args...)
}

func (a *App) refuse(err error) error {
	fmt.Fprintf(a.Stderr, "claude: %s\n", err)
	return &exitError{code: exitUnavailable}
}

// nativeClaude is the launcher this layer execs: CC_ROUTER_CLAUDE, or the
// native installer's ~/.local/bin/claude. A configured path and never a PATH
// search — the shim IS `claude` on PATH, so a name lookup would find itself,
// and a PATH entry must not decide which binary receives the routing secrets.
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
		shim, _ := filepath.EvalSymlinks(filepath.Join(filepath.Dir(self), "shim", "claude"))
		if real == self || real == shim {
			return "", fail(exitCapability, "%s is cc-router itself, not Claude Code — point CC_ROUTER_CLAUDE at the native launcher", oneLine(path))
		}
	}
	return path, nil
}

// restore re-enters the routing a resumed session last ran on. Explicit
// caller choices never reach here; this decides only what the session itself
// recorded, and on doubt changes nothing or falls back to the Anthropic
// default — it never guesses a foreign route. A recorded model the gateway
// dropped is no doubt but a choice the caller has to make: it stops there.
func (a *App) restore(home string, p *launchPlan, table func() *Table) error {
	r := a.resolveResume(home, p, table)
	if r.status == resumeResolved && r.agent != "native" {
		// An explicit --local outranks the recorded route; a recorded local
		// route stays local and meets its own readiness gate in exec.
		p.restoredLocal = r.route == "local"
		// The table maps the model, but the gateway may have dropped it since.
		// Neither way forward is the session's own, so the resume stops and
		// names both; every other failure is the route's and keeps its layer.
		// The local route has no catalog to ask.
		if !p.local && !p.restoredLocal {
			if err := a.servesModel(home, r.model); err != nil {
				var ee *exitError
				if !errors.As(err, &ee) || ee.code != exitNotServed {
					return err
				}
				return a.refuse(retiredModel(r))
			}
		}
	}
	if r.id != "" && r.agent != "native" && r.status != resumeUnknown {
		p.pin(r.id)
	}
	switch {
	case r.status == resumeResolved && r.agent != "native":
		p.agent = r.agent
		a.pinModel(p, r.agent, r.model)
		fmt.Fprintf(a.Stderr, "claude: restoring %s via %s:%s\n", r.model, agentNamespace, r.agent)
	case r.status == resumeUnroutable:
		// The session DID run on a foreign model nothing routes any more.
		// Claude re-applies a session's stored model on resume, so leaving it
		// alone would send a foreign id to Anthropic. Why is already on stderr.
		p.accountDefault = true
		fmt.Fprintln(a.Stderr, "claude: could not restore this session's foreign model; continuing with normal Anthropic defaults")
	}
	// resumeUnknown: cannot tell (no session, the picker, nothing recorded) —
	// change NOTHING, or a plain resume would outrank the user's own model.
	return nil
}

// retiredModel refuses a resume whose recorded model the gateway no longer
// serves, naming the two explicit choices that bypass the restore. Both name
// the resolved session: an explicit choice leaves -c to Claude's own pick,
// which may be another conversation than the one refused here.
func retiredModel(r resumeRoute) conflict {
	return conflict(fmt.Sprintf("this session ran on %s via %s:%s, which the gateway no longer serves — "+
		"continue on %[3]s's current model with `claude --resume %[4]s --proxy %[3]s`, or on Anthropic with `claude --resume %[4]s --noproxy`",
		oneLine(r.model), agentNamespace, r.agent, r.id))
}

// Resume verdicts, numbered as cc-harness-resume's exit statuses.
const (
	resumeResolved   = 0
	resumeUnknown    = 1
	resumeUnroutable = 3
)

type resumeRoute struct {
	status                  int
	id, agent, model, route string
}

// resolveResume answers which session a selector means and what it ran on:
// the session's sidecar first, Claude's own transcript (cc-harness-resume)
// for a session that has none.
func (a *App) resolveResume(home string, p *launchPlan, table func() *Table) resumeRoute {
	look := sessionLookup{dir: sessionDir(a.Env, home), stderr: a.Stderr, now: a.Now()}
	var rec sessionRecord
	found := false
	switch {
	case p.resumeKind == pickerSelector:
		// The conversation is unknown until after the pick.
		return resumeRoute{status: resumeUnknown}
	case p.resumeKind == continueSelector:
		if cwd, err := os.Getwd(); err == nil {
			if real, err := filepath.EvalSymlinks(cwd); err == nil {
				cwd = real
			}
			rec, found = look.latestIn(cwd)
		}
		// A native record is no answer for -c: Claude's own pick may be a newer
		// transcript the cache never saw (a `claude -p` has no statusline), and
		// the transcript reader below asks Claude's files directly.
		found = found && !rec.native()
		if !found {
			return a.transcriptResume("continue")
		}
	case sessionIDRe.MatchString(p.resume):
		if rec, found = look.byID(p.resume); !found {
			return a.transcriptResume(p.resume)
		}
	default:
		// A search term: the conversation is unknown until Claude matches it.
		fmt.Fprintln(a.Stderr, "claude: search selector; model restore skipped (use --resume <UUID> or an explicit model flag)")
		return resumeRoute{status: resumeUnknown}
	}
	if rec.native() {
		return resumeRoute{status: resumeResolved, id: rec.SessionID, agent: "native", model: rec.Model}
	}
	r := resumeRoute{status: resumeResolved, id: rec.SessionID, model: rec.Model, route: rec.Route}
	// The recorded agent first; a session recorded before the marker existed,
	// or whose row was renamed since, goes through the table's own mapping.
	// An Anthropic id is never mapped onto an agent: only the record's own
	// marker restores one, so a session on a renamed row resumes natively.
	if t := table(); t.has(rec.Agent) {
		r.agent = rec.Agent
	} else if nativeID(rec.Model) {
		r.agent = "native"
	} else if agent, model, err := t.ResolveModel(rec.Model); err == nil {
		r.agent, r.model = agent, model
	} else {
		fmt.Fprintf(a.Stderr, "claude: %s\n", err)
		r.status = resumeUnroutable
	}
	return r
}

// native: a session no exec ever routed, on a Claude model. Claude restores
// those itself.
func (r sessionRecord) native() bool {
	return r.Agent == "" && r.Route == "" && nativeID(r.Model)
}

// nativeID: an Anthropic model id. Claude runs those natively, so a resume
// never infers a route from one — cc-harness-resume holds the same rule.
func nativeID(model string) bool { return strings.HasPrefix(model, "claude-") }

// transcriptResume asks the interim bridge, which reads the session's last
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
