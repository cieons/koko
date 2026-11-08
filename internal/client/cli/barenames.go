package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/cieons/koko/internal/client/apiclient"
	"github.com/cieons/koko/internal/client/devicefiles"
	"github.com/cieons/koko/internal/shared/api"
)

// writeBareNames rewrites the bare names in koko's ssh_config on the device
// whose home is home, from the Hosts its User can see now, through c, a
// client of the device's Device Connection (P0 spec: reach, "Bare names").
// What asked for it has happened by then, a join or a change to a Host, so
// a failure does not fail the command: it says so on stderr, since ssh
// <host>.koko reaches every Host meanwhile.
func writeBareNames(ctx context.Context, c *apiclient.Client, home string, stderr io.Writer) {
	resp, err := c.Call(ctx, api.HostList, api.HostListRequest{})
	if err == nil {
		names := make([]string, 0, len(resp.Hosts))
		for _, h := range resp.Hosts {
			names = append(names, h.Name)
		}
		_, err = devicefiles.WriteSSHConfig(home, names)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "koko: could not rewrite the bare names in koko's ssh_config (%v); until koko does, reach a Host as ssh <host>.koko\n", err)
	}
}

// rewriteBareNamesAfterHostChange runs writeBareNames after the caller's
// own host add, rename, move, retire or rm, so that the change takes effect
// at once. Only a joined device's Device Connection has a list to rewrite:
// over the Unix socket the caller is the Local caller, whose machine joined
// nothing.
func (in *Input) rewriteBareNamesAfterHostChange(ctx context.Context) {
	if in.joined != nil {
		writeBareNames(ctx, in.client(), in.inv.getenv("HOME"), in.inv.Stderr)
	}
}
