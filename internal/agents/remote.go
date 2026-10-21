package agents

import (
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

// header is the gateway's two per-machine credentials, built in memory and set
// on the request — never handed to a subprocess. Assigned directly so the
// names go out exactly as spelled.
func (r *remoteRoute) header(accept string) http.Header {
	return http.Header{
		headerAccessID:     {r.access.id},
		headerAccessSecret: {r.access.secret},
		"Authorization":    {"Bearer " + r.apiKey},
		"Accept":           {accept},
		"User-Agent":       {progName},
	}
}

// probe returns the route's catalog; on failure the catalog is stale and the
// string is a classified, secret-free reason naming the failing layer.
func (p *prober) probe(r *remoteRoute) (*Catalog, string) {
	stale := &Catalog{State: catalogStale}
	body, code, note := p.get(r.baseURL+"/v1/models", r.header("*/*"), "remote", "remote model probe")
	switch {
	case note != "":
		return stale, note
	case code != http.StatusOK:
		return stale, gatewayStatus("remote model probe", code)
	}
	cat, err := parseCatalog(body)
	if err != nil {
		return stale, "remote model probe returned invalid JSON"
	}
	return cat, ""
}

// gatewayStatus names the layer behind a gateway answer other than 200.
func gatewayStatus(what string, code int) string {
	switch {
	case code == http.StatusUnauthorized:
		return "remote proxy API key rejected (HTTP 401)"
	case code == http.StatusForbidden:
		return "Cloudflare Access rejected the client credentials (HTTP 403)"
	case code == http.StatusBadGateway || code == http.StatusServiceUnavailable:
		return fmt.Sprintf("remote proxy origin unavailable (HTTP %d)", code)
	case code >= 300 && code < 400:
		return fmt.Sprintf("remote endpoint returned a redirect (HTTP %d); redirects are disabled", code)
	}
	return fmt.Sprintf("%s failed (HTTP %d)", what, code)
}

// get performs one bounded GET. A transport failure or an oversized body is a
// note naming layer (TLS vs. network) or what; any other answer returns its
// status code, and the body only with 200.
func (p *prober) get(url string, header http.Header, layer, what string) (body []byte, code int, note string) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, layer + " network request failed"
	}
	req.Header = header
	resp, err := p.client().Do(req)
	if err != nil {
		if isTLSError(err) {
			return nil, 0, layer + " TLS validation failed"
		}
		return nil, 0, layer + " network request failed"
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, ""
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, remoteMaxBodyBytes+1))
	if err != nil {
		return nil, 0, layer + " network request failed"
	}
	if len(body) > remoteMaxBodyBytes {
		return nil, 0, fmt.Sprintf("%s response exceeds %d bytes", what, remoteMaxBodyBytes)
	}
	return body, http.StatusOK, ""
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
	var top map[string]any
	if err := decodeSingle(body, &top); err != nil {
		return nil, err
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
