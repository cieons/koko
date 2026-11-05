package cli

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/cieons/koko/internal/shared/api"
)

// The CLI's own error codes, beside the API's, which it passes through. Like
// the API's, they are only ever added within a koko major version.
const (
	// codeInvalidUsage: the command line was refused before any call (exit 2).
	codeInvalidUsage = "invalid_usage"
	// codeNotJoined: this device has not joined a koko server, and none runs
	// on this machine.
	codeNotJoined = "not_joined"
	// codeServerUnreachable: the koko server did not answer.
	codeServerUnreachable = "server_unreachable"
	// codeUnsupportedPlatform: the command does not run on this OS.
	codeUnsupportedPlatform = "unsupported_platform"
	// codePortTaken: another process holds the port the command listens on.
	codePortTaken = "port_taken"
)

// cliCodes lists the CLI's own codes, for `koko skill` and for the test that
// keeps them apart from the API's.
var cliCodes = []string{
	codeInvalidUsage,
	codeNotJoined,
	codeServerUnreachable,
	codeUnsupportedPlatform,
	codePortTaken,
}

// The exit statuses, part of the CLI's contract: new ones may be added, and
// these never change meaning.
const (
	exitOK      = 0
	exitFailed  = 1
	exitRefused = 2
)

// cliError is a failure the CLI reports with one of its own codes.
type cliError struct {
	code    string
	message string
}

func (e *cliError) Error() string {
	return e.message
}

// usageError is a command line refused before any call.
type usageError struct {
	message string
}

func (e *usageError) Error() string {
	return e.message
}

func usagef(format string, args ...any) error {
	return &usageError{message: fmt.Sprintf(format, args...)}
}

// wantsJSON tells whether a command line asked for --json anywhere, even in
// a form the parser refuses, so that a refused command line still gets its
// error as an object.
func wantsJSON(args []string) bool {
	return slices.ContainsFunc(args, func(a string) bool {
		return a == "--json" || strings.HasPrefix(a, "--json=")
	})
}

// writeResult prints a command's result to stdout, rendered in full first so
// that a failure leaves stdout empty.
func writeResult(stdout io.Writer, res Result, asJSON bool) error {
	var buf bytes.Buffer
	if asJSON {
		if err := encodeJSON(&buf, res); err != nil {
			return fmt.Errorf("encode the result: %w", err)
		}
	} else if err := res.Human(&buf); err != nil {
		return fmt.Errorf("write the result: %w", err)
	}
	_, err := stdout.Write(buf.Bytes())
	return err
}

// writeError prints err to stderr and returns the exit status it means.
func writeError(stderr io.Writer, err error, asJSON bool) int {
	status, e := classify(err)
	if asJSON {
		var buf bytes.Buffer
		_ = encodeJSON(&buf, api.ErrorResponse{Error: e})
		_, _ = stderr.Write(buf.Bytes())
	} else {
		_, _ = fmt.Fprintf(stderr, "koko: %s\n", e.Message)
	}
	return status
}

// classify gives an error its code and exit status: a refused command line
// exits 2, every other failure 1.
func classify(err error) (int, api.Error) {
	if e, ok := errors.AsType[*usageError](err); ok {
		return exitRefused, api.Error{Code: codeInvalidUsage, Message: e.message}
	}
	if e, ok := errors.AsType[*cliError](err); ok {
		return exitFailed, api.Error{Code: e.code, Message: e.message}
	}
	if e, ok := errors.AsType[*api.Error](err); ok {
		return exitFailed, *e
	}
	return exitFailed, api.Error{Code: api.CodeInternal.Name, Message: err.Error()}
}

// encodeJSON writes v as the CLI's --json: indented by two spaces, ending in
// a newline.
func encodeJSON(w io.Writer, v any) error {
	b, err := json.Marshal(v, jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}
