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
// client of the device's Device Connection, or of the Unix socket after koko
// ssh setup (P0 spec: reach, "Bare names").
// What asked for it has happened by then, a join or a change to a Host, so
// a failure does not fail the command: it says so on stderr, since ssh
// <host>.koko reaches every Host meanwhile.
func writeBareNames(ctx context.Context, c *apiclient.Client, home string, stderr io.Writer) {
	resp, err := c.Call(ctx, api.HostList, api.HostListRequest{})
	if err == nil {
		_, err = devicefiles.WriteSSHConfig(home, bareNames(resp))
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "koko: could not rewrite the bare names in koko's ssh_config (%v); until koko does, reach a Host as ssh <host>.koko\n", err)
	}
}

// bareNames are the names of the Hosts host.list answered: every Host the
// caller can see that is not Retired.
func bareNames(resp api.HostListResponse) []string {
	names := make([]string, 0, len(resp.Hosts))
	for _, h := range resp.Hosts {
		names = append(names, h.Name)
	}
	return names
}

// rewriteBareNamesAfterHostChange runs writeBareNames after the caller's
// own host add, rename, move, retire or rm, so that the change takes effect
// at once. A joined device's Device Connection always has a list to
// rewrite. Over the Unix socket the caller is the Local caller, on the koko
// server's own machine, where nothing joins: there it rewrites one only for
// an OS user whose koko ssh setup wrote koko's ssh_config, and writes none
// into a home that never asked for it.
func (in *Input) rewriteBareNamesAfterHostChange(ctx context.Context) {
	home := in.inv.home()
	if in.joined != nil || (in.socket != "" && devicefiles.HasSSHConfig(home)) {
		writeBareNames(ctx, in.client(), home, in.inv.Stderr)
	}
}
