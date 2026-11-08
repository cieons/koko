// Package devicemux is the Device Mux: the background process on a joined
// device that holds the device's one Device Connection and carries every
// koko command, koko web and koko tunnel of the device over it (ADR 0017),
// and the client every command reaches it with (Dial). The first command
// that finds none of its build answering starts one from the same binary
// (Spawn); koko join starts one too.
//
// One runs per build, under a file lock beside its socket, in the user's
// private runtime directory (Socket), for one device: one home of that OS
// user. It admits only its own OS user. It probes its Device Connection
// once it goes quiet, at once on a wake or a route change, and reconnects
// when it dies, at once and then with backoff while something waits for it.
// It exits after timing.DeviceMuxIdle with nothing connected, when koko
// disconnect stops it (Stop), when the koko server revokes its Device
// Session, once it has run the revocation cleanup, and when the device is
// no longer joined. It carries channels without reading them and holds no
// Tunnel state, so a crash ends no SSH Session. The packages under
// internal/client never import a server package.
package devicemux

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/devicefiles"
	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/shared/build"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// Config is what a Device Mux runs with.
type Config struct {
	// Home is the home directory of the device whose join the Device Mux
	// carries. It reads the device's files there at each Device Connection
	// it opens, runs the revocation cleanup there, and carries only
	// commands of this home.
	Home string
	// Socket is the path of its socket, as Socket returns it.
	Socket string
	// Log is where it logs.
	Log *slog.Logger
	// Timings is the timing-constants hook, for tests only: the zero
	// Timings is the product's.
	Timings timing.Timings
}

// Why a Device Mux stops, besides its context ending.
var (
	errIdle         = errors.New("nothing was connected to it for a while")
	errRevoked      = errors.New("the koko server revoked this device's Device Session")
	errNotJoined    = errors.New("this device is no longer joined")
	errDisconnected = errors.New("koko disconnect stopped it")
)

// Run runs a Device Mux until it has had nothing connected for
// timing.DeviceMuxIdle, the koko server revokes the device's Device Session,
// the device is no longer joined, or ctx ends; then it removes its socket.
// When another Device Mux of its build holds the lock, it waits up to
// timing.DeviceMuxStart for that one to exit, and returns nil if it does
// not. It fails only when it cannot start.
func Run(ctx context.Context, c Config) error {
	t := c.Timings.Resolve()
	lock, err := takeLock(ctx, lockPath(c.Socket), t.DeviceMuxStart)
	if err != nil || lock == nil {
		if lock == nil && err == nil {
			c.Log.Debug("another Device Mux of this build runs", "socket", c.Socket)
		}
		return err
	}
	defer func() { _ = lock.Close() }()

	// Holding the lock, it is the only one of its build: a socket there is
	// one that crashed left.
	if err := os.Remove(c.Socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove the socket a Device Mux left: %w", err)
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "unix", c.Socket)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", c.Socket, err)
	}
	_, key, _ := ed25519.GenerateKey(rand.Reader) // crypto/rand never fails
	hostKey, err := ssh.NewSignerFromKey(key)
	if err != nil {
		_ = ln.Close()
		return fmt.Errorf("make the Device Mux's host key: %w", err)
	}

	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	m := &deviceMux{
		Config: c, timings: t, ctx: ctx, stop: stop, hostKey: hostKey,
		commands: map[*commandConn]struct{}{},
		tried:    make(chan struct{}),
		retryNow: make(chan struct{}, 1),
	}
	m.idle = time.AfterFunc(t.DeviceMuxIdle, m.idleOut)
	b := build.Read()
	c.Log.Info("device mux started", "version", b.Version, "revision", b.Revision, "pid", os.Getpid(), "socket", c.Socket)

	m.wg.Go(func() { m.accept(ln) })
	m.wg.Go(m.watchSignals)
	m.wg.Go(m.refreshBareNames)
	// It connects at once, so the first command finds the Device Connection
	// open, as after koko join.
	m.mu.Lock()
	m.reconnect()
	m.mu.Unlock()

	<-ctx.Done()
	m.idle.Stop()
	_ = ln.Close() // removes the socket
	m.closeAll(context.Cause(ctx))
	m.wg.Wait()
	c.Log.Info("device mux stopped", "reason", context.Cause(ctx))
	return nil
}

// takeLock takes the lock at path, waiting up to wait for a Device Mux of
// the same build that is exiting. It returns nil, and no error, when the
// lock stays taken.
func takeLock(ctx context.Context, path string, wait time.Duration) (*os.File, error) {
	deadline := time.Now().Add(wait)
	for {
		f, err := tryLock(path)
		if f != nil || err != nil {
			return f, err
		}
		if time.Now().After(deadline) {
			return nil, nil
		}
		select {
		case <-ctx.Done():
			return nil, nil
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// deviceMux is a running Device Mux.
type deviceMux struct {
	Config
	timings timing.Timings
	// ctx ends when the Device Mux stops, with why as its cause.
	ctx     context.Context
	stop    context.CancelCauseFunc
	hostKey ssh.Signer
	wg      sync.WaitGroup

	// retryNow cuts the reconnect loop's wait short, on a wake or a route
	// change.
	retryNow chan struct{}

	mu sync.Mutex
	// current is the Device Connection it holds, nil while it holds none.
	current *deviceConn
	// reconnecting tells that the reconnect loop runs.
	reconnecting bool
	// tried is closed, and replaced, each time a try to open a Device
	// Connection ends.
	tried chan struct{}
	// waiting counts the commands, channels and Tunnels waiting to Resume
	// that wait for a Device Connection; the reconnect loop keeps trying
	// while there are any.
	waiting int
	// state is what commands are told on connecting: whether it is
	// connected, since when, and why its last try failed.
	state State
	// commands are the commands connected, each counted from when it
	// connects; the Device Mux idles out only with none.
	commands map[*commandConn]struct{}
	idle     *time.Timer
	// resuming counts the Tunnels of the commands that wait to Resume.
	resuming int
}

// deviceConn is one Device Connection the Device Mux opened.
type deviceConn struct {
	conn   serverconn.Conn
	report wire.Report
	// probeNow makes its liveness watch probe at once.
	probeNow chan struct{}
	// config is the client config it was opened with, for the revocation
	// cleanup.
	config devicefiles.Config
	// done is closed once the connection has ended and the Device Mux has
	// let go of it.
	done chan struct{}
}

// commandConn is one command's connection to the Device Mux.
type commandConn struct {
	nc net.Conn
	// ssh is the command's SSH connection once it has authenticated.
	ssh ssh.Conn
	// reports holds the latest report not yet sent to the command; only
	// the Device Mux's lock guards a send to it, and sendReports takes from
	// it.
	reports chan []byte
}

// await returns the Device Connection the Device Mux holds, waiting up to
// wait for one while it reconnects, and the state it is in. It answers
// sooner, with no Device Connection and the state saying why, once a try
// fails for a reason trying again does not change, or the Device Mux
// stops. A command waits here as it connects, and a channel as it opens.
func (m *deviceMux) await(wait time.Duration) (*deviceConn, State) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.waiting++
	defer func() { m.waiting-- }()
	for {
		if m.current != nil || m.ctx.Err() != nil {
			return m.current, m.state
		}
		m.reconnect()
		tried := m.tried
		m.mu.Unlock()
		timedOut := false
		select {
		case <-tried:
		case <-timer.C:
			timedOut = true
		case <-m.ctx.Done():
		}
		m.mu.Lock()
		if m.current == nil && (timedOut || final(m.state.Err)) {
			return nil, m.state
		}
	}
}

// final tells whether a try to open a Device Connection failed with err
// for a reason trying again does not change.
func final(err error) bool {
	return serverconn.Permanent(err) || errors.Is(err, devicefiles.ErrNotJoined)
}

// reconnect starts the reconnect loop, unless it runs already, the Device
// Mux holds a Device Connection, or it is stopping. The Device Mux's lock
// is held.
func (m *deviceMux) reconnect() {
	if m.reconnecting || m.current != nil || m.ctx.Err() != nil {
		return
	}
	m.reconnecting = true
	m.wg.Go(m.reconnectLoop)
}

// reconnectLoop tries to open a Device Connection, from the device's files
// as they are now, so a join or address that changed since the Device Mux
// started holds; each try lasts at most timing.DeviceConnectAttempt. The
// first try goes at once; while they fail, and something
// waits for a Device Connection, it tries again after
// timing.ReconnectFirst, doubling the wait up to timing.ReconnectMax, or at
// once on a wake or a route change. It stops once a try succeeds, fails for
// a reason trying again does not change, or nothing waits.
func (m *deviceMux) reconnectLoop() {
	// A wake or route change that came while no loop ran is no reason to
	// skip a wait in this one.
	select {
	case <-m.retryNow:
	default:
	}
	wait := time.Duration(0)
	for {
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-m.retryNow:
			case <-m.ctx.Done():
			}
			timer.Stop()
			m.mu.Lock()
			if m.waiting == 0 {
				// Whatever waited has given up meanwhile.
				m.reconnecting = false
				m.mu.Unlock()
				return
			}
			m.mu.Unlock()
		}
		d, err := m.open()
		m.mu.Lock()
		m.record(d, err)
		if err == nil || final(err) || m.waiting == 0 || m.ctx.Err() != nil {
			m.reconnecting = false
			m.mu.Unlock()
			return
		}
		m.mu.Unlock()
		wait = m.timings.NextReconnect(wait)
	}
}

// record takes the outcome of a try to open a Device Connection, d or why
// not, and wakes whoever waits for it. The Device Mux's lock is held.
func (m *deviceMux) record(d *deviceConn, err error) {
	close(m.tried)
	m.tried = make(chan struct{})
	if err != nil {
		if m.state.Connected || m.state.Since.IsZero() {
			m.state.Since = time.Now()
		}
		m.state.Connected, m.state.Err = false, err
		if m.ctx.Err() == nil {
			m.Log.Info("cannot open a Device Connection", "err", err)
		}
		return
	}
	if m.ctx.Err() != nil {
		// It is stopping, and closeAll may have run already.
		_ = d.conn.Close()
		return
	}
	m.current = d
	m.state = State{Connected: true, Since: time.Now()}
	m.Log.Info("device connection opened", "server", d.config.ServerAddress, "server_version", d.report.Version)
	payload := d.report.Marshal()
	for c := range m.commands {
		c.send(payload)
	}
	m.wg.Go(func() {
		// Let go of first, so a channel waiting on done opens on the next
		// Device Connection rather than this one.
		m.dropped(d, d.conn.Wait())
		close(d.done)
	})
}

// open opens a Device Connection and waits for its report. A Revoked Device
// Session runs the revocation cleanup and stops the Device Mux, and so does
// a device that is no longer joined.
func (m *deviceMux) open() (*deviceConn, error) {
	c, key, err := devicefiles.Read(m.Home)
	if errors.Is(err, devicefiles.ErrNotJoined) {
		m.mu.Lock()
		m.state = State{Since: time.Now(), Err: err}
		m.stop(errNotJoined)
		m.mu.Unlock()
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(m.ctx, m.timings.DeviceConnectAttempt)
	defer cancel()
	probeNow := make(chan struct{}, 1)
	conn, err := serverconn.DialDevice(ctx, serverconn.Device{
		Address: c.ServerAddress, HostKeys: c.HostKeys, Key: key,
		Probe: m.timings.DeviceProbe, Dead: m.timings.DeviceDead, ProbeNow: probeNow,
	})
	if _, ok := errors.AsType[*serverconn.RevokedError](err); ok {
		m.revoke(c)
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	report, err := conn.Report(ctx)
	if err != nil {
		_ = conn.Close()
		if errors.Is(conn.Wait(), serverconn.ErrRevoked) {
			// It authenticated while its revocation committed.
			m.revoke(c)
			return nil, &serverconn.RevokedError{Device: c.Device}
		}
		return nil, err
	}
	return &deviceConn{conn: conn, report: report, probeNow: probeNow, config: c, done: make(chan struct{})}, nil
}

// dropped lets go of the Device Connection d, which ended with err, and
// reconnects at once. Commands stay connected: what they open next waits
// for the next Device Connection.
func (m *deviceMux) dropped(d *deviceConn, err error) {
	if errors.Is(err, serverconn.ErrRevoked) {
		m.revoke(d.config)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current != d {
		return
	}
	m.current = nil
	m.state = State{Since: time.Now(), Err: fmt.Errorf("the Device Connection to %s dropped: %w", d.config.ServerAddress, err)}
	if m.ctx.Err() == nil {
		m.Log.Info("device connection dropped", "server", d.config.ServerAddress, "err", err)
	}
	m.reconnect()
}

// watchSignals probes the Device Connection at once each time the device
// wakes from sleep or its network routes change, or, while the Device Mux
// reconnects, tries again at once, until the Device Mux stops. It tells a
// wake by the clocks: every timing.WakeCheck it compares how far the wall
// clock moved with how far the monotonic clock did, which stands still
// while the device sleeps. A route watch the OS will not give is logged
// and done without.
func (m *deviceMux) watchSignals() {
	wake, routes := m.timings.Wake, m.timings.RouteChange
	var ticks <-chan time.Time
	if wake == nil {
		ticker := time.NewTicker(m.timings.WakeCheck)
		defer ticker.Stop()
		ticks = ticker.C
	}
	if routes == nil {
		var err error
		if routes, err = watchRoutes(m.ctx, &m.wg); err != nil {
			m.Log.Debug("no route-change watch", "err", err)
		}
	}
	last := time.Now()
	for {
		var why string
		select {
		case <-m.ctx.Done():
			return
		case <-ticks:
			now := time.Now()
			slept := now.Round(0).Sub(last.Round(0)) - now.Sub(last)
			last = now
			if slept <= m.timings.WakeCheck {
				continue
			}
			why = "wake"
		case <-wake:
			why = "wake"
		case <-routes:
			why = "route change"
		}
		m.mu.Lock()
		d := m.current
		m.mu.Unlock()
		m.Log.Debug("probing the Device Connection at once", "reason", why, "connected", d != nil)
		poke := m.retryNow
		if d != nil {
			poke = d.probeNow
		}
		select {
		case poke <- struct{}{}:
		default:
		}
	}
}

// revoke runs the revocation cleanup, once the koko server has said that
// the Device Session of the client config c was Revoked, then stops the
// Device Mux. It holds the lock throughout, so a command connecting
// meanwhile is told of the revocation only once the cleanup is done, and
// finds the revocation record.
func (m *deviceMux) revoke(c devicefiles.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = nil
	if err := devicefiles.Revoke(m.Home, c, time.Now()); err != nil {
		m.Log.Error("the Device Session was revoked, but the revocation cleanup failed", "device_session", c.DeviceSession, "err", err)
	} else {
		m.Log.Info("the Device Session was revoked: deleted this device's key and client config", "device_session", c.DeviceSession)
	}
	m.state = State{Since: time.Now(), Err: &serverconn.RevokedError{Device: c.Device}}
	m.stop(errRevoked)
}

// accept serves each command that connects, until the listener closes.
func (m *deviceMux) accept(ln net.Listener) {
	for {
		nc, err := ln.Accept()
		if err != nil {
			return
		}
		m.wg.Go(func() { m.serve(nc) })
	}
}

// serve carries one command's connection until it ends.
func (m *deviceMux) serve(nc net.Conn) {
	if uid, err := peerUID(nc); err != nil || uid != os.Getuid() {
		m.Log.Warn("refused a connection from another OS user", "uid", uid, "err", err)
		_ = nc.Close()
		return
	}
	c := m.join(nc)
	if c == nil {
		_ = nc.Close()
		return
	}
	defer m.leave(c)

	var bound *deviceConn
	cfg := &ssh.ServerConfig{
		ServerVersion: wire.Version,
		NoClientAuth:  true,
		BannerCallback: func(meta ssh.ConnMetadata) string {
			if meta.User() == disconnectUser {
				return m.disconnect(c)
			}
			if filepath.Clean(meta.User()) != filepath.Clean(m.Home) {
				return elsewhere(m.Home)
			}
			d, st := m.await(m.timings.CommandWait)
			if m.stopsQuietly() {
				// No state: the command starts a new Device Mux.
				return ""
			}
			bound = d
			m.mu.Lock()
			st.Resuming = m.resuming
			m.mu.Unlock()
			return st.banner()
		},
		NoClientAuthCallback: func(ssh.ConnMetadata) (*ssh.Permissions, error) {
			if bound == nil {
				return nil, errors.New("the Device Mux carries no Device Connection for this command")
			}
			return nil, nil
		},
	}
	cfg.AddHostKey(m.hostKey)
	conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		_ = nc.Close()
		return
	}
	defer func() { _ = conn.Close() }()
	m.wg.Go(func() { ssh.DiscardRequests(reqs) })

	m.mu.Lock()
	c.ssh = conn
	report := bound.report
	if m.current != nil {
		report = m.current.report
	}
	c.send(report.Marshal())
	m.mu.Unlock()
	m.wg.Go(func() { c.sendReports() })

	for nch := range chans {
		if nch.ChannelType() == channelResuming {
			m.wg.Go(func() { m.countResuming(nch) })
			continue
		}
		m.wg.Go(func() { m.carry(nch) })
	}
}

// countResuming counts a Tunnel of the command's that waits to Resume, for
// as long as the command holds the channelResuming it opened. The Tunnel
// waits for a Device Connection meanwhile, so the reconnect loop keeps
// trying, with its backoff, between the opens its resume sends.
func (m *deviceMux) countResuming(nch ssh.NewChannel) {
	ch, reqs, err := nch.Accept()
	if err != nil {
		return
	}
	m.wg.Go(func() { ssh.DiscardRequests(reqs) })
	m.mu.Lock()
	m.resuming++
	m.waiting++
	m.reconnect()
	m.mu.Unlock()
	// Until the command closes it, or its connection ends.
	_, _ = io.Copy(io.Discard, ch)
	_ = ch.Close()
	m.mu.Lock()
	m.resuming--
	m.waiting--
	m.mu.Unlock()
}

// stopsQuietly tells whether the Device Mux is stopping for a reason a
// command need not hear, as when it idled out: the command then starts a
// new one, while a revocation, or a join that is gone, is its answer.
func (m *deviceMux) stopsQuietly() bool {
	cause := context.Cause(m.ctx)
	return cause != nil && !errors.Is(cause, errRevoked) && !errors.Is(cause, errNotJoined)
}

// carry carries one channel a command opened onto the Device Connection,
// waiting up to timing.CommandWait for one when the Device Mux holds none,
// and passes back the koko
// server's refusal as it is. It sends the open once: only one that never
// went out, since the Device Connection had ended before the Device Mux let
// go of it, goes on the next one. An open the drop cut after it went out
// fails, since the koko server may have acted on it, as on a Tunnel's open.
func (m *deviceMux) carry(nch ssh.NewChannel) {
	var remote io.ReadWriteCloser
	var d *deviceConn
	for attempt := 0; ; attempt++ {
		var st State
		if d, st = m.await(m.timings.CommandWait); d == nil {
			_ = nch.Reject(ssh.ConnectionFailed, encodeError(st.Err).Message)
			return
		}
		var err error
		remote, err = d.conn.OpenChannel(m.ctx, nch.ChannelType(), nch.ExtraData())
		if e, ok := errors.AsType[*ssh.OpenChannelError](err); ok {
			_ = nch.Reject(e.Reason, e.Message)
			return
		}
		if err == nil {
			break
		}
		// x/crypto/ssh fails an open with io.EOF only when the connection
		// had ended before it sent it.
		if !errors.Is(err, io.EOF) || attempt > 0 {
			_ = nch.Reject(ssh.ConnectionFailed, fmt.Sprintf("the Device Connection dropped: %v", err))
			return
		}
		select {
		case <-d.done:
		case <-m.ctx.Done():
		}
	}
	ch, reqs, err := nch.Accept()
	if err != nil {
		_ = remote.Close()
		return
	}
	m.wg.Go(func() { ssh.DiscardRequests(reqs) })
	pipe(ch, remote, d.done)
}

// pipe copies bytes both ways between a command's channel and the koko
// server's, passing on each half-close, until both directions have ended or
// the Device Connection has; then it closes both channels.
func pipe(ch ssh.Channel, remote io.ReadWriteCloser, dropped <-chan struct{}) {
	closeBoth := sync.OnceFunc(func() {
		_ = ch.Close()
		_ = remote.Close()
	})
	ended := make(chan struct{})
	go func() {
		select {
		case <-dropped:
			closeBoth()
		case <-ended:
		}
	}()
	var wg sync.WaitGroup
	wg.Go(func() {
		_, _ = io.Copy(remote, ch)
		if w, ok := remote.(interface{ CloseWrite() error }); ok {
			_ = w.CloseWrite()
		}
	})
	wg.Go(func() {
		_, _ = io.Copy(ch, remote)
		_ = ch.CloseWrite()
	})
	wg.Wait()
	close(ended)
	closeBoth()
}

// join counts a command that connected, which keeps the Device Mux from
// idling out; it returns nil once the Device Mux is stopping.
func (m *deviceMux) join(nc net.Conn) *commandConn {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx.Err() != nil {
		return nil
	}
	c := &commandConn{nc: nc, reports: make(chan []byte, 1)}
	m.commands[c] = struct{}{}
	m.idle.Stop()
	return c
}

// leave forgets a command whose connection ended; the last one to leave
// starts the idle wait.
func (m *deviceMux) leave(c *commandConn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.commands[c]; !ok {
		return
	}
	delete(m.commands, c)
	close(c.reports)
	if len(m.commands) == 0 && m.ctx.Err() == nil {
		m.idle.Reset(m.timings.DeviceMuxIdle)
	}
}

// disconnect stops the Device Mux for koko disconnect, whose connection c
// asked it to, and returns the banner that answers it: how many commands
// it carried, which it ends. A command still connecting is none: it gets
// no state, and starts a new Device Mux. c is no command it ends: it
// leaves at once, so closeAll does not cut the answer short.
func (m *deviceMux) disconnect(c *commandConn) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.commands, c)
	n := 0
	for cc := range m.commands {
		if cc.ssh != nil {
			n++
		}
	}
	if m.ctx.Err() == nil {
		m.Log.Info("koko disconnect stops the device mux", "commands", n)
		m.stop(errDisconnected)
	}
	return stopped(n)
}

// idleOut stops the Device Mux if nothing has connected since the idle wait
// began. It stops it holding the lock, so a command that connects after is
// refused at join, and starts a new one.
func (m *deviceMux) idleOut() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.commands) == 0 {
		m.stop(errIdle)
	}
}

// closeAll ends every command's connection and the Device Connection, once
// the Device Mux is stopping for cause; a revocation tells each command
// first, as the koko server told the Device Mux, and so does koko
// disconnect, so what they carried ends for good.
func (m *deviceMux) closeAll(cause error) {
	type conns struct {
		nc  net.Conn
		ssh ssh.Conn
	}
	m.mu.Lock()
	commands := make([]conns, 0, len(m.commands))
	for c := range m.commands {
		commands = append(commands, conns{c.nc, c.ssh})
	}
	d := m.current
	m.mu.Unlock()
	for _, c := range commands {
		switch {
		case c.ssh == nil:
		case errors.Is(cause, errRevoked):
			_, _, _ = c.ssh.SendRequest(wire.RequestRevoked, false, nil)
		case errors.Is(cause, errDisconnected):
			_, _, _ = c.ssh.SendRequest(wire.RequestDisconnected, false, nil)
		}
		_ = c.nc.Close()
	}
	if d != nil {
		_ = d.conn.Close()
	}
}

// send hands the command the report payload, replacing one not sent yet.
// The Device Mux's lock is held.
func (c *commandConn) send(payload []byte) {
	select {
	case <-c.reports:
	default:
	}
	c.reports <- payload
}

// sendReports sends the command each report handed to it, in turn, until it
// leaves.
func (c *commandConn) sendReports() {
	for payload := range c.reports {
		_, _, _ = c.ssh.SendRequest(wire.RequestReport, false, payload)
	}
}
