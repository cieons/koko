package wire

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// ChannelTunnel is the channel type of one Tunnel, which carries one SSH
// Session between the CLI and the Gateway inside a resumable stream
// (P0 spec: reach, "The Tunnel on the wire"). Its open payload starts with a
// kind: TunnelOpen's "open", or later "resume".
const ChannelTunnel = "tunnel@koko"

// TunnelKindOpen is the kind of a tunnel@koko open payload that opens a new
// Tunnel to the Host an SSH Name names.
const TunnelKindOpen = "open"

// TunnelIDSize is the size of a Tunnel id: 128 random bits the koko server
// picks for each Tunnel it accepts. It sends them as the first TunnelIDSize
// bytes of the stream, so in its first frame, before the SSH Session's
// first byte; a resume names the Tunnel by them.
const TunnelIDSize = 16

// TunnelUser is the SSH user name an SSH client asks the Gateway for inside
// a Tunnel: the Gateway logs in to the Host as its Host Account whatever the
// client asks, so koko's own clients name nobody.
const TunnelUser = "koko"

// TunnelCiphers are the only ciphers the Gateway's SSH server offers inside a
// Tunnel, and koko's own clients ask for: AES-GCM, which costs the least CPU
// per byte relayed (P0 spec: reach, "The Gateway").
var TunnelCiphers = []string{"aes128-gcm@openssh.com", "aes256-gcm@openssh.com"}

// TunnelOpen is the open payload of a tunnel@koko channel that opens a new
// Tunnel to the Host sshName names: in SSH's wire encoding, the string
// "open", then the string sshName, as typed (the Gateway drops a trailing
// ".koko").
func TunnelOpen(sshName string) []byte {
	return ssh.Marshal(struct{ Kind, Name string }{TunnelKindOpen, sshName})
}

// ParseTunnelPayload reads a tunnel@koko channel's open payload: its kind,
// and for an "open", the SSH Name it carries. It refuses a payload that is
// no kind, and an "open" with anything but one SSH Name after it; another
// kind comes back with its payload unread, for the Gateway to refuse until
// it takes that kind.
func ParseTunnelPayload(b []byte) (kind, sshName string, err error) {
	var p struct {
		Kind string
		Rest []byte `ssh:"rest"`
	}
	if err := ssh.Unmarshal(b, &p); err != nil {
		return "", "", errors.New("a tunnel@koko channel's open payload starts with its kind, an SSH string")
	}
	if p.Kind != TunnelKindOpen {
		return p.Kind, "", nil
	}
	var open struct{ Name string }
	if err := ssh.Unmarshal(p.Rest, &open); err != nil {
		return "", "", errors.New(`a tunnel@koko "open" payload carries one SSH Name, an SSH string, and nothing after it`)
	}
	return p.Kind, open.Name, nil
}

// TunnelHost returns the Host name an SSH Name names: the name itself, or
// the name a trailing ".koko" follows, so web1 and web1.koko name the same
// Host.
func TunnelHost(sshName string) string {
	host, _ := strings.CutSuffix(sshName, ".koko")
	return host
}

// TunnelConn is a Tunnel's stream as an SSH client or server runs on it: the
// net.Conn x/crypto/ssh takes, whose addresses are local and remote. A
// stream has no deadlines, so setting one fails.
func TunnelConn(stream io.ReadWriteCloser, local, remote net.Addr) net.Conn {
	return &tunnelConn{ReadWriteCloser: stream, local: local, remote: remote}
}

type tunnelConn struct {
	io.ReadWriteCloser
	local, remote net.Addr
}

func (c *tunnelConn) LocalAddr() net.Addr  { return c.local }
func (c *tunnelConn) RemoteAddr() net.Addr { return c.remote }

var errNoDeadline = fmt.Errorf("a Tunnel has no deadlines: %w", errors.ErrUnsupported)

func (c *tunnelConn) SetDeadline(time.Time) error      { return errNoDeadline }
func (c *tunnelConn) SetReadDeadline(time.Time) error  { return errNoDeadline }
func (c *tunnelConn) SetWriteDeadline(time.Time) error { return errNoDeadline }

// TunnelAddr is the address of one end of a Tunnel, for TunnelConn: what it
// is, such as the Host its SSH Session reaches.
type TunnelAddr string

func (TunnelAddr) Network() string  { return ChannelTunnel }
func (a TunnelAddr) String() string { return string(a) }
