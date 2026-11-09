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
	"github.com/cieons/koko/internal/shared/install"
)

// The CLI's own error codes, beside the API's, which it passes through. Like
// the API's, they are only ever added within a koko major version.
const (
	// codeInvalidUsage: the command line was refused before any call (exit
	// 2). kokod reports it too, so it is install's.
	codeInvalidUsage = install.CodeInvalidUsage
	// codeNotJoined: this device has not joined a koko server, and none runs
	// on this machine; or the koko server it joined holds no Device Session
	// with its key.
	codeNotJoined = "not_joined"
	// codeServerUnreachable: the koko server did not answer.
	codeServerUnreachable = "server_unreachable"
	// codeHostKeyMismatch: the koko server presented a host key other than
	// the one pinned, from the Join Code or at the join; nothing was sent.
	codeHostKeyMismatch = "host_key_mismatch"
	// codeAlreadyJoined: koko join on a device that has joined a koko server.
	codeAlreadyJoined = "already_joined"
	// codeInvalidJoinCode: the koko server refused the Join Code: used,
	// expired, voided or never issued, which it does not say.
	codeInvalidJoinCode = "invalid_join_code"
	// codeUnsupportedPlatform: the command does not run on this OS, or on
	// this machine, such as the koko server on a network filesystem. kokod
	// reports it too, so it is install's.
	codeUnsupportedPlatform = install.CodeUnsupportedPlatform
	// codePortTaken: another process holds the port the command listens on,
	// such as koko server install's. kokod reports it too, also when
	// something answers on the socket kokod run would listen on, so it is
	// install's.
	codePortTaken = install.CodePortTaken
	// codeServerTooOld: the koko server does not serve the command's
	// operation, so it is older than the CLI, and nothing was sent; or it
	// answered without a field the command needs, as koko ssh setup needs
	// server.show's host_key.
	codeServerTooOld = "server_too_old"
)

// cliCodes lists the CLI's own codes, for `koko skill` and for the test that
// keeps them apart from the API's.
var cliCodes = []string{
	codeInvalidUsage,
	codeNotJoined,
	codeServerUnreachable,
	codeHostKeyMismatch,
	codeAlreadyJoined,
	codeInvalidJoinCode,
	codeUnsupportedPlatform,
	codePortTaken,
	codeServerTooOld,
}

// The exit statuses, part of the CLI's contract: new ones may be added, and
// these never change meaning.
const (
	exitOK      = 0
	exitFailed  = 1
	exitRefused = 2
	// exitRemote is every failure of koko's own in a command with
	// RemoteCommand, such as koko exec, whose other statuses are the remote
	// command's: 255, as ssh's.
	exitRemote = 255
)

// exitStatuses are the exit statuses with what each means, for koko skill;
// a command that adds one, such as koko exec's, adds its line.
var exitStatuses = []exitStatusJSON{
	{exitOK, "success"},
	{exitFailed, "the operation failed; the error's code says why"},
	{exitRefused, "the command line was refused before any call to the koko server, such as for an unknown flag, " +
		"a bad filter term, a flag before the command path, JSON on stdin that does not parse or has an unknown field, " +
		"or a secret input missing without a terminal"},
	{exitRemote, "koko exec only, whose other statuses are the remote command's: koko itself failed, its command line " +
		"included, or the remote command ended without an exit status, such as by a signal, as with ssh"},
}

// remoteStatus ends a command with RemoteCommand with its remote command's
// exit status, having printed nothing of its own.
type remoteStatus int

func (s remoteStatus) Error() string {
	return fmt.Sprintf("the remote command exited with status %d", int(s))
}

// remoteFailure is a failure of koko's own in a command with RemoteCommand:
// it is printed as any failure is, and exits 255.
type remoteFailure struct {
	err error
}

func (e *remoteFailure) Error() string { return e.err.Error() }

func (e *remoteFailure) Unwrap() error { return e.err }

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
	// items, for a batch read on stdin, holds each item at fault.
	items []api.ItemError
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
	if s, ok := errors.AsType[remoteStatus](err); ok {
		return int(s)
	}
	status, e := classify(err)
	if _, ok := errors.AsType[*remoteFailure](err); ok {
		// A command with RemoteCommand takes no --json: its failures are
		// lines.
		status, asJSON = exitRemote, false
	}
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
		return exitRefused, api.Error{Code: codeInvalidUsage, Message: e.message, Items: e.items}
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
// a newline, with the members of a map, such as a Host's Attributes, sorted
// by key, so the same record always prints the same.
func encodeJSON(w io.Writer, v any) error {
	b, err := json.Marshal(v, jsontext.WithIndent("  "), json.Deterministic(true))
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}
