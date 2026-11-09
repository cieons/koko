package server

import "testing"

// The koko server warns of a web listener address that reaches beyond this
// machine's loopback, every address included; a test binds none of them,
// so the rule is checked here rather than through a start.
func TestExposedTellsAWebListenerBeyondLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:7823":    false,
		"127.0.0.53:7823":   false,
		"[::1]:7823":        false,
		"localhost:7823":    false,
		":7823":             true,
		"0.0.0.0:7823":      true,
		"[::]:7823":         true,
		"192.0.2.10:7823":   true,
		"[2001:db8::1]:443": true,
		"web1:7823":         true,
	} {
		if got := exposed(addr); got != want {
			t.Errorf("exposed(%q) = %t, want %t", addr, got, want)
		}
	}
}
