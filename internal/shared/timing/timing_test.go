package timing_test

import (
	"testing"
	"time"

	"github.com/cieons/koko/internal/shared/timing"
)

// The zero Timings is the product: each timing is the reach spec's constant,
// or the one its ticket chose.
func TestZeroTimingsAreTheProductsConstants(t *testing.T) {
	got := timing.Timings{}.Resolve()
	want := timing.Timings{
		HostConnectAttempt: 10 * time.Second,
		HostConnectWait:    time.Second,
		InstallSocketWait:  30 * time.Second,
		EnrollAddKey:       30 * time.Second,
		DeviceMuxIdle:      10 * time.Minute,
		DeviceMuxStart:     5 * time.Second,
	}
	if got != want {
		t.Errorf("Timings{}.Resolve() = %+v, want %+v", got, want)
	}
}

// A test shrinks one timing and keeps the product's constant for the rest.
func TestASetTimingReplacesOnlyItsConstant(t *testing.T) {
	got := timing.Timings{HostConnectWait: time.Millisecond}.Resolve()
	want := timing.Timings{
		HostConnectAttempt: 10 * time.Second,
		HostConnectWait:    time.Millisecond,
		InstallSocketWait:  30 * time.Second,
		EnrollAddKey:       30 * time.Second,
		DeviceMuxIdle:      10 * time.Minute,
		DeviceMuxStart:     5 * time.Second,
	}
	if got != want {
		t.Errorf("Resolve() = %+v, want %+v", got, want)
	}
}
