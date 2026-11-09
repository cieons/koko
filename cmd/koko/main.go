// Command koko is the koko CLI, on every OS koko builds for (ADR 0040). It
// imports no server package: `koko server install` and `uninstall` run the
// koko server's own program, kokod, which this command gives them the way to
// read and run (machine).
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
		Kokod:          machine{},
		StartDeviceMux: devicemux.Spawn,
		Signals:        notify,
		SocketPath:     socketPath,
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
