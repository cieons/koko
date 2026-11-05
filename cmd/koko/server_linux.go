package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/server"
	"github.com/cieons/koko/internal/server/installer"
	"github.com/cieons/koko/internal/shared/install"
)

// serverRun runs the koko server in this process. Only Linux builds carry the
// koko server, which runs on Linux only.
func serverRun(ctx context.Context, s cli.ServerSettings, log io.Writer) error {
	return server.Run(ctx, server.Config{
		DataDir:     s.DataDir,
		RootKeyPath: s.RootKeyPath,
		Listen:      s.Listen,
		LogLevel:    s.LogLevel,
		Log:         log,
	})
}

// serverInstall installs this binary as the koko server on this machine.
// Only Linux builds carry the installer, since the koko server runs on Linux
// only.
func serverInstall(ctx context.Context, req install.Request, initialise install.Initialise) (install.Report, error) {
	binary, err := os.Executable()
	if err != nil {
		return install.Report{}, fmt.Errorf("find the running koko binary: %w", err)
	}
	inst := &installer.Installer{Machine: installer.System{}, Binary: binary}
	return inst.Install(ctx, req, initialise)
}
