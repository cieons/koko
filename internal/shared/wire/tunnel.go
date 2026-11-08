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
// kind: TunnelOpen's "open", or TunnelResume's "resume".
const ChannelTunnel = "tunnel@koko"

// The kinds of a tunnel@koko open payload: "open" opens a new Tunnel to the
// Host an SSH Name names, and "resume" Resumes a Tunnel the koko server
// holds.
const (
	TunnelKindOpen   = "open"
	TunnelKindResume = "resume"
)

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

// TunnelResume is the open payload of a tunnel@koko channel that Resumes
// the Tunnel whose id is id, the device having received received bytes of
// its stream, the Tunnel id's included: in SSH's wire encoding, the string
// "resume", the id's TunnelIDSize bytes as they are, then received as a
// uint64. The koko server answers on the channel with its own received
// offset (package resumable).
func TunnelResume(id [TunnelIDSize]byte, received uint64) []byte {
	return ssh.Marshal(struct {
		Kind     string
		ID       [TunnelIDSize]byte
		Received uint64
	}{TunnelKindResume, id, received})
}

// TunnelPayload is a tunnel@koko channel's open payload, as
// ParseTunnelPayload reads it.
type TunnelPayload struct {
	Kind string
	// SSHName is what an "open" carries.
	SSHName string
	// ID and Received are what a "resume" carries.
	ID       [TunnelIDSize]byte
	Received uint64
}

// ParseTunnelPayload reads a tunnel@koko channel's open payload: its kind,
// and what an "open" or a "resume" carries. It refuses a payload that is no
// kind, an "open" with anything but one SSH Name after it, and a "resume"
// with anything but an id and an offset after it; another kind comes back
// with its payload unread, for the Gateway to refuse.
func ParseTunnelPayload(b []byte) (TunnelPayload, error) {
	var p struct {
		Kind string
		Rest []byte `ssh:"rest"`
	}
	if err := ssh.Unmarshal(b, &p); err != nil {
		return TunnelPayload{}, errors.New("a tunnel@koko channel's open payload starts with its kind, an SSH string")
	}
	switch p.Kind {
	case TunnelKindOpen:
		var open struct{ Name string }
		if err := ssh.Unmarshal(p.Rest, &open); err != nil {
			return TunnelPayload{}, errors.New(`a tunnel@koko "open" payload carries one SSH Name, an SSH string, and nothing after it`)
		}
		return TunnelPayload{Kind: p.Kind, SSHName: open.Name}, nil
	case TunnelKindResume:
		var resume struct {
			ID       [TunnelIDSize]byte
			Received uint64
		}
		if err := ssh.Unmarshal(p.Rest, &resume); err != nil {
			return TunnelPayload{}, fmt.Errorf(`a tunnel@koko "resume" payload carries a Tunnel id, %d bytes, then an offset, a uint64, and nothing after them`, TunnelIDSize)
		}
		return TunnelPayload{Kind: p.Kind, ID: resume.ID, Received: resume.Received}, nil
	}
	return TunnelPayload{Kind: p.Kind}, nil
}

// TunnelNoticeLine is how the Gateway writes the notice of an SSH Session's
// end, one sentence, on the stderr of each of its session channels, the
// notice also being its Tunnel's close-frame reason: koko exec, which shows
// that stderr, so knows whether the notice reached it.
func TunnelNoticeLine(notice string) string {
	return "koko: " + notice + "\n"
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
