package main

import (
	"context"
	"io"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/server"
)

// serverRun runs the koko server in this process. Only Linux builds carry the
// koko server, which runs on Linux only.
func serverRun(ctx context.Context, s cli.ServerSettings, log io.Writer) error {
	return server.Run(ctx, server.Config{
		DataDir:     s.DataDir,
		RootKeyPath: s.RootKeyPath,
		LogLevel:    s.LogLevel,
		Log:         log,
	})
}
