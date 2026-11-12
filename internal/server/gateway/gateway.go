// Package gateway is the Gateway: it ends every SSH Session and opens its
// own SSH connection to the Host, logging in with the koko key, which never
// leaves the koko server (ADR 0002; P0 spec: reach, "The Gateway").
//
// Each tunnel@koko channel is one Tunnel: a resumable stream (ADR 0021)
// beneath one SSH Session. The Gateway sends the Tunnel's id first, then runs
// an SSH server on the stream with "none" authentication, the koko server's
// sealed host key and AES-GCM ciphers only. It allows a new SSH Session only
// when the caller passes the access check for the Host, the Host and its
// Relay, if any, are Managed and the caller's Device Session has fewer than
// 64 open, and dials the Host, through its Relay, while the client's
// handshake runs; every refusal is a banner on the
// refused "none", the user name and the Host connector's failures among
// them. Once the client is in, each channel it opens is opened on the Host as
// asked, and the two are relayed both ways: every byte, EOF and request but
// agent and X11 forwarding (relay.go). An SSH Session that ends for a
// reason the client did not see coming, the Host's, the koko server's or
// its access withdrawn (withdraw.go), tells it in one line on the stderr of
// each of its session channels.
//
// The Tunnel registry holds every Tunnel the Gateway accepted, by its id,
// for as long as it lives: a Tunnel whose channel fails is Detached, and
// lives for the grace, for the device to Resume it on another channel
// (resume.go). A web terminal's SSH Session, which a browser opens over a
// WebSocket the web front hands over, is one more kind of Tunnel in the same
// registry, with a pty and a shell on the Host and a grace of its own
// (terminal.go).
package gateway

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/host"
	"github.com/cieons/koko/internal/server/hostconn"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/name"
	"github.com/cieons/koko/internal/shared/resumable"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// Gateway takes the tunnel@koko channels of every connection to the koko
// server (transport.Tunnels).
type Gateway struct {
	store *store.Store
	// rootKey opens the koko server's host key and the koko key.
	rootKey   *seal.RootKey
	connector hostconn.Connector
	log       *slog.Logger
	// clock is the koko server's clock, which a Sign-in's expiry is read on.
	clock func() time.Time
	// timings are the P0 timings, resolved: the grace among them.
	timings timing.Timings

	mu     sync.Mutex
	closed bool
	// terminalsClosed refuses new web terminals once CloseTerminals has
	// ended them, before the Tunnels end (terminal.go).
	terminalsClosed bool
	// tunnels is the Tunnel registry.
	tunnels map[tunnelID]*tunnel
	// ended holds, by its id, how each Tunnel that left the registry with
	// a notice ended, until its grace would have ended, so that a resume
	// of it is refused with why (resume.go).
	ended map[tunnelID]endedTunnel
	// sshSessions counts, by the caller's id (op.Caller.ID), the Tunnels in the
	// registry that the access check admitted, attached or Detached: from
	// then until the Tunnel ends, its dial to the Host included.
	sshSessions map[string]int
	// withdrawals counts the access changes Withdraw took, so that a Tunnel
	// admitted across one is checked again once it is in the registry.
	withdrawals uint64
	// webURLChanged is the count of withdrawals at the last change of the
	// Web URL, which no check of access tells: a Sign-in's web terminal
	// admitted across it ends once registered.
	webURLChanged uint64
	// stopped is closed once the Gateway closes, which stops sweepSignIns.
	stopped chan struct{}
	wg      sync.WaitGroup
}

// maxSSHSessions is how many SSH Sessions one Device Session may have open
// at once, from the CLI and koko web's terminal together, so that one
// runaway agent cannot exhaust the koko server.
const maxSSHSessions = 64

// maxBrowserSSHSessions is how many SSH Sessions one Sign-in may hold at
// once, attached or Detached, across all its tabs: fewer than a Device
// Session's, since a browser's are held fifteen times longer, and a stolen
// cookie or a reload loop could otherwise hold many Host connections.
const maxBrowserSSHSessions = 16

// The clients an SSH Session comes from, as its log lines name them: the
// CLI's SSH client in a Tunnel, or a browser's web terminal.
const (
	clientSSH     = "ssh"
	clientBrowser = "browser"
)

// New returns a Gateway that reads the record in st, opens the koko server's
// keys with rootKey, reads a Sign-in's expiry on clock, the koko server's,
// connects to Hosts as timings say (the zero Timings is the product's), and
// logs each SSH Session to log. Until it closes, it looks for web terminals
// whose Sign-in has expired every timing.SignInCheck (sweepSignIns).
func New(st *store.Store, rootKey *seal.RootKey, clock func() time.Time, timings timing.Timings, log *slog.Logger) *Gateway {
	g := &Gateway{
		store:       st,
		rootKey:     rootKey,
		connector:   hostconn.Connector{Timings: timings},
		log:         log,
		clock:       clock,
		timings:     timings.Resolve(),
		tunnels:     map[tunnelID]*tunnel{},
		ended:       map[tunnelID]endedTunnel{},
		sshSessions: map[string]int{},
		stopped:     make(chan struct{}),
	}
	g.wg.Go(g.sweepSignIns)
	return g
}

// readContext is ctx for a read that asks the access check about a caller,
// such as a Sign-in, whose expiry it reads at the koko server's time.
func (g *Gateway) readContext(ctx context.Context) context.Context {
	return op.AtTime(ctx, g.clock())
}

// tunnelID is a Tunnel's id: 128 random bits, written in logs as 32
// lowercase hex characters.
type tunnelID [wire.TunnelIDSize]byte

func (id tunnelID) String() string { return hex.EncodeToString(id[:]) }

// tunnel is one Tunnel in the registry.
type tunnel struct {
	id     tunnelID
	caller op.Caller
	// client is who the SSH Session comes from: clientSSH for a Tunnel of
	// the CLI's, clientBrowser for a web terminal (terminal.go).
	client string
	// run, for a web terminal of the Local caller's, names the run of koko
	// web it came through, which alone may Resume it.
	run string
	// holdFor is the grace: how long the Tunnel is held Detached.
	holdFor time.Duration
	// from is the address of the connection the Tunnel came on.
	from   net.Addr
	stream *resumable.Stream
	// ctx ends with the Tunnel, end ending it with why.
	ctx context.Context
	end context.CancelCauseFunc
	// limited says the Tunnel counts against its caller's limit.
	limited bool
	// hostID and hostName are the Host the access check admitted the SSH
	// Session to, by its id and by its name then, relayID the id of the
	// Relay it passes through, empty for none, and user the name of the
	// User it acts as; hostID is empty for an SSH Session refused.
	hostID, hostName, relayID, user string
	// done is closed once the Tunnel has left the registry.
	done chan struct{}

	// mu guards the Tunnel's Detach and Resume (resume.go): watched is the
	// channel watched for its end, resuming counts the resumes moving the
	// Tunnel onto a channel, and grace, while the Tunnel is Detached, ends
	// it unless a resume comes first.
	mu       sync.Mutex
	watched  <-chan struct{}
	resuming int
	grace    *time.Timer
	// detached is when the running grace started.
	detached time.Time
	// socket, for a web terminal, is the WebSocket its stream was last
	// attached to, which cut closes at once.
	socket TerminalSocket

	// files, for a web terminal once its shell has started, are its files
	// on the Host (files.go). g.mu guards it.
	files *terminalFiles
}

// newTunnel returns a new Tunnel, not yet in the registry, of the caller's,
// from client, which came on a connection from the address from: a Tunnel
// of the CLI's, held for timing.Grace once Detached.
func (g *Gateway) newTunnel(caller op.Caller, client string, from net.Addr) *tunnel {
	t := &tunnel{caller: caller, client: client, holdFor: g.timings.Grace, from: from, done: make(chan struct{})}
	_, _ = rand.Read(t.id[:]) // never fails
	t.ctx, t.end = context.WithCancelCause(context.Background())
	return t
}

// callerKey is whose a Tunnel is, as a resume or an end must show to reach
// it: the caller's id (op.Caller.ID), and for the Local caller's web terminal,
// the run of koko web it came through.
type callerKey struct {
	id, run string
}

func (t *tunnel) key() callerKey {
	return callerKey{id: t.caller.ID(), run: t.run}
}

// callerAttrs names who an SSH Session came from in a log line: a Sign-in by
// its id, or a Device Session by its id, or the Unix socket, as other lines
// name who acted.
func callerAttrs(c op.Caller) []any {
	if c.SignIn != "" {
		return []any{"signin", c.SignIn}
	}
	return []any{"device_session", deviceSession(c)}
}

// named starts each log line about the Tunnel t's SSH Session: its id and
// the client it comes from.
func (t *tunnel) named() []any {
	return []any{"tunnel", t.id, "client", t.client}
}

// ending is why the Gateway ends an SSH Session, as its log line's reason
// says, and the notice it gives the client, if any: one line on the stderr
// of each session channel the client has open (wire.TunnelNoticeLine), then
// the reason of the Tunnel's close frame, which koko exec shows when no
// session channel of its took the line. why, set beside each notice, is
// the notice's clause on why, such as "web1 closed the connection": the
// message of a resume refused once the Tunnel has ended (resume.go), which
// the device prints after "could not be resumed: ".
type ending struct {
	reason string
	notice string
	why    string
}

func (e *ending) Error() string { return e.reason }

// endingOf returns the ending a Tunnel's end cause carries, or an ending
// with no notice for any other cause.
func endingOf(cause error) *ending {
	if e, ok := errors.AsType[*ending](cause); ok {
		return e
	}
	return &ending{reason: cause.Error()}
}

// Why a Tunnel ends, besides its client or its Host closing the SSH Session.
var (
	// errGraceOver: the Tunnel stayed Detached for the whole grace.
	errGraceOver = &ending{reason: "grace over"}
	// errStopping: the koko server is stopping, as for an upgrade.
	errStopping = &ending{reason: "server stopping", notice: "the koko server is restarting; this SSH Session has ended.", why: "the koko server is restarting"}
	// errEnded: the SSH Session has ended, or never started.
	errEnded = errors.New("the SSH Session ended")
)

// Open takes one tunnel@koko channel the connection c asks for. It refuses a
// payload it cannot read, and refuses every channel before the koko server is
// initialised, since the Gateway has no host key to present until then.
// Otherwise it accepts the Tunnel and serves it in a goroutine of its own.
func (g *Gateway) Open(c *transport.Connection, nch ssh.NewChannel) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		_ = nch.Reject(ssh.ConnectionFailed, "the koko server is stopping")
		return
	}
	g.wg.Go(func() { g.serve(c, nch) })
}

// Close ends every Tunnel, telling each SSH Session that the koko server is
// restarting (errStopping), refuses new ones, and waits for the Tunnels'
// goroutines. Ending a Tunnel closes its stream with the notice, which waits
// until its channel took the close or failed, so the transport's Close,
// which ends a connection too stalled to take it, must follow in time.
func (g *Gateway) Close() {
	g.mu.Lock()
	if !g.closed {
		close(g.stopped)
	}
	g.closed = true
	for _, t := range g.tunnels {
		t.end(errStopping)
	}
	g.mu.Unlock()
	g.wg.Wait()
}

// serve serves one Tunnel from its channel open until it ends, or Resumes
// the one a resume names (resume.go).
func (g *Gateway) serve(c *transport.Connection, nch ssh.NewChannel) {
	caller := c.Caller()
	from := append(callerAttrs(caller), "addr", c.RemoteAddr())
	p, err := wire.ParseTunnelPayload(nch.ExtraData())
	if err == nil && p.Kind != wire.TunnelKindOpen && p.Kind != wire.TunnelKindResume {
		err = fmt.Errorf("this koko server takes no tunnel@koko %q", p.Kind)
	}
	if err != nil {
		g.log.Info("tunnel refused", append(from, "err", err)...)
		_ = nch.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	if p.Kind == wire.TunnelKindResume {
		g.resume(c, nch, p)
		return
	}
	sshName := p.SSHName
	hostName := wire.TunnelHost(sshName)
	seen := g.withdrawalsTaken()
	a, err := g.admit(c.Context(), caller, hostName, "")
	if errors.Is(err, errNotInitialised) {
		g.log.Info("tunnel refused", append(from, "name", sshName, "err", err)...)
		_ = nch.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	if err != nil {
		g.log.Error("open a Tunnel", append(from, "name", sshName, "err", err)...)
		_ = nch.Reject(ssh.ConnectionFailed, "the koko server could not open this Tunnel; its log says why")
		return
	}
	ch, reqs, err := nch.Accept()
	if err != nil {
		g.log.Debug("accept a tunnel@koko channel", "addr", c.RemoteAddr(), "err", err)
		return
	}
	g.wg.Go(func() { ssh.DiscardRequests(reqs) })

	t := g.newTunnel(caller, clientSSH, c.RemoteAddr())
	t.stream = resumable.New(ch)
	limit, ok := g.register(t, a, seen)
	if !ok {
		_ = t.stream.CloseWithReason(errStopping.notice)
		return
	}
	defer g.unregister(t)
	if limit != nil {
		a.refused = limit
	}
	// Losing its channel Detaches the Tunnel for the grace.
	g.watch(t)
	// Until its SSH Session starts, the Tunnel's end closes the stream at
	// once, with the end's notice; once it has started, the relay gives the
	// notice first.
	stopEarlyClose := context.AfterFunc(t.ctx, func() { _ = t.stream.CloseWithReason(endingOf(context.Cause(t.ctx)).notice) })
	defer t.end(errEnded)

	if _, err := t.stream.Write(t.id[:]); err != nil {
		return
	}
	g.session(t, sshName, a, stopEarlyClose)
}

// register puts the new Tunnel t in the registry, under its id. One the
// access check admitted, as a says, counts against its caller's limit
// (op.Caller.ID), unless the caller holds the most already, when register
// returns the refusal: maxBrowserSSHSessions for a Sign-in, maxSSHSessions
// for a Device Session. Withdraw could not see one admitted across an access
// change, a change it took after the first seen, so register has it checked
// again, and ends a Sign-in's admitted across a change of the Web URL,
// which no check of access tells. It fails once the Gateway is closing, or
// for a web terminal once CloseTerminals has ended them.
func (g *Gateway) register(t *tunnel, a admission, seen uint64) (*refusal, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || t.client == clientBrowser && g.terminalsClosed {
		t.end(errStopping)
		return nil, false
	}
	g.tunnels[t.id] = t
	if a.refused != nil {
		return nil, true
	}
	if id := t.caller.ID(); id != "" {
		most, holder := maxSSHSessions, "Device Session"
		if t.caller.SignIn != "" {
			most, holder = maxBrowserSSHSessions, "Sign-in"
		}
		if g.sshSessions[id] >= most {
			return &refusal{
				banner: fmt.Sprintf("koko: this %s already has %d SSH Sessions open, the most koko allows; this SSH Session did not start.\n", holder, most),
				reason: "ssh session limit", code: wire.TerminalSSHSessionLimit,
			}, true
		}
		g.sshSessions[id]++
		t.limited = true
	}
	t.hostID, t.hostName, t.user = a.host.ID, a.host.Name, a.who.User.Name
	if r := a.host.Relay; r != nil {
		t.relayID = r.ID
	}
	switch {
	case t.caller.SignIn != "" && g.webURLChanged > seen:
		t.end(errWebURLChanged)
	case g.withdrawals != seen:
		g.wg.Go(func() { g.recheck([]*tunnel{t}) })
	}
	return nil, true
}

// withdrawalsTaken returns how many access changes Withdraw has taken.
func (g *Gateway) withdrawalsTaken() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.withdrawals
}

func (g *Gateway) unregister(t *tunnel) {
	t.mu.Lock()
	from := cmp.Or(t.detached, time.Now())
	t.stopGraceLocked()
	t.mu.Unlock()
	g.mu.Lock()
	defer g.mu.Unlock()
	close(t.done)
	delete(g.tunnels, t.id)
	g.keepEnding(t, from.Add(t.holdFor))
	if id := t.caller.ID(); t.limited {
		if g.sshSessions[id]--; g.sshSessions[id] == 0 {
			delete(g.sshSessions, id)
		}
	}
}

// admission is what the Gateway reads for a new Tunnel: the koko server's
// keys, who is calling, and the Host they asked for, or the banner that
// refuses them.
type admission struct {
	// hostKey is the koko server's host key, which the Gateway presents
	// inside every Tunnel; kokoKey logs in to the Host.
	hostKey ssh.Signer
	kokoKey ssh.Signer
	who     access.Identity
	host    host.Reach
	// refused refuses the SSH Session, or is nil when it may start.
	refused *refusal
}

// refusal is why the Gateway refuses an SSH Session: the banner its client's
// "none" is refused with, the code a web terminal's page tests
// (wire.TerminalUnknownHost, ...), and, for the log line, the reason, any
// further fields, and whether it is logged as a warning.
type refusal struct {
	banner string
	code   string
	reason string
	attrs  []any
	warn   bool
}

// message is the refusal's banner as a web terminal's page shows it: one
// sentence, without "koko: " and the newline.
func (r *refusal) message() string {
	return strings.TrimSuffix(strings.TrimPrefix(r.banner, "koko: "), "\n")
}

// errRefused is a "none" the Gateway refused with a banner.
var errRefused = errors.New("the Gateway refused the SSH Session")

// errNotInitialised refuses a Tunnel before the koko server has its keys.
var errNotInitialised = errors.New("the koko server is not initialised, so it has no host key to present yet")

// admit reads, in one read-only transaction, the koko server's keys, and
// whether the caller may start a new SSH Session to the Host named hostName:
// the access check passes and the Host and its Relay, if any, are Managed. An unknown name, a Host the
// caller may not reach and a Retired Host, which holds no name, get the same
// refusal, byte for byte. A web terminal's page that the web listener
// checked against webURL, when not empty, is refused once the Web URL is
// another. It fails only on an error of the koko server's own.
func (g *Gateway) admit(ctx context.Context, caller op.Caller, hostName, webURL string) (admission, error) {
	var a admission
	ctx = g.readContext(ctx)
	err := g.store.Read(ctx, func(tx store.Tx) error {
		keys, err := deployment.Open(ctx, tx, g.rootKey)
		switch {
		case err != nil:
			return err
		case keys == nil:
			return errNotInitialised
		}
		if a.hostKey, err = ssh.NewSignerFromKey(keys.Host); err != nil {
			return fmt.Errorf("use the host key: %w", err)
		}
		if a.kokoKey, err = ssh.NewSignerFromKey(keys.Koko); err != nil {
			return fmt.Errorf("use the koko key: %w", err)
		}
		if webURL != "" {
			current, _, err := deployment.WebURL(ctx, tx)
			if err != nil {
				return err
			}
			if current != webURL {
				// The page is signed out: its cookie is the old address's.
				a.refused = &refusal{
					banner: "koko: the Web URL changed; this SSH Session did not start. Sign in at the new address.\n",
					code:   wire.TerminalAccessRefused, reason: "web url changed",
				}
				return nil
			}
		}
		if name.Check(hostName) != nil {
			// Not a Host name, such as a Host's id: no SSH Name names it.
			a.refused = &refusal{banner: unknownHost(hostName), code: wire.TerminalUnknownHost, reason: "unknown host"}
			return nil
		}
		a.who, a.host, err = host.ReachOf(ctx, tx, caller, hostName)
		if e, ok := errors.AsType[*api.Error](err); ok {
			a.refused = accessRefusal(hostName, e)
			return nil
		}
		if err != nil {
			return err
		}
		switch a.host.State {
		case api.HostManaged:
		case api.HostRegistered:
			a.refused = &refusal{
				banner: fmt.Sprintf("koko: %s is Registered: koko logs in to it only once it is Enrolled, with koko host enroll %s.\n", hostName, hostName),
				code:   wire.TerminalHostRegistered, reason: "host registered",
			}
		default:
			a.refused = &refusal{banner: unknownHost(hostName), code: wire.TerminalUnknownHost, reason: "host " + a.host.State}
		}
		if r := a.host.Relay; a.refused == nil && r != nil && r.State != api.HostManaged {
			a.refused = unmanagedRelay(hostName, r)
		}
		return nil
	})
	if err != nil {
		return admission{}, fmt.Errorf("decide whether the caller may reach %q: %w", hostName, err)
	}
	return a, nil
}

// unknownHost is the banner for a name no Host the caller can reach holds:
// the same for a Host in a Space they cannot act in, so the Gateway tells
// nothing of Hosts outside their Spaces.
func unknownHost(hostName string) string {
	return fmt.Sprintf("koko: no Host is named %q.\n", hostName)
}

// accessRefusal refuses a caller the access check refused with e: with the
// unknown Host's banner for a Host they cannot see, and otherwise with the
// refusal itself, such as a Revoked Device Session's.
func accessRefusal(hostName string, e *api.Error) *refusal {
	if e.Code == api.CodeUnknownHost.Name || e.Code == api.CodeInvalidName.Name {
		return &refusal{banner: unknownHost(hostName), code: wire.TerminalUnknownHost, reason: e.Code}
	}
	return &refusal{banner: "koko: " + e.Message + ".\n", code: wire.TerminalAccessRefused, reason: e.Code}
}

// unmanagedRelay refuses an SSH Session to the Host named hostName, whose
// Relay r is not Managed: koko passes through a Relay only once it is
// Enrolled. It names the Relay only to a caller who can see it.
func unmanagedRelay(hostName string, r *host.Relay) *refusal {
	way := "."
	if r.Seen {
		way = ", with koko host enroll " + r.Name + "."
	}
	return &refusal{
		banner: fmt.Sprintf("koko: %s is %s: koko passes through it only once it is Enrolled%s\n", r.Of(hostName), stateName(r.State), way),
		code:   wire.TerminalRelayRegistered, reason: "relay " + r.State,
	}
}

// stateName is how a message names the state of a Host.
func stateName(state string) string {
	switch state {
	case api.HostRegistered:
		return "Registered"
	case api.HostManaged:
		return "Managed"
	case api.HostRetired:
		return "Retired"
	}
	return state
}

// userRefusal refuses the SSH user name user for the Host h, which the
// caller may reach: the Gateway takes koko, which koko's ssh_config and koko
// exec ask for, and the Host Account's own name, and no other, so that a
// later release may give other names a meaning. It returns nil for a name it
// takes.
func userRefusal(h host.Reach, user string) *refusal {
	if user == wire.TunnelUser || user == h.Account {
		return nil
	}
	return &refusal{
		banner: fmt.Sprintf("koko: %s is reached as its Host Account %q; leave out %q.\n", h.Name, h.Account, user+"@"),
		reason: "user name", attrs: []any{"ssh_user", user},
	}
}

// connectRefusal refuses an SSH Session to the Host h, which the Host
// connector failed to log in to with err: the Host is down after the
// connector's retries, its host key is not the pinned one, which the banner
// shows beside the one offered and says how to pin, or it refused the koko
// key. Each is logged as a warning, since the Owner may have to act; the
// connector's error, which names addresses, goes to the log only. A failure
// on the leg to h's Relay is told as relayConnectRefusal tells it.
func connectRefusal(h host.Reach, err error) *refusal {
	if _, ok := errors.AsType[*hostconn.RelayError](err); ok && h.Relay != nil {
		return relayConnectRefusal(h.Name, h.Relay, err)
	}
	if m, ok := errors.AsType[*hostconn.HostKeyMismatchError](err); ok {
		return &refusal{
			banner: fmt.Sprintf("koko: the host key of %s does not match the one pinned at Enroll (pinned %s, offered %s); koko did not log in. "+
				"If %s was rebuilt, pin its new key with koko host enroll %s --host-key <the fingerprint its provider shows>, "+
				"or with --replace-host-key, which trusts whatever key it offers.\n", h.Name, m.Pinned, m.Offered, h.Name, h.Name),
			code:   wire.TerminalHostKeyMismatch,
			reason: "host key mismatch", attrs: []any{"pinned", m.Pinned, "offered", m.Offered}, warn: true,
		}
	}
	r := &refusal{attrs: []any{"err", err}, warn: true}
	switch {
	case errors.Is(err, hostconn.ErrUnreachable):
		r.banner = fmt.Sprintf("koko: %s is down or unreachable: koko could not connect to it; this SSH Session did not start.\n", h.Name)
		r.code, r.reason = wire.TerminalHostUnreachable, "host unreachable"
	case errors.Is(err, hostconn.ErrLoginRefused):
		r.banner = fmt.Sprintf("koko: %s refused the koko key for its Host Account %q; this SSH Session did not start.\n", h.Name, h.Account)
		r.code, r.reason = wire.TerminalLoginRefused, "login refused"
	default:
		r.banner = fmt.Sprintf("koko: koko could not connect to %s; this SSH Session did not start.\n", h.Name)
		r.code, r.reason = wire.TerminalHostConnectionFailed, "host connection failed"
	}
	return r
}

// relayConnectRefusal refuses an SSH Session to the Host named hostName,
// since the Host connector failed to log in to its Relay r with err. It is a
// Host's refusal, with the Relay in the Host's place, named only to a caller
// who can see it: one who cannot learns neither its fingerprints nor its
// Host Account, and cannot act on it.
func relayConnectRefusal(hostName string, r *host.Relay, err error) *refusal {
	relay := r.Of(hostName)
	if m, ok := errors.AsType[*hostconn.HostKeyMismatchError](err); ok {
		banner := fmt.Sprintf("koko: the host key of %s does not match the one pinned at Enroll; koko did not log in.\n", relay)
		if r.Seen {
			banner = fmt.Sprintf("koko: the host key of %s does not match the one pinned at Enroll (pinned %s, offered %s); koko did not log in. "+
				"If %s was rebuilt, pin its new key with koko host enroll %s --host-key <the fingerprint its provider shows>, "+
				"or with --replace-host-key, which trusts whatever key it offers.\n", relay, m.Pinned, m.Offered, r.Name, r.Name)
		}
		return &refusal{
			banner: banner, code: wire.TerminalHostKeyMismatch,
			reason: "relay host key mismatch", attrs: []any{"relay_id", r.ID, "pinned", m.Pinned, "offered", m.Offered}, warn: true,
		}
	}
	ref := &refusal{attrs: []any{"relay_id", r.ID, "err", err}, warn: true}
	switch {
	case errors.Is(err, hostconn.ErrUnreachable):
		ref.banner = fmt.Sprintf("koko: %s is down or unreachable: koko could not connect to it; this SSH Session did not start.\n", relay)
		ref.code, ref.reason = wire.TerminalHostUnreachable, "relay unreachable"
	case errors.Is(err, hostconn.ErrLoginRefused) && r.Seen:
		ref.banner = fmt.Sprintf("koko: %s refused the koko key for its Host Account %q; this SSH Session did not start.\n", relay, r.Account)
		ref.code, ref.reason = wire.TerminalLoginRefused, "relay login refused"
	case errors.Is(err, hostconn.ErrLoginRefused):
		ref.banner = fmt.Sprintf("koko: %s refused the koko key; this SSH Session did not start.\n", relay)
		ref.code, ref.reason = wire.TerminalLoginRefused, "relay login refused"
	default:
		ref.banner = fmt.Sprintf("koko: koko could not connect to %s; this SSH Session did not start.\n", relay)
		ref.code, ref.reason = wire.TerminalHostConnectionFailed, "relay connection failed"
	}
	return ref
}

// hostLeg is the Gateway's SSH connection to the Host, dialled while the
// client's handshake runs: done is closed once conn or err holds the
// outcome.
type hostLeg struct {
	done chan struct{}
	conn *hostconn.Conn
	err  error
}

// session runs the SSH Session inside the Tunnel t to the Host a admitted:
// it dials the Host, through its Relay if any, while the client's handshake
// runs, answers the client's
// "none" with the outcome, then relays the client's channels to the Host
// until either end closes the SSH Session or the Tunnel ends. Once the SSH
// Session starts, it calls stopEarlyClose, which stops the Tunnel's end
// from closing the stream before the relay has given its notice.
func (g *Gateway) session(t *tunnel, sshName string, a admission, stopEarlyClose func() bool) {
	var leg *hostLeg
	if a.refused == nil {
		leg = &hostLeg{done: make(chan struct{})}
		target := kokoTarget(a.host, a.kokoKey)
		var relay *hostconn.Target
		if r := a.host.Relay; r != nil {
			relay = new(kokoTarget(r.Reach, a.kokoKey))
		}
		g.wg.Go(func() {
			defer close(leg.done)
			leg.conn, leg.err = g.connector.Connect(t.ctx, target, relay)
		})
		defer func() {
			<-leg.done
			if leg.conn != nil {
				_ = leg.conn.Close()
			}
		}()
	}
	// Before the wait for the Host above: a client that gives up during the
	// dial stops it.
	defer t.end(errEnded)

	// refused is why the client's "none" was refused, if it was.
	refused := a.refused
	cfg := &ssh.ServerConfig{
		Config:        ssh.Config{Ciphers: wire.TunnelCiphers},
		ServerVersion: wire.Version,
		NoClientAuth:  true,
		NoClientAuthCallback: func(md ssh.ConnMetadata) (*ssh.Permissions, error) {
			if refused == nil {
				refused = userRefusal(a.host, md.User())
			}
			if refused != nil {
				return nil, &ssh.BannerError{Err: errRefused, Message: refused.banner}
			}
			select {
			case <-leg.done:
			case <-t.ctx.Done():
				return nil, context.Cause(t.ctx)
			}
			if leg.err != nil {
				refused = connectRefusal(a.host, leg.err)
				return nil, &ssh.BannerError{Err: leg.err, Message: refused.banner}
			}
			return nil, nil
		},
	}
	cfg.AddHostKey(a.hostKey)
	counted := &countingConn{Conn: wire.TunnelConn(t.stream, wire.TunnelAddr("gateway"), t.from)}
	client, chans, reqs, err := ssh.NewServerConn(counted, cfg)
	if err != nil {
		if refused == nil {
			g.log.Debug("ssh session ended before it started", slices.Concat(t.named(), []any{"name", sshName}, callerAttrs(t.caller), []any{"addr", t.from, "err", err})...)
			return
		}
		g.logRefused(t, sshName, refused)
		return
	}

	// The relay closes the stream from now on, after its notice. Should the
	// Tunnel have ended just before, the stream is closing with the same
	// notice, which the relay then gives in vain.
	stopEarlyClose()
	started := time.Now()
	who := sessionAttrs(t, a)
	g.log.Info("ssh session started", slices.Concat(who, []any{"addr", t.from})...)
	s := &sshSession{
		host: leg.conn, hostName: a.host.Name, relayName: relayName(a.host), stream: t.stream,
		log:    g.log.With("tunnel", t.id, "client", t.client, "host", a.host.Name),
		logged: map[string]bool{}, sessionChannels: map[ssh.Channel]struct{}{},
	}
	end := s.relay(t.ctx, client, chans, reqs)
	// The Tunnel ends as its SSH Session did, which a resume refused after
	// tells (resume.go).
	t.end(end)
	g.log.Info("ssh session ended", slices.Concat(who, []any{
		"duration", time.Since(started).Round(time.Millisecond),
		"bytes_from_device", counted.read.Load(), "bytes_to_device", counted.written.Load(),
	}, s.exit(), []any{"reason", end.reason})...)
}

// sessionAttrs are the fields that the started and ended lines of the SSH
// Session in the Tunnel t, to the Host a admitted, begin with. The log is
// the Owner's, so it names the Relay whoever connects.
func sessionAttrs(t *tunnel, a admission) []any {
	who := slices.Concat(t.named(), []any{"user", a.who.User.Name}, callerAttrs(t.caller), []any{"host", a.host.Name, "host_id", a.host.ID})
	if r := a.host.Relay; r != nil {
		who = append(who, "relay", r.Name, "relay_id", r.ID)
	}
	return who
}

// relayName is how a message to the caller names the Relay of the Host h
// (host.Relay.Of), or "" for a Host without one.
func relayName(h host.Reach) string {
	if h.Relay == nil {
		return ""
	}
	return h.Relay.Of(h.Name)
}

// logRefused logs that the Gateway refused the SSH Session in the Tunnel t
// to the Host named name, as r says: at warning level for a refusal the
// Owner may have to act on.
func (g *Gateway) logRefused(t *tunnel, name string, r *refusal) {
	level := slog.LevelInfo
	if r.warn {
		level = slog.LevelWarn
	}
	g.log.Log(t.ctx, level, "ssh session refused", slices.Concat(t.named(), []any{"name", name}, callerAttrs(t.caller), []any{"addr", t.from, "reason", r.reason}, r.attrs)...)
}

// kokoTarget is how the Gateway logs in to the Host or Relay h: as its Host
// Account, with the koko key kokoKey, checking its pinned host key.
func kokoTarget(h host.Reach, kokoKey ssh.Signer) hostconn.Target {
	return hostconn.Target{Address: h.Address, Port: h.Port, Account: h.Account, Login: hostconn.KeyLogin(kokoKey), Pin: hostconn.PinKey(h.Pin)}
}

// deviceSession names who a Tunnel came from in a log line: its Device
// Session's id, or the Unix socket, as other lines name who acted.
func deviceSession(c op.Caller) string {
	if c.DeviceSession == "" {
		return "unix socket"
	}
	return c.DeviceSession
}

// countingConn counts the bytes of a Tunnel each way.
type countingConn struct {
	net.Conn
	read, written atomic.Int64
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.read.Add(int64(n))
	return n, err
}

func (c *countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.written.Add(int64(n))
	return n, err
}
