package devicemux

import "golang.org/x/sys/unix"

// runtimeVar names the user's private runtime directory on Linux.
const runtimeVar = "XDG_RUNTIME_DIR"

// peerUIDOf reads the OS user id of the peer of the Unix socket fd
// (SO_PEERCRED).
func peerUIDOf(fd uintptr) (int, error) {
	cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return 0, err
	}
	return int(cred.Uid), nil
}
