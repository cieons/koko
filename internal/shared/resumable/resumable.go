// Package resumable is the byte stream beneath a Tunnel's SSH Session
// (ADR 0021), which outlives the channel it runs on. Its frames, offset
// exchange and failures are in docs/subsystems/resumable-stream.md.
package resumable

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

const (
	// MaxData is the most data one frame carries: one SSH channel packet.
	MaxData = 32 << 10
	// MaxBuffer is the most positions an end keeps for the other to
	// acknowledge, per direction; a full buffer blocks the writer.
	MaxBuffer = 2 << 20
)

// ackThreshold is how much an end delivers before it sends an
// acknowledgement alone: small beside MaxBuffer, so the other end never waits.
const ackThreshold = MaxData

// ErrClosed is returned once this end has closed the stream.
var ErrClosed = errors.New("resumable stream closed")

// ErrOffset is the cause of a stream's end when the other end's received
// offset is outside this end's replay buffer: gone, or never sent.
var ErrOffset = errors.New("received offset outside the replay buffer")

// PeerClosedError is returned once the other end has closed the stream.
type PeerClosedError struct {
	Reason string // what the other end gave, perhaps empty
}

func (e *PeerClosedError) Error() string {
	if e.Reason == "" {
		return "the other end closed the resumable stream"
	}
	return "the other end closed the resumable stream: " + e.Reason
}

var errWriteAfterCloseWrite = errors.New("resumable stream: write after CloseWrite")

// eofState is how far the other end's EOF has come.
type eofState int

const (
	eofNone      eofState = iota
	eofReceived           // it has arrived; Read returns io.EOF once in is empty
	eofDelivered          // Read has returned io.EOF
)

// Stream is one end of a resumable byte stream, safe for concurrent use.
type Stream struct {
	attachMu sync.Mutex // held by an attach throughout

	mu         sync.Mutex
	cond       sync.Cond // on mu: broadcast on every change below
	att        *attachment
	detached   chan struct{}      // closed once the stream is off its latest channel
	exchanging io.ReadWriteCloser // an attach's channel during its exchange
	err        error              // why the stream ended, once it has
	owesClose  bool               // a close frame carrying reason is due after the data
	reason     string

	// This end's writes, by position (a byte each, then one for the EOF):
	// acknowledged up to outAcked, sent on the current channel up to outSent,
	// written up to outEnd. out holds the data from outAcked on.
	out      []byte
	outAcked uint64
	outSent  uint64
	outEnd   uint64
	outEOF   bool // outEnd's last position is the EOF

	// The other end's writes: Read has delivered up to inDelivered, this end
	// has acknowledged up to inAcked, and in holds what arrived beyond.
	in          []byte
	inEOF       eofState
	inDelivered uint64
	inAcked     uint64
}

// attachment is the channel a stream runs on.
type attachment struct {
	ch   io.ReadWriteCloser
	r    *byteReader
	done chan struct{} // closed when the stream comes off the channel
}

// New starts a new Tunnel's stream on its first channel, which it then owns.
// Both ends start at position 0, so nothing is exchanged.
func New(ch io.ReadWriteCloser) *Stream {
	s := &Stream{}
	s.cond.L = &s.mu
	s.mu.Lock()
	s.attachLocked(ch, &byteReader{Reader: bufio.NewReader(ch)})
	s.mu.Unlock()
	return s
}

// Err returns why the stream ended (ErrClosed, *PeerClosedError, ErrOffset
// or ErrMalformed), or nil while it lives. Once the other end has half-closed,
// Read says io.EOF whatever comes after, and Err still tells.
func (s *Stream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Detach takes the stream off its channel, if any, and returns its received
// offset: what the resume payload carries, unchanged until the next attach.
func (s *Stream) Detach() uint64 {
	s.attachMu.Lock()
	defer s.attachMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropLocked(s.att)
	return s.inDelivered
}

// Detached returns a channel closed once the stream is off its latest
// channel: the channel failed, or the stream was detached or ended.
func (s *Stream) Detached() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.detached
}

// Resume attaches ch at the device, once the Gateway accepted a resume whose
// payload carried received, the offset Detach returned: it reads the
// Gateway's answer and resends from it. attach says how it fails.
func (s *Stream) Resume(ch io.ReadWriteCloser, received uint64) error {
	return s.attach(ch, func(r *byteReader, own uint64) (uint64, error) {
		if received != own {
			return 0, fmt.Errorf("resume: the payload carried offset %d, but this end has received %d", received, own)
		}
		peer, err := r.uvarint("the other end's received offset")
		if err != nil {
			return 0, fmt.Errorf("read the other end's answer: %w", err)
		}
		return peer, nil
	})
}

// AcceptResume attaches ch at the Gateway, given the device's received offset
// from the resume payload: it answers with its own, a uvarint before any
// frame, and resends from the device's. attach says how it fails.
func (s *Stream) AcceptResume(ch io.ReadWriteCloser, peerReceived uint64) error {
	return s.attach(ch, func(_ *byteReader, own uint64) (uint64, error) {
		if _, err := ch.Write(binary.AppendUvarint(nil, own)); err != nil {
			return 0, fmt.Errorf("answer with this end's received offset: %w", err)
		}
		return peerReceived, nil
	})
}

// attach carries the stream onto ch, which it then owns: off its current
// channel (the last attach wins), through exchange to the other end's received
// offset, and on from there. A failed exchange leaves the stream Detached; a
// malformed answer or an offset outside the buffer ends it, telling the other
// end why on ch.
func (s *Stream) attach(ch io.ReadWriteCloser, exchange func(r *byteReader, own uint64) (peer uint64, err error)) error {
	s.attachMu.Lock()
	defer s.attachMu.Unlock()
	s.mu.Lock()
	s.dropLocked(s.att)
	err := s.err
	own := s.inDelivered
	if err == nil {
		s.exchanging = ch
	}
	s.mu.Unlock()
	if err != nil {
		_ = ch.Close()
		return err
	}

	r := &byteReader{Reader: bufio.NewReader(ch)}
	peer, err := exchange(r, own)

	s.mu.Lock()
	s.exchanging = nil
	if s.err != nil {
		err = s.err // closed during the exchange, which then failed or not
	}
	if err == nil && (peer < s.outAcked || peer > s.outSent) {
		err = fmt.Errorf("%w: the other end has received %d, and this end holds %d to %d", ErrOffset, peer, s.outAcked, s.outSent)
	}
	ends := errors.Is(err, ErrOffset) || errors.Is(err, ErrMalformed)
	if ends {
		s.endLocked(err)
	}
	if err != nil {
		s.mu.Unlock()
		if ends {
			_, _ = ch.Write(appendClose(nil, 0, err.Error()))
		}
		_ = ch.Close()
		return err
	}
	defer s.mu.Unlock()
	s.freeLocked(peer)
	s.outSent = peer
	s.inAcked = own
	s.attachLocked(ch, r)
	return nil
}

// Read reads what the other end wrote, in order, then io.EOF once it has
// half-closed. After the other end's close, it returns what came before.
func (s *Stream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		if s.err != nil && !s.peerClosedLocked() {
			return 0, s.err
		}
		if len(s.in) > 0 {
			n := copy(p, s.in)
			s.in = trim(s.in, n)
			s.deliverLocked(uint64(n))
			return n, nil
		}
		switch s.inEOF {
		case eofReceived:
			s.inEOF = eofDelivered
			s.deliverLocked(1)
			return 0, io.EOF
		case eofDelivered:
			return 0, io.EOF
		}
		if s.err != nil {
			return 0, s.err
		}
		s.cond.Wait()
	}
}

// Write writes p to the stream. It returns once p is in the replay buffer,
// and blocks while the buffer is full.
func (s *Stream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for n < len(p) {
		if s.err != nil {
			return n, s.err
		}
		if s.outEOF {
			return n, errWriteAfterCloseWrite
		}
		room := MaxBuffer - (s.outEnd - s.outAcked)
		if room == 0 {
			s.cond.Wait()
			continue
		}
		k := int(min(room, uint64(len(p)-n)))
		s.out = append(s.out, p[n:n+k]...)
		s.outEnd += uint64(k)
		n += k
		s.cond.Broadcast()
	}
	return n, nil
}

// CloseWrite half-closes the stream: the other end reads what came before,
// then io.EOF. It blocks while the buffer is full.
func (s *Stream) CloseWrite() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		if s.err != nil {
			return s.err
		}
		if s.outEOF {
			return nil
		}
		if s.outEnd-s.outAcked < MaxBuffer {
			break
		}
		s.cond.Wait()
	}
	s.outEOF = true
	s.outEnd++
	s.cond.Broadcast()
	return nil
}

// Close ends the stream for good, giving the other end no reason.
func (s *Stream) Close() error {
	return s.CloseWithReason("")
}

// CloseWithReason ends the stream for good. On a channel, it sends what is
// unsent, then a close frame carrying reason (cut to MaxData bytes), and
// returns once the channel took them or failed; on a stalled channel, that is
// when the connection's liveness check fails it, or when Detach is called.
func (s *Stream) CloseWithReason(reason string) error {
	if len(reason) > MaxData {
		reason = strings.ToValidUTF8(reason[:MaxData], "")
	}
	s.mu.Lock()
	s.closeLocked(ErrClosed, reason)
	a, ex := s.att, s.exchanging
	s.mu.Unlock()
	if ex != nil {
		_ = ex.Close() // the attach in progress fails with ErrClosed
	}
	if a != nil {
		<-a.done
	}
	return nil
}

// endLocked ends the stream for err, unless it has ended, and says whether it
// did.
func (s *Stream) endLocked(err error) bool {
	if s.err != nil {
		return false
	}
	s.err = err
	s.cond.Broadcast()
	return true
}

// closeLocked ends the stream for err and owes the other end a close frame
// carrying reason.
func (s *Stream) closeLocked(err error, reason string) {
	if s.endLocked(err) {
		s.owesClose, s.reason = true, reason
	}
}

// peerClosedLocked says whether the other end's close ended the stream.
func (s *Stream) peerClosedLocked() bool {
	_, ok := errors.AsType[*PeerClosedError](s.err)
	return ok
}

// deliverLocked counts n positions as read, and wakes the sender once an
// acknowledgement alone is due.
func (s *Stream) deliverLocked(n uint64) {
	s.inDelivered += n
	if s.inDelivered-s.inAcked >= ackThreshold {
		s.cond.Broadcast()
	}
}

// freeLocked lets go of what the other end has acknowledged, up to upTo.
func (s *Stream) freeLocked(upTo uint64) {
	s.out = trim(s.out, int(min(upTo-s.outAcked, uint64(len(s.out)))))
	s.outAcked = upTo
	s.cond.Broadcast()
}

// attachLocked starts the stream on ch.
func (s *Stream) attachLocked(ch io.ReadWriteCloser, r *byteReader) {
	a := &attachment{ch: ch, r: r, done: make(chan struct{})}
	s.att, s.detached = a, a.done
	go s.send(a)
	go s.receive(a)
}

// dropLocked takes the stream off a, if still on it, and while the stream
// lives drops what arrived unread, which the other end resends. It closes a's
// channel in a goroutine, as closing an SSH channel can block on a stalled
// connection.
func (s *Stream) dropLocked(a *attachment) {
	if a == nil || s.att != a {
		return
	}
	s.att = nil
	close(a.done)
	go func() { _ = a.ch.Close() }()
	if s.err == nil {
		s.in = nil
		if s.inEOF == eofReceived {
			s.inEOF = eofNone
		}
	}
	s.cond.Broadcast()
}

// owesFrameLocked says whether this end has a frame to send.
func (s *Stream) owesFrameLocked() bool {
	return s.outSent < s.outEnd || s.owesClose || s.inDelivered-s.inAcked >= ackThreshold
}

// send writes frames on a's channel for as long as the stream is on it.
func (s *Stream) send(a *attachment) {
	buf := make([]byte, 0, maxFrameLen)
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		for s.att == a && !s.owesFrameLocked() {
			s.cond.Wait()
		}
		if s.att != a {
			return
		}
		var last bool
		buf, last = s.appendFrameLocked(buf[:0])
		s.mu.Unlock()
		_, err := a.ch.Write(buf)
		s.mu.Lock()
		if err != nil || last {
			s.dropLocked(a)
			return
		}
	}
}

// appendFrameLocked appends the next frame to buf: unsent data, then the EOF,
// then a close frame owed, or else an acknowledgement alone. last says it is
// the close frame.
func (s *Stream) appendFrameLocked(buf []byte) (_ []byte, last bool) {
	ack := s.inDelivered - s.inAcked
	s.inAcked = s.inDelivered
	dataEnd := s.outEnd
	if s.outEOF {
		dataEnd--
	}
	switch {
	case s.outSent < dataEnd:
		k := min(MaxData, dataEnd-s.outSent)
		off := s.outSent - s.outAcked
		s.outSent += k
		return appendData(buf, ack, s.out[off:off+k]), false
	case s.outSent < s.outEnd:
		s.outSent++
		return appendEOF(buf, ack), false
	case s.owesClose:
		s.owesClose = false
		return appendClose(buf, ack, s.reason), true
	default:
		return appendData(buf, ack, nil), false
	}
}

// receive reads frames from a's channel for as long as the stream is on it.
func (s *Stream) receive(a *attachment) {
	scratch := make([]byte, MaxData)
	for {
		f, err := readFrame(a.r, scratch)
		s.mu.Lock()
		if s.att != a {
			s.mu.Unlock()
			return
		}
		if err == nil {
			err = s.applyLocked(f)
		}
		if err != nil {
			s.stopLocked(a, err)
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}

// stopLocked handles what stopped the receiving on a: the other end's close,
// or a malformed frame, which the other end is told of, ends the stream; any
// other error is the channel's, and leaves the stream Detached.
func (s *Stream) stopLocked(a *attachment, err error) {
	if _, ok := errors.AsType[*PeerClosedError](err); ok {
		s.endLocked(err)
		s.dropLocked(a)
		return
	}
	if errors.Is(err, ErrMalformed) {
		s.closeLocked(err, err.Error())
		return
	}
	s.dropLocked(a)
}

// applyLocked applies one frame from the other end, unless its state forbids
// it.
func (s *Stream) applyLocked(f frame) error {
	if unacked := s.outSent - s.outAcked; f.ack > unacked {
		return fmt.Errorf("%w: it acknowledges %d bytes, of %d sent and unacknowledged", ErrMalformed, f.ack, unacked)
	}
	s.freeLocked(s.outAcked + f.ack)
	if f.kind == kindClose {
		return &PeerClosedError{Reason: f.reason}
	}
	if s.inEOF != eofNone && (f.kind == kindEOF || len(f.data) > 0) {
		return fmt.Errorf("%w: data or an EOF after the EOF", ErrMalformed)
	}
	n := uint64(len(f.data))
	if f.kind == kindEOF {
		n = 1
	}
	if unacked := s.inDelivered + uint64(len(s.in)) + n - s.inAcked; unacked > MaxBuffer {
		return fmt.Errorf("%w: %d bytes unacknowledged, more than the %d a buffer holds", ErrMalformed, unacked, MaxBuffer)
	}
	if f.kind == kindEOF {
		s.inEOF = eofReceived
	}
	s.in = append(s.in, f.data...)
	return nil
}

// trim drops the first n bytes of b, and lets go of its array once nothing
// is left, so that an idle stream holds no buffer.
func trim(b []byte, n int) []byte {
	if n == len(b) {
		return nil
	}
	return b[n:]
}
