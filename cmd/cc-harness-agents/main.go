// Command cc-harness-agents routes the Claude Code harness to a foreign model
// through CLIProxyAPI. See README.md for the contract.
package main

import (
	"os"

	"github.com/gering/cc-router/internal/agents"
)

func main() { os.Exit(agents.Main(nil)) }
