package agents

import (
	"fmt"
	"strings"
	"unicode"
)

// Exit codes. They describe failures BEFORE the exec; once the target runs,
// the process IS the target and its status comes back unchanged.
const (
	exitOK          = 0
	exitUnavailable = 1 // exec: agent not available; resolve-model: no profile
	exitUsage       = 2 // usage error, or exec: unknown agent
	exitCapability  = 3 // the selected route has no usable configuration
	// A failed exec reports like a shell would.
	exitCannotExecute = 126
	exitNotFound      = 127
)

const progName = "cc-harness-agents"

// exitError carries an exit status and an optional diagnostic up to Run,
// which prints it once with the program prefix.
type exitError struct {
	code int
	msg  string
	// usage requests the usage text on stderr instead of (or after) msg.
	usage bool
}

func (e *exitError) Error() string { return e.msg }

func fail(code int, format string, args ...any) error {
	return &exitError{code: code, msg: fmt.Sprintf(format, args...)}
}

func usageError() error { return &exitError{code: exitUsage, usage: true} }

// hasControl reports bytes a shell's [[:cntrl:]] matches in the C locale.
func hasControl(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0
}

// oneLine flattens what would otherwise add rows or columns to the TSV
// contract, or steer a terminal: a note can embed a credential FILENAME and a
// diagnostic an argument, so every control byte — not just CR, LF and TAB —
// and every invisible format character (bidi overrides, zero-width marks)
// becomes a space before either reaches stdout or stderr.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, s)
}

// isDigits reports a non-empty run of ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
