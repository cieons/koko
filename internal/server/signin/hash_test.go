package signin_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// startOn starts a koko server with its web listener on dataDir with the
// Root Key file keyFile, initialising it unless it is initialised already.
func startOn(t *testing.T, dataDir, keyFile string, initialised bool) *harness.Harness {
	t.Helper()
	opts := []harness.Option{harness.DataDir(dataDir), harness.RootKeyFile(keyFile), harness.WebListen("127.0.0.1:0")}
	if initialised {
		opts = append(opts, harness.NoInit())
	}
	return harness.Start(t, opts...)
}

// storedHash returns the PHC string of the User name's Password, unsealed
// with the Root Key in keyFile from the data directory of a stopped koko
// server.
func storedHash(t *testing.T, dataDir, keyFile, name string) string {
	t.Helper()
	rootKey, err := seal.Load(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	var phc string
	withStore(t, dataDir, func(tx store.Tx) error {
		var id string
		var sealed []byte
		err := tx.QueryRowContext(t.Context(), `SELECT p.id, p.sealed FROM passwords p JOIN users u ON u.id = p.user_id
			WHERE u.name = ?`, name).Scan(&id, &sealed)
		if err != nil {
			return err
		}
		plain, err := rootKey.Open(id, sealed)
		phc = string(plain)
		return err
	})
	return phc
}

// withStore runs fn in a write transaction on the data directory of a
// stopped koko server, as someone with a copy of it would.
func withStore(t *testing.T, dataDir string, fn func(tx store.Tx) error) {
	t.Helper()
	st, err := store.Open(t.Context(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if err := st.Write(t.Context(), fn); err != nil {
		t.Fatal(err)
	}
}

// A Password is kept as an Argon2id hash of OWASP's parameters, a PHC
// string sealed under the Root Key: no file of the data directory holds
// the hash or the Password in plain.
func TestAPasswordIsKeptAsASealedArgon2idHash(t *testing.T) {
	t.Parallel()
	dataDir, keyFile := t.TempDir(), harness.NewRootKeyFile(t)
	h := startOn(t, dataDir, keyFile, false)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.SignIn("bob")
	h.Stop()

	err := filepath.WalkDir(dataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, plain := range []string{"$argon2id$", harness.Password} {
			if bytes.Contains(b, []byte(plain)) {
				t.Errorf("%s holds %q in plain", path, plain)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	phc := storedHash(t, dataDir, keyFile, "bob")
	fields := strings.Split(phc, "$")
	if len(fields) != 6 || fields[1] != "argon2id" || fields[2] != "v=19" || fields[3] != "m=19456,t=2,p=1" {
		t.Fatalf("the unsealed hash is %q, want an Argon2id PHC string of m=19456,t=2,p=1", phc)
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(fields[4])
	key, err2 := base64.RawStdEncoding.DecodeString(fields[5])
	if err1 != nil || err2 != nil || len(salt) != 16 || len(key) != 32 {
		t.Errorf("the hash's salt and key are %d and %d bytes, want 16 and 32", len(salt), len(key))
	}
	if want := argon2.IDKey([]byte(harness.Password), salt, 2, 19456, 1, 32); !bytes.Equal(key, want) {
		t.Error("the hash is not Argon2id of the Password with its salt")
	}
}

// A hash of older parameters is rehashed with the current ones at the next
// sign-in, and the Password still signs in.
func TestASignInRehashesAnOlderHash(t *testing.T) {
	t.Parallel()
	dataDir, keyFile := t.TempDir(), harness.NewRootKeyFile(t)
	h := startOn(t, dataDir, keyFile, false)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.SignIn("bob")
	h.Stop()

	rootKey, err := seal.Load(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	salt := []byte("an older salt 16")
	older := fmt.Sprintf("$argon2id$v=19$m=8192,t=3,p=1$%s$%s", base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(argon2.IDKey([]byte(harness.Password), salt, 3, 8192, 1, 32)))
	withStore(t, dataDir, func(tx store.Tx) error {
		var id string
		if err := tx.QueryRowContext(t.Context(), "SELECT id FROM passwords").Scan(&id); err != nil {
			return err
		}
		_, err := tx.ExecContext(t.Context(), "UPDATE passwords SET sealed = ? WHERE id = ?", rootKey.Seal(id, []byte(older)), id)
		return err
	})

	h = startOn(t, dataDir, keyFile, true)
	b := h.Browser()
	status, body := b.Post(api.AuthSignInPath, api.AuthSignInRequest{Name: "bob", Password: harness.Password})
	if status != http.StatusOK {
		t.Fatalf("signing in with an older hash answered %d %s", status, body)
	}
	h.Stop()
	if phc := storedHash(t, dataDir, keyFile, "bob"); !strings.HasPrefix(phc, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("after the sign-in the hash is %q, want it rehashed with m=19456,t=2,p=1", phc)
	}
}
