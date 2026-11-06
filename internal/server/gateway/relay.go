package gateway

import (
	"context"
	"errors"
	"io"
	"slices"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/hostconn"
)

// sshSession is one SSH Session the Gateway relays: the client's connection
// inside its Tunnel on one side, the Gateway's own to the Host on the other.
type sshSession struct {
	host *hostconn.Conn

	mu sync.Mutex
	// exitStatus and exitSignal are what the Host last sent on a channel to
	// say how a command ended.
	exitStatus *uint32
	exitSignal string
	// hostClosed is set once the Host's connection has ended: no channel
	// opens on it after that. fromHost counts the channels still passing on
	// what the Host sent before it ended.
	hostClosed bool
	fromHost   sync.WaitGroup
}

// Why an SSH Session ended, besides the Tunnel's own end.
const (
	clientClosed = "client closed"
	hostClosed   = "Host closed"
)

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

// forwardedChannels are the channels the Host opens for the client's -R
// forwards, which the Gateway carries back to the client. The Host's client
// refuses every other channel the Host opens.
var forwardedChannels = []string{"forwarded-tcpip", "forwarded-streamlocal@openssh.com"}

// relay opens on the Host each channel the client opens, as the client asked
// for it, and relays the two both ways, until the client or the Host closes
// its connection, or ctx ends with the Tunnel. It passes the client's -R
// forwards on to the Host (forwardRequests), and opens on the client each
// channel the Host opens for one, as the Host asked for it. When the Host
// closed first, what it sent before is passed on to the client first. It
// then closes both and returns why the SSH Session ended. The Host's own
// global requests are answered by the Host's client, which refuses them.
func (s *sshSession) relay(ctx context.Context, client *ssh.ServerConn, chans <-chan ssh.NewChannel, reqs <-chan *ssh.Request) string {
	var wg sync.WaitGroup
	defer wg.Wait()
	// Before any forward reaches the Host, so none of its channels is
	// refused.
	for _, kind := range forwardedChannels {
		fromHost := s.host.HandleChannelOpen(kind)
		wg.Go(func() { each(fromHost, func(nch ssh.NewChannel) { s.forwarded(client, nch) }) })
	}
	wg.Go(func() { s.globalRequests(reqs) })
	wg.Go(func() { each(chans, s.channel) })

	ended := make(chan string, 2)
	wg.Go(func() {
		_ = client.Wait()
		ended <- clientClosed
	})
	wg.Go(func() {
		_ = s.host.Wait()
		ended <- hostClosed
	})
	var why string
	select {
	case why = <-ended:
	case <-ctx.Done():
	}
	if why == hostClosed {
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
		why = context.Cause(ctx).Error()
	}
	_ = client.Close()
	_ = s.host.Close()
	return why
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
	s.pair(clientCh, clientReqs, hostCh, hostReqs)
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
	s.pair(clientCh, clientReqs, hostCh, hostReqs)
}

// pair relays a channel of the client's and its twin on the Host both ways
// until both are closed, counting the Host's direction among the channels
// passing on what the Host sends. Once the Host's connection has ended, it
// closes both instead.
func (s *sshSession) pair(clientCh ssh.Channel, clientReqs <-chan *ssh.Request, hostCh ssh.Channel, hostReqs <-chan *ssh.Request) {
	if !s.startFromHost() {
		_ = clientCh.Close()
		_ = hostCh.Close()
		ssh.DiscardRequests(hostReqs)
		return
	}
	// Each is held while its side's request is passed on and answered.
	var clientAsks, hostAsks sync.Mutex
	var wg sync.WaitGroup
	wg.Go(func() { forward(hostCh, clientCh, clientReqs, nil, &clientAsks, &hostAsks) })
	wg.Go(func() {
		defer s.fromHost.Done()
		forward(clientCh, hostCh, hostReqs, s.sawHostRequest, &hostAsks, &clientAsks)
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
// dst's reply when src wants one, holding srcAsks meanwhile; saw, unless nil,
// sees each request first. Once src is closed it closes dst, but only while
// dstAsks is free: the other direction may still be answering a request of
// dst's, such as an exec the Host answered just before a command that ended
// at once closed its channel, and an answer after dst is closed is lost.
func forward(dst, src ssh.Channel, reqs <-chan *ssh.Request, saw func(*ssh.Request), srcAsks, dstAsks *sync.Mutex) {
	var data sync.WaitGroup
	data.Go(func() { _, _ = io.Copy(dst, src) })
	data.Go(func() { _, _ = io.Copy(dst.Stderr(), src.Stderr()) })
	eof := make(chan struct{})
	go func() {
		data.Wait()
		_ = dst.CloseWrite()
		close(eof)
	}()
	// The requests end once src is closed, or its connection is; those it
	// sent before stay to be read.
	for r := range reqs {
		if saw != nil {
			saw(r)
		}
		srcAsks.Lock()
		ok, err := dst.SendRequest(r.Type, r.WantReply, r.Payload)
		if r.WantReply {
			_ = r.Reply(ok && err == nil, nil)
		}
		srcAsks.Unlock()
	}
	<-eof
	// The other direction's wait ends: its dst is this src, which has closed.
	dstAsks.Lock()
	_ = dst.Close()
	dstAsks.Unlock()
}
