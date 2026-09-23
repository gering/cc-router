package agents

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// providerSelectors sit ABOVE the auth token in Claude Code's auth
// precedence: an inherited one would bypass the proxy entirely, so every
// documented selector is shadowed (exported empty) on both routes.
var providerSelectors = []string{
	"CLAUDE_CODE_USE_BEDROCK",
	"CLAUDE_CODE_USE_VERTEX",
	"CLAUDE_CODE_USE_FOUNDRY",
	"CLAUDE_CODE_USE_MANTLE",
	"CLAUDE_CODE_USE_ANTHROPIC_AWS",
	"CLAUDE_CODE_USE_GATEWAY",
	"CLAUDE_CODE_USE_ANTHROPIC_GOOGLE_CLOUD",
}

// routing is the route-specific half of the environment recipe.
type routing struct {
	route   string // CLIPROXY_ROUTE: the statusline keys its caches on it
	baseURL string
	token   string
	headers string
	noProxy string // local only: set as both NO_PROXY and no_proxy
}

// routedEnv is the one environment recipe: inherited environment, the route's
// endpoint and credentials, shadowed provider selectors, and the effective
// row's models and ceiling.
func routedEnv(base *Env, rt routing, eff Row) *Env {
	env := base.Clone()
	set := [][2]string{
		{"ANTHROPIC_BASE_URL", rt.baseURL},
		{"ANTHROPIC_AUTH_TOKEN", rt.token},
		{"ANTHROPIC_CUSTOM_HEADERS", rt.headers},
		{"CLIPROXY_ROUTE", rt.route},
	}
	if rt.noProxy != "" {
		set = append(set, [2]string{"NO_PROXY", rt.noProxy}, [2]string{"no_proxy", rt.noProxy})
	}
	for _, s := range providerSelectors {
		set = append(set, [2]string{s, ""})
	}
	set = append(set,
		[2]string{"ANTHROPIC_MODEL", eff.Model},
		[2]string{"CLAUDE_CODE_SUBAGENT_MODEL", eff.Model},
		[2]string{"ANTHROPIC_DEFAULT_FABLE_MODEL", eff.Fable},
		[2]string{"ANTHROPIC_DEFAULT_OPUS_MODEL", eff.Opus},
		[2]string{"ANTHROPIC_DEFAULT_SONNET_MODEL", eff.Sonnet},
		[2]string{"ANTHROPIC_DEFAULT_HAIKU_MODEL", eff.Haiku},
		// Honoured only for non-claude-* model ids, so native runs are untouched.
		[2]string{"CLAUDE_CODE_MAX_CONTEXT_TOKENS", strconv.Itoa(eff.MaxCtx)},
	)
	for _, kv := range set {
		env.Set(kv[0], kv[1])
	}
	return env
}

// localNoProxy exempts the loopback gateway from an inherited HTTP proxy —
// otherwise the token and every prompt would leave the machine. Both casings'
// existing entries are merged, since libraries read either.
func localNoProxy(env *Env, host string) string {
	list := host + ",localhost"
	upper, lower := env.Get("NO_PROXY"), env.Get("no_proxy")
	if upper != "" {
		list += "," + upper
	}
	if lower != "" && lower != upper {
		list += "," + lower
	}
	return list
}

// exec routes <name> and replaces the process with argv, so the caller BECOMES
// the target (herdr's detection keys on the pane's root process). argv is
// deliberately unconstrained, which makes exec secret-bearing: the target
// receives the route's key and headers.
func (a *App) exec(home string, args []string) error {
	local := len(args) > 0 && args[0] == "--local"
	if local {
		args = args[1:]
	}
	want := ""
	if len(args) > 0 {
		want, args = args[0], args[1:]
	}
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if want == "" || len(args) == 0 {
		return usageError()
	}
	inv, err := a.prepare(home)
	if err != nil {
		return err
	}
	row, ok := inv.table.Find(want)
	if !ok {
		// oneLine: the name is argv, and a diagnostic must not carry control
		// bytes into the caller's terminal or log.
		return fail(exitUsage, "unknown agent '%s' — see: %s list", oneLine(strings.TrimPrefix(want, agentNamespace+":")), progName)
	}
	var rt routing
	var eff Row
	if local {
		rt, eff, err = a.routeLocal(inv, row)
	} else {
		rt, eff, err = a.routeRemote(inv, row)
	}
	if err != nil {
		return err
	}
	return a.replace(args, routedEnv(a.Env, rt, eff))
}

func (a *App) routeRemote(inv *invocation, row Row) (routing, Row, error) {
	route, err := loadRemote(inv.cfg, a.Env)
	if err != nil {
		return routing{}, Row{}, err
	}
	headers, err := normalizeCustomHeaders(a.Env.Get("ANTHROPIC_CUSTOM_HEADERS"), &route.access)
	if err != nil {
		return routing{}, Row{}, err
	}
	cat, note := a.prober().probe(route)
	if cat.State != catalogValid {
		return routing{}, Row{}, fail(exitUnavailable, "%s unavailable — %s", row.Name, note)
	}
	eff := a.selectAndReport(row, cat)
	if missing := cat.missing(eff); len(missing) > 0 {
		return routing{}, Row{}, fail(exitUnavailable, "%s unavailable — remote models missing: %s", row.Name, strings.Join(missing, ", "))
	}
	return routing{route: "remote", baseURL: route.baseURL, token: route.apiKey, headers: headers}, eff, nil
}

func (a *App) routeLocal(inv *invocation, row Row) (routing, Row, error) {
	l, err := loadLocal(inv.cfg)
	if err != nil {
		return routing{}, Row{}, err
	}
	now := a.Now()
	if blocked := l.blocked(now); blocked != "" {
		return routing{}, Row{}, fail(exitCapability, "%s", blocked)
	}
	headers, err := normalizeCustomHeaders(a.Env.Get("ANTHROPIC_CUSTOM_HEADERS"), nil)
	if err != nil {
		return routing{}, Row{}, err
	}
	token := l.token()
	if token == "" {
		return routing{}, Row{}, a.noToken(inv.table, l)
	}
	if !l.live(a.DialTimeout) {
		return routing{}, Row{}, fail(exitUnavailable, "%s", l.downNote())
	}
	// No catalog here: this applies an explicit override only, never discovery.
	eff := a.selectAndReport(row, &Catalog{State: catalogNone})
	if ok, note := l.checkCreds(eff, now); !ok {
		return routing{}, Row{}, fail(exitUnavailable, "%s unavailable — %s", row.Name, note)
	}
	return routing{route: "local", baseURL: l.baseURL(), token: token, headers: headers,
		noProxy: localNoProxy(a.Env, l.host)}, eff, nil
}

// selectAndReport resolves the row and names any deviation on stderr.
func (a *App) selectAndReport(row Row, cat *Catalog) Row {
	sel := (&selector{env: a.Env, cat: cat}).Resolve(row)
	if sel.Note != "" {
		a.Stderr.Write([]byte(progName + ": " + sel.Note + "\n"))
	}
	return sel.Apply(row)
}

// replace execs argv[0] from PATH (or as a path when it contains a slash)
// with the routed environment. It returns only when the exec failed, with
// the shell's 127 (not found) / 126 (not executable) statuses.
func (a *App) replace(argv []string, env *Env) error {
	path, err := lookPath(argv[0], a.Env.Get("PATH"))
	if errors.Is(err, os.ErrPermission) {
		return fail(exitCannotExecute, "cannot execute %s: permission denied", oneLine(argv[0]))
	}
	if err != nil {
		return fail(exitNotFound, "%s: command not found", oneLine(argv[0]))
	}
	err = a.Exec(path, argv, env.Entries())
	if errors.Is(err, syscall.ENOENT) {
		return fail(exitNotFound, "%s: command not found", oneLine(argv[0]))
	}
	return fail(exitCannotExecute, "cannot execute %s: %v", oneLine(argv[0]), err)
}

// lookPath mirrors a shell's command lookup with one deliberate difference: a
// name with a slash is used as is; otherwise the first executable regular file
// on PATH, but a relative entry — "" or "." — is SKIPPED, not searched. A
// shell would run ./claude there, and this exec hands the target the routing
// credentials, so the current directory must never decide which binary gets
// them (what os/exec.LookPath reports as ErrDot). Like the shell, a name found
// only as a non-executable file is "permission denied" (126), not "not found".
func lookPath(file, path string) (string, error) {
	if strings.Contains(file, "/") {
		return file, nil
	}
	notFound := os.ErrNotExist
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			continue
		}
		candidate := dir + "/" + file
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if info.Mode()&0o111 != 0 {
			return candidate, nil
		}
		notFound = os.ErrPermission
	}
	return "", notFound
}
