package main

import (
	"context"
	"debug/buildinfo"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/shared/build"
)

// machine is this machine, as koko server install and uninstall put the koko
// server on it: they read a kokod file's build from its Go build
// information, without running it, and run it as a child process.
type machine struct{}

var _ cli.Kokod = machine{}

func (machine) Platform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

func (machine) RunsAsRoot() bool {
	return os.Geteuid() == 0
}

func (machine) Path(p string) string {
	return p
}

func (machine) Build(file string) (build.Info, error) {
	bi, err := buildinfo.ReadFile(file)
	if err != nil {
		return build.Info{}, err
	}
	return build.Of(bi), nil
}

// kokodStopWait is how long koko waits for kokod to end after passing it an
// interrupt, before it kills it.
const kokodStopWait = 10 * time.Second

// Run runs file with stdin from /dev/null. An interrupt of koko reaches
// kokod as one too, so that it ends as it ends on Ctrl-C.
func (machine) Run(ctx context.Context, file string, args []string, stdout, stderr io.Writer) (int, error) {
	cmd := exec.CommandContext(ctx, file, args...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = kokodStopWait
	err := cmd.Run()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok && exit.Exited() {
		return exit.ExitCode(), nil
	}
	return 0, err
}
