package harness

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"slices"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cieons/koko/internal/client/cli"
)

// Proxy is one koko command line running in process in the background as
// ssh runs its ProxyCommand, such as koko tunnel: its stdin and stdout are a
// pipe each, which Conn joins into one connection for an SSH client to run
// on, and Signal sends it a signal as the OS would, through
// cli.Runner.Signals.
type Proxy struct {
	t       testing.TB
	stdinR  *io.PipeReader
	stdinW  *io.PipeWriter
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter
	stderr  *syncBuffer
	// interrupt ends the command's context, as Ctrl-C does.
	interrupt context.CancelFunc
	done      chan struct{}
	status    int

	mu sync.Mutex
	// notified holds each channel the command asked for signals on, with
	// the signals it asked for.
	notified map[chan<- os.Signal][]os.Signal
}

// errExited is what a write to the stdin of a Proxy that has exited fails
// with, as a write to a pipe whose reader is gone does.
var errExited = errors.New("the command has exited")

// Proxy starts one koko command line in process, as Koko does, with a pipe
// for its stdin and one for its stdout, as ssh runs its ProxyCommand. It
// is stopped when the test ends.
func (h *Harness) Proxy(args ...string) *Proxy {
	h.t.Helper()
	return proxy(h.t, h.runner(), []string{HomeEnv(h.home)}, args...)
}

// Proxy starts one koko command line in process on the device, as KokoEnv
// does, with a pipe for its stdin and one for its stdout, as ssh runs its
// ProxyCommand. It is stopped when the test ends.
func (d *Device) Proxy(env []string, args ...string) *Proxy {
	d.t.Helper()
	return proxy(d.t, d.runner, d.environ(env...), args...)
}

func proxy(t testing.TB, r cli.Runner, env []string, args ...string) *Proxy {
	t.Helper()
	ctx, interrupt := context.WithCancel(context.Background())
	p := &Proxy{t: t, stderr: &syncBuffer{}, interrupt: interrupt, done: make(chan struct{}), notified: map[chan<- os.Signal][]os.Signal{}}
	p.stdinR, p.stdinW = io.Pipe()
	p.stdoutR, p.stdoutW = io.Pipe()
	r.Signals = p.notify
	go func() {
		defer close(p.done)
		p.status = r.Run(ctx, cli.Invocation{
			Args:   args,
			Stdin:  p.stdinR,
			Stdout: p.stdoutW,
			Stderr: p.stderr,
			Env:    env,
		})
		_ = p.stdoutW.Close()
		_ = p.stdinR.CloseWithError(errExited)
	}()
	t.Cleanup(p.stop)
	return p
}

// notify is the command's cli.Runner.Signals: it relays to c each signal
// of sigs that Signal sends, until stop is called.
func (p *Proxy) notify(c chan<- os.Signal, sigs ...os.Signal) (stop func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.notified[c] = sigs
	return func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		delete(p.notified, c)
	}
}

// Signal sends the command sig, as the OS would: to each channel the
// command asked for it on, without waiting for a channel that is full. A
// signal it did not ask for is lost.
func (p *Proxy) Signal(sig os.Signal) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for c, sigs := range p.notified {
		if slices.Contains(sigs, sig) {
			select {
			case c <- sig:
			default:
			}
		}
	}
}

// Interrupt ends the command's context, as Ctrl-C or SIGTERM does to every
// koko command.
func (p *Proxy) Interrupt() {
	p.interrupt()
}

// Conn joins the command's stdout and stdin into one connection, as ssh
// sees its ProxyCommand: reads read the command's stdout, writes write its
// stdin, and Close closes its stdin, which the command reads to its end,
// while its stdout stays to be read until it exits.
func (p *Proxy) Conn() net.Conn {
	return &proxyConn{p: p}
}

// Done is closed once the command has exited.
func (p *Proxy) Done() <-chan struct{} {
	return p.done
}

// Wait waits for the command to exit by itself and returns what it wrote to
// stderr and its exit status; its stdout went to Conn. It fails the test
// when 10 s pass first.
func (p *Proxy) Wait() Result {
	p.t.Helper()
	select {
	case <-p.done:
	case <-time.After(waitFor):
		p.t.Fatalf("the command did not exit within %v: stderr %q", waitFor, p.stderr.String())
	}
	return Result{Stderr: p.stderr.String(), Status: p.status}
}

// stop ends the command as ssh does when it exits: it closes the pipes and
// sends SIGHUP. It then waits for the command, at most 10 s.
func (p *Proxy) stop() {
	_ = p.stdinW.Close()
	_ = p.stdoutR.Close()
	p.Signal(syscall.SIGHUP)
	select {
	case <-p.done:
	case <-time.After(waitFor):
		p.t.Errorf("the command did not exit within %v of losing its pipes and a SIGHUP: stderr %q", waitFor, p.stderr.String())
	}
}

// proxyConn is a Proxy's stdin and stdout as one net.Conn.
type proxyConn struct {
	p *Proxy
}

func (c *proxyConn) Read(b []byte) (int, error)  { return c.p.stdoutR.Read(b) }
func (c *proxyConn) Write(b []byte) (int, error) { return c.p.stdinW.Write(b) }
func (c *proxyConn) Close() error                { return c.p.stdinW.Close() }

func (c *proxyConn) LocalAddr() net.Addr  { return proxyAddr("ssh") }
func (c *proxyConn) RemoteAddr() net.Addr { return proxyAddr("koko tunnel") }

var errNoDeadline = errors.New("a ProxyCommand's pipes have no deadlines")

func (c *proxyConn) SetDeadline(time.Time) error      { return errNoDeadline }
func (c *proxyConn) SetReadDeadline(time.Time) error  { return errNoDeadline }
func (c *proxyConn) SetWriteDeadline(time.Time) error { return errNoDeadline }

// proxyAddr names one end of a Proxy's connection.
type proxyAddr string

func (proxyAddr) Network() string  { return "pipe" }
func (a proxyAddr) String() string { return string(a) }
