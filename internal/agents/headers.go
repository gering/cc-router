package agents

import (
	"regexp"
	"strings"
)

const (
	headerAccessID     = "CF-Access-Client-Id"
	headerAccessSecret = "CF-Access-Client-Secret"
)

var headerNameRe = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

// normalizeCustomHeaders validates the inherited ANTHROPIC_CUSTOM_HEADERS,
// keeps valid headers in order and drops every inherited Access header. The
// remote route then appends the selected profile's Access pair; the local
// route leaves them absent, so a child launched from a remote session cannot
// forward those secrets to the loopback proxy. Authentication headers never
// belong in this channel. Diagnostics never include a header value.
func normalizeCustomHeaders(inherited string, access *accessPair) (string, error) {
	var out []string
	if inherited != "" {
		normalized := strings.ReplaceAll(inherited, "\r\n", "\n")
		if strings.Contains(normalized, "\r") {
			return "", headerError("contains a bare carriage return")
		}
		// splitLines tolerates one trailing newline, like a shell `read` loop.
		lines := splitLines(normalized)
		seen := map[string]bool{}
		for _, line := range lines {
			switch {
			case line == "":
				return "", headerError("contains an empty header line")
			case hasControl(line):
				return "", headerError("contains a control character")
			case !strings.Contains(line, ":"):
				return "", headerError("contains a malformed header")
			}
			name, _, _ := strings.Cut(line, ":")
			if !headerNameRe.MatchString(name) {
				return "", headerError("contains an invalid header name")
			}
			switch lower := strings.ToLower(name); lower {
			case "authorization", "proxy-authorization", "x-api-key":
				return "", headerError("must not contain authentication headers")
			case strings.ToLower(headerAccessID), strings.ToLower(headerAccessSecret):
				continue
			default:
				if seen[lower] {
					return "", headerError("contains duplicate header names")
				}
				seen[lower] = true
			}
			out = append(out, line)
		}
	}
	if access != nil {
		out = append(out, headerAccessID+": "+access.id, headerAccessSecret+": "+access.secret)
	}
	return strings.Join(out, "\n"), nil
}

func headerError(reason string) error {
	return fail(exitUnavailable, "ANTHROPIC_CUSTOM_HEADERS %s", reason)
}
