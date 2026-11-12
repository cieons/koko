// Package muxprocess_test runs koko commands as real processes, which start
// a real Device Mux process with devicemux.Spawn, as cmd/koko does: on
// Windows detached from the console and the job, on Linux and macOS in a
// session of its own. The test binary itself is the koko they run
// (TestMain), so the package builds no koko of its own. It is a package of
// its own so that its processes count against no other package's time
// limit.
package muxprocess_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/client/devicemux"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/timing"
)

// asKoko is the variable that makes the test binary run as koko, with its
// arguments, rather than run tests. A Device Mux a command spawns inherits
// it, and so runs as koko too.
const asKoko = "KOKO_TEST_AS_KOKO"

// socketVar names the path where the koko commands look for a koko
// server's Unix socket, one where none is, so that they never reach a koko
// server running on the machine.
const socketVar = "KOKO_TEST_SOCKET_PATH"

func TestMain(m *testing.M) {
	if os.Getenv(asKoko) == "1" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		r := cli.Runner{
			StartDeviceMux: devicemux.Spawn,
			SocketPath:     os.Getenv(socketVar),
			Executable:     os.Executable,
		}
		status := r.Run(ctx, cli.Invocation{Args: os.Args[1:], Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Env: os.Environ()})
		stop()
		os.Exit(status)
	}
	os.Exit(m.Run())
}

// A Device Mux that koko join starts runs on once koko join has exited,
// and carries the next command; koko disconnect stops it.
func TestADeviceMuxOutlivesTheCommandThatStartedIt(t *testing.T) {
	h := harness.Start(t)
	invite := h.Koko("user", "invite", "bob", "--json")
	invite.WantSuccess(t)
	var invited struct {
		JoinCode struct {
			Code string `json:"code"`
		} `json:"join_code"`
	}
	if err := json.Unmarshal([]byte(invite.Stdout), &invited); err != nil {
		t.Fatalf("user invite printed %q: %v", invite.Stdout, err)
	}

	home := t.TempDir()
	runtimeDir := removedAtTheEnd(t)
	env := append(os.Environ(),
		asKoko+"=1",
		socketVar+"="+filepath.Join(harness.ShortTempDir(t), "koko.sock"),
		harness.HomeEnv(home),
		"TMPDIR="+runtimeDir, "XDG_RUNTIME_DIR="+runtimeDir, "LOCALAPPDATA="+runtimeDir,
	)
	run := func(args ...string) (string, error) {
		// Not the test's context, which has ended once koko disconnect runs.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], args...)
		cmd.Env = env
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("koko %q: %w; stdout %q, stderr %q", args, err, stdout.String(), stderr.String())
		}
		return stdout.String(), nil
	}
	koko := func(args ...string) string {
		t.Helper()
		out, err := run(args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	// The socket the Device Mux listens on, once it answers.
	var socket string
	t.Cleanup(func() {
		if _, err := run("disconnect"); err != nil {
			t.Errorf("stop the Device Mux: %v", err)
			if socket != "" {
				kill(t, strings.TrimSuffix(socket, ".sock")+".log")
			}
		}
	})
	koko("join", invited.JoinCode.Code)

	// koko join has exited, and does not wait for the Device Mux it
	// started to listen.
	getenv := func(key string) string {
		for i := len(env) - 1; i >= 0; i-- {
			if k, v, ok := strings.Cut(env[i], "="); ok && k == key {
				return v
			}
		}
		return ""
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		sockets, err := devicemux.Sockets(getenv)
		if err != nil {
			t.Fatalf("list the Device Mux sockets: %v", err)
		}
		if len(sockets) == 1 {
			conn, st, err := devicemux.Dial(t.Context(), sockets[0], home, timing.Timings{})
			if err == nil {
				socket = sockets[0]
				_ = conn.Close()
				if !st.Connected {
					t.Errorf("the Device Mux answered %+v, want it connected", st)
				}
				break
			}
			if !errors.Is(err, devicemux.ErrNotRunning) {
				t.Fatalf("connect to the Device Mux: %v", err)
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no Device Mux answered within 10 s of koko join's exit; the runtime directory holds %v", sockets)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var status struct {
		DeviceMux *struct {
			State string `json:"state"`
		} `json:"device_mux"`
	}
	if out := koko("status", "--json"); json.Unmarshal([]byte(out), &status) != nil || status.DeviceMux == nil || status.DeviceMux.State != "connected" {
		t.Errorf("koko status printed %s, want it through the Device Mux, connected", out)
	}
	// Its directory, lock and log are private to this user.
	harness.WantPrivate(t, filepath.Dir(socket))
	harness.WantPrivate(t, strings.TrimSuffix(socket, ".sock")+".lock")
	harness.WantPrivate(t, strings.TrimSuffix(socket, ".sock")+".log")
}

// kill ends each Device Mux process the log at path says started, for a
// test whose koko disconnect failed to stop it, so that none outlives the
// test.
func kill(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("read the Device Mux log, to end the process: %v", err)
		return
	}
	for _, m := range regexp.MustCompile(`msg="device mux started" .*pid=(\d+)`).FindAllSubmatch(b, -1) {
		pid, _ := strconv.Atoi(string(m[1]))
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
	}
}

// removedAtTheEnd makes a temporary directory, short enough for the Device
// Mux's socket, removed once the test's other cleanups have run: once the
// Device Mux that koko disconnect stopped has exited, which on Windows may
// take a moment after it answered, and until then holds its log and lock
// open, which Windows does not let a directory be removed with.
func removedAtTheEnd(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "koko-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		deadline := time.Now().Add(10 * time.Second)
		for {
			err := os.RemoveAll(dir)
			if err == nil {
				return
			}
			if time.Now().After(deadline) {
				t.Errorf("remove the runtime directory once the Device Mux stopped: %v", err)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
	return dir
}
