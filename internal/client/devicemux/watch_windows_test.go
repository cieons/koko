package devicemux_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/devicemux"
	"github.com/cieons/koko/internal/shared/liveness"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// stalling is one end of a connection that reads nothing more once stall
// is closed, as an end whose socket never told it of the other's close,
// until the test ends: it answers no probe then.
type stalling struct {
	net.Conn
	stall, end chan struct{}
}

func newStalling(t *testing.T, nc net.Conn) *stalling {
	s := &stalling{Conn: nc, stall: make(chan struct{}), end: make(chan struct{})}
	t.Cleanup(func() {
		close(s.end)
		_ = nc.Close()
	})
	return s
}

func (s *stalling) Read(b []byte) (int, error) {
	select {
	case <-s.stall:
		<-s.end
		return 0, net.ErrClosed
	default:
	}
	return s.Conn.Read(b)
}

// On Windows the Device Mux probes a command's connection once it goes
// quiet, and closes it once a probe goes unanswered, as for a command whose
// close the socket never told it of: the command leaves, and the Device Mux
// idles out.
func TestTheDeviceMuxLetsGoOfACommandThatAnswersNoProbe(t *testing.T) {
	const idle = time.Second
	srv := startReporter(t, nil)
	home, socket, muxLog := runMux(t, srv, timing.Timings{LocalProbe: 100 * time.Millisecond, LocalAnswer: 200 * time.Millisecond, DeviceMuxIdle: idle})
	var nc net.Conn
	deadline := time.Now().Add(10 * time.Second)
	for {
		var d net.Dialer
		c, err := d.DialContext(t.Context(), "unix", socket)
		if err == nil {
			nc = c
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("connect to the Device Mux: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	s := newStalling(t, nc)
	conn, chans, reqs, err := ssh.NewClientConn(s, socket, &ssh.ClientConfig{User: home, HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	if err != nil {
		t.Fatalf("connect to the Device Mux: %v", err)
	}
	go ssh.DiscardRequests(reqs)
	go func() {
		for nch := range chans {
			_ = nch.Reject(ssh.Prohibited, "none")
		}
	}()
	t.Cleanup(func() { _ = conn.Close() })
	close(s.stall)

	waitLog(t, muxLog, `msg="closed a command's connection that stopped answering"`)
	waitLogWithin(t, muxLog, `msg="device mux stopped" reason="nothing was connected to it for a while"`, 10*time.Second+idle)
}

// On Windows a command probes its connection to the Device Mux once it
// goes quiet, and ends it once a probe goes unanswered, as for a Device
// Mux whose close the socket never told it of: its Wait returns.
func TestACommandEndsAConnectionToADeviceMuxThatAnswersNoProbe(t *testing.T) {
	dir, err := os.MkdirTemp("", "koko-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "mux.sock")
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	// A Device Mux that answers, connected, and reports, then reads nothing
	// more.
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	hostKey, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		NoClientAuth: true,
		BannerCallback: func(ssh.ConnMetadata) string {
			return `{"state": "connected", "since": "` + time.Now().UTC().Format(time.RFC3339Nano) + `"}`
		},
	}
	cfg.AddHostKey(hostKey)
	go func() {
		nc, err := ln.Accept()
		if err != nil {
			return
		}
		s := newStalling(t, nc)
		sc, chans, reqs, err := ssh.NewServerConn(s, cfg)
		if err != nil {
			return
		}
		go ssh.DiscardRequests(reqs)
		go func() {
			for nch := range chans {
				_ = nch.Reject(ssh.Prohibited, "none")
			}
		}()
		_, _, _ = sc.SendRequest(wire.RequestReport, false, wire.Report{Version: "v0.0.1"}.Marshal())
		close(s.stall)
	}()

	conn, _, err := devicemux.Dial(t.Context(), socket, t.TempDir(), timing.Timings{LocalProbe: 100 * time.Millisecond, LocalAnswer: 200 * time.Millisecond})
	if err != nil {
		t.Fatalf("connect to the Device Mux: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ended := make(chan error, 1)
	go func() { ended <- conn.Wait() }()
	select {
	case err := <-ended:
		if _, ok := errors.AsType[*liveness.DeadError](err); !ok {
			t.Errorf("the connection ended with %v, want a *liveness.DeadError", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the connection to a Device Mux that answers no probe did not end within 10 s")
	}
}
