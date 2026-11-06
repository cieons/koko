package liveness_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/shared/liveness"
)

// peer is the far end of a watched SSH connection: it answers every global
// request, as any SSH server does, and notes when each probe arrived.
type peer struct {
	mu     sync.Mutex
	probes []time.Time
}

func (p *peer) probed() []time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]time.Time(nil), p.probes...)
}

// gate passes what the peer sends until it is closed: then nothing more
// arrives, as on a network path that went dead, until the test ends.
type gate struct {
	net.Conn
	shut chan struct{}
	once sync.Once
	end  <-chan struct{}
}

func (g *gate) Read(b []byte) (int, error) {
	n, err := g.Conn.Read(b)
	select {
	case <-g.shut:
		// What arrives once it is shut is lost, as on a dead path, even
		// for a read that began before.
		<-g.end
		return 0, net.ErrClosed
	default:
	}
	return n, err
}

func (g *gate) close() { g.once.Do(func() { close(g.shut) }) }

// pair returns the two ends of a loopback TCP connection, closed when the
// test ends. (net.Pipe will not do: both SSH ends write their version at
// once, and its writes wait for the other end to read.)
func pair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	var d net.Dialer
	a, err := d.DialContext(t.Context(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	b, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	return a, b
}

// watched opens an SSH connection to a peer over loopback TCP, the
// device's end wrapped in a liveness.Conn behind a gate, and watches it with
// probe, dead and now until the test ends. The channel it returns carries
// what Watch returned.
func watched(t *testing.T, probe, dead time.Duration, now <-chan struct{}) (*peer, *gate, <-chan error) {
	t.Helper()
	end := make(chan struct{})
	a, b := pair(t)
	g := &gate{Conn: a, shut: make(chan struct{}), end: end}
	lc := liveness.NewConn(g)

	_, key, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)
	p := &peer{}
	served := make(chan struct{})
	go func() {
		defer close(served)
		conn, chans, reqs, err := ssh.NewServerConn(b, cfg)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		go func() {
			for nch := range chans {
				_ = nch.Reject(ssh.Prohibited, "none")
			}
		}()
		for r := range reqs {
			if r.Type == liveness.Request {
				p.mu.Lock()
				p.probes = append(p.probes, time.Now())
				p.mu.Unlock()
			}
			_ = r.Reply(false, nil)
		}
	}()
	conn, chans, reqs, err := ssh.NewClientConn(lc, "peer", &ssh.ClientConfig{User: "koko", HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	if err != nil {
		t.Fatal(err)
	}
	client := ssh.NewClient(conn, chans, reqs)
	result := make(chan error, 1)
	watching := make(chan struct{})
	go func() {
		defer close(watching)
		result <- lc.Watch(client, probe, dead, now, end)
	}()
	t.Cleanup(func() {
		close(end)
		_ = client.Close()
		_ = b.Close()
		<-served
		<-watching
	})
	return p, g, result
}

// A quiet connection whose peer answers is probed each time nothing has
// arrived for probe, and never declared dead.
func TestAQuietConnectionThatAnswersLives(t *testing.T) {
	const probe, dead = 20 * time.Millisecond, time.Second
	p, _, result := watched(t, probe, dead, nil)
	start := time.Now()
	select {
	case err := <-result:
		t.Fatalf("Watch declared a connection that answers dead: %v", err)
	case <-time.After(dead / 2):
	}
	probes := p.probed()
	if len(probes) < 5 {
		t.Fatalf("the peer got %d probes in %v, want one each %v without a byte", len(probes), dead/2, probe)
	}
	if first := probes[0].Sub(start); first < probe/2 {
		t.Errorf("the first probe came %v after the watch began, before %v without a byte", first, probe)
	}
	select {
	case err := <-result:
		t.Fatalf("Watch declared a connection that answers dead: %v", err)
	case <-time.After(dead):
	}
}

// A connection on which nothing arrives any more is declared dead after
// dead without a byte, having been probed after probe.
func TestASilentConnectionIsDeadAfterDead(t *testing.T) {
	const probe, dead = 50 * time.Millisecond, 200 * time.Millisecond
	p, g, result := watched(t, probe, dead, nil)
	for deadline := time.Now().Add(5 * time.Second); len(p.probed()) == 0; time.Sleep(probe / 5) {
		if time.Now().After(deadline) {
			t.Fatal("no probe came within 5 s")
		}
	}
	g.close()
	silent := time.Now()
	var err error
	select {
	case err = <-result:
	case <-time.After(5 * time.Second):
		t.Fatalf("Watch did not declare a silent connection dead within 5 s")
	}
	quiet := time.Since(silent)
	if e, ok := errors.AsType[*liveness.DeadError](err); !ok || e.Quiet < dead {
		t.Errorf("Watch returned %v, want a *DeadError of at least %v", err, dead)
	}
	// The last answer came at most a probe's wait, and its round trip,
	// before the gate shut.
	if quiet < dead-2*probe {
		t.Errorf("declared dead %v after the gate shut, want at least %v", quiet, dead-2*probe)
	}
	var after int
	for _, at := range p.probed() {
		if at.After(silent) {
			after++
		}
	}
	if after == 0 {
		t.Errorf("no probe went out once the connection went silent")
	}
}

// A value on now sends a probe at once, which has dead-probe to be
// answered: long before probe would have sent one.
func TestAProbeSentAtOnceGetsDeadMinusProbe(t *testing.T) {
	const probe, dead = time.Hour, time.Hour + 100*time.Millisecond
	now := make(chan struct{}, 1)
	p, g, result := watched(t, probe, dead, now)
	g.close()
	sent := time.Now()
	now <- struct{}{}
	select {
	case err := <-result:
		if _, ok := errors.AsType[*liveness.DeadError](err); !ok {
			t.Errorf("Watch returned %v, want a *DeadError", err)
		}
		if d := time.Since(sent); d < dead-probe {
			t.Errorf("declared dead %v after the probe, want at least %v", d, dead-probe)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("a probe sent at once and never answered did not end the connection within 5 s")
	}
	if n := len(p.probed()); n != 1 {
		t.Errorf("the peer got %d probes, want the one sent at once", n)
	}
}

// Watch returns nil once the connection it watches has ended.
func TestWatchEndsWithTheConnection(t *testing.T) {
	end := make(chan struct{})
	a, _ := pair(t)
	lc := liveness.NewConn(a)
	result := make(chan error, 1)
	go func() { result <- lc.Watch(nil, time.Hour, 2*time.Hour, nil, end) }()
	close(end)
	select {
	case err := <-result:
		if err != nil {
			t.Errorf("Watch returned %v once its connection ended, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Watch did not return once its connection ended")
	}
}
