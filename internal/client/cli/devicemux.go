package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cieons/koko/internal/client/devicemux"
	"github.com/cieons/koko/internal/client/serverconn"
)

// errDirect says no Device Mux can carry a command, which then opens its
// own Device Connection: the direct path.
var errDirect = errors.New("no Device Mux can carry this command")

// deviceMuxSeen is the Device Mux a command reached, by its socket, and the
// state it answered in.
type deviceMuxSeen struct {
	socket string
	state  devicemux.State
}

// viaDeviceMux reaches the koko server through this build's Device Mux,
// starting one when none answers and waiting up to timing.DeviceMuxStart for
// its socket, or for the socket of the one that won the race to start. A
// Device Mux that answers decides the outcome, an error included. It fails
// wrapping errDirect, and logs why at debug level, when no Device Mux can
// carry the command: KOKO_DIRECT=1, a runtime directory koko cannot use, a
// Device Mux of another home, or one that does not start or exits at once.
func (in *Input) viaDeviceMux(ctx context.Context) (conn serverconn.Conn, err error) {
	defer func() {
		if errors.Is(err, errDirect) {
			in.log.Debug("opening a direct Device Connection", "reason", err)
		}
	}()
	socket, err := in.deviceMuxSocket()
	if err != nil {
		return nil, err
	}
	conn, err = in.dialDeviceMuxAt(ctx, socket)
	if !errors.Is(err, devicemux.ErrNotRunning) {
		return conn, err
	}
	exited, err := in.runner.StartDeviceMux(ctx, socket, in.inv.Env)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errDirect, err)
	}
	wait := in.runner.Timings.Resolve().DeviceMuxStart
	deadline := time.After(wait)
	for delay := 5 * time.Millisecond; ; delay = min(2*delay, 100*time.Millisecond) {
		var gone, late bool
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-exited:
			gone, exited = true, nil
		case <-deadline:
			late = true
		case <-time.After(delay):
		}
		conn, err = in.dialDeviceMuxAt(ctx, socket)
		switch {
		case !errors.Is(err, devicemux.ErrNotRunning):
			return conn, err
		case gone:
			return nil, fmt.Errorf("%w: the Device Mux it started exited, and none answers (its log is beside %s): %w", errDirect, socket, err)
		case late:
			return nil, fmt.Errorf("%w: the Device Mux it started did not answer within %v: %w", errDirect, wait, err)
		}
	}
}

// deviceMuxSocket returns this build's Device Mux socket, failing wrapping
// errDirect when no Device Mux may carry the command.
func (in *Input) deviceMuxSocket() (string, error) {
	switch {
	case in.inv.getenv("KOKO_DIRECT") == "1":
		return "", fmt.Errorf("%w: KOKO_DIRECT=1", errDirect)
	case in.runner.StartDeviceMux == nil:
		return "", fmt.Errorf("%w: this koko starts no Device Mux", errDirect)
	}
	socket, err := devicemux.Socket(in.inv.getenv)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errDirect, err)
	}
	return socket, nil
}

// dialDeviceMuxAt connects to the Device Mux at socket, waits for the
// report it sends at once, and notes the state it answered in, for koko
// status. One of another home fails wrapping errDirect.
func (in *Input) dialDeviceMuxAt(ctx context.Context, socket string) (serverconn.Conn, error) {
	conn, state, err := devicemux.Dial(ctx, socket, in.inv.getenv("HOME"))
	switch {
	case errors.Is(err, devicemux.ErrOtherHome):
		return nil, fmt.Errorf("%w: %w", errDirect, err)
	case !errors.Is(err, devicemux.ErrNotRunning):
		in.deviceMux = &deviceMuxSeen{socket: socket, state: state}
	}
	if err == nil {
		if _, err = conn.Report(ctx); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("the Device Mux at %s sent no report: %w", socket, err)
		}
	}
	return conn, err
}

// json is the Device Mux m as koko status and koko connect show it, from
// the state it answered in.
func (m *deviceMuxSeen) json() *deviceMuxJSON {
	j := &deviceMuxJSON{Socket: m.socket, State: "connected", Since: m.state.Since.UTC().Format(time.RFC3339), WaitingToResume: m.state.Resuming}
	if !m.state.Connected {
		j.State = "reconnecting"
	}
	return j
}

// startDeviceMux starts this build's Device Mux once koko join has joined,
// so the next command finds its Device Connection open. The join holds
// whether or not it starts: a command starts one later, or goes direct.
func (in *Input) startDeviceMux(ctx context.Context) {
	socket, err := in.deviceMuxSocket()
	if err == nil {
		_, err = in.runner.StartDeviceMux(ctx, socket, in.inv.Env)
	}
	if err != nil {
		in.log.Debug("koko join started no Device Mux", "reason", err)
	}
}

// runDeviceMux runs this build's Device Mux, for `koko device-mux`, the
// entry point devicemux.Spawn starts this binary at: it is no command of
// the tree. It logs to stderr, which Spawn points at the log beside its
// socket.
func (r *Runner) runDeviceMux(ctx context.Context, inv *Invocation) int {
	log, err := logger(inv)
	if err != nil {
		return writeError(inv.Stderr, err, false)
	}
	socket, err := devicemux.Socket(inv.getenv)
	if err == nil {
		err = devicemux.Run(ctx, devicemux.Config{Home: inv.getenv("HOME"), Socket: socket, Log: log, Timings: r.Timings})
	}
	if err != nil {
		log.Error("the Device Mux cannot run", "err", err)
		return exitFailed
	}
	return exitOK
}

// logLevels are the values KOKO_LOG_LEVEL takes, as kokod run's
// --log-level does.
var logLevels = map[string]slog.Level{"debug": slog.LevelDebug, "info": slog.LevelInfo, "warn": slog.LevelWarn, "error": slog.LevelError}

// logger returns a log on the invocation's stderr, in slog's text format,
// at the level KOKO_LOG_LEVEL names, info when it is unset. A value it does
// not know refuses the command line, as kokod run refuses it.
func logger(inv *Invocation) (*slog.Logger, error) {
	name := cmp.Or(inv.getenv("KOKO_LOG_LEVEL"), "info")
	level, ok := logLevels[name]
	if !ok {
		return nil, usagef("KOKO_LOG_LEVEL is %q: it takes debug, info, warn or error", name)
	}
	return slog.New(slog.NewTextHandler(inv.Stderr, &slog.HandlerOptions{Level: level})), nil
}
