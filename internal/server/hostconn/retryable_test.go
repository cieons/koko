package hostconn

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"

	"golang.org/x/crypto/ssh"
)

// The network errors a test cannot cause on loopback are told apart as the
// ones it can: each is retried, and nothing else is.
func TestRetriesOnlyNetworkErrors(t *testing.T) {
	dial := func(errno syscall.Errno) error {
		return fmt.Errorf("ssh: handshake failed: %w", &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", errno)})
	}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"refused", dial(syscall.ECONNREFUSED), true},
		{"host unreachable", dial(syscall.EHOSTUNREACH), true},
		{"network unreachable", dial(syscall.ENETUNREACH), true},
		{"timed out", dial(syscall.ETIMEDOUT), true},
		{"reset", dial(syscall.ECONNRESET), true},
		{"name lookup timed out", &net.DNSError{Err: "i/o timeout", Name: "web1.example", IsTimeout: true}, true},
		{"Relay could not connect", &ssh.OpenChannelError{Reason: ssh.ConnectionFailed, Message: "connect failed: No route to host"}, true},
		{"no such name", &net.DNSError{Err: "no such host", Name: "web1.example", IsNotFound: true}, false},
		{"Relay forbids forwarding", &ssh.OpenChannelError{Reason: ssh.Prohibited, Message: "administratively prohibited"}, false},
		{"login refused", errors.New("ssh: handshake failed: ssh: unable to authenticate, attempted methods [none publickey], no supported methods remain"), false},
		{"host key mismatch", &HostKeyMismatchError{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryable(tc.err); got != tc.want {
				t.Errorf("retryable = %v, want %v", got, tc.want)
			}
		})
	}
}
