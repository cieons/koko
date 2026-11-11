package cli

import (
	"fmt"

	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/build"
	"github.com/cieons/koko/internal/shared/compat"
	"github.com/cieons/koko/internal/shared/wire"
)

// self is this koko's build: the machine's (Kokod.Self), which the S1
// harness may state as a release, and otherwise this binary's.
func (r *Runner) self() build.Info {
	if r.Kokod != nil {
		return r.Kokod.Self()
	}
	return build.Read()
}

// outsideWindow says that the koko server and this CLI do not work
// together (ADR 0049): this CLI is below the oldest koko the koko server
// works with (cli_too_old), or the koko server below compat.Oldest
// (server_too_old). The command sent nothing.
type outsideWindow struct {
	err *cliError
	// report is the koko server's report, which koko status still shows.
	report wire.Report
}

func (e *outsideWindow) Error() string { return e.err.Error() }

func (e *outsideWindow) Unwrap() error { return e.err }

// window checks, from the koko server's report, that it and this CLI work
// together, and fails with an *outsideWindow otherwise.
func (in *Input) window(report wire.Report) error {
	self := in.runner.self().Version
	switch {
	case report.Oldest != "" && compat.Newer(report.Oldest, self):
		return &outsideWindow{report: report, err: &cliError{code: api.CodeCLITooOld.Name, message: fmt.Sprintf(
			"this koko is %s, older than %s, the oldest koko the koko server %s works with: run koko update",
			self, report.Oldest, in.where())}}
	case compat.Newer(compat.Oldest, report.Version):
		return &outsideWindow{report: report, err: &cliError{code: codeServerTooOld, message: fmt.Sprintf(
			"the koko server %s runs koko %s, older than %s, the oldest koko this koko works with: upgrade the koko server",
			in.where(), report.Version, compat.Oldest)}}
	}
	return nil
}

// where names the koko server the command reached, for a message: at the
// Server Address this device joined, or on this machine.
func (in *Input) where() string {
	if in.joined != nil {
		return "at " + in.joined.ServerAddress
	}
	return "on this machine"
}
