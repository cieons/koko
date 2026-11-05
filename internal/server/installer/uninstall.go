package installer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cieons/koko/internal/shared/install"
)

// Uninstall stops the koko service and removes its unit, then its binary,
// and keeps the data directory, the Root Key's file and the koko user: there
// is no purge, since P0 has no backup (P0 spec: install and run). It
// reports the port the koko service listened on, for install to take again.
// What is not there is not removed, so running it again removes nothing.
func (in *Installer) Uninstall(ctx context.Context) (install.UninstallReport, error) {
	report := install.UninstallReport{Removed: []string{}, DataDir: DataDir, RootKeyFile: RootKeyFile, SystemUser: User}
	unit, err := in.exists(UnitPath)
	if err != nil {
		return install.UninstallReport{}, err
	}
	if unit {
		// A port systemd does not tell stays unknown (0), and does not stop
		// the uninstall.
		report.ListenPort, _ = in.servicePort(ctx)
		// The service stops, and leaves the boot, while systemd still knows
		// its unit, and systemd forgets the unit once it is gone.
		for _, args := range [][]string{{"stop", UnitName}, {"disable", UnitName}} {
			if err := in.systemctl(ctx, args...); err != nil {
				return install.UninstallReport{}, err
			}
		}
		if err := in.remove(UnitPath); err != nil {
			return install.UninstallReport{}, err
		}
		report.Removed = append(report.Removed, UnitPath)
		if err := in.systemctl(ctx, "daemon-reload"); err != nil {
			return install.UninstallReport{}, err
		}
	}
	binary, err := in.exists(BinaryPath)
	if err != nil {
		return install.UninstallReport{}, err
	}
	if binary {
		if err := in.remove(BinaryPath); err != nil {
			return install.UninstallReport{}, err
		}
		report.Removed = append(report.Removed, BinaryPath)
	}
	return report, nil
}

// exists tells whether the file p is there.
func (in *Installer) exists(p string) (bool, error) {
	switch _, err := os.Lstat(in.path(p)); {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("find %s: %w", p, err)
	}
	return true, nil
}

// remove removes the file p, which koko owns, so that its removal lasts.
func (in *Installer) remove(p string) error {
	real := in.path(p)
	if err := os.Remove(real); err != nil {
		return err
	}
	if err := syncDir(filepath.Dir(real)); err != nil {
		return fmt.Errorf("remove %s: %w", p, err)
	}
	return nil
}
