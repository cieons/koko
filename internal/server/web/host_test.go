package web

import "testing"

// The Host header must name koko web's port on localhost or 127.0.0.1, as a
// browser sends it: without the port when it is HTTP's own, 80.
func TestHostAllowed(t *testing.T) {
	cases := []struct {
		host string
		port int
		want bool
	}{
		{"localhost:5656", 5656, true},
		{"127.0.0.1:5656", 5656, true},
		{"localhost", 80, true},
		{"127.0.0.1", 80, true},
		{"localhost:80", 80, true},
		{"localhost", 5656, false},
		{"localhost:5657", 5656, false},
		{"evil.example:5656", 5656, false},
		{"evil.example", 80, false},
		{"[::1]:5656", 5656, false},
		{"LOCALHOST:5656", 5656, false},
	}
	for _, c := range cases {
		if got := hostAllowed(c.host, c.port); got != c.want {
			t.Errorf("hostAllowed(%q, %d) = %t, want %t", c.host, c.port, got, c.want)
		}
	}
}
