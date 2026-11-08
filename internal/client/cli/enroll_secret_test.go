package cli_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/server/store"
)

// Enroll with --secret logs in with the password a Secret holds, which the
// koko server reads on its own side: the CLI neither prompts on a terminal
// nor reads stdin, and the line ending echo adds to a password piped into
// secret add is dropped, as --password-stdin drops it. The Secret stays as
// it was.
func TestEnrollWithASecretHoldingThePassword(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	host := sshhost.Start(t, sshhost.Password(hostPassword))
	addHostAt(t, h.Koko, "web1", host)
	h.KokoStdin(hostPassword+"\n", "secret", "add", "ops/root-password").WantSuccess(t)
	before := h.Koko("secret", "show", "ops/root-password", "--json")
	before.WantSuccess(t)
	h.Advance(time.Minute)

	term := harness.NewTerminal()
	r := h.KokoTerminal(term, "host", "enroll", "web1", "--secret", "ops/root-password", "--json")
	wantPinned(t, decodePrompted(t, r, ""), "managed", fingerprint(host))
	if term.Reads() != 0 {
		t.Errorf("koko read %d lines from the terminal, want none", term.Reads())
	}
	wantOneLoginWith(t, host, "password", nil)
	wantNoLoginWith(t, host, kokoKeyLine(t, host))
	if after := h.Koko("secret", "show", "ops/root-password", "--json"); after.Stdout != before.Stdout {
		t.Errorf("Enroll changed the Secret from\n%s\nto\n%s", before.Stdout, after.Stdout)
	}
}

// A Secret whose value begins with a PEM header, past any blank space,
// holds a private key, in any PEM form koko reads, stored byte for byte from
// its file: Enroll logs in with it once, by public key, and never with the
// koko key.
func TestEnrollWithASecretHoldingAPrivateKey(t *testing.T) {
	t.Parallel()
	openSSH := newUserKey(t, "")
	indented := filepath.Join(t.TempDir(), "indented")
	if err := os.WriteFile(indented, append([]byte(" \t"), openSSH.file...), 0o600); err != nil {
		t.Fatal(err)
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(ecKey)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8 := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(pkcs8, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	ecPub, err := ssh.NewPublicKey(&ecKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		path string
		pub  ssh.PublicKey
	}{
		{"in OpenSSH's form", openSSH.path, openSSH.pub},
		{"in PKCS #8", pkcs8, ecPub},
		{"after blank space", indented, openSSH.pub},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := startWithOps(t)
			host := sshhost.Start(t, sshhost.AuthorizedKey(tc.pub))
			addHostAt(t, h.Koko, "web1", host)
			h.Koko("secret", "add", "ops/deploy-key", "--from-file", tc.path).WantSuccess(t)

			r := h.Koko("host", "enroll", "web1", "--secret", "ops/deploy-key", "--json")
			wantPinned(t, decodeHost(t, r), "managed", fingerprint(host))
			wantOneLoginWith(t, host, "publickey", tc.pub)
			wantNoLoginWith(t, host, kokoKeyLine(t, host))
		})
	}
}

// A caller who can act on the Host but cannot read the Secret is refused as
// for a Space that does not exist, before koko connects to the Host: the
// Secret's Space is checked apart from the Host's, and a Secret the caller
// can read grants no Host they cannot see either. Membership is read on
// every call, so a Member given the Space Enrolls with its Secret at once.
func TestEnrollWithASecretTheCallerCannotReadIsRefused(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	h.Koko("space", "add", "vault").WantSuccess(t)
	h.Koko("user", "invite", "bob").WantSuccess(t)
	h.Koko("space", "member", "add", "ops", "bob").WantSuccess(t)
	host := sshhost.Start(t, sshhost.Password(hostPassword))
	addHostAt(t, h.Koko, "web1", host, "space=ops")
	hidden := sshhost.Start(t, sshhost.Password(hostPassword))
	addHostAt(t, h.Koko, "db1", hidden, "space=vault")
	h.KokoStdin(hostPassword, "secret", "add", "vault/root-password").WantSuccess(t)
	h.KokoStdin(hostPassword, "secret", "add", "ops/root-password").WantSuccess(t)
	bob := h.Join("bob")

	r := bob.Koko("host", "enroll", "web1", "--secret", "vault/root-password", "--json")
	r.WantError(t, 1, "unknown_space")
	never := bob.Koko("host", "enroll", "web1", "--secret", "nowhere/root-password", "--json")
	if want := strings.ReplaceAll(never.Stderr, "nowhere", "vault"); r.Stderr != want {
		t.Errorf("the refusal of a Secret bob cannot read is %q, want it as for a Space that does not exist: %q", r.Stderr, want)
	}
	bob.Koko("host", "enroll", "web1", "--secret", "ops/none", "--json").WantError(t, 1, "unknown_secret")
	bob.Koko("host", "enroll", "db1", "--secret", "ops/root-password", "--json").WantError(t, 1, "unknown_host")
	if host.Conns() != 0 || hidden.Conns() != 0 {
		t.Fatalf("the refusals made %d and %d connections to the Hosts, want none", host.Conns(), hidden.Conns())
	}
	wantPinned(t, showHost(t, h.Koko, "web1"), "registered", "")

	h.Koko("space", "member", "add", "vault", "bob").WantSuccess(t)
	r = bob.Koko("host", "enroll", "web1", "--secret", "vault/root-password", "--json")
	wantPinned(t, decodeHost(t, r), "managed", fingerprint(host))
}

// A Secret that holds no password or private key Enroll can use is refused
// before koko connects to the Host, naming the Secret: an encrypted key,
// since koko keeps no passphrase; a value that begins with a PEM header but
// holds no private key, which is never tried as a password; and a line
// ending alone.
func TestEnrollRefusesASecretItCannotUse(t *testing.T) {
	t.Parallel()
	h := startWithOps(t)
	host := sshhost.Start(t, sshhost.Password(hostPassword))
	addHostAt(t, h.Koko, "web1", host)
	encrypted := newUserKey(t, "open sesame")
	for _, tc := range []struct {
		name, value, says string
	}{
		{"key", string(encrypted.file), "the private key in the Secret ops/key is encrypted"},
		{"certificate", "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n", "the private key in the Secret ops/certificate does not parse"},
		{"newline", "\n", "the Secret ops/newline holds only a line ending"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h.KokoStdin(tc.value, "secret", "add", "ops/"+tc.name).WantSuccess(t)
			r := h.Koko("host", "enroll", "web1", "--secret", "ops/"+tc.name, "--json")
			if msg := r.WantError(t, 1, "invalid_value"); !strings.Contains(msg, tc.says) {
				t.Errorf("the refusal is %q, want it to say %q", msg, tc.says)
			}
		})
	}
	if host.Conns() != 0 {
		t.Errorf("the refusals made %d connections to the Host, want none", host.Conns())
	}
	wantPinned(t, showHost(t, h.Koko, "web1"), "registered", "")
}

// The value of a Secret Enroll logs in with never reaches the device: it is
// in no output, no log line and no file under any HOME, over a Device
// Connection or the Unix socket, whether the Host takes it, refuses it, or
// Enroll refuses a key it cannot use. Enroll stores nothing it read either:
// the koko server's data directory holds the value only sealed, as the
// Secret.
func TestASecretsValueNeverReachesTheDeviceThatEnrolls(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	h := harness.Start(t, harness.DataDir(dataDir))
	h.Koko("space", "add", "ops").WantSuccess(t)
	alice := h.Join(harness.Owner)
	const (
		password = "koko-enroll-marker-password-9Rv2"
		wrong    = "koko-enroll-marker-wrong-3Hd8"
	)
	key, encrypted, cut := newUserKey(t, ""), newUserKey(t, "open sesame"), newUserKey(t, "")
	// cut is a key without the last line of its body, which does not parse.
	lines := strings.Split(string(cut.file), "\n")
	truncated := strings.Join(slices.Delete(lines, len(lines)-3, len(lines)-2), "\n")
	byPassword := sshhost.Start(t, sshhost.Password(password))
	addHostAt(t, h.Koko, "web1", byPassword)
	byKey := sshhost.Start(t, sshhost.AuthorizedKey(key.pub))
	addHostAt(t, h.Koko, "web2", byKey)
	h.KokoStdin(password+"\n", "secret", "add", "ops/password").WantSuccess(t)
	h.KokoStdin(wrong, "secret", "add", "ops/wrong").WantSuccess(t)
	h.Koko("secret", "add", "ops/key", "--from-file", key.path).WantSuccess(t)
	h.Koko("secret", "add", "ops/encrypted", "--from-file", encrypted.path).WantSuccess(t)
	h.KokoStdin(truncated, "secret", "add", "ops/cut").WantSuccess(t)

	refused := alice.Koko("host", "enroll", "web1", "--secret", "ops/wrong", "--json")
	if msg := refused.WantError(t, 1, "login_refused"); !strings.Contains(msg, "refused the password in the Secret ops/wrong") {
		t.Errorf("the refusal %q does not name the Secret", msg)
	}
	encryptedRefused := alice.Koko("host", "enroll", "web2", "--secret", "ops/encrypted", "--json")
	encryptedRefused.WantError(t, 1, "invalid_value")
	cutRefused := h.Koko("host", "enroll", "web2", "--secret", "ops/cut")
	if cutRefused.Status != 1 || !strings.Contains(cutRefused.Stderr, "the private key in the Secret ops/cut does not parse") {
		t.Errorf("Enroll with a cut key: exit status %d, stderr %q, want it refused as a key that does not parse", cutRefused.Status, cutRefused.Stderr)
	}
	outputs := []harness.Result{
		refused,
		encryptedRefused,
		cutRefused,
		alice.Koko("host", "enroll", "web1", "--secret", "ops/password"),
		h.Koko("host", "enroll", "web2", "--secret", "ops/key", "--json"),
	}
	for _, host := range []string{"web1", "web2"} {
		if got := showHost(t, h.Koko, host); got.State != "managed" {
			t.Fatalf("%s is %s, want it Enrolled", host, got.State)
		}
	}

	// The first line of a key's body is the same in every OpenSSH ed25519
	// key without a passphrase, the device's own among them; the fourth
	// holds bytes of that key alone.
	values := map[string]string{
		"the key's file":           strings.Split(string(key.file), "\n")[4],
		"the key's file in base64": base64.StdEncoding.EncodeToString(key.file),
		"the encrypted key's file": strings.Split(string(encrypted.file), "\n")[4],
		"the cut key's file":       strings.Split(truncated, "\n")[4],
	}
	for name, value := range map[string]string{"the password": password, "the wrong password": wrong} {
		values[name] = value
		values[name+" in base64"] = base64.StdEncoding.EncodeToString([]byte(value))
	}
	running := readDir(t, dataDir)
	if len(running[store.FileName+"-wal"]) == 0 {
		t.Fatalf("the running koko server's data directory holds no -wal to check: %v", running)
	}
	h.Stop()
	places := map[string]string{"the log": h.Log()}
	for i, r := range outputs {
		places[fmt.Sprintf("the output of Enroll %d", i+1)] = r.Stdout + r.Stderr
	}
	for _, home := range []string{h.Home(), alice.Home()} {
		for path, content := range contents(t, home) {
			places[path] = content
		}
	}
	for file, content := range running {
		places[file+" while running"] = string(content)
	}
	for file, content := range readDir(t, dataDir) {
		places[file+" once stopped"] = string(content)
	}
	for place, content := range places {
		for name, value := range values {
			if strings.Contains(content, value) {
				t.Errorf("%s holds %s", place, name)
			}
		}
	}
}
