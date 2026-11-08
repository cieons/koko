package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/client/tunnel"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/resumable"
	"github.com/cieons/koko/internal/shared/sshconn"
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
	t, err := tunnel.Open(ctx, conn, sshName, in.redial, in.runner.Timings)
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
	stderr := &tailWriter{w: in.inv.Stderr}
	s.Stdin, s.Stdout, s.Stderr = in.inv.Stdin, in.inv.Stdout, stderr
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
	if notice := gatewayNotice(t); notice != "" {
		if stderr.endsWith(wire.TunnelNoticeLine(notice)) {
			// The Gateway wrote why on the command's stderr, as ssh shows
			// it: 255 says that koko failed, and nothing more is needed.
			return exitRemote, nil
		}
		return 0, errors.New(notice)
	}
	if _, ok := errors.AsType[*tunnel.LostError](t.Err()); ok {
		// The command's channel closed with the Tunnel beneath it, which
		// could not Resume.
		return 0, sessionError(hostName, t, err)
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
	return ssh.NewClient(sshconn.Wrap(c), chans, reqs), nil
}

// sessionError says why the SSH Session to hostName failed with err: the
// Gateway ended it with a notice, koko disconnect ended the Tunnel t
// beneath it, which does not Resume, t lost its channel and could not
// Resume, or the SSH Session itself failed.
func sessionError(hostName string, t *tunnel.Tunnel, err error) error {
	if notice := gatewayNotice(t); notice != "" {
		return errors.New(notice)
	}
	if lost, ok := errors.AsType[*tunnel.LostError](t.Err()); ok {
		if errors.Is(lost, serverconn.ErrDisconnected) {
			return fmt.Errorf("koko disconnect ended the SSH Session to %s", hostName)
		}
		line := fmt.Sprintf("%v; the SSH Session to %s could not be resumed", lost, hostName)
		if why := lostWhy(lost); why != "" {
			line += ": " + why
		}
		// Not wrapping lost, whose error a revocation's code would print
		// in place of the line.
		return errors.New(line + ".")
	}
	return fmt.Errorf("the SSH Session to %s failed: %w", hostName, err)
}

// lostWhy says why the Tunnel that lost could not Resume, as the clause
// its line ends with: the koko server's reason for refusing the resume,
// the device's Device Session Revoked, or the device giving up; "" when
// none is known, as when the koko server no longer held the Tunnel.
func lostWhy(lost *tunnel.LostError) string {
	if r, ok := errors.AsType[*tunnel.RefusedError](lost); ok {
		return r.Reason
	}
	if _, e := classify(lost.Err); errors.Is(lost, serverconn.ErrRevoked) || e.Code == api.CodeRevoked.Name {
		return "this device's Device Session was Revoked"
	}
	if errors.Is(lost, tunnel.ErrGaveUp) {
		return tunnel.ErrGaveUp.Error()
	}
	return ""
}

// gatewayNotice returns the notice the Gateway closed the Tunnel t with,
// such as that the koko server is restarting, or "" for none.
func gatewayNotice(t *tunnel.Tunnel) string {
	if e, ok := errors.AsType[*resumable.PeerClosedError](t.Err()); ok {
		return e.Reason
	}
	return ""
}

// tailWriter writes to w and keeps the last bytes written, so koko exec can
// tell whether the Gateway's notice is the last line its stderr shows.
type tailWriter struct {
	w    io.Writer
	tail []byte
}

// tailSize is more than a notice line takes.
const tailSize = 1024

func (t *tailWriter) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	t.tail = append(t.tail, p[:n]...)
	if over := len(t.tail) - tailSize; over > 0 {
		t.tail = bytes.Clone(t.tail[over:])
	}
	return n, err
}

func (t *tailWriter) endsWith(line string) bool {
	return bytes.HasSuffix(t.tail, []byte(line))
}
