package devicemux

import "golang.org/x/sys/unix"

// runtimeVar names the user's private runtime directory on macOS, which
// launchd sets for the user's sessions.
const runtimeVar = "TMPDIR"

// peerUIDOf reads the OS user id of the peer of the Unix socket fd
// (LOCAL_PEERCRED, which getpeereid reads).
func peerUIDOf(fd uintptr) (int, error) {
	cred, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil {
		return 0, err
	}
	return int(cred.Uid), nil
}
