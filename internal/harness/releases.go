package harness

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/shared/install"
)

// releasesPath is where GitHub serves koko's releases, each release's
// assets at <releasesPath>/download/<version>/<name>.
const releasesPath = "/cieons/koko/releases"

// Releases is a fake GitHub that serves one koko release's assets over
// HTTPS, as koko server install downloads them: an httptest TLS server that
// logs every request. Machine.Release starts one.
type Releases struct {
	srv     *httptest.Server
	version string

	mu     sync.Mutex
	assets map[string]http.Handler
	log    []string
}

// startReleases starts a fake GitHub serving the release version with the
// assets, by name, stopped when the test ends.
func startReleases(m *Machine, version string, assets map[string][]byte) *Releases {
	r := &Releases{version: version, assets: map[string]http.Handler{}}
	for name, content := range assets {
		r.Put(name, content)
	}
	r.srv = httptest.NewTLSServer(http.HandlerFunc(r.serve))
	m.t.Cleanup(r.srv.Close)
	return r
}

func (r *Releases) serve(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.log = append(r.log, req.URL.Path)
	name, ok := strings.CutPrefix(req.URL.Path, r.Path(""))
	h := r.assets[name]
	r.mu.Unlock()
	if !ok || h == nil || req.Method != http.MethodGet {
		http.NotFound(w, req)
		return
	}
	h.ServeHTTP(w, req)
}

// hook is the releases hook that points koko at r.
func (r *Releases) hook() cli.Releases {
	return cli.Releases{URL: r.srv.URL + releasesPath, Client: r.srv.Client()}
}

// URL is the HTTPS URL of the release's asset name, as koko names it.
func (r *Releases) URL(name string) string {
	return r.srv.URL + r.Path(name)
}

// Path is the request path of the release's asset name, as the log names
// it.
func (r *Releases) Path(name string) string {
	return releasesPath + "/download/" + r.version + "/" + name
}

// Put makes the release's asset name content, leaving SHA256SUMS as it is.
func (r *Releases) Put(name string, content []byte) {
	r.Handle(name, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
}

// Handle makes h answer every request for the release's asset name, such
// as a redirect or a 404.
func (r *Releases) Handle(name string, h http.Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.assets[name] = h
}

// Requests returns the path of every request made so far, in order.
func (r *Releases) Requests() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.log)
}

// Sums returns SHA256SUMS as a release lists the assets, by name: one
// `<hex>  <name>` line each, in the order given.
func Sums(names []string, assets map[string][]byte) []byte {
	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "%x  %s\n", sha256.Sum256(assets[name]), name)
	}
	return []byte(b.String())
}

// Release states that koko on m is the release version, of this binary's
// commit, and serves that release from a fake GitHub, which koko on m
// downloads from from now on. The release holds a kokod-linux-<arch> for
// each platform kokod is built for, of koko's build, the one for m's
// platform as m.Kokod() names it from now on, and SHA256SUMS listing them.
// It returns the fake GitHub.
func (m *Machine) Release(version string) *Releases {
	m.t.Helper()
	m.mu.Lock()
	m.self.Version = version
	m.mu.Unlock()
	assets := map[string][]byte{}
	var names []string
	for _, platform := range install.KokodPlatforms {
		b := m.kokodBuild()
		b.Platform = platform
		file := m.NewKokod(b)
		if platform == m.OSArch {
			m.kokod = file
		}
		content, err := os.ReadFile(file)
		if err != nil {
			m.t.Fatal(err)
		}
		name := install.KokodAsset(platform)
		assets[name] = content
		names = append(names, name)
	}
	assets["SHA256SUMS"] = Sums(names, assets)
	r := startReleases(m, version, assets)
	m.releases = r
	return r
}
