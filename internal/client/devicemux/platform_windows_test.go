package devicemux

import (
	"testing"
	"time"
)

// On Windows a wake is told by the gap between two looks at the clock
// alone, the monotonic clock running on while Windows sleeps: a look a
// minute after the last, where a second was due, slept 59 s.
func TestAWakeIsTheGapBetweenTwoLooks(t *testing.T) {
	last := time.Now()
	if got := slept(last, last.Add(time.Minute), time.Second); got != 59*time.Second {
		t.Errorf("slept = %v, want 59s", got)
	}
	if got := slept(last, last.Add(time.Second), time.Second); got != 0 {
		t.Errorf("slept on time = %v, want 0", got)
	}
}
