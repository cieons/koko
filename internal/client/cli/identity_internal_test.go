package cli

import (
	"bytes"
	"strings"
	"testing"
)

// identity ls quotes a field holding a control character, such as a
// terminal's escape, rather than print it raw.
func TestIdentityLsPrintsNoControlCharacter(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	l := identityList{{ID: "identity_3f9k2m7q", User: "bob", Issuer: "https://idp.example.com/", Subject: "s\x1b[2J", Label: "l\x07", LinkedAt: "2026-11-16T00:00:00Z"}}
	if err := l.Human(&out); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsFunc(out.String(), func(r rune) bool { return r != '\n' && r < 0x20 }) {
		t.Errorf("identity ls printed a control character:\n%q", out.String())
	}
	if !strings.Contains(out.String(), `"s\x1b[2J"`) {
		t.Errorf("identity ls printed %q, want the subject quoted", out.String())
	}
}
