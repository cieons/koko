package cli_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/shared/api"
)

// wantCallRefused fails the test unless err is the koko server's refusal with
// code.
func wantCallRefused(t *testing.T, what string, err error, code api.Code) {
	t.Helper()
	if e, ok := errors.AsType[*api.Error](err); !ok || e.Code != code.Name {
		t.Errorf("%s: got %v, want %s", what, err, code.Name)
	}
}

// withRounds is file, a private key in OpenSSH's format encrypted with
// bcrypt_pbkdf, with its header asking for rounds rounds, as ssh-keygen -a
// writes it. Its passphrase no longer opens it, since the key was derived
// with the rounds it had.
func withRounds(t *testing.T, file []byte, rounds uint32) []byte {
	t.Helper()
	block, _ := pem.Decode(file)
	const magic = "openssh-key-v1\x00"
	var key struct {
		CipherName, KdfName, KdfOpts string
		NumKeys                      uint32
		PubKey, PrivKeyBlock         []byte
	}
	if block == nil || ssh.Unmarshal(block.Bytes[len(magic):], &key) != nil || key.KdfName != "bcrypt" {
		t.Fatalf("%q is no key in OpenSSH's format encrypted with bcrypt", file)
	}
	var opts struct {
		Salt   string
		Rounds uint32
	}
	if err := ssh.Unmarshal([]byte(key.KdfOpts), &opts); err != nil {
		t.Fatal(err)
	}
	opts.Rounds = rounds
	key.KdfOpts = string(ssh.Marshal(opts))
	block.Bytes = append([]byte(magic), ssh.Marshal(key)...)
	return pem.EncodeToMemory(block)
}

// pkcs1Key is an RSA key in PKCS #1's PEM, encrypted with passphrase in
// PEM's older form, which x/crypto still decrypts.
func pkcs1Key(t *testing.T, passphrase string) []byte {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	//nolint:staticcheck // SA1019: the old form is deprecated, but keys made with it are still in use, and Enroll takes them.
	block, err := x509.EncryptPEMBlock(rand.Reader, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(k), []byte(passphrase), x509.PEMCipherAES256)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(block)
}

// host.enroll takes an encrypted private key with its passphrase, as the
// web UI sends them, and the koko server decrypts the key to log in. A key
// without its passphrase is refused with passphrase_needed, one the
// passphrase does not open with wrong_passphrase, in OpenSSH's format or
// PKCS #1's, each before any login; a key whose KDF asks for more than 256
// rounds is refused with key_too_costly before any KDF runs, while one
// with ssh-keygen -a 100's is taken; and a passphrase is taken only with
// an encrypted key. The passphrase is in
// no log line and no file of the data directory.
func TestEnrollDecryptsAnEncryptedKeyWithThePassphraseGiven(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	h := harness.Start(t, harness.DataDir(dataDir))
	const passphrase = "opens the key in the browser's request"
	encrypted, plain := newUserKey(t, passphrase), newUserKey(t, "")
	host := sshhost.Start(t, sshhost.AuthorizedKey(encrypted.pub))
	addHostAt(t, h.Koko, "web1", host)

	for _, c := range []struct {
		what string
		req  api.HostEnrollRequest
		code api.Code
	}{
		{"no passphrase", api.HostEnrollRequest{Host: "web1", PrivateKey: encrypted.file}, api.CodePassphraseNeeded},
		{"a wrong passphrase", api.HostEnrollRequest{Host: "web1", PrivateKey: encrypted.file, Passphrase: []byte("not it")}, api.CodeWrongPassphrase},
		{"a passphrase for a key that is not encrypted", api.HostEnrollRequest{Host: "web1", PrivateKey: plain.file, Passphrase: []byte(passphrase)}, api.CodeInvalidRequest},
		{"a passphrase with a password", api.HostEnrollRequest{Host: "web1", Password: []byte(hostPassword), Passphrase: []byte(passphrase)}, api.CodeInvalidRequest},
		{"a key whose KDF asks for 257 rounds", api.HostEnrollRequest{Host: "web1", PrivateKey: withRounds(t, encrypted.file, 257), Passphrase: []byte(passphrase)}, api.CodeKeyTooCostly},
		// Taken, then not opened, since its rounds are not those its key was derived with.
		{"a key whose KDF asks for 100 rounds", api.HostEnrollRequest{Host: "web1", PrivateKey: withRounds(t, encrypted.file, 100), Passphrase: []byte(passphrase)}, api.CodeWrongPassphrase},
		{"a wrong passphrase for a key in PKCS #1's encrypted PEM", api.HostEnrollRequest{Host: "web1", PrivateKey: pkcs1Key(t, passphrase), Passphrase: []byte("not it")}, api.CodeWrongPassphrase},
	} {
		_, err := h.Call(api.HostEnroll, c.req)
		wantCallRefused(t, c.what, err, c.code)
		if len(c.req.Passphrase) > 0 && err != nil && strings.Contains(err.Error(), string(c.req.Passphrase)) {
			t.Errorf("%s: the refusal %q holds the passphrase", c.what, err)
		}
	}
	if logins := host.Logins(); len(logins) != 0 {
		t.Fatalf("the refused Enrolls logged in to the Host: %+v", logins)
	}
	if got := showHost(t, h.Koko, "web1"); got.State != "registered" {
		t.Fatalf("after the refusals web1 is %s, want registered", got.State)
	}

	resp, err := h.Call(api.HostEnroll, api.HostEnrollRequest{Host: "web1", PrivateKey: encrypted.file, Passphrase: []byte(passphrase)})
	if err != nil {
		t.Fatalf("Enroll with the key and its passphrase: %v", err)
	}
	if resp.Host.State != api.HostManaged || resp.Host.HostKey == nil || *resp.Host.HostKey != fingerprint(host) {
		t.Errorf("Enroll answered %+v, want web1 Managed with the pin %s", resp.Host, fingerprint(host))
	}
	wantOneLoginWith(t, host, "publickey", encrypted.pub)

	running := readDir(t, dataDir)
	h.Stop()
	places := map[string][]byte{"the log": []byte(h.Log())}
	for file, content := range running {
		places[file+" while running"] = content
	}
	for file, content := range readDir(t, dataDir) {
		places[file+" once stopped"] = content
	}
	for place, content := range places {
		for name, value := range map[string]string{
			"the passphrase":           passphrase,
			"the passphrase in base64": base64.StdEncoding.EncodeToString([]byte(passphrase)),
		} {
			if bytes.Contains(content, []byte(value)) {
				t.Errorf("%s holds %s", place, name)
			}
		}
	}
}
