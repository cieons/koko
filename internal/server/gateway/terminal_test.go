package gateway

import (
	"cmp"
	"log/slog"
	"testing"
	"time"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/host"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// The Gateway counts a Device Session's Tunnels by the caller's id, at most
// 64, and the Local caller's not at all; a Workspace's SSH Session counts
// against its tabs instead, whoever opened it. An SSH Session that ends
// frees its place.
func TestTheLimitCountsByTheCallersID(t *testing.T) {
	g := New(nil, nil, time.Now, timing.Timings{}, 0, slog.New(slog.DiscardHandler))
	t.Cleanup(g.Close)
	a := admission{who: access.Identity{User: api.User{Name: "alice"}}, host: host.Reach{ID: "host_1", Name: "web1"}}
	register := func(c op.Caller, client string) (*tunnel, *refusal) {
		t.Helper()
		tun := g.newTunnel(c, client, nil)
		limit, ok := g.register(tun, a, 0)
		if !ok {
			t.Fatal("the Gateway refused to register a Tunnel")
		}
		return tun, limit
	}
	for _, tc := range []struct {
		caller  op.Caller
		clients []string
		most    int
		banner  string
	}{
		{op.Caller{DeviceSession: "devicesession_1"}, []string{clientSSH}, 64, "koko: this Device Session already has 64 SSH Sessions open, the most koko allows; this SSH Session did not start.\n"},
	} {
		var first *tunnel
		for i := range tc.most {
			tun, limit := register(tc.caller, tc.clients[i%len(tc.clients)])
			if limit != nil {
				t.Fatalf("%+v: the SSH Session %d was refused", tc.caller, i+1)
			}
			first = cmp.Or(first, tun)
		}
		for _, client := range tc.clients {
			if _, limit := register(tc.caller, client); limit == nil || limit.banner != tc.banner || limit.code != wire.TerminalSSHSessionLimit {
				t.Errorf("%+v: one more from %s got %+v, want the limit's refusal", tc.caller, client, limit)
			}
		}
		first.end(errEnded)
		g.unregister(first)
		if _, limit := register(tc.caller, tc.clients[0]); limit != nil {
			t.Errorf("%+v: once one ended, another was refused", tc.caller)
		}
	}
	for _, c := range []op.Caller{{DeviceSession: "devicesession_1"}, {SignIn: "signin_1"}} {
		tun := g.newTunnel(c, clientBrowser, nil)
		tun.session = &termSession{}
		if limit, ok := g.register(tun, a, 0); !ok || limit != nil || tun.limited {
			t.Errorf("%+v: a Workspace's SSH Session was counted against its opener", c)
		}
	}
	for range 100 {
		if _, limit := register(op.Caller{Local: true}, clientBrowser); limit != nil {
			t.Fatal("the Local caller met a limit")
		}
	}
}

// The access check's refusal of a Host the caller cannot see answers a web
// terminal as an unknown name does; any other, such as a Revoked Device
// Session's, is access_refused with the refusal's message as one sentence.
func TestAnAccessRefusalCarriesItsCode(t *testing.T) {
	unseen := accessRefusal("web1", api.CodeUnknownHost.Errorf("no Host the caller can act on is named %q", "web1"))
	if unseen.code != wire.TerminalUnknownHost || unseen.message() != `no Host is named "web1".` {
		t.Errorf("an unseen Host is refused with %q and %q, want an unknown name's", unseen.code, unseen.message())
	}
	revoked := accessRefusal("web1", api.CodeRevoked.Errorf("the Device Session %q of this device was revoked", "laptop"))
	if revoked.code != wire.TerminalAccessRefused || revoked.message() != `the Device Session "laptop" of this device was revoked.` {
		t.Errorf("a Revoked Device Session is refused with %q and %q, want access_refused with its message", revoked.code, revoked.message())
	}
}
