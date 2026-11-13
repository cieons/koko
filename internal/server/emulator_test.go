package server_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

// The koko server serves as soon as it starts, while the web terminals'
// emulator still compiles, and logs which engine runs it once the compile
// ends (#222).
func TestTheKokoServerServesWhileTheEmulatorCompiles(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	h := harness.Start(t, harness.Emulator(func(context.Context) (string, error) {
		select {
		case <-release:
		// A start that waits for the compile gets on after this, and the
		// test fails below rather than hangs.
		case <-time.After(10 * time.Second):
		}
		return "compiler", nil
	}))
	// Start initialised the koko server over its socket.
	if _, err := h.Call(api.ServerShow, api.ServerShowRequest{}); err != nil {
		t.Fatalf("server.show while the emulator compiles: %v", err)
	}
	const logged = `msg="web terminal emulator" engine=compiler memory_bound_mib=`
	if strings.Contains(h.Log(), logged) {
		t.Fatal("the koko server logged the emulator's engine before its compile ended")
	}

	close(release)
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(h.Log(), logged) {
		if time.Now().After(deadline) {
			t.Fatalf("the koko server logged no %s once the compile ended:\n%s", logged, h.Log())
		}
		time.Sleep(5 * time.Millisecond)
	}
}
