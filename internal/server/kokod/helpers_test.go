//go:build unix

// The helpers of the first-start tests, which run on Unix only.

package kokod_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
)

// serverOut is the koko server's settings, as koko server show --json
// prints them.
type serverOut struct {
	Address            string `json:"address"`
	ListenPort         int    `json:"listen_port"`
	HostKeyFingerprint string `json:"host_key_fingerprint"`
	Version            string `json:"version"`
	RootKeyFile        string `json:"root_key_file"`
	RootKeyID          string `json:"root_key_id"`
	DataDir            string `json:"data_dir"`
}

// parseServer wants r, a koko server show with --json, to have succeeded,
// and returns what it printed.
func parseServer(t *testing.T, r harness.Result) serverOut {
	t.Helper()
	r.WantSuccess(t)
	var s serverOut
	if err := json.Unmarshal([]byte(r.Stdout), &s, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("printed %q: %v", r.Stdout, err)
	}
	return s
}

// joinCodeOut is an issued Join Code as koko device add --json prints it.
type joinCodeOut struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	User      string `json:"user"`
	Device    string `json:"device"`
	ExpiresAt string `json:"expires_at"`
}

// clientConfig is the part of the client config a join writes in
// ~/.ssh/koko/ that these tests read.
type clientConfig struct {
	HostKeys []string `json:"host_keys"`
}

// readClientConfig reads the client config the device d's join wrote.
func readClientConfig(t *testing.T, d *harness.Device) clientConfig {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(d.Home(), ".ssh", "koko", "client.json"))
	if err != nil {
		t.Fatalf("read the client config: %v", err)
	}
	var c clientConfig
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("the client config %s does not parse: %v", b, err)
	}
	return c
}

// wantNoRootKey fails the test if what kokod run printed, on stdout or
// stderr, holds the key in the Root Key file whose content is file. The id
// before it is printed on purpose.
func wantNoRootKey(t *testing.T, r harness.Result, file []byte) {
	t.Helper()
	_, key, ok := strings.Cut(strings.TrimSpace(string(file)), ":")
	if !ok || key == "" {
		t.Fatalf("the Root Key file does not hold <id>:<key>")
	}
	if strings.Contains(r.Stdout+r.Stderr, key) {
		t.Error("kokod run printed the Root Key") // and this test does not print it either
	}
}
