package devicemux

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
)

// runtimeDir is koko's runtime directory: koko/ in the user's private
// directory that runtimeVar names on this OS, or /tmp/koko-<uid>/ without
// it, which ensurePrivate refuses unless it is this user's, mode 0700. An
// OS with no runtimeVar has no Device Mux yet.
func runtimeDir(getenv func(string) string) (string, error) {
	if runtimeVar == "" {
		return "", fmt.Errorf("the Device Mux is not supported on %s yet", runtime.GOOS)
	}
	if d := getenv(runtimeVar); d != "" {
		return filepath.Join(d, "koko"), nil
	}
	return fmt.Sprintf("/tmp/koko-%d", os.Getuid()), nil
}

// peerUID returns the OS user id of the process at the other end of nc, a
// Unix socket connection, as the kernel recorded it when it connected.
func peerUID(nc net.Conn) (int, error) {
	uc, ok := nc.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("read the peer's credentials: %T is not a Unix socket connection", nc)
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("read the peer's credentials: %w", err)
	}
	var uid int
	var uidErr error
	if err := raw.Control(func(fd uintptr) { uid, uidErr = peerUIDOf(fd) }); err != nil {
		return 0, fmt.Errorf("read the peer's credentials: %w", err)
	}
	if uidErr != nil {
		return 0, fmt.Errorf("read the peer's credentials: %w", uidErr)
	}
	return uid, nil
}
