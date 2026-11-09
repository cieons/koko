package resumable_test

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cieons/koko/internal/shared/resumable"
)

// start starts both ends of a new Tunnel's stream, the device's on devCh and
// the Gateway's on gwCh, and closes them when the test ends.
func start(t *testing.T, devCh, gwCh io.ReadWriteCloser) (dev, gw *resumable.Stream) {
	t.Helper()
	dev, gw = resumable.New(devCh), resumable.New(gwCh)
	t.Cleanup(func() {
		_ = dev.Close()
		_ = gw.Close()
	})
	return dev, gw
}

// open starts both ends of a new Tunnel's stream on one in-memory channel.
func open(t *testing.T) (dev, gw *resumable.Stream) {
	t.Helper()
	a, b := net.Pipe()
	return start(t, a, b)
}

// await runs f in a goroutine of its own and fails the test unless f returns
// within a minute. A bubble's clock moves only once every goroutine in it is
// blocked, so the minute passes at once when what f waits for never comes.
func await(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(time.Minute):
		t.Fatalf("%s: never returned", what)
	}
}

// readN reads exactly n bytes from s.
func readN(t *testing.T, s *resumable.Stream, n int) string {
	t.Helper()
	buf := make([]byte, n)
	var err error
	await(t, fmt.Sprintf("read %d bytes", n), func() { _, err = io.ReadFull(s, buf) })
	if err != nil {
		t.Fatalf("read %d bytes: %v", n, err)
	}
	return string(buf)
}

// readAll reads s up to the other end's EOF or close.
func readAll(t *testing.T, s *resumable.Stream) (string, error) {
	t.Helper()
	var got []byte
	var err error
	await(t, "read to the end", func() { got, err = io.ReadAll(s) })
	return string(got), err
}

// write writes p to s from a goroutine of its own, so that a test can read
// while the write blocks; the returned channel yields its error.
func write(s *resumable.Stream, p string) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := s.Write([]byte(p))
		done <- err
	}()
	return done
}

func TestBytesFlowBothWays(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dev, gw := open(t)
		toGW, toDev := write(dev, "ping"), write(gw, "pong")
		if got := readN(t, gw, 4); got != "ping" {
			t.Errorf("gw read %q, want ping", got)
		}
		if got := readN(t, dev, 4); got != "pong" {
			t.Errorf("dev read %q, want pong", got)
		}
		for _, err := range []error{<-toGW, <-toDev} {
			if err != nil {
				t.Errorf("write: %v", err)
			}
		}
	})
}

func TestCloseWriteEndsOneDirection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dev, gw := open(t)
		if _, err := dev.Write([]byte("bye")); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := dev.CloseWrite(); err != nil {
			t.Fatalf("close write: %v", err)
		}
		if got, err := readAll(t, gw); err != nil || got != "bye" {
			t.Errorf("gw read %q, %v; want bye and EOF", got, err)
		}
		if _, err := dev.Write([]byte("more")); err == nil {
			t.Error("dev wrote after CloseWrite")
		}
		toDev := write(gw, "still open")
		if got := readN(t, dev, 10); got != "still open" {
			t.Errorf("dev read %q after its CloseWrite, want the other direction open", got)
		}
		if err := <-toDev; err != nil {
			t.Errorf("gw write: %v", err)
		}
	})
}

func TestCloseCarriesItsReasonAfterTheData(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dev, gw := open(t)
		if _, err := dev.Write([]byte("last words")); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := dev.CloseWithReason("the ssh client exited"); err != nil {
			t.Fatalf("close: %v", err)
		}
		got, err := readAll(t, gw)
		if got != "last words" {
			t.Errorf("gw read %q before the close, want last words", got)
		}
		if !isPeerClose(err, "the ssh client exited") {
			t.Errorf("gw read error %v, want the peer's close with its reason", err)
		}
		if _, err := gw.Write([]byte("x")); err == nil {
			t.Error("gw wrote after the peer closed")
		}
		if _, err := dev.Read(make([]byte, 1)); !errors.Is(err, resumable.ErrClosed) {
			t.Errorf("dev read after its close: %v, want ErrClosed", err)
		}
		if _, err := dev.Write([]byte("x")); !errors.Is(err, resumable.ErrClosed) {
			t.Errorf("dev write after its close: %v, want ErrClosed", err)
		}
	})
}

// isPeerClose says whether err is the other end's close with reason.
func isPeerClose(err error, reason string) bool {
	pce, ok := errors.AsType[*resumable.PeerClosedError](err)
	return ok && pce.Reason == reason
}

// rawPeer starts a stream on one end of an in-memory channel and gives the
// test the other end, to speak frames byte by byte.
func rawPeer(t *testing.T) (raw net.Conn, s *resumable.Stream) {
	t.Helper()
	raw, b := net.Pipe()
	s = resumable.New(b)
	t.Cleanup(func() { _ = s.Close() })
	t.Cleanup(func() { _ = raw.Close() }) // first, so s.Close sends into nothing
	return raw, s
}

// expectBytes reads len(want) bytes from raw and compares them with want.
func expectBytes(t *testing.T, raw net.Conn, what string, want ...byte) {
	t.Helper()
	got := make([]byte, len(want))
	if _, err := io.ReadFull(raw, got); err != nil {
		t.Fatalf("read %s: %v", what, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s on the wire: % x, want % x", what, got, want)
	}
}

// TestFrameLayout pins the frames of docs/subsystems/resumable-stream.md on
// the wire, with bytes worked out by hand.
func TestFrameLayout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		raw, s := rawPeer(t)

		done := write(s, "hi")
		expectBytes(t, raw, "a data frame", 0x00, 0x02, 'h', 'i')
		if err := <-done; err != nil {
			t.Fatalf("write: %v", err)
		}

		full := append([]byte{0x00, 0x80, 0x80, 0x02}, bytes.Repeat([]byte{'x'}, resumable.MaxData)...)
		if _, err := raw.Write(full); err != nil {
			t.Fatalf("write a full data frame: %v", err)
		}
		readN(t, s, resumable.MaxData)
		expectBytes(t, raw, "an acknowledgement of 32 KiB", 0x80, 0x80, 0x02, 0x00)

		if err := s.CloseWrite(); err != nil {
			t.Fatalf("close write: %v", err)
		}
		expectBytes(t, raw, "an EOF frame", 0x00, 0x81, 0x80, 0x02)

		if _, err := raw.Write([]byte{0x00, 0x81, 0x80, 0x02}); err != nil {
			t.Fatalf("write an EOF frame: %v", err)
		}
		if _, err := s.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
			t.Errorf("read after the other end's EOF frame: %v, want io.EOF", err)
		}
		if _, err := raw.Write([]byte{0x00, 0x82, 0x80, 0x02, 0x03, 'b', 'y', 'e'}); err != nil {
			t.Fatalf("write a close frame: %v", err)
		}
		synctest.Wait()
		if _, err := s.Write([]byte("x")); !isPeerClose(err, "bye") {
			t.Errorf("write after the other end's close frame: %v, want its reason", err)
		}

		raw2, s2 := rawPeer(t)
		go func() { _ = s2.CloseWithReason("bye") }()
		expectBytes(t, raw2, "a close frame", 0x00, 0x82, 0x80, 0x02, 0x03, 'b', 'y', 'e')
	})
}

// resume carries both ends onto a new in-memory channel the way a Tunnel
// Resumes: the device takes the stream off its channel and puts its received
// offset in the resume payload; the Gateway accepts with that offset and
// answers with its own. It returns the device's end of the new channel.
func resume(t *testing.T, dev, gw *resumable.Stream) net.Conn {
	t.Helper()
	sent := dev.Detach()
	a, b := net.Pipe()
	accepted := make(chan error, 1)
	go func() { accepted <- gw.AcceptResume(b, sent) }()
	if err := dev.Resume(a, sent); err != nil {
		t.Fatalf("device resume: %v", err)
	}
	if err := <-accepted; err != nil {
		t.Fatalf("gateway accept resume: %v", err)
	}
	return a
}

func TestResumeCarriesOnAfterACut(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := net.Pipe()
		dev, gw := start(t, a, b)
		if _, err := dev.Write([]byte("before ")); err != nil {
			t.Fatalf("write: %v", err)
		}
		if got := readN(t, gw, 7); got != "before " {
			t.Fatalf("gw read %q, want before", got)
		}

		_ = a.Close()
		<-dev.Detached()
		<-gw.Detached()
		toGW, toDev := write(dev, "after the cut"), write(gw, "and back")

		resume(t, dev, gw)
		if got := readN(t, gw, 13); got != "after the cut" {
			t.Errorf("gw read %q after the resume, want after the cut", got)
		}
		if got := readN(t, dev, 8); got != "and back" {
			t.Errorf("dev read %q after the resume, want and back", got)
		}
		for _, err := range []error{<-toGW, <-toDev} {
			if err != nil {
				t.Errorf("write while Detached: %v", err)
			}
		}
	})
}

// blackHole is a channel whose far end has gone silent: it takes every write
// and delivers nothing until it is closed.
type blackHole struct {
	closed chan struct{}
	close  func()
}

func newBlackHole() *blackHole {
	b := &blackHole{closed: make(chan struct{})}
	b.close = sync.OnceFunc(func() { close(b.closed) })
	return b
}

func (b *blackHole) Write(p []byte) (int, error) {
	select {
	case <-b.closed:
		return 0, net.ErrClosed
	default:
		return len(p), nil
	}
}

func (b *blackHole) Read([]byte) (int, error) {
	<-b.closed
	return 0, net.ErrClosed
}

func (b *blackHole) Close() error {
	b.close()
	return nil
}

func TestHalfCloseSurvivesAResume(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dev, gw := start(t, newBlackHole(), newBlackHole())
		if _, err := dev.Write([]byte("hello")); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := dev.CloseWrite(); err != nil {
			t.Fatalf("close write: %v", err)
		}
		synctest.Wait() // the data and the EOF are sent, and lost

		resume(t, dev, gw)
		if got, err := readAll(t, gw); err != nil || got != "hello" {
			t.Errorf("gw read %q, %v after the resume; want hello and EOF", got, err)
		}
	})
}

// cutLink is an in-memory channel cut after a budget of bytes has crossed
// it, either way: the write that spends the budget delivers only the bytes
// that fit, so the cut falls anywhere, mid-frame included.
type cutLink struct {
	mu   sync.Mutex
	left int
	a, b net.Conn
}

// newCutLink returns the two ends of a channel cut after budget bytes.
func newCutLink(budget int) (dev, gw io.ReadWriteCloser) {
	l := &cutLink{left: budget}
	l.a, l.b = net.Pipe()
	return cutEnd{l, l.a}, cutEnd{l, l.b}
}

// write writes p to one end, conn, as far as the budget goes, and cuts the
// channel where it runs out.
func (l *cutLink) write(conn net.Conn, p []byte) (int, error) {
	l.mu.Lock()
	n := min(len(p), l.left)
	l.left -= n
	l.mu.Unlock()
	if _, err := conn.Write(p[:n]); err != nil {
		return 0, err
	}
	if n < len(p) {
		_ = l.a.Close()
		_ = l.b.Close()
		return n, net.ErrClosed
	}
	return n, nil
}

type cutEnd struct {
	l *cutLink
	net.Conn
}

func (e cutEnd) Write(p []byte) (int, error) { return e.l.write(e.Conn, p) }

// transfer writes a payload into one end in chunks of random sizes and
// half-closes it, and reads the other end to its EOF in reads of random sizes.
type transfer struct {
	want, got         []byte
	writeErr, readErr error
}

func (x *transfer) run(rng *rand.Rand, from, to *resumable.Stream, wg *sync.WaitGroup) {
	writes := make([]int, 0, 64)
	for left := len(x.want); left > 0; {
		n := min(left, 1+rng.IntN(64<<10))
		writes = append(writes, n)
		left -= n
	}
	readSizes := make([]int, 256)
	for i := range readSizes {
		readSizes[i] = 1 + rng.IntN(64<<10)
	}
	wg.Go(func() {
		p := x.want
		for _, n := range writes {
			if _, err := from.Write(p[:n]); err != nil {
				x.writeErr = err
				return
			}
			p = p[n:]
		}
		if err := from.CloseWrite(); err != nil {
			x.writeErr = fmt.Errorf("close write: %w", err)
		}
	})
	wg.Go(func() {
		buf := make([]byte, 64<<10)
		for i := 0; ; i++ {
			n, err := to.Read(buf[:readSizes[i%len(readSizes)]])
			x.got = append(x.got, buf[:n]...)
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				x.readErr = err
				return
			}
		}
	})
}

// check reports where the bytes out first differ from the bytes in.
func (x *transfer) check(t *testing.T, way string) {
	t.Helper()
	for what, err := range map[string]error{"write": x.writeErr, "read": x.readErr} {
		if err != nil {
			t.Errorf("%s: %s: %v", way, what, err)
		}
	}
	if bytes.Equal(x.got, x.want) {
		return
	}
	i := 0
	for i < min(len(x.got), len(x.want)) && x.got[i] == x.want[i] {
		i++
	}
	t.Errorf("%s: %d bytes out for %d in, first different at offset %d", way, len(x.got), len(x.want), i)
}

// TestBytesSurviveAnySequenceOfAttaches moves a few MiB each way, more than
// the replay buffers hold, over channels cut at random points, sometimes in
// the offset exchange itself. Some resumes start while the Gateway's end is
// still on a working channel, so the last attach wins. Every byte must come
// out once, in order, with the EOF after it.
func TestBytesSurviveAnySequenceOfAttaches(t *testing.T) {
	seed := uint64(time.Now().UnixNano())
	t.Logf("seed %d", seed)
	for round := range uint64(4) {
		synctest.Test(t, func(t *testing.T) {
			rng := rand.New(rand.NewPCG(seed, round))
			devCh, gwCh := newCutLink(rng.IntN(256 << 10))
			dev, gw := start(t, devCh, gwCh)

			up := &transfer{want: make([]byte, 5<<19+rng.IntN(3<<19))}
			down := &transfer{want: make([]byte, 5<<19+rng.IntN(3<<19))}
			payloads := rand.NewChaCha8([32]byte{byte(seed), byte(round)})
			_, _ = payloads.Read(up.want)
			_, _ = payloads.Read(down.want)
			var wg sync.WaitGroup
			up.run(rng, dev, gw, &wg)
			down.run(rng, gw, dev, &wg)
			finished := make(chan struct{})
			go func() {
				wg.Wait()
				close(finished)
			}()

			attaches := 0
			for range 40 {
				if rng.IntN(4) > 0 { // else resume at once, racing the Gateway's end
					select {
					case <-dev.Detached():
					case <-finished:
					}
				}
				if isClosed(finished) {
					break
				}
				budget := rng.IntN(256 << 10)
				if rng.IntN(8) == 0 {
					budget = rng.IntN(16) // cut in the offset exchange, or just after
				}
				if attachEnds(t, dev, gw, budget) {
					attaches++
				}
			}
			for !isClosed(finished) && !attachEnds(t, dev, gw, math.MaxInt) {
			}
			select {
			case <-finished:
			case <-time.After(time.Minute):
				t.Fatal("the transfer stalled on a whole channel")
			}
			up.check(t, "device to Gateway")
			down.check(t, "Gateway to device")
			t.Logf("round %d: %d attaches", round, attaches)
		})
	}
}

// isClosed says whether c is closed.
func isClosed(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// attachEnds resumes both ends on a new channel cut after budget bytes, as
// the device and the Gateway do, and says whether both ends attached. A
// failed exchange leaves both Detached, to try again; an error that ended a
// stream fails the test.
func attachEnds(t *testing.T, dev, gw *resumable.Stream, budget int) bool {
	t.Helper()
	sent := dev.Detach()
	devCh, gwCh := newCutLink(budget)
	accepted := make(chan error, 1)
	go func() { accepted <- gw.AcceptResume(gwCh, sent) }()
	devErr, gwErr := dev.Resume(devCh, sent), <-accepted
	for _, s := range []*resumable.Stream{dev, gw} {
		if err := s.Err(); err != nil {
			t.Fatalf("resume ended a stream: %v", err)
		}
	}
	return devErr == nil && gwErr == nil
}

func TestAFullBufferBlocksTheWriterAndLosesNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dev, gw := open(t)
		payload := make([]byte, resumable.MaxBuffer+1)
		_, _ = rand.NewChaCha8([32]byte{}).Read(payload)

		await(t, "write a full buffer the other end does not read", func() {
			if _, err := dev.Write(payload[:resumable.MaxBuffer]); err != nil {
				t.Errorf("write: %v", err)
			}
		})
		last := write(dev, string(payload[resumable.MaxBuffer:]))
		synctest.Wait()
		select {
		case err := <-last:
			t.Fatalf("a write past a full buffer returned (%v) before the other end read", err)
		default:
		}

		resume(t, dev, gw) // what the Gateway held unread is dropped and resent
		if got := readN(t, gw, len(payload)); got != string(payload) {
			t.Error("gw read other bytes than were written")
		}
		if err := <-last; err != nil {
			t.Errorf("the blocked write: %v", err)
		}
	})
}

// readCloseFrame reads a close frame from r and returns its reason.
func readCloseFrame(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	var ack, size, n uint64
	var reason []byte
	var err error
	await(t, "read a close frame", func() {
		if ack, err = binary.ReadUvarint(r); err != nil {
			return
		}
		if size, err = binary.ReadUvarint(r); err != nil || size != resumable.MaxData+2 {
			return
		}
		if n, err = binary.ReadUvarint(r); err != nil {
			return
		}
		reason = make([]byte, n)
		_, err = io.ReadFull(r, reason)
	})
	if err != nil || size != resumable.MaxData+2 {
		t.Fatalf("read a frame of acknowledgement %d and size %d (%v), want a close frame", ack, size, err)
	}
	return string(reason)
}

// expectEnded checks that s ended for want, telling the other end so in the
// close frame read from r.
func expectEnded(t *testing.T, s *resumable.Stream, want error, r *bufio.Reader) {
	t.Helper()
	if reason := readCloseFrame(t, r); !strings.Contains(reason, want.Error()) {
		t.Errorf("the close frame says %q, want it to name %q", reason, want)
	}
	if err := s.Err(); !errors.Is(err, want) {
		t.Errorf("Err: %v, want %v", err, want)
	}
	if _, err := s.Read(make([]byte, 1)); !errors.Is(err, want) {
		t.Errorf("read: %v, want %v", err, want)
	}
	if _, err := s.Write([]byte("x")); !errors.Is(err, want) {
		t.Errorf("write: %v, want %v", err, want)
	}
}

// expectAttachEnded checks that an attach, whose result comes on attached,
// ended s for want, as expectEnded does.
func expectAttachEnded(t *testing.T, s *resumable.Stream, attached <-chan error, want error, r *bufio.Reader) {
	t.Helper()
	expectEnded(t, s, want, r)
	if err := <-attached; !errors.Is(err, want) {
		t.Errorf("attach: %v, want %v", err, want)
	}
}

func TestAnOffsetOutsideTheBufferEndsTheStream(t *testing.T) {
	t.Run("the device has received more than the Gateway sent", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			_, gw := rawPeer(t)
			devEnd, gwEnd := net.Pipe()
			accepted := make(chan error, 1)
			go func() { accepted <- gw.AcceptResume(gwEnd, 5) }()
			r := bufio.NewReader(devEnd)
			if off, err := binary.ReadUvarint(r); err != nil || off != 0 {
				t.Fatalf("the Gateway answered %d (%v), want its received offset 0", off, err)
			}
			expectAttachEnded(t, gw, accepted, resumable.ErrOffset, r)
		})
	})
	for _, c := range []struct {
		name   string
		answer uint64
	}{
		{"the Gateway has received less than the device has freed", 0},
		{"the Gateway has received more than the device sent", resumable.MaxData + 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				dev, gw := open(t)
				if _, err := dev.Write(make([]byte, resumable.MaxData)); err != nil {
					t.Fatalf("write: %v", err)
				}
				readN(t, gw, resumable.MaxData)
				synctest.Wait() // the device has the acknowledgement and freed the bytes

				sent := dev.Detach()
				devEnd, gwEnd := net.Pipe()
				resumed := make(chan error, 1)
				go func() { resumed <- dev.Resume(devEnd, sent) }()
				if _, err := gwEnd.Write(binary.AppendUvarint(nil, c.answer)); err != nil {
					t.Fatalf("answer: %v", err)
				}
				expectAttachEnded(t, dev, resumed, resumable.ErrOffset, bufio.NewReader(gwEnd))
			})
		})
	}
}

func TestAMalformedFrameEndsTheStream(t *testing.T) {
	eof := []byte{0x00, 0x81, 0x80, 0x02}
	full := append([]byte{0x00, 0x80, 0x80, 0x02}, make([]byte, resumable.MaxData)...)
	for _, c := range []struct {
		name  string
		bytes []byte
	}{
		{"a size beyond 32 KiB that is no frame type", []byte{0x00, 0x83, 0x80, 0x02}},
		{"a uvarint that overflows 64 bits", []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x02}},
		{"an acknowledgement of bytes never sent", []byte{0x05, 0x00}},
		{"data after the EOF", append(eof, 0x00, 0x01, 'x')},
		{"a second EOF", append(eof, eof...)},
		{"a close reason over 32 KiB", []byte{0x00, 0x82, 0x80, 0x02, 0x81, 0x80, 0x02}},
		{"more than the buffer unacknowledged", bytes.Repeat(full, resumable.MaxBuffer/resumable.MaxData+1)},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				raw, s := rawPeer(t)
				go func() { _, _ = raw.Write(c.bytes) }()
				expectEnded(t, s, resumable.ErrMalformed, bufio.NewReader(raw))
			})
		})
	}
}

func TestTheLastAttachWins(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// The Gateway's end is still on its old channel, which has gone
		// silent without failing, as a half-open Device Connection does.
		old := newBlackHole()
		dev, gw := start(t, newBlackHole(), old)

		resume(t, dev, gw)
		await(t, "close the Gateway's old channel", func() { <-old.closed })
		toGW := write(dev, "on the new channel")
		if got := readN(t, gw, 18); got != "on the new channel" {
			t.Errorf("gw read %q, want what the device wrote", got)
		}
		if err := <-toGW; err != nil {
			t.Errorf("write: %v", err)
		}
	})
}

func TestAFailedResumeLeavesTheStreamDetached(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dev, gw := open(t)
		if _, err := dev.Write([]byte("kept")); err != nil {
			t.Fatalf("write: %v", err)
		}
		sent := dev.Detach()

		devEnd, gwEnd := net.Pipe()
		_ = gwEnd.Close() // the channel breaks before the Gateway answers
		if err := dev.Resume(devEnd, sent); err == nil {
			t.Fatal("resumed on a channel that broke in the exchange")
		}
		devEnd, _ = net.Pipe()
		if err := dev.Resume(devEnd, sent+1); err == nil {
			t.Fatal("resumed with an offset other than the one the payload carried")
		}

		resume(t, dev, gw)
		if got := readN(t, gw, 4); got != "kept" {
			t.Errorf("gw read %q after the resume, want kept", got)
		}
	})
}

func TestCloseAbortsAResumeInProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dev, _ := open(t)
		sent := dev.Detach()
		devEnd, _ := net.Pipe() // the Gateway never answers
		resumed := make(chan error, 1)
		go func() { resumed <- dev.Resume(devEnd, sent) }()
		synctest.Wait()

		if err := dev.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		if err := <-resumed; !errors.Is(err, resumable.ErrClosed) {
			t.Errorf("the resume in progress returned %v, want ErrClosed", err)
		}
	})
}

func TestAMalformedResumeAnswerEndsTheStream(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dev, _ := open(t)
		sent := dev.Detach()
		devEnd, gwEnd := net.Pipe()
		resumed := make(chan error, 1)
		go func() { resumed <- dev.Resume(devEnd, sent) }()
		go func() { _, _ = gwEnd.Write(bytes.Repeat([]byte{0xff}, binary.MaxVarintLen64)) }()
		expectAttachEnded(t, dev, resumed, resumable.ErrMalformed, bufio.NewReader(gwEnd))
	})
}

// closedByPeer is a channel whose other end sent what it holds, then closed
// it: as an SSH channel then does, it fails every write, and its reads
// return what arrived, then io.EOF. Its reads wait for a write to fail, so
// the stream fails a send before it reads anything.
type closedByPeer struct {
	arrived     io.Reader
	writeFailed chan struct{}
	fail        func()
}

func newClosedByPeer(arrived ...byte) *closedByPeer {
	c := &closedByPeer{arrived: bytes.NewReader(arrived), writeFailed: make(chan struct{})}
	c.fail = sync.OnceFunc(func() { close(c.writeFailed) })
	return c
}

func (c *closedByPeer) Write([]byte) (int, error) {
	c.fail()
	return 0, io.EOF
}

func (c *closedByPeer) Read(p []byte) (int, error) {
	<-c.writeFailed
	return c.arrived.Read(p)
}

func (c *closedByPeer) Close() error { return nil }

// The other end's close frame, followed by its channel's close, fails a
// send of this end's that crosses it, but the frame has arrived: the stream
// still reads it, and ends with the other end's close rather than coming off
// the channel Detached, which would leave the Gateway holding a Tunnel the
// device closed for its whole grace.
func TestAFailedSendStillReadsTheCloseFrameThatArrived(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := resumable.New(newClosedByPeer(0x00, 0x82, 0x80, 0x02, 0x03, 'b', 'y', 'e'))
		t.Cleanup(func() { _ = s.Close() })
		if _, err := s.Write([]byte("x")); err != nil {
			t.Fatalf("write: %v", err)
		}
		await(t, "the stream comes off its channel", func() { <-s.Detached() })
		synctest.Wait()
		if err := s.Err(); !isPeerClose(err, "bye") {
			t.Errorf("Err once the send failed: %v, want the other end's close saying bye", err)
		}
	})
}

// A malformed frame read after a send failed ends the stream and takes it
// off its channel, though no close frame can tell the other end why.
func TestAMalformedFrameAfterAFailedSendEndsTheStream(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := resumable.New(newClosedByPeer(0x00, 0x83, 0x80, 0x02))
		t.Cleanup(func() { _ = s.Close() })
		if _, err := s.Write([]byte("x")); err != nil {
			t.Fatalf("write: %v", err)
		}
		await(t, "the stream comes off its channel", func() { <-s.Detached() })
		if err := s.Err(); !errors.Is(err, resumable.ErrMalformed) {
			t.Errorf("Err after the malformed frame: %v, want ErrMalformed", err)
		}
		await(t, "close", func() { _ = s.Close() })
	})
}

func TestErrSaysWhyTheStreamEnded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dev, gw := open(t)
		if err := gw.Err(); err != nil {
			t.Errorf("Err of a live stream: %v, want nil", err)
		}
		if err := dev.CloseWrite(); err != nil {
			t.Fatalf("close write: %v", err)
		}
		if _, err := readAll(t, gw); err != nil {
			t.Fatalf("read to the EOF: %v", err)
		}
		if err := dev.CloseWithReason("done"); err != nil {
			t.Fatalf("close: %v", err)
		}
		synctest.Wait()
		if _, err := gw.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
			t.Errorf("read after the half-close and the close: %v, want io.EOF", err)
		}
		if err := gw.Err(); !isPeerClose(err, "done") {
			t.Errorf("Err after the other end's close: %v, want its reason", err)
		}
		if err := dev.Err(); !errors.Is(err, resumable.ErrClosed) {
			t.Errorf("Err after this end's close: %v, want ErrClosed", err)
		}
	})
}
