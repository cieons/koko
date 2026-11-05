// Package cli is the koko command line: the command tree, the parser, the
// runner and the output layer, with the CLI's own --json types (ADR 0016).
//
// Every command is `koko <noun> <verb>`, its flags come after its path, and
// its path carries its risk (ADR 0019). Each area declares its commands, its
// --json types and their mappings from the API in a file of its own and adds
// one line to the central lists (commands in tree.go, mappings in mapping.go).
//
// The packages under internal/client are the client-only group: they never
// import a server package (docs/subsystems/code-layout.md).
package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"strings"

	"github.com/cieons/koko/internal/client/apiclient"
	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/wire"
)

// Runner runs koko command lines.
type Runner struct {
	// ServerRun runs the koko server in this process until ctx ends, for
	// `koko server run`. cmd/koko sets it in the builds that carry the koko
	// server (Linux), so that no client package imports a server package;
	// where it is nil, `koko server run` refuses.
	ServerRun func(ctx context.Context, s ServerSettings, log io.Writer) error

	// SocketPath is the socket-path hook, for tests only: empty means the
	// fixed wire.SocketPath. No flag or environment variable sets it.
	SocketPath string
}

// ServerSettings are the startup settings `koko server run` takes as flags or
// KOKO_* variables.
type ServerSettings struct {
	DataDir  string
	LogLevel slog.Level
}

// Invocation is one command line with its streams and environment.
type Invocation struct {
	// Args is argv without the program name.
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Env is the environment as KEY=value strings, HOME included.
	Env []string
}

// getenv returns the value of the last KEY=value in the environment.
func (inv *Invocation) getenv(key string) string {
	value := ""
	for _, kv := range inv.Env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			value = v
		}
	}
	return value
}

// Run runs one command line and returns its exit status: 0 on success, 1 when
// the operation failed, 2 when the command line was refused before any call.
// On failure stdout stays empty and the error goes to stderr, as an error
// object when --json was given.
func (r *Runner) Run(ctx context.Context, inv Invocation) int {
	p, err := parse(Tree(), inv.Args, inv.getenv)
	if err != nil {
		return writeError(inv.Stderr, err, wantsJSON(inv.Args))
	}
	in := &Input{inv: &inv, runner: r, args: p.args, flags: p.flags}
	defer in.close()
	res, err := p.cmd.Run.run(ctx, in)
	if err != nil {
		return writeError(inv.Stderr, err, p.json)
	}
	if res == nil {
		return exitOK
	}
	if err := writeResult(inv.Stdout, res, p.json); err != nil {
		return writeError(inv.Stderr, err, p.json)
	}
	return exitOK
}

// Input is what a command's Run gets: its arguments and flags, the
// invocation, and the koko server, reached on the first call.
type Input struct {
	inv    *Invocation
	runner *Runner
	args   map[string]string
	flags  map[string]string

	conn serverconn.Conn
	api  *apiclient.Client
}

// Arg returns the positional argument with this name.
func (in *Input) Arg(name string) string {
	return in.args[name]
}

// Flag returns a value flag: given, from its KOKO_* variable, or its default.
func (in *Input) Flag(name string) string {
	return in.flags[name]
}

// Bool tells whether a boolean flag was given.
func (in *Input) Bool(name string) bool {
	return in.flags[name] != ""
}

// Call calls an operation on the koko server, connecting on the first call.
func (in *Input) Call[Req, Resp any](ctx context.Context, op api.Op[Req, Resp], req Req) (Resp, error) {
	if in.api == nil {
		if err := in.connect(ctx); err != nil {
			var zero Resp
			return zero, err
		}
	}
	resp, err := in.api.Call(ctx, op, req)
	if errors.Is(err, apiclient.ErrUnreachable) {
		return resp, &cliError{code: codeServerUnreachable, message: err.Error()}
	}
	return resp, err
}

// connect reaches the koko server over its Unix socket.
func (in *Input) connect(ctx context.Context) error {
	path := cmp.Or(in.runner.SocketPath, wire.SocketPath)
	conn, err := serverconn.DialSocket(ctx, path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return &cliError{code: codeNotJoined, message: fmt.Sprintf(
			"this device has not joined a koko server, and no koko server runs on this machine (nothing at %s)", path)}
	case err != nil:
		return &cliError{code: codeServerUnreachable, message: fmt.Sprintf(
			"cannot reach the koko server on its Unix socket %s: %v", path, err)}
	}
	in.conn, in.api = conn, apiclient.New(conn)
	return nil
}

func (in *Input) close() {
	if in.api != nil {
		in.api.Close()
		_ = in.conn.Close()
	}
}
