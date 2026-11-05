// PROTOTYPE, throwaway: never merged. See README.md.
//
// server.go is the koko server: it accepts Device Connections over SSH,
// authenticates each by the device's registered key (its Device Session),
// and hands every Tunnel opened on one to the Gateway.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// tunnelChannel is the channel type a Tunnel uses on a Device Connection.
// Its extra data is one SSH string: the name ssh gave `koko tunnel` as %h.
const tunnelChannel = "tunnel@koko"

type server struct {
	hostKey ssh.Signer
	kokoKey ssh.Signer
	serial  bool     // dial the Host only after the inner SSH handshake
	ciphers []string // what the Gateway offers inside the Tunnel; nil is x/crypto's default

	mu    sync.Mutex
	conns map[*deviceConn]struct{}
}

// deviceConn is one live Device Connection and the SSH Sessions it carries.
type deviceConn struct {
	sconn  *ssh.ServerConn
	device string // "user/device", the comment on the registered key
	fp     string

	mu       sync.Mutex
	sessions map[*gatewaySession]struct{}
}

func runServer(args []string) int {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	listen := fs.String("listen", "127.0.0.1:47443", "address for Device Connections")
	serial := fs.Bool("serial", false, "dial the Host after the inner handshake instead of alongside it")
	initOnly := fs.Bool("init", false, "generate the host key and koko key, then exit")
	ciphers := fs.String("gateway-ciphers", "", "comma-separated ciphers the Gateway offers inside the Tunnel")
	fs.Parse(args)
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	log.SetPrefix("server ")

	hostKey, err := loadOrCreateKey(serverHostKeyPath())
	if err != nil {
		log.Fatalf("load host key: %v", err)
	}
	kokoKey, err := loadOrCreateKey(kokoKeyPath())
	if err != nil {
		log.Fatalf("load koko key: %v", err)
	}
	if *initOnly {
		return 0
	}
	s := &server{hostKey: hostKey, kokoKey: kokoKey, serial: *serial, conns: map[*deviceConn]struct{}{}}
	if *ciphers != "" {
		s.ciphers = strings.Split(*ciphers, ",")
	}

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Printf("listening on %s, host key %s, serial=%v, gateway ciphers=%v", *listen, ssh.FingerprintSHA256(hostKey.PublicKey()), *serial, s.ciphers)
	go s.watchRevocations()
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Fatalf("accept: %v", err)
		}
		go s.handleDevice(c)
	}
}

func (s *server) deviceConfig() *ssh.ServerConfig {
	cfg := &ssh.ServerConfig{
		// Called for the key query and again before the signature check.
		// A revoked key is still recognised here, so the signed attempt can
		// be told why it is refused; an unknown key is refused at once.
		PublicKeyCallback: func(m ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			fp := ssh.FingerprintSHA256(key)
			if name, ok := findKey(devicesPath(), key); ok {
				return &ssh.Permissions{Extensions: map[string]string{"device": name, "fp": fp}}, nil
			}
			if name, ok := findKey(revokedPath(), key); ok {
				return &ssh.Permissions{Extensions: map[string]string{"device": name, "fp": fp, "revoked": "yes"}}, nil
			}
			return nil, &ssh.BannerError{
				Err:     fmt.Errorf("unknown device key %s", fp),
				Message: "koko: this koko server does not know this device's key.\n",
			}
		},
		// Called only once the client has proven it holds the key, so only
		// the holder of a revoked key learns that it was revoked.
		VerifiedPublicKeyCallback: func(m ssh.ConnMetadata, key ssh.PublicKey, p *ssh.Permissions, alg string) (*ssh.Permissions, error) {
			if p.Extensions["revoked"] != "" {
				return nil, &ssh.BannerError{
					Err:     fmt.Errorf("revoked Device Session %s", p.Extensions["device"]),
					Message: fmt.Sprintf("koko: the Device Session %q of this device was revoked.\n", p.Extensions["device"]),
				}
			}
			return p, nil
		},
		AuthLogCallback: func(m ssh.ConnMetadata, method string, err error) {
			log.Printf("device %s auth method=%s user=%q err=%v", m.RemoteAddr(), method, m.User(), err)
		},
	}
	cfg.AddHostKey(s.hostKey)
	return cfg
}

func (s *server) handleDevice(c net.Conn) {
	start := time.Now()
	sconn, chans, reqs, err := ssh.NewServerConn(c, s.deviceConfig())
	if err != nil {
		log.Printf("device connection from %s refused after %v: %v", c.RemoteAddr(), time.Since(start).Round(time.Microsecond), err)
		return
	}
	dc := &deviceConn{sconn: sconn, device: sconn.Permissions.Extensions["device"], fp: sconn.Permissions.Extensions["fp"], sessions: map[*gatewaySession]struct{}{}}
	log.Printf("device connection %s up: %s (%s), client %q, handshake+auth %v",
		c.RemoteAddr(), dc.device, dc.fp, sconn.ClientVersion(), time.Since(start).Round(time.Microsecond))
	s.mu.Lock()
	s.conns[dc] = struct{}{}
	s.mu.Unlock()

	go func() {
		for r := range reqs {
			log.Printf("device connection %s: global request %q refused", c.RemoteAddr(), r.Type)
			if r.WantReply {
				r.Reply(false, nil)
			}
		}
	}()
	for nc := range chans {
		switch nc.ChannelType() {
		case tunnelChannel:
			var m struct{ Name string }
			if err := ssh.Unmarshal(nc.ExtraData(), &m); err != nil {
				nc.Reject(ssh.ConnectionFailed, "malformed Tunnel request")
				continue
			}
			go s.openTunnel(dc, nc, m.Name)
		case "direct-tcpip":
			// The same Tunnel opened by OpenSSH itself (`ssh -W %h:%p`),
			// used to compare against a shared Device Connection.
			var m struct {
				Host     string
				Port     uint32
				OrigHost string
				OrigPort uint32
			}
			if err := ssh.Unmarshal(nc.ExtraData(), &m); err != nil {
				nc.Reject(ssh.ConnectionFailed, "malformed direct-tcpip request")
				continue
			}
			go s.openTunnel(dc, nc, m.Host)
		default:
			nc.Reject(ssh.UnknownChannelType, "this prototype carries only Tunnels")
		}
	}

	s.mu.Lock()
	delete(s.conns, dc)
	s.mu.Unlock()
	log.Printf("device connection %s (%s) closed after %v", c.RemoteAddr(), dc.device, time.Since(start).Round(time.Millisecond))
}

func (s *server) openTunnel(dc *deviceConn, nc ssh.NewChannel, name string) {
	opened := time.Now()
	short, _, ok := strings.Cut(name, ".") // web1.koko (or web1.kokomux) -> web1
	h, err := lookupHost(short)
	if !ok || err != nil {
		log.Printf("tunnel for %q from %s refused: no such Host", name, dc.device)
		nc.Reject(ssh.ConnectionFailed, fmt.Sprintf("no Host named %q in your Spaces", name))
		return
	}
	ch, reqs, err := nc.Accept()
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	log.Printf("tunnel for %q from %s accepted (%s)", name, dc.device, nc.ChannelType())
	s.gateway(dc, ch, h, opened)
}

// watchRevocations closes the Device Connections of a Device Session that
// has been removed from the devices file, telling each open SSH Session why.
func (s *server) watchRevocations() {
	for range time.Tick(500 * time.Millisecond) {
		s.mu.Lock()
		var gone []*deviceConn
		for dc := range s.conns {
			if !stillRegistered(dc.fp) {
				gone = append(gone, dc)
			}
		}
		s.mu.Unlock()
		for _, dc := range gone {
			log.Printf("Device Session %s revoked: closing its Device Connection", dc.device)
			dc.mu.Lock()
			for sess := range dc.sessions {
				sess.notify(fmt.Sprintf("\r\nkoko: the Device Session %q was revoked; this SSH Session is closed.\r\n", dc.device))
			}
			dc.mu.Unlock()
			dc.sconn.Close()
		}
	}
}

func stillRegistered(fp string) bool {
	b, err := os.ReadFile(devicesPath())
	if err != nil {
		return true // unknown is not "revoked"
	}
	b = bytes.TrimSpace(b)
	for len(b) > 0 {
		pk, _, _, rest, err := ssh.ParseAuthorizedKey(b)
		if err != nil {
			return true
		}
		if ssh.FingerprintSHA256(pk) == fp {
			return true
		}
		b = rest
	}
	return false
}
