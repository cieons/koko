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

// Uninstall stops the koko service and the upgrade helper's path unit and
// removes their units, then kokod and the helper's directory, and keeps the data directory, the Root Key's file and the koko user: there is
// no purge, since P0 has no backup (P0 spec: install and run). It never
// removes /usr/local/bin/koko, the CLI, which install.sh put there: on a
// machine as v0.1.0 or v0.2.0 left it, whose unit ran that file, it removes
// only the unit. It reports the port the koko service listened on, and the
// web listener's address its unit set, for install to take again. What is
// not there is not removed, so running it again removes nothing.
func (in *Installer) Uninstall(ctx context.Context) (install.UninstallReport, error) {
	if err := in.checkRoot("uninstall"); err != nil {
		return install.UninstallReport{}, err
	}
	report := install.UninstallReport{Removed: []string{}, DataDir: DataDir, RootKeyFile: RootKeyFile, SystemUser: User}
	helper, err := in.exists(UpgradePathUnit)
	if err != nil {
		return install.UninstallReport{}, err
	}
	if helper {
		// It stops, and leaves the boot, while systemd still knows it, so
		// that no request starts the helper any more.
		if err := in.systemctl(ctx, "disable", "--now", UpgradePathName); err != nil {
			return install.UninstallReport{}, err
		}
	}
	unit, err := in.exists(UnitPath)
	if err != nil {
		return install.UninstallReport{}, err
	}
	if unit {
		// A port systemd does not tell stays unknown (0), and does not stop
		// the uninstall.
		report.ListenPort, _ = in.servicePort(ctx)
		// The unit is the one place install keeps it, and it is about to go.
		// One that cannot be read stays unknown (""), left out of the hint
		// as the port is, and does not stop the uninstall either.
		report.WebListen, _ = in.unitWebListen()
		// The service stops, and leaves the boot, while systemd still knows
		// its unit, and systemd forgets the unit once it is gone.
		for _, args := range [][]string{{"stop", UnitName}, {"disable", UnitName}} {
			if err := in.systemctl(ctx, args...); err != nil {
				return install.UninstallReport{}, err
			}
		}
	}
	reload := false
	for _, file := range []string{UnitPath, UpgradePathUnit, UpgradeServiceUnit} {
		removed, err := in.removeIfThere(file)
		if err != nil {
			return install.UninstallReport{}, err
		}
		if removed {
			report.Removed = append(report.Removed, file)
			reload = true
		}
	}
	// systemd forgets the units once they are gone.
	if reload {
		if err := in.systemctl(ctx, "daemon-reload"); err != nil {
			return install.UninstallReport{}, err
		}
	}
	removed, err := in.removeIfThere(BinaryPath)
	if err != nil {
		return install.UninstallReport{}, err
	}
	if removed {
		report.Removed = append(report.Removed, BinaryPath)
	}
	// The helper's directory holds the kokod it replaced last and the
	// outcome of the last upgrade, which mean nothing without kokod.
	switch _, err := os.Lstat(in.path(UpgradeDir)); {
	case err == nil:
		if err := os.RemoveAll(in.path(UpgradeDir)); err != nil {
			return install.UninstallReport{}, fmt.Errorf("remove %s: %w", UpgradeDir, err)
		}
		report.Removed = append(report.Removed, UpgradeDir)
	case !errors.Is(err, fs.ErrNotExist):
		return install.UninstallReport{}, fmt.Errorf("find %s: %w", UpgradeDir, err)
	}
	return report, nil
}

// removeIfThere removes the file p when it is there, and tells whether it
// was.
func (in *Installer) removeIfThere(p string) (bool, error) {
	there, err := in.exists(p)
	if !there || err != nil {
		return false, err
	}
	return true, in.remove(p)
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
