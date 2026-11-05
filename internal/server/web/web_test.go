package web_test

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
)

// The key the koko server signs koko web's cookies with is made on first
// use, when a token is first traded, and kept sealed under the Root Key for
// its own row: it opens with the Root Key, and no file of the data directory
// holds it, while the koko server runs or once it stops.
func TestTheCookieKeyIsMadeOnFirstUseAndSealed(t *testing.T) {
	dataDir, keyFile := t.TempDir(), harness.NewRootKeyFile(t)
	h := harness.Start(t, harness.DataDir(dataDir), harness.RootKeyFile(keyFile))
	if _, _, ok := cookieKeyRow(t, dataDir); ok {
		t.Fatal("the cookie key exists before any koko web")
	}

	tradeAToken(t, h, dataDir)
	running := readFiles(t, dataDir)
	h.Stop()
	stopped := readFiles(t, dataDir)

	id, sealed, ok := cookieKeyRow(t, dataDir)
	if !ok {
		t.Fatal("no cookie key after a token was traded")
	}
	rootKey, err := seal.Load(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if by, err := seal.SealedBy(sealed); err != nil || by != rootKey.ID() {
		t.Errorf("the cookie key is sealed by %q (%v), want the Root Key %s", by, err, rootKey.ID())
	}
	key, err := rootKey.Open(id, sealed)
	if err != nil || len(key) != 32 {
		t.Fatalf("open the cookie key with the Root Key: %d bytes, %v", len(key), err)
	}
	encodings := map[string][]byte{
		"raw":       key,
		"hex":       []byte(hex.EncodeToString(key)),
		"base64":    []byte(base64.RawStdEncoding.EncodeToString(key)),
		"base64url": []byte(base64.RawURLEncoding.EncodeToString(key)),
	}
	for when, files := range map[string]map[string][]byte{"while running": running, "once stopped": stopped} {
		for file, content := range files {
			for enc, b := range encodings {
				if bytes.Contains(content, b) {
					t.Errorf("%s, %s holds the cookie key (%s)", when, file, enc)
				}
			}
		}
	}
}

// tradeAToken runs koko web on the koko server of h, on dataDir, and opens
// its address, as a browser does.
func tradeAToken(t *testing.T, h *harness.Harness, dataDir string) {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	p := h.Background(nil, "web", "--port", strconv.Itoa(port), "--no-open", "--json")
	var printed struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal([]byte(p.WaitStdout("}\n")), &printed); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(printed.Address)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := cookieKeyRow(t, dataDir); ok {
		t.Fatal("the cookie key exists before a token was traded")
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:"+u.Port()+"/?"+u.RawQuery, nil)
	if err != nil {
		t.Fatal(err)
	}
	tr := &http.Transport{}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || len(resp.Cookies()) != 1 {
		t.Fatalf("opening the address answered %s with %d cookies", resp.Status, len(resp.Cookies()))
	}
	if r := p.Stop(); r.Status != 0 {
		t.Fatalf("koko web exited %d: %s", r.Status, r.Stderr)
	}
}

// cookieKeyRow reads the cookie key's row of own_keys in dataDir.
func cookieKeyRow(t *testing.T, dataDir string) (id string, sealed []byte, ok bool) {
	t.Helper()
	st, err := store.Open(t.Context(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	err = st.Read(t.Context(), func(tx store.Tx) error {
		return tx.QueryRowContext(t.Context(), "SELECT id, sealed FROM own_keys WHERE kind = 'cookie'").Scan(&id, &sealed)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return id, sealed, true
}

// readFiles reads every file in dir, by name.
func readFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = b
	}
	return files
}
