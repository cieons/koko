package gateway

import (
	"bytes"
	"cmp"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/host"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/resumable"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// chunks is a shell's stdout or stderr that sends n chunks of fill, each a
// record's worth, one per read.
type chunks struct {
	fill byte
	n    int
}

func (c *chunks) Read(p []byte) (int, error) {
	if c.n == 0 {
		return 0, io.EOF
	}
	c.n--
	n := min(len(p), wire.TerminalMaxRecord)
	for i := range n {
		p[i] = c.fill
	}
	return n, nil
}

// The Gateway counts SSH Sessions by the caller's id, whichever client they
// come from: a Sign-in holds at most 16, a Device Session 64 from the CLI
// and koko web's terminal together, and the Local caller has no limit. An
// SSH Session that ends frees its place.
func TestTheLimitCountsByTheCallersID(t *testing.T) {
	g := New(nil, nil, time.Now, timing.Timings{}, slog.New(slog.DiscardHandler))
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
		{op.Caller{SignIn: "signin_1"}, []string{clientBrowser}, 16, "koko: this Sign-in already has 16 SSH Sessions open, the most koko allows; this SSH Session did not start.\n"},
		{op.Caller{DeviceSession: "devicesession_1"}, []string{clientSSH, clientBrowser}, 64, "koko: this Device Session already has 64 SSH Sessions open, the most koko allows; this SSH Session did not start.\n"},
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
	if _, limit := register(op.Caller{SignIn: "signin_2"}, clientBrowser); limit != nil {
		t.Error("another Sign-in's first SSH Session was refused")
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

// The records a web terminal's shell writes into its stream, from the
// Host's stdout and stderr and an ending's notice, take turns while the
// stream's buffer fills: each arrives whole, the notice last, and nothing
// after it.
func TestATerminalsRecordsTakeTurns(t *testing.T) {
	a, b := net.Pipe()
	gw, page := resumable.New(a), resumable.New(b)
	t.Cleanup(func() {
		_ = gw.Close()
		_ = page.Close()
	})
	sh := &terminalShell{stream: gw}
	type record struct {
		fill byte
		n    int
		data string
	}
	read := make(chan []record, 1)
	flowing := make(chan struct{})
	go func() {
		var recs []record
		r := wire.NewTerminalReader(page)
		for {
			rec, err := r.Next()
			if err != nil {
				read <- recs
				return
			}
			if len(recs) == 20 {
				close(flowing)
			}
			if rec.Type != wire.TerminalData || len(rec.Data) == 0 {
				t.Errorf("read a record of type %d and %d bytes", rec.Type, len(rec.Data))
				continue
			}
			if rec.Data[0] == '\r' {
				recs = append(recs, record{data: string(rec.Data)})
				continue
			}
			if !bytes.Equal(rec.Data, bytes.Repeat(rec.Data[:1], len(rec.Data))) {
				t.Error("a record's data mixes the shell's stdout with its stderr")
			}
			recs = append(recs, record{fill: rec.Data[0], n: len(rec.Data)})
		}
	}()

	const notice = "\r\nkoko: the koko server is restarting; this SSH Session has ended.\r\n"
	var wg sync.WaitGroup
	wg.Go(func() { sh.output(&chunks{fill: 'o', n: 200}) })
	wg.Go(func() { sh.output(&chunks{fill: 'e', n: 200}) })
	<-flowing
	wg.Go(func() {
		if !sh.last(noticeRecord("the koko server is restarting; this SSH Session has ended."), gw.Detached(), &wg) {
			t.Error("the notice was not written while the page reads")
		}
	})
	wg.Wait()
	_ = gw.CloseWithReason("the koko server is restarting; this SSH Session has ended.")
	recs := <-read
	if len(recs) == 0 || recs[len(recs)-1].data != notice {
		t.Fatalf("the page read %d records, the last %+v, want the notice last", len(recs), recs[len(recs)-1:])
	}
	for _, r := range recs[:len(recs)-1] {
		if r.data != "" || r.n != wire.TerminalMaxRecord {
			t.Errorf("a record before the notice is %+v, want a whole chunk of output", r)
		}
	}
}
