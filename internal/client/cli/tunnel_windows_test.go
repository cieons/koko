package cli_test

import (
	"testing"

	"github.com/cieons/koko/internal/harness"
)

// On Windows koko tunnel reads its SSH Name with or without one pair of
// single quotes around it: OpenSSH for Windows passes the '%h' of koko's
// ssh_config with its quotes, Git for Windows' ssh without them.
func TestKokoTunnelReadsAQuotedSSHNameOnWindows(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	managedHost(t, h, "web1")
	pinned := gatewayHostKey(t, h)
	for _, arg := range []string{"'web1.koko'", "web1.koko", "'web1'"} {
		p := h.Proxy("tunnel", arg)
		c := sshOver(t, p, "web1.koko", pinned)
		s, err := c.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		if out, err := s.Output("echo reached"); err != nil || string(out) != "reached\n" {
			t.Errorf("koko tunnel %s printed %q (%v), want it to reach web1", arg, out, err)
		}
		_ = c.Close()
		if r := p.Wait(); r.Status != 0 || r.Stderr != "" {
			t.Errorf("koko tunnel %s exited %d with stderr %q once ssh closed; want 0 and nothing", arg, r.Status, r.Stderr)
		}
	}
}
