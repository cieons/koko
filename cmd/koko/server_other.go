//go:build !linux

package main

import (
	"context"
	"io"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/shared/install"
)

// serverRun and serverInstall are nil: the koko server runs on Linux only, so
// this build does not carry it, and `koko server run` and `koko server
// install` refuse.
var (
	serverRun     func(ctx context.Context, s cli.ServerSettings, log io.Writer) error
	serverInstall func(ctx context.Context, req install.Request, initialise install.Initialise) (install.Report, error)
)
