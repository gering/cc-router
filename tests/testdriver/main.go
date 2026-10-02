// Command testdriver is cc-harness-agents with ONE difference: it trusts the
// fixture CA named by CC_ROUTER_TEST_CA_FILE instead of the system store, so
// the contract suite can drive the remote route against a local HTTPS fixture
// with certificate verification fully on. It is never built into bin/ or
// installed.
package main

import (
	"crypto/x509"
	"fmt"
	"os"

	"github.com/gering/cc-router/internal/agents"
)

func main() {
	pem, err := os.ReadFile(os.Getenv("CC_ROUTER_TEST_CA_FILE"))
	pool := x509.NewCertPool()
	if err != nil || !pool.AppendCertsFromPEM(pem) {
		fmt.Fprintln(os.Stderr, "testdriver: CC_ROUTER_TEST_CA_FILE must name a PEM CA certificate")
		os.Exit(90)
	}
	os.Exit(agents.Main(pool))
}
