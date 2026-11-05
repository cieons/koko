//go:build !linux

package main

import (
	"context"
	"io"

	"github.com/cieons/koko/internal/client/cli"
)

// serverRun is nil: the koko server runs on Linux only, so this build does not
// carry it, and `koko server run` refuses.
var serverRun func(ctx context.Context, s cli.ServerSettings, log io.Writer) error
