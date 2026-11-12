package sockerr_test

import (
	"fmt"
	"net"
	"syscall"
	"testing"

	"github.com/cieons/koko/internal/shared/sockerr"
)

// A connection nothing listens for is told as refused, and a port something
// holds as in use, by the errors the OS itself returns, on every OS the
// tests run on; an error of another kind is neither.
func TestIsTellsARefusedConnectionAndATakenPort(t *testing.T) {
	t.Parallel()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()

	_, err = lc.Listen(t.Context(), "tcp", addr)
	if !sockerr.Is(err, syscall.EADDRINUSE) {
		t.Errorf("listening on a port in use failed with %v, which Is does not tell as EADDRINUSE", err)
	}
	if sockerr.Is(err, syscall.ECONNREFUSED, syscall.ETIMEDOUT) {
		t.Errorf("Is tells %v as refused or timed out", err)
	}

	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	var d net.Dialer
	conn, err := d.DialContext(t.Context(), "tcp", addr)
	if err == nil {
		_ = conn.Close()
		t.Fatalf("something answers on %s, which this test just closed", addr)
	}
	if !sockerr.Is(fmt.Errorf("connect: %w", err), syscall.ETIMEDOUT, syscall.ECONNREFUSED) {
		t.Errorf("connecting where nothing listens failed with %v, which Is does not tell as ECONNREFUSED", err)
	}
	if sockerr.Is(err, syscall.EADDRINUSE) || sockerr.Is(nil, syscall.ECONNREFUSED) || sockerr.Is(err) {
		t.Errorf("Is tells %v as in use, or nil or no number as a match", err)
	}
}
