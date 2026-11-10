package server

import (
	"net"
	"strings"
	"testing"
)

// A taken port's holder that portowner cannot tell by its pid, here one
// nobody this user can see holds, comes with the commands that name it; a
// start that binds the port cannot play such a holder.
func TestPortHolderSaysHowToFindAHolderItCannotName(t *testing.T) {
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	if got, want := portHolder(t.Context(), port), "(sudo ss -ltnp or sudo lsof -i :"+port+" names it)"; !strings.HasSuffix(got, want) {
		t.Errorf("portHolder(%s) = %q, want it to end %q", port, got, want)
	}
}
