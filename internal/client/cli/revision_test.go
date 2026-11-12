package cli

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cieons/koko/internal/client/devicefiles"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/wire"
)

// revisedServer serves on a socket of its own a koko server that sends
// report, the real transport in front of a handler that answers every call
// with an empty object. It returns the socket's path and a count of the
// calls it answered.
func revisedServer(t *testing.T, report wire.Report) (string, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	})
	// A socket's path must be short: t.TempDir's can pass the limit.
	dir, err := os.MkdirTemp("", "koko")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "koko.sock")
	s := transport.New(h, h, func() wire.Report { return report }, slog.New(slog.DiscardHandler))
	if err := s.ListenSocket(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return path, &calls
}

// reaching returns the Input of the command line argv, reaching the koko
// server on the socket at path.
func reaching(t *testing.T, path string, argv ...string) *Input {
	t.Helper()
	in := inputFor(t, Tree(), argv...)
	in.runner.SocketPath = path
	t.Cleanup(in.close)
	return in
}

// A request field added at a revision of its operation above the koko
// server's is never sent: the call fails with server_too_old, naming the
// option that set it, the command and the koko server's version. A report
// that lists no revision of the operation, or none at all, as a koko
// server from before revisions sends, is at revision 1. At the field's
// revision, and with the field unset, the call is sent.
func TestAFieldTheServersRevisionLacksIsServerTooOld(t *testing.T) {
	t.Parallel()
	hostAdd := api.HostAdd
	hostAdd.Revision, hostAdd.Added = 2, map[string]int{"hosts[].relay": 2}
	hostList := api.HostList
	hostList.Revision, hostList.Added = 2, map[string]int{"with_note": 2}
	relayed := api.HostAddRequest{Hosts: []api.NewHost{{Name: "web1", Address: "192.0.2.10", Account: "root", Relay: new("gw")}}}
	direct := api.HostAddRequest{Hosts: []api.NewHost{{Name: "web1", Address: "192.0.2.10", Account: "root"}}}
	hostAddLine := []string{"host", "add", "web1", "address=192.0.2.10", "account=root", "relay=gw"}

	for _, tc := range []struct {
		name      string
		revisions map[string]int
		call      func(in *Input) error
		argv      []string
		// option is the option the refusal names, "" for a call sent.
		option string
	}{
		{"no revisions", nil, func(in *Input) error { _, err := in.Call(t.Context(), hostAdd, relayed); return err }, hostAddLine, "relay="},
		{"revision 1", map[string]int{"host.add": 1}, func(in *Input) error { _, err := in.Call(t.Context(), hostAdd, relayed); return err }, hostAddLine, "relay="},
		{"a flag", nil, func(in *Input) error {
			_, err := in.Call(t.Context(), hostList, api.HostListRequest{WithNote: true})
			return err
		}, []string{"host", "ls", "--with-note"}, "--with-note"},
		{"revision 2", map[string]int{"host.add": 2}, func(in *Input) error { _, err := in.Call(t.Context(), hostAdd, relayed); return err }, hostAddLine, ""},
		{"unset", nil, func(in *Input) error { _, err := in.Call(t.Context(), hostAdd, direct); return err }, hostAddLine[:5], ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path, calls := revisedServer(t, wire.Report{Version: "v0.4.0", Operations: []string{"host.add", "host.list"}, Revisions: tc.revisions})
			err := tc.call(reaching(t, path, tc.argv...))
			if tc.option == "" {
				if err != nil || calls.Load() != 1 {
					t.Fatalf("the call failed with %v after %d call(s) answered, want it sent and answered", err, calls.Load())
				}
				return
			}
			e, ok := errors.AsType[*cliError](err)
			if !ok || e.code != codeServerTooOld {
				t.Fatalf("the call failed with %v, want server_too_old", err)
			}
			cmd := "koko " + strings.Join(tc.argv[:2], " ")
			for _, want := range []string{"v0.4.0", "does not take " + tc.option + " of " + cmd, "upgrade the koko server"} {
				if !strings.Contains(e.message, want) {
					t.Errorf("the message %q does not say %q", e.message, want)
				}
			}
			if n := calls.Load(); n != 0 {
				t.Errorf("the koko server answered %d call(s), want none", n)
			}
		})
	}
}

// A batch goes only to a koko server whose revision of the operation takes
// it: to one at revision 1, several names fail with server_too_old, naming
// the several records, with nothing sent, while one name goes in the
// one-record form, which every koko server takes (ADR 0050).
func TestABatchToAKokoServerWithoutBatchesIsServerTooOld(t *testing.T) {
	t.Parallel()
	ops := []string{"host.set", "host.move", "host.retire", "host.remove", "secret.move", "secret.remove", "host.list"}
	for _, tc := range []struct {
		several, one []string
		option       string
	}{
		{[]string{"host", "set", "web1", "web2", "role=node"}, []string{"host", "set", "web1", "role=node"}, "several <host>"},
		{[]string{"host", "move", "web1", "web2", "--to", "ops"}, []string{"host", "move", "web1", "ops"}, "several <host>"},
		{[]string{"host", "retire", "web1", "web2"}, []string{"host", "retire", "web1"}, "several <host>"},
		{[]string{"host", "rm", "web1", "web2"}, []string{"host", "rm", "web1"}, "several <host>"},
		{[]string{"secret", "move", "ops/a", "ops/b", "--to", "web"}, []string{"secret", "move", "ops/a", "web"}, "several <space>/<secret>"},
		{[]string{"secret", "rm", "ops/a", "ops/b"}, []string{"secret", "rm", "ops/a"}, "several <space>/<secret>"},
	} {
		t.Run(strings.Join(tc.several[:2], " "), func(t *testing.T) {
			t.Parallel()
			path, calls := revisedServer(t, wire.Report{Version: "v0.4.0", Operations: ops})
			run := func(argv []string) (int, string) {
				var stderr strings.Builder
				r := &Runner{SocketPath: path}
				status := r.Run(t.Context(), Invocation{Args: argv, Stdout: io.Discard, Stderr: &stderr, Env: []string{devicefiles.HomeVar + "=" + t.TempDir()}})
				return status, stderr.String()
			}
			status, stderr := run(append(tc.several, "--json"))
			cmd := "koko " + strings.Join(tc.several[:2], " ")
			if status != 1 || !strings.Contains(stderr, `"server_too_old"`) || !strings.Contains(stderr, "does not take "+tc.option+" of "+cmd) {
				t.Errorf("%s: exit %d, stderr %q; want 1 and server_too_old naming %s", tc.several, status, stderr, tc.option)
			}
			if n := calls.Load(); n != 0 {
				t.Errorf("%s sent %d call(s), want none", tc.several, n)
			}
			if status, stderr := run(tc.one); status != 0 || calls.Load() != 1 {
				t.Errorf("%s: exit %d, stderr %q, %d call(s); want it sent", tc.one, status, stderr, calls.Load())
			}
		})
	}
}
