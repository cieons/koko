package cli

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cieons/koko/internal/shared/install"
)

// Releases is where koko downloads the assets of its release from: the
// releases hook (Runner.Releases).
type Releases struct {
	// URL is the HTTPS URL below which each release's assets are, as
	// <URL>/<version>/<name>; empty means GitHub's, releasesURL.
	URL string
	// Client makes the requests, under koko's own rules for redirects and
	// time; nil means one with Go's default transport.
	Client *http.Client
}

// releasesURL is where GitHub serves koko's release assets (ADR 0026, ADR
// 0041).
const releasesURL = "https://github.com/cieons/koko/releases/download"

// The download's bounds: a kokod is about 40 MiB, and SHA256SUMS a few
// lines.
const (
	// downloadTime bounds the whole download, SHA256SUMS and kokod: 10
	// minutes takes 40 MiB over a link of about 0.6 Mbit/s.
	downloadTime = 10 * time.Minute
	// sumsLimit and kokodLimit bound the size of each file.
	sumsLimit  = 1 << 20
	kokodLimit = 512 << 20
	// redirectLimit bounds the redirects a request follows, as Go's default
	// policy does.
	redirectLimit = 10
)

// downloadKokod downloads the kokod of koko's release for the machine's
// platform from in's releases into a new 0700 directory under TMPDIR, over
// HTTPS only, and checks it against the release's SHA256SUMS and its build
// against koko's. It returns the file's path and a function that removes the
// directory; on a failure, it has removed it already.
func downloadKokod(ctx context.Context, in *Input, k Kokod) (file string, done func(), err error) {
	ctx, cancel := context.WithTimeout(ctx, downloadTime)
	defer cancel()
	version := k.Self().Version
	base := cmp.Or(in.runner.Releases.URL, releasesURL) + "/" + version + "/"
	name := install.KokodAsset(k.Platform())
	fail := func(format string, args ...any) error {
		return &cliError{code: codeDownloadFailed, message: fmt.Sprintf(format, args...) +
			fmt.Sprintf("; on a machine that cannot download it, as one offline, give %s of %s with --kokod <file>; nothing was written", name, version)}
	}
	client := downloadClient(in.runner.Releases.Client)

	var sums bytes.Buffer
	if err := fetch(ctx, client, base+"SHA256SUMS", sumsLimit, &sums); err != nil {
		return "", nil, fail("koko could not download the kokod of its release: %v", err)
	}
	want, ok := listed(sums.String(), name)
	if !ok {
		return "", nil, fail("%sSHA256SUMS lists no %s, and koko runs only a kokod its release lists", base, name)
	}

	dir, err := os.MkdirTemp(in.inv.getenv("TMPDIR"), "koko-kokod-") // mode 0700
	if err != nil {
		return "", nil, fmt.Errorf("make a directory to download kokod into: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	file = filepath.Join(dir, name)
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return "", nil, fmt.Errorf("make the file to download kokod into: %w", err)
	}
	h := sha256.New()
	err = fetch(ctx, client, base+name, kokodLimit, io.MultiWriter(f, h))
	if cerr := f.Close(); err == nil && cerr != nil {
		return "", nil, fmt.Errorf("write %s: %w", file, cerr)
	}
	switch _, local := errors.AsType[*fs.PathError](err); {
	case local:
		// The file could not be written, as on a full disk: the download
		// itself did not fail.
		return "", nil, fmt.Errorf("write %s: %w", file, err)
	case err != nil:
		return "", nil, fail("koko could not download the kokod of its release: %v", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return "", nil, fail("%s%s has the SHA-256 %s, and the release's SHA256SUMS lists %s, so koko does not run it", base, name, got, want)
	}
	if problem := kokodProblem(k, file); problem != "" {
		return "", nil, fail("%s%s matches the release's SHA256SUMS, and it is %s", base, name, problem)
	}
	return file, func() { _ = os.RemoveAll(dir) }, nil
}

// downloadClient returns a copy of c, or of a client with Go's default
// transport when c is nil, which follows redirects only to HTTPS.
func downloadClient(c *http.Client) *http.Client {
	var d http.Client
	if c != nil {
		d = *c
	}
	d.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= redirectLimit {
			return fmt.Errorf("stopped after %d redirects", redirectLimit)
		}
		return httpsOnly(req.URL)
	}
	return &d
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
