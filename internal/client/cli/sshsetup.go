package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/devicefiles"
	"github.com/cieons/koko/internal/shared/api"
)

var sshSetupCommands = []Command{
	{
		Noun:    "ssh",
		Verb:    "setup",
		Summary: "On the koko server's own machine, write koko's ssh_config and the Include line for this OS user, so that ssh <host>.koko reaches Hosts through koko",
		// It writes only koko's own files and the one Include line, which
		// it writes again on every run (ADR 0019).
		Tier: Reversible,
		Who:  Anyone,
		// It also lists the Hosts for the bare names, as a join does.
		Op:  api.ServerShow,
		Run: action(sshSetup),
	},
}

// sshSetupJSON is what koko ssh setup prints: where it wrote koko's
// ssh_config, and why ssh does not read it yet, if it does not.
type sshSetupJSON struct {
	// SSHConfig is the absolute path of koko's ssh_config it wrote.
	SSHConfig string `json:"ssh_config"`
	// IncludeWarning is status's include_warning after the setup: why ssh
	// does not read koko's ssh_config for every host, such as an Include
	// line koko could not add, and how to fix it. Null when it does.
	IncludeWarning *string `json:"include_warning"`
}

// Human writes where koko's ssh_config is, then that ssh reaches Hosts
// through it, or why it does not yet and how to fix it.
func (s sshSetupJSON) Human(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "Wrote koko's ssh_config for this user: %s\n", s.SSHConfig); err != nil {
		return err
	}
	if s.IncludeWarning != nil {
		_, err := fmt.Fprintf(w, "%s.\n", *s.IncludeWarning)
		return err
	}
	_, err := io.WriteString(w, "ssh <host>.koko now reaches a Host through koko.\n")
	return err
}

// sshSetup is koko ssh setup: on the koko server's own machine, where
// nothing joins, it writes for the OS user running it what koko join writes
// for ssh on a device (P0 spec: reach, "Device files and ssh integration"):
// koko's known_hosts with the koko server's host key, which the Gateway
// presents inside every Tunnel; koko's ssh_config with the bare names of
// every Host the Local caller sees, which is every Host but the Retired,
// since it acts as the Owner (ADR 0008); and the Include line in
// ~/.ssh/config, by the join's rules. It runs only over the Unix socket. On
// a joined device it is refused before any call, since koko join wrote
// those files there.
func sshSetup(ctx context.Context, in *Input) (sshSetupJSON, error) {
	home := in.inv.home()
	switch c, err := devicefiles.ReadConfig(home); {
	case err == nil:
		return sshSetupJSON{}, &cliError{code: codeAlreadyJoined, message: fmt.Sprintf(
			"this device has joined the koko server at %s as %s's device %s, and koko join wrote koko's ssh_config when it joined: koko ssh setup is for the koko server's own machine, where nothing joins; koko status says whether ssh reads koko's ssh_config",
			c.ServerAddress, c.User, c.Device)}
	case !errors.Is(err, devicefiles.ErrNotJoined):
		return sshSetupJSON{}, fmt.Errorf("cannot tell whether this device has joined a koko server: %w", err)
	}

	// With no join, only the Unix socket reaches a koko server: a machine
	// without it fails here as not_joined, naming koko join and the
	// socket's path.
	resp, err := in.Call(ctx, api.ServerShow, api.ServerShowRequest{})
	if err != nil {
		return sshSetupJSON{}, err
	}
	if resp.Server.HostKey == "" {
		return sshSetupJSON{}, &cliError{code: codeServerTooOld, message: fmt.Sprintf(
			"the koko server %s does not name its host key in server.show, so koko ssh setup needs a newer koko server: upgrade the koko server", resp.Server.Version)}
	}
	hostKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(resp.Server.HostKey))
	if err != nil {
		return sshSetupJSON{}, fmt.Errorf("read the koko server's host key: %w", err)
	}
	hosts, err := in.Call(ctx, api.HostList, api.HostListRequest{})
	if err != nil {
		return sshSetupJSON{}, err
	}
	if err := devicefiles.SetUpSSH(home, []ssh.PublicKey{hostKey}, bareNames(hosts)); err != nil {
		return sshSetupJSON{}, fmt.Errorf("write koko's ssh files: %w", err)
	}
	return sshSetupJSON{SSHConfig: devicefiles.SSHConfigPath(home), IncludeWarning: addInclude(home)}, nil
}
