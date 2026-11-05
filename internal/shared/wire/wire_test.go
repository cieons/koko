package wire_test

import (
	"io"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/cieons/koko/internal/shared/wire"
)

// heldChannel is a channel whose Write takes its bytes only once release is
// closed, as an SSH channel's Write returns only once its packet is on the
// connection. Read waits until the channel is closed.
type heldChannel struct {
	release   chan struct{}
	closed    chan struct{}
	closeOnce sync.Once

	mu  sync.Mutex
	got []byte
}

func newHeldChannel() *heldChannel {
	return &heldChannel{release: make(chan struct{}), closed: make(chan struct{})}
}

func (c *heldChannel) Read([]byte) (int, error) {
	<-c.closed
	return 0, io.EOF
}

func (c *heldChannel) Write(p []byte) (int, error) {
	<-c.release
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, p...)
	return len(p), nil
}

func (c *heldChannel) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

// A Write on the net.Conn returns only once the channel has taken its bytes,
// so an answer net/http has written is on the SSH connection when the call
// counts as answered, and closing the connection then cannot cut it (#76).
// Close closes the channel.
func TestConnWritesReachTheChannelBeforeWriteReturns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ch := newHeldChannel()
		conn := wire.Conn(ch)
		t.Cleanup(func() { _ = conn.Close() })
		const answer = "HTTP/1.1 200 OK\r\n"

		var werr error
		returned := make(chan struct{})
		go func() {
			_, werr = conn.Write([]byte(answer))
			close(returned)
		}()
		synctest.Wait()
		select {
		case <-returned:
			t.Error("Write returned before the channel took its bytes")
		default:
		}

		close(ch.release)
		<-returned
		if werr != nil {
			t.Fatalf("Write: %v", werr)
		}
		synctest.Wait()
		ch.mu.Lock()
		got := string(ch.got)
		ch.mu.Unlock()
		if got != answer {
			t.Errorf("the channel took %q, want %q", got, answer)
		}

		if err := conn.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		synctest.Wait()
		select {
		case <-ch.closed:
		default:
			t.Error("Close left the channel open")
		}
	})
}
