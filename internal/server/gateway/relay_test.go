package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// sessionPair returns the two ends of one session channel, the server's and
// the client's, over an SSH connection on a loopback port, closed when the
// test ends.
func sessionPair(t *testing.T) (server, client ssh.Channel) {
	t.Helper()
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	var d net.Dialer
	clientNC, err := d.DialContext(t.Context(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	serverNC, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = serverNC.Close()
		_ = clientNC.Close()
	})
	accepted := make(chan ssh.Channel, 1)
	go func() {
		_, chans, reqs, err := ssh.NewServerConn(serverNC, cfg)
		if err != nil {
			close(accepted)
			return
		}
		go ssh.DiscardRequests(reqs)
		nch, ok := <-chans
		if !ok {
			close(accepted)
			return
		}
		ch, chReqs, err := nch.Accept()
		if err != nil {
			close(accepted)
			return
		}
		go ssh.DiscardRequests(chReqs)
		accepted <- ch
	}()
	conn, chans, reqs, err := ssh.NewClientConn(clientNC, "gateway", &ssh.ClientConfig{HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	if err != nil {
		t.Fatal(err)
	}
	go ssh.DiscardRequests(reqs)
	go func() {
		for nch := range chans {
			_ = nch.Reject(ssh.Prohibited, "no")
		}
	}()
	client, clientReqs, err := conn.OpenChannel("session", nil)
	if err != nil {
		t.Fatal(err)
	}
	go ssh.DiscardRequests(clientReqs)
	server, ok := <-accepted
	if !ok {
		t.Fatal("the server end did not accept the session channel")
	}
	return server, client
}

// The lines the Gateway writes on a client's session channel, beside what
// the Host sends to its stderr, take turns with each other and with the EOF
// the Host's direction passes on, which x/crypto's channel needs (go test
// -race fails otherwise), and no line follows the EOF: every line arrives
// whole, and a write after the EOF fails with io.EOF.
func TestASessionChannelsLinesTakeTurnsWithItsEOF(t *testing.T) {
	server, client := sessionPair(t)
	ch := &sessionChannel{Channel: server}
	read := make(chan string, 1)
	go func() {
		got, _ := io.ReadAll(client.Stderr())
		read <- string(got)
	}()

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 50 {
				if _, err := io.WriteString(ch.Stderr(), "koko: a line\n"); err != nil {
					return
				}
			}
		})
	}
	wg.Go(func() { _ = ch.CloseWrite() })
	wg.Wait()
	if _, err := io.WriteString(ch.Stderr(), "koko: late\n"); !errors.Is(err, io.EOF) {
		t.Errorf("a line after the EOF: %v, want io.EOF", err)
	}
	_ = server.Close()
	got := <-read
	for line := range strings.SplitSeq(strings.TrimSuffix(got, "\n"), "\n") {
		if got != "" && line != "koko: a line" {
			t.Errorf("the client read the line %q, want each line whole and none after the EOF", line)
		}
	}
}
