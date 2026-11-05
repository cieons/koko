package server_test

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/store"
)

// copyMarker is a Secret value made for the copy test, looked for in the
// data directory's raw bytes.
const copyMarker = "koko-marker-value-Hq8Vt3Rk6P"

// The copy test: a copy of the data directory, taken while the koko server
// runs or once it is stopped, holds no Secret value, no private key of the
// koko server's and not the Root Key, in any common encoding: not in the
// database, its -wal, or a snapshot (P0 spec: Secrets, story 39; P0 spec:
// install and run, story 52).
func TestACopyOfTheDataDirectoryRevealsNoSecret(t *testing.T) {
	dataDir, keyFile := t.TempDir(), harness.NewRootKeyFile(t)
	h := harness.Start(t, harness.DataDir(dataDir), harness.RootKeyFile(keyFile))
	h.Koko("space", "add", "ops").WantSuccess(t)
	h.KokoStdin(copyMarker, "secret", "add", "ops/db-password").WantSuccess(t)
	h.KokoStdin(copyMarker+"\n", "secret", "add", harness.Owner+"/db-password").WantSuccess(t)
	running := readFiles(t, dataDir)
	if len(running[store.FileName+"-wal"]) == 0 {
		t.Fatalf("the running koko server's data directory holds no -wal to check: %v", running)
	}
	h.Stop()
	snapshot := writeSnapshot(t, dataDir)
	stopped := readFiles(t, dataDir)
	if len(stopped[snapshot]) == 0 {
		t.Fatalf("the stopped koko server's data directory holds no snapshot to check")
	}

	secrets := map[string][]byte{
		"the Secret value":           []byte(copyMarker),
		"the Secret value in base64": []byte(base64.StdEncoding.EncodeToString([]byte(copyMarker))),
		"the Secret value in hex":    []byte(hex.EncodeToString([]byte(copyMarker))),
	}
	keyText, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	_, rootKey, _ := strings.Cut(strings.TrimSpace(string(keyText)), ":")
	rootKeyRaw, err := base64.RawURLEncoding.DecodeString(rootKey)
	if err != nil {
		t.Fatal(err)
	}
	secrets["the Root Key"], secrets["the raw Root Key"] = []byte(rootKey), rootKeyRaw
	keys := openKeys(t, dataDir, keyFile)
	for name, priv := range map[string][]byte{"koko key": keys.Koko, "host key": keys.Host} {
		seed := priv[:32]
		secrets["the "+name] = priv
		secrets["the "+name+"'s seed"] = seed
		secrets["the "+name+" in hex"] = []byte(hex.EncodeToString(seed))
		secrets["the "+name+" in base64"] = []byte(base64.RawStdEncoding.EncodeToString(seed))
		secrets["the "+name+" in base64url"] = []byte(base64.RawURLEncoding.EncodeToString(seed))
	}
	for when, files := range map[string]map[string][]byte{"while running": running, "once stopped": stopped} {
		for file, content := range files {
			for name, secret := range secrets {
				if bytes.Contains(content, secret) {
					t.Errorf("%s, %s holds %s", when, file, name)
				}
			}
		}
	}
}

// writeSnapshot writes a snapshot of a stopped koko server's database into
// its data directory, as an upgrade writes one (VACUUM INTO), under the name
// the next schema version's would take, and returns that name.
func writeSnapshot(t *testing.T, dataDir string) string {
	t.Helper()
	name := store.FileName + ".before-" + strconv.Itoa(store.SchemaVersion()+1)
	db, err := sql.Open("sqlite", filepath.Join(dataDir, store.FileName))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(t.Context(), "VACUUM INTO ?", filepath.Join(dataDir, name)); err != nil {
		t.Fatalf("write a snapshot: %v", err)
	}
	return name
}
