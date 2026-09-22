package agents

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gering/cc-router/internal/testfixture"
)

// gateway is an HTTPS test server whose certificate chains to ca.
func gateway(t *testing.T, ca *testfixture.CA, hosts []string, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(h)
	cfg, err := ca.ServerConfig(hosts...)
	if err != nil {
		t.Fatal(err)
	}
	srv.TLS = cfg
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func newCA(t *testing.T) *testfixture.CA {
	t.Helper()
	ca, err := testfixture.NewCA("test CA")
	if err != nil {
		t.Fatal(err)
	}
	return ca
}

func testProber(ca *testfixture.CA, env ...string) *prober {
	return &prober{env: NewEnv(env), rootCAs: ca.Pool(), connectTimeout: time.Second, totalTimeout: 2 * time.Second}
}

var testRoute = remoteRoute{apiKey: "api-secret", access: accessPair{id: "access-id", secret: "access-secret"}}

func route(url string) *remoteRoute {
	r := testRoute
	r.baseURL = url
	return &r
}

func TestProbeSendsExactlyTheAuthHeaders(t *testing.T) {
	ca := newCA(t)
	var got http.Header
	var path string
	srv := gateway(t, ca, []string{"127.0.0.1"}, func(w http.ResponseWriter, r *http.Request) {
		got, path = r.Header.Clone(), r.URL.Path
		io.WriteString(w, `{"data":[{"id":"m1","context_length":300000.0},{"id":"m2","context_length":"x"}]}`)
	})
	cat, note := testProber(ca).probe(route(srv.URL + "/prefix"))
	if note != "" || cat.State != catalogValid || strings.Join(cat.IDs, ",") != "m1,m2" || cat.Context["m1"] != 300000 {
		t.Fatalf("probe = %+v %q", cat, note)
	}
	if _, ok := cat.Context["m2"]; ok {
		t.Fatal("non-numeric context_length believed")
	}
	if path != "/prefix/v1/models" {
		t.Fatalf("path = %s", path)
	}
	want := map[string]string{
		"Authorization": "Bearer api-secret", "Cf-Access-Client-Id": "access-id",
		"Cf-Access-Client-Secret": "access-secret", "User-Agent": progName,
	}
	for k, v := range want {
		if got.Get(k) != v {
			t.Errorf("header %s = %q", k, got.Get(k))
		}
	}
	// No compression negotiation: the body cap applies to what is sent.
	if got.Get("Accept-Encoding") != "" {
		t.Errorf("Accept-Encoding = %q", got.Get("Accept-Encoding"))
	}
}

func TestProbeClassifiesFailures(t *testing.T) {
	ca := newCA(t)
	respond := func(status int, body string, header ...string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			for i := 0; i+1 < len(header); i += 2 {
				w.Header().Set(header[i], header[i+1])
			}
			w.WriteHeader(status)
			io.WriteString(w, body)
		}
	}
	cases := []struct {
		name string
		h    http.HandlerFunc
		want string
	}{
		{"401", respond(401, ""), "remote proxy API key rejected (HTTP 401)"},
		{"403", respond(403, ""), "Cloudflare Access rejected the client credentials (HTTP 403)"},
		{"502", respond(502, ""), "remote proxy origin unavailable (HTTP 502)"},
		{"503", respond(503, ""), "remote proxy origin unavailable (HTTP 503)"},
		{"redirect", respond(302, "", "Location", "/elsewhere"), "remote endpoint returned a redirect (HTTP 302); redirects are disabled"},
		{"500", respond(500, ""), "remote model probe failed (HTTP 500)"},
		{"not json", respond(200, "not-json"), "remote model probe returned invalid JSON"},
		{"data not array", respond(200, `{"data":"nope"}`), "remote model probe returned invalid JSON"},
		{"null", respond(200, `null`), "remote model probe returned invalid JSON"},
		{"trailing", respond(200, `{"data":[]} {}`), "remote model probe returned invalid JSON"},
		{"control id", respond(200, `{"data":[{"id":"a\u0085b"}]}`), "remote model probe returned invalid JSON"},
		{"empty id", respond(200, `{"data":[{"id":""}]}`), "remote model probe returned invalid JSON"},
		{"too large", respond(200, `{"data":[],"pad":"`+strings.Repeat("x", remoteMaxBodyBytes)+`"}`), "remote model probe response exceeds 1048576 bytes"},
		{"too large chunked", func(w http.ResponseWriter, r *http.Request) {
			for range 3 {
				io.WriteString(w, strings.Repeat("x", remoteMaxBodyBytes/2))
				w.(http.Flusher).Flush()
			}
		}, "remote model probe response exceeds 1048576 bytes"},
	}
	for _, c := range cases {
		requests := 0
		srv := gateway(t, ca, []string{"127.0.0.1"}, func(w http.ResponseWriter, r *http.Request) { requests++; c.h(w, r) })
		cat, note := testProber(ca).probe(route(srv.URL))
		if cat.State != catalogStale || note != c.want || requests != 1 {
			t.Errorf("%s: state %v note %q requests %d", c.name, cat.State, note, requests)
		}
	}
}

func TestProbeTransportFailures(t *testing.T) {
	ca, other := newCA(t), newCA(t)
	requests := 0
	count := func(w http.ResponseWriter, r *http.Request) { requests++ }

	// Untrusted CA and a certificate for another name: TLS fails before any
	// request — the secrets never reach the peer.
	untrusted := gateway(t, other, []string{"127.0.0.1"}, count)
	wrongName := gateway(t, ca, []string{"gw.example"}, count)
	for _, url := range []string{untrusted.URL, wrongName.URL} {
		if _, note := testProber(ca).probe(route(url)); note != "remote TLS validation failed" {
			t.Errorf("%s: %q", url, note)
		}
	}
	if requests != 0 {
		t.Fatalf("a request reached an unverified peer")
	}

	// A peer that drops the connection, and one that never answers.
	l, err := testfixture.Loopback()
	if err != nil {
		t.Fatal(err)
	}
	go testfixture.AcceptAndClose(l)
	defer l.Close()
	hang := gateway(t, ca, []string{"127.0.0.1"}, func(w http.ResponseWriter, r *http.Request) { time.Sleep(3 * time.Second) })
	p := testProber(ca)
	p.totalTimeout = 300 * time.Millisecond
	for _, url := range []string{fmt.Sprintf("https://127.0.0.1:%d", testfixture.Port(l)), hang.URL} {
		start := time.Now()
		if _, note := p.probe(route(url)); note != "remote network request failed" {
			t.Errorf("%s: %q", url, note)
		}
		if time.Since(start) > 2*time.Second {
			t.Errorf("%s: deadline not enforced", url)
		}
	}
}

// The remote probe honours the environment's proxy settings the way curl
// does: https_proxy before HTTPS_PROXY, ALL_PROXY as the fallback, NO_PROXY
// exempting hosts.
func TestProbeProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://gw.example/v1/models", nil)
	cases := []struct {
		env  []string
		want string
	}{
		{nil, ""},
		{[]string{"https_proxy=http://lower:1", "HTTPS_PROXY=http://upper:1"}, "http://lower:1"},
		{[]string{"HTTPS_PROXY=http://upper:1"}, "http://upper:1"},
		{[]string{"ALL_PROXY=socks5://all:1"}, "socks5://all:1"},
		{[]string{"HTTPS_PROXY=http://upper:1", "NO_PROXY=gw.example"}, ""},
		{[]string{"HTTPS_PROXY=http://upper:1", "no_proxy=.example"}, ""},
		{[]string{"HTTP_PROXY=http://plain:1"}, ""}, // an https request never uses HTTP_PROXY
	}
	for _, c := range cases {
		p := &prober{env: NewEnv(c.env)}
		u, err := p.client().Transport.(*http.Transport).Proxy(req)
		got := ""
		if u != nil {
			got = u.String()
		}
		if err != nil || got != c.want {
			t.Errorf("%v: proxy %q, %v; want %q", c.env, got, err, c.want)
		}
	}
}
