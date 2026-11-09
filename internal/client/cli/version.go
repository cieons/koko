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
	_, err := fmt.Fprintf(w, "koko %s\n", build.Info(v))
	return err
}

func version(context.Context, *Input) (versionJSON, error) {
	b := build.Read()
	return versionJSON(b), nil
}
