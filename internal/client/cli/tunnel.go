package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/cieons/koko/internal/client/tunnel"
	"github.com/cieons/koko/internal/shared/resumable"
	"github.com/cieons/koko/internal/shared/wire"
)

var tunnelCommands = []Command{
	{
		Verb:    "tunnel",
		Summary: "Carry ssh's connection to a Host on stdin and stdout, as the ProxyCommand of koko's ssh_config; not for direct use",
		Args:    []Arg{{Name: "ssh-name"}},
		Stdio:   true,
		// It reaches a Host (ADR 0019).
		Tier: Irreversible,
		Run:  quietAction(proxySSH),
	},
}

// proxySSH is koko tunnel, the ProxyCommand of koko's ssh_config: it relays
// a Tunnel to the Host the SSH Name names on stdin and stdout, for ssh's SSH
// Session to the Gateway (docs/subsystems/gateway.md, "koko tunnel"). It
// ignores SIGINT, which ends ctx, and ends on SIGHUP or SIGTERM with a close
// frame.
func proxySSH(ctx context.Context, in *Input) error {
	sshName := in.Arg("ssh-name")
	hostName := wire.TunnelHost(sshName)
	ctx, hangUp := context.WithCancel(context.WithoutCancel(ctx))
	defer hangUp()
	if in.runner.Signals != nil {
		sigs := make(chan os.Signal, 1)
		stop := in.runner.Signals(sigs, syscall.SIGHUP, syscall.SIGTERM)
		defer stop()
		go func() {
			select {
			case <-sigs:
				hangUp()
			case <-ctx.Done():
			}
		}()
	}

	conn, err := in.server(ctx)
	switch {
	case ctx.Err() != nil:
		return nil
	case err != nil:
		return err
	}
	t, err := tunnel.Open(ctx, conn, sshName)
	switch {
	case err != nil && ctx.Err() != nil:
		return nil
	case err != nil:
		return fmt.Errorf("open a Tunnel to %s: %w", hostName, err)
	}
	defer func() { _ = t.Close() }()
	// At once for a signal that came as the Tunnel opened.
	stop := context.AfterFunc(ctx, func() { _ = t.Close() })
	defer stop()

	// Reading stdin may outlive the command, for a stdin that never ends,
	// once the Tunnel has.
	go func() {
		_, _ = io.Copy(t, in.inv.Stdin)
		_ = t.CloseWrite()
	}()
	_, err = io.Copy(in.inv.Stdout, t)
	if ctx.Err() != nil {
		return nil
	}
	cause := t.Err()
	if _, ok := errors.AsType[*tunnel.LostError](cause); ok {
		return sessionError(hostName, t, err)
	}
	if _, ok := errors.AsType[*resumable.PeerClosedError](cause); ok {
		return nil
	}
	if err != nil {
		return fmt.Errorf("relay the Tunnel to %s: %w", hostName, err)
	}
	return nil
}
