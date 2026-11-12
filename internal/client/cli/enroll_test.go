package cli_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/apiclient"
	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/skip"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/timing"
)

// hostPassword is the Host Account's password on the in-process Hosts
// these tests Enroll.
const hostPassword = "correct horse battery staple"

// addHostAt records the in-process Host host as the Host named hostName, at
// its address and port, with its Host Account, on the device run runs
// commands on.
func addHostAt(t *testing.T, run func(args ...string) harness.Result, hostName string, host *sshhost.Host, pairs ...string) printedHost {
	t.Helper()
	at := []string{"address=" + host.Address(), "port=" + strconv.Itoa(host.Port()), "account=" + sshhost.Account}
	return addHost(t, run, hostName, append(at, pairs...)...)
}

// enroll Enrolls the Host named hostName with the in-process Hosts'
// password on stdin, as the Local caller, with args after the command, and
// returns the Host it printed.
func enroll(t *testing.T, h *harness.Harness, hostName string, args ...string) printedHost {
	t.Helper()
	return decodeHost(t, h.KokoStdin(hostPassword, append([]string{"host", "enroll", hostName, "--password-stdin", "--json"}, args...)...))
}

// enrollGolden compares what host enroll printed with its golden file,
// after replacing the in-process Host's port, which differs on every run.
func enrollGolden(t *testing.T, name string, host *sshhost.Host, out string) {
	t.Helper()
	harness.Golden(t, name, strings.ReplaceAll(out, fmt.Sprintf(`"port": %d,`, host.Port()), `"port": <port>,`))
}

// decodePrompted reads the Host that a host command run on a terminal
// printed with --json, once it has exited 0 with only prompt on stderr.
func decodePrompted(t *testing.T, r harness.Result, prompt string) printedHost {
	t.Helper()
	if r.Status != 0 || r.Stderr != prompt {
		t.Fatalf("exit status %d, stderr %q, want success with the prompt %q", r.Status, r.Stderr, prompt)
	}
	return decodeHost(t, harness.Result{Stdout: r.Stdout})
}

// fingerprint is the SHA-256 fingerprint of the in-process Host's host key.
func fingerprint(host *sshhost.Host) string {
	return ssh.FingerprintSHA256(host.HostKey())
}

// wantPinned fails the test unless the Host is in state with the host key
// fp pinned, or none when fp is "".
func wantPinned(t *testing.T, got printedHost, state, fp string) {
	t.Helper()
	switch {
	case got.State != state:
		t.Errorf("%s is %s, want %s", got.Name, got.State, state)
	case fp == "" && got.HostKey != nil:
		t.Errorf("%s has the host key %s pinned, want none", got.Name, *got.HostKey)
	case fp != "" && (got.HostKey == nil || *got.HostKey != fp):
		t.Errorf("%s has the host key %v pinned, want %s", got.Name, got.HostKey, fp)
	}
}

// authorizedKeyLines returns the lines of the in-process Host's
// authorized_keys, each without its newline.
func authorizedKeyLines(host *sshhost.Host) []string {
	return strings.Split(strings.TrimSuffix(string(host.AuthorizedKeys()), "\n"), "\n")
}

// kokoKeyLine returns the line Enroll appended to host's authorized_keys,
// its last, once it is checked to be a key with the comment koko.
func kokoKeyLine(t *testing.T, host *sshhost.Host) string {
	t.Helper()
	lines := authorizedKeyLines(host)
	last := lines[len(lines)-1]
	if _, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(last)); err != nil || comment != "koko" {
		t.Fatalf("the last line of authorized_keys, %q, is not the koko key (%v)", last, err)
	}
	return last
}

// wantNoLoginWith fails the test unless the Host received no login with
// the key whose authorized_keys line is line.
func wantNoLoginWith(t *testing.T, host *sshhost.Host, line string) {
	t.Helper()
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range host.Logins() {
		if l.Key == ssh.FingerprintSHA256(key) {
			t.Errorf("the Host received a login with the koko key: %+v", l)
		}
	}
}

// userKey is a private key of the User's own, in a file in OpenSSH's
// format.
type userKey struct {
	path string
	pub  ssh.PublicKey
	// file is what the file holds.
	file []byte
}

// newUserKey makes an ed25519 key and writes it to a file of its own,
// encrypted with passphrase unless it is empty.
func newUserKey(t *testing.T, passphrase string) userKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "me@laptop")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "me@laptop", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ssh.NewPublicKey(priv.Public())
	if err != nil {
		t.Fatal(err)
	}
	k := userKey{path: filepath.Join(t.TempDir(), "id_ed25519"), pub: pub, file: pem.EncodeToMemory(block)}
	if err := os.WriteFile(k.path, k.file, 0o600); err != nil {
		t.Fatal(err)
	}
	return k
}

// wantOneLoginWith fails the test unless the Host received exactly one
// login, accepted, by method, with key for "publickey".
func wantOneLoginWith(t *testing.T, host *sshhost.Host, method string, key ssh.PublicKey) {
	t.Helper()
	logins := host.Logins()
	want := sshhost.Login{Account: sshhost.Account, Method: method, OK: true}
	if key != nil {
		want.Key = ssh.FingerprintSHA256(key)
	}
	if len(logins) != 1 || logins[0] != want {
		t.Errorf("the Host received the logins %+v, want one: %+v", logins, want)
	}
}

// Enroll makes the Host Account's ~/.ssh with mode 0700 and its
// authorized_keys with mode 0600 when they are missing, as sshd asks of
// them.
func TestEnrollMakesTheHostsSSHFilesWithTheModesSSHDAsksFor(t *testing.T) {
	t.Parallel()
	skip.OnWindows(t, "a Host is a Unix machine, whose files have modes for the group and others; Windows keeps none on the in-process Host's files")
	h := harness.Start(t)
	host := sshhost.Start(t, sshhost.Password(hostPassword))
	addHostAt(t, h.Koko, "web1", host)
	h.KokoStdin(hostPassword+"\n", "host", "enroll", "web1", "--password-stdin").WantSuccess(t)
	if m := mode(t, filepath.Join(host.Home(), ".ssh")); !m.IsDir() || m.Perm() != 0o700 {
		t.Errorf("~/.ssh on the Host has the mode %v, want a directory of mode 0700", m)
	}
	if m := mode(t, filepath.Join(host.Home(), ".ssh", "authorized_keys")); !m.IsRegular() || m.Perm() != 0o600 {
		t.Errorf("authorized_keys on the Host has the mode %v, want a file of mode 0600", m)
	}
}

// Enroll with the password on stdin makes a Registered Host Managed: koko
// logs in once with the password and never with the koko key, pins the host
// key the Host offered, appends exactly one line, the koko key, to the Host
// Account's authorized_keys, which it creates with ~/.ssh when they are
// missing, and prints the Host.
func TestEnrollWithThePasswordOnStdinMakesTheHostManaged(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	host := sshhost.Start(t, sshhost.Password(hostPassword))
	addHostAt(t, h.Koko, "web1", host)

	r := h.KokoStdin(hostPassword+"\n", "host", "enroll", "web1", "--password-stdin", "--json")
	got := decodeHost(t, r)
	enrollGolden(t, "host-enroll.json", host, r.Stdout)
	wantPinned(t, got, "managed", fingerprint(host))

	if lines := authorizedKeyLines(host); len(lines) != 1 {
		t.Fatalf("authorized_keys holds %q, want one line, the koko key", lines)
	}
	line := kokoKeyLine(t, host)
	if !strings.HasPrefix(line, ssh.KeyAlgoED25519+" ") {
		t.Errorf("the koko key's line is %q, want an ed25519 key", line)
	}
	wantOneLoginWith(t, host, "password", nil)
	wantNoLoginWith(t, host, line)

	wantPinned(t, showHost(t, h.Koko, "web1"), "managed", fingerprint(host))
	if out := h.Koko("host", "show", "web1").Stdout; !strings.Contains(out, "Host key:    "+fingerprint(host)+"\n") {
		t.Errorf("host show printed\n%s\nwant the pinned host key as Host key: %s", out, fingerprint(host))
	}
	want := fmt.Sprintf(`msg="host enrolled" host=web1 host_id=%s user=%s enrolled_by="unix socket" host_key=%s`, got.ID, harness.Owner, fingerprint(host))
	if !strings.Contains(h.Log(), want) {
		t.Errorf("the log holds no line %s:\n%s", want, h.Log())
	}
}

// With no input flag on a terminal, Enroll asks for the password with echo
// off, and says for people what it pinned.
func TestEnrollPromptsForThePasswordOnATerminal(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	host := sshhost.Start(t, sshhost.Password(hostPassword))
	added := addHostAt(t, h.Koko, "web1", host)
	term := harness.NewTerminal(hostPassword)
	r := h.KokoTerminal(term, "host", "enroll", "web1")
	if r.Status != 0 || r.Stderr != "Enter the password of web1: \n" {
		t.Fatalf("exit status %d, stderr %q, want success after the prompt", r.Status, r.Stderr)
	}
	if want := fmt.Sprintf("Enrolled Host web1 (%s): it is Managed, with its host key %s pinned.\n", added.ID, fingerprint(host)); r.Stdout != want {
		t.Errorf("printed %q, want %q", r.Stdout, want)
	}
	if term.Reads() != 1 {
		t.Errorf("koko read %d lines from the terminal, want the password's", term.Reads())
	}
	wantOneLoginWith(t, host, "password", nil)
}

// Enroll with --key logs in with the private key in the file, once, and
// never with the koko key; the Host's own keys stay in authorized_keys
// before the koko key.
func TestEnrollWithAPrivateKeyFile(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	key := newUserKey(t, "")
	host := sshhost.Start(t, sshhost.AuthorizedKey(key.pub))
	addHostAt(t, h.Koko, "web1", host)
	before := host.AuthorizedKeys()

	wantPinned(t, decodeHost(t, h.Koko("host", "enroll", "web1", "--key", key.path, "--json")), "managed", fingerprint(host))
	wantOneLoginWith(t, host, "publickey", key.pub)
	after := host.AuthorizedKeys()
	if !bytes.HasPrefix(after, before) || len(authorizedKeyLines(host)) != 2 {
		t.Errorf("authorized_keys went from %q to %q, want the koko key appended", before, after)
	}
	wantNoLoginWith(t, host, kokoKeyLine(t, host))
}

// An encrypted --key asks for its passphrase with echo off on a terminal,
// and is decrypted on the device. Without a terminal, or with a passphrase
// that does not open it, it is refused with exit 2 before any call.
func TestEnrollWithAnEncryptedKeyAsksForItsPassphrase(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	key := newUserKey(t, "open sesame")
	host := sshhost.Start(t, sshhost.AuthorizedKey(key.pub))
	addHostAt(t, h.Koko, "web1", host)
	calls := h.Calls()

	msg := h.Koko("host", "enroll", "web1", "--key", key.path, "--json").WantError(t, 2, "invalid_usage")
	if want := "the passphrase of " + key.path; !strings.Contains(msg, want) || !strings.Contains(msg, "terminal") {
		t.Errorf("the refusal without a terminal is %q, want it to name %s and the terminal", msg, want)
	}
	r := h.KokoTerminal(harness.NewTerminal("wrong"), "host", "enroll", "web1", "--key", key.path, "--json")
	if r.Status != 2 || !strings.Contains(r.Stderr, `"invalid_usage"`) || !strings.Contains(r.Stderr, "does not open") {
		t.Errorf("a wrong passphrase: exit status %d, stderr %q, want exit 2 saying it does not open the key", r.Status, r.Stderr)
	}
	if n := h.Calls() - calls; n != 0 || host.Conns() != 0 {
		t.Fatalf("the refusals made %d calls and %d connections to the Host, want none", n, host.Conns())
	}

	term := harness.NewTerminal("open sesame")
	r = h.KokoTerminal(term, "host", "enroll", "web1", "--key", key.path, "--json")
	wantPinned(t, decodePrompted(t, r, "Enter the passphrase of "+key.path+": \n"), "managed", fingerprint(host))
	wantOneLoginWith(t, host, "publickey", key.pub)
}

// Enroll refuses before any call, with exit 2, a command line it cannot
// carry out: no password, key or Secret and no terminal to ask on, two ways
// to log in or to pin, a fingerprint that is not one, a key file it cannot
// read, a Secret named without its Space, and a password that is empty once
// its line ending is dropped.
func TestEnrollRefusesWhatItCannotUseBeforeAnyCall(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	host := sshhost.Start(t, sshhost.Password(hostPassword))
	addHostAt(t, h.Koko, "web1", host)
	key := newUserKey(t, "")
	public := filepath.Join(t.TempDir(), "id_ed25519.pub")
	if err := os.WriteFile(public, ssh.MarshalAuthorizedKey(key.pub), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := h.Calls()
	for _, tc := range []struct {
		name  string
		stdin string
		args  []string
		says  string
	}{
		{"no password and no terminal", hostPassword, nil, "--password-stdin, --key <file> or --secret <space>/<secret>"},
		{"both ways to log in", hostPassword, []string{"--password-stdin", "--key", key.path}, "give one of them"},
		{"a password and a Secret", hostPassword, []string{"--password-stdin", "--secret", "ops/root-password"}, "give one of them"},
		{"a key and a Secret", "", []string{"--key", key.path, "--secret", "ops/root-password"}, "give one of them"},
		{"a Secret without its Space", "", []string{"--secret", "root-password"}, `--secret "root-password" is not <space>/<secret>`},
		{"a Secret with an empty name", "", []string{"--secret", "ops/"}, "the <secret> is empty"},
		{"both ways to pin", hostPassword, []string{"--password-stdin", "--host-key", fingerprint(host), "--replace-host-key"}, "give one of them"},
		{"an MD5 fingerprint", hostPassword, []string{"--password-stdin", "--host-key", "MD5:16:27:ac:a5:76:28:2d:36:63:1b:56:4d:eb:df:a6:48"}, "SHA256:"},
		{"a cut fingerprint", hostPassword, []string{"--password-stdin", "--host-key", fingerprint(host)[:20]}, "43 characters"},
		{"a missing key file", "", []string{"--key", filepath.Join(t.TempDir(), "none")}, "cannot read --key"},
		{"a public key file", "", []string{"--key", public}, "no private key"},
		{"empty stdin", "", []string{"--password-stdin"}, "empty"},
		{"stdin holding a line ending only", "\n", []string{"--password-stdin"}, "empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := h.KokoStdin(tc.stdin, append([]string{"host", "enroll", "web1", "--json"}, tc.args...)...)
			if msg := r.WantError(t, 2, "invalid_usage"); !strings.Contains(msg, tc.says) {
				t.Errorf("the refusal is %q, want it to say %q", msg, tc.says)
			}
		})
	}
	if n := h.Calls() - calls; n != 0 || host.Conns() != 0 {
		t.Errorf("the refusals made %d calls and %d connections to the Host, want none", n, host.Conns())
	}
}

// --host-key accepts only the key it names: a Host that offers another is
// refused with both fingerprints, before koko sends it anything to log in
// with, and stays Registered with no pin.
func TestEnrollWithHostKeyAcceptsOnlyThatKey(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	host := sshhost.Start(t, sshhost.Password(hostPassword))
	addHostAt(t, h.Koko, "web1", host)
	other := ssh.FingerprintSHA256(sshhost.NewKey(t, ssh.KeyAlgoED25519).PublicKey())

	r := h.KokoStdin(hostPassword, "host", "enroll", "web1", "--password-stdin", "--host-key", other, "--json")
	msg := r.WantError(t, 1, "unexpected_host_key")
	for _, want := range []string{other, fingerprint(host), "--host-key", "--replace-host-key", "koko did not log in"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal %q does not say %q", msg, want)
		}
	}
	if logins := host.Logins(); len(logins) != 0 {
		t.Errorf("the Host received the logins %+v, want none", logins)
	}
	wantPinned(t, showHost(t, h.Koko, "web1"), "registered", "")

	wantPinned(t, enroll(t, h, "web1", "--host-key", fingerprint(host)), "managed", fingerprint(host))
}

// The Host states that Enroll brings (P0 spec: the record): an address or
// port change keeps a Host Managed; a Host Account change sends it back to
// Registered, keeping its pin, and the next Enroll checks the Host against
// that pin; a Managed Host Enrolls again only to pin a new host key; and
// Enrolling again leaves the koko key's line as it is.
func TestTheHostStatesFollowEnroll(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	host := sshhost.Start(t, sshhost.Password(hostPassword))
	addHostAt(t, h.Koko, "web1", host)
	pinned := fingerprint(host)
	wantPinned(t, enroll(t, h, "web1"), "managed", pinned)
	kokoLine := kokoKeyLine(t, host)

	wantPinned(t, decodeHost(t, h.Koko("host", "set", "web1", "port=2222", "--json")), "managed", pinned)
	wantPinned(t, decodeHost(t, h.Koko("host", "set", "web1", "address=203.0.113.10", "--json")), "managed", pinned)
	wantPinned(t, decodeHost(t, h.Koko("host", "set", "web1", "account=root", "--json")), "registered", pinned)
	back := []string{"address=" + host.Address(), "port=" + strconv.Itoa(host.Port()), "account=" + sshhost.Account}
	wantPinned(t, decodeHost(t, h.Koko(append([]string{"host", "set", "web1", "--json"}, back...)...)), "registered", pinned)

	wantPinned(t, enroll(t, h, "web1"), "managed", pinned)
	if lines := authorizedKeyLines(host); len(lines) != 1 || lines[0] != kokoLine {
		t.Errorf("authorized_keys holds %q after a second Enroll, want the koko key's one line", lines)
	}

	r := h.KokoStdin(hostPassword, "host", "enroll", "web1", "--password-stdin", "--json")
	if msg := r.WantError(t, 1, "already_managed"); !strings.Contains(msg, "--host-key") || !strings.Contains(msg, "--replace-host-key") {
		t.Errorf("the refusal %q does not name --host-key and --replace-host-key", msg)
	}
	logins := len(host.Logins())
	wantPinned(t, enroll(t, h, "web1", "--host-key", pinned), "managed", pinned)
	if n := len(host.Logins()) - logins; n != 1 {
		t.Errorf("Enrolling a Managed Host with --host-key made %d logins, want 1", n)
	}
}

// A Host whose host key changed, sent back to Registered, is refused with
// both fingerprints and the two ways on, before koko sends it anything to
// log in with, and keeps its pin. --host-key naming the old key is refused
// alike. --replace-host-key pins the key it offers now once the login
// succeeds, and the log records the old and the new fingerprint.
func TestEnrollRefusesAChangedHostKeyUntilItIsReplaced(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	host := sshhost.Start(t, sshhost.Password(hostPassword))
	rebuilt := sshhost.Start(t, sshhost.Password(hostPassword))
	added := addHostAt(t, h.Koko, "web1", host)
	enroll(t, h, "web1")
	h.Koko("host", "set", "web1", "account=root").WantSuccess(t)
	h.Koko("host", "set", "web1", "account="+sshhost.Account, "address="+rebuilt.Address(), "port="+strconv.Itoa(rebuilt.Port())).WantSuccess(t)
	old, offered := fingerprint(host), fingerprint(rebuilt)

	r := h.KokoStdin(hostPassword, "host", "enroll", "web1", "--password-stdin", "--json")
	msg := r.WantError(t, 1, "unexpected_host_key")
	harness.Golden(t, "host-enroll-unexpected-host-key.json", r.Stderr)
	if want := fmt.Sprintf("does not match the one pinned at Enroll (pinned %s, offered %s); koko did not log in", old, offered); !strings.Contains(msg, want) {
		t.Errorf("the refusal %q does not say %q", msg, want)
	}
	for _, way := range []string{"--host-key", "--replace-host-key"} {
		if !strings.Contains(msg, way) {
			t.Errorf("the refusal %q does not name %s", msg, way)
		}
	}
	h.KokoStdin(hostPassword, "host", "enroll", "web1", "--password-stdin", "--host-key", old, "--json").WantError(t, 1, "unexpected_host_key")
	if logins := rebuilt.Logins(); len(logins) != 0 {
		t.Errorf("the rebuilt Host received the logins %+v, want none", logins)
	}
	wantPinned(t, showHost(t, h.Koko, "web1"), "registered", old)
	if !strings.Contains(h.Log(), `msg="enroll refused the host key" host=web1`) {
		t.Errorf("the log holds no line for the refused host key:\n%s", h.Log())
	}

	wantPinned(t, enroll(t, h, "web1", "--replace-host-key"), "managed", offered)
	wantOneLoginWith(t, rebuilt, "password", nil)
	want := fmt.Sprintf(`msg="host key re-pinned" host=web1 host_id=%s user=%s enrolled_by="unix socket" old=%s new=%s`, added.ID, harness.Owner, old, offered)
	if !strings.Contains(h.Log(), want) {
		t.Errorf("the log holds no line %s:\n%s", want, h.Log())
	}
}

// A Managed Host that was rebuilt, as the Gateway finds it, Enrolls again
// to pin its new host key: Enrolled plainly it is refused as Managed
// already, and --host-key naming the new key, or --replace-host-key, pins
// the key it offers now and keeps it Managed.
func TestAManagedHostEnrollsAgainToPinANewHostKey(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	host := sshhost.Start(t, sshhost.Password(hostPassword))
	addHostAt(t, h.Koko, "web1", host)
	enroll(t, h, "web1")
	for _, way := range []string{"--host-key", "--replace-host-key"} {
		t.Run(way, func(t *testing.T) {
			rebuilt := sshhost.Start(t, sshhost.Password(hostPassword))
			moved := decodeHost(t, h.Koko("host", "set", "web1", "address="+rebuilt.Address(), "port="+strconv.Itoa(rebuilt.Port()), "--json"))
			wantPinned(t, moved, "managed", *showHost(t, h.Koko, "web1").HostKey)
			h.KokoStdin(hostPassword, "host", "enroll", "web1", "--password-stdin", "--json").WantError(t, 1, "already_managed")
			args := []string{way}
			if way == "--host-key" {
				args = append(args, fingerprint(rebuilt))
			}
			wantPinned(t, enroll(t, h, "web1", args...), "managed", fingerprint(rebuilt))
			wantOneLoginWith(t, rebuilt, "password", nil)
			kokoKeyLine(t, rebuilt)
		})
	}
}

// Enroll appends the koko key and changes nothing else in authorized_keys:
// it ends a last line that lacks its newline first, and leaves the file as
// it is when a line holds the koko key already, whatever its options and
// comment; a line that is a comment does not count.
func TestEnrollChangesNothingElseInAuthorizedKeys(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	first := sshhost.Start(t, sshhost.Password(hostPassword))
	addHostAt(t, h.Koko, "web0", first)
	enroll(t, h, "web0")
	kokoLine := kokoKeyLine(t, first)
	kokoKey := strings.TrimSuffix(kokoLine, " koko")
	mine := strings.TrimSuffix(string(ssh.MarshalAuthorizedKey(newUserKey(t, "").pub)), "\n") + " me@laptop"

	for i, tc := range []struct {
		name   string
		before string
		want   string
	}{
		{"keys and comments", "# my keys\n" + mine + "\n\n", "# my keys\n" + mine + "\n\n" + kokoLine + "\n"},
		{"a last line without its newline", mine, mine + "\n" + kokoLine + "\n"},
		{"the koko key with options and another comment", "# mine\n" + `from="10.0.0.0/8" ` + kokoKey + " koko@old\n", "# mine\n" + `from="10.0.0.0/8" ` + kokoKey + " koko@old\n"},
		{"the koko key commented out", "# " + kokoLine + "\n", "# " + kokoLine + "\n" + kokoLine + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := sshhost.Start(t, sshhost.Password(hostPassword))
			dir := filepath.Join(host.Home(), ".ssh")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "authorized_keys"), []byte(tc.before), 0o600); err != nil {
				t.Fatal(err)
			}
			name := "web" + strconv.Itoa(i+1)
			addHostAt(t, h.Koko, name, host)
			wantPinned(t, enroll(t, h, name), "managed", fingerprint(host))
			if got := string(host.AuthorizedKeys()); got != tc.want {
				t.Errorf("authorized_keys holds %q, want %q", got, tc.want)
			}
		})
	}
}

// A Host that refuses the login, cannot be reached, or cannot take the
// koko key stays Registered with no pin, and the log says why.
func TestAFailedEnrollLeavesTheHostRegistered(t *testing.T) {
	t.Parallel()
	h := harness.Start(t, harness.Timings(timing.Timings{HostConnectWait: time.Millisecond}))
	refusing := sshhost.Start(t, sshhost.Password("another password"))
	addHostAt(t, h.Koko, "web1", refusing)
	unwritable := sshhost.Start(t, sshhost.Password(hostPassword))
	if err := os.MkdirAll(filepath.Join(unwritable.Home(), ".ssh", "authorized_keys"), 0o700); err != nil {
		t.Fatal(err)
	}
	addHostAt(t, h.Koko, "web2", unwritable)
	down, port, err := net.SplitHostPort(harness.FreeAddr(t))
	if err != nil {
		t.Fatal(err)
	}
	addHost(t, h.Koko, "web3", "address="+down, "port="+port, "account="+sshhost.Account)

	for _, tc := range []struct {
		host, code, says string
	}{
		{"web1", "login_refused", `web1 refused the password for its Host Account "deploy"`},
		{"web2", "key_not_added", "could not add the koko key to ~/.ssh/authorized_keys"},
		{"web3", "host_unreachable", "koko could not connect to web3"},
	} {
		r := h.KokoStdin(hostPassword, "host", "enroll", tc.host, "--password-stdin", "--json")
		if msg := r.WantError(t, 1, tc.code); !strings.Contains(msg, tc.says) {
			t.Errorf("%s: the refusal %q does not say %q", tc.host, msg, tc.says)
		}
		wantPinned(t, showHost(t, h.Koko, tc.host), "registered", "")
		if want := fmt.Sprintf(`msg="enroll failed" host=%s`, tc.host); !strings.Contains(h.Log(), want) || !strings.Contains(h.Log(), "code="+tc.code) {
			t.Errorf("the log holds no line %s with code=%s:\n%s", want, tc.code, h.Log())
		}
	}
	if n := len(refusing.Logins()); n != 1 {
		t.Errorf("the Host that refused the password received %d logins, want 1: a refused login is never retried", n)
	}
}

// host rm is refused for a Host that has been Managed, as its pin shows,
// even once a Host Account change has sent it back to Registered: it can
// only be kept on record.
func TestHostRmIsRefusedOnceAHostHasAPin(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	host := sshhost.Start(t, sshhost.Password(hostPassword))
	addHostAt(t, h.Koko, "web1", host)
	enroll(t, h, "web1")
	r := h.Koko("host", "rm", "web1", "--json")
	r.WantError(t, 1, "host_was_managed")
	harness.Golden(t, "host-rm-was-managed.json", r.Stderr)
	h.Koko("host", "set", "web1", "account=root").WantSuccess(t)
	h.Koko("host", "rm", "web1", "--json").WantError(t, 1, "host_was_managed")
	wantPinned(t, showHost(t, h.Koko, "web1"), "registered", fingerprint(host))
}

// The caller dropping its call does not cancel a running Enroll: the login
// it began goes on, the koko key is added, and the Host ends up Managed.
func aDroppedCallDoesNotCancelARunningEnroll(t *testing.T) {
	h := harness.Start(t)
	host := sshhost.Start(t, sshhost.Password(hostPassword), sshhost.SlowLogins(300*time.Millisecond))
	addHostAt(t, h.Koko, "web1", host)
	conn, err := serverconn.DialSocket(t.Context(), h.Socket())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	var dialled atomic.Bool
	c := apiclient.New(func(context.Context) (serverconn.Conn, error) {
		if dialled.Swap(true) {
			return nil, errors.New("the caller is gone")
		}
		return conn, nil
	})
	t.Cleanup(c.Close)
	done := inBackground(t, func() error {
		_, err := c.Call(t.Context(), api.HostEnroll, api.HostEnrollRequest{Host: "web1", Password: api.EnrollInput(hostPassword)})
		return err
	})
	eventually(t, "the koko server connects to the Host", func() bool { return host.Conns() == 1 })
	_ = conn.Close()
	if err := <-done; err == nil {
		t.Fatal("the dropped call succeeded")
	}
	eventually(t, "web1 is Managed", func() bool { return showHost(t, h.Koko, "web1").State == "managed" })
	kokoKeyLine(t, host)
	wantOneLoginWith(t, host, "password", nil)
}

// The login runs outside the write's transaction, so a Host whose Host
// Account changes meanwhile is not recorded as Managed: the koko key went
// to the Host Account Enroll logged in as.
func enrollRecordsNothingWhenTheHostChangesMeanwhile(t *testing.T) {
	h := harness.Start(t)
	host := sshhost.Start(t, sshhost.Password(hostPassword), sshhost.SlowLogins(500*time.Millisecond))
	addHostAt(t, h.Koko, "web1", host)
	done := inBackground(t, func() harness.Result {
		return h.KokoStdin(hostPassword, "host", "enroll", "web1", "--password-stdin", "--json")
	})
	eventually(t, "the koko server connects to the Host", func() bool { return host.Conns() == 1 })
	h.Koko("host", "set", "web1", "account=root").WantSuccess(t)
	msg := (<-done).WantError(t, 1, "host_changed")
	for _, want := range []string{`the authorized_keys of "deploy"`, "koko host enroll web1 again"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal %q does not say %q", msg, want)
		}
	}
	wantPinned(t, showHost(t, h.Koko, "web1"), "registered", "")
	kokoKeyLine(t, host)
	if !strings.Contains(h.Log(), `msg="enroll failed" host=web1`) || !strings.Contains(h.Log(), "code=host_changed") {
		t.Errorf("the log does not say the Enroll failed with host_changed:\n%s", h.Log())
	}
}

// inBackground runs f in a goroutine and returns where its result arrives.
// The test waits for f to end before it ends.
func inBackground[T any](t *testing.T, f func() T) <-chan T {
	t.Helper()
	result, finished := make(chan T, 1), make(chan struct{})
	go func() {
		defer close(finished)
		result <- f()
	}()
	t.Cleanup(func() { <-finished })
	return result
}

// eventually waits up to 10 s for cond, which says what, to hold.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !cond(); {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not within 10 s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Neither the password, the key, nor the key's passphrase supplied to
// Enroll reaches any output, the koko server's log or its database, in any
// form it travels in, for an Enroll that succeeds or fails.
func TestNoEnrollInputIsStoredOrLogged(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	h := harness.Start(t, harness.DataDir(dataDir))
	const passphrase = "open sesame, but only here"
	plain, encrypted := newUserKey(t, ""), newUserKey(t, passphrase)
	var outputs []harness.Result

	byPassword := sshhost.Start(t, sshhost.Password(hostPassword))
	addHostAt(t, h.Koko, "web1", byPassword)
	outputs = append(outputs,
		h.KokoStdin("not the password", "host", "enroll", "web1", "--password-stdin", "--json"),
		h.KokoStdin(hostPassword, "host", "enroll", "web1", "--password-stdin"))
	byKey := sshhost.Start(t, sshhost.AuthorizedKey(plain.pub))
	addHostAt(t, h.Koko, "web2", byKey)
	outputs = append(outputs, h.Koko("host", "enroll", "web2", "--key", plain.path))
	byEncryptedKey := sshhost.Start(t, sshhost.AuthorizedKey(encrypted.pub))
	addHostAt(t, h.Koko, "web3", byEncryptedKey)
	outputs = append(outputs, h.KokoTerminal(harness.NewTerminal(passphrase), "host", "enroll", "web3", "--key", encrypted.path, "--json"))
	for _, host := range []string{"web1", "web2", "web3"} {
		if got := showHost(t, h.Koko, host); got.State != "managed" {
			t.Fatalf("%s is %s, want it Enrolled", host, got.State)
		}
	}

	secrets := map[string]string{"a private key": "PRIVATE KEY"}
	for name, value := range map[string]string{
		"the password":       hostPassword,
		"the wrong password": "not the password",
		"the passphrase":     passphrase,
		"a private key file": "-----BEGIN OPENSSH PRIVATE KEY-----\n",
	} {
		secrets[name] = value
		secrets[name+" in base64"] = base64.StdEncoding.EncodeToString([]byte(value))
	}
	for name, k := range map[string]userKey{"the plain key": plain, "the encrypted key": encrypted} {
		body := strings.Split(string(k.file), "\n")[1]
		secrets[name+"'s file"] = body
		secrets[name+"'s file in base64"] = base64.StdEncoding.EncodeToString(k.file)[:60]
	}
	running := readDir(t, dataDir)
	h.Stop()
	places := map[string][]byte{"the log": []byte(h.Log())}
	for i, r := range outputs {
		places[fmt.Sprintf("the output of Enroll %d", i+1)] = []byte(r.Stdout + r.Stderr)
	}
	for file, content := range running {
		places[file+" while running"] = content
	}
	for file, content := range readDir(t, dataDir) {
		places[file+" once stopped"] = content
	}
	if len(running[store.FileName+"-wal"]) == 0 {
		t.Fatalf("the running koko server's data directory holds no -wal to check: %v", running)
	}
	for place, content := range places {
		for name, secret := range secrets {
			if bytes.Contains(content, []byte(secret)) {
				t.Errorf("%s holds %s", place, name)
			}
		}
	}
}
