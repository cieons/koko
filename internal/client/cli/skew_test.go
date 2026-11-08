package cli_test

import (
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/shared/wire"
)

// otherServer serves on a socket of its own a koko server of another
// version than this CLI: the real transport, sending report, in front of a
// handler that answers space.list with a Space and a field this CLI does
// not know. It returns the socket's path and a count of the calls it
// answered.
func otherServer(t *testing.T, report wire.Report) (string, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/space.list" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error": {"code": "unknown_operation", "message": "no operation here"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"spaces": [{"id": "space_3f9k2m7q", "name": "alice", "colour": "red"}], "cursor": ""}`)
	})
	path := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	s := transport.New(h, h, func() wire.Report { return report }, slog.New(slog.DiscardHandler))
	if err := s.ListenSocket(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return path, &calls
}

// A command whose operation the koko server does not report fails with
// server_too_old, naming the koko server's version and the command, and
// sends nothing.
func TestACommandTheServerLacksIsServerTooOld(t *testing.T) {
	t.Parallel()
	path, calls := otherServer(t, wire.Report{Version: "v0.1.0", Operations: []string{"space.list"}})
	r := harness.Run(t, cli.Runner{SocketPath: path}, nil, "space", "add", "ops", "--json")
	msg := r.WantError(t, 1, "server_too_old")
	if !strings.Contains(msg, "v0.1.0") || !strings.Contains(msg, "koko space add") {
		t.Errorf("message %q does not name the koko server's version, v0.1.0, and the command, koko space add", msg)
	}
	harness.Golden(t, "space-add-server-too-old.json", r.Stderr)
	if n := calls.Load(); n != 0 {
		t.Errorf("the koko server answered %d call(s), want none", n)
	}
	// What it serves still works.
	harness.Run(t, cli.Runner{SocketPath: path}, nil, "space", "ls").WantSuccess(t)
}

// A CLI older than the koko server keeps working within /v1: the koko server
// reports operations this CLI does not know, and answers with fields it does
// not know.
func TestACLIOlderThanTheServerKeepsWorking(t *testing.T) {
	t.Parallel()
	path, _ := otherServer(t, wire.Report{Version: "v9.0.0", Operations: []string{"space.list", "space.archive"}})
	r := harness.Run(t, cli.Runner{SocketPath: path}, nil, "space", "ls", "--json")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, `"name": "alice"`) || strings.Contains(r.Stdout, "colour") {
		t.Errorf("space ls printed %s, want the Space alice without the field this CLI does not know", r.Stdout)
	}
}
