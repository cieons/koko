// Package harness is the S1 test harness: a real koko server in the test
// process, and koko command lines run in process against it, as the Local
// caller under a temporary HOME. Everything it starts stops through
// t.Cleanup.
//
// It is the one package besides cmd/koko that imports both the server and the
// client groups, and only tests import it. It grows by addition: add helpers
// and options, and never change an existing helper's signature.
package harness

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/server"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/shared/api"
)

// Harness is one in-process koko server and the CLI that reaches it.
type Harness struct {
	t      testing.TB
	socket string
	home   string
	log    *syncBuffer
}

// Option changes how Start sets up the harness. There are none yet; later
// tickets add them here.
type Option func(*options)

type options struct{}

// Start starts a koko server in the test process on a temporary data
// directory, a Root Key file made for this test outside it, and a temporary
// Unix socket, logging at debug level. It stops when the test ends.
func Start(t testing.TB, opts ...Option) *Harness {
	t.Helper()
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	h := &Harness{
		t:      t,
		socket: filepath.Join(ShortTempDir(t), "koko.sock"),
		home:   t.TempDir(),
		log:    &syncBuffer{},
	}
	srv, err := server.Start(t.Context(), server.Config{
		DataDir:     t.TempDir(),
		RootKeyPath: NewRootKeyFile(t),
		LogLevel:    slog.LevelDebug,
		Log:         h.log,
		SocketPath:  h.socket,
	})
	if err != nil {
		t.Fatalf("start the koko server: %v", err)
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Errorf("stop the koko server: %v", err)
		}
	})
	return h
}

// NewRootKeyFile makes a new Root Key in a file of its own in a temporary
// directory, readable by its owner only, and returns the file's path. A test
// makes every Root Key it uses; none is ever committed.
func NewRootKeyFile(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "root.key")
	if err := os.WriteFile(path, seal.NewRootKey(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// ServerStart returns a cli.Runner.ServerRun for S1 tests of `koko server
// run`: the koko server's real start-up, given its settings as cmd/koko gives
// them, on a temporary socket. A koko server that starts stops at once, so
// the command exits 0, or 1 naming why the start-up refused, and never serves.
func ServerStart(t testing.TB) func(ctx context.Context, s cli.ServerSettings, log io.Writer) error {
	t.Helper()
	socket := filepath.Join(ShortTempDir(t), "koko.sock")
	return func(ctx context.Context, s cli.ServerSettings, log io.Writer) error {
		srv, err := server.Start(ctx, server.Config{
			DataDir:     s.DataDir,
			RootKeyPath: s.RootKeyPath,
			LogLevel:    s.LogLevel,
			Log:         log,
			SocketPath:  socket,
		})
		if err != nil {
			return err
		}
		return srv.Close()
	}
}

// ShortTempDir makes a temporary directory whose path is short enough for a
// Unix socket inside it (macOS allows 104 bytes), removed when the test ends.
func ShortTempDir(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "koko-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// Koko runs one koko command line in process, as the Local caller under the
// harness's temporary HOME, with empty stdin.
func (h *Harness) Koko(args ...string) Result {
	h.t.Helper()
	return Run(h.t, cli.Runner{SocketPath: h.socket}, []string{"HOME=" + h.home}, args...)
}

// Log returns what the koko server has logged so far.
func (h *Harness) Log() string {
	return h.log.String()
}

// Calls counts the API calls the koko server has answered so far.
func (h *Harness) Calls() int {
	return strings.Count(h.Log(), `msg="api call"`)
}

// Run runs one koko command line in process with the runner r and the
// environment env, and returns what it printed and its exit status.
func Run(t testing.TB, r cli.Runner, env []string, args ...string) Result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	status := r.Run(t.Context(), cli.Invocation{
		Args:   args,
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    env,
	})
	return Result{Stdout: stdout.String(), Stderr: stderr.String(), Status: status}
}

// Result is what one koko command line printed, and its exit status.
type Result struct {
	Stdout string
	Stderr string
	Status int
}

// WantSuccess fails the test unless the command exited 0 with nothing on
// stderr.
func (r Result) WantSuccess(t testing.TB) {
	t.Helper()
	if r.Status != 0 || r.Stderr != "" {
		t.Fatalf("exit status %d, stderr %q, want success", r.Status, r.Stderr)
	}
}

// WantError fails the test unless a command run with --json exited with
// status, left stdout empty, and wrote the error object with code to stderr.
// It returns the error's message.
func (r Result) WantError(t testing.TB, status int, code string) string {
	t.Helper()
	if r.Status != status {
		t.Errorf("exit status %d, want %d (stderr %q)", r.Status, status, r.Stderr)
	}
	if r.Stdout != "" {
		t.Errorf("stdout = %q, want it empty on failure", r.Stdout)
	}
	var e api.ErrorResponse
	if err := json.Unmarshal([]byte(r.Stderr), &e, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("stderr %q is not one error object: %v", r.Stderr, err)
	}
	if e.Error.Code != code || e.Error.Message == "" {
		t.Errorf("error = %+v, want code %q with a message", e.Error, code)
	}
	return e.Error.Message
}

// syncBuffer is a log the koko server writes while the test reads it.
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
