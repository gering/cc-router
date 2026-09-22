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
		return fail(exitUsage, "unknown agent '%s' — see: %s list", strings.TrimPrefix(want, agentNamespace+":"), progName)
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
	if err != nil {
		return fail(127, "%s: command not found", argv[0])
	}
	err = a.Exec(path, argv, env.Entries())
	if errors.Is(err, syscall.ENOENT) {
		return fail(127, "%s: command not found", argv[0])
	}
	return fail(126, "cannot execute %s: %v", argv[0], err)
}

// lookPath mirrors a shell's command lookup: a name with a slash is used as
// is; otherwise the first executable regular file on PATH (an empty entry is
// the current directory).
func lookPath(file, path string) (string, error) {
	if strings.Contains(file, "/") {
		return file, nil
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			dir = "."
		}
		candidate := dir + "/" + file
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", os.ErrNotExist
}
