// Package wire holds what both ends of a connection to the koko server agree
// on: where the Unix socket is, the SSH version string, the version and
// operations report, the channel types, how an HTTP/1.1 exchange runs inside
// a channel (ADR 0015), how a join carries a Join Code's secret, and how a
// Revoked device is told so.
package wire

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"

	"github.com/cieons/koko/internal/shared/joincode"
	"github.com/cieons/koko/internal/shared/name"
)

// SocketPath is the koko server's fixed Unix socket. Every connection on it is
// the Local caller, acting as the Owner (ADR 0008). Tests move it only through
// the socket-path hook (docs/subsystems/code-layout.md); no flag or
// environment variable does.
const SocketPath = "/run/koko/koko.sock"

// Version is koko's SSH version string. The koko server, the Gateway and koko's
// connections to Hosts send it with no version number: the koko server
// reports its version after authentication. The CLI's connections to the
// koko server add the CLI's version as a comment (compat.SSHVersion).
const Version = "SSH-2.0-koko"

// RequestReport is the SSH global request that carries the koko server's
// version and operations report, its payload Report in JSON. The koko server
// sends it on every connection, wanting no reply, as soon as the connection
// has authenticated and before it serves any channel (ADR 0027); so the
// report reaches the CLI before the answer to its first channel open, and
// costs no round trip.
const RequestReport = "report@koko"

// RequestRevoked is the SSH global request the koko server sends a Device
// Connection, wanting no reply and with no payload, just before it closes the
// connection because its Device Session was Revoked: the revocation
// disconnect, which tells whoever holds the connection to run the revocation
// cleanup. A device that misses it learns the same from RevokedBanner on
// its next connection.
const RequestRevoked = "revoked@koko"

// RequestDisconnected is the SSH global request a Device Mux sends each
// command connected to it, wanting no reply and with no payload, just before
// koko disconnect stops it: what it carried ends on purpose, and an SSH
// Session among it ends for good rather than Resume.
const RequestDisconnected = "disconnected@koko"

// Report is the koko server's version and operations report. Within /v1 a
// koko server only adds operations, so a CLI calls an operation only when
// the report lists it, and otherwise says the koko server is too old.
type Report struct {
	// Version is the koko server's build version, as koko version prints it.
	Version string `json:"version"`
	// Operations names every operation the koko server serves, such as
	// "space.add".
	Operations []string `json:"operations"`
	// Oldest is the oldest koko the koko server works with
	// (compat.Oldest): it refuses every call and channel of a CLI below
	// it, and a CLI that reads it fails every command itself. A koko
	// server from before the window reports none.
	Oldest string `json:"oldest,omitempty"`
}

// Marshal encodes the report as RequestReport's payload.
func (r Report) Marshal() []byte {
	b, err := json.Marshal(r)
	if err != nil {
		panic(fmt.Sprintf("wire: encode the report: %v", err)) // strings only: never fails
	}
	return b
}

// ParseReport reads RequestReport's payload, ignoring fields a later koko
// server adds.
func ParseReport(b []byte) (Report, error) {
	var r Report
	if err := json.Unmarshal(b, &r); err != nil {
		return Report{}, fmt.Errorf("read the koko server's report: %w", err)
	}
	if r.Version == "" {
		return Report{}, errors.New("read the koko server's report: it names no version")
	}
	return r, nil
}

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

// Conn turns a channel into a net.Conn for net/http. Reads come through
// net.Pipe, for the read deadlines net/http needs and a channel lacks. A
// Write goes to the channel itself, so an answer net/http has written has
// gone to the SSH connection; write deadlines are not kept. Closing either
// end closes both.
func Conn(ch io.ReadWriteCloser) net.Conn {
	inner, outer := net.Pipe()
	go func() {
		_, _ = io.Copy(outer, ch)
		_ = outer.Close()
	}()
	return &channelConn{Conn: inner, ch: ch}
}

// channelConn is a channel as Conn returns it: reads and deadlines come from
// the pipe, writes go to the channel.
type channelConn struct {
	net.Conn
	ch io.ReadWriteCloser
	// writeMu serialises writes, which an SSH channel does not.
	writeMu sync.Mutex
}

func (c *channelConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.ch.Write(p)
}

// Close closes the pipe at once and the channel after it: closing an SSH
// channel can wait on its connection, which net/http's Shutdown and Close,
// holding their lock, must not.
func (c *channelConn) Close() error {
	go func() { _ = c.ch.Close() }()
	return c.Conn.Close()
}

// JoinSecret is the SSH password a join's second step sends: the Join Code's
// secret in lowercase hex. The first step is public key authentication with
// the device's new key, which the koko server answers with a partial success
// (P0 spec: people and Spaces, "Joining").
func JoinSecret(secret [joincode.SecretSize]byte) string {
	return hex.EncodeToString(secret[:])
}

// The parts of RevokedBanner around the device name.
const (
	revokedPrefix = `koko: the Device Session "`
	revokedSuffix = `" of this device was revoked.` + "\n"
)

// RevokedBanner is the SSH banner the koko server answers a Revoked Device
// Session's key with, once the device has signed with it: the device is
// told by name that it was cut off (P0 spec: reach).
func RevokedBanner(device string) string {
	return revokedPrefix + device + revokedSuffix
}

// ParseRevokedBanner returns the device name a RevokedBanner names; ok is
// false for any other banner.
func ParseRevokedBanner(banner string) (device string, ok bool) {
	rest, ok := strings.CutPrefix(banner, revokedPrefix)
	if !ok {
		return "", false
	}
	device, ok = strings.CutSuffix(rest, revokedSuffix)
	if !ok || name.Check(device) != nil {
		return "", false
	}
	return device, true
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
