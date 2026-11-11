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

// kokod run reports the version of its own file, as the machine reads its
// build, and the koko server upgrades from it.
func TestRunReportsTheVersionOfItsOwnFile(t *testing.T) {
	t.Parallel()
	m := harness.NewMachine(t)
	b := harness.KokodBuild()
	b.Version = "v0.4.0"
	file := m.NewKokod(b)
	socket := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	p := harness.KokodServing(t, harness.SocketPath(socket))
	p.Machine, p.Binary = m, func() (string, error) { return file, nil }
	run := harness.RunKokodBackground(t, p, nil,
		"run", "--data-dir", filepath.Join(t.TempDir(), "data"), "--root-key-file", filepath.Join(t.TempDir(), "root.key"),
		"--owner", "alice", "--address", "koko.example.com")
	if s := waitServing(t, run, socket); s.Version != "v0.4.0" {
		t.Errorf("server show names the version %q, want v0.4.0, its file's", s.Version)
	}
}
