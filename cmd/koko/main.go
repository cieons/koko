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
	"github.com/cieons/koko/internal/client/devicemux"
)

// socketPath is the path of the koko server's Unix socket, for the
// real-OpenSSH test only, which builds this binary with -ldflags "-X
// main.socketPath=<path>" so that it reaches the test's koko server there
// and never one running on the machine. Every other build leaves it empty:
// the fixed path (cli.Runner.SocketPath, the socket-path hook).
var socketPath string

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	r := cli.Runner{
		ServerRun:       serverRun,
		ServerInstall:   serverInstall,
		ServerUninstall: serverUninstall,
		StartDeviceMux:  devicemux.Spawn,
		Signals:         notify,
		SocketPath:      socketPath,
	}
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

// notify relays the signals sigs this process receives to c until stop is
// called, for a command that handles a signal itself (cli.Runner.Signals).
func notify(c chan<- os.Signal, sigs ...os.Signal) (stop func()) {
	signal.Notify(c, sigs...)
	return func() { signal.Stop(c) }
}
