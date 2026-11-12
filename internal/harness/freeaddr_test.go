package harness

import (
	"net"
	"strconv"
	"testing"
)

// A port another test process's FreeAddr returned is passed over while that
// test runs, though nothing listens on it over TCP yet: its test, such as one
// that hands the port to koko web, binds it only later (#185). The test holds
// the port's reservation itself, as the other process would.
func TestFreeAddrPassesOverAPortAnotherTestProcessReserved(t *testing.T) {
	t.Parallel()
	theirs := freeAddr(t, &portSeq{last: firstFreePort + 1000})
	_, port, err := net.SplitHostPort(theirs)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}

	// This process's sequence starts just before the other's port.
	mine := freeAddr(t, &portSeq{last: n - 1})
	if mine == theirs {
		t.Errorf("FreeAddr returned %s, which another test process reserved and has yet to bind", mine)
	}
}
