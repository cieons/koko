package harness

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/cieons/koko/internal/server"
	"github.com/cieons/koko/internal/server/kokod"
)

// Kokod returns a kokod.Program for S1 tests of kokod run: the koko
// server's real start-up, given the config
// kokod run makes of its command line, on a temporary socket, on LocalDisk
// and as NotRoot unless SocketPath, Mountinfo or ProcessUIDs say otherwise,
// and, whatever --listen says, on a loopback TCP port of the system's
// choosing, so a test binds no port below 1024. A koko server that starts
// stops at once, so kokod run exits 0, or exits naming why the start-up
// refused, and never serves.
func Kokod(t testing.TB, opts ...Option) kokod.Program {
	t.Helper()
	complete := startConfig(t, opts)
	return kokod.Program{Serve: func(ctx context.Context, c server.Config) error {
		srv, err := server.Start(ctx, complete(c))
		if err != nil {
			return err
		}
		return srv.Close()
	}}
}

// KokodServing is Kokod, with the same options, for a koko server that
// keeps serving, as cmd/kokod's does, until the command is interrupted,
// such as by Process.Stop: a test then reaches it on the socket SocketPath
// names and, once initialised, over TCP. Start it with RunKokodBackground.
func KokodServing(t testing.TB, opts ...Option) kokod.Program {
	t.Helper()
	complete := startConfig(t, opts)
	return kokod.Program{Serve: func(ctx context.Context, c server.Config) error {
		return server.Run(ctx, complete(c))
	}}
}

// RunKokod runs one kokod command line in process with the program p and
// the environment env, and returns what it printed and its exit status.
func RunKokod(t testing.TB, p kokod.Program, env []string, args ...string) Result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	status := p.Run(t.Context(), kokod.Invocation{Args: args, Stdout: &stdout, Stderr: &stderr, Env: env})
	return Result{Stdout: stdout.String(), Stderr: stderr.String(), Status: status}
}

// RunKokodBackground starts one kokod command line in process, as RunKokod
// does, and returns at once, for kokod run with KokodServing. It is stopped
// when the test ends.
func RunKokodBackground(t testing.TB, p kokod.Program, env []string, args ...string) *Process {
	t.Helper()
	return backgroundRun(t, func(ctx context.Context, stdout, stderr io.Writer) int {
		return p.Run(ctx, kokod.Invocation{Args: args, Stdout: stdout, Stderr: stderr, Env: env})
	})
}
