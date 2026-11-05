// Package serverconn reaches the koko server. All CLI traffic opens its
// channels (api@koko now; web@koko and tunnel@koko later) through one
// interface, Conn. DialSocket implements it over the Unix socket as the Local
// caller; a direct Device Connection and the Device Mux implement it later,
// so commands written against Conn do not change.
package serverconn

import (
	"context"
	"fmt"
	"io"
	"net"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/shared/wire"
)

// Conn is one way to the koko server.
type Conn interface {
	// OpenChannel opens a channel of the given type, such as wire.ChannelAPI.
	OpenChannel(ctx context.Context, kind string) (io.ReadWriteCloser, error)
	// Close ends the connection and every channel on it.
	Close() error
}

// user is the SSH user name koko sends; it identifies nobody.
const user = "koko"

// DialSocket connects over the koko server's Unix socket at path, as the
// Local caller. The socket pins no host key: it sits in a directory only koko
// can write (ADR 0008). When nothing is at path, the error wraps
// fs.ErrNotExist.
func DialSocket(ctx context.Context, path string) (Conn, error) {
	var d net.Dialer
	nc, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = nc.Close() })
	defer stop()
	conn, chans, reqs, err := ssh.NewClientConn(nc, path, &ssh.ClientConfig{
		User:            user,
		ClientVersion:   wire.Version,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // the socket's directory is its pin
	})
	if err != nil {
		_ = nc.Close()
		return nil, fmt.Errorf("SSH handshake: %w", err)
	}
	return sshConn{ssh.NewClient(conn, chans, reqs)}, nil
}

// sshConn is a Conn over one SSH connection.
type sshConn struct {
	client *ssh.Client
}

// OpenChannel opens a channel, or gives up when ctx ends; a channel the koko
// server grants after that is closed at once.
func (c sshConn) OpenChannel(ctx context.Context, kind string) (io.ReadWriteCloser, error) {
	type opened struct {
		ch   ssh.Channel
		reqs <-chan *ssh.Request
		err  error
	}
	done := make(chan opened, 1)
	go func() {
		ch, reqs, err := c.client.OpenChannel(kind, nil)
		done <- opened{ch, reqs, err}
	}()
	select {
	case o := <-done:
		if o.err != nil {
			return nil, fmt.Errorf("open %s channel: %w", kind, o.err)
		}
		go ssh.DiscardRequests(o.reqs)
		return o.ch, nil
	case <-ctx.Done():
		go func() {
			if o := <-done; o.err == nil {
				go ssh.DiscardRequests(o.reqs)
				_ = o.ch.Close()
			}
		}()
		return nil, fmt.Errorf("open %s channel: %w", kind, ctx.Err())
	}
}

func (c sshConn) Close() error {
	return c.client.Close()
}
