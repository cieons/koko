package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"slices"
	"strconv"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/hostconn"
	"github.com/cieons/koko/internal/shared/liveness"
	"github.com/cieons/koko/internal/shared/resumable"
	"github.com/cieons/koko/internal/shared/sshconn"
	"github.com/cieons/koko/internal/shared/wire"
)

// sshSession is one SSH Session the Gateway relays: the client's connection
// inside its Tunnel on one side, the Gateway's own to the Host named
// hostName on the other, through the Relay a message names as relayName
// (host.Relay.Of), empty for none. stream is the Tunnel's, which the SSH
// Session's end closes.
type sshSession struct {
	host      *hostconn.Conn
	hostName  string
	relayName string
	stream    *resumable.Stream
	// log logs the SSH Session's requests, with its Tunnel and Host.
	log *slog.Logger

	mu sync.Mutex
	// logged holds each kind of request logged, with its subsystem or
	// target.
	logged map[string]bool
	// exitStatus and exitSignal are what the Host last sent on a channel to
	// say how a command ended.
	exitStatus *uint32
	exitSignal string
	// hostClosed is set once the Host's connection has ended: no channel
	// opens on it after that. fromHost counts the channels still passing on
	// what the Host sent before it ended.
	hostClosed bool
	fromHost   sync.WaitGroup
	// sessionChannels are the client's session channels open, where an
	// ending's notice goes.
	sessionChannels map[ssh.Channel]struct{}
}

// logRequest logs a request of the client's, the first of each kind
// (request) and target or subsystem (attrs) in the SSH Session, so a busy
// forward is logged once; never a command's text.
func (s *sshSession) logRequest(request string, attrs ...any) {
	key := fmt.Sprint(request, attrs)
	s.mu.Lock()
	seen := s.logged[key]
	s.logged[key] = true
	s.mu.Unlock()
	if !seen {
		s.log.Info("ssh session request", append([]any{"request", request}, attrs...)...)
	}
}

// forwardTarget returns where a forward goes, as the payload of the channel
// or global request of kind that asks for it says: for -L, and -D, the
// address on the Host's side the channel opens (direct-tcpip and
// direct-streamlocal@openssh.com); for -R, the address the Host listens on
// (tcpip-forward and streamlocal-forward@openssh.com). ok is false for any
// other kind, or a payload that does not parse.
func forwardTarget(kind string, payload []byte) (target string, ok bool) {
	switch kind {
	case "direct-tcpip", "tcpip-forward":
		var p struct {
			Host string
			Port uint32
			Rest []byte `ssh:"rest"`
		}
		if ssh.Unmarshal(payload, &p) != nil {
			return "", false
		}
		return net.JoinHostPort(p.Host, strconv.FormatUint(uint64(p.Port), 10)), true
	case "direct-streamlocal@openssh.com", "streamlocal-forward@openssh.com":
		var p struct {
			Path string
			Rest []byte `ssh:"rest"`
		}
		if ssh.Unmarshal(payload, &p) != nil {
			return "", false
		}
		return p.Path, true
	}
	return "", false
}

// clientClosed ends an SSH Session whose client closed its connection.
var clientClosed = &ending{reason: "client closed"}

// hostEnding says why the Host's connection ended, once it has: the
// connector found the Host or its Relay silent, or one of them, or the
// network beneath, ended it. Each notice names the leg that broke: the Host,
// or its Relay, as relayName names it.
func (s *sshSession) hostEnding() *ending {
	broken := s.host.Broken()
	leg, lost, closed := s.hostName, "Host leg lost", "Host closed"
	if _, ok := errors.AsType[*hostconn.RelayError](broken); ok {
		leg, lost, closed = s.relayName, "Relay leg lost", "Relay closed"
	}
	if e, ok := errors.AsType[*liveness.DeadError](broken); ok {
		why := fmt.Sprintf("lost the connection to %s (no answer for %s s)", leg, strconv.FormatFloat(e.Quiet.Seconds(), 'f', -1, 64))
		return &ending{reason: lost, notice: why + "; this SSH Session has ended.", why: why}
	}
	why := leg + " closed the connection"
	return &ending{reason: closed, notice: why + ".", why: why}
}

// forwardRequests are the client's global requests the Gateway passes on to
// the Host, with the Host's reply: the -R forwards, from a TCP port or a
// Unix socket, and their cancels. The Gateway answers every other one
// itself, refusing it, which OpenSSH takes as the answer its
// keepalive@openssh.com asks for: the Host never sees the client's
// keepalives.
var forwardRequests = []string{
	"tcpip-forward",
	"cancel-tcpip-forward",
	"streamlocal-forward@openssh.com",
	"cancel-streamlocal-forward@openssh.com",
}

// refusedRequests are the session requests the Gateway refuses itself, so
// the Host never sees them, with the line it writes to the session channel's
// stderr for each: agent forwarding, which is P2, and X11 forwarding, which koko
// leaves out. ssh asks for an agent without wanting a reply, so the line is
// all it shows.
var refusedRequests = map[string]string{
	"auth-agent-req@openssh.com": "koko: agent forwarding is not available through koko.\n",
	"x11-req":                    "koko: X11 forwarding is not available through koko.\n",
}

// hostKeepalive is the request with which a Host's sshd that sets
// ClientAliveInterval probes a quiet client, on an open channel if it has
// one, and wanting a reply. The Gateway answers it on the channel itself,
// refusing it, which sshd takes as the answer, as OpenSSH's client does: a
// Detached client could not answer before sshd gave up on it. On the
// connection the Host's client refuses it, as every global request of the
// Host's.
const hostKeepalive = "keepalive@openssh.com"

// forwardedChannels are the channels the Host opens for the client's -R
// forwards, which the Gateway carries back to the client. The Host's client
// refuses every other channel the Host opens.
var forwardedChannels = []string{"forwarded-tcpip", "forwarded-streamlocal@openssh.com"}

// relay opens on the Host each channel the client opens, as the client asked
// for it, and relays the two both ways, until the client or the Host closes
// its connection, or ctx ends with the Tunnel. It passes the client's -R
// forwards on to the Host (forwardRequests), and opens on the client each
// channel the Host opens for one, as the Host asked for it. When the Host's
// connection ended first, what it sent before is passed on to the client
// first. An ending with a notice, such as the Host's or the koko server
// stopping, then tells it to each session channel (tell), and closes the Tunnel's
// stream with it. It then closes both connections and returns why the SSH
// Session ended. The Host's own global requests are answered by the Host's
// client, which refuses them.
func (s *sshSession) relay(ctx context.Context, client *ssh.ServerConn, chans <-chan ssh.NewChannel, reqs <-chan *ssh.Request) *ending {
	var wg sync.WaitGroup
	defer wg.Wait()
	// Before any forward reaches the Host, so none of its channels is
	// refused.
	toClient := sshconn.Wrap(client)
	for _, kind := range forwardedChannels {
		fromHost := s.host.HandleChannelOpen(kind)
		wg.Go(func() { each(fromHost, func(nch ssh.NewChannel) { s.forwarded(toClient, nch) }) })
	}
	wg.Go(func() { s.globalRequests(reqs) })
	wg.Go(func() { each(chans, s.channel) })

	ended := make(chan *ending, 2)
	wg.Go(func() {
		_ = client.Wait()
		ended <- clientClosed
	})
	wg.Go(func() {
		_ = s.host.Wait()
		ended <- s.hostEnding()
	})
	var end *ending
	select {
	case end = <-ended:
	case <-ctx.Done():
	}
	if end != nil && end != clientClosed {
		drained := make(chan struct{})
		wg.Go(func() {
			s.mu.Lock()
			s.hostClosed = true
			s.mu.Unlock()
			s.fromHost.Wait()
			close(drained)
		})
		select {
		case <-drained:
		case <-ctx.Done():
		}
	}
	if ctx.Err() != nil {
		// The Tunnel's end, such as the koko server stopping, also closes
		// the client's connection: its cause is why.
		end = endingOf(context.Cause(ctx))
	}
	if end.notice != "" {
		s.tell(wire.TunnelNoticeLine(end.notice), &wg)
	}
	_ = s.stream.CloseWithReason(end.notice)
	_ = client.Close()
	_ = s.host.Close()
	return end
}

// tell writes line to the stderr of each session channel the client has
// open, and returns once the writes are done, or once the Tunnel's stream is
// off its channel, which a stalled client never takes them from. wg waits
// for the writes still running.
func (s *sshSession) tell(line string, wg *sync.WaitGroup) {
	s.mu.Lock()
	open := slices.Collect(maps.Keys(s.sessionChannels))
	s.mu.Unlock()
	var writes sync.WaitGroup
	for _, ch := range open {
		writes.Go(func() { _, _ = io.WriteString(ch.Stderr(), line) })
	}
	written := make(chan struct{})
	wg.Go(func() {
		writes.Wait()
		close(written)
	})
	select {
	case <-written:
	case <-s.stream.Detached():
	}
}

// each runs serve on every channel chans brings, each in a goroutine of its
// own, and returns once chans is closed and every serve has returned.
func each(chans <-chan ssh.NewChannel, serve func(ssh.NewChannel)) {
	var wg sync.WaitGroup
	for nch := range chans {
		wg.Go(func() { serve(nch) })
	}
	wg.Wait()
}

// channel opens on the Host the channel the client asks for, of the same type
// and with the same payload, and relays the two until both are closed. A
// channel the Host refuses is refused to the client with the Host's reason.
func (s *sshSession) channel(nch ssh.NewChannel) {
	if target, ok := forwardTarget(nch.ChannelType(), nch.ExtraData()); ok {
		s.logRequest("-L", "target", target)
	}
	hostCh, hostReqs, err := s.host.OpenChannel(nch.ChannelType(), nch.ExtraData())
	if err != nil {
		if e, ok := errors.AsType[*ssh.OpenChannelError](err); ok {
			_ = nch.Reject(e.Reason, e.Message)
		} else {
			_ = nch.Reject(ssh.ConnectionFailed, "the connection to the Host has ended")
		}
		return
	}
	clientCh, clientReqs, err := nch.Accept()
	if err != nil {
		_ = hostCh.Close()
		ssh.DiscardRequests(hostReqs)
		return
	}
	var pass func(*ssh.Request) bool
	if nch.ChannelType() == "session" {
		clientCh = &sessionChannel{Channel: clientCh}
		pass = func(r *ssh.Request) bool { return s.sessionRequest(clientCh, r) }
		s.mu.Lock()
		s.sessionChannels[clientCh] = struct{}{}
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			delete(s.sessionChannels, clientCh)
			s.mu.Unlock()
		}()
	}
	s.pair(clientCh, clientReqs, pass, hostCh, hostReqs)
}

// sessionChannel is a session channel of the client's. Its stderr takes the
// lines the Gateway writes itself, a refused request's and an ending's
// notice, beside what the Host's twin sends there, while the Host's
// direction may be passing on its EOF; x/crypto's channel takes neither two
// writes to one stream at once nor a write beside its CloseWrite, so these
// take turns, and no line follows the EOF.
type sessionChannel struct {
	ssh.Channel
	mu  sync.Mutex
	eof bool
}

func (c *sessionChannel) Stderr() io.ReadWriter { return sessionStderr{c} }

func (c *sessionChannel) CloseWrite() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.eof = true
	return c.Channel.CloseWrite()
}

// sessionStderr is a sessionChannel's stderr, whose writes take turns.
type sessionStderr struct {
	c *sessionChannel
}

func (s sessionStderr) Read(p []byte) (int, error) { return s.c.Channel.Stderr().Read(p) }

func (s sessionStderr) Write(p []byte) (int, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if s.c.eof {
		return 0, io.EOF
	}
	return s.c.Channel.Stderr().Write(p)
}

// sessionRequest says whether a request on the client's session channel ch
// passes to the Host. It writes the line of a refused one (refusedRequests)
// to the channel's stderr, and logs the command it asks for: a shell, an
// exec, without its text, or a subsystem by name.
func (s *sshSession) sessionRequest(ch ssh.Channel, r *ssh.Request) bool {
	if line, refused := refusedRequests[r.Type]; refused {
		_, _ = io.WriteString(ch.Stderr(), line)
		return false
	}
	var subsystem struct{ Name string }
	switch {
	case r.Type == "shell" || r.Type == "exec":
		s.logRequest(r.Type)
	case r.Type == "subsystem" && ssh.Unmarshal(r.Payload, &subsystem) == nil:
		s.logRequest(r.Type, "subsystem", subsystem.Name)
	}
	return true
}

// forwarded opens on the client the channel the Host opens for one of the
// client's -R forwards, of the same type and with the same payload, and
// relays the two until both are closed. A channel the client refuses is
// refused to the Host with the client's reason.
func (s *sshSession) forwarded(client ssh.Conn, nch ssh.NewChannel) {
	clientCh, clientReqs, err := client.OpenChannel(nch.ChannelType(), nch.ExtraData())
	if err != nil {
		if e, ok := errors.AsType[*ssh.OpenChannelError](err); ok {
			_ = nch.Reject(e.Reason, e.Message)
		} else {
			_ = nch.Reject(ssh.ConnectionFailed, "the connection to the client has ended")
		}
		return
	}
	hostCh, hostReqs, err := nch.Accept()
	if err != nil {
		_ = clientCh.Close()
		ssh.DiscardRequests(clientReqs)
		return
	}
	s.pair(clientCh, clientReqs, nil, hostCh, hostReqs)
}

// pair relays a channel of the client's and its twin on the Host both ways
// until both are closed, counting the Host's direction among the channels
// passing on what the Host sends. pass, unless nil, decides which of the
// client's requests reach the Host. Should the Host's connection break, the
// client's channel is left open for the relay's end, which tells why on it.
// Once the Host's connection has ended, it closes both instead.
func (s *sshSession) pair(clientCh ssh.Channel, clientReqs <-chan *ssh.Request, pass func(*ssh.Request) bool, hostCh ssh.Channel, hostReqs <-chan *ssh.Request) {
	if !s.startFromHost() {
		_ = clientCh.Close()
		_ = hostCh.Close()
		ssh.DiscardRequests(hostReqs)
		return
	}
	// Each is held while its side's request is passed on and answered.
	var clientAsks, hostAsks sync.Mutex
	var wg sync.WaitGroup
	wg.Go(func() { forward(hostCh, clientCh, clientReqs, pass, nil, &clientAsks, &hostAsks) })
	wg.Go(func() {
		defer s.fromHost.Done()
		saw := func(r *ssh.Request) bool {
			if r.Type == hostKeepalive {
				return false
			}
			s.sawHostRequest(r)
			return true
		}
		broke := func() bool { return s.host.Broken() != nil }
		forward(clientCh, hostCh, hostReqs, saw, broke, &hostAsks, &clientAsks)
	})
	wg.Wait()
}

// globalRequests passes each of the client's global requests that asks for
// a -R forward, or cancels one, on to the Host, and answers it with the
// Host's reply, such as the port the Host chose; it refuses every other
// one. The replies keep the requests' order, as SSH requires.
func (s *sshSession) globalRequests(reqs <-chan *ssh.Request) {
	for r := range reqs {
		if !slices.Contains(forwardRequests, r.Type) {
			_ = r.Reply(false, nil)
			continue
		}
		if target, ok := forwardTarget(r.Type, r.Payload); ok {
			s.logRequest("-R", "target", target)
		}
		ok, payload, err := s.host.SendRequest(r.Type, r.WantReply, r.Payload)
		_ = r.Reply(ok && err == nil, payload)
	}
}

// startFromHost counts one more channel passing on what the Host sends,
// unless the Host's connection has ended.
func (s *sshSession) startFromHost() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hostClosed {
		return false
	}
	s.fromHost.Add(1)
	return true
}

// sawHostRequest keeps how a command ended, as the Host says on a channel,
// for the SSH Session's end in the log.
func (s *sshSession) sawHostRequest(r *ssh.Request) {
	var status struct{ Status uint32 }
	var signal struct {
		Signal     string
		CoreDumped bool
		Error      string
		Lang       string
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case r.Type == "exit-status" && ssh.Unmarshal(r.Payload, &status) == nil:
		s.exitStatus = &status.Status
	case r.Type == "exit-signal" && ssh.Unmarshal(r.Payload, &signal) == nil:
		s.exitSignal = signal.Signal
	}
}

// exit returns the log fields for how the Host said a command ended: its
// exit status, the signal that ended it, both, or neither.
func (s *sshSession) exit() []any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var attrs []any
	if s.exitStatus != nil {
		attrs = append(attrs, "exit_status", *s.exitStatus)
	}
	if s.exitSignal != "" {
		attrs = append(attrs, "exit_signal", s.exitSignal)
	}
	return attrs
}

// forward relays to dst what src sends: its data and extended data, the EOF
// that half-closes it, once both have ended, and each request, answered with
// dst's reply when src wants one, holding srcAsks meanwhile; pass, unless
// nil, sees each request first, and one it does not pass is refused. Once
// src is closed it closes dst, but only while dstAsks is free: the other
// direction may still be answering a request of dst's, such as an exec the
// Host answered just before a command that ended at once closed its
// channel, and an answer after dst is closed is lost. When broke, unless
// nil, says src's data ended because its connection broke, dst is left as
// it is instead, for the relay's end to write its notice to.
func forward(dst, src ssh.Channel, reqs <-chan *ssh.Request, pass func(*ssh.Request) bool, broke func() bool, srcAsks, dstAsks *sync.Mutex) {
	var data sync.WaitGroup
	data.Go(func() { _, _ = io.Copy(dst, src) })
	data.Go(func() { _, _ = io.Copy(dst.Stderr(), src.Stderr()) })
	eof := make(chan bool, 1)
	go func() {
		data.Wait()
		keep := broke != nil && broke()
		if !keep {
			_ = dst.CloseWrite()
		}
		eof <- keep
	}()
	// The requests end once src is closed, or its connection is; those it
	// sent before stay to be read.
	for r := range reqs {
		if pass != nil && !pass(r) {
			_ = r.Reply(false, nil)
			continue
		}
		srcAsks.Lock()
		ok, err := dst.SendRequest(r.Type, r.WantReply, r.Payload)
		if r.WantReply {
			_ = r.Reply(ok && err == nil, nil)
		}
		srcAsks.Unlock()
	}
	if keep := <-eof; !keep {
		// The other direction's wait ends: its dst is this src, which has
		// closed.
		dstAsks.Lock()
		_ = dst.Close()
		dstAsks.Unlock()
	}
}
