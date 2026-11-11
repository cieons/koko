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
	"github.com/cieons/koko/internal/shared/release"
)

// releasesPath is where GitHub serves koko's releases: the latest's tag
// at <releasesPath>/latest, and each release's assets at
// <releasesPath>/download/<version>/<name>.
const releasesPath = "/cieons/koko/releases"

// KokoPlatforms are the platforms a release has a koko for, as the
// Makefile's RELEASE_TARGETS builds them.
var KokoPlatforms = []string{"linux/amd64", "linux/arm64", "darwin/arm64"}

// Releases is one release of a fake GitHub that serves koko's releases over
// HTTPS, as koko finds and downloads them: an httptest TLS server that logs
// every request. Machine.Release starts one, and Publish adds a release to
// it.
type Releases struct {
	gh      *github
	version string
}

// github is the fake GitHub every release of a machine's is served from.
type github struct {
	m   *Machine
	srv *httptest.Server

	mu sync.Mutex
	// assets holds each release's assets, by version and name, and
	// content what Put made each, by version/name.
	assets  map[string]map[string]http.Handler
	content map[string][]byte
	// latest is the version the releases/latest redirect names, or "" for
	// none.
	latest string
	log    []string
}

// startReleases starts a fake GitHub serving the release version with the
// assets, by name, stopped when the test ends.
func startReleases(m *Machine, version string, assets map[string][]byte) *Releases {
	gh := &github{m: m, assets: map[string]map[string]http.Handler{}, content: map[string][]byte{}}
	gh.srv = httptest.NewTLSServer(http.HandlerFunc(gh.serve))
	m.t.Cleanup(gh.srv.Close)
	r := &Releases{gh: gh, version: version}
	for name, content := range assets {
		r.Put(name, content)
	}
	return r
}

// serve answers a HEAD or GET of releases/latest with GitHub's redirect to
// the latest release's tag page, and a GET of an asset with the asset.
func (gh *github) serve(w http.ResponseWriter, req *http.Request) {
	gh.mu.Lock()
	gh.log = append(gh.log, req.URL.Path)
	latest := gh.latest
	var h http.Handler
	if rest, ok := strings.CutPrefix(req.URL.Path, releasesPath+"/download/"); ok && req.Method == http.MethodGet {
		if version, name, ok := strings.Cut(rest, "/"); ok {
			h = gh.assets[version][name]
		}
	}
	gh.mu.Unlock()
	switch {
	case req.URL.Path == releasesPath+"/latest" && latest != "":
		http.Redirect(w, req, gh.srv.URL+releasesPath+"/tag/"+latest, http.StatusFound)
	case h != nil:
		h.ServeHTTP(w, req)
	default:
		http.NotFound(w, req)
	}
}

// hook is the releases hook that points koko at r.
func (r *Releases) hook() cli.Releases {
	return cli.Releases{URL: r.gh.srv.URL + releasesPath, Client: r.gh.srv.Client()}
}

// URL is the HTTPS URL of the release's asset name, as koko names it.
func (r *Releases) URL(name string) string {
	return r.gh.srv.URL + r.Path(name)
}

// Path is the request path of the release's asset name, as the log names
// it.
func (r *Releases) Path(name string) string {
	return releasesPath + "/download/" + r.version + "/" + name
}

// LatestPath is the request path of the releases/latest redirect, as the
// log names it.
func LatestPath() string {
	return releasesPath + "/latest"
}

// Put makes the release's asset name content, leaving SHA256SUMS as it is.
func (r *Releases) Put(name string, content []byte) {
	r.gh.mu.Lock()
	r.gh.content[r.version+"/"+name] = content
	r.gh.mu.Unlock()
	r.Handle(name, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
}

// Content is what the release's asset name was last Put as.
func (r *Releases) Content(name string) []byte {
	r.gh.mu.Lock()
	defer r.gh.mu.Unlock()
	return r.gh.content[r.version+"/"+name]
}

// Handle makes h answer every request for the release's asset name, such
// as a redirect or a 404.
func (r *Releases) Handle(name string, h http.Handler) {
	r.gh.mu.Lock()
	defer r.gh.mu.Unlock()
	if r.gh.assets[r.version] == nil {
		r.gh.assets[r.version] = map[string]http.Handler{}
	}
	r.gh.assets[r.version][name] = h
}

// Latest makes the releases/latest redirect name this release, as GitHub's
// does for the newest release that is no pre-release.
func (r *Releases) Latest() {
	r.gh.mu.Lock()
	defer r.gh.mu.Unlock()
	r.gh.latest = r.version
}

// Requests returns the path of every request made to the fake GitHub so
// far, for any release, in order.
func (r *Releases) Requests() []string {
	r.gh.mu.Lock()
	defer r.gh.mu.Unlock()
	return slices.Clone(r.gh.log)
}

// Publish adds the release version to the fake GitHub r is a release of,
// and returns it: a koko-<os>-<arch> for each of KokoPlatforms, of koko's
// main package at that version and of another commit, files the machine
// states the build of, and SHA256SUMS listing them. The releases/latest
// redirect names it from now on.
func (r *Releases) Publish(version string) *Releases {
	m := r.gh.m
	m.t.Helper()
	p := &Releases{gh: r.gh, version: version}
	assets := map[string][]byte{}
	var names []string
	for _, platform := range KokoPlatforms {
		b := m.kokodBuild()
		b.Path, b.Version, b.Platform = release.KokoPackage, version, platform
		b.Revision = fmt.Sprintf("%x", sha256.Sum256([]byte(version)))[:40]
		content, err := os.ReadFile(m.NewKokod(b))
		if err != nil {
			m.t.Fatal(err)
		}
		name := release.KokoAsset(platform)
		assets[name] = content
		names = append(names, name)
	}
	for name, content := range assets {
		p.Put(name, content)
	}
	p.Put("SHA256SUMS", Sums(names, assets))
	p.Latest()
	return p
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
// It returns the fake GitHub's release; its releases/latest redirect names
// none until a test says so.
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
