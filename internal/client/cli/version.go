package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/cieons/koko/internal/shared/build"
)

var versionCommands = []Command{
	{
		Verb:    "version",
		Summary: "Print this koko's build",
		Tier:    Read,
		Run:     action(version),
	},
}

// versionJSON is what version prints: this binary's build.
type versionJSON struct {
	// Version is the module version, such as v0.3.0, or "(devel)".
	Version string `json:"version"`
	// Revision is the commit it was built from, or empty when unknown.
	Revision string `json:"revision"`
	// Go is the Go release that built it.
	Go string `json:"go"`
	// Platform is GOOS/GOARCH, such as linux/amd64.
	Platform string `json:"platform"`
}

func (v versionJSON) Human(w io.Writer) error {
	rev := v.Revision
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if rev == "" {
		rev = "unknown revision"
	}
	_, err := fmt.Fprintf(w, "koko %s (%s, %s, %s)\n", v.Version, rev, v.Go, v.Platform)
	return err
}

func version(context.Context, *Input) (versionJSON, error) {
	b := build.Read()
	return versionJSON(b), nil
}
