//go:build !linux

package main

import (
	"context"
	"io"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/shared/install"
)

// serverRun, serverInstall and serverUninstall are nil: the koko server runs
// on Linux only, so this build does not carry it, and `koko server run`,
// `koko server install` and `koko server uninstall` refuse.
var (
	serverRun       func(ctx context.Context, s cli.ServerSettings, log io.Writer) error
	serverInstall   func(ctx context.Context, req install.Request, initialise install.Initialise) (install.Report, error)
	serverUninstall func(ctx context.Context) (install.UninstallReport, error)
)
