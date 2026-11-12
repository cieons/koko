//go:build unix

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// openAddress opens address in the user's browser by running this OS's
// opener, found on the invocation's PATH, and waits for it to exit. The
// token shows in the opener's command line while it runs, an accepted cost
// (ADR 0022).
func openAddress(ctx context.Context, inv *Invocation, address string) error {
	name, err := lookPath(opener, inv.getenv("PATH"))
	if err != nil {
		return err
	}
	// Not cancelled with ctx: stopping koko web does not stop the opener,
	// which may be the browser itself.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), name, address)
	cmd.Env = inv.Env
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", opener, err)
	}
	return nil
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
