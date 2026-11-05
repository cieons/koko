package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/client/tunnel"
	"github.com/cieons/koko/internal/shared/wire"
)

var execCommands = []Command{
	{
		Verb:          "exec",
		Summary:       "Run one command on a Host through the Gateway, passing its stdin, stdout, stderr and exit status",
		Args:          []Arg{{Name: "host"}, {Name: "command", Many: true}},
		RemoteCommand: true,
		// It reaches a Host (ADR 0019).
		Tier: Irreversible,
		Run:  remoteAction(execute),
	},
}

// execute is koko exec: an SSH Session through a Tunnel, the path ssh takes,
// with Go's SSH client inside the Tunnel instead of OpenSSH. It joins the
// command's words with spaces, as ssh does, so the Host Account's shell reads
// them as one command line; sends that as one exec request, with no pty;
// passes stdin to the command and its stdout and stderr apart; and returns
// the command's exit status. A command a signal ended fails, as under ssh,
// naming the signal. It forwards no signal: Ctrl-C ends the SSH Session, as
// it ends ssh without a pty (ADR 0033).
func execute(ctx context.Context, in *Input) (int, error) {
	sshName := in.Arg("host")
	hostName := wire.TunnelHost(sshName)
	conn, err := in.server(ctx)
	if err != nil {
		return 0, err
	}
	t, err := tunnel.Open(ctx, conn, sshName)
	switch {
	case err != nil && ctx.Err() != nil:
		return 0, interrupted(hostName)
	case err != nil:
		return 0, fmt.Errorf("open a Tunnel to %s: %w", hostName, err)
	}
	defer func() { _ = t.Close() }()
	client, err := in.dialGateway(ctx, t, sshName)
	if err != nil {
		return 0, err
	}
	defer func() { _ = client.Close() }()

	s, err := client.NewSession()
	if err != nil {
		return 0, sessionError(hostName, t, err)
	}
	s.Stdin, s.Stdout, s.Stderr = in.inv.Stdin, in.inv.Stdout, in.inv.Stderr
	done := make(chan struct{})
	go func() {
		defer close(done)
		err = s.Run(strings.Join(in.Args("command"), " "))
	}()
	select {
	case <-done:
	case <-ctx.Done():
		_ = client.Close()
		<-done
		return 0, interrupted(hostName)
	}
	exit, exited := errors.AsType[*ssh.ExitError](err)
	switch {
	case exited && exit.Signal() != "":
		return 0, fmt.Errorf("the command on %s was ended by SIG%s", hostName, exit.Signal())
	case exited:
		return exit.ExitStatus(), nil
	}
	if _, ok := errors.AsType[*ssh.ExitMissingError](err); ok {
		return 0, fmt.Errorf("the command on %s ended without an exit status", hostName)
	}
	if err != nil {
		return 0, sessionError(hostName, t, err)
	}
	return 0, nil
}

// interrupted is the failure of a koko exec that Ctrl-C ended.
func interrupted(hostName string) error {
	return fmt.Errorf("interrupted, so koko ended the SSH Session to %s", hostName)
}

// dialGateway runs koko's SSH client on t against the Gateway: "none"
// authentication as wire.TunnelUser, AES-GCM, and the koko server's host key
// checked against the ones this device pinned when it joined, as on the
// Device Connection beneath it. Over the Unix socket, whose directory is its
// pin (ADR 0008), nothing is pinned, and the key is not checked. A refusal
// fails with the Gateway's banner as its message, without its "koko: ", and
// any other failure with sessionError.
func (in *Input) dialGateway(ctx context.Context, t *tunnel.Tunnel, sshName string) (*ssh.Client, error) {
	hostName := wire.TunnelHost(sshName)
	hostKey := ssh.InsecureIgnoreHostKey()
	if in.joined != nil {
		hostKey = serverconn.Pinned(in.joined.ServerAddress, in.joined.HostKeys)
	}
	var banner string
	stop := context.AfterFunc(ctx, func() { _ = t.Close() })
	defer stop()
	nc := wire.TunnelConn(t, wire.TunnelAddr("koko exec"), wire.TunnelAddr(sshName))
	c, chans, reqs, err := ssh.NewClientConn(nc, sshName, &ssh.ClientConfig{
		Config:          ssh.Config{Ciphers: wire.TunnelCiphers},
		User:            wire.TunnelUser,
		ClientVersion:   wire.Version,
		HostKeyCallback: hostKey,
		BannerCallback: func(message string) error {
			banner = message
			return nil
		},
	})
	switch {
	case err != nil && banner != "":
		return nil, errors.New(strings.TrimPrefix(strings.TrimSpace(banner), "koko: "))
	case err != nil && ctx.Err() != nil:
		return nil, interrupted(hostName)
	case err != nil:
		return nil, sessionError(hostName, t, err)
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// sessionError says why the SSH Session to hostName failed with err: the
// Tunnel t beneath it lost its channel, or the SSH Session itself failed.
func sessionError(hostName string, t *tunnel.Tunnel, err error) error {
	if lost, ok := errors.AsType[*tunnel.LostError](t.Err()); ok {
		return fmt.Errorf("%w; the SSH Session to %s could not be resumed", lost, hostName)
	}
	return fmt.Errorf("the SSH Session to %s failed: %w", hostName, err)
}
