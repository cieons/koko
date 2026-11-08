package devicemux_test

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// What the koko server sent on a channel before it revoked this device's
// Device Session and closed the Device Connection, such as its answer to
// the call that revoked it, reaches the command in full before the Device
// Mux tells the command and exits. The answer is longer than the command's
// channel window, so part of it is still in the Device Mux when the Device
// Connection closes, and the command reads it only once the Device Mux is
// stopping.
func TestWhatTheKokoServerSentBeforeARevocationReachesTheCommand(t *testing.T) {
	answer := bytes.Repeat([]byte("answer, "), 3<<20/8)
	srv := startReporter(t, answerThenRevoke(t, answer))
	home, socket, muxLog := runMux(t, srv, timing.Timings{})
	conn := dial(t, socket, home)
	ch, err := conn.OpenChannel(t.Context(), wire.ChannelAPI, nil)
	if err != nil {
		t.Fatalf("open a channel: %v", err)
	}

	// The Device Mux removes its socket as it stops, just before it ends
	// the commands' connections.
	deadline := time.Now().Add(10 * time.Second)
	for _, err := os.Stat(socket); !errors.Is(err, os.ErrNotExist); _, err = os.Stat(socket) {
		if time.Now().After(deadline) {
			t.Fatalf("the Device Mux did not stop within 10 s; it logged:\n%s", muxLog)
		}
		time.Sleep(5 * time.Millisecond)
	}
	got, _ := io.ReadAll(ch)
	if !bytes.Equal(got, answer) {
		t.Errorf("the command got %d bytes of the koko server's %d-byte answer", len(got), len(answer))
	}
	if err := conn.Wait(); !errors.Is(err, serverconn.ErrRevoked) {
		t.Errorf("the command's connection ended with %v, want %v", err, serverconn.ErrRevoked)
	}
}

// A command that does not read what the koko server sent before the
// revocation keeps the Device Mux from exiting no longer than
// timing.PassOn: the command is then told, and the Device Mux exits.
func TestACommandThatDoesNotReadKeepsARevokedDeviceMuxForPassOnAtMost(t *testing.T) {
	srv := startReporter(t, answerThenRevoke(t, bytes.Repeat([]byte("answer, "), 3<<20/8)))
	home, socket, muxLog := runMux(t, srv, timing.Timings{PassOn: 100 * time.Millisecond})
	conn := dial(t, socket, home)
	if _, err := conn.OpenChannel(t.Context(), wire.ChannelAPI, nil); err != nil {
		t.Fatalf("open a channel: %v", err)
	}

	waitLog(t, muxLog, `msg="device mux stopped"`)
	if err := conn.Wait(); !errors.Is(err, serverconn.ErrRevoked) {
		t.Errorf("the command's connection ended with %v, want %v", err, serverconn.ErrRevoked)
	}
	if !strings.Contains(muxLog.String(), `reason="the koko server revoked this device's Device Session"`) {
		t.Errorf("the Device Mux stopped for another reason; it logged:\n%s", muxLog)
	}
}

// answerThenRevoke answers a channel as the koko server answers the call
// that revokes the caller's own Device Session: it writes answer, sends the
// revocation disconnect, and ends the connection. It ends it with a
// half-close, then waits for the device to close: closing a TCP connection
// with the device's window adjusts unread would reset it and drop the
// answer's tail before it left, which is not the Device Mux's to pass on.
func answerThenRevoke(t *testing.T, answer []byte) func(nc net.Conn, conn ssh.Conn, ch ssh.Channel) {
	return func(nc net.Conn, conn ssh.Conn, ch ssh.Channel) {
		if _, err := ch.Write(answer); err != nil {
			t.Errorf("the koko server's answer: %v", err)
		}
		_, _, _ = conn.SendRequest(wire.RequestRevoked, false, nil)
		_ = nc.(*net.TCPConn).CloseWrite()
		_ = conn.Wait()
	}
}
