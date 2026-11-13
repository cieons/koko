package server

import "testing"

// The web terminals' emulators are bounded by the figure given, else by a
// quarter of the memory the koko server may use, at most 2 GiB, and by
// 2 GiB when it cannot tell.
func TestTheTerminalsMemoryBound(t *testing.T) {
	t.Parallel()
	machine := func(n uint64) func() uint64 { return func() uint64 { return n } }
	for _, c := range []struct {
		name string
		cfg  Config
		want int
	}{
		{"given", Config{TerminalMemory: 512 << 20, MachineMemory: machine(64 << 30)}, 512 << 20},
		{"a small machine", Config{MachineMemory: machine(1 << 30)}, 256 << 20},
		{"a large machine", Config{MachineMemory: machine(64 << 30)}, 2 << 30},
		{"a machine that cannot tell", Config{MachineMemory: machine(0)}, 2 << 30},
		{"no machine", Config{}, 2 << 30},
	} {
		if got := terminalMemory(c.cfg); got != c.want {
			t.Errorf("%s: the bound is %d MiB, want %d MiB", c.name, got>>20, c.want>>20)
		}
	}
}
