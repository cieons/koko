package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// openBrowser opens address in the user's browser with this OS's opener,
// found on the invocation's PATH. It returns at once, with a function that
// waits for the opener to exit, which koko web calls before it exits itself.
// When the opener cannot run or fails, it says so on stderr: koko web serves
// on, and the address it printed can be opened by hand. The token shows in
// the opener's command line while it runs, an accepted cost (ADR 0022).
func openBrowser(ctx context.Context, inv *Invocation, address string) (wait func()) {
	warn := func(err error) {
		_, _ = fmt.Fprintf(inv.Stderr, "koko: could not open a browser (%v); open the address above in one, or run koko web --no-open\n", err)
	}
	name, args, err := browserOpener(address)
	if err == nil {
		name, err = lookPath(name, inv.getenv("PATH"))
	}
	if err != nil {
		warn(err)
		return func() {}
	}
	// Not cancelled with ctx: stopping koko web does not stop the opener,
	// which may be the browser itself.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), name, args...)
	cmd.Env = inv.Env
	if err := cmd.Start(); err != nil {
		warn(err)
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := cmd.Wait(); err != nil {
			warn(fmt.Errorf("%s: %w", filepath.Base(name), err))
		}
	}()
	return func() { <-done }
}

// lookPath finds the executable name in the absolute directories of path, a
// PATH value, so that the invocation's environment decides, as a shell's
// would.
func lookPath(name, path string) (string, error) {
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			continue
		}
		file := filepath.Join(dir, name)
		if fi, err := os.Stat(file); err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0 {
			return file, nil
		}
	}
	return "", errors.New(name + " is not on PATH")
}
