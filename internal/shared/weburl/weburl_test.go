package weburl_test

import (
	"strings"
	"testing"

	"github.com/cieons/koko/internal/shared/weburl"
)

// A Web URL is stored lowercased, without the default port 443 or a lone
// trailing "/", whatever host form it names.
func TestParseTakesAnHTTPSOrigin(t *testing.T) {
	for s, want := range map[string]string{
		"https://koko.example.com":            "https://koko.example.com",
		"HTTPS://Koko.Example.COM":            "https://koko.example.com",
		"https://koko.example.com/":           "https://koko.example.com",
		"https://koko.example.com:443":        "https://koko.example.com",
		"https://koko.example.com:8443":       "https://koko.example.com:8443",
		"https://koko.example.com:8443/":      "https://koko.example.com:8443",
		"https://203.0.113.7":                 "https://203.0.113.7",
		"https://[2001:DB8::1]:443":           "https://[2001:db8::1]",
		"https://[2001:db8::1]:8443":          "https://[2001:db8::1]:8443",
		"https://localhost:4430":              "https://localhost:4430",
		"https://xn--bcher-kva.example:44300": "https://xn--bcher-kva.example:44300",
	} {
		got, err := weburl.Parse(s)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %q, %v; want %q", s, got, err, want)
		}
		if again, err := weburl.Parse(got); err != nil || again != got {
			t.Errorf("Parse(%q), as stored, = %q, %v; want it unchanged", got, again, err)
		}
	}
}

// Anything but an https:// origin is refused, saying why.
func TestParseRefusesWhatIsNotAnOrigin(t *testing.T) {
	for _, tc := range []struct{ s, says string }{
		{"", "empty"},
		{"koko.example.com", "https://"},
		{"http://koko.example.com", "https://"},
		{"ftp://koko.example.com", "https://"},
		{"https://", "host"},
		{"https:///", "host"},
		{"https://koko.example.com/koko", "path"},
		{"https://koko.example.com//", "path"},
		{"https://koko.example.com?a=b", "query"},
		{"https://koko.example.com/?a=b", "query"},
		{"https://koko.example.com#top", "fragment"},
		{"https://alice@koko.example.com", "user information"},
		{"https://koko.example.com:0", "port"},
		{"https://koko.example.com:65536", "port"},
		{"https://koko.example.com:https", "port"},
		{"https://koko_1.example.com", "'_'"},
		{"https://2001:db8::1", "brackets"},
		{"https://[fe80::1%25eth0]", "IPv6"},
		{" https://koko.example.com", "https://"},
	} {
		got, err := weburl.Parse(tc.s)
		if err == nil || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("Parse(%q) = %q, %v; want an error that says %q", tc.s, got, err, tc.says)
		}
	}
}

func TestAuthorityIsTheHostHeader(t *testing.T) {
	for u, want := range map[string]string{
		"https://koko.example.com":      "koko.example.com",
		"https://koko.example.com:8443": "koko.example.com:8443",
		"https://[2001:db8::1]":         "[2001:db8::1]",
	} {
		if got := weburl.Authority(u); got != want {
			t.Errorf("Authority(%q) = %q, want %q", u, got, want)
		}
	}
}
