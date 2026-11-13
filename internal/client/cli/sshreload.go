package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/cieons/koko/internal/client/devicefiles"
	"github.com/cieons/koko/internal/shared/api"
)

// codeSSHNotSetUp: koko ssh reload on the koko server's own machine for an
// OS user koko ssh setup never wrote koko's ssh_config for; nothing was
// written.
const codeSSHNotSetUp = "ssh_not_set_up"

var sshReloadCommands = []Command{
	{
		Noun:    "ssh",
		Verb:    "reload",
		Summary: "Rewrite the bare names in koko's ssh_config now, from the Hosts you can see, and say which changed",
		// It rewrites only koko's own ssh_config, from the record (ADR 0019).
		Tier: Reversible,
		Op:   api.HostList,
		Run:  action(sshReload),
	},
}

// sshReloadJSON is what koko ssh reload prints: koko's ssh_config, the bare
// names it holds now, in host.list's order, and those that came and went.
type sshReloadJSON struct {
	// SSHConfig is the absolute path of koko's ssh_config.
	SSHConfig string `json:"ssh_config"`
	// Names are the bare names it holds now.
	Names []string `json:"names"`
	// Added are the names it did not hold before, in Names' order.
	Added []string `json:"added"`
	// Removed are the names it held before and holds no more, in the order
	// the file held them.
	Removed []string `json:"removed"`
}

// Human writes how many bare names koko's ssh_config holds, then the names
// added and removed, or that none changed.
func (s sshReloadJSON) Human(w io.Writer) error {
	held := fmt.Sprintf("koko's ssh_config holds %d bare names", len(s.Names))
	if len(s.Names) == 1 {
		held = "koko's ssh_config holds 1 bare name"
	}
	if len(s.Added) == 0 && len(s.Removed) == 0 {
		_, err := fmt.Fprintf(w, "%s; none changed.\n", held)
		return err
	}
	var b strings.Builder
	b.WriteString(held + ".\n")
	if len(s.Added) > 0 {
		b.WriteString("Added:   " + strings.Join(s.Added, " ") + "\n")
	}
	if len(s.Removed) > 0 {
		b.WriteString("Removed: " + strings.Join(s.Removed, " ") + "\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// sshReload is koko ssh reload: it rewrites the bare names in koko's
// ssh_config at once, as the caller's own Host changes do
// (rewriteBareNamesAfterHostChange), rather than at the Device Mux's next
// refresh. On a joined device it lists the Hosts on the device's Device
// Connection. On the koko server's own machine it lists them as the Local
// caller, and rewrites the file koko ssh setup wrote; for an OS user who
// never ran it, it writes none and says to run it, since koko writes no
// ssh_config into a home that did not ask for one.
func sshReload(ctx context.Context, in *Input) (sshReloadJSON, error) {
	resp, err := in.Call(ctx, api.HostList, api.HostListRequest{})
	if err != nil {
		return sshReloadJSON{}, err
	}
	home := in.inv.home()
	if in.joined == nil && !devicefiles.HasSSHConfig(home) {
		return sshReloadJSON{}, &cliError{code: codeSSHNotSetUp, message: fmt.Sprintf(
			"koko ssh setup has not written koko's ssh_config for this OS user (nothing at %s), so it holds no bare names to reload: run koko ssh setup, which writes it with them",
			devicefiles.SSHConfigPath(home))}
	}
	before, err := devicefiles.BareNames(home)
	if err != nil {
		return sshReloadJSON{}, fmt.Errorf("read the bare names koko's ssh_config holds: %w", err)
	}
	names := bareNames(resp)
	if _, err := devicefiles.WriteSSHConfig(home, names); err != nil {
		return sshReloadJSON{}, fmt.Errorf("rewrite the bare names in koko's ssh_config: %w", err)
	}
	return sshReloadJSON{
		SSHConfig: devicefiles.SSHConfigPath(home),
		Names:     names,
		Added:     slices.DeleteFunc(slices.Clone(names), func(n string) bool { return slices.Contains(before, n) }),
		Removed:   slices.DeleteFunc(before, func(n string) bool { return slices.Contains(names, n) }),
	}, nil
}
