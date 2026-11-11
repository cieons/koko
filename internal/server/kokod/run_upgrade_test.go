//go:build unix

package kokod_test

import (
	"path/filepath"
	"testing"

	"github.com/cieons/koko/internal/harness"
)

// kokod run in the container image, whose KOKO_IMAGE names it, is upgraded
// by a new image, and by hand without it: server show says which.
func TestRunTakesItsUpgradeWayFromKokoImage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, way string
		env       []string
	}{
		{"in the image", "image", []string{"KOKO_IMAGE=ghcr.io/cieons/koko"}},
		{"by hand", "none", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			socket := filepath.Join(harness.ShortTempDir(t), "koko.sock")
			p := harness.RunKokodBackground(t, harness.KokodServing(t, harness.SocketPath(socket)), tc.env,
				"run", "--data-dir", filepath.Join(t.TempDir(), "data"), "--root-key-file", filepath.Join(t.TempDir(), "root.key"),
				"--owner", "alice", "--address", "koko.example.com")
			s := waitServing(t, p, socket)
			if s.UpgradeWay != tc.way || s.LastUpgrade != nil {
				t.Errorf("server show names the upgrade way %q and the last upgrade %+v; want %s and null", s.UpgradeWay, s.LastUpgrade, tc.way)
			}
		})
	}
}
