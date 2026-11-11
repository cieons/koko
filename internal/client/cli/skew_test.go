package cli_test

import (
	"bufio"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/compat"
	kokorelease "github.com/cieons/koko/internal/shared/release"
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

// clientAs connects to the koko server's socket at path as a client whose
// SSH version string is sshVersion, as a CLI of another version would.
func clientAs(t *testing.T, path, sshVersion string) *ssh.Client {
	t.Helper()
	var d net.Dialer
	nc, err := d.DialContext(t.Context(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	conn, chans, reqs, err := ssh.NewClientConn(nc, path, &ssh.ClientConfig{
		User:            "koko",
		ClientVersion:   sshVersion,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	c := ssh.NewClient(conn, chans, reqs)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// callAs makes one space.list call over c, as an older CLI would, and
// returns the HTTP status and the error object, if it answered one.
func callAs(t *testing.T, c *ssh.Client) (int, api.Error) {
	t.Helper()
	ch, reqs, err := c.OpenChannel(wire.ChannelAPI, nil)
	if err != nil {
		t.Fatalf("open %s: %v", wire.ChannelAPI, err)
	}
	go ssh.DiscardRequests(reqs)
	defer func() { _ = ch.Close() }()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://koko/v1/space.list", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	if err := req.Write(ch); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(ch), req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var e api.ErrorResponse
	if resp.StatusCode != http.StatusOK {
		if err := json.UnmarshalRead(resp.Body, &e); err != nil {
			t.Fatalf("read the %s answer: %v", resp.Status, err)
		}
	}
	return resp.StatusCode, e.Error
}

// A CLI older than the oldest koko the koko server works with, by the
// version it states or, once the window has moved past v0.3.0, by stating
// none, keeps its connection, and hears why on every call and channel:
// cli_too_old, naming the version needed, koko update and the install
// line, with nothing passed on.
func TestTheServerRefusesACLIBelowTheWindow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, sshVersion, oldest string }{
		{"stated", "SSH-2.0-koko v0.0.1", compat.Oldest},
		{"unstated", "SSH-2.0-koko", "v0.5.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path, calls := otherServer(t, wire.Report{Version: "v0.5.0", Operations: []string{"space.list"}, Oldest: tc.oldest})
			c := clientAs(t, path, tc.sshVersion)
			for range 2 { // the connection stays open after a refusal
				status, e := callAs(t, c)
				if status != http.StatusUpgradeRequired || e.Code != api.CodeCLITooOld.Name {
					t.Fatalf("a call answered %d %q, want %d cli_too_old", status, e.Code, http.StatusUpgradeRequired)
				}
				for _, want := range []string{tc.oldest, "koko update", kokorelease.InstallLine} {
					if !strings.Contains(e.Message, want) {
						t.Errorf("the message %q does not name %q", e.Message, want)
					}
				}
				for _, kind := range []string{wire.ChannelTunnel, wire.ChannelWeb} {
					_, _, err := c.OpenChannel(kind, wire.WebPayload(8080))
					oe, ok := errors.AsType[*ssh.OpenChannelError](err)
					if !ok || oe.Reason != ssh.Prohibited || oe.Message != e.Message {
						t.Errorf("opening %s failed with %v, want administratively prohibited with %q", kind, err, e.Message)
					}
				}
			}
			if n := calls.Load(); n != 0 {
				t.Errorf("the koko server's API answered %d call(s), want none", n)
			}
		})
	}
}

// A CLI at the oldest koko the koko server works with keeps working, and
// so does a development build that names no release before it, as one
// built from a clone without tags.
func TestTheServerServesACLIAtTheWindow(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"v0.5.0", "v0.0.0-20261111074729-d6db4d8b9455+dirty"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			path, calls := otherServer(t, wire.Report{Version: "v0.5.0", Operations: []string{"space.list"}, Oldest: "v0.5.0"})
			c := clientAs(t, path, compat.SSHVersion(version))
			if status, e := callAs(t, c); status != http.StatusOK {
				t.Errorf("a call answered %d %+v, want 200", status, e)
			}
			ch, reqs, err := c.OpenChannel(wire.ChannelWeb, wire.WebPayload(8080))
			if err != nil {
				t.Fatalf("open %s: %v", wire.ChannelWeb, err)
			}
			go ssh.DiscardRequests(reqs)
			_ = ch.Close()
			if n := calls.Load(); n != 1 {
				t.Errorf("the koko server's API answered %d call(s), want 1", n)
			}
		})
	}
}

// A CLI that reads in the report an oldest above itself fails every
// command with cli_too_old, naming both versions and koko update, and
// sends nothing; koko tunnel, ssh's ProxyCommand, prints the same line,
// after which ssh exits 255. A CLI at the oldest keeps working.
func TestACLIBelowTheWindowFailsEveryCommandItself(t *testing.T) {
	t.Parallel()
	path, calls := otherServer(t, wire.Report{Version: "v0.6.0", Operations: []string{"space.list"}, Oldest: "v0.5.0"})
	below := releaseCLI(t, "v0.4.0", path)
	r := harness.Run(t, below, nil, "space", "ls", "--json")
	msg := r.WantError(t, 1, "cli_too_old")
	if want := "this koko is v0.4.0, older than v0.5.0, the oldest koko the koko server on this machine works with: run koko update"; msg != want {
		t.Errorf("the message is %q, want %q", msg, want)
	}
	tunnel := harness.Run(t, below, nil, "tunnel", "web1.koko")
	if tunnel.Status != 1 || tunnel.Stdout != "" || tunnel.Stderr != "koko: "+msg+"\n" {
		t.Errorf("koko tunnel: exit status %d, stdout %q, stderr %q; want 1 and the line %q", tunnel.Status, tunnel.Stdout, tunnel.Stderr, msg)
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("the koko server answered %d call(s), want none", n)
	}
	harness.Run(t, releaseCLI(t, "v0.5.0", path), nil, "space", "ls").WantSuccess(t)
}

// A command whose operation the koko server does not report fails with
// server_too_old, naming the koko server's version and the command, and
// sends nothing.
func TestACommandTheServerLacksIsServerTooOld(t *testing.T) {
	t.Parallel()
	path, calls := otherServer(t, wire.Report{Version: "v0.3.0", Operations: []string{"space.list"}})
	r := harness.Run(t, cli.Runner{SocketPath: path}, nil, "space", "add", "ops", "--json")
	msg := r.WantError(t, 1, "server_too_old")
	if !strings.Contains(msg, "v0.3.0") || !strings.Contains(msg, "koko space add") {
		t.Errorf("message %q does not name the koko server's version, v0.3.0, and the command, koko space add", msg)
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
