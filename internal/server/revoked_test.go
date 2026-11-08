package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json/v2"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/joincode"
	"github.com/cieons/koko/internal/shared/wire"
)

// A Revoked Device Session gets nothing from any operation the koko server
// serves: each answers it revoked, naming its device, or local_only when
// only the Local caller may call it. This is the access matrix's "Revoked
// Device Session" caller (P0 spec: people and Spaces, S2), for every
// registered operation at once. The matrix itself calls over Device
// Connections, which never open for a Revoked key; only a call already on its
// way when the revocation commits reaches the operations, so this calls them
// as the caller beneath the transport.
func TestARevokedDeviceSessionIsRefusedByEveryOperation(t *testing.T) {
	b := startBeneathTransport(t)
	id := b.join(`{"device": "laptop"}`)
	if status, body := b.call(local, api.DeviceRevoke.Name, `{"device": "laptop", "last": true}`); status != http.StatusOK {
		t.Fatalf("device.revoke: %d %s", status, body)
	}
	b.wantRefusedByEveryOperation(id, "laptop")
}

// A Removed User's Device Session gets nothing from any operation either:
// Removing revoked it, so each answers it revoked, naming its device, or
// local_only. This is the access matrix's "Removed User's Device Session"
// caller, called beneath the transport for the same reason as the Revoked
// Device Session's.
func TestARemovedUsersDeviceSessionIsRefusedByEveryOperation(t *testing.T) {
	b := startBeneathTransport(t)
	if status, body := b.call(local, api.UserInvite.Name, `{"name": "bob"}`); status != http.StatusOK {
		t.Fatalf("user.invite: %d %s", status, body)
	}
	id := b.join(`{"user": "bob", "device": "laptop"}`)
	if status, body := b.call(local, api.UserRemove.Name, `{"name": "bob"}`); status != http.StatusOK {
		t.Fatalf("user.remove: %d %s", status, body)
	}
	b.wantRefusedByEveryOperation(id, "laptop")
}

// local is the Local caller.
var local = op.Caller{Local: true}

// beneathTransport is a koko server, initialised with the Owner alice,
// whose operations a test calls as any caller, beneath the transport.
type beneathTransport struct {
	t   *testing.T
	srv *Server
}

func startBeneathTransport(t *testing.T) *beneathTransport {
	t.Helper()
	keyFile := filepath.Join(t.TempDir(), "root.key")
	if err := os.WriteFile(keyFile, seal.NewRootKey(), 0o600); err != nil {
		t.Fatal(err)
	}
	// A socket path short enough for macOS's 104 bytes.
	socketDir, err := os.MkdirTemp("", "koko-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	srv, err := Start(t.Context(), Config{
		DataDir:     t.TempDir(),
		RootKeyPath: keyFile,
		Listen:      "127.0.0.1:0",
		LogLevel:    slog.LevelError,
		Log:         io.Discard,
		Filesystems: localDisk{},
		ProcessUIDs: notRoot,
		SocketPath:  filepath.Join(socketDir, "koko.sock"),
	})
	if err != nil {
		t.Fatalf("start the koko server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	b := &beneathTransport{t: t, srv: srv}
	if status, body := b.call(local, api.ServerInit.Name, `{"owner": "alice", "address": "127.0.0.1"}`); status != http.StatusOK {
		t.Fatalf("server.init: %d %s", status, body)
	}
	return b
}

// call calls the operation name with body as caller, and returns the status
// and the body of the answer.
func (b *beneathTransport) call(caller op.Caller, name, body string) (int, string) {
	b.t.Helper()
	req := httptest.NewRequestWithContext(op.WithCaller(b.t.Context(), caller), http.MethodPost, api.PathPrefix+name, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(api.IdempotencyKeyHeader, api.NewIdempotencyKey())
	rec := httptest.NewRecorder()
	b.srv.api.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// join issues a Join Code over the Unix socket with the device.add request
// body, joins a new key with it through the device area's join step, as
// over TCP, and returns the new Device Session's id.
func (b *beneathTransport) join(body string) string {
	b.t.Helper()
	_, answer := b.call(local, api.DeviceAdd.Name, body)
	var added api.DeviceAddResponse
	if err := json.Unmarshal([]byte(answer), &added); err != nil {
		b.t.Fatalf("device.add answered %s: %v", answer, err)
	}
	code, err := joincode.Decode(added.JoinCode.Code)
	if err != nil {
		b.t.Fatal(err)
	}
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		b.t.Fatal(err)
	}
	id, err := b.srv.tcp.devices.Join(b.t.Context(), key, []byte(wire.JoinSecret(code.Secret)), &net.TCPAddr{})
	if err != nil {
		b.t.Fatalf("join: %v", err)
	}
	return id
}

// wantRefusedByEveryOperation fails the test unless every registered
// operation, called as the Device Session id of the device named device,
// answers revoked, naming the device, or local_only.
func (b *beneathTransport) wantRefusedByEveryOperation(id, device string) {
	b.t.Helper()
	for _, name := range b.srv.api.Names() {
		status, body := b.call(op.Caller{DeviceSession: id}, name, `{}`)
		var e api.ErrorResponse
		_ = json.Unmarshal([]byte(body), &e)
		switch {
		case status == api.CodeRevoked.Status && e.Error.Code == api.CodeRevoked.Name:
			if want := `the Device Session "` + device + `" of this device was revoked`; e.Error.Message != want {
				b.t.Errorf("%s answers the Revoked %s %q, want %q", name, device, e.Error.Message, want)
			}
		case status == api.CodeLocalOnly.Status && e.Error.Code == api.CodeLocalOnly.Name:
		default:
			b.t.Errorf("%s answers the Revoked %s %d %s, want revoked (or local_only)", name, device, status, body)
		}
	}
}
