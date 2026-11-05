// Package wire holds what both ends of a connection to the koko server agree
// on: where the Unix socket is, the SSH version string, the channel types, and
// how an HTTP/1.1 exchange runs inside a channel (ADR 0015).
package wire

import (
	"io"
	"net"
)

// SocketPath is the koko server's fixed Unix socket. Every connection on it is
// the Local caller, acting as the Owner (ADR 0008). Tests move it only through
// the socket-path hook (docs/subsystems/code-layout.md); no flag or
// environment variable does.
const SocketPath = "/run/koko/koko.sock"

// Version is the SSH version string of the koko server and the CLI. It carries
// no version number; the koko server reports its version after authentication.
const Version = "SSH-2.0-koko"

// ChannelAPI is the channel type that carries API calls: HTTP/1.1 requests in
// turn, kept alive between calls, so one channel serves a command's calls.
const ChannelAPI = "api@koko"

// Conn turns a channel into a net.Conn for net/http, which needs deadlines a
// channel lacks: it copies the channel's bytes through net.Pipe. Closing either
// end closes both.
func Conn(ch io.ReadWriteCloser) net.Conn {
	inner, outer := net.Pipe()
	go func() {
		_, _ = io.Copy(outer, ch)
		_ = outer.Close()
	}()
	go func() {
		_, _ = io.Copy(ch, outer)
		_ = ch.Close()
	}()
	return inner
}
