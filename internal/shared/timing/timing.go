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
	// WakeCheck is how often the Device Mux compares the wall clock with the
	// monotonic clock, which stands still while the device sleeps: the wall
	// clock having moved more than WakeCheck further than the monotonic one
	// since the last look is a wake.
	WakeCheck = time.Second
)

// Timings is the timing-constants hook. Each field is named after its
// constant, and a zero field means that constant, so the zero Timings is the
// product; cmd/koko never sets one.
type Timings struct {
	HostConnectAttempt   time.Duration
	HostConnectWait      time.Duration
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
