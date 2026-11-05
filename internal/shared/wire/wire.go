// Package wire holds what both ends of a connection to the koko server agree
// on: where the Unix socket is, the SSH version string, the channel types,
// how an HTTP/1.1 exchange runs inside a channel (ADR 0015), and how a join
// carries a Join Code's secret.
package wire

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"

	"github.com/cieons/koko/internal/shared/joincode"
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

// ChannelWeb is the channel type that carries one browser connection koko web
// forwards: HTTP/1.1 to the koko server's web front, which asks for its
// cookie. Its open payload is WebPayload.
const ChannelWeb = "web@koko"

// WebPayload is the open payload of a web@koko channel: the port koko web
// listens on, on 127.0.0.1, as an SSH uint32 (4 bytes, big-endian). The web
// front answers only a Host header that names it.
func WebPayload(port int) []byte {
	return binary.BigEndian.AppendUint32(nil, uint32(port))
}

// ParseWebPayload returns the port a web@koko channel's open payload names,
// refusing anything but a TCP port in exactly 4 bytes.
func ParseWebPayload(b []byte) (int, error) {
	if len(b) != 4 {
		return 0, errors.New("a web@koko channel names the port koko web listens on, in 4 bytes")
	}
	port := binary.BigEndian.Uint32(b)
	if port < 1 || port > 65535 {
		return 0, errors.New("a web@koko channel names the port koko web listens on, from 1 to 65535")
	}
	return int(port), nil
}

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

// JoinSecret is the SSH password a join's second step sends: the Join Code's
// secret in lowercase hex. The first step is public key authentication with
// the device's new key, which the koko server answers with a partial success
// (P0 spec: people and Spaces, "Joining").
func JoinSecret(secret [joincode.SecretSize]byte) string {
	return hex.EncodeToString(secret[:])
}

// ParseJoinSecret reads the secret from a password JoinSecret wrote; ok is
// false for any other password.
func ParseJoinSecret(password []byte) (secret [joincode.SecretSize]byte, ok bool) {
	if hex.EncodedLen(len(secret)) != len(password) {
		return secret, false
	}
	if _, err := hex.Decode(secret[:], password); err != nil {
		return secret, false
	}
	// Only the lowercase form JoinSecret writes.
	return secret, hex.EncodeToString(secret[:]) == string(password)
}
