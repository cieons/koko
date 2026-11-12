// Package sshhost is the in-process Host: an SSH server in the test process
// that stands in for a Host or a Relay. It has one Host Account with a home
// directory and an authorized_keys file of its own, password and key login,
// host keys of its own, sessions whose commands run as local processes in
// that home directory, the forwards sshd allows by default, and a record of
// what it receives. It listens on loopback only and stops when the test
// ends.
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
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
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
	// resetNext counts the connections still to reset, as ResetNext asked.
	resetNext int
	// thawed is closed while the Host is not frozen (Freeze).
	thawed chan struct{}
	// holdChannels leaves the channels opened from now on unanswered
	// (HoldChannels).
	holdChannels atomic.Bool
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
	clientAliveInterval time.Duration
	clientAliveCountMax int
	// subsystems are the commands that serve each subsystem, by name.
	subsystems map[string]string
}

// Subsystem lets a session ask for the subsystem name, which the Host runs
// as command through /bin/sh -c, as sshd runs the command of a Subsystem
// line, such as the sftp-server that serves "sftp".
func Subsystem(name, command string) Option {
	return func(s *settings) {
		if s.subsystems == nil {
			s.subsystems = map[string]string{}
		}
		s.subsystems[name] = command
	}
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

// ClientAlive makes the Host probe a quiet client as sshd's
// ClientAliveInterval and ClientAliveCountMax do: once interval has passed
// without a byte from the client, it sends keepalive@openssh.com wanting a
// reply, on an open session channel if it has one and on the connection
// otherwise, and when countMax have gone unanswered in a row and the client
// is quiet still, it ends the connection. Any reply, a refusal included,
// answers.
func ClientAlive(interval time.Duration, countMax int) Option {
	return func(s *settings) { s.clientAliveInterval, s.clientAliveCountMax = interval, countMax }
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
	h := &Host{t: t, settings: s, home: t.TempDir(), open: map[net.Conn]struct{}{}, thawed: make(chan struct{})}
	close(h.thawed)
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

// ResetNext makes the Host reset the next n TCP connections it accepts
// before the SSH version exchange, as a network path that drops for a moment
// does, such as once it has been Enrolled.
func (h *Host) ResetNext(n int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.resetNext = n
}

// Freeze makes the Host stop reading and writing on every connection, as a
// Host whose network path stops passing packets does, until Thaw: what
// arrives meanwhile waits, and nothing is lost. DropConns and the test's end
// still close a frozen connection at once; a client's close is noticed only
// at Thaw.
func (h *Host) Freeze() {
	h.mu.Lock()
	defer h.mu.Unlock()
	select {
	case <-h.thawed:
		h.thawed = make(chan struct{})
	default:
	}
}

// Thaw lets a frozen Host carry on with every connection where it stopped.
func (h *Host) Thaw() {
	h.mu.Lock()
	defer h.mu.Unlock()
	select {
	case <-h.thawed:
	default:
		close(h.thawed)
	}
}

// HoldChannels makes the Host leave every channel opened from now on
// unanswered, neither accepted nor refused, as a Host too loaded to serve one
// does, while its connections carry on otherwise. It records each such open,
// so a test can tell that it has arrived.
func (h *Host) HoldChannels() {
	h.holdChannels.Store(true)
}

// waitThawed waits until the Host is not frozen, or closed is.
func (h *Host) waitThawed(closed <-chan struct{}) {
	h.mu.Lock()
	thawed := h.thawed
	h.mu.Unlock()
	select {
	case <-thawed:
	case <-closed:
	}
}

// hostConn is a connection the Host accepted, which stops reading and
// writing while the Host is frozen.
type hostConn struct {
	net.Conn
	h      *Host
	once   sync.Once
	closed chan struct{}
	// lastRead is when a byte last came from the client, in Unix
	// nanoseconds, for ClientAlive.
	lastRead atomic.Int64
}

func (c *hostConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.h.waitThawed(c.closed)
	if n > 0 {
		c.lastRead.Store(time.Now().UnixNano())
	}
	return n, err
}

func (c *hostConn) Write(p []byte) (int, error) {
	c.h.waitThawed(c.closed)
	return c.Conn.Write(p)
}

func (c *hostConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
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
		tcp, err := h.listener.Accept()
		if err != nil {
			return
		}
		nc := &hostConn{Conn: tcp, h: h, closed: make(chan struct{})}
		nc.lastRead.Store(time.Now().UnixNano())
		h.mu.Lock()
		if h.stopped {
			h.mu.Unlock()
			_ = nc.Close()
			return
		}
		h.conns++
		n := h.conns
		h.open[nc] = struct{}{}
		reset := h.resetNext > 0
		if reset {
			h.resetNext--
		}
		h.mu.Unlock()
		h.wg.Go(func() {
			defer h.forget(nc)
			switch {
			case reset || n <= h.settings.resetFirst:
				_ = tcp.(*net.TCPConn).SetLinger(0)
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
func (h *Host) serve(nc *hostConn, config *ssh.ServerConfig) {
	conn, chans, reqs, err := ssh.NewServerConn(nc, config)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	h.wg.Go(func() { h.globalRequests(conn, reqs) })
	open := &openSessions{}
	if h.settings.clientAliveInterval > 0 {
		h.wg.Go(func() { h.probeClient(conn, nc, open) })
	}
	for nch := range chans {
		if h.holdChannels.Load() {
			h.recordChannel(nch, "")
			continue
		}
		switch nch.ChannelType() {
		case "session":
			h.session(nch, open)
		case "direct-tcpip":
			h.directTCPIP(nch)
		case "direct-streamlocal@openssh.com":
			h.directStreamLocal(nch)
		default:
			h.recordChannel(nch, "")
			_ = nch.Reject(ssh.UnknownChannelType, "unknown channel type")
		}
	}
}

// session accepts a session channel and records its requests. As sshd
// does, it takes env requests and a pty-req before the command, setting the
// variables, and TERM to the terminal the pty-req names (it allocates no
// terminal), then runs the first exec, shell or known subsystem request as
// the session's command (start), sends a signal request's signal to that
// command, and takes a window-change. It refuses every other request. The
// channel is among the connection's open sessions until it closes.
func (h *Host) session(nch ssh.NewChannel, open *openSessions) {
	h.recordChannel(nch, "")
	ch, reqs, err := nch.Accept()
	if err != nil {
		return
	}
	open.add(ch)
	h.wg.Go(func() {
		defer func() { _ = ch.Close() }()
		defer open.remove(ch)
		var env []string
		started := false
		var cmd *exec.Cmd
		for r := range reqs {
			h.record(Request{Type: r.Type, Payload: r.Payload})
			var argv []string
			ok := false
			switch r.Type {
			case "env":
				var v struct{ Name, Value string }
				if ok = !started && ssh.Unmarshal(r.Payload, &v) == nil; ok {
					env = append(env, v.Name+"="+v.Value)
				}
			case "pty-req":
				var pty struct {
					Term                         string
					Columns, Rows, Width, Height uint32
					Modes                        string
				}
				if ok = !started && ssh.Unmarshal(r.Payload, &pty) == nil; ok {
					env = append(env, "TERM="+pty.Term)
				}
			case "window-change":
				ok = true
			case "exec":
				var command struct{ Command string }
				if ssh.Unmarshal(r.Payload, &command) == nil {
					argv = []string{"-c", command.Command}
				}
			case "shell":
				argv = []string{}
			case "subsystem":
				var subsystem struct{ Name string }
				if ssh.Unmarshal(r.Payload, &subsystem) == nil && h.settings.subsystems[subsystem.Name] != "" {
					argv = []string{"-c", h.settings.subsystems[subsystem.Name]}
				}
			case "signal":
				var signal struct{ Signal string }
				ok = cmd != nil && ssh.Unmarshal(r.Payload, &signal) == nil && sendSignal(cmd, signal.Signal)
			}
			start := argv != nil && !started
			if start {
				ok, started = true, true
			}
			// The reply goes first: a command that ends at once closes the
			// channel, and a reply after that is lost. The command still
			// starts before the next request, such as a signal, is read.
			_ = r.Reply(ok, nil)
			if start {
				cmd = h.start(ch, argv, env)
			}
		}
	})
}

// openSessions are the session channels open on one connection, where a
// ClientAlive probe goes, the oldest first, as sshd sends it on the first
// open channel it finds.
type openSessions struct {
	mu       sync.Mutex
	channels []ssh.Channel
}

func (o *openSessions) add(ch ssh.Channel) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.channels = append(o.channels, ch)
}

func (o *openSessions) remove(ch ssh.Channel) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.channels = slices.DeleteFunc(o.channels, func(c ssh.Channel) bool { return c == ch })
}

func (o *openSessions) first() ssh.Channel {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.channels) == 0 {
		return nil
	}
	return o.channels[0]
}

// probeClient probes the client of conn, whose bytes come over nc, as
// ClientAlive says, until the connection ends.
func (h *Host) probeClient(conn *ssh.ServerConn, nc *hostConn, open *openSessions) {
	interval, countMax := h.settings.clientAliveInterval, h.settings.clientAliveCountMax
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var unanswered, sent, answered atomic.Int32
	// lastAnswer is when a probe was last answered, in Unix nanoseconds.
	var lastAnswer atomic.Int64
	start := time.Now()
	for {
		select {
		case <-ticker.C:
		case <-nc.closed:
			return
		}
		if time.Since(time.Unix(0, nc.lastRead.Load())) < interval {
			continue
		}
		if unanswered.Add(1) > int32(countMax) {
			// A test that sees its SSH Session end learns why from this.
			h.t.Logf("sshhost: ClientAlive closed the connection %v after it opened: %d probes unanswered; %d sent, %d answered, the last %s; the client's last byte came %v ago",
				time.Since(start).Round(time.Millisecond), countMax, sent.Load(), answered.Load(),
				since(lastAnswer.Load()), time.Since(time.Unix(0, nc.lastRead.Load())).Round(time.Millisecond))
			_ = conn.Close()
			return
		}
		// The reply may never come: each probe waits for it apart, until
		// the connection ends.
		h.wg.Go(func() {
			sent.Add(1)
			var err error
			if ch := open.first(); ch != nil {
				_, err = ch.SendRequest("keepalive@openssh.com", true, nil)
			} else {
				_, _, err = conn.SendRequest("keepalive@openssh.com", true, nil)
			}
			if err == nil {
				answered.Add(1)
				lastAnswer.Store(time.Now().UnixNano())
				unanswered.Store(0)
			}
		})
	}
}

// since says how long ago the Unix nanoseconds at were, or "never" for 0.
func since(at int64) string {
	if at == 0 {
		return "never"
	}
	return time.Since(time.Unix(0, at)).Round(time.Millisecond).String() + " ago"
}

// sendSignal sends the command the signal named name, without its "SIG", as
// sshd sends a signal request's, and says whether it knows the name.
func sendSignal(cmd *exec.Cmd, name string) bool {
	for sig, n := range signalNames {
		if n == name {
			return cmd.Process.Signal(sig) == nil
		}
	}
	return false
}

// start starts a session's command as sshd does: the Host Account's shell
// (shell: /bin/sh, or on Windows the sh.exe of Git for Windows), with argv,
// as a local process in the Host Account's home directory with HOME set to
// it and the variables env, its stdin, stdout and stderr on the channel.
// Once the command ends, it sends the exit status, or the signal that ended
// it, and closes the channel (finish). Without a shell it fails the test,
// naming what is missing.
func (h *Host) start(ch ssh.Channel, argv, env []string) *exec.Cmd {
	sh, err := shell()
	var cmd *exec.Cmd
	var stdin io.WriteCloser
	if err != nil {
		h.t.Errorf("the in-process Host cannot run a command: %v", err)
	} else {
		cmd = exec.CommandContext(h.t.Context(), sh, argv...)
		cmd.Dir = h.home
		cmd.Env = append([]string{"HOME=" + h.home, "USER=" + Account, "LOGNAME=" + Account, "SHELL=/bin/sh", "PATH=" + os.Getenv("PATH")}, env...)
		cmd.Stdout, cmd.Stderr = ch, ch.Stderr()
		if stdin, err = cmd.StdinPipe(); err == nil {
			err = cmd.Start()
		}
	}
	if err != nil {
		_, _ = fmt.Fprintf(ch.Stderr(), "sh: %v\n", err)
		sendExitStatus(ch, 127)
		_ = ch.Close()
		return nil
	}
	// The command may end before the client closes its stdin, so nothing
	// waits for this copy, which ends when the channel does.
	h.wg.Go(func() {
		_, _ = io.Copy(stdin, ch)
		_ = stdin.Close()
	})
	h.wg.Go(func() { h.finish(ch, cmd) })
	return cmd
}

// finish waits for a session's command, then sends its exit status, or the
// signal that ended it, and closes the channel.
func (h *Host) finish(ch ssh.Channel, cmd *exec.Cmd) {
	defer func() { _ = ch.Close() }()
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

// globalRequests records each request on a connection and answers it. It
// takes the -R forwards, from a TCP port or a Unix socket, and their
// cancels, as sshd does by default (AllowTcpForwarding and
// AllowStreamLocalForwarding), and refuses every other request. The
// forwards end with the connection.
func (h *Host) globalRequests(conn ssh.Conn, reqs <-chan *ssh.Request) {
	f := &forwards{h: h, conn: conn, listeners: map[string]net.Listener{}}
	defer f.close()
	for r := range reqs {
		h.record(Request{Type: r.Type, Payload: r.Payload})
		var tcp struct {
			Addr string
			Port uint32
		}
		var unix struct{ SocketPath string }
		ok, reply := false, []byte(nil)
		switch r.Type {
		case "tcpip-forward":
			if ssh.Unmarshal(r.Payload, &tcp) == nil {
				ok, reply = f.listenTCP(tcp.Addr, tcp.Port)
			}
		case "cancel-tcpip-forward":
			ok = ssh.Unmarshal(r.Payload, &tcp) == nil && f.cancel(tcpForward(tcp.Addr, tcp.Port))
		case "streamlocal-forward@openssh.com":
			ok = ssh.Unmarshal(r.Payload, &unix) == nil && f.listenUnix(unix.SocketPath)
		case "cancel-streamlocal-forward@openssh.com":
			ok = ssh.Unmarshal(r.Payload, &unix) == nil && f.cancel("unix:"+unix.SocketPath)
		}
		_ = r.Reply(ok, reply)
	}
}

// forwards are the -R forwards one connection asked the Host for: a
// listener for each, by what it was asked for.
type forwards struct {
	h    *Host
	conn ssh.Conn

	mu sync.Mutex
	// closed refuses forwards once the connection has ended.
	closed    bool
	listeners map[string]net.Listener
}

// tcpForward is how a -R forward from a TCP port is named, in its request
// and its cancel alike.
func tcpForward(addr string, port uint32) string {
	return "tcp:" + net.JoinHostPort(addr, strconv.FormatUint(uint64(port), 10))
}

// listenTCP takes a tcpip-forward for addr and port: as sshd does without
// GatewayPorts, it listens on loopback whatever address was asked, on port,
// or a port of the system's choosing for 0, which the reply then names. It
// carries each connection it accepts back to the client in a
// forwarded-tcpip channel naming addr and the port.
func (f *forwards) listenTCP(addr string, port uint32) (bool, []byte) {
	var lc net.ListenConfig
	ln, err := lc.Listen(f.h.t.Context(), "tcp", net.JoinHostPort("127.0.0.1", strconv.FormatUint(uint64(port), 10)))
	if err != nil {
		return false, nil
	}
	bound := uint32(ln.Addr().(*net.TCPAddr).Port)
	if !f.add(tcpForward(addr, bound), ln) {
		return false, nil
	}
	f.h.wg.Go(func() {
		f.serve(ln, "forwarded-tcpip", func(nc net.Conn) []byte {
			from := nc.RemoteAddr().(*net.TCPAddr)
			return ssh.Marshal(struct {
				Addr       string
				Port       uint32
				OriginAddr string
				OriginPort uint32
			}{addr, bound, from.IP.String(), uint32(from.Port)})
		})
	})
	if port != 0 {
		return true, nil
	}
	return true, ssh.Marshal(struct{ Port uint32 }{bound})
}

// listenUnix takes a streamlocal-forward@openssh.com for the Unix socket
// path: it listens there, and carries each connection it accepts back to
// the client in a forwarded-streamlocal@openssh.com channel naming path.
func (f *forwards) listenUnix(path string) bool {
	var lc net.ListenConfig
	ln, err := lc.Listen(f.h.t.Context(), "unix", path)
	if err != nil {
		return false
	}
	if !f.add("unix:"+path, ln) {
		return false
	}
	payload := ssh.Marshal(struct{ SocketPath, Reserved string }{path, ""})
	f.h.wg.Go(func() {
		f.serve(ln, "forwarded-streamlocal@openssh.com", func(net.Conn) []byte { return payload })
	})
	return true
}

// add keeps ln as the forward named name, unless the connection has ended
// or the name is taken, when it closes ln.
func (f *forwards) add(name string, ln net.Listener) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, taken := f.listeners[name]; f.closed || taken {
		_ = ln.Close()
		return false
	}
	f.listeners[name] = ln
	return true
}

// cancel ends the forward named name, and says whether there was one.
func (f *forwards) cancel(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	ln, ok := f.listeners[name]
	if ok {
		_ = ln.Close()
		delete(f.listeners, name)
	}
	return ok
}

// close ends every forward, and refuses new ones.
func (f *forwards) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	for _, ln := range f.listeners {
		_ = ln.Close()
	}
	clear(f.listeners)
}

// serve carries each connection ln accepts back to the client, in a channel
// of type kind with the payload payloadOf gives, until ln is closed.
func (f *forwards) serve(ln net.Listener, kind string, payloadOf func(net.Conn) []byte) {
	for {
		nc, err := ln.Accept()
		if err != nil {
			return
		}
		f.h.wg.Go(func() {
			ch, reqs, err := f.conn.OpenChannel(kind, payloadOf(nc))
			if err != nil {
				_ = nc.Close()
				return
			}
			f.h.splice(ch, reqs, nc)
		})
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
	h.splice(ch, reqs, tc)
}

// directStreamLocal forwards a direct-streamlocal@openssh.com channel to the
// Unix socket it names, as sshd does for -L to a Unix socket.
func (h *Host) directStreamLocal(nch ssh.NewChannel) {
	var p struct {
		SocketPath string
		Reserved0  string
		Reserved1  uint32
	}
	if err := ssh.Unmarshal(nch.ExtraData(), &p); err != nil {
		_ = nch.Reject(ssh.ConnectionFailed, "malformed direct-streamlocal@openssh.com request")
		return
	}
	h.recordChannel(nch, p.SocketPath)
	var d net.Dialer
	uc, err := d.DialContext(h.t.Context(), "unix", p.SocketPath)
	if err != nil {
		_ = nch.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	ch, reqs, err := nch.Accept()
	if err != nil {
		_ = uc.Close()
		return
	}
	h.splice(ch, reqs, uc)
}

// splice carries the bytes of a forwarding channel and its connection both
// ways, closing each once the other has ended, and refuses the channel's
// requests.
func (h *Host) splice(ch ssh.Channel, reqs <-chan *ssh.Request, nc net.Conn) {
	h.wg.Go(func() { ssh.DiscardRequests(reqs) })
	h.wg.Go(func() {
		_, _ = io.Copy(nc, ch)
		_ = nc.Close()
	})
	h.wg.Go(func() {
		_, _ = io.Copy(ch, nc)
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
