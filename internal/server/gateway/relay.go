package gateway

import (
	"context"
	"errors"
	"io"
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

// relay opens on the Host each channel the client opens, as the client asked
// for it, and relays the two both ways, until the client or the Host closes
// its connection, or ctx ends with the Tunnel. When the Host closed first,
// what it sent before is passed on to the client first. It then closes both
// and returns why the SSH Session ended. The client's global requests are
// refused: the forwards they ask for come later. The Host's are answered by
// the Host's client, which refuses them, and so are the channels the Host
// opens.
func (s *sshSession) relay(ctx context.Context, client *ssh.ServerConn, chans <-chan ssh.NewChannel, reqs <-chan *ssh.Request) string {
	var wg sync.WaitGroup
	defer wg.Wait()
	wg.Go(func() { ssh.DiscardRequests(reqs) })
	wg.Go(func() {
		var channels sync.WaitGroup
		for nch := range chans {
			channels.Go(func() { s.channel(nch) })
		}
		channels.Wait()
	})

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
	if err == nil && !s.startFromHost() {
		_ = clientCh.Close()
		err = errors.New("the Host's connection has ended")
	}
	if err != nil {
		_ = hostCh.Close()
		ssh.DiscardRequests(hostReqs)
		return
	}
	var wg sync.WaitGroup
	wg.Go(func() { forward(hostCh, clientCh, clientReqs, nil) })
	wg.Go(func() {
		defer s.fromHost.Done()
		forward(clientCh, hostCh, hostReqs, s.sawHostRequest)
	})
	wg.Wait()
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
// dst's reply when src wants one; saw, unless nil, sees each request first.
// Once src is closed it closes dst.
func forward(dst, src ssh.Channel, reqs <-chan *ssh.Request, saw func(*ssh.Request)) {
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
		ok, err := dst.SendRequest(r.Type, r.WantReply, r.Payload)
		if r.WantReply {
			_ = r.Reply(ok && err == nil, nil)
		}
	}
	<-eof
	_ = dst.Close()
}
