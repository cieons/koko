package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/cieons/koko/internal/shared/build"
	"github.com/cieons/koko/internal/shared/install"
	"github.com/cieons/koko/internal/shared/release"
)

// Releases is where koko finds its releases and downloads their assets
// from: the releases hook (Runner.Releases).
type Releases struct {
	// URL is the HTTPS URL of koko's releases, below which are latest and
	// each release's assets, as download/<version>/<name>; empty means
	// GitHub's, release.GitHub.
	URL string
	// Client makes the requests, under koko's own rules for redirects and
	// time; nil means one with Go's default transport.
	Client *http.Client
}

// releases is where koko finds its releases, reading a downloaded file's
// build as k does.
func (in *Input) releases(k Kokod) release.Source {
	return release.Source{URL: in.runner.Releases.URL, Client: in.runner.Releases.Client, Build: k.Build}
}

// downloadFailed is err as the CLI reports it: download_failed, with
// suffix after its message, for a release koko could not find or download,
// or a download that failed a check; any other error as it is.
func downloadFailed(err error, suffix string) error {
	if failed, ok := errors.AsType[*release.Error](err); ok {
		return &cliError{code: codeDownloadFailed, message: failed.Error() + suffix}
	}
	return err
}

// ownKokod is the build of the kokod of koko's own build, for the
// machine's platform: the only kokod koko runs.
func ownKokod(k Kokod) release.Want {
	self := k.Self()
	return release.Want{
		Info: build.Info{Path: install.KokodPackage, Version: self.Version, Platform: k.Platform()},
		Own:  &self,
	}
}

// downloadKokod downloads the kokod of koko's release for the machine's
// platform into a new 0700 directory under TMPDIR, and checks it against the
// release's SHA256SUMS and its build against koko's. It returns the file's
// path and a function that removes the directory; on a failure, it has
// removed it already.
func downloadKokod(ctx context.Context, in *Input, k Kokod) (file string, done func(), err error) {
	want := ownKokod(k)
	name := install.KokodAsset(want.Platform)
	dir, err := os.MkdirTemp(in.inv.getenv("TMPDIR"), "koko-kokod-") // mode 0700
	if err != nil {
		return "", nil, fmt.Errorf("make a directory to download kokod into: %w", err)
	}
	file, err = in.releases(k).Fetch(ctx, want.Version, name, dir, want)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, downloadFailed(err, fmt.Sprintf("; on a machine that cannot download it, as one offline, give %s of %s with --kokod <file>; nothing was written", name, want.Version))
	}
	return file, func() { _ = os.RemoveAll(dir) }, nil
}
