// PROTOTYPE, throwaway: never merged. See README.md.
//
// tunnel.go is `koko tunnel <name>`, the ProxyCommand in koko's ssh_config
// block: it opens a Device Connection with the device's key, opens a Tunnel
// on it for the name ssh passed as %h, and relays ssh's stdin and stdout.
package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
)

// trace appends timestamped events to $KOKO_PROTO_TRACE, because stderr is
// the user's terminal (or /dev/null under ControlPersist).
type trace struct {
	start time.Time
	f     *os.File
}

func newTrace() *trace {
	t := &trace{start: time.Now()}
	if p := os.Getenv("KOKO_PROTO_TRACE"); p != "" {
		t.f, _ = os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	}
	return t
}

func (t *trace) event(format string, args ...any) {
	if t.f == nil {
		return
	}
	fmt.Fprintf(t.f, "tunnel pid=%d +%8.3fms %s\n", os.Getpid(),
		float64(time.Since(t.start).Microseconds())/1000, fmt.Sprintf(format, args...))
}

func (t *trace) exit(code int, why string) int {
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	cpu := time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
	t.event("exit %d (%s), cpu %v", code, why, cpu.Round(time.Millisecond))
	return code
}

func runTunnel(args []string) int {
	t := newTrace()
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: koko tunnel <name>")
		return t.exit(2, "usage")
	}
	name := args[0]
	t.event("start %q", name)

	// SIGINT reaches this process too when ssh runs without a tty and the
	// user presses Ctrl-C; leave that to ssh, which SIGHUPs us as it exits.
	if os.Getenv("KOKO_PROTO_SIGINT") != "default" {
		signal.Ignore(syscall.SIGINT)
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGTERM)

	keyPEM, err := os.ReadFile(deviceKeyPath())
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Fprint(os.Stderr, "koko: this device has not joined a koko server, so it has no Device Session.\n"+
			"koko: run `koko join <Join Code>` with an Invite, or with a code from `koko device add` on a device that has joined.\n")
		return t.exit(1, "no device key")
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "koko: read this device's key: %v\n", err)
		return t.exit(1, "device key unreadable")
	}
	signer, err := ssh.ParsePrivateKey(keyPEM)
	if err != nil {
		fmt.Fprintf(os.Stderr, "koko: parse this device's key: %v\n", err)
		return t.exit(1, "device key bad")
	}
	pin, err := os.ReadFile(deviceServerPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "koko: read the joined koko server: %v\n", err)
		return t.exit(1, "no server file")
	}
	addr, pinnedFP, _ := strings.Cut(strings.TrimSpace(string(pin)), " ")
	if a := os.Getenv("KOKO_PROTO_SERVER"); a != "" {
		addr = a // prototype knob: go through the WAN emulator
	}

	var banner string
	cfg := &ssh.ClientConfig{
		User: "koko",
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if fp := ssh.FingerprintSHA256(key); fp != pinnedFP {
				return fmt.Errorf("the koko server at %s presented host key %s, but this device pinned %s when it joined", addr, fp, pinnedFP)
			}
			return nil
		},
		HostKeyAlgorithms: []string{ssh.KeyAlgoED25519},
		BannerCallback:    func(m string) error { banner += m; return nil },
		Timeout:           10 * time.Second,
	}
	c, err := net.DialTimeout("tcp", addr, cfg.Timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "koko: cannot reach the koko server at %s: %v\n", addr, err)
		return t.exit(1, "dial")
	}
	t.event("tcp connected")
	conn, chans, reqs, err := ssh.NewClientConn(c, addr, cfg)
	if err != nil {
		switch {
		case banner != "":
			fmt.Fprint(os.Stderr, banner)
			fmt.Fprint(os.Stderr, "koko: join again with a new Join Code: koko join <Join Code>\n")
		case strings.Contains(err.Error(), "unable to authenticate"):
			fmt.Fprintf(os.Stderr, "koko: the koko server at %s refused this device's Device Session.\n", addr)
		default:
			fmt.Fprintf(os.Stderr, "koko: Device Connection to %s failed: %v\n", addr, err)
		}
		return t.exit(1, "handshake: "+err.Error())
	}
	t.event("device connection up (handshake+auth)")
	go ssh.DiscardRequests(reqs)
	go func() {
		for nc := range chans {
			nc.Reject(ssh.Prohibited, "no")
		}
	}()

	ch, chReqs, err := conn.OpenChannel(tunnelChannel, ssh.Marshal(struct{ Name string }{name}))
	if err != nil {
		if oce, ok := errors.AsType[*ssh.OpenChannelError](err); ok {
			fmt.Fprintf(os.Stderr, "koko: %s\n", oce.Message)
		} else {
			fmt.Fprintf(os.Stderr, "koko: open a Tunnel to %s: %v\n", name, err)
		}
		conn.Close()
		return t.exit(1, "tunnel refused")
	}
	t.event("tunnel open")
	go ssh.DiscardRequests(chReqs)

	stdinEOF := make(chan struct{})
	tunnelEOF := make(chan struct{})
	connGone := make(chan struct{})
	go func() {
		n, err := io.Copy(ch, os.Stdin)
		t.event("stdin EOF after %d bytes (err=%v); half-closing the Tunnel", n, err)
		ch.CloseWrite()
		close(stdinEOF)
	}()
	go func() {
		first := true
		buf := make([]byte, 32*1024)
		var n int64
		for {
			k, err := ch.Read(buf)
			if k > 0 && first {
				t.event("first bytes from the Gateway")
				first = false
			}
			if k > 0 {
				os.Stdout.Write(buf[:k])
				n += int64(k)
			}
			if err != nil {
				t.event("Tunnel EOF after %d bytes", n)
				break
			}
		}
		close(tunnelEOF)
	}()
	go func() { conn.Wait(); close(connGone) }()

	select {
	case <-tunnelEOF:
		conn.Close()
		return t.exit(0, "tunnel closed by the Gateway")
	case <-connGone:
		// Let whatever the Gateway sent last (a revocation notice) reach
		// ssh before saying anything ourselves.
		select {
		case <-tunnelEOF:
		case <-time.After(time.Second):
		}
		fmt.Fprintf(os.Stderr, "\r\nkoko: the Device Connection to %s closed.\r\n", addr)
		return t.exit(1, "device connection lost")
	case s := <-sigs:
		t.event("signal %v", s)
		select {
		case <-stdinEOF:
			t.event("stdin had already reached EOF")
		default:
		}
		conn.Close()
		return t.exit(0, "signal "+s.String())
	}
}
