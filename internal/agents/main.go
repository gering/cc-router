package agents

import (
	"crypto/x509"
	"os"
	"syscall"
	"time"

	"golang.org/x/term"
)

// Main runs the program against the real process. rootCAs is nil in
// production (system trust store); only the test driver passes a pool.
func Main(rootCAs *x509.CertPool) int {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	app := &App{
		Env:            NewEnv(os.Environ()),
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
		StdoutIsTTY:    term.IsTerminal(int(os.Stdout.Fd())),
		Executable:     exe,
		Now:            time.Now,
		RootCAs:        rootCAs,
		ConnectTimeout: 3 * time.Second,
		TotalTimeout:   8 * time.Second,
		DialTimeout:    3 * time.Second,
		Exec:           syscall.Exec,
	}
	return app.Run(os.Args[1:])
}
