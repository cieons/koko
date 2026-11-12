package devicemux

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/cieons/koko/internal/client/devicefiles"
)

// tryLock takes the lock file at path without waiting, with LockFileEx,
// exclusive and failing at once, as Go's own toolchain locks a file. It
// returns nil, and no error, when another Device Mux holds it. The lock
// lasts until the file returned is closed or the process ends. The file
// is private to this user, as koko's own files are.
func tryLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the Device Mux lock: %w", err)
	}
	if err := devicefiles.MakePrivate(path); err != nil {
		_ = f.Close()
		return nil, err
	}
	const all = ^uint32(0) // every byte, as Go's toolchain locks
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, all, all, new(windows.Overlapped))
	if err != nil {
		_ = f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, nil
		}
		return nil, fmt.Errorf("take the Device Mux lock: %w", err)
	}
	return f, nil
}

// startDetached starts the command newCmd makes as a Device Mux, detached
// from the command's console: with no console of its own
// (DETACHED_PROCESS), so neither Ctrl-C nor the console's closing reaches
// it, in a process group of its own, and in the root of the Windows
// directory's drive, so it holds no directory of the user's open. It asks
// to start outside the job object the command runs in, which may end its
// processes when it closes, as some terminals' and CI runners' do; when the
// job forbids that, it starts inside it, with a second command from newCmd.
func startDetached(newCmd func() *exec.Cmd) (*exec.Cmd, error) {
	win, err := windows.GetWindowsDirectory()
	if err != nil {
		return nil, fmt.Errorf("find the Windows directory: %w", err)
	}
	dir := filepath.VolumeName(win) + `\`
	const flags = windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP
	cmd := newCmd()
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags | windows.CREATE_BREAKAWAY_FROM_JOB}
	err = cmd.Start()
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		// A job that does not allow breaking away refuses the start.
		cmd = newCmd()
		cmd.Dir = dir
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
		err = cmd.Start()
	}
	return cmd, err
}
