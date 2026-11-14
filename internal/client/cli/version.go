package cli

import (
	"context"
	_ "embed"
	"fmt"
	"io"

	"github.com/cieons/koko/internal/shared/build"
)

var versionCommands = []Command{
	{
		Verb:    "version",
		Summary: "Print this koko's build",
		Flags: []Flag{
			{Name: "licenses", Bool: true, Summary: "print the third-party software this koko includes, with its licences and notices"},
		},
		Examples: []string{
			"koko version",
			"koko version --licenses",
		},
		Tier: Read,
		Who:  Anyone,
		Run:  action(version),
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
	// Licenses is thirdPartyNotices, with --licenses.
	Licenses string `json:"licenses,omitempty"`
}

// thirdPartyNotices names every third-party component this koko carries,
// with its licence and notices; internal/notices writes it, and fails
// make check when it is out of date or a licence is not one koko accepts.
//
//go:embed THIRD_PARTY_NOTICES
var thirdPartyNotices string

func (v versionJSON) Human(w io.Writer) error {
	if v.Licenses != "" {
		_, err := io.WriteString(w, v.Licenses)
		return err
	}
	_, err := fmt.Fprintf(w, "koko %s\n", build.Info{Version: v.Version, Revision: v.Revision, Go: v.Go, Platform: v.Platform})
	return err
}

func version(_ context.Context, in *Input) (versionJSON, error) {
	b := build.Read()
	v := versionJSON{Version: b.Version, Revision: b.Revision, Go: b.Go, Platform: b.Platform}
	if in.Bool("licenses") {
		v.Licenses = thirdPartyNotices
	}
	return v, nil
}
