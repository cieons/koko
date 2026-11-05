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
		SocketPath:  filepath.Join(socketDir, "koko.sock"),
	})
	if err != nil {
		t.Fatalf("start the koko server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	call := func(caller op.Caller, name, body string) (int, string) {
		t.Helper()
		req := httptest.NewRequestWithContext(op.WithCaller(t.Context(), caller), http.MethodPost, api.PathPrefix+name, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(api.IdempotencyKeyHeader, api.NewIdempotencyKey())
		rec := httptest.NewRecorder()
		srv.api.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	local := op.Caller{Local: true}
	if status, body := call(local, api.ServerInit.Name, `{"owner": "alice", "address": "127.0.0.1"}`); status != http.StatusOK {
		t.Fatalf("server.init: %d %s", status, body)
	}

	// A laptop joins through the device area's join step, as over TCP, and
	// is then revoked.
	_, body := call(local, api.DeviceAdd.Name, `{"device": "laptop"}`)
	var added api.DeviceAddResponse
	if err := json.Unmarshal([]byte(body), &added); err != nil {
		t.Fatalf("device.add answered %s: %v", body, err)
	}
	code, err := joincode.Decode(added.JoinCode.Code)
	if err != nil {
		t.Fatal(err)
	}
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	id, err := srv.tcp.devices.Join(t.Context(), key, []byte(wire.JoinSecret(code.Secret)), &net.TCPAddr{})
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if status, body := call(local, api.DeviceRevoke.Name, `{"device": "laptop", "last": true}`); status != http.StatusOK {
		t.Fatalf("device.revoke: %d %s", status, body)
	}

	for _, name := range srv.api.Names() {
		status, body := call(op.Caller{DeviceSession: id}, name, `{}`)
		var e api.ErrorResponse
		_ = json.Unmarshal([]byte(body), &e)
		switch {
		case status == api.CodeRevoked.Status && e.Error.Code == api.CodeRevoked.Name:
			if want := `the Device Session "laptop" of this device was revoked`; e.Error.Message != want {
				t.Errorf("%s answers the Revoked laptop %q, want %q", name, e.Error.Message, want)
			}
		case status == api.CodeLocalOnly.Status && e.Error.Code == api.CodeLocalOnly.Name:
		default:
			t.Errorf("%s answers the Revoked laptop %d %s, want revoked (or local_only)", name, status, body)
		}
	}
}
