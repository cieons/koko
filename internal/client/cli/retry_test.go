package cli_test

import (
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
)

// A call cut mid-flight on a direct Device Connection, after the koko server
// has carried it out and before its answer reaches the device, is sent once
// more on a new Device Connection: the command succeeds, and a write applies
// once, its retry answered with the response stored for its Idempotency-Key.
func TestACallCutMidFlightIsRetriedAndAppliesOnce(t *testing.T) {
	h, fwd, _, _ := initThrough(t)
	d := h.Join(harness.Owner)
	for _, tc := range []struct {
		name string
		args []string
		path string
	}{
		{"a write", []string{"space", "add", "ops", "--json"}, "/v1/space.add"},
		{"a read", []string{"space", "ls", "--json"}, "/v1/space.list"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			answered := "path=" + tc.path + " status=200"
			calls, conns := strings.Count(h.Log(), answered), fwd.Conns()
			fwd.CutWhen(func() bool { return strings.Count(h.Log(), answered) > calls })
			r := d.Koko(tc.args...)
			r.WantSuccess(t)
			if !strings.Contains(r.Stdout, `"name": "ops"`) {
				t.Errorf("printed %s, want the Space ops", r.Stdout)
			}
			if n := fwd.Conns() - conns; n != 2 {
				t.Errorf("the command made %d Device Connections, want 2: the one cut, and the retry's", n)
			}
		})
	}
	if n := strings.Count(h.Log(), "path=/v1/space.add status=200 replayed=true"); n != 1 {
		t.Errorf("the koko server answered %d space.add call(s) with the stored response, want the retry's:\n%s", n, h.Log())
	}
}

// When the call koko join makes once joined is cut mid-flight, it is sent
// again on a Device Connection of the key the join made a Device Session,
// and the join completes.
func TestAJoinWhoseCallIsCutStillJoins(t *testing.T) {
	h, fwd, _, init := initThrough(t)
	fwd.CutWhen(func() bool { return strings.Contains(h.Log(), "path=/v1/user.current status=200") })
	d := harness.NewDevice(t)
	d.Koko("join", init.JoinCode.Code).WantSuccess(t)
	if n := fwd.Conns(); n != 2 {
		t.Errorf("the join made %d connections, want 2: the one that joined, and the retry's", n)
	}
	d.Koko("space", "ls").WantSuccess(t)
}
