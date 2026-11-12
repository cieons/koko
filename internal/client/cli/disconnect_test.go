package cli_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/devicemux"
	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/build"
	"github.com/cieons/koko/internal/shared/timing"
)

// disconnectOut is what koko disconnect --json prints.
type disconnectOut struct {
	DeviceMuxes []struct {
		Socket        string `json:"socket"`
		CommandsEnded int    `json:"commands_ended"`
	} `json:"device_muxes"`
	CommandsEnded int `json:"commands_ended"`
}

func kokoDisconnect(t *testing.T, d *harness.Device) (disconnectOut, string) {
	t.Helper()
	r := d.Koko("disconnect", "--json")
	r.WantSuccess(t)
	var out disconnectOut
	if err := json.Unmarshal([]byte(r.Stdout), &out, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("disconnect --json printed %q: %v", r.Stdout, err)
	}
	return out, r.Stdout
}

// koko disconnect stops the Device Mux and says how many commands it
// carried, which end: each is told it was koko disconnect, so an SSH
// Session ends for good. With none running it says so. The next command
// starts a new Device Mux.
func TestKokoDisconnectStopsTheDeviceMuxAndCountsWhatItEnded(t *testing.T) {
	t.Parallel()
	_, _, d := muxDevice(t, timing.Timings{})
	waitLogs(t, d, `msg="device connection opened"`, 1)
	held := []serverconn.Conn{d.DialMux(), d.DialMux()}

	out, raw := kokoDisconnect(t, d)
	harness.Golden(t, "disconnect.json", harness.Paths(raw, muxSocket(d), "<socket>"))
	if len(out.DeviceMuxes) != 1 || out.DeviceMuxes[0].Socket != muxSocket(d) || out.DeviceMuxes[0].CommandsEnded != 2 || out.CommandsEnded != 2 {
		t.Errorf("disconnect printed %+v, want the Device Mux with the 2 commands it carried", out)
	}
	waitLog(t, d, `msg="device mux stopped" reason="koko disconnect stopped it"`)
	for i, conn := range held {
		if err := conn.Wait(); !errors.Is(err, serverconn.ErrDisconnected) {
			t.Errorf("command %d was cut off with %v, want koko disconnect named", i+1, err)
		}
	}

	out, raw = kokoDisconnect(t, d)
	harness.Golden(t, "disconnect-none.json", raw)
	if len(out.DeviceMuxes) != 0 || out.CommandsEnded != 0 {
		t.Errorf("disconnect with no Device Mux running printed %+v", out)
	}
	if r := d.Koko("disconnect"); r.Stdout != "No Device Mux was running.\n" {
		t.Errorf("disconnect with no Device Mux running says %q", r.Stdout)
	}
	d.Koko("space", "ls", "--json").WantSuccess(t)
	if n := strings.Count(d.MuxLog(), `msg="device mux started"`); n != 2 {
		t.Errorf("%d Device Muxes started, want koko join's and the next command's", n)
	}
}

// koko disconnect stops every Device Mux of this OS user, whatever its
// build, each with its own count, and passes over a socket a crashed one
// left.
func TestKokoDisconnectStopsTheDeviceMuxOfEveryBuild(t *testing.T) {
	t.Parallel()
	_, _, d := muxDevice(t, timing.Timings{})
	waitLogs(t, d, `msg="device connection opened"`, 1)
	d.DialMux()
	dir := filepath.Dir(muxSocket(d))

	// Another build's Device Mux runs beside this build's, one command
	// connected to it.
	other := filepath.Join(dir, "v0.0.1-0123456789ab.sock")
	if other == muxSocket(d) {
		t.Fatal("this build's Device Mux has the other build's name")
	}
	ctx, cancel := context.WithCancel(context.Background())
	var ran error
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ran = devicemux.Run(ctx, devicemux.Config{Home: d.Home(), Socket: other, Log: slog.New(slog.DiscardHandler)})
	}()
	t.Cleanup(func() {
		cancel()
		<-stopped
	})
	var conn serverconn.Conn
	eventually(t, "the other build's Device Mux answers", func() bool {
		c, _, err := devicemux.Dial(t.Context(), other, d.Home(), timing.Timings{})
		conn = c
		return err == nil
	})
	t.Cleanup(func() { _ = conn.Close() })

	// A socket a crashed Device Mux left, where nothing answers.
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(dir, "v0.0.0-crashed.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	_ = ln.Close()

	out, _ := kokoDisconnect(t, d)
	got := map[string]int{}
	for _, m := range out.DeviceMuxes {
		got[filepath.Base(m.Socket)] = m.CommandsEnded
	}
	this := devicemux.Name(build.Read()) + ".sock"
	if len(got) != 2 || got[this] != 1 || got[filepath.Base(other)] != 1 || out.CommandsEnded != 2 {
		t.Errorf("disconnect stopped %v, %d commands in all; want %s and %s, each with its one command", got, out.CommandsEnded, this, filepath.Base(other))
	}
	select {
	case <-stopped:
		if ran != nil {
			t.Errorf("the other build's Device Mux ended with %v", ran)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the other build's Device Mux did not stop within 10 s")
	}
	if err := conn.Wait(); !errors.Is(err, serverconn.ErrDisconnected) {
		t.Errorf("the other build's command was cut off with %v, want koko disconnect named", err)
	}
	waitLog(t, d, `msg="device mux stopped" reason="koko disconnect stopped it"`)
}

// A socket that answers, but not as a Device Mux stopping, as one built
// before koko disconnect would, does not keep koko disconnect from
// stopping the others: it fails once it has asked each, naming that socket
// and saying what it stopped.
func TestKokoDisconnectStopsTheOthersWhenOneDoesNotStop(t *testing.T) {
	t.Parallel()
	_, _, d := muxDevice(t, timing.Timings{})
	waitLogs(t, d, `msg="device connection opened"`, 1)
	d.DialMux()
	stubborn := filepath.Join(filepath.Dir(muxSocket(d)), "a0.0.0-older.sock")
	answerElsewhere(t, stubborn)

	msg := d.Koko("disconnect", "--json").WantError(t, 1, "internal_error")
	for _, want := range []string{stubborn + " did not stop", "it stopped the Device Mux at " + muxSocket(d) + ", ending 1 command(s)"} {
		if !strings.Contains(msg, want) {
			t.Errorf("disconnect says %q, without %q", msg, want)
		}
	}
	waitLog(t, d, `msg="device mux stopped" reason="koko disconnect stopped it"`)
}

// answerElsewhere serves SSH on the Unix socket path as a Device Mux of
// another home does, whatever the user name, until the test ends.
func answerElsewhere(t *testing.T, path string) {
	t.Helper()
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		NoClientAuth:         true,
		BannerCallback:       func(ssh.ConnMetadata) string { return `{"state":"elsewhere","home":"/elsewhere"}` },
		NoClientAuthCallback: func(ssh.ConnMetadata) (*ssh.Permissions, error) { return nil, errors.New("elsewhere") },
	}
	cfg.AddHostKey(signer)
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})
	go func() {
		defer close(done)
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			_, _, _, _ = ssh.NewServerConn(nc, cfg)
			_ = nc.Close()
		}
	}()
}

// A Device Mux whose answer never reaches koko disconnect, as a Unix
// socket on Windows once kept it from koko disconnect for good (#184),
// holds koko disconnect for at most timing.CommandWait. One that still
// listens then did not answer, and fails koko disconnect as one that does
// not stop does; one that no longer listens stopped, but how many commands
// it ended is not known, so koko disconnect fails saying so.
func TestKokoDisconnectWaitsForAnAnswerAtMostCommandWait(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		stops bool
		want  string
	}{
		{"listening", false, "koko disconnect could not stop every Device Mux: the Device Mux at %s did not answer within 200ms"},
		{"stopped", true, "koko disconnect cannot say how many commands it ended: the Device Mux at %s stopped, but its answer, with how many commands it ended, never came within 200ms"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Nothing a real peer answers waits on the short wait: this
			// device runs no Device Mux of its own.
			d := harness.NewMuxDevice(t, timing.Timings{CommandWait: 200 * time.Millisecond})
			d.Koko("disconnect").WantSuccess(t) // makes the runtime directory
			silent := filepath.Join(d.MuxDir(), "v0.0.1-silent.sock")
			holdAnswer(t, silent, "", tc.stops)

			msg := d.Koko("disconnect", "--json").WantError(t, 1, "internal_error")
			if want := fmt.Sprintf(tc.want, silent); !strings.Contains(msg, want) {
				t.Errorf("disconnect says %q, without %q", msg, want)
			}
		})
	}
}

// The banner is koko disconnect's answer: a Device Mux whose refusal after
// it never comes is counted all the same, once koko disconnect stops
// waiting.
func TestKokoDisconnectCountsABannerWhoseRefusalNeverCame(t *testing.T) {
	t.Parallel()
	d := harness.NewMuxDevice(t, timing.Timings{CommandWait: 200 * time.Millisecond})
	d.Koko("disconnect").WantSuccess(t) // makes the runtime directory
	silent := filepath.Join(d.MuxDir(), "v0.0.1-silent.sock")
	holdAnswer(t, silent, `{"state":"stopped","commands":3}`, true)

	out, _ := kokoDisconnect(t, d)
	if len(out.DeviceMuxes) != 1 || out.DeviceMuxes[0].Socket != silent || out.DeviceMuxes[0].CommandsEnded != 3 || out.CommandsEnded != 3 {
		t.Errorf("disconnect printed %+v, want the Device Mux at %s with its 3 commands", out, silent)
	}
}

// koko leave needs every Device Mux stopped, not their counts, so one that
// stopped without its answer reaching koko leave does not keep the device
// joined.
func TestKokoLeavePassesOverADeviceMuxThatStoppedWithoutAnswering(t *testing.T) {
	t.Parallel()
	_, _, d := muxDevice(t, timing.Timings{})
	d.Koko("disconnect").WantSuccess(t)
	// The Device Mux the device ran has stopped: the short wait is for
	// the one whose answer never comes alone.
	d.UseTimings(timing.Timings{CommandWait: 200 * time.Millisecond})
	holdAnswer(t, filepath.Join(d.MuxDir(), "v0.0.1-silent.sock"), "", true)

	d.Koko("leave", "--json").WantSuccess(t)
	if _, err := os.Lstat(filepath.Join(kokoDir(d), "client.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("leave left koko's client config (%v)", err)
	}
}

// holdAnswer serves SSH on the Unix socket path, as a Device Mux does,
// until the test ends, and answers the authentication koko disconnect
// opens with banner alone, or with nothing when banner is "", but never
// with the refusal after it, as when the rest of the answer never reaches
// koko disconnect. With stops it closes its listener as it is asked, as a
// Device Mux that stops does.
func holdAnswer(t *testing.T, path, banner string, stops bool) {
	t.Helper()
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		NoClientAuth: true,
		BannerCallback: func(ssh.ConnMetadata) string {
			if stops {
				_ = ln.Close()
			}
			if banner == "" {
				<-t.Context().Done()
			}
			return banner
		},
		NoClientAuthCallback: func(ssh.ConnMetadata) (*ssh.Permissions, error) {
			<-t.Context().Done()
			return nil, errors.New("koko disconnect is never let in")
		},
	}
	cfg.AddHostKey(signer)
	var wg sync.WaitGroup
	t.Cleanup(func() {
		_ = ln.Close()
		wg.Wait()
	})
	wg.Go(func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Go(func() {
				stop := context.AfterFunc(t.Context(), func() { _ = nc.Close() })
				defer stop()
				_, _, _, _ = ssh.NewServerConn(nc, cfg)
				_ = nc.Close()
			})
		}
	})
}
