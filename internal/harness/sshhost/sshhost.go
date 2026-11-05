// Package sshhost is the in-process Host: an SSH server in the test process
// that stands in for a Host or a Relay. It has one Host Account with a home
// directory and an authorized_keys file of its own, password and key login,
// host keys of its own, exec requests run as local processes in that home
// directory, and a record of what it receives. It listens on loopback only
// and stops when the test ends.
//
// Like the S1 harness, it grows by addition: add Options and methods, and
// never change an existing one's signature (docs/subsystems/code-layout.md).
package sshhost

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// Account is the Host Account of every in-process Host.
const Account = "deploy"

// Host is one running in-process Host.
type Host struct {
	t        testing.TB
	settings settings
	listener net.Listener
	hostKeys []ssh.PublicKey
	home     string

	wg sync.WaitGroup
	mu sync.Mutex
	// open holds every connection still being served, closed when the test
	// ends; stopped refuses any accepted after that.
	open     map[net.Conn]struct{}
	stopped  bool
	conns    int
	logins   []Login
	requests []Request
}

// Login is one login attempt the Host received.
type Login struct {
	// Account is the user name asked for.
	Account string
	// Method is "publickey", "password" or "keyboard-interactive".
	Method string
	// Key is the SHA256 fingerprint of the key a "publickey" login offered.
	Key string
	// OK says whether the Host accepted it.
	OK bool
}

// Request is something the Host received after a login: a channel it was
// asked to open, or a request on its connection or on a channel.
type Request struct {
	// Channel is true for a channel open, false for a request.
	Channel bool
	// Type is the channel or request type: "session", "direct-tcpip",
	// "exec", "keepalive@openssh.com", ...
	Type string
	// Payload is its type-specific data, as sent.
	Payload []byte
	// Target is where a "direct-tcpip" channel asked to go, as host:port.
	Target string
}

// Option changes how Start sets up a Host.
type Option func(*settings)

type settings struct {
	password            string
	keyboardInteractive string
	authorizedKeys      []ssh.PublicKey
	hostKeys            []ssh.Signer
	resetFirst          int
	stallFirst          int
	slowLogins          time.Duration
}

// Password lets the Host Account log in with password over the "password"
// method.
func Password(password string) Option {
	return func(s *settings) { s.password = password }
}

// KeyboardInteractive lets the Host Account log in with password over the
// "keyboard-interactive" method, asked as one no-echo question, as sshd does
// through PAM.
func KeyboardInteractive(password string) Option {
	return func(s *settings) { s.keyboardInteractive = password }
}

// AuthorizedKey adds a line for key to the Host Account's authorized_keys.
// Without one, the Host Account has no authorized_keys file.
func AuthorizedKey(key ssh.PublicKey) Option {
	return func(s *settings) { s.authorizedKeys = append(s.authorizedKeys, key) }
}

// HostKeys gives the Host these host keys in place of the ed25519 key it
// makes for itself.
func HostKeys(keys ...ssh.Signer) Option {
	return func(s *settings) { s.hostKeys = keys }
}

// ResetFirst makes the Host reset its first n TCP connections before the SSH
// version exchange, as a dropped network path does.
func ResetFirst(n int) Option {
	return func(s *settings) { s.resetFirst = n }
}

// SlowLogins makes the Host take d to answer each login attempt, as a slow
// PAM stack does.
func SlowLogins(d time.Duration) Option {
	return func(s *settings) { s.slowLogins = d }
}

// StallFirst makes the Host hold its first n TCP connections without a word
// until the client gives up, as an unanswering network path does.
func StallFirst(n int) Option {
	return func(s *settings) { s.stallFirst = n }
}

// Start starts a Host on a loopback port. It stops when the test ends.
func Start(t testing.TB, opts ...Option) *Host {
	t.Helper()
	s := settings{}
	for _, opt := range opts {
		opt(&s)
	}
	if len(s.hostKeys) == 0 {
		s.hostKeys = []ssh.Signer{NewKey(t, ssh.KeyAlgoED25519)}
	}
	h := &Host{t: t, settings: s, home: t.TempDir(), open: map[net.Conn]struct{}{}}
	for _, k := range s.hostKeys {
		h.hostKeys = append(h.hostKeys, k.PublicKey())
	}
	if len(s.authorizedKeys) > 0 {
		h.writeAuthorizedKeys()
	}
	config := h.serverConfig()

	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for the in-process Host: %v", err)
	}
	h.listener = ln
	h.wg.Go(func() { h.accept(config) })
	t.Cleanup(h.stop)
	return h
}

// Address is the Host's address, as its record holds it.
func (h *Host) Address() string {
	return h.listener.Addr().(*net.TCPAddr).IP.String()
}

// Port is the port the Host's sshd listens on.
func (h *Host) Port() int {
	return h.listener.Addr().(*net.TCPAddr).Port
}

// HostKey is the Host's first host key: the one it made for itself, unless
// HostKeys gave it others.
func (h *Host) HostKey() ssh.PublicKey {
	return h.hostKeys[0]
}

// Home is the Host Account's home directory, where an exec request runs and
// ~/.ssh/authorized_keys lives, for a test to set up or inspect.
func (h *Host) Home() string {
	return h.home
}

// AuthorizedKeys returns the Host Account's authorized_keys file as it is
// now, byte for byte, or nil when there is none.
func (h *Host) AuthorizedKeys() []byte {
	h.t.Helper()
	b, err := os.ReadFile(h.authorizedKeysPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		h.t.Fatalf("read the in-process Host's authorized_keys: %v", err)
	}
	return b
}

// Conns counts the TCP connections the Host has accepted.
func (h *Host) Conns() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conns
}

// DropConns closes every open connection, as a Host or Relay whose network
// path breaks does, and keeps listening.
func (h *Host) DropConns() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for nc := range h.open {
		_ = nc.Close()
	}
}

// Open counts the Host's connections still open: those a client has not
// closed yet, as far as the Host has noticed.
func (h *Host) Open() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.open)
}

// Logins returns the login attempts the Host has received, in order.
func (h *Host) Logins() []Login {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Login(nil), h.logins...)
}

// Requests returns the channel opens and requests the Host has received, in
// order.
func (h *Host) Requests() []Request {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Request(nil), h.requests...)
}

// NewKey makes a private key of keyType: ssh.KeyAlgoED25519,
// ssh.KeyAlgoECDSA256 or ssh.KeyAlgoRSA (2048 bits).
func NewKey(t testing.TB, keyType string) ssh.Signer {
	t.Helper()
	var key crypto.Signer
	var err error
	switch keyType {
	case ssh.KeyAlgoED25519:
		_, key, err = ed25519.GenerateKey(rand.Reader)
	case ssh.KeyAlgoECDSA256:
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case ssh.KeyAlgoRSA:
		key, err = rsa.GenerateKey(rand.Reader, 2048)
	default:
		t.Fatalf("NewKey: unknown key type %q", keyType)
	}
	if err != nil {
		t.Fatalf("make a %s key: %v", keyType, err)
	}
	signer, err := ssh.NewSignerFromSigner(key)
	if err != nil {
		t.Fatalf("make a %s signer: %v", keyType, err)
	}
	return signer
}

func (h *Host) authorizedKeysPath() string {
	return filepath.Join(h.home, ".ssh", "authorized_keys")
}

func (h *Host) writeAuthorizedKeys() {
	var b bytes.Buffer
	for _, k := range h.settings.authorizedKeys {
		b.Write(ssh.MarshalAuthorizedKey(k))
	}
	if err := os.MkdirAll(filepath.Dir(h.authorizedKeysPath()), 0o700); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(h.authorizedKeysPath(), b.Bytes(), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

// authorized says whether key has a line in the Host Account's
// authorized_keys, read afresh on each login, as sshd does.
func (h *Host) authorized(key ssh.PublicKey) bool {
	rest, err := os.ReadFile(h.authorizedKeysPath())
	if err != nil {
		return false
	}
	for len(rest) > 0 {
		var line ssh.PublicKey
		line, _, _, rest, err = ssh.ParseAuthorizedKey(rest)
		if err != nil {
			return false
		}
		if bytes.Equal(line.Marshal(), key.Marshal()) {
			return true
		}
	}
	return false
}

var errRefused = errors.New("login refused")

func (h *Host) serverConfig() *ssh.ServerConfig {
	s := h.settings
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(md ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			ok := md.User() == Account && h.authorized(key)
			return nil, h.login(Login{Account: md.User(), Method: "publickey", Key: ssh.FingerprintSHA256(key), OK: ok})
		},
	}
	if s.password != "" {
		config.PasswordCallback = func(md ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			ok := md.User() == Account && string(password) == s.password
			return nil, h.login(Login{Account: md.User(), Method: "password", OK: ok})
		}
	}
	if s.keyboardInteractive != "" {
		config.KeyboardInteractiveCallback = func(md ssh.ConnMetadata, ask ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			answers, err := ask("", "", []string{"Password: "}, []bool{false})
			ok := err == nil && md.User() == Account && len(answers) == 1 && answers[0] == s.keyboardInteractive
			return nil, h.login(Login{Account: md.User(), Method: "keyboard-interactive", OK: ok})
		}
	}
	for _, k := range s.hostKeys {
		config.AddHostKey(k)
	}
	return config
}

// login records a login attempt and answers it.
func (h *Host) login(l Login) error {
	select {
	case <-time.After(h.settings.slowLogins):
	case <-h.t.Context().Done():
	}
	h.mu.Lock()
	h.logins = append(h.logins, l)
	h.mu.Unlock()
	if !l.OK {
		return errRefused
	}
	return nil
}

func (h *Host) record(r Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.requests = append(h.requests, r)
}

func (h *Host) accept(config *ssh.ServerConfig) {
	for {
		nc, err := h.listener.Accept()
		if err != nil {
			return
		}
		h.mu.Lock()
		if h.stopped {
			h.mu.Unlock()
			_ = nc.Close()
			return
		}
		h.conns++
		n := h.conns
		h.open[nc] = struct{}{}
		h.mu.Unlock()
		h.wg.Go(func() {
			defer h.forget(nc)
			switch {
			case n <= h.settings.resetFirst:
				_ = nc.(*net.TCPConn).SetLinger(0)
			case n <= h.settings.stallFirst:
				_, _ = io.Copy(io.Discard, nc)
			default:
				h.serve(nc, config)
			}
		})
	}
}

func (h *Host) forget(nc net.Conn) {
	_ = nc.Close()
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.open, nc)
}

// serve runs one SSH connection until the client or the test ends it.
func (h *Host) serve(nc net.Conn, config *ssh.ServerConfig) {
	conn, chans, reqs, err := ssh.NewServerConn(nc, config)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	h.wg.Go(func() { h.refuse(reqs) })
	for nch := range chans {
		switch nch.ChannelType() {
		case "session":
			h.session(nch)
		case "direct-tcpip":
			h.directTCPIP(nch)
		default:
			h.recordChannel(nch, "")
			_ = nch.Reject(ssh.UnknownChannelType, "unknown channel type")
		}
	}
}

// session accepts a session channel and records its requests. It runs the
// first exec request (exec), and refuses every other request: a later
// ticket that needs a shell, a pty or a subsystem adds it here.
func (h *Host) session(nch ssh.NewChannel) {
	h.recordChannel(nch, "")
	ch, reqs, err := nch.Accept()
	if err != nil {
		return
	}
	h.wg.Go(func() {
		defer func() { _ = ch.Close() }()
		started := false
		for r := range reqs {
			h.record(Request{Type: r.Type, Payload: r.Payload})
			var command struct{ Command string }
			ok := r.Type == "exec" && !started && ssh.Unmarshal(r.Payload, &command) == nil
			_ = r.Reply(ok, nil)
			if ok {
				started = true
				h.wg.Go(func() { h.exec(ch, command.Command) })
			}
		}
	})
}

// exec runs command as sshd runs an exec request: through the Host
// Account's shell, here /bin/sh, as a local process in the Host Account's
// home directory with HOME set to it, its stdin, stdout and stderr on the
// channel. It then sends the command's exit status, or the signal that ended
// it, and closes the channel.
func (h *Host) exec(ch ssh.Channel, command string) {
	defer func() { _ = ch.Close() }()
	cmd := exec.CommandContext(h.t.Context(), "/bin/sh", "-c", command)
	cmd.Dir = h.home
	cmd.Env = []string{"HOME=" + h.home, "USER=" + Account, "LOGNAME=" + Account, "SHELL=/bin/sh", "PATH=" + os.Getenv("PATH")}
	cmd.Stdout, cmd.Stderr = ch, ch.Stderr()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		_, _ = fmt.Fprintf(ch.Stderr(), "sh: %v\n", err)
		sendExitStatus(ch, 127)
		return
	}
	// The command may end before the client closes its stdin, so nothing
	// waits for this copy, which ends when the channel does.
	h.wg.Go(func() {
		_, _ = io.Copy(stdin, ch)
		_ = stdin.Close()
	})
	status := 0
	if err := cmd.Wait(); err != nil {
		status = 255
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() && signalNames[ws.Signal()] != "" {
				sendExitSignal(ch, signalNames[ws.Signal()])
				return
			}
			if ee.ExitCode() >= 0 {
				status = ee.ExitCode()
			}
		}
	}
	sendExitStatus(ch, status)
}

// signalNames are the names sshd gives the signals a test sends a command,
// without their "SIG". Another signal is told as the exit status 255.
var signalNames = map[syscall.Signal]string{
	syscall.SIGHUP:  "HUP",
	syscall.SIGINT:  "INT",
	syscall.SIGKILL: "KILL",
	syscall.SIGTERM: "TERM",
}

// sendExitSignal tells the client that the signal named name ended an exec
// request's command, as sshd does in place of its exit status.
func sendExitSignal(ch ssh.Channel, name string) {
	payload := ssh.Marshal(struct {
		Signal     string
		CoreDumped bool
		Error      string
		Lang       string
	}{Signal: name})
	_, _ = ch.SendRequest("exit-signal", false, payload)
}

// sendExitStatus tells the client an exec request's exit status, as sshd
// does before it closes the channel.
func sendExitStatus(ch ssh.Channel, status int) {
	_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{uint32(status)}))
}

// refuse records each request and refuses it.
func (h *Host) refuse(reqs <-chan *ssh.Request) {
	for r := range reqs {
		h.record(Request{Type: r.Type, Payload: r.Payload})
		_ = r.Reply(false, nil)
	}
}

func (h *Host) recordChannel(nch ssh.NewChannel, target string) {
	h.record(Request{Channel: true, Type: nch.ChannelType(), Payload: nch.ExtraData(), Target: target})
}

// directTCPIP forwards a direct-tcpip channel to its target, as a Relay's
// sshd does.
func (h *Host) directTCPIP(nch ssh.NewChannel) {
	var p struct {
		Host     string
		Port     uint32
		OrigHost string
		OrigPort uint32
	}
	if err := ssh.Unmarshal(nch.ExtraData(), &p); err != nil {
		_ = nch.Reject(ssh.ConnectionFailed, "malformed direct-tcpip request")
		return
	}
	target := net.JoinHostPort(p.Host, strconv.FormatUint(uint64(p.Port), 10))
	h.recordChannel(nch, target)
	var d net.Dialer
	tc, err := d.DialContext(h.t.Context(), "tcp", target)
	if err != nil {
		_ = nch.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	ch, reqs, err := nch.Accept()
	if err != nil {
		_ = tc.Close()
		return
	}
	h.wg.Go(func() { ssh.DiscardRequests(reqs) })
	h.wg.Go(func() {
		_, _ = io.Copy(tc, ch)
		_ = tc.Close()
	})
	h.wg.Go(func() {
		_, _ = io.Copy(ch, tc)
		_ = ch.Close()
	})
}

// stop closes the listener and every open connection, and waits for the
// Host's goroutines to end.
func (h *Host) stop() {
	_ = h.listener.Close()
	h.mu.Lock()
	h.stopped = true
	for nc := range h.open {
		_ = nc.Close()
	}
	h.mu.Unlock()
	h.wg.Wait()
}
