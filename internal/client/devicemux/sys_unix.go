//go:build !windows

package devicemux

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/cieons/koko/internal/client/devicefiles"
)

// tryLock takes the lock file at path, private to this user (mode 0600),
// without waiting. It
// returns nil, and no error, when another Device Mux holds it. The lock
// lasts until the file returned is closed or the process ends, so one that
// crashed holds none.
func tryLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the Device Mux lock: %w", err)
	}
	if err := devicefiles.MakePrivate(path); err != nil {
		_ = f.Close()
		return nil, err
	}
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, nil
		}
		return nil, fmt.Errorf("take the Device Mux lock: %w", err)
	}
	return f, nil
}

// startDetached starts the command newCmd makes as a Device Mux: in a
// session of its own, with no controlling terminal, so no hang-up or Ctrl-C
// meant for the command that started it reaches it, and in /, so it holds
// no directory of the user's open.
func startDetached(newCmd func() *exec.Cmd) (*exec.Cmd, error) {
	cmd := newCmd()
	cmd.Dir = "/"
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd, cmd.Start()
}
