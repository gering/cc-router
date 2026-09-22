// Package agents implements cc-harness-agents: run the Claude Code harness
// against a foreign model through CLIProxyAPI. It is the single owner of the
// agent table, the route probes and the routing environment; callers choose
// an agent and, optionally, the route.
//
// The CLI (list, exec, resolve-model) is an external contract — see
// README.md. Remote is the default route; --local selects the machine-local
// gateway and is never an automatic fallback.
package agents

import (
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

const usageText = `usage: cc-harness-agents list [--local] [--header|--no-header]
       cc-harness-agents exec [--local] <name> [--] <argv…>
       cc-harness-agents resolve-model <model-id>
`

// App is one invocation with every side effect injectable.
type App struct {
	Env         *Env
	Stdout      io.Writer
	Stderr      io.Writer
	StdoutIsTTY bool
	// Executable is the running binary; share/models.tsv is located next to
	// its real (symlink-resolved) path.
	Executable string
	Now        func() time.Time
	// RootCAs replaces the system trust store; nil in production.
	RootCAs        *x509.CertPool
	ConnectTimeout time.Duration
	TotalTimeout   time.Duration
	DialTimeout    time.Duration
	// Exec replaces the process. It returns only on failure.
	Exec func(path string, argv, env []string) error
}

// Run executes args (without the program name) and returns the exit status.
// On a successful exec it never returns.
func (a *App) Run(args []string) int {
	err := a.run(args)
	if err == nil {
		return exitOK
	}
	var ee *exitError
	if !errors.As(err, &ee) {
		ee = &exitError{code: exitUnavailable, msg: err.Error()}
	}
	if ee.msg != "" {
		fmt.Fprintf(a.Stderr, "%s: %s\n", progName, ee.msg)
	}
	if ee.usage {
		io.WriteString(a.Stderr, usageText)
	}
	return ee.code
}

func (a *App) run(args []string) error {
	// Checked before dispatch so a missing HOME is "capability absent", not a
	// crash, whatever the subcommand.
	home := a.Env.Get("HOME")
	if home == "" {
		return fail(exitCapability, "HOME is not set — cannot locate the credential dir")
	}
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "list":
		return a.list(home, args[1:])
	case "exec":
		return a.exec(home, args[1:])
	case "resolve-model":
		return a.resolveModel(home, args[1:])
	case "-h", "--help", "help":
		io.WriteString(a.Stdout, usageText)
		return nil
	}
	return usageError()
}

func (a *App) resolveModel(home string, args []string) error {
	if len(args) == 0 || args[0] == "" {
		return usageError()
	}
	table, err := LoadTable(home, a.Executable)
	if err != nil {
		return err
	}
	agent, model, err := table.ResolveModel(args[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "%s\t%s\n", agent, model)
	return nil
}

// invocation is what list and exec share once the arguments are parsed.
type invocation struct {
	table *Table
	cfg   *Config
}

func (a *App) prepare(home string) (*invocation, error) {
	table, err := LoadTable(home, a.Executable)
	if err != nil {
		return nil, err
	}
	cfg, err := LoadConfig(a.Env, home)
	if err != nil {
		return nil, err
	}
	return &invocation{table: table, cfg: cfg}, nil
}

func (a *App) prober() *prober {
	return &prober{env: a.Env, rootCAs: a.RootCAs, connectTimeout: a.ConnectTimeout, totalTimeout: a.TotalTimeout}
}

func (a *App) noToken(table *Table, l *localRoute) error {
	return fail(exitCapability, "no usable token in %s — is cliproxyapi set up? (brew install cliproxyapi && cliproxyapi %s)",
		l.tokenFile(), table.logins())
}

// listRow is one line of the four-column contract.
type listRow struct {
	name, model, available, note string
}

// list prints one row per agent: raw TSV to a pipe, a padded table to a
// terminal; --header/--no-header force either shape. Exit 0 means the probe
// ran, whatever each agent's availability.
func (a *App) list(home string, args []string) error {
	local, header := false, ""
	for _, arg := range args {
		switch arg {
		case "--local":
			local = true
		case "--header":
			header = "yes"
		case "--no-header":
			header = "no"
		default:
			return usageError()
		}
	}
	inv, err := a.prepare(home)
	if err != nil {
		return err
	}
	var rows []listRow
	if local {
		rows, err = a.listLocal(inv)
	} else {
		rows, err = a.listRemote(inv)
	}
	if err != nil {
		return err
	}
	if header == "yes" || header == "" && a.StdoutIsTTY {
		renderTable(a.Stdout, rows)
	} else {
		for _, r := range rows {
			fmt.Fprintf(a.Stdout, "%s\t%s\t%s\t%s\n", r.name, r.model, r.available, r.note)
		}
	}
	return nil
}

// row resolves one table row against a catalog and pairs it with the route's
// availability verdict. Resolution comes first, so what a row reports is the
// model exec would export.
func (a *App) row(r Row, cat *Catalog, verdict func(Row) (bool, string)) listRow {
	sel := (&selector{env: a.Env, cat: cat}).Resolve(r)
	eff := sel.Apply(r)
	ok, note := verdict(eff)
	available := "no"
	if ok {
		available = "yes"
	}
	return listRow{
		name:      agentNamespace + ":" + r.Name,
		model:     eff.Model,
		available: available,
		note:      oneLine(mergeNotes(oneLine(note), sel.Note)),
	}
}

func (a *App) listRemote(inv *invocation) ([]listRow, error) {
	route, err := loadRemote(inv.cfg, a.Env)
	if err != nil {
		return nil, err
	}
	cat, probeNote := a.prober().probe(route)
	var rows []listRow
	for _, r := range inv.table.Rows {
		rows = append(rows, a.row(r, cat, func(eff Row) (bool, string) {
			if cat.State != catalogValid {
				return false, probeNote
			}
			if missing := cat.missing(eff); len(missing) > 0 {
				return false, "remote models missing: " + strings.Join(missing, ", ")
			}
			return true, ""
		}))
	}
	return rows, nil
}

func (a *App) listLocal(inv *invocation) ([]listRow, error) {
	l, err := loadLocal(inv.cfg)
	if err != nil {
		return nil, err
	}
	none := &Catalog{State: catalogNone}
	now := a.Now()
	// A closed fallback keeps list's contract (exit 0) and is reported per row.
	blocked := l.blocked(now)
	if blocked == "" && l.token() == "" {
		return nil, a.noToken(inv.table, l)
	}
	down := ""
	if blocked == "" && !l.live(a.DialTimeout) {
		down = l.downNote()
	}
	// One credential probe per provider: sol/terra/luna share codex, and a
	// login rewriting the file mid-listing must not split their verdicts.
	type result struct {
		ok   bool
		note string
	}
	cache := map[string]result{}
	var rows []listRow
	for _, r := range inv.table.Rows {
		rows = append(rows, a.row(r, none, func(eff Row) (bool, string) {
			switch {
			case blocked != "":
				return false, blocked
			case down != "":
				return false, down
			}
			res, seen := cache[eff.Cred]
			if !seen {
				res.ok, res.note = l.checkCreds(eff, now)
				cache[eff.Cred] = res
			}
			return res.ok, res.note
		}))
	}
	return rows, nil
}

// renderTable pads the first three columns to the widest value; the free-form
// note stays last and unpadded.
func renderTable(w io.Writer, rows []listRow) {
	all := append([]listRow{{"name", "model", "available", "note"}}, rows...)
	widths := [3]int{}
	for _, r := range all {
		for i, v := range []string{r.name, r.model, r.available} {
			widths[i] = max(widths[i], utf8.RuneCountInString(v))
		}
	}
	pad := func(s string, n int) string { return s + strings.Repeat(" ", n-utf8.RuneCountInString(s)) }
	for _, r := range all {
		fmt.Fprintf(w, "%s  %s  %s  %s\n", pad(r.name, widths[0]), pad(r.model, widths[1]), pad(r.available, widths[2]), r.note)
	}
}
