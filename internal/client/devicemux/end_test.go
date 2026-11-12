package devicemux_test

import (
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/devicemux"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// endWithin is how long a connection may take to end once the other end
// has closed it: at once where the OS tells it, and on Windows, where a
// Unix socket's reader that is writing as the other end closes may never
// be told, once a probe has gone unanswered (timing.LocalProbe and
// timing.LocalAnswer), with time to spare on a loaded machine.
const endWithin = timing.LocalProbe + timing.LocalAnswer + 5*time.Second

// Commands that are writing to the Device Mux as it stops, as koko
// disconnect stops it, each see their connection end: on Windows the reader
// of a Unix socket that writes as the other end closes it can miss the
// close, once in some hundreds, and the connection's probe ends it then.
func TestCommandsWritingAsTheDeviceMuxStopsSeeTheirConnectionsEnd(t *testing.T) {
	const commands = 200
	srv := startReporter(t, nil)
	home, socket, _ := runMux(t, srv, timing.Timings{})
	start := make(chan struct{})
	startOnce := sync.OnceFunc(func() { close(start) })
	t.Cleanup(startOnce) // a writer started before a failure ends with its connection
	var writing sync.WaitGroup
	waits := make([]<-chan time.Time, commands)
	for i := range commands {
		conn := dial(t, socket, home)
		// The Device Mux reads what a command writes on resuming@koko, the
		// channel a command holds while a Tunnel waits to Resume, and
		// carries it nowhere.
		ch, err := conn.OpenChannel(t.Context(), "resuming@koko", nil)
		if err != nil {
			t.Fatalf("open a channel: %v", err)
		}
		writing.Add(1)
		go func() {
			<-start
			buf := make([]byte, 1<<12)
			for n := 0; ; n++ {
				if _, err := ch.Write(buf); err != nil {
					if n == 0 {
						writing.Done()
					}
					return
				}
				if n == 0 {
					writing.Done()
				}
			}
		}()
		end := make(chan time.Time, 1)
		go func() {
			_ = conn.Wait()
			end <- time.Now()
		}()
		waits[i] = end
	}
	startOnce()
	// Every command has written, and writes on, as the Device Mux stops.
	writing.Wait()
	stopped := time.Now()
	if _, err := devicemux.Stop(t.Context(), socket); err != nil {
		t.Fatalf("stop the Device Mux: %v", err)
	}
	deadline := time.After(endWithin)
	late := 0
	for i, end := range waits {
		select {
		case at := <-end:
			if at.Sub(stopped) > time.Second {
				late++
			}
		case <-deadline:
			t.Fatalf("command %d of %d did not see its connection end within %v of the Device Mux's stop", i+1, commands, endWithin)
		}
	}
	t.Logf("%d of %d connections ended only after a second, by their probe", late, commands)
}

// Commands that close their connections as the Device Mux writes to them,
// passing on what the koko server sends, each leave it, so it idles out: on
// Windows the Device Mux, reading a Unix socket as it writes, can miss a
// command's close, and its probe ends that connection then.
func TestTheDeviceMuxSeesCommandsLeaveAsItWritesToThem(t *testing.T) {
	const commands = 200
	const idle = 2 * time.Second
	srv := startReporter(t, func(_ net.Conn, _ ssh.Conn, ch ssh.Channel) {
		go func() { _, _ = io.Copy(io.Discard, ch) }()
		buf := make([]byte, 1<<12)
		for {
			if _, err := ch.Write(buf); err != nil {
				return
			}
		}
	})
	home, socket, muxLog := runMux(t, srv, timing.Timings{DeviceMuxIdle: idle})
	var conns []io.Closer
	var reading sync.WaitGroup
	for range commands {
		conn := dial(t, socket, home)
		ch, err := conn.OpenChannel(t.Context(), wire.ChannelAPI, nil)
		if err != nil {
			t.Fatalf("open a channel: %v", err)
		}
		// The Device Mux passes on what the koko server sends from the first
		// byte, which the command takes, then reads on.
		if _, err := io.ReadFull(ch, make([]byte, 1)); err != nil {
			t.Fatalf("read the first byte the koko server sent: %v", err)
		}
		reading.Go(func() { _, _ = io.Copy(io.Discard, ch) })
		conns = append(conns, conn)
	}
	for _, c := range conns {
		_ = c.Close()
	}
	reading.Wait()
	closed := time.Now()
	waitLogWithin(t, muxLog, `msg="device mux stopped" reason="nothing was connected to it for a while"`, endWithin+idle)
	t.Logf("the Device Mux idled out %v after the commands closed, its idle time %v", time.Since(closed), idle)
}
