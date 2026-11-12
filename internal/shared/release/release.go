// Package release finds a koko release, downloads one of its assets and
// checks it, for every program that installs one: `koko server install`,
// which downloads the kokod of its own release, and `koko update`, which
// downloads the koko of the latest release or of the one it is given (ADR
// 0026, ADR 0041, ADR 0047). It reads the `releases/latest` redirect as
// install.sh does, downloads over HTTPS only, within bounds of size and
// time, and checks an asset against the release's SHA256SUMS and its Go
// build information before anyone runs it.
package release

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/cieons/koko/internal/shared/build"
)

// GitHub is where koko's releases are (ADR 0026): the latest's tag at
// <GitHub>/latest, and each release's assets at
// <GitHub>/download/<tag>/<name>.
const GitHub = "https://github.com/cieons/koko/releases"

// InstallLine installs the latest koko for the user who runs it: the way
// up for a koko before v0.4.0, which has no koko update.
const InstallLine = "curl -fsSL " + GitHub + "/latest/download/install.sh | sh"

// KokoPackage is koko's main package, which a koko file's Go build
// information names, so that koko update tells koko from another program,
// such as kokod, before it installs it.
const KokoPackage = "github.com/cieons/koko/cmd/koko"

// KokoAsset is the name of a release's asset that holds the koko for
// platform, such as koko-linux-amd64, which koko update downloads (ADR
// 0047): koko-<os>-<arch>, with .exe on Windows, which runs a file by its
// extension (ADR 0052).
func KokoAsset(platform string) string {
	name := "koko-" + strings.ReplaceAll(platform, "/", "-")
	if strings.HasPrefix(platform, "windows/") {
		name += ".exe"
	}
	return name
}

// The bounds of a download: a koko or kokod is about 40 MiB, SHA256SUMS a
// few lines.
const (
	// fetchTime bounds a whole download, SHA256SUMS and the asset: 10
	// minutes takes 40 MiB over a link of about 0.6 Mbit/s.
	fetchTime = 10 * time.Minute
	// latestTime bounds finding the latest release, one request.
	latestTime = time.Minute
	// sumsLimit and assetLimit bound the size of each file.
	sumsLimit  = 1 << 20
	assetLimit = 512 << 20
	// redirectLimit bounds the redirects a request follows, as Go's default
	// policy does.
	redirectLimit = 10
)

// Source is where koko finds its releases, and how it reads a downloaded
// file's build.
type Source struct {
	// URL is the HTTPS URL of koko's releases, as GitHub is; empty means
	// GitHub. Only a test's releases hook moves it (ADR 0041).
	URL string
	// Client makes the requests, under koko's own rules for redirects and
	// time; nil means one with Go's default transport.
	Client *http.Client
	// Build reads the build of a file without running it; nil means from its
	// Go build information.
	Build func(file string) (build.Info, error)
}

// Error is a release koko could not find, an asset it could not download, or
// one that failed a check: never a failure of this machine, such as a full
// disk. Its message names the URL.
type Error struct {
	message string
}

func (e *Error) Error() string { return e.message }

func failf(format string, args ...any) *Error {
	return &Error{message: fmt.Sprintf(format, args...)}
}

// Latest returns the tag of koko's latest release: the one the
// `releases/latest` redirect names, read as install.sh reads it, with one
// request whose redirect it does not follow. A pre-release is never the
// latest.
func (s Source) Latest(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, latestTime)
	defer cancel()
	latest := s.url() + "/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, latest, nil)
	if err != nil {
		return "", fmt.Errorf("find the latest release at %s: %w", latest, err)
	}
	if err := httpsOnly(req.URL); err != nil {
		return "", failf("koko could not find its latest release: head %s: %v", latest, err)
	}
	client := s.client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return "", failf("koko could not find its latest release: head %s: %v", latest, err)
	}
	_ = resp.Body.Close()
	where, err := resp.Location()
	if resp.StatusCode/100 != 3 || err != nil {
		return "", failf("koko could not find its latest release: %s answers %s, not a redirect to the release", latest, resp.Status)
	}
	dir, tag := path.Split(where.Path)
	if !strings.HasSuffix(dir, "/releases/tag/") || !(build.Info{Version: tag}).Release() {
		return "", failf("koko could not find its latest release: %s redirects to %s, which names no release", latest, where.Redacted())
	}
	return tag, nil
}

// Want is the build a file must be: its main package, version and
// platform, and, when it is the caller's own build, its commit too.
type Want struct {
	build.Info
	// Own, when set, is the caller's own build, whose version and commit
	// the file must be of, as koko server install runs only the kokod of
	// its own build (ADR 0041).
	Own *build.Info
}

// Fetch downloads the asset of the release tag into dir, a directory only
// this user can write, over HTTPS only, and checks it: its SHA-256 against
// the one the release's SHA256SUMS lists, then its build against want. It
// returns the file's path, mode 0700. A download or a check that
// fails is an *Error, and leaves the file behind for the caller to remove
// with dir.
func (s Source) Fetch(ctx context.Context, tag, asset, dir string, want Want) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTime)
	defer cancel()
	base := s.url() + "/download/" + tag + "/"
	client := s.client()

	var sums bytes.Buffer
	if err := fetch(ctx, client, base+"SHA256SUMS", sumsLimit, &sums); err != nil {
		return "", failf("koko could not download %s of %s: %v", asset, tag, err)
	}
	sum, ok := listed(sums.String(), asset)
	if !ok {
		return "", failf("%sSHA256SUMS lists no %s, and koko takes only an asset its release lists", base, asset)
	}

	file := filepath.Join(dir, asset)
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return "", fmt.Errorf("make the file to download %s into: %w", asset, err)
	}
	h := sha256.New()
	err = fetch(ctx, client, base+asset, assetLimit, io.MultiWriter(f, h))
	if cerr := f.Close(); err == nil && cerr != nil {
		return "", fmt.Errorf("write %s: %w", file, cerr)
	}
	switch _, local := errors.AsType[*fs.PathError](err); {
	case local:
		// The file could not be written, as on a full disk: the download
		// itself did not fail.
		return "", fmt.Errorf("write %s: %w", file, err)
	case err != nil:
		return "", failf("koko could not download %s of %s: %v", asset, tag, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sum {
		return "", failf("%s%s has the SHA-256 %s, and the release's SHA256SUMS lists %s, so koko does not run it", base, asset, got, sum)
	}
	b, err := s.read(file)
	if problem := Mismatch(file, b, err, want); problem != "" {
		return "", failf("%s%s matches the release's SHA256SUMS, and it is %s", base, asset, problem)
	}
	return file, nil
}

// Mismatch says why the file at file, whose build reading it gave as b or
// err, is not the build want, as what the file is, or returns "" when it
// is.
func Mismatch(file string, b build.Info, err error, want Want) string {
	program := path.Base(want.Path)
	switch {
	case err != nil:
		return fmt.Sprintf("%s, which is not %s: %v", file, program, err)
	case b.Path != want.Path:
		return fmt.Sprintf("%s, which is %s, not %s (%s)", file, b.Path, program, want.Path)
	case want.Own != nil && !b.Same(*want.Own):
		return fmt.Sprintf("%s %s, and this koko is %s: koko runs only the %s of its own build", program, b, *want.Own, program)
	case b.Version != want.Version:
		return fmt.Sprintf("%s %s, not %s", program, b, want.Version)
	case b.Platform != want.Platform:
		return fmt.Sprintf("%s for %s, and this machine is %s", program, b.Platform, want.Platform)
	}
	return ""
}

func (s Source) url() string {
	return strings.TrimSuffix(cmp.Or(s.URL, GitHub), "/")
}

// read reads the build of file.
func (s Source) read(file string) (build.Info, error) {
	if s.Build != nil {
		return s.Build(file)
	}
	bi, err := buildinfo.ReadFile(file)
	if err != nil {
		return build.Info{}, err
	}
	return build.Of(bi), nil
}

// client returns a copy of the source's client, or of one with Go's default
// transport, which follows redirects only to HTTPS.
func (s Source) client() *http.Client {
	var c http.Client
	if s.Client != nil {
		c = *s.Client
	}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= redirectLimit {
			return fmt.Errorf("stopped after %d redirects", redirectLimit)
		}
		return httpsOnly(req.URL)
	}
	return &c
}

// httpsOnly refuses a URL that is not HTTPS.
func httpsOnly(u *url.URL) error {
	if u.Scheme != "https" {
		return fmt.Errorf("%s is not HTTPS, which koko downloads over only", u.Redacted())
	}
	return nil
}

// fetch writes what the HTTPS GET of rawURL answers to w, failing for an
// answer other than 200 OK or one longer than limit bytes. Its error names
// rawURL.
func fetch(ctx context.Context, client *http.Client, rawURL string, limit int64, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("get %s: %w", rawURL, err)
	}
	if err := httpsOnly(req.URL); err != nil {
		return fmt.Errorf("get %s: %w", rawURL, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		// What failed, without the URL Go names, which may be a redirect's.
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return fmt.Errorf("get %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("get %s: the answer is %s", rawURL, resp.Status)
	}
	if resp.ContentLength > limit {
		return fmt.Errorf("get %s: it is %d bytes, over the %d koko takes", rawURL, resp.ContentLength, limit)
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, limit+1))
	switch {
	case err != nil:
		return fmt.Errorf("get %s: %w", rawURL, err)
	case n > limit:
		return fmt.Errorf("get %s: it is over the %d bytes koko takes", rawURL, limit)
	}
	return nil
}

// listed returns the SHA-256 that sums, as sha256sum writes it, lists for
// name, as it lists it.
func listed(sums, name string) (string, bool) {
	for line := range strings.Lines(sums) {
		// sha256sum marks a file it read in binary mode with a *.
		if f := strings.Fields(line); len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), true
		}
	}
	return "", false
}
