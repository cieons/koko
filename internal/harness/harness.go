// Package harness is the S1 test harness: a real koko server in the test
// process, initialised with an Owner, and koko command lines run in process
// against it, as the Local caller under a temporary HOME. Its clock moves
// only when the test moves it. Everything it starts stops through t.Cleanup.
//
// It is the one package besides cmd/koko that imports both the server and the
// client groups, and only tests import it. It grows by addition: add helpers
// and options, and never change an existing helper's signature.
package harness

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cieons/koko/internal/client/apiclient"
	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/client/devicefiles"
	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/server"
	"github.com/cieons/koko/internal/server/filesystem"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/timing"
)

// What Start initialises its koko server with.
const (
	// Owner is the Owner's name, which their Personal Space takes too.
	Owner = "alice"
	// Address is the Server Address. It names no port, so a Join Code
	// carries the port the koko server listens on, and a device joining
	// with it reaches the koko server over loopback.
	Address = "127.0.0.1"
)

// listenAddr is where Start's koko server listens on TCP once it is
// initialised, unless Listen names another address: a loopback port of the
// system's choosing, so a test binds no port below 1024 and none in use.
const listenAddr = "127.0.0.1:0"

// Harness is one in-process koko server and the CLI that reaches it.
type Harness struct {
	t      testing.TB
	socket string
	home   string
	log    *syncBuffer
	clock  *clock
	// stop stops the koko server, once.
	stop func() error
	// startCalls is how many API calls Start made itself.
	startCalls int
	// web is the web listener's bound address, "" while it is off.
	web string
}

// Option changes how Start sets up the harness.
type Option func(*options)

type options struct {
	noInit      bool
	dataDir     string
	rootKeyFile string
	listen      string
	webListen   string
	socket      string
	timings     timing.Timings
	filesystems filesystem.Types
	processUIDs func() (real, effective, saved int)
}

// Mountinfo makes the koko server read the machine's mounts from path, a
// file in the format of /proc/self/mountinfo, such as one that puts its data
// directory on NFS. Without it, every path is on a local filesystem
// (LocalDisk), and no koko server the harness starts reads the real /proc.
func Mountinfo(path string) Option {
	return func(o *options) { o.filesystems = filesystem.Mountinfo(path) }
}

// LocalDisk is a filesystem.Types on which every path that exists is on
// ext4, a local filesystem.
type LocalDisk struct{}

func (LocalDisk) FilesystemType(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return "ext4", nil
}

// filesystemsOr is the mount table o names, or LocalDisk.
func (o *options) filesystemsOrLocalDisk() filesystem.Types {
	if o.filesystems == nil {
		return LocalDisk{}
	}
	return o.filesystems
}

// ProcessUIDs makes the koko server's start-up read the process's real,
// effective and saved uid as these, such as 0 for root. Without it, they are
// NotRoot's, so a koko server the harness starts runs alike in a test run as
// root.
func ProcessUIDs(real, effective, saved int) Option {
	return func(o *options) {
		o.processUIDs = func() (int, int, int) { return real, effective, saved }
	}
}

// NotRoot is a server.Config.ProcessUIDs whose process runs as uid 1000, a
// user who is not root.
func NotRoot() (real, effective, saved int) {
	return 1000, 1000, 1000
}

// processUIDsOrNotRoot is the uids o names, or NotRoot.
func (o *options) processUIDsOrNotRoot() func() (int, int, int) {
	if o.processUIDs == nil {
		return NotRoot
	}
	return o.processUIDs
}

// Timings starts the koko server with t through the timing-constants hook,
// so a test waits milliseconds where the product waits seconds, such as
// between attempts to connect to a Host.
func Timings(t timing.Timings) Option {
	return func(o *options) { o.timings = t }
}

// NoInit leaves the koko server as Start finds it, without initialising it.
func NoInit() Option {
	return func(o *options) { o.noInit = true }
}

// DataDir starts the koko server on dir, instead of a new temporary data
// directory.
func DataDir(dir string) Option {
	return func(o *options) { o.dataDir = dir }
}

// RootKeyFile starts the koko server with the Root Key file at path, instead
// of one made for the test.
func RootKeyFile(path string) Option {
	return func(o *options) { o.rootKeyFile = path }
}

// Listen makes the koko server listen on TCP at addr once it is
// initialised, such as a FreeAddr, so a test can connect to a port it knows
// before initialisation or across a restart.
func Listen(addr string) Option {
	return func(o *options) { o.listen = addr }
}

// WebListen turns the koko server's web listener on at addr, such as
// 127.0.0.1:0 for a loopback port of the system's choosing, which
// h.WebAddress names.
func WebListen(addr string) Option {
	return func(o *options) { o.webListen = addr }
}

// SocketPath makes the koko server serve its Unix socket at path, instead
// of at a new temporary path, such as the path a koko binary built for a
// test reaches its koko server at. The path must be short enough for a Unix
// socket (macOS allows 104 bytes), and nothing may answer there.
func SocketPath(path string) Option {
	return func(o *options) { o.socket = path }
}

// FreeAddr returns a loopback address with a TCP port that nothing listened
// on a moment ago, for Listen or as an address where nothing answers. A test
// running in parallel never takes its port meanwhile: the port lies below
// those the OS picks for a listener on port 0 or an outgoing connection
// (from 32768 on Linux, 49152 on macOS), and FreeAddr returns none twice in
// one test process.
func FreeAddr(t testing.TB) string {
	t.Helper()
	for range freePortTries {
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(freePorts.next()))
		var lc net.ListenConfig
		ln, err := lc.Listen(t.Context(), "tcp", addr)
		if err != nil {
			continue // something outside this process holds it
		}
		if err := ln.Close(); err != nil {
			t.Fatal(err)
		}
		return addr
	}
	t.Fatalf("no free TCP port on 127.0.0.1 in %d tries", freePortTries)
	return ""
}

// The ports FreeAddr returns, from firstFreePort up to the start of Linux's
// range for port 0, and how many it tries before it gives up.
const (
	firstFreePort = 20000
	endFreePort   = 32768
	freePortTries = 100
)

// freePorts hands out FreeAddr's ports in turn, from a random one on, so two
// test processes running side by side seldom try the same ones.
var freePorts = &portSeq{last: firstFreePort + rand.IntN(endFreePort-firstFreePort)}

type portSeq struct {
	mu   sync.Mutex
	last int
}

func (s *portSeq) next() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last++
	if s.last >= endFreePort {
		s.last = firstFreePort
	}
	return s.last
}

// Start starts a koko server in the test process on a temporary data
// directory, a Root Key file made for this test outside it, a temporary
// Unix socket and a loopback TCP port, logging at debug level, and
// initialises it over the socket with Owner and Address. It stops when the
// test ends.
func Start(t testing.TB, opts ...Option) *Harness {
	t.Helper()
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if o.dataDir == "" {
		o.dataDir = t.TempDir()
	}
	if o.rootKeyFile == "" {
		o.rootKeyFile = NewRootKeyFile(t)
	}
	if o.socket == "" {
		o.socket = filepath.Join(ShortTempDir(t), "koko.sock")
	}
	h := &Harness{
		t:      t,
		socket: o.socket,
		home:   t.TempDir(),
		log:    &syncBuffer{},
		clock:  &clock{now: time.Now().UTC().Truncate(time.Second)},
	}
	srv, err := server.Start(t.Context(), server.Config{
		DataDir:     o.dataDir,
		RootKeyPath: o.rootKeyFile,
		Listen:      cmp.Or(o.listen, listenAddr),
		WebListen:   o.webListen,
		LogLevel:    slog.LevelDebug,
		Log:         h.log,
		Filesystems: o.filesystemsOrLocalDisk(),
		ProcessUIDs: o.processUIDsOrNotRoot(),
		SocketPath:  h.socket,
		Clock:       h.clock.Now,
		Timings:     o.timings,
	})
	if err != nil {
		t.Fatalf("start the koko server: %v", err)
	}
	h.stop = sync.OnceValue(srv.Close)
	h.web = srv.WebAddress()
	t.Cleanup(func() {
		if err := h.stop(); err != nil {
			t.Errorf("stop the koko server: %v", err)
		}
	})
	if !o.noInit {
		if _, err := h.Call(api.ServerInit, api.ServerInitRequest{Owner: Owner, Address: Address}); err != nil {
			t.Fatalf("initialise the koko server: %v", err)
		}
	}
	h.startCalls = h.countCalls()
	return h
}

// Stop stops the koko server before the test ends, such as to start another
// on its data directory.
func (h *Harness) Stop() {
	h.t.Helper()
	if err := h.stop(); err != nil {
		h.t.Fatalf("stop the koko server: %v", err)
	}
}

// Call calls the operation o over the Unix socket as the Local caller, for
// an operation no command calls, such as server.init. A failure the koko
// server answered is an *api.Error.
func (h *Harness) Call[Req, Resp any](o api.Op[Req, Resp], req Req) (Resp, error) {
	h.t.Helper()
	c := apiclient.New(func(ctx context.Context) (serverconn.Conn, error) {
		return serverconn.DialSocket(ctx, h.socket)
	})
	defer c.Close()
	resp, err := c.Call(h.t.Context(), o, req)
	if e, ok := errors.AsType[*api.Error](err); ok && e.Code == api.CodeInternal.Name {
		// The answer only points at the log, which the failing test would
		// otherwise never show.
		err = fmt.Errorf("%w; the koko server logged:\n%s", err, h.Log())
	}
	return resp, err
}

// NewRootKeyFile makes a new Root Key in a file of its own in a temporary
// directory, readable by its owner only, and returns the file's path. A test
// makes every Root Key it uses; none is ever committed.
func NewRootKeyFile(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "root.key")
	if err := os.WriteFile(path, seal.NewRootKey(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// startConfig returns what completes the server.Config a start-up is given,
// for a koko server a test starts through its command line: on the socket,
// mount table and process uids opts name, and on a loopback TCP port of the
// system's choosing whatever --listen says.
func startConfig(t testing.TB, opts []Option) func(server.Config) server.Config {
	t.Helper()
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	socket := cmp.Or(o.socket, filepath.Join(ShortTempDir(t), "koko.sock"))
	filesystems, uids := o.filesystemsOrLocalDisk(), o.processUIDsOrNotRoot()
	return func(c server.Config) server.Config {
		c.Listen = listenAddr
		c.Filesystems = filesystems
		c.ProcessUIDs = uids
		c.SocketPath = socket
		return c
	}
}

// ShortTempDir makes a temporary directory whose path is short enough for a
// Unix socket inside it (macOS allows 104 bytes), removed when the test ends.
func ShortTempDir(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "koko-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// Koko runs one koko command line in process, as the Local caller under the
// harness's temporary HOME, with empty stdin. The command reads the koko
// server's clock (Now) through the clock hook, as relative dates such as
// +30d count from.
func (h *Harness) Koko(args ...string) Result {
	h.t.Helper()
	return Run(h.t, h.runner(), []string{"HOME=" + h.home}, args...)
}

// KokoEnv runs one koko command line in process, as Koko does, with the
// variables env beside HOME, such as TZ.
func (h *Harness) KokoEnv(env []string, args ...string) Result {
	h.t.Helper()
	return Run(h.t, h.runner(), append([]string{"HOME=" + h.home}, env...), args...)
}

// KokoStdin runs one koko command line in process, as Koko does, with stdin
// as its stdin, which is not a terminal.
func (h *Harness) KokoStdin(stdin string, args ...string) Result {
	h.t.Helper()
	return run(h.t, h.runner(), []string{"HOME=" + h.home}, stdin, args...)
}

// KokoTerminal runs one koko command line in process, as Koko does, with
// term as the terminal on its stdin, through the fake-terminal hook.
func (h *Harness) KokoTerminal(term *Terminal, args ...string) Result {
	h.t.Helper()
	r := h.runner()
	r.Terminal = term
	return run(h.t, r, []string{"HOME=" + h.home}, "", args...)
}

// runner is the runner of the Local caller's command lines: over the
// harness's socket, on the koko server's clock.
func (h *Harness) runner() cli.Runner {
	return cli.Runner{SocketPath: h.socket, Clock: h.clock.Now}
}

// Home returns the temporary HOME that Koko runs command lines under.
func (h *Harness) Home() string {
	return h.home
}

// Now returns the koko server's clock: the second Start began, moved only by
// Advance.
func (h *Harness) Now() time.Time {
	return h.clock.Now()
}

// Advance moves the koko server's clock forward by d, through the clock
// hook, so that a test never waits for time to pass.
func (h *Harness) Advance(d time.Duration) {
	h.clock.advance(d)
}

// SetNow sets the koko server's clock, which the Local caller's command
// lines read too, to now, earlier or later, for a test of a given day, such
// as a leap day.
func (h *Harness) SetNow(now time.Time) {
	h.clock.set(now)
}

// clock is the koko server's clock in a test, which stands still until the
// test moves it.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *clock) set(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}

// Background starts one koko command line in process, as Koko does but with
// the variables env beside HOME, for a command that runs until it is stopped,
// such as koko web.
func (h *Harness) Background(env []string, args ...string) *Process {
	h.t.Helper()
	return background(h.t, h.runner(), append([]string{"HOME=" + h.home}, env...), args...)
}

// WebAddress returns the address the koko server's web listener listens
// on, such as 127.0.0.1:52817, or "" when WebListen did not turn it on.
func (h *Harness) WebAddress() string {
	return h.web
}

// Log returns what the koko server has logged so far.
func (h *Harness) Log() string {
	return h.log.String()
}

// Calls counts the API calls the koko server has answered since Start
// returned, leaving out the call that initialised it.
func (h *Harness) Calls() int {
	return h.countCalls() - h.startCalls
}

func (h *Harness) countCalls() int {
	return strings.Count(h.Log(), `msg="api call"`)
}

// Socket returns the path of the koko server's Unix socket, for a runner on
// the koko server's own machine.
func (h *Harness) Socket() string {
	return h.socket
}

// Device is a device that runs koko under a HOME of its own on a machine
// where no koko server runs, so it reaches a koko server only over the
// Device Connection of a join.
type Device struct {
	t      testing.TB
	home   string
	runner cli.Runner
	// env are the variables its commands get beside HOME.
	env []string
	// muxes are the Device Muxes it started, when it starts any
	// (NewMuxDevice).
	muxes *muxes
}

// environ returns the environment the device's commands run with, and
// then extra.
func (d *Device) environ(extra ...string) []string {
	return slices.Concat([]string{"HOME=" + d.home}, d.env, extra)
}

// NewDevice returns a device that has not joined a koko server. Its HOME
// and the socket path it finds empty are temporary.
func NewDevice(t testing.TB) *Device {
	t.Helper()
	return &Device{
		t:      t,
		home:   t.TempDir(),
		runner: cli.Runner{SocketPath: filepath.Join(ShortTempDir(t), "koko.sock")},
	}
}

// Join issues a Join Code over the Unix socket for a new device of the User
// named user, and returns a new device joined with it, whose command lines
// read the koko server's clock, as the Local caller's do.
func (h *Harness) Join(user string) *Device {
	h.t.Helper()
	resp, err := h.Call(api.DeviceAdd, api.DeviceAddRequest{User: user})
	if err != nil {
		h.t.Fatalf("issue a Join Code for %s: %v", user, err)
	}
	d := NewDevice(h.t)
	d.runner.Clock = h.clock.Now
	d.Koko("join", resp.JoinCode.Code).WantSuccess(h.t)
	return d
}

// RunsFrom makes the device's koko run from the file exe, whose build k
// reads as it reads its own, as cmd/koko does from os.Executable: a Device
// Mux spawn reads that file's build first.
func (d *Device) RunsFrom(k cli.Kokod, exe string) {
	d.runner.Kokod = k
	d.runner.Executable = func() (string, error) { return exe, nil }
}

// UseTimings makes the device's commands run with t as their
// timing-constants hook, such as a short wait for a koko server that does
// not answer.
func (d *Device) UseTimings(t timing.Timings) {
	d.runner.Timings = t
}

// Home returns the device's HOME.
func (d *Device) Home() string {
	return d.home
}

// Dial opens a Device Connection from the joined device's files, as its
// commands do, and holds it until the test ends or the koko server closes
// it, as a long-lived connection such as the Device Mux's would.
func (d *Device) Dial() serverconn.Conn {
	d.t.Helper()
	c, key, err := devicefiles.Read(d.home)
	if err != nil {
		d.t.Fatalf("read the device's files: %v", err)
	}
	conn, err := serverconn.DialDevice(d.t.Context(), serverconn.Device{Address: c.ServerAddress, HostKeys: c.HostKeys, Key: key})
	if err != nil {
		d.t.Fatalf("open a Device Connection: %v", err)
	}
	d.t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// Koko runs one koko command line in process on the device, with empty
// stdin.
func (d *Device) Koko(args ...string) Result {
	d.t.Helper()
	return run(d.t, d.runner, d.environ(), "", args...)
}

// KokoStdin runs one koko command line in process on the device, with
// stdin as its stdin.
func (d *Device) KokoStdin(stdin string, args ...string) Result {
	d.t.Helper()
	return run(d.t, d.runner, d.environ(), stdin, args...)
}

// Background starts one koko command line in process on the device, as
// KokoEnv does but returning at once, for a command that runs until it is
// stopped, such as a koko exec that keeps its SSH Session open.
func (d *Device) Background(env []string, args ...string) *Process {
	d.t.Helper()
	return background(d.t, d.runner, d.environ(env...), args...)
}

// Run runs one koko command line in process with the runner r and the
// environment env, and returns what it printed and its exit status.
func Run(t testing.TB, r cli.Runner, env []string, args ...string) Result {
	t.Helper()
	return run(t, r, env, "", args...)
}

func run(t testing.TB, r cli.Runner, env []string, stdin string, args ...string) Result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	status := r.Run(t.Context(), cli.Invocation{
		Args:   args,
		Stdin:  strings.NewReader(stdin),
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    env,
	})
	return Result{Stdout: stdout.String(), Stderr: stderr.String(), Status: status}
}

// waitFor bounds how long a Process helper waits for a command.
const waitFor = 10 * time.Second

// Process is one koko or kokod command line running in process in the
// background.
type Process struct {
	t      testing.TB
	stdout *syncBuffer
	stderr *syncBuffer
	// interrupt ends the command's context, as Ctrl-C does.
	interrupt context.CancelFunc
	done      chan struct{}
	status    int
}

// background starts one koko command line in process with the runner r and
// the environment env, as Run does, and returns at once. The command runs
// until it exits or Stop interrupts it; it is stopped when the test ends.
func background(t testing.TB, r cli.Runner, env []string, args ...string) *Process {
	t.Helper()
	return backgroundRun(t, func(ctx context.Context, stdout, stderr io.Writer) int {
		return r.Run(ctx, cli.Invocation{
			Args:   args,
			Stdin:  strings.NewReader(""),
			Stdout: stdout,
			Stderr: stderr,
			Env:    env,
		})
	})
}

// backgroundRun starts run in process, writing to the Process's stdout and
// stderr, and returns at once. It runs until it exits or Stop interrupts
// it; it is stopped when the test ends.
func backgroundRun(t testing.TB, run func(ctx context.Context, stdout, stderr io.Writer) int) *Process {
	t.Helper()
	ctx, interrupt := context.WithCancel(context.Background())
	p := &Process{t: t, stdout: &syncBuffer{}, stderr: &syncBuffer{}, interrupt: interrupt, done: make(chan struct{})}
	go func() {
		defer close(p.done)
		p.status = run(ctx, p.stdout, p.stderr)
	}()
	t.Cleanup(func() { p.Stop() })
	return p
}

// WaitStdout waits until the command's stdout holds substr, and returns its
// stdout so far. It fails the test when the command exits first or 10 s
// pass.
func (p *Process) WaitStdout(substr string) string {
	p.t.Helper()
	deadline := time.Now().Add(waitFor)
	for {
		out := p.stdout.String()
		if strings.Contains(out, substr) {
			return out
		}
		select {
		case <-p.done:
			p.t.Fatalf("the command exited with status %d before printing %q: stdout %q, stderr %q", p.status, substr, p.stdout.String(), p.stderr.String())
		case <-time.After(10 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			p.t.Fatalf("the command did not print %q within %v: stdout %q, stderr %q", substr, waitFor, out, p.stderr.String())
		}
	}
}

// Stop interrupts the command, as Ctrl-C does, and returns what it printed
// and its exit status once it has exited.
func (p *Process) Stop() Result {
	p.t.Helper()
	p.interrupt()
	return p.Wait()
}

// Wait waits for the command to exit by itself and returns what it printed
// and its exit status. It fails the test when 10 s pass first.
func (p *Process) Wait() Result {
	p.t.Helper()
	select {
	case <-p.done:
	case <-time.After(waitFor):
		p.t.Fatalf("the command did not exit within %v: stdout %q, stderr %q", waitFor, p.stdout.String(), p.stderr.String())
	}
	return Result{Stdout: p.stdout.String(), Stderr: p.stderr.String(), Status: p.status}
}

// Result is what one koko or kokod command line printed, and its exit
// status.
type Result struct {
	Stdout string
	Stderr string
	Status int
}

// WantSuccess fails the test unless the command exited 0 with nothing on
// stderr.
func (r Result) WantSuccess(t testing.TB) {
	t.Helper()
	if r.Status != 0 || r.Stderr != "" {
		t.Fatalf("exit status %d, stderr %q, want success", r.Status, r.Stderr)
	}
}

// WantError fails the test unless a command run with --json exited with
// status, left stdout empty, and wrote the error object with code to stderr.
// It returns the error's message.
func (r Result) WantError(t testing.TB, status int, code string) string {
	t.Helper()
	if r.Status != status {
		t.Errorf("exit status %d, want %d (stderr %q)", r.Status, status, r.Stderr)
	}
	if r.Stdout != "" {
		t.Errorf("stdout = %q, want it empty on failure", r.Stdout)
	}
	var e api.ErrorResponse
	if err := json.Unmarshal([]byte(r.Stderr), &e, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("stderr %q is not one error object: %v", r.Stderr, err)
	}
	if e.Error.Code != code || e.Error.Message == "" {
		t.Errorf("error = %+v, want code %q with a message", e.Error, code)
	}
	return e.Error.Message
}

// syncBuffer is a log the koko server writes while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
