package agents

import (
	"strings"
	"testing"
)

func TestNormalizeCustomHeaders(t *testing.T) {
	access := &accessPair{id: "id-secret", secret: "secret-value"}
	cases := []struct {
		name, in string
		access   *accessPair
		want     string
	}{
		{"remote empty", "", access, "CF-Access-Client-Id: id-secret\nCF-Access-Client-Secret: secret-value"},
		{"local empty", "", nil, ""},
		{"crlf and trailing newline", "X-A: 1\r\nX-B: 2\n", nil, "X-A: 1\nX-B: 2"},
		{"inherited access dropped", "cf-access-client-id: old\nX-A: 1\nCF-ACCESS-CLIENT-SECRET: old", nil, "X-A: 1"},
		{"remote replaces access", "CF-Access-Client-Id: old\nX-A: 1", access, "X-A: 1\nCF-Access-Client-Id: id-secret\nCF-Access-Client-Secret: secret-value"},
	}
	for _, c := range cases {
		got, err := normalizeCustomHeaders(c.in, c.access)
		if err != nil || got != c.want {
			t.Errorf("%s: %q, %v", c.name, got, err)
		}
	}

	for name, in := range map[string]string{
		"bare cr":       "X-A: 1\rX-B: 2",
		"empty line":    "X-A: 1\n\nX-B: 2",
		"only newline":  "\n",
		"control":       "X-A: 1\tbad",
		"no colon":      "No-Colon",
		"empty name":    ": value",
		"bad name":      "X A: 1",
		"authorization": "Authorization: inherited-secret",
		"proxy auth":    "proxy-authorization: inherited-secret",
		"api key":       "X-Api-Key: inherited-secret",
		"duplicate":     "X-Test: one\nx-test: two",
	} {
		_, err := normalizeCustomHeaders(in, access)
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), "inherited-secret") || strings.Contains(err.Error(), "secret-value") {
			t.Errorf("%s: diagnostic leaks a value: %v", name, err)
		}
	}
}
