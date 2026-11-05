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
)

// Timings is the timing-constants hook. Each field is named after its
// constant, and a zero field means that constant, so the zero Timings is the
// product; cmd/koko never sets one.
type Timings struct {
	HostConnectAttempt time.Duration
	HostConnectWait    time.Duration
	InstallSocketWait  time.Duration
	EnrollAddKey       time.Duration
	DeviceMuxIdle      time.Duration
	DeviceMuxStart     time.Duration
}

// Resolve returns t with each zero field set to the product's constant.
func (t Timings) Resolve() Timings {
	t.HostConnectAttempt = cmp.Or(t.HostConnectAttempt, HostConnectAttempt)
	t.HostConnectWait = cmp.Or(t.HostConnectWait, HostConnectWait)
	t.InstallSocketWait = cmp.Or(t.InstallSocketWait, InstallSocketWait)
	t.EnrollAddKey = cmp.Or(t.EnrollAddKey, EnrollAddKey)
	t.DeviceMuxIdle = cmp.Or(t.DeviceMuxIdle, DeviceMuxIdle)
	t.DeviceMuxStart = cmp.Or(t.DeviceMuxStart, DeviceMuxStart)
	return t
}
