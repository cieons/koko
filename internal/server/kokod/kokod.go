// Package kokod is kokod's command line: kokod is the program that is the
// koko server, on Linux, as koko is the CLI (ADR 0040). It takes arguments,
// an environment and stdio, and gives an exit status, and hides its parser,
// its flags' variables, its JSON and its refusals' codes.
//
// kokod may not use the CLI's parser, which is in the client group, so it
// has a small one of its own, whose refusals read as the CLI's do. Its
// errors have the shape of koko's --json errors ({"error": {"code",
// "message"}}, ADR 0016) and its exit statuses: 2 for a refused command
// line, 1 for every other failure.
package kokod

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/cieons/koko/internal/server"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/install"
)

// Program runs kokod command lines.
type Program struct {
	// Serve runs the koko server with config until ctx ends, for kokod run;
	// config holds what the command line gives. cmd/kokod sets it to
	// server.Run on this machine's mounts and process uids; the harness
	// (harness.Kokod) starts a koko server in the test process.
	Serve func(ctx context.Context, config server.Config) error
}

// Invocation is one command line with its streams and environment.
type Invocation struct {
	// Args is argv without the program name.
	Args   []string
	Stdout io.Writer
	Stderr io.Writer
	// Env is the environment as KEY=value strings.
	Env []string
}

// getenv returns the value of the last KEY=value in the environment.
func (inv *Invocation) getenv(key string) string {
	var value string
	for _, kv := range inv.Env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			value = v
		}
	}
	return value
}

// The exit statuses, as koko's: 0 on success, 1 when the command failed, 2
// when its command line was refused before anything ran.
const (
	exitOK      = 0
	exitFailed  = 1
	exitRefused = 2
)

// Run runs one command line and returns its exit status. On failure stdout
// stays empty and the error goes to stderr, as an error object when --json
// was given.
func (p *Program) Run(ctx context.Context, inv Invocation) int {
	line, err := parse(inv.Args, inv.getenv)
	if err != nil {
		return writeError(inv.Stderr, err, wantsJSON(inv.Args))
	}
	if line.help {
		var b strings.Builder
		if line.cmd == nil {
			writeHelp(&b)
		} else {
			line.cmd.writeHelp(&b)
		}
		_, _ = io.WriteString(inv.Stdout, b.String())
		return exitOK
	}
	if err := line.cmd.run(ctx, p, &inv, line); err != nil {
		return writeError(inv.Stderr, err, line.json)
	}
	return exitOK
}

// wantsJSON tells whether argv asks for --json, for a refusal of the command
// line before it is parsed.
func wantsJSON(args []string) bool {
	return slices.ContainsFunc(args, func(a string) bool {
		return a == "--json" || strings.HasPrefix(a, "--json=")
	})
}

// writeError writes err to stderr, as an error object when asJSON, and
// returns the exit status it calls for.
func writeError(stderr io.Writer, err error, asJSON bool) int {
	status, e := classify(err)
	if asJSON {
		var buf bytes.Buffer
		_ = encodeJSON(&buf, api.ErrorResponse{Error: e})
		_, _ = stderr.Write(buf.Bytes())
	} else {
		_, _ = fmt.Fprintf(stderr, "kokod: %s\n", e.Message)
	}
	return status
}

// classify gives an error its code and exit status, as koko gives the same
// failures: a refused command line exits 2, every other failure 1.
func classify(err error) (int, api.Error) {
	if r, ok := errors.AsType[*install.Refusal](err); ok {
		status := exitFailed
		if r.Reason == install.Usage {
			status = exitRefused
		}
		return status, api.Error{Code: r.Code(), Message: r.Message}
	}
	if e, ok := errors.AsType[*api.Error](err); ok {
		return exitFailed, *e
	}
	return exitFailed, api.Error{Code: api.CodeInternal.Name, Message: err.Error()}
}

// encodeJSON writes v as koko's --json does: indented by two spaces, ending
// in a newline.
func encodeJSON(w io.Writer, v any) error {
	b, err := json.Marshal(v, jsontext.WithIndent("  "), json.Deterministic(true))
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// usagef is a refusal of the command line, which exits 2.
func usagef(format string, args ...any) error {
	return &install.Refusal{Reason: install.Usage, Message: fmt.Sprintf(format, args...)}
}
