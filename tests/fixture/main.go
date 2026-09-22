// Command fixture is the contract suite's stand-in gateway. It serves the
// model catalog over HTTPS on loopback and answers each request from files the
// suite writes per case, so the helper's real HTTP/TLS code runs unmodified:
//
//	GET /c/<case>/v1/models  ->  <root>/<case>/fixture/{status,body,location}
//
// and records every request (count, headers) under the same directory. Ports
// and the CA are published in <root>/fixture.env. It runs until stdin closes.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/gering/cc-router/internal/testfixture"
)

var caseRe = regexp.MustCompile(`^/c/([A-Za-z0-9._]+)/v1/models$`)

func main() {
	root := flag.String("root", "", "suite temp directory")
	flag.Parse()
	if *root == "" {
		log.Fatal("fixture: -root is required")
	}
	trusted, err := testfixture.NewCA("cc-router fixture CA")
	check(err)
	untrusted, err := testfixture.NewCA("untrusted CA")
	check(err)
	caFile := filepath.Join(*root, "fixture-ca.pem")
	check(os.WriteFile(caFile, trusted.PEM, 0o600))

	var mu sync.Mutex
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := caseRe.FindStringSubmatch(r.URL.Path)
		if m == nil {
			http.NotFound(w, r)
			return
		}
		dir := filepath.Join(*root, m[1], "fixture")
		mu.Lock()
		defer mu.Unlock()
		check(os.MkdirAll(dir, 0o700))
		count, _ := strconv.Atoi(strings.TrimSpace(read(dir, "count", "0")))
		check(os.WriteFile(filepath.Join(dir, "count"), []byte(strconv.Itoa(count+1)), 0o600))
		var lines []string
		for name, values := range r.Header {
			for _, v := range values {
				lines = append(lines, name+": "+v)
			}
		}
		sort.Strings(lines)
		lines = append([]string{r.Method + " " + r.URL.Path}, lines...)
		check(os.WriteFile(filepath.Join(dir, "request"), []byte(strings.Join(lines, "\n")+"\n"), 0o600))
		if loc := read(dir, "location", ""); loc != "" {
			w.Header().Set("Location", loc)
		}
		status, _ := strconv.Atoi(strings.TrimSpace(read(dir, "status", "200")))
		w.WriteHeader(status)
		io.WriteString(w, read(dir, "body", `{"data":[]}`))
	})

	serve := func(ca *testfixture.CA) int {
		l, err := testfixture.Loopback()
		check(err)
		cfg, err := ca.ServerConfig("127.0.0.1", "localhost")
		check(err)
		srv := &http.Server{Handler: handler, TLSConfig: cfg, ErrorLog: log.New(io.Discard, "", 0)}
		go srv.ServeTLS(l, "", "")
		return testfixture.Port(l)
	}
	accepting := func() int {
		l, err := testfixture.Loopback()
		check(err)
		go testfixture.AcceptAndClose(l)
		return testfixture.Port(l)
	}
	closed, err := testfixture.ClosedPort()
	check(err)

	env := fmt.Sprintf("FIXTURE_CA=%s\nFIXTURE_TRUSTED_PORT=%d\nFIXTURE_UNTRUSTED_PORT=%d\nFIXTURE_RESET_PORT=%d\nFIXTURE_LIVE_PORT=%d\nFIXTURE_CLOSED_PORT=%d\n",
		caFile, serve(trusted), serve(untrusted), accepting(), accepting(), closed)
	tmp := filepath.Join(*root, "fixture.env.tmp")
	check(os.WriteFile(tmp, []byte(env), 0o600))
	check(os.Rename(tmp, filepath.Join(*root, "fixture.env")))
	io.Copy(io.Discard, os.Stdin)
}

func read(dir, name, def string) string {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return def
	}
	return string(data)
}

func check(err error) {
	if err != nil {
		log.Fatal("fixture: ", err)
	}
}
