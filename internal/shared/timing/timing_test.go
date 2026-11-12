package timing_test

import (
	"slices"
	"testing"
	"time"

	"github.com/cieons/koko/internal/shared/timing"
)

// The zero Timings is the product: each timing is the reach spec's constant,
// or the one its ticket chose.
func TestZeroTimingsAreTheProductsConstants(t *testing.T) {
	got := timing.Timings{}.Resolve()
	want := timing.Timings{
		HostConnectAttempt:   10 * time.Second,
		HostConnectWait:      time.Second,
		HostProbe:            15 * time.Second,
		HostDead:             60 * time.Second,
		StopNotice:           5 * time.Second,
		InstallSocketWait:    30 * time.Second,
		EnrollAddKey:         30 * time.Second,
		DeviceMuxIdle:        10 * time.Minute,
		DeviceMuxStart:       5 * time.Second,
		DeviceProbe:          5 * time.Second,
		DeviceDead:           15 * time.Second,
		ServerProbe:          15 * time.Second,
		ServerDead:           45 * time.Second,
		ReconnectFirst:       250 * time.Millisecond,
		ReconnectMax:         5 * time.Second,
		CommandWait:          15 * time.Second,
		DeviceConnectAttempt: 10 * time.Second,
		WakeCheck:            time.Second,
		BareNameRefresh:      10 * time.Minute,
		Grace:                time.Minute,
		BrowserGrace:         15 * time.Minute,
		SignInCheck:          time.Minute,
		FirstMessage:         10 * time.Second,
		ResumeExchange:       5 * time.Second,
		ResumeGiveUp:         2 * time.Minute,
		PassOn:               5 * time.Second,
		LocalProbe:           2 * time.Second,
		LocalAnswer:          5 * time.Second,
	}
	if got != want {
		t.Errorf("Timings{}.Resolve() = %+v, want %+v", got, want)
	}
}

// A test shrinks one timing and keeps the product's constant for the rest.
func TestASetTimingReplacesOnlyItsConstant(t *testing.T) {
	got := timing.Timings{HostConnectWait: time.Millisecond}.Resolve()
	want := timing.Timings{
		HostConnectAttempt:   10 * time.Second,
		HostConnectWait:      time.Millisecond,
		HostProbe:            15 * time.Second,
		HostDead:             60 * time.Second,
		StopNotice:           5 * time.Second,
		InstallSocketWait:    30 * time.Second,
		EnrollAddKey:         30 * time.Second,
		DeviceMuxIdle:        10 * time.Minute,
		DeviceMuxStart:       5 * time.Second,
		DeviceProbe:          5 * time.Second,
		DeviceDead:           15 * time.Second,
		ServerProbe:          15 * time.Second,
		ServerDead:           45 * time.Second,
		ReconnectFirst:       250 * time.Millisecond,
		ReconnectMax:         5 * time.Second,
		CommandWait:          15 * time.Second,
		DeviceConnectAttempt: 10 * time.Second,
		WakeCheck:            time.Second,
		BareNameRefresh:      10 * time.Minute,
		Grace:                time.Minute,
		BrowserGrace:         15 * time.Minute,
		SignInCheck:          time.Minute,
		FirstMessage:         10 * time.Second,
		ResumeExchange:       5 * time.Second,
		ResumeGiveUp:         2 * time.Minute,
		PassOn:               5 * time.Second,
		LocalProbe:           2 * time.Second,
		LocalAnswer:          5 * time.Second,
	}
	if got != want {
		t.Errorf("Resolve() = %+v, want %+v", got, want)
	}
}

// The Device Mux reconnects at once, then waits 250 ms before its next try,
// doubling the wait at each try up to 5 s.
func TestTheReconnectBackoffDoublesFrom250msTo5s(t *testing.T) {
	var got []time.Duration
	wait := time.Duration(0)
	for range 8 {
		wait = timing.Timings{}.Resolve().NextReconnect(wait)
		got = append(got, wait)
	}
	want := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second, 5 * time.Second}
	if !slices.Equal(got, want) {
		t.Errorf("the waits between tries are %v, want %v", got, want)
	}
}
