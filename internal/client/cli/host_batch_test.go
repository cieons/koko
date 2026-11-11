package cli_test

import (
	"encoding/json/v2"
	"net/http"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

// sendError calls o with body as the Local caller and returns its error
// object, the zero one when the call succeeded.
func sendError(t *testing.T, h *harness.Harness, o api.Operation, body string) api.Error {
	t.Helper()
	status, b := h.Send(o, []byte(body))
	if status == http.StatusOK {
		return api.Error{}
	}
	var e api.ErrorResponse
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatalf("%s answered %d %s, which is not an error object: %v", o.Info().Name, status, b, err)
	}
	return e.Error
}

// A batch host.retire counts only the Hosts outside it as using a Relay:
// a Relay retires with every Host behind it, and is at fault while one
// outside the batch is not Retired.
func TestARelayRetiresInABatchWithTheHostsBehindIt(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	relay := managedHost(t, h, "gw")
	relayedHost(t, h, "gw", "web1", relay)
	relayedHost(t, h, "gw", "web2", relay)

	e := sendError(t, h, api.HostRetire, `{"hosts": ["gw", "web1"]}`)
	if e.Code != "relay_in_use" || len(e.Items) != 1 || e.Items[0].Index != 0 {
		t.Errorf("retiring gw with web1 alone = %+v, want gw at fault with relay_in_use, as web2 still uses it", e)
	}
	if got := h.Koko("host", "show", "web1", "--json"); got.Status != 0 {
		t.Errorf("web1 is gone after the refused batch: %q", got.Stderr)
	}
	if e := sendError(t, h, api.HostRetire, `{"hosts": ["gw", "web1", "web2"]}`); e.Code != "" {
		t.Errorf("retiring gw with every Host behind it = %+v, want it done", e)
	}
	for _, n := range []string{"gw", "web1", "web2"} {
		h.Koko("host", "show", n, "--json").WantError(t, 1, "unknown_host")
	}
}

// A batch host.move ends, once it commits, the live SSH Sessions to each
// moved Host of a Member who cannot act in the new Space; a refused batch
// ends none.
func TestABatchMoveEndsTheSSHSessionsThatLoseTheirHostsOnceItIsApplied(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	addSpaceWithMembers(t, h, "ops", "bob")
	laptop := h.Join("bob")
	managedHostIn(t, h, "web1", "ops")
	managedHostIn(t, h, "web2", "ops")
	h.Koko("space", "add", "vault").WantSuccess(t)
	web1 := openLiveSSH(t, h, laptop.Proxy(nil, "tunnel", "web1"), "web1")
	web2 := openLiveSSH(t, h, laptop.Proxy(nil, "tunnel", "web2"), "web2")

	if e := sendError(t, h, api.HostMove, `{"hosts": ["web1", "web2", "nowhere"], "space": "vault"}`); e.Code != "unknown_host" {
		t.Fatalf("a batch move with nowhere = %+v, want unknown_host", e)
	}
	web1.wantOpen("after the refused batch")
	web2.wantOpen("after the refused batch")

	if e := sendError(t, h, api.HostMove, `{"hosts": ["web1", "web2"], "space": "vault"}`); e.Code != "" {
		t.Fatalf("the batch move = %+v", e)
	}
	web1.wantEnded("koko: access to web1 was withdrawn; this SSH Session is closed.\n")
	web2.wantEnded("koko: access to web2 was withdrawn; this SSH Session is closed.\n")
}
