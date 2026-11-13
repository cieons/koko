// Package timing holds koko's P0 timing constants and the timing-constants
// hook, through which tests wait milliseconds where the product waits
// seconds. Every timing is a constant in P0: no flag, setting or environment
// variable changes one (docs/subsystems/code-layout.md, "Test-only hooks").
//
// The constants come from the table in the reach spec's "Liveness and
// timings", and from the tickets that decide one the specs leave open; each
// ticket that uses one adds it here, with its field.
package timing

import (
	"cmp"
	"time"
)

// The product's timings.
const (
	// HostConnectAttempt bounds one attempt to connect to a Host or a Relay:
	// TCP, or the channel through a Relay, and the SSH handshake.
	HostConnectAttempt = 10 * time.Second
	// HostConnectWait is the wait before the second attempt to connect to a
	// Host or a Relay; it doubles before the third.
	HostConnectWait = time.Second
	// HostConnect bounds one whole connect to a Host, every leg through its
	// chain of Relays and every wait included, so that a web terminal's
	// page, which hears nothing while the Gateway dials, has its answer
	// before a proxy cuts a quiet connection at 60 seconds.
	HostConnect = 45 * time.Second
	// HostProbe is how long a connection to a Host or a Relay goes without a
	// byte from it before the koko server probes it with
	// keepalive@openssh.com, wanting a reply, and HostDead before the koko
	// server ends it.
	HostProbe = 15 * time.Second
	HostDead  = 60 * time.Second
	// StopNotice bounds how long a koko server stop waits for its Tunnels to
	// take the Gateway's restart notice, which takes a round trip, before it
	// closes the connections beneath them anyway, which a connection too
	// stalled to take it would never let it do. A revocation waits as long
	// for its SSH Sessions' notice before it closes their Device
	// Connections.
	StopNotice = 5 * time.Second
	// InstallSocketWait bounds how long koko server install waits for the
	// koko service it started to open its Unix socket. A start-up, migration
	// and snapshot included, takes seconds; one that refuses never opens it.
	InstallSocketWait = 30 * time.Second
	// EnrollAddKey bounds how long Enroll, once logged in to a Host, waits
	// for the Host to append the koko key to its authorized_keys: a shell
	// command that takes milliseconds, so a Host that has not answered by
	// then never will.
	EnrollAddKey = 30 * time.Second
	// DeviceMuxIdle is how long a Device Mux runs with nothing open, no
	// command, koko web or koko tunnel connected to it, before it exits.
	DeviceMuxIdle = 10 * time.Minute
	// DeviceMuxStart bounds how long a command waits for the socket of a
	// Device Mux it started, and how long a Device Mux starting waits for
	// the lock of one of its build that is exiting. A start takes
	// milliseconds; past this, the command opens its own Device Connection.
	DeviceMuxStart = 5 * time.Second
	// DeviceProbe is how long a device's Device Connection goes without a
	// byte from the koko server before the device probes it with
	// keepalive@openssh.com, wanting a reply.
	DeviceProbe = 5 * time.Second
	// DeviceDead is how long it goes without any before the device declares
	// it dead and closes it. A probe sent at once, on a wake or a route
	// change, has DeviceDead-DeviceProbe to be answered, as a probe sent
	// after DeviceProbe does.
	DeviceDead = 15 * time.Second
	// ServerProbe is how long a Device Connection goes without a byte from
	// the device before the koko server probes it, and ServerDead before it
	// declares it dead and closes it.
	ServerProbe = 15 * time.Second
	ServerDead  = 45 * time.Second
	// ReconnectFirst is how long the Device Mux waits after its first try
	// to reconnect fails, the first going at once; the wait doubles at each
	// try, up to ReconnectMax (NextReconnect). A command on the direct path
	// tries again the same way.
	ReconnectFirst = 250 * time.Millisecond
	ReconnectMax   = 5 * time.Second
	// CommandWait bounds how long a command waits for the Device
	// Connection, and for the koko server's report on it, before it fails:
	// it cannot reach the koko server.
	CommandWait = 15 * time.Second
	// DeviceConnectAttempt bounds one try to open a Device Connection: TCP,
	// the SSH handshake and the koko server's report. A try against an
	// address that drops packets would otherwise last until the OS gives
	// up, and hold up the tries after it, an address koko connect saved
	// among them.
	DeviceConnectAttempt = 10 * time.Second
	// WakeCheck is how often the Device Mux looks at the clocks for a wake:
	// on Linux and macOS, whose monotonic clock stands still while the
	// device sleeps, the wall clock having moved more than WakeCheck further
	// than the monotonic one since the last look is a wake; on Windows,
	// whose monotonic clock runs on, the gap since the last look having been
	// more than WakeCheck longer than WakeCheck is.
	WakeCheck = time.Second
	// BareNameRefresh is how long the Device Mux waits after it refreshed
	// the bare names in koko's ssh_config, which it does first once it holds
	// its first Device Connection, before it refreshes them again: so a Host
	// someone else records becomes a bare name by itself.
	BareNameRefresh = 10 * time.Minute
	// Grace is how long the Gateway holds a Detached SSH Session, and its
	// Host connection, for the device to Resume its Tunnel, from when the
	// Tunnel's channel failed: as when the koko server declared its Device
	// Connection dead.
	Grace = time.Minute
	// BrowserGrace is how long the Gateway holds a Detached SSH Session
	// opened from a web terminal, and its Host connection, for the page to
	// Resume it, from when its WebSocket closed or was found dead: longer
	// than Grace, since the leg that drops is a browser's, which a phone
	// put away keeps from reconnecting for minutes.
	BrowserGrace = 15 * time.Minute
	// WorkspaceIdle is how long a Workspace's SSH Session goes on with no
	// page attached, from its last page's detach or from its start, before
	// the Gateway ends it: a Sign-in's own idle time (spec #213).
	WorkspaceIdle = 7 * 24 * time.Hour
	// SignInCheck is how often the Gateway looks for web terminals whose
	// Sign-in has expired, to end their SSH Sessions.
	SignInCheck = time.Minute
	// UploadIdle is how long a web terminal's upload goes without a chunk
	// before the web front gives it up and removes its partial file: as
	// long as BrowserGrace, which a page that keeps its uploads across a
	// drop resends a failed chunk within.
	UploadIdle = 15 * time.Minute
	// FirstMessage bounds how long the web front waits for a web
	// terminal's first message once its WebSocket is upgraded: a page sends
	// it at once, and an upgrade that sends none holds nothing for long.
	FirstMessage = 10 * time.Second
	// ResumeExchange bounds how long the device waits for the koko server's
	// answer to a resume once its channel opened; past it, the device tries
	// again, on the next Device Connection should this one have died.
	ResumeExchange = 5 * time.Second
	// ResumeGiveUp is how long a Tunnel tries to Resume, from when its
	// channel failed, before it gives up: time the device is awake, since
	// the monotonic clock it is counted on stands still while the device
	// sleeps. A koko server reached by then Resumes the Tunnel or refuses.
	ResumeGiveUp = 2 * time.Minute
	// PassOn bounds how long the Device Mux, once a Device Connection has
	// ended, lets each channel on it pass on to its command what the koko
	// server sent before, such as the answer to the call that revoked this
	// device, for a command that does not read; and how long koko web, once
	// its connection to the koko server has ended, lets its browser
	// connections pass on what the koko server sent before, for a browser
	// that does not read. On koko disconnect it bounds, too, how long the
	// Device Mux waits for the Tunnels it carries to pass on their close
	// frames.
	PassOn = 5 * time.Second
	// LocalProbe is how long a command's connection to its Device Mux goes
	// without a byte from the other end, on Windows, before that end probes
	// it with keepalive@openssh.com, and LocalAnswer how long the probe then
	// has to be answered before the end declares the connection dead and
	// closes it. Both ends watch it there, since a Unix socket on Windows
	// may never tell one end that the other closed it; a peer on the same
	// machine answers at once, so a silence alone is never counted
	// (liveness.Conn.WatchAnswers).
	LocalProbe  = 2 * time.Second
	LocalAnswer = 5 * time.Second
)

// Timings is the timing-constants hook. Each field is named after its
// constant, and a zero field means that constant, so the zero Timings is the
// product; cmd/koko never sets one.
type Timings struct {
	HostConnectAttempt   time.Duration
	HostConnectWait      time.Duration
	HostConnect          time.Duration
	HostProbe            time.Duration
	HostDead             time.Duration
	StopNotice           time.Duration
	InstallSocketWait    time.Duration
	EnrollAddKey         time.Duration
	DeviceMuxIdle        time.Duration
	DeviceMuxStart       time.Duration
	DeviceProbe          time.Duration
	DeviceDead           time.Duration
	ServerProbe          time.Duration
	ServerDead           time.Duration
	ReconnectFirst       time.Duration
	ReconnectMax         time.Duration
	CommandWait          time.Duration
	DeviceConnectAttempt time.Duration
	WakeCheck            time.Duration
	BareNameRefresh      time.Duration
	Grace                time.Duration
	BrowserGrace         time.Duration
	WorkspaceIdle        time.Duration
	SignInCheck          time.Duration
	UploadIdle           time.Duration
	FirstMessage         time.Duration
	ResumeExchange       time.Duration
	ResumeGiveUp         time.Duration
	PassOn               time.Duration
	LocalProbe           time.Duration
	LocalAnswer          time.Duration

	// Wake and RouteChange stand in for the Device Mux's watches of the
	// device waking from sleep and of a change of its network routes: each
	// value received is one. Nil means the device's own watches. They are
	// signals, not timings, injected here, as the reach spec asks, so that
	// no flag or setting exposes them.
	Wake        <-chan struct{}
	RouteChange <-chan struct{}
}

// Resolve returns t with each zero field set to the product's constant.
func (t Timings) Resolve() Timings {
	t.HostConnectAttempt = cmp.Or(t.HostConnectAttempt, HostConnectAttempt)
	t.HostConnectWait = cmp.Or(t.HostConnectWait, HostConnectWait)
	t.HostConnect = cmp.Or(t.HostConnect, HostConnect)
	t.HostProbe = cmp.Or(t.HostProbe, HostProbe)
	t.HostDead = cmp.Or(t.HostDead, HostDead)
	t.StopNotice = cmp.Or(t.StopNotice, StopNotice)
	t.InstallSocketWait = cmp.Or(t.InstallSocketWait, InstallSocketWait)
	t.EnrollAddKey = cmp.Or(t.EnrollAddKey, EnrollAddKey)
	t.DeviceMuxIdle = cmp.Or(t.DeviceMuxIdle, DeviceMuxIdle)
	t.DeviceMuxStart = cmp.Or(t.DeviceMuxStart, DeviceMuxStart)
	t.DeviceProbe = cmp.Or(t.DeviceProbe, DeviceProbe)
	t.DeviceDead = cmp.Or(t.DeviceDead, DeviceDead)
	t.ServerProbe = cmp.Or(t.ServerProbe, ServerProbe)
	t.ServerDead = cmp.Or(t.ServerDead, ServerDead)
	t.ReconnectFirst = cmp.Or(t.ReconnectFirst, ReconnectFirst)
	t.ReconnectMax = cmp.Or(t.ReconnectMax, ReconnectMax)
	t.CommandWait = cmp.Or(t.CommandWait, CommandWait)
	t.DeviceConnectAttempt = cmp.Or(t.DeviceConnectAttempt, DeviceConnectAttempt)
	t.WakeCheck = cmp.Or(t.WakeCheck, WakeCheck)
	t.BareNameRefresh = cmp.Or(t.BareNameRefresh, BareNameRefresh)
	t.Grace = cmp.Or(t.Grace, Grace)
	t.BrowserGrace = cmp.Or(t.BrowserGrace, BrowserGrace)
	t.WorkspaceIdle = cmp.Or(t.WorkspaceIdle, WorkspaceIdle)
	t.SignInCheck = cmp.Or(t.SignInCheck, SignInCheck)
	t.UploadIdle = cmp.Or(t.UploadIdle, UploadIdle)
	t.FirstMessage = cmp.Or(t.FirstMessage, FirstMessage)
	t.ResumeExchange = cmp.Or(t.ResumeExchange, ResumeExchange)
	t.ResumeGiveUp = cmp.Or(t.ResumeGiveUp, ResumeGiveUp)
	t.PassOn = cmp.Or(t.PassOn, PassOn)
	t.LocalProbe = cmp.Or(t.LocalProbe, LocalProbe)
	t.LocalAnswer = cmp.Or(t.LocalAnswer, LocalAnswer)
	return t
}

// NextReconnect returns how long to wait before the next try to reconnect,
// after waiting wait before the last one: ReconnectFirst after the first
// try, which goes at once (wait 0), then twice the last wait, up to
// ReconnectMax.
func (t Timings) NextReconnect(wait time.Duration) time.Duration {
	if wait == 0 {
		return t.ReconnectFirst
	}
	return min(2*wait, t.ReconnectMax)
}
