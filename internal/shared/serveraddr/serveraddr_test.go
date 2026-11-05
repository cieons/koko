package serveraddr_test

import (
	"strings"
	"testing"

	"github.com/cieons/koko/internal/shared/serveraddr"
)

func TestCheckTakesHostOrHostAndPort(t *testing.T) {
	for _, s := range []string{
		"koko.example.com",
		"koko.example.com:443",
		"Koko.Example.COM:8443",
		"localhost",
		"xn--bcher-kva.example",
		"a-b.c1:1",
		"203.0.113.7",
		"203.0.113.7:65535",
		"[2001:db8::1]",
		"[2001:db8::1]:443",
		"[::ffff:203.0.113.7]:443",
		strings.Repeat("a", 63) + ".example",
		strings.Repeat(strings.Repeat("a", 61)+".", 4) + "com", // 251 characters
	} {
		if err := serveraddr.Check(s); err != nil {
			t.Errorf("Check(%q) = %v, want nil", s, err)
		}
	}
}

func TestCheckRefusesWhatIsNotAServerAddress(t *testing.T) {
	for _, tc := range []struct {
		s, says string
	}{
		{"", "empty"},
		{":443", "no host"},
		{"koko.example.com:", "port"},
		{"koko.example.com:0", "port"},
		{"koko.example.com:65536", "port"},
		{"koko.example.com:+443", "port"},
		{"koko.example.com:0443", "port"},
		{"koko.example.com:https", "port"},
		{"koko.example.com:443:1", "brackets"},
		{"2001:db8::1", "brackets"},
		{"[2001:db8::1]443", "after ']'"},
		{"[2001:db8::1", "']'"},
		{"[203.0.113.7]:443", "IPv6"},
		{"[fe80::1%eth0]:443", "IPv6"},
		{"[koko.example.com]", "IPv6"},
		{"203.0.113.256", "neither"},
		{"10.1", "neither"},
		{"koko..example.com", "empty label"},
		{"koko.example.com.", "empty label"},
		{".koko", "empty label"},
		{"-koko.example.com", "'-'"},
		{"koko-.example.com", "'-'"},
		{"koko_1.example.com", "'_'"},
		{"koko example.com", "' '"},
		{"https://koko.example.com", "not a URL"},
		{"bücher.example", "'ü'"},
		{strings.Repeat("a", 64) + ".example", "63"},
		{strings.Repeat(strings.Repeat("a", 62)+".", 4) + "com", "253"},
	} {
		err := serveraddr.Check(tc.s)
		if err == nil || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("Check(%q) = %v, want an error that says %q", tc.s, err, tc.says)
		}
	}
}
