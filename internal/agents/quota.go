package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"slices"
	"time"
)

// quotaWindows are the two bars a statusline renders, in display order.
var quotaWindows = []string{"five_hour", "seven_day"}

// quotaWindow is one rendered bar. ResetAt is epoch seconds, absent when the
// source names none (or names it in a shape this parser does not trust).
type quotaWindow struct {
	UsedPercentage float64 `json:"used_percentage"`
	ResetAt        *int64  `json:"reset_at,omitempty"`
}

// quota maps a window name (quotaWindows) to its bar; either may be absent.
type quota map[string]quotaWindow

// quotaSnapshot is what a refresh publishes: the quota and the time its source
// measured it, one document, so a single rename keeps the two consistent.
type quotaSnapshot struct {
	Quota       quota `json:"quota"`
	SourceEpoch int64 `json:"source_epoch"`
}

// Usage data is believed within this window around now: older is not shown,
// and a source ahead by more than the skew is not believed at all.
const (
	quotaMaxAge = 1800 * time.Second
	quotaSkew   = 300 * time.Second
)

func usableAge(source, now time.Time) bool {
	return !source.After(now.Add(quotaSkew)) && now.Sub(source) <= quotaMaxAge
}

// percent accepts a number in [0, 100]; nothing else renders as a bar.
func percent(v any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, err := n.Float64()
	return f, err == nil && f >= 0 && f <= 100
}

// epochSeconds accepts a whole number; a reset time in any other shape is
// dropped rather than failing the window it belongs to.
func epochSeconds(v any) *int64 {
	n, ok := v.(json.Number)
	if !ok {
		return nil
	}
	f, err := n.Float64()
	if err != nil || f != math.Floor(f) || math.Abs(f) > 1<<53 {
		return nil
	}
	i := int64(f)
	return &i
}

// stamp parses an RFC 3339 time (fraction optional, Z or a ±HH:MM offset) and
// the ±HHMM spelling some providers write.
func stamp(v any) (time.Time, bool) {
	s, ok := v.(string)
	if !ok {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999Z0700"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

var errUsageShape = errors.New("usage response has an unexpected shape")

// parseRemoteUsage reads the companion's /usage/v1/<provider> answer. The
// envelope, each account's provider, status and fetch time are what the
// aggregation consumes, so a wrong one fails the whole answer; within an
// account only the two rendered windows are read, each validated on its own,
// so a provider publishing a window this statusline never renders (Codex's
// per-tool limits, xAI's monthly period) cannot blank the bars. Each window
// takes the account with the highest usage: the conservative reading.
func parseRemoteUsage(body []byte, provider string, now time.Time) (quotaSnapshot, error) {
	var top map[string]any
	if err := decodeSingle(body, &top); err != nil {
		return quotaSnapshot{}, errUsageShape
	}
	accounts, ok := top["accounts"].([]any)
	if !ok {
		return quotaSnapshot{}, errUsageShape
	}
	type pick struct {
		w       quotaWindow
		fetched time.Time
	}
	best := map[string]pick{}
	usable, unusable := 0, 0
	for _, a := range accounts {
		acct, ok := a.(map[string]any)
		if !ok || acct["provider"] != provider {
			return quotaSnapshot{}, errUsageShape
		}
		fetched, ok := stamp(acct["fetched_at"])
		if !ok {
			return quotaSnapshot{}, errUsageShape
		}
		switch acct["status"] {
		case "error":
			continue
		case "ok":
		default:
			return quotaSnapshot{}, errUsageShape
		}
		limits, ok := acct["rate_limits"].(map[string]any)
		if !ok {
			return quotaSnapshot{}, errUsageShape
		}
		if !usableAge(fetched, now) {
			unusable++
			continue
		}
		usable++
		for _, key := range quotaWindows {
			win, ok := limits[key].(map[string]any)
			if !ok {
				continue
			}
			used, ok := percent(win["used_percentage"])
			if !ok {
				continue
			}
			if p, seen := best[key]; !seen || used > p.w.UsedPercentage {
				best[key] = pick{quotaWindow{used, epochSeconds(win["reset_at"])}, fetched}
			}
		}
	}
	switch {
	case unusable > 0 && usable == 0:
		return quotaSnapshot{}, errors.New("usage data is too old or from the future")
	case usable == 0:
		// Every account errored: an outage, not an empty quota.
		return quotaSnapshot{}, errors.New("no account reported usage")
	}
	snap := quotaSnapshot{Quota: quota{}, SourceEpoch: now.Unix()}
	for key, p := range best {
		snap.Quota[key] = p.w
		snap.SourceEpoch = min(snap.SourceEpoch, p.fetched.Unix())
	}
	return snap, nil
}

// usageSource is one provider's own usage API for the local route, reached
// with the credentials the local proxy keeps (<cred>-*.json, newest first)
// and then the provider CLI's own login. Adding a provider is one entry.
type usageSource struct {
	url    string
	native string // the provider CLI's login, relative to HOME
	header func(cred map[string]any) (http.Header, bool)
	parse  func(body []byte) (quota, error)
}

// localUsage is keyed by the table's cred column. kimi left with its row; its
// parser returns as one entry when a kimi row does.
var localUsage = map[string]usageSource{
	"codex": {
		url:    "https://chatgpt.com/backend-api/wham/usage",
		native: ".codex/auth.json",
		header: codexHeader,
		parse:  parseCodexUsage,
	},
	"xai": {
		url:    "https://cli-chat-proxy.grok.com/v1/billing?format=credits",
		native: ".grok/auth.json",
		header: grokHeader,
		parse:  parseGrokUsage,
	},
}

// credString is a non-empty value safe to put in a header.
func credString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok && s != "" && !hasControl(s)
}

func bearer(token string) http.Header {
	return http.Header{"Authorization": {"Bearer " + token}, "Accept": {"application/json"}, "User-Agent": {progName}}
}

// codexHeader takes the token and account from the credential's top level,
// the proxy's token_data or the codex CLI's tokens.
func codexHeader(cred map[string]any) (http.Header, bool) {
	for _, inner := range []map[string]any{cred, asObject(cred["token_data"]), asObject(cred["tokens"])} {
		token, okT := credString(inner["access_token"])
		account, okA := credString(inner["account_id"])
		if okT && okA {
			h := bearer(token)
			h.Set("ChatGPT-Account-ID", account)
			return h, true
		}
	}
	return nil, false
}

func asObject(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// grokHeader takes the proxy's access_token, else the first key of the grok
// CLI's per-issuer entries (in name order, so the choice is stable).
func grokHeader(cred map[string]any) (http.Header, bool) {
	if token, ok := credString(cred["access_token"]); ok {
		return bearer(token), true
	}
	names := make([]string, 0, len(cred))
	for name := range cred {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		entry, _ := cred[name].(map[string]any)
		if token, ok := credString(entry["key"]); ok {
			return bearer(token), true
		}
	}
	return nil, false
}

// parseCodexUsage maps each window to a bar by its length: a week or more is
// seven_day, five hours or less five_hour, anything between renders nowhere.
func parseCodexUsage(body []byte) (quota, error) {
	var top map[string]any
	if err := decodeSingle(body, &top); err != nil {
		return nil, errUsageShape
	}
	limits, ok := top["rate_limit"].(map[string]any)
	if !ok {
		return nil, errUsageShape
	}
	q := quota{}
	for _, key := range []string{"primary_window", "secondary_window"} {
		win, _ := limits[key].(map[string]any)
		secs, ok := win["limit_window_seconds"].(json.Number)
		if !ok {
			continue
		}
		n, err := secs.Float64()
		bar := ""
		switch {
		case err != nil || n <= 0:
		case n >= 6*24*3600:
			bar = "seven_day"
		case n <= 6*3600:
			bar = "five_hour"
		}
		used, ok := percent(win["used_percent"])
		if bar == "" || !ok {
			continue
		}
		q[bar] = quotaWindow{used, epochSeconds(win["reset_at"])}
	}
	return q, nil
}

// parseGrokUsage reads xAI's credit usage as the weekly bar, resetting at the
// end of the billing period.
func parseGrokUsage(body []byte) (quota, error) {
	var top map[string]any
	if err := decodeSingle(body, &top); err != nil {
		return nil, errUsageShape
	}
	cfg, _ := top["config"].(map[string]any)
	used, ok := percent(cfg["creditUsagePercent"])
	if !ok {
		return nil, errUsageShape
	}
	w := quotaWindow{UsedPercentage: used}
	end := cfg["billingPeriodEnd"]
	if end == nil {
		period, _ := cfg["currentPeriod"].(map[string]any)
		end = period["end"]
	}
	if t, ok := stamp(end); ok {
		at := t.Unix()
		w.ResetAt = &at
	}
	return quota{"seven_day": w}, nil
}

// Backoffs after a failed refresh. A rejected credential or an unserved
// provider waits the long one: CLIProxyAPI bans an address after five failed
// authentications in 30 minutes, and a retry a minute would reach that from
// the statusline alone.
const (
	quotaFailureBackoff = 60 * time.Second
	quotaLongBackoff    = quotaMaxAge
)

// statusBackoff is the wait after an HTTP answer other than 200: a rejected
// credential or an unserved provider does not heal within a minute.
func statusBackoff(code int) time.Duration {
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return quotaLongBackoff
	}
	return quotaFailureBackoff
}

// fetchRemoteUsage asks the usage API for provider's quota. Missing or
// malformed credentials stop it before any request: a keyless probe counts
// as a failed authentication too.
func (a *App) fetchRemoteUsage(home, provider string) (quotaSnapshot, time.Duration, error) {
	cfg, err := LoadConfig(a.Env, home)
	if err != nil {
		return quotaSnapshot{}, quotaFailureBackoff, err
	}
	r, err := loadRemote(cfg, a.Env)
	if err != nil {
		return quotaSnapshot{}, quotaFailureBackoff, err
	}
	base := r.baseURL
	if v, ok, err := cfg.Value(keyUsageURL); err != nil {
		return quotaSnapshot{}, quotaFailureBackoff, err
	} else if ok {
		base = v
	}
	body, code, note := a.prober().get(base+"/usage/v1/"+provider, r.header("application/json"), "remote", "remote usage request")
	switch {
	case note != "":
		return quotaSnapshot{}, quotaFailureBackoff, errors.New(note)
	case code != http.StatusOK:
		return quotaSnapshot{}, statusBackoff(code), errors.New(gatewayStatus("remote usage request", code))
	}
	snap, err := parseRemoteUsage(body, provider, a.Now())
	if err != nil {
		return quotaSnapshot{}, quotaFailureBackoff, fmt.Errorf("remote usage request: %w", err)
	}
	return snap, 0, nil
}

// fetchLocalUsage tries each credential in turn, so a broken newer login does
// not hide an older working one; a disabled proxy entry is skipped.
func (a *App) fetchLocalUsage(home, provider string) (quotaSnapshot, time.Duration, error) {
	src, ok := localUsage[provider]
	if !ok {
		return quotaSnapshot{}, quotaLongBackoff, fmt.Errorf("no local usage source for provider %s", provider)
	}
	cfg, err := LoadConfig(a.Env, home)
	if err != nil {
		return quotaSnapshot{}, quotaFailureBackoff, err
	}
	dir, err := cfg.Must(keyProxyDir)
	if err != nil {
		return quotaSnapshot{}, quotaFailureBackoff, err
	}
	paths := append((&localRoute{proxyDir: dir}).credentials(provider), filepath.Join(home, src.native))
	what := provider + " usage request"
	last := fmt.Sprintf("no usable %s credentials in %s or ~/%s", provider, dir, src.native)
	backoff := quotaFailureBackoff
	for _, path := range paths {
		data, err := readRegularFile(path)
		var cred map[string]any
		if err != nil || decodeSingle(data, &cred) != nil || cred["disabled"] == true {
			continue
		}
		header, ok := src.header(cred)
		if !ok {
			continue
		}
		body, code, note := a.prober().get(src.url, header, provider+" usage", what)
		switch {
		case note != "":
			last = note
			continue
		case code != http.StatusOK:
			last, backoff = fmt.Sprintf("%s failed (HTTP %d)", what, code), statusBackoff(code)
			continue
		}
		q, err := src.parse(body)
		if err != nil {
			last = fmt.Sprintf("%s: %v", what, err)
			continue
		}
		return quotaSnapshot{Quota: q, SourceEpoch: a.Now().Unix()}, 0, nil
	}
	return quotaSnapshot{}, backoff, errors.New(last)
}
