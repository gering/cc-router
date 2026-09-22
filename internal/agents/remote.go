package agents

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"golang.org/x/net/http/httpproxy"
)

const remoteMaxBodyBytes = 1048576

// accessPair is the Cloudflare Access service token.
type accessPair struct{ id, secret string }

// remoteRoute holds the resolved remote configuration. Its secrets live only
// in memory and in the exec environment — never in argv or a diagnostic.
type remoteRoute struct {
	baseURL string
	apiKey  string
	access  accessPair
}

// loadRemote resolves URL, profile and the profile's three secrets. Every
// failure is "capability absent" (exit 3) and names variables, never values.
func loadRemote(cfg *Config, env *Env) (*remoteRoute, error) {
	base, ok, err := cfg.Value(keyRemoteURL)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fail(exitCapability, "remote gateway not configured — set %s in the environment or %s", keyRemoteURL, cfg.path)
	}
	profile, err := cfg.Profile()
	if err != nil {
		return nil, err
	}
	r := &remoteRoute{baseURL: base}
	for _, s := range []struct {
		name string
		dst  *string
	}{
		{"CLIPROXY_API_KEY_" + profile, &r.apiKey},
		{"CLIPROXY_CF_ACCESS_" + profile + "_CLIENT_ID", &r.access.id},
		{"CLIPROXY_CF_ACCESS_" + profile + "_CLIENT_SECRET", &r.access.secret},
	} {
		v := env.Get(s.name)
		switch {
		case v == "":
			return nil, fail(exitCapability, "%s is empty", s.name)
		case hasControl(v):
			return nil, fail(exitCapability, "%s contains a control character", s.name)
		}
		*s.dst = v
	}
	return r, nil
}

// prober performs the one authenticated catalog request per invocation.
type prober struct {
	env            *Env
	rootCAs        *x509.CertPool // nil = the system trust store
	connectTimeout time.Duration
	totalTimeout   time.Duration
}

// probe returns the route's catalog; on failure the catalog is stale and the
// string is a classified, secret-free reason naming the failing layer.
func (p *prober) probe(r *remoteRoute) (*Catalog, string) {
	stale := &Catalog{State: catalogStale}
	req, err := http.NewRequest(http.MethodGet, r.baseURL+"/v1/models", nil)
	if err != nil {
		return stale, "remote network request failed"
	}
	// Assigned directly so the names go out exactly as spelled.
	req.Header = http.Header{
		headerAccessID:     {r.access.id},
		headerAccessSecret: {r.access.secret},
		"Authorization":    {"Bearer " + r.apiKey},
		"Accept":           {"*/*"},
		"User-Agent":       {progName},
	}
	resp, err := p.client().Do(req)
	if err != nil {
		if isTLSError(err) {
			return stale, "remote TLS validation failed"
		}
		return stale, "remote network request failed"
	}
	defer resp.Body.Close()

	switch code := resp.StatusCode; {
	case code == http.StatusOK:
	case code == http.StatusUnauthorized:
		return stale, "remote proxy API key rejected (HTTP 401)"
	case code == http.StatusForbidden:
		return stale, "Cloudflare Access rejected the client credentials (HTTP 403)"
	case code == http.StatusBadGateway || code == http.StatusServiceUnavailable:
		return stale, fmt.Sprintf("remote proxy origin unavailable (HTTP %d)", code)
	case code >= 300 && code < 400:
		return stale, fmt.Sprintf("remote endpoint returned a redirect (HTTP %d); redirects are disabled", code)
	default:
		return stale, fmt.Sprintf("remote model probe failed (HTTP %d)", code)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, remoteMaxBodyBytes+1))
	if err != nil {
		return stale, "remote network request failed"
	}
	if len(body) > remoteMaxBodyBytes {
		return stale, fmt.Sprintf("remote model probe response exceeds %d bytes", remoteMaxBodyBytes)
	}
	cat, err := parseCatalog(body)
	if err != nil {
		return stale, "remote model probe returned invalid JSON"
	}
	return cat, ""
}

// client refuses redirects, bounds every phase, sends no compression request
// (the body cap applies to what the server sends) and uses the proxy settings
// of the injected environment.
func (p *prober) client() *http.Client {
	env := p.env
	proxy := httpproxy.Config{
		HTTPSProxy: firstSet(env, "https_proxy", "HTTPS_PROXY", "all_proxy", "ALL_PROXY"),
		NoProxy:    firstSet(env, "no_proxy", "NO_PROXY"),
	}
	proxyFunc := proxy.ProxyFunc()
	transport := &http.Transport{
		Proxy:                  func(r *http.Request) (*url.URL, error) { return proxyFunc(r.URL) },
		DialContext:            (&net.Dialer{Timeout: p.connectTimeout}).DialContext,
		TLSClientConfig:        &tls.Config{RootCAs: p.rootCAs, MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    p.connectTimeout,
		DisableCompression:     true,
		MaxResponseHeaderBytes: 64 << 10,
	}
	return &http.Client{
		Transport:     transport,
		Timeout:       p.totalTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func firstSet(env *Env, keys ...string) string {
	for _, k := range keys {
		if v := env.Get(k); v != "" {
			return v
		}
	}
	return ""
}

func isTLSError(err error) bool {
	var (
		verify   *tls.CertificateVerificationError
		unknown  x509.UnknownAuthorityError
		hostname x509.HostnameError
		invalid  x509.CertificateInvalidError
		alert    tls.AlertError
		record   tls.RecordHeaderError
	)
	if errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return errors.As(err, &verify) || errors.As(err, &unknown) || errors.As(err, &hostname) ||
		errors.As(err, &invalid) || errors.As(err, &alert) || errors.As(err, &record)
}

var contextIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// parseCatalog validates only what it consumes: a data array of objects with a
// non-empty printable string id. Optional context_length metadata is kept per
// id only when it is a plain integer in range — anything else leaves that
// model's window UNKNOWN rather than wrong, and never fails the catalog.
func parseCatalog(body []byte) (*Catalog, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var top map[string]any
	if err := dec.Decode(&top); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data after the model list")
	}
	entries, ok := top["data"].([]any)
	if !ok {
		return nil, errors.New("data is not an array")
	}
	cat := &Catalog{State: catalogValid, Context: map[string]int{}}
	for _, e := range entries {
		obj, ok := e.(map[string]any)
		if !ok {
			return nil, errors.New("model entry is not an object")
		}
		id, ok := obj["id"].(string)
		if !ok || id == "" || !printable(id) {
			return nil, errors.New("invalid model id")
		}
		cat.IDs = append(cat.IDs, id)
		if n, ok := obj["context_length"].(json.Number); ok && contextIDRe.MatchString(id) {
			if _, dup := cat.Context[id]; !dup {
				if ctx, ok := catalogContext(n); ok {
					cat.Context[id] = ctx
				}
			}
		}
	}
	return cat, nil
}

// printable rejects C0 and C1 control characters and DEL.
func printable(s string) bool {
	for _, r := range s {
		if r < 32 || (r >= 127 && r <= 159) {
			return false
		}
	}
	return true
}

func catalogContext(n json.Number) (int, bool) {
	f, err := n.Float64()
	if err != nil || f != math.Floor(f) || f < catalogContextMin || f > catalogContextMax {
		return 0, false
	}
	return int(f), true
}
