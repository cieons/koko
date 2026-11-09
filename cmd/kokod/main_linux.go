package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/cieons/koko/internal/server"
	"github.com/cieons/koko/internal/server/filesystem"
	"github.com/cieons/koko/internal/server/installer"
	"github.com/cieons/koko/internal/server/kokod"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	p := kokod.Program{Serve: serve, Machine: installer.System{}, Binary: os.Executable}
	status := p.Run(ctx, kokod.Invocation{
		Args:   os.Args[1:],
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Env:    os.Environ(),
	})
	stop()
	os.Exit(status)
}

// serve runs the koko server in this process on this machine's mounts and
// process uids.
func serve(ctx context.Context, config server.Config) error {
	config.Filesystems = filesystem.System
	config.ProcessUIDs = unix.Getresuid
	return server.Run(ctx, config)
}
