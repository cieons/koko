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
	"github.com/cieons/koko/internal/client/devicefiles"
	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/install"
	"github.com/cieons/koko/internal/shared/wire"
)

// Runner runs koko command lines.
type Runner struct {
	// ServerRun runs the koko server in this process until ctx ends, for
	// `koko server run`. cmd/koko sets it in the builds that carry the koko
	// server (Linux), so that no client package imports a server package;
	// where it is nil, `koko server run` refuses.
	ServerRun func(ctx context.Context, s ServerSettings, log io.Writer) error

	// ServerInstall installs, starts and initialises the koko server on this
	// machine, for `koko server install`, calling initialise once the koko
	// server's Unix socket is there. cmd/koko sets it in the builds that
	// carry the koko server (Linux); where it is nil, `koko server install`
	// refuses.
	ServerInstall func(ctx context.Context, req install.Request, initialise install.Initialise) (install.Report, error)

	// SocketPath is the socket-path hook, for tests only: empty means the
	// fixed wire.SocketPath. No flag or environment variable sets it.
	SocketPath string

	// Terminal is the fake-terminal hook, for tests only: when set, stdin is
	// this terminal, so a secret input not given prompts on it. Nil means
	// stdin is a terminal when it is one. No flag or environment variable
	// sets it.
	Terminal Terminal
}

// ServerSettings are the startup settings `koko server run` takes as flags or
// KOKO_* variables.
type ServerSettings struct {
	DataDir string
	// RootKeyPath is the Root Key's file, never the key itself.
	RootKeyPath string
	// Listen is the TCP listen address, such as :443.
	Listen   string
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
	in := &Input{inv: &inv, runner: r, line: p}
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

// Input is what a command's Run gets: its command line as parsed, the
// invocation, and the koko server, reached on the first call.
type Input struct {
	inv    *Invocation
	runner *Runner
	line   *parsed

	conn serverconn.Conn
	api  *apiclient.Client
}

// Arg returns the positional argument with this name, as typed.
func (in *Input) Arg(name string) string {
	return in.line.args[name]
}

// Qualified returns the two parts of the qualified argument arg: "ops" and
// "db-password" for ops/db-password. The qualifier is "" for one written
// bare.
func (in *Input) Qualified(arg string) (qualifier, name string) {
	q := in.line.qualified[arg]
	return q.qualifier, q.name
}

// Flag returns a value flag: given, from its KOKO_* variable, or its default.
func (in *Input) Flag(name string) string {
	return in.line.flags[name]
}

// Bool tells whether a boolean flag was given.
func (in *Input) Bool(name string) bool {
	return in.line.flags[name] != ""
}

// Pairs returns the key=value words, in the order given.
func (in *Input) Pairs() []Pair {
	return in.line.pairs
}

// Unset returns the keys given with --unset, in the order given.
func (in *Input) Unset() []string {
	return in.line.unset
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

// server returns the connection to the koko server, connecting on first
// use, for a command that opens channels of its own, such as koko web's
// web@koko.
func (in *Input) server(ctx context.Context) (serverconn.Conn, error) {
	if in.conn == nil {
		if err := in.connect(ctx); err != nil {
			return nil, err
		}
	}
	return in.conn, nil
}

// connect reaches the koko server: over its Unix socket when the socket is
// at its fixed path, and otherwise the koko server this device joined, over
// a direct Device Connection.
func (in *Input) connect(ctx context.Context) error {
	path := cmp.Or(in.runner.SocketPath, wire.SocketPath)
	conn, err := serverconn.DialSocket(ctx, path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if conn, err = in.dialJoined(ctx, path); err != nil {
			return err
		}
	case err != nil:
		return &cliError{code: codeServerUnreachable, message: fmt.Sprintf(
			"cannot reach the koko server on its Unix socket %s: %v", path, err)}
	}
	in.conn, in.api = conn, apiclient.New(conn)
	return nil
}

// dialJoined opens a direct Device Connection to the koko server this
// device joined, on a machine whose Unix socket path holds nothing.
func (in *Input) dialJoined(ctx context.Context, socket string) (serverconn.Conn, error) {
	c, key, err := devicefiles.Read(in.inv.getenv("HOME"))
	switch {
	case errors.Is(err, devicefiles.ErrNotJoined):
		return nil, &cliError{code: codeNotJoined, message: fmt.Sprintf(
			"this device has not joined a koko server, and no koko server runs on this machine (nothing at %s): join one with koko join <join-code>", socket)}
	case err != nil:
		return nil, err
	}
	conn, err := serverconn.DialDevice(ctx, serverconn.Device{Address: c.ServerAddress, HostKeys: c.HostKeys, Key: key})
	if err != nil {
		return nil, deviceConnError(c.ServerAddress, err)
	}
	return conn, nil
}

// deviceConnError gives a Device Connection that failed to the koko server
// at address the CLI's code for it.
func deviceConnError(address string, err error) error {
	if e, ok := errors.AsType[*serverconn.HostKeyMismatchError](err); ok {
		return &cliError{code: codeHostKeyMismatch, message: e.Error() +
			"; koko sent nothing past the SSH key exchange, since something else may be answering at that address"}
	}
	if errors.Is(err, serverconn.ErrUnknownKey) {
		return &cliError{code: codeNotJoined, message: fmt.Sprintf(
			"the koko server at %s holds no Device Session with this device's key, so this device is not joined to it", address)}
	}
	if e, ok := errors.AsType[*serverconn.RefusedError](err); ok {
		return &cliError{code: codeNotJoined, message: fmt.Sprintf("the koko server at %s refused this device: %v", address, e)}
	}
	return &cliError{code: codeServerUnreachable, message: fmt.Sprintf(
		"cannot reach the koko server at %s: %v; if it has moved, take its new address with koko connect <host:port>", address, err)}
}

func (in *Input) close() {
	if in.api != nil {
		in.api.Close()
		_ = in.conn.Close()
	}
}
