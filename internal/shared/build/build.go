// Package build reports which build of koko this binary is, for `koko
// version` and for the koko server's own report.
package build

import (
	"runtime"
	"runtime/debug"
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
}

// Read returns this binary's build.
func Read() Info {
	info := Info{Version: "unknown", Go: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	info.Version = bi.Main.Version
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" {
			info.Revision = s.Value
		}
	}
	return info
}
