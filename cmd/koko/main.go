// Command koko is the koko CLI and, in Linux builds, the koko server.
//
// It is one of the two places (with the test harness) that import both the
// server and the client package groups: it hands `koko server run` the koko
// server, and `koko server install` and `uninstall` its installer, through
// cli.Runner, so no client package imports a server package.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/cieons/koko/internal/client/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	r := cli.Runner{ServerRun: serverRun, ServerInstall: serverInstall, ServerUninstall: serverUninstall}
	status := r.Run(ctx, cli.Invocation{
		Args:   os.Args[1:],
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Env:    os.Environ(),
	})
	stop()
	os.Exit(status)
}
