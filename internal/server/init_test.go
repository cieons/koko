package server_test

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/joincode"
)

// wantCode fails the test unless err is an API error with code.
func wantCode(t *testing.T, err error, code api.Code) {
	t.Helper()
	e, ok := errors.AsType[*api.Error](err)
	if !ok || e.Code != code.Name || e.Message == "" {
		t.Errorf("error = %v, want code %s with a message", err, code.Name)
	}
}

// fingerprint is a host key's SHA-256 fingerprint as OpenSSH prints it.
var fingerprint = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`)

// Initialisation names the Owner, who gets a Personal Space named after
// them, stores the Server Address, and returns it with the host key's
// fingerprint.
func TestInitialisationCreatesTheOwnerWithTheirPersonalSpace(t *testing.T) {
	h := harness.Start(t, harness.NoInit())
	resp, err := h.Call(api.ServerInit, api.ServerInitRequest{Owner: "bob", Address: "koko.example.com:8443"})
	if err != nil {
		t.Fatalf("server.init: %v", err)
	}
	if resp.Address != "koko.example.com:8443" || !fingerprint.MatchString(resp.HostKeyFingerprint) {
		t.Errorf("server.init = %+v, want the Server Address given and a SHA256 fingerprint", resp)
	}

	r := h.Koko("space", "ls", "--json")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, `"name": "bob"`) || strings.Count(r.Stdout, `"name"`) != 1 {
		t.Errorf("space ls = %s, want only bob's Personal Space", r.Stdout)
	}
	// Users and Spaces share one namespace, and the Personal Space holds bob.
	h.Koko("space", "add", "bob", "--json").WantError(t, 1, "name_taken")
}

// Before initialisation the koko server answers no operation but
// server.init: a command is refused with not_initialised and records
// nothing.
func TestCommandsAreRefusedBeforeInitialisation(t *testing.T) {
	h := harness.Start(t, harness.NoInit())
	msg := h.Koko("space", "add", "ops", "--json").WantError(t, 1, "not_initialised")
	for _, how := range []string{"koko server install --owner <user>", "kokod run", "--address <host[:port]>", "KOKO_OWNER and KOKO_ADDRESS"} {
		if !strings.Contains(msg, how) {
			t.Errorf("message %q does not name %q, a way to initialise the koko server", msg, how)
		}
	}
	h.Koko("space", "ls", "--json").WantError(t, 1, "not_initialised")

	if _, err := h.Call(api.ServerInit, api.ServerInitRequest{Owner: harness.Owner, Address: harness.Address}); err != nil {
		t.Fatalf("server.init: %v", err)
	}
	if r := h.Koko("space", "ls", "--json"); strings.Contains(r.Stdout, `"ops"`) {
		t.Errorf("space ls = %s: the refused space add was recorded", r.Stdout)
	}
}

// A second initialisation is refused, so nothing replaces the Owner: no
// second Owner's Personal Space appears.
func TestASecondInitialisationIsRefused(t *testing.T) {
	h := harness.Start(t)
	_, err := h.Call(api.ServerInit, api.ServerInitRequest{Owner: "mallory", Address: "evil.example.com"})
	wantCode(t, err, api.CodeAlreadyInitialised)
	_, err = h.Call(api.ServerInit, api.ServerInitRequest{Owner: harness.Owner, Address: harness.Address})
	wantCode(t, err, api.CodeAlreadyInitialised)

	r := h.Koko("space", "ls", "--json")
	if strings.Contains(r.Stdout, "mallory") || strings.Count(r.Stdout, `"name"`) != 1 {
		t.Errorf("space ls = %s, want only %s's Personal Space", r.Stdout, harness.Owner)
	}
}

// The Owner's name is checked as a User name and has no default; a refused
// initialisation leaves the koko server uninitialised.
func TestInitialisationChecksTheOwnersName(t *testing.T) {
	h := harness.Start(t, harness.NoInit())
	for _, owner := range []string{"", "Alice", "al_ice", "al/ice", "-alice", strings.Repeat("a", 64)} {
		_, err := h.Call(api.ServerInit, api.ServerInitRequest{Owner: owner, Address: harness.Address})
		wantCode(t, err, api.CodeInvalidName)
	}
	h.Koko("space", "ls", "--json").WantError(t, 1, "not_initialised")
	if _, err := h.Call(api.ServerInit, api.ServerInitRequest{Owner: strings.Repeat("a", 63), Address: harness.Address}); err != nil {
		t.Errorf("server.init with a 63-character Owner name: %v", err)
	}
}

// The Server Address is host or host:port; anything else is refused and
// leaves the koko server uninitialised. Its forms are tested with package
// serveraddr.
func TestInitialisationChecksTheServerAddress(t *testing.T) {
	h := harness.Start(t, harness.NoInit())
	for _, address := range []string{"", "https://koko.example.com", "koko.example.com:0", "2001:db8::1"} {
		_, err := h.Call(api.ServerInit, api.ServerInitRequest{Owner: harness.Owner, Address: address})
		wantCode(t, err, api.CodeInvalidAddress)
	}
	h.Koko("space", "ls", "--json").WantError(t, 1, "not_initialised")
	resp, err := h.Call(api.ServerInit, api.ServerInitRequest{Owner: harness.Owner, Address: "[2001:db8::1]"})
	if err != nil || resp.Address != "[2001:db8::1]" {
		t.Errorf("server.init with an IPv6 Server Address = %+v, %v", resp, err)
	}
}

// No private key exists unsealed in the data directory: neither the koko key
// nor the host key, in any common encoding, is in the raw bytes of koko.db or
// its -wal, while the koko server runs or after it stops. Nor is the Root
// Key.
func TestNoPrivateKeyIsUnsealedInTheDataDirectory(t *testing.T) {
	dataDir, keyFile := t.TempDir(), harness.NewRootKeyFile(t)
	h := harness.Start(t, harness.DataDir(dataDir), harness.RootKeyFile(keyFile))
	running := readFiles(t, dataDir)
	if len(running[store.FileName+"-wal"]) == 0 {
		t.Fatalf("the running koko server's data directory holds no -wal to check: %v", running)
	}
	h.Stop()
	stopped := readFiles(t, dataDir)

	keyText, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	_, rootKey, _ := strings.Cut(strings.TrimSpace(string(keyText)), ":")
	rootKeyRaw, err := base64.RawURLEncoding.DecodeString(rootKey)
	if err != nil {
		t.Fatal(err)
	}
	keys := openKeys(t, dataDir, keyFile)
	secrets := map[string][]byte{"the Root Key": []byte(rootKey), "the raw Root Key": rootKeyRaw}
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

// openKeys opens the koko server's keys in dataDir with the Root Key in
// keyFile, as its start-up does.
func openKeys(t *testing.T, dataDir, keyFile string) *deployment.Keys {
	t.Helper()
	st, err := store.Open(t.Context(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	rootKey, err := seal.Load(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	var keys *deployment.Keys
	err = st.Read(t.Context(), func(tx store.Tx) (err error) {
		keys, err = deployment.Open(t.Context(), tx, rootKey)
		return err
	})
	if err != nil || keys == nil {
		t.Fatalf("open the koko server's keys: %v, %v", keys, err)
	}
	return keys
}

// Initialisation returns the Owner's first Join Code, issued over the Unix
// socket as koko device add issues one there: for the Owner's device-1,
// valid for 15 minutes, naming the Server Address with its port, or with the
// port the koko server listens on when it names none, and the host key
// initialisation made.
func TestInitialisationIssuesTheOwnersFirstJoinCode(t *testing.T) {
	listen := harness.FreeAddr(t)
	_, port, _ := net.SplitHostPort(listen)
	for _, tc := range []struct{ address, inCode string }{
		{"koko.example.com", "koko.example.com:" + port},
		{"koko.example.com:8443", "koko.example.com:8443"},
		{"[2001:db8::1]", "[2001:db8::1]:" + port},
	} {
		h := harness.Start(t, harness.NoInit(), harness.Listen(listen))
		resp, err := h.Call(api.ServerInit, api.ServerInitRequest{Owner: harness.Owner, Address: tc.address})
		if err != nil {
			t.Fatalf("server.init: %v", err)
		}
		jc := resp.JoinCode
		if want := h.Now().Add(15 * time.Minute).Format(time.RFC3339); jc.User != harness.Owner || jc.Device != "device-1" || jc.ExpiresAt != want {
			t.Errorf("server.init issued %+v, want a Join Code for %s's device-1 that expires at %s", jc, harness.Owner, want)
		}
		c, err := joincode.Decode(jc.Code)
		if err != nil {
			t.Fatalf("decode the first Join Code: %v", err)
		}
		if got := "SHA256:" + base64.RawStdEncoding.EncodeToString(c.Fingerprint[:]); c.Address != tc.inCode || got != resp.HostKeyFingerprint {
			t.Errorf("with the Server Address %s, the first Join Code names %s with host key %s, want %s with %s",
				tc.address, c.Address, got, tc.inCode, resp.HostKeyFingerprint)
		}
		// The code holds device-1, as one device add issued would.
		if r := h.Koko("device", "add", "device-1", "--json"); r.Status != 1 || !strings.Contains(r.Stderr, "name_taken") {
			t.Errorf("device add device-1 after initialisation = %d %s, want name_taken", r.Status, r.Stderr)
		}
		h.Stop() // frees the port for the next case
	}
}

// A Join Code carries the Server Address, with its port, in at most 255
// bytes. A Server Address too long for that is refused, and leaves the koko
// server uninitialised, since initialisation issues the first Join Code.
func TestInitialisationRefusesAServerAddressNoJoinCodeCarries(t *testing.T) {
	h := harness.Start(t, harness.NoInit())
	long := strings.Repeat(strings.Repeat("a", 62)+".", 3) + strings.Repeat("a", 61) // 250 characters
	_, err := h.Call(api.ServerInit, api.ServerInitRequest{Owner: harness.Owner, Address: long + ":18443"})
	wantCode(t, err, api.CodeInvalidAddress)
	h.Koko("space", "ls", "--json").WantError(t, 1, "not_initialised")
	if _, err := h.Call(api.ServerInit, api.ServerInitRequest{Owner: harness.Owner, Address: long + ":1"}); err != nil {
		t.Errorf("server.init with a 252-byte Server Address: %v", err)
	}
}
