package cli

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/build"
	"github.com/cieons/koko/internal/shared/install"
)

// Kokod is the machine koko server install and uninstall put the koko server
// on, and how they read and run the kokod files there, which do the
// machine's part (ADR 0041). cmd/koko sets it to this machine, reading a
// file's Go build information and running it as a child process; the S1
// harness to a fake machine (harness.NewMachine), which runs kokod's command
// line in process.
type Kokod interface {
	// Platform is the machine's OS and architecture, as GOOS/GOARCH, such
	// as linux/amd64.
	Platform() string
	// RunsAsRoot tells whether koko runs as root.
	RunsAsRoot() bool
	// Path is the real path of the machine's path p: p itself on a real
	// machine.
	Path(p string) string
	// Build reads the build of the file at the real path file, without
	// running it.
	Build(file string) (build.Info, error)
	// Run runs the kokod file at the real path file with args, stdin empty,
	// and returns its exit status; an error when it did not run at all.
	Run(ctx context.Context, file string, args []string, stdout, stderr io.Writer) (status int, err error)
}

// kokodFlag is the flag of koko server install and uninstall that gives the
// kokod to run.
var kokodFlag = Flag{
	Name:  "kokod",
	Value: "file",
	Summary: "the kokod file of this koko's build to run, as on a machine offline; without it, the kokod installed at " +
		install.KokodPath + " when it is this koko's build",
}

// kokodToRun returns the real path of the kokod file koko server install or
// uninstall is to run: --kokod's, or else the kokod installed, when it is
// this koko's build. Before it reads any kokod, it refuses a --kokod that
// names no file, then a machine that cannot run the koko server; it refuses
// a kokod of another build, before it runs.
func kokodToRun(in *Input) (string, error) {
	given := in.Flag("kokod")
	if given != "" {
		abs, err := filepath.Abs(given)
		if err != nil {
			return "", fmt.Errorf("find --kokod %s: %w", given, err)
		}
		switch info, err := os.Stat(abs); {
		case errors.Is(err, fs.ErrNotExist):
			return "", usagef("--kokod names %s, which is not there; nothing was written", given)
		case err != nil:
			return "", fmt.Errorf("read --kokod %s: %w", given, err)
		case !info.Mode().IsRegular():
			return "", usagef("--kokod names %s, which is not a file; nothing was written", given)
		}
		given = abs
	}
	k := in.runner.Kokod
	if k == nil {
		return "", errors.New("this koko has no way to run kokod")
	}
	if err := checkMachine(in, k); err != nil {
		return "", refusalError(err)
	}
	self := build.Read()
	if given != "" {
		return given, checkKokod(k, given, self)
	}
	installed := k.Path(install.KokodPath)
	there := "there is none at " + install.KokodPath
	if b, err := k.Build(installed); err == nil && b.Path == install.KokodPackage {
		if b.Same(self) && b.Platform == k.Platform() {
			return installed, nil
		}
		there = fmt.Sprintf("the one at %s is %s", install.KokodPath, b)
	} else if !errors.Is(err, fs.ErrNotExist) {
		there = "the file at " + install.KokodPath + " is no kokod"
	}
	return "", usagef("koko server %s runs the kokod of this koko's build, %s, and %s: give it with --kokod <file>, such as --kokod bin/kokod beside a development build's bin/koko; nothing was written",
		in.line.cmd.Verb, self, there)
}

// checkMachine refuses, before any kokod is read, a machine that cannot run
// the koko server: not Linux, an architecture no kokod is built for, koko
// not running as root, and systemd not running the machine. kokod install
// refuses the last too; it is checked here so that a container learns of
// the image at once.
func checkMachine(in *Input, k Kokod) error {
	platform := k.Platform()
	goos, _, _ := strings.Cut(platform, "/")
	switch {
	case goos != "linux":
		return install.Refuse(install.Unsupported, "the koko server runs only on Linux with systemd, and this machine is %s: to run it elsewhere, run the image %s in a container (%s)",
			platform, install.ContainerImage, install.ContainerDocs)
	case !slices.Contains(install.KokodPlatforms, platform):
		return install.Refuse(install.Unsupported, "there is no kokod for %s: the koko server is built for %s only",
			platform, strings.Join(install.KokodPlatforms, " and "))
	case !k.RunsAsRoot():
		return install.Refuse(install.Unsupported, "koko server %s changes the machine's services, files and users, so it runs as root: run %s",
			in.line.cmd.Verb, shellJoin(append([]string{"sudo", "koko"}, in.inv.Args...)))
	}
	if info, err := os.Stat(k.Path(install.SystemdRunDir)); err != nil || !info.IsDir() {
		return install.NoSystemd()
	}
	return nil
}

// checkKokod refuses the file given with --kokod, before it runs, unless it
// is kokod of the build self, built for the machine's platform.
func checkKokod(k Kokod, file string, self build.Info) error {
	b, err := k.Build(file)
	switch {
	case err != nil:
		return usagef("--kokod names %s, which is not kokod: %v; nothing was written", file, err)
	case b.Path != install.KokodPackage:
		return usagef("--kokod names %s, which is %s, not kokod (%s); nothing was written", file, b.Path, install.KokodPackage)
	case !b.Same(self):
		return usagef("--kokod names kokod %s, and this koko is %s: koko runs only the kokod of its own build, so give that one; nothing was written",
			b, self)
	case b.Platform != k.Platform():
		return usagef("--kokod names kokod for %s, and this machine is %s: give the kokod built for it; nothing was written",
			b.Platform, k.Platform())
	}
	return nil
}

// runKokod runs `kokod <args> --json` from file and reads the report it
// prints into report. A refusal kokod prints becomes the command's, with
// its code and message; whatever else kokod writes on stderr is passed
// through.
func runKokod(ctx context.Context, in *Input, file string, args []string, report any) error {
	var stdout, stderr bytes.Buffer
	status, err := in.runner.Kokod.Run(ctx, file, append(slices.Clone(args), "--json"), &stdout, &stderr)
	if err != nil {
		return fmt.Errorf("run kokod %s from %s: %w", args[0], file, err)
	}
	if status == exitOK {
		_, _ = in.inv.Stderr.Write(stderr.Bytes())
		if err := json.Unmarshal(stdout.Bytes(), report); err != nil {
			return fmt.Errorf("read the report kokod %s printed: %w", args[0], err)
		}
		return nil
	}
	var refused api.ErrorResponse
	if err := json.Unmarshal(stderr.Bytes(), &refused); err == nil && refused.Error.Code != "" {
		if status == exitRefused {
			return &usageError{message: refused.Error.Message}
		}
		return &cliError{code: refused.Error.Code, message: refused.Error.Message}
	}
	_, _ = in.inv.Stderr.Write(stderr.Bytes())
	return fmt.Errorf("kokod %s exited with status %d", args[0], status)
}
