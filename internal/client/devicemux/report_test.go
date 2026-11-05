package devicemux_test

import (
	"bytes"
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
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/devicefiles"
	"github.com/cieons/koko/internal/client/devicemux"
	"github.com/cieons/koko/internal/shared/wire"
)

// A command connected to the Device Mux is handed the report of the Device
// Connection the Device Mux holds now: after a drop, that of the next one,
// which a koko server upgraded meanwhile reports with its new version.
func TestACommandGetsTheReportOfTheNextDeviceConnection(t *testing.T) {
	srv := startReporter(t)
	home := t.TempDir()
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	staged, err := devicefiles.Stage(home, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := staged.Commit(devicefiles.Config{
		ServerAddress: srv.addr(),
		HostKeys:      []ssh.PublicKey{srv.hostKey.PublicKey()},
		User:          "alice",
		Device:        "laptop",
		DeviceSession: "device_abcdefgh",
	}); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "koko-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "mux.sock")
	muxLog := &syncBuffer{}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- devicemux.Run(ctx, devicemux.Config{Home: home, Socket: socket, Log: slog.New(slog.NewTextHandler(muxLog, nil))})
	}()
	t.Cleanup(func() {
		stop()
		if err := <-done; err != nil {
			t.Errorf("the Device Mux failed: %v", err)
		}
	})

	conn := dial(t, socket, home)
	waitReport(t, conn, "v0.0.1")
	srv.drop()
	// An open sent on the Device Connection as it drops is refused, not sent
	// again, so the channel is opened only once the Device Mux has let go of
	// that one: the open then goes on the next.
	waitLog(t, muxLog, `msg="device connection dropped"`)
	ch, err := conn.OpenChannel(t.Context(), wire.ChannelAPI, nil)
	if err != nil {
		t.Fatalf("open a channel after the drop: %v; the Device Mux logged:\n%s", err, muxLog)
	}
	_ = ch.Close()
	waitReport(t, conn, "v0.0.2")
}

// dial connects to the Device Mux at socket, for the device whose home is
// home, once it answers, at most 10 s.
func dial(t *testing.T, socket, home string) interface {
	Report(context.Context) (wire.Report, error)
	OpenChannel(context.Context, string, []byte) (io.ReadWriteCloser, error)
} {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, st, err := devicemux.Dial(t.Context(), socket, home)
		if err == nil {
			if !st.Connected {
				t.Fatalf("the Device Mux answered %+v without an error", st)
			}
			t.Cleanup(func() { _ = conn.Close() })
			return conn
		}
		if !errors.Is(err, devicemux.ErrNotRunning) || time.Now().After(deadline) {
			t.Fatalf("connect to the Device Mux: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitReport waits until the connection's report names version, at most
// 10 s.
func waitReport(t *testing.T, conn interface {
	Report(context.Context) (wire.Report, error)
}, version string,
) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		r, err := conn.Report(t.Context())
		if err != nil {
			t.Fatalf("the report: %v", err)
		}
		if r.Version == version {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the report names %s, want %s", r.Version, version)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitLog waits until the Device Mux has logged substr, at most 10 s.
func waitLog(t *testing.T, muxLog *syncBuffer, substr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(muxLog.String(), substr) {
		if time.Now().After(deadline) {
			t.Fatalf("the Device Mux did not log %q within 10 s; it logged:\n%s", substr, muxLog)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// syncBuffer holds what a Device Mux logs, for the test to read while it
// logs.
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

// reporter stands in for a koko server: it takes any device key, reports
// the version v0.0.<n> on its nth connection, as an upgraded koko server
// would, and accepts every channel only to close it.
type reporter struct {
	ln      net.Listener
	hostKey ssh.Signer

	mu    sync.Mutex
	n     int
	conns []ssh.Conn
	wg    sync.WaitGroup
}

func startReporter(t *testing.T) *reporter {
	t.Helper()
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	hostKey, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &reporter{ln: ln, hostKey: hostKey}
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) { return nil, nil }}
	cfg.AddHostKey(hostKey)
	r.wg.Go(func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			r.wg.Go(func() { r.serve(nc, cfg) })
		}
	})
	t.Cleanup(func() {
		_ = ln.Close()
		r.drop()
		r.wg.Wait()
	})
	return r
}

func (r *reporter) addr() string { return r.ln.Addr().String() }

func (r *reporter) serve(nc net.Conn, cfg *ssh.ServerConfig) {
	conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		_ = nc.Close()
		return
	}
	r.mu.Lock()
	r.n++
	report := wire.Report{Version: fmt.Sprintf("v0.0.%d", r.n), Operations: []string{"space.list"}}
	r.conns = append(r.conns, conn)
	r.mu.Unlock()
	go ssh.DiscardRequests(reqs)
	_, _, _ = conn.SendRequest(wire.RequestReport, false, report.Marshal())
	for nch := range chans {
		if ch, reqs, err := nch.Accept(); err == nil {
			go ssh.DiscardRequests(reqs)
			_ = ch.Close()
		}
	}
}

// drop closes every connection it holds, as a network drop does.
func (r *reporter) drop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.conns {
		_ = c.Close()
	}
	r.conns = nil
}
