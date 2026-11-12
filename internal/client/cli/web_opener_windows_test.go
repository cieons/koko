package cli_test

import (
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/cieons/koko/internal/harness"
)

// fakeOpener gives koko web on h a fake of the shell's "open", through the
// browser hook: the shell asks the registry, not PATH, and would open a
// real browser on this machine. opened returns the addresses opened so far.
// koko web waits for the opening before it exits, so each run's is done
// once the run is stopped.
func fakeOpener(t *testing.T, h *harness.Harness) (env []string, opened func() []string) {
	t.Helper()
	var mu sync.Mutex
	var got []string
	h.OpenBrowserWith(func(address string) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, address)
		return nil
	})
	return nil, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(got)
	}
}

// noOpener gives koko web on h a shell whose "open" fails, as on a machine
// without a browser.
func noOpener(t *testing.T, h *harness.Harness) (env []string) {
	t.Helper()
	h.OpenBrowserWith(func(string) error { return errors.New("no program opens http addresses") })
	return nil
}
