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
	"os"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/apiclient"
	"github.com/cieons/koko/internal/client/devicefiles"
	"github.com/cieons/koko/internal/client/devicemux"
	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/install"
	"github.com/cieons/koko/internal/shared/serverrun"
	"github.com/cieons/koko/internal/shared/timing"
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

	// ServerUninstall stops the koko service on this machine and removes its
	// unit and binary, for `koko server uninstall`. cmd/koko sets it in the
	// builds that carry the koko server (Linux); where it is nil, `koko
	// server uninstall` refuses.
	ServerUninstall func(ctx context.Context) (install.UninstallReport, error)

	// StartDeviceMux starts this build's Device Mux in the background, with
	// the environment env, for a command on a joined device that finds none
	// answering at socket, and for koko join; it does not wait for it to
	// listen, and the Device Mux outlives ctx, the command's. The channel it
	// returns closes if the Device Mux exits. cmd/koko sets it to start this
	// binary, detached (devicemux.Spawn); the S1 harness runs one in the test
	// process. Where it is nil, commands open their own Device Connection.
	StartDeviceMux func(ctx context.Context, socket string, env []string) (exited <-chan struct{}, err error)

	// Signals relays to c the signals sigs the process receives until stop
	// is called, for a command that handles a signal itself, as koko tunnel
	// does SIGHUP. cmd/koko sets it to signal.Notify; the S1 harness relays
	// the signals a test sends. Where it is nil, no command gets one.
	Signals func(c chan<- os.Signal, sigs ...os.Signal) (stop func())

	// SocketPath is the socket-path hook, for tests only: empty means the
	// fixed wire.SocketPath. No flag or environment variable sets it;
	// cmd/koko sets it from a value only the real-OpenSSH test's build
	// links in, empty in every other build.
	SocketPath string

	// Timings is the timing-constants hook, for tests only: the zero
	// Timings is the product's. No flag or environment variable sets it.
	Timings timing.Timings

	// Terminal is the fake-terminal hook, for tests only: when set, stdin is
	// this terminal, so a secret input not given prompts on it. Nil means
	// stdin is a terminal when it is one. No flag or environment variable
	// sets it.
	Terminal Terminal

	// Clock is the clock hook, for tests only: nil means the system clock.
	// The CLI reads it for today's date, which a relative date such as +30d
	// counts from. No flag or environment variable sets it.
	Clock func() time.Time
}

// ServerSettings are the startup settings `koko server run` takes as flags or
// KOKO_* variables, which kokod run takes too (package serverrun).
type ServerSettings = serverrun.Settings

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
	value, _ := inv.lookupEnv(key)
	return value
}

// lookupEnv returns the value of the last KEY=value in the environment, and
// whether there is one, empty or not.
func (inv *Invocation) lookupEnv(key string) (value string, ok bool) {
	for _, kv := range inv.Env {
		if k, v, found := strings.Cut(kv, "="); found && k == key {
			value, ok = v, true
		}
	}
	return value, ok
}

// Run runs one command line and returns its exit status: 0 on success, 1 when
// the operation failed, 2 when the command line was refused before any call.
// On failure stdout stays empty and the error goes to stderr, as an error
// object when --json was given.
func (r *Runner) Run(ctx context.Context, inv Invocation) int {
	if slices.Equal(inv.Args, []string{devicemux.Arg}) {
		return r.runDeviceMux(ctx, &inv)
	}
	tree := Tree()
	p, err := parse(tree, inv.Args, inv.getenv)
	if err != nil {
		return writeError(inv.Stderr, err, wantsJSON(inv.Args))
	}
	log, err := logger(&inv)
	if err != nil {
		return writeError(inv.Stderr, err, p.json)
	}
	in := &Input{inv: &inv, runner: r, line: p, log: log, tree: tree}
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
	// log is the command's own log on stderr, at the level KOKO_LOG_LEVEL
	// names (info by default), for what only someone debugging wants.
	log *slog.Logger
	// tree is the command tree the command line was parsed against, for the
	// commands generated from it, such as koko skill.
	tree []Command

	// api calls the koko server, dialling it on the first call; nil until
	// then.
	api *apiclient.Client
	// joined is the client config of this device, when the connection is
	// its Device Connection.
	joined *devicefiles.Config
	// socket is the path of the Unix socket the connection went over, when
	// it did.
	socket string
	// deviceMux is the Device Mux the latest connection reached, and the
	// state it answered in; nil when none answered.
	deviceMux *deviceMuxSeen
}

// unconnected returns an Input of the same command line that has reached
// nothing yet: the fields above api, without those that note what a
// connection reached, for a connection made beside the command's own.
func (in *Input) unconnected() *Input {
	return &Input{inv: in.inv, runner: in.runner, line: in.line, log: in.log, tree: in.tree}
}

// Arg returns the positional argument with this name, as typed.
func (in *Input) Arg(name string) string {
	return in.line.args[name]
}

// Args returns the words of the argument with this name that takes many
// (Arg.Many), as typed and in order.
func (in *Input) Args(name string) []string {
	if a := in.line.cmd.many(); a == nil || a.Name != name {
		return nil
	}
	return in.line.many
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

// Terms returns the filter terms of a command with Filters, as typed and in
// order.
func (in *Input) Terms() []string {
	return in.line.terms
}

// Sorts returns the values given with --sort, in order.
func (in *Input) Sorts() []string {
	return in.line.sorts
}

// Call calls an operation on the koko server, connecting on the first call.
// A call the connection drops is sent once more on a new connection
// (package apiclient).
func (in *Input) Call[Req, Resp any](ctx context.Context, op api.Op[Req, Resp], req Req) (Resp, error) {
	resp, err := in.client().Call(ctx, op, req)
	if errors.Is(err, apiclient.ErrUnreachable) {
		return resp, &cliError{code: codeServerUnreachable, message: err.Error()}
	}
	if e, ok := errors.AsType[*apiclient.MissingOperationError](err); ok {
		return resp, &cliError{code: codeServerTooOld, message: fmt.Sprintf(
			"%v, so koko %s needs a newer koko server: upgrade the koko server", e, in.line.cmd.Path())}
	}
	return resp, err
}

// server returns the connection the command's calls go through, connecting
// on first use, for a command that opens channels of its own, such as koko
// web's web@koko.
func (in *Input) server(ctx context.Context) (serverconn.Conn, error) {
	return in.client().Conn(ctx)
}

func (in *Input) client() *apiclient.Client {
	if in.api == nil {
		in.api = apiclient.New(in.connect)
	}
	return in.api
}

// connect reaches the koko server: over its Unix socket when the socket is
// at its fixed path, and otherwise the koko server this device joined. It
// returns once the koko server's report has come, and waits for the
// connection and the report at most timing.CommandWait, so a command is
// never stuck (P0 spec: reach).
func (in *Input) connect(ctx context.Context) (serverconn.Conn, error) {
	path := cmp.Or(in.runner.SocketPath, wire.SocketPath)
	socketCtx, cancel := context.WithTimeout(ctx, in.runner.Timings.Resolve().CommandWait)
	defer cancel()
	conn, err := serverconn.DialSocket(socketCtx, path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return in.dialJoined(ctx, path)
	case err == nil:
		if _, err = conn.Report(socketCtx); err != nil {
			_ = conn.Close()
		}
	}
	if err != nil {
		return nil, &cliError{code: codeServerUnreachable, message: fmt.Sprintf(
			"cannot reach the koko server on its Unix socket %s: %v", path, err)}
	}
	in.socket = path
	return conn, nil
}

// dialJoined reaches the koko server this device joined, on a machine whose
// Unix socket path holds nothing: through this build's Device Mux, or over a
// direct Device Connection of its own when no Device Mux can carry the
// command (viaDeviceMux). When the koko server refuses the device's key as
// a Revoked Device Session's, the revocation cleanup runs; a device that
// holds the revocation record instead of a join is told why from it. socket
// is the Unix socket's path that held nothing, which a device that has not
// joined is told of, or "" when the socket was not looked for.
//
// It waits at most timing.CommandWait for the Device Connection: a Device
// Mux answers within it, and the direct path, should the command take it
// after trying the Device Mux, has what is left of it. Starting a Device
// Mux, or one that does not answer, may add timing.DeviceMuxStart.
func (in *Input) dialJoined(ctx context.Context, socket string) (serverconn.Conn, error) {
	home := in.inv.getenv("HOME")
	c, key, err := devicefiles.Read(home)
	switch {
	case errors.Is(err, devicefiles.ErrNotJoined):
		return nil, notJoined(home, socket)
	case err != nil:
		return nil, err
	}
	t := in.runner.Timings.Resolve()
	start := time.Now()
	ctx, cancel := context.WithDeadline(ctx, start.Add(t.CommandWait+t.DeviceMuxStart))
	defer cancel()
	conn, err := in.viaDeviceMux(ctx)
	if errors.Is(err, errDirect) {
		direct, cancel := context.WithDeadline(ctx, start.Add(t.CommandWait))
		defer cancel()
		conn, err = in.dialDirect(direct, c.ServerAddress, c, key)
	}
	if e, ok := errors.AsType[*serverconn.RevokedError](err); ok {
		return nil, revoked(home, socket, c, e)
	}
	if errors.Is(err, devicefiles.ErrNotJoined) {
		// The Device Mux found the join gone since it was read above.
		return nil, notJoined(home, socket)
	}
	if err != nil {
		return nil, deviceConnError(c.ServerAddress, err)
	}
	in.joined = &c
	return conn, nil
}

// dialDirect opens a direct Device Connection to the koko server at
// address, with the join c and the device's key, and waits for its report:
// the direct path, and koko connect's try of a new address. While it cannot
// reach the koko server it tries again, as the Device Mux does, after
// timing.ReconnectFirst and then twice the last wait, up to
// timing.ReconnectMax, for at most timing.CommandWait, each try lasting at
// most timing.DeviceConnectAttempt; a failure trying again does not change
// ends it at once. The connection probes the koko server as the Device
// Mux's does.
func (in *Input) dialDirect(ctx context.Context, address string, c devicefiles.Config, key ssh.Signer) (serverconn.Conn, error) {
	t := in.runner.Timings.Resolve()
	ctx, cancel := context.WithTimeout(ctx, t.CommandWait)
	defer cancel()
	d := serverconn.Device{Address: address, HostKeys: c.HostKeys, Key: key, Probe: t.DeviceProbe, Dead: t.DeviceDead}
	try := func() (serverconn.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, t.DeviceConnectAttempt)
		defer cancel()
		conn, err := serverconn.DialDevice(ctx, d)
		if err != nil {
			return nil, err
		}
		if _, err := conn.Report(ctx); err != nil {
			_ = conn.Close()
			return nil, err
		}
		return conn, nil
	}
	var wait time.Duration
	for {
		conn, err := try()
		if err == nil {
			return conn, nil
		}
		if serverconn.Permanent(err) {
			return nil, err
		}
		wait = t.NextReconnect(wait)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, err
		case <-timer.C:
		}
	}
}

// revoked runs the revocation cleanup on the device whose home is home,
// once the koko server refused its key as the key of the Revoked Device
// Session e names, with the join c, unless the Device Mux ran it already,
// and says so.
func revoked(home, socket string, c devicefiles.Config, e *serverconn.RevokedError) error {
	if _, _, err := devicefiles.Read(home); errors.Is(err, devicefiles.ErrNotJoined) {
		// The Device Mux ran the cleanup already.
		return notJoined(home, socket)
	}
	if err := devicefiles.Revoke(home, c, time.Now()); err != nil {
		return api.CodeRevoked.Errorf("%v, but koko could not finish clearing this device's files of it: %v", e, err)
	}
	return api.CodeRevoked.Errorf("%v, so koko deleted this device's key and client config; %s", e, rejoin(c.ServerAddress))
}

// notJoined is why a device that holds no join reaches no koko server: its
// revocation record, when it keeps one, or that it never joined one, and,
// unless socket is "", that nothing is at the Unix socket's path socket.
func notJoined(home, socket string) error {
	r, err := devicefiles.ReadRevocation(home)
	switch {
	case errors.Is(err, fs.ErrNotExist) && socket == "":
		return &cliError{code: codeNotJoined, message: "this device has not joined a koko server: join one with koko join <join-code>"}
	case errors.Is(err, fs.ErrNotExist):
		return &cliError{code: codeNotJoined, message: fmt.Sprintf(
			"this device has not joined a koko server, and no koko server runs on this machine (nothing at %s): join one with koko join <join-code>", socket)}
	case err != nil:
		return err
	}
	return api.CodeRevoked.Errorf("%v (koko learned it at %s), so this device is no longer joined; %s",
		&serverconn.RevokedError{Device: r.Device}, r.LearnedAt.Format(time.RFC3339), rejoin(r.ServerAddress))
}

// rejoin says how a device whose Device Session was Revoked reaches the koko
// server at address again.
func rejoin(address string) string {
	return fmt.Sprintf("to reach the koko server at %s again, join with a new Join Code: koko join <join-code>", address)
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
	}
}
