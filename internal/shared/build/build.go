// Package build reports which build of koko a binary is: this one, for `koko
// version`, `kokod version` and the koko server's own report, and a kokod
// file's, which koko server install reads before it runs it.
package build

import (
	"fmt"
	"regexp"
	"runtime"
	"runtime/debug"
	"slices"
)

// Info identifies a build.
type Info struct {
	// Version is the module version the Go toolchain stamped, such as v0.3.0,
	// a pseudo-version for a build from a commit, or "(devel)".
	Version string
	// Revision is the VCS commit the binary was built from, or empty when the
	// toolchain stamped none.
	Revision string
	// Go is the Go release that built the binary.
	Go string
	// Platform is GOOS/GOARCH, such as linux/amd64.
	Platform string
	// Path is the main package's import path, such as
	// github.com/cieons/koko/cmd/kokod, which tells the programs apart.
	Path string
}

// Read returns this binary's build.
func Read() Info {
	info := Info{Version: "unknown", Go: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	info.Version, info.Revision, info.Path = bi.Main.Version, setting(bi, "vcs.revision"), bi.Path
	return info
}

// Of returns the build that a binary's Go build information bi names, as
// debug/buildinfo reads it from a file without running it.
func Of(bi *debug.BuildInfo) Info {
	return Info{
		Version:  bi.Main.Version,
		Revision: setting(bi, "vcs.revision"),
		Go:       bi.GoVersion,
		Platform: setting(bi, "GOOS") + "/" + setting(bi, "GOARCH"),
		Path:     bi.Path,
	}
}

// setting returns the value of bi's build setting key, or "".
func setting(bi *debug.BuildInfo, key string) string {
	i := slices.IndexFunc(bi.Settings, func(s debug.BuildSetting) bool { return s.Key == key })
	if i < 0 {
		return ""
	}
	return bi.Settings[i].Value
}

// Same tells whether i and o are one build: the same module version, which
// says +dirty for a build from changed files, and the same commit.
func (i Info) Same(o Info) bool {
	return i.Version == o.Version && i.Revision == o.Revision
}

// releaseVersion matches a release's version, the tag it was built at: a
// semantic version with an optional pre-release part, such as v0.3.0 or
// v0.3.0-rc.1, and no build metadata, such as Go's +dirty.
var releaseVersion = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$`)

// pseudoVersion matches the end of a pseudo-version, which Go stamps on a
// build from a commit no tag names: a 14-digit time and the commit, as in
// v0.3.1-0.20261109080000-0123456789ab.
var pseudoVersion = regexp.MustCompile(`[-.][0-9]{14}-[0-9A-Za-z]+$`)

// Release tells whether i is a release's build, built at its tag with
// nothing changed, so that the release's other assets are of its build: not
// a pseudo-version, not +dirty and not (devel).
func (i Info) Release() bool {
	return releaseVersion.MatchString(i.Version) && !pseudoVersion.MatchString(i.Version)
}

// String is the build as `koko version` and `kokod version` print it after
// the program's name: the version, then the commit's first 12 characters,
// the Go release and the platform, such as
// v0.3.0 (0123456789ab, go1.27.3, linux/amd64).
func (i Info) String() string {
	rev := i.Revision
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if rev == "" {
		rev = "unknown revision"
	}
	return fmt.Sprintf("%s (%s, %s, %s)", i.Version, rev, i.Go, i.Platform)
}
