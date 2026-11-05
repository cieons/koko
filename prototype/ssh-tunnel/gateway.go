// PROTOTYPE, throwaway: never merged. See README.md.
//
// gateway.go is the Gateway: it ends the SSH Session that arrives inside a
// Tunnel, accepting `none` authentication because the Device Connection
// already says who is on it, opens its own SSH connection to the Host with
// the koko key, and relays channels and requests between the two.
package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// gatewaySession is one SSH Session through the Gateway.
type gatewaySession struct {
	name string

	mu    sync.Mutex
	shell []ssh.Channel // client-side session channels, for notices
}

func (g *gatewaySession) notify(msg string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, ch := range g.shell {
		io.WriteString(ch.Stderr(), msg)
	}
}

type upstream struct {
	conn         ssh.Conn
	chans        <-chan ssh.NewChannel
	reqs         <-chan *ssh.Request
	err          error
	dial, handsh time.Duration
}

func (s *server) dialHost(h hostRecord) upstream {
	t := time.Now()
	c, err := net.DialTimeout("tcp", h.addr, 10*time.Second)
	if err != nil {
		return upstream{err: err}
	}
	dial := time.Since(t)
	t = time.Now()
	conn, chans, reqs, err := ssh.NewClientConn(c, h.addr, &ssh.ClientConfig{
		User:            h.account,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(s.kokoKey)},
		HostKeyCallback: ssh.FixedHostKey(h.hostKey),
		Timeout:         10 * time.Second,
	})
	if err != nil {
		c.Close()
		return upstream{err: err}
	}
	return upstream{conn: conn, chans: chans, reqs: reqs, dial: dial, handsh: time.Since(t)}
}

func (s *server) gateway(dc *deviceConn, ch ssh.Channel, h hostRecord, opened time.Time) {
	var up upstream
	upDone := make(chan struct{})
	startUp := func() {
		go func() { up = s.dialHost(h); close(upDone) }()
	}
	if !s.serial {
		startUp() // overlap the Host login with the inner handshake
	}
	var innerKex time.Duration
	cfg := &ssh.ServerConfig{
		Ciphers:      s.ciphers, // promoted from ssh.Config
		NoClientAuth: true,
		NoClientAuthCallback: func(m ssh.ConnMetadata) (*ssh.Permissions, error) {
			innerKex = time.Since(opened)
			log.Printf("gateway %s: auth none from %s, ssh user %q, client %q", h.name, dc.device, m.User(), m.ClientVersion())
			if s.serial {
				startUp()
			}
			<-upDone
			if up.err != nil {
				return nil, &ssh.BannerError{Err: up.err, Message: fmt.Sprintf("koko: cannot reach %s: %v\r\n", h.name, up.err)}
			}
			return &ssh.Permissions{}, nil
		},
	}
	cfg.AddHostKey(s.hostKey)

	sconn, chans, reqs, err := ssh.NewServerConn(chanConn{ch}, cfg)
	if err != nil {
		log.Printf("gateway %s: inner handshake for %s failed after %v: %v", h.name, dc.device, time.Since(opened).Round(time.Microsecond), err)
		ch.Close()
		go func() {
			<-upDone
			if up.conn != nil {
				up.conn.Close()
			}
		}()
		return
	}
	defer sconn.Close()
	defer up.conn.Close()
	log.Printf("gateway %s: SSH Session for %s ready %v after the Tunnel opened (inner kex %v; Host dial %v, Host handshake %v)",
		h.name, dc.device, time.Since(opened).Round(time.Microsecond), innerKex.Round(time.Microsecond),
		up.dial.Round(time.Microsecond), up.handsh.Round(time.Microsecond))

	sess := &gatewaySession{name: h.name}
	dc.mu.Lock()
	dc.sessions[sess] = struct{}{}
	dc.mu.Unlock()
	defer func() {
		dc.mu.Lock()
		delete(dc.sessions, sess)
		dc.mu.Unlock()
	}()

	// Either side ending ends the SSH Session.
	go func() { up.conn.Wait(); sconn.Close() }()
	go func() { sconn.Wait(); up.conn.Close() }()

	// Global requests: remote forwards (-R) go to the Host; others are refused.
	go func() {
		for r := range reqs {
			switch r.Type {
			case "tcpip-forward", "cancel-tcpip-forward":
				ok, payload, err := up.conn.SendRequest(r.Type, r.WantReply, r.Payload)
				log.Printf("gateway %s: %s -> ok=%v err=%v", h.name, r.Type, ok, err)
				if r.WantReply {
					r.Reply(ok && err == nil, payload)
				}
			default:
				log.Printf("gateway %s: client global request %q refused", h.name, r.Type)
				if r.WantReply {
					r.Reply(false, nil)
				}
			}
		}
	}()
	go func() {
		for r := range up.reqs {
			log.Printf("gateway %s: Host global request %q dropped", h.name, r.Type)
			if r.WantReply {
				r.Reply(false, nil)
			}
		}
	}()
	// Channels the Host opens: connections arriving on a remote forward.
	go func() {
		for nc := range up.chans {
			if nc.ChannelType() != "forwarded-tcpip" {
				log.Printf("gateway %s: Host channel %q refused", h.name, nc.ChannelType())
				nc.Reject(ssh.Prohibited, "refused by the koko Gateway")
				continue
			}
			go relayChannel(h.name, nc, sconn, nil)
		}
	}()
	// Channels the client opens: shells, exec, sftp, -L and -D.
	for nc := range chans {
		switch nc.ChannelType() {
		case "session", "direct-tcpip":
			go relayChannel(h.name, nc, up.conn, sess)
		default:
			log.Printf("gateway %s: client channel %q refused", h.name, nc.ChannelType())
			nc.Reject(ssh.UnknownChannelType, "not carried by the koko Gateway")
		}
	}
	log.Printf("gateway %s: SSH Session for %s ended after %v", h.name, dc.device, time.Since(opened).Round(time.Millisecond))
}

// refusedRequests are channel requests the Gateway answers itself.
var refusedRequests = map[string]bool{
	"auth-agent-req@openssh.com": true,
	"x11-req":                    true,
}

// relayChannel opens the same channel on the other connection and relays
// data, stderr and requests both ways until both sides have closed it.
func relayChannel(host string, nc ssh.NewChannel, other ssh.Conn, sess *gatewaySession) {
	far, farReqs, err := other.OpenChannel(nc.ChannelType(), nc.ExtraData())
	if err != nil {
		if oce, ok := errors.AsType[*ssh.OpenChannelError](err); ok {
			nc.Reject(oce.Reason, oce.Message)
		} else {
			nc.Reject(ssh.ConnectionFailed, err.Error())
		}
		return
	}
	near, nearReqs, err := nc.Accept()
	if err != nil {
		far.Close()
		return
	}
	if sess != nil && nc.ChannelType() == "session" {
		sess.mu.Lock()
		sess.shell = append(sess.shell, near)
		sess.mu.Unlock()
	}

	var farToNear sync.WaitGroup
	farToNear.Go(func() { io.Copy(near, far); near.CloseWrite() })
	farToNear.Go(func() { io.Copy(near.Stderr(), far.Stderr()) })
	farToNear.Go(func() { relayRequests(host, farReqs, near) })
	go func() { io.Copy(far, near); far.CloseWrite() }()
	go func() {
		farToNear.Wait()
		near.Close()
	}()
	relayRequests(host, nearReqs, far)
	far.Close()
}

func relayRequests(host string, in <-chan *ssh.Request, out ssh.Channel) {
	for r := range in {
		if refusedRequests[r.Type] {
			log.Printf("gateway %s: channel request %q refused", host, r.Type)
			if r.WantReply {
				r.Reply(false, nil)
			}
			continue
		}
		ok, err := out.SendRequest(r.Type, r.WantReply, r.Payload)
		if r.WantReply {
			r.Reply(ok && err == nil, nil)
		}
	}
}

// chanConn lets ssh.NewServerConn run over a Tunnel channel.
type chanConn struct{ ssh.Channel }

type tunnelAddr struct{}

func (tunnelAddr) Network() string { return "koko-tunnel" }
func (tunnelAddr) String() string  { return "tunnel" }

func (chanConn) LocalAddr() net.Addr                { return tunnelAddr{} }
func (chanConn) RemoteAddr() net.Addr               { return tunnelAddr{} }
func (chanConn) SetDeadline(t time.Time) error      { return nil }
func (chanConn) SetReadDeadline(t time.Time) error  { return nil }
func (chanConn) SetWriteDeadline(t time.Time) error { return nil }
