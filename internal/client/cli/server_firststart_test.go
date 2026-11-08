//go:build unix

package cli_test

import (
	"database/sql"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
)

// firstStart runs `koko server run` in the background with the koko
// server's real start-up on dataDir and the Root Key file keyFile, with
// args such as --owner and --address, serving on socket until the test
// stops it.
func firstStart(t *testing.T, socket, dataDir, keyFile string, args ...string) *harness.Process {
	t.Helper()
	return harness.RunBackground(t, cli.Runner{ServerRun: harness.ServerServeWith(t, harness.SocketPath(socket))}, nil,
		append([]string{"server", "run", "--data-dir", dataDir, "--root-key-file", keyFile}, args...)...)
}

// local runs one koko command line as the Local caller on the koko server's
// machine whose socket is socket.
func local(t *testing.T, socket string, args ...string) harness.Result {
	t.Helper()
	return harness.Run(t, cli.Runner{SocketPath: socket}, []string{"HOME=" + t.TempDir()}, args...)
}

// waitServing waits up to 10 s for the koko server on socket to answer
// koko server show, and returns what it printed. Each answer before that
// must not be not_initialised: a koko server started with --owner answers
// nothing before its initialisation has committed.
func waitServing(t *testing.T, p *harness.Process, socket string) serverOut {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; {
		r := local(t, socket, "server", "show", "--json")
		if r.Status == 0 {
			return parseServer(t, r)
		}
		if strings.Contains(r.Stderr, "not_initialised") {
			t.Fatalf("a caller got not_initialised during the first start: %s", r.Stderr)
		}
		if time.Now().After(deadline) {
			got := p.Stop()
			t.Fatalf("the koko server did not answer within 10 s: last %q; koko server run exited %d, stderr %q", r.Stderr, got.Status, got.Stderr)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// joinCodeRows counts the Join Codes recorded in the data directory's
// koko.db.
func joinCodeRows(t *testing.T, dataDir string) int {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "koko.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM join_codes").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// rootKeyLine matches a Root Key file's content: one line, <id>:<key>.
var rootKeyLine = regexp.MustCompile(`^([a-z]+_[0-9a-z]+):[A-Za-z0-9_-]+=*\n$`)

// A fresh koko server started with --owner and --address writes its missing
// Root Key, one line, 0600, as the process's user, and comes up initialised
// with no Join Code issued. koko device add over the socket issues the
// first, and a device joins with it, pinning the host key the log named.
// The log names the host key, the listen address, the Root Key's path and
// id and the koko device add line, and holds neither the Root Key nor a
// Join Code (P1 spec: the container image, stories 3, 4, 13, 14, 15, 18).
func TestServerRunInitialisesAFreshKokoServerAtItsFirstStart(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dataDir, keyFile := filepath.Join(root, "data"), filepath.Join(root, "keys", "root.key")
	socket := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	p := firstStart(t, socket, dataDir, keyFile, "--owner", "alice", "--address", harness.Address)
	show := waitServing(t, p, socket)

	content, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatalf("the first start wrote no Root Key: %v", err)
	}
	m := rootKeyLine.FindSubmatch(content)
	if m == nil {
		t.Fatal("the Root Key file is not one <id>:<key> line")
	}
	id := string(m[1])
	info, err := os.Stat(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("the Root Key file's mode is %v, want 0600", info.Mode().Perm())
	}
	if uid := info.Sys().(*syscall.Stat_t).Uid; int(uid) != os.Geteuid() {
		t.Errorf("the Root Key file is uid %d's, want the process's, %d", uid, os.Geteuid())
	}
	if beside := files(t, filepath.Dir(keyFile)); !slices.Equal(beside, []string{".", "root.key"}) {
		t.Errorf("the Root Key's directory holds %v, want only root.key", beside)
	}
	if show.Address != harness.Address || show.RootKeyFile != keyFile || show.RootKeyID != id || show.DataDir != dataDir {
		t.Errorf("server show = %+v, want the address %s, the Root Key %s (%s) and the data directory %s",
			show, harness.Address, keyFile, id, dataDir)
	}
	if n := joinCodeRows(t, dataDir); n != 0 {
		t.Errorf("the first start recorded %d Join Codes, want none", n)
	}

	r := local(t, socket, "device", "add", "--json")
	var jc joinCodeOut
	r.WantSuccess(t)
	if err := json.Unmarshal([]byte(r.Stdout), &jc); err != nil {
		t.Fatalf("device add --json printed %q: %v", r.Stdout, err)
	}
	if jc.User != "alice" {
		t.Errorf("the first Join Code is for %s, want the Owner, alice", jc.User)
	}
	d := harness.NewDevice(t)
	d.Koko("join", jc.Code).WantSuccess(t)
	d.Koko("user", "ls").WantSuccess(t)
	pinned := readClientConfig(t, d).HostKeys
	if len(pinned) != 1 {
		t.Fatalf("the device pinned %d host keys, want 1", len(pinned))
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pinned[0]))
	if err != nil {
		t.Fatal(err)
	}

	log := p.Stop()
	if log.Status != 0 {
		t.Errorf("koko server run exited %d", log.Status)
	}
	initialised := regexp.MustCompile(`msg="koko server initialised" host_key=` + regexp.QuoteMeta(ssh.FingerprintSHA256(key)) +
		` listen=127\.0\.0\.1:\d+ root_key_file=` + regexp.QuoteMeta(keyFile) + ` root_key=` + id + "\n")
	if !initialised.MatchString(log.Stderr) {
		t.Errorf("the log of the first start holds no line matching %s:\n%s", initialised, strings.ReplaceAll(log.Stderr, string(content), "<root key>"))
	}
	for _, want := range []string{
		"off this machine",
		"docker exec <container> koko device add",
	} {
		if !strings.Contains(log.Stderr, want) {
			t.Errorf("the log of the first start does not hold %q:\n%s", want, strings.ReplaceAll(log.Stderr, string(content), "<root key>"))
		}
	}
	wantNoRootKey(t, log, content)
	secret := strings.TrimPrefix(jc.Code, "koko")
	if strings.Contains(log.Stderr, jc.Code) || strings.Contains(log.Stderr, secret[len(secret)/2:]) {
		t.Error("the log holds the Join Code")
	}
}

// A caller dialling the socket while a fresh koko server initialises itself
// at its first start is never answered not_initialised: the socket opens
// only once initialisation has committed (P1 spec: the container image).
func TestServerRunAnswersNothingBeforeItsFirstStartHasInitialised(t *testing.T) {
	t.Parallel()
	for range 3 {
		root := t.TempDir()
		socket := filepath.Join(harness.ShortTempDir(t), "koko.sock")
		p := firstStart(t, socket, filepath.Join(root, "data"), filepath.Join(root, "root.key"), "--owner", "alice", "--address", harness.Address)
		waitServing(t, p, socket)
		p.Stop()
	}
}

// An existing Root Key file on a fresh data directory is reused, left byte
// for byte; one that does not load is refused, left byte for byte, and
// nothing is written beside it.
func TestServerRunReusesTheRootKeyFileAFreshKokoServerFinds(t *testing.T) {
	t.Parallel()
	t.Run("one that loads", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		keyFile := filepath.Join(root, "root.key")
		id, _ := newKeyFile(t, keyFile)
		before, _ := os.ReadFile(keyFile)
		r := serverRunInit(t, filepath.Join(root, "data"), keyFile, "alice", harness.Address)
		if r.Status != 0 || !strings.Contains(r.Stderr, `msg="koko server initialised"`) || !strings.Contains(r.Stderr, "root_key="+id) {
			t.Fatalf("exit status %d, stderr %q", r.Status, strings.ReplaceAll(r.Stderr, string(before), "<root key>"))
		}
		if after, _ := os.ReadFile(keyFile); string(after) != string(before) {
			t.Error("the first start changed the Root Key file it reused")
		}
	})
	t.Run("one that does not load", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		keyFile := filepath.Join(root, "keys", "root.key")
		_, key := newKeyFile(t, keyFile)
		content := []byte(key + "\n") // the key without the id its file names
		if err := os.WriteFile(keyFile, content, 0o600); err != nil {
			t.Fatal(err)
		}
		r := serverRunInit(t, filepath.Join(root, "data"), keyFile, "alice", harness.Address)
		refused(t, r, key, "the Root Key file "+keyFile+" does not parse")
		if got, _ := os.ReadFile(keyFile); string(got) != string(content) {
			t.Error("the refusal changed the Root Key file")
		}
		if got := files(t, filepath.Dir(keyFile)); !slices.Equal(got, []string{".", "root.key"}) {
			t.Errorf("the refusal wrote %v beside the Root Key file", got)
		}
	})
}

// serverRunInit runs `koko server run` with --owner and --address through
// the koko server's real start-up, which stops once it has started.
func serverRunInit(t *testing.T, dataDir, keyFile, owner, address string) harness.Result {
	t.Helper()
	return harness.Run(t, cli.Runner{ServerRun: harness.ServerStart(t)}, nil,
		"server", "run", "--data-dir", dataDir, "--root-key-file", keyFile, "--owner", owner, "--address", address)
}

// An initialised data directory whose Root Key was removed is refused at
// start, --owner or not, naming the id the sealed data needs, and no Root
// Key is made over the sealed data (P1 spec: the container image, story 20).
func TestServerRunNeverMakesARootKeyOverSealedData(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dataDir, keyFile := filepath.Join(root, "data"), filepath.Join(root, "etc", "root.key")
	id, _ := newKeyFile(t, keyFile)
	initialise(t, dataDir, keyFile)
	if err := os.Remove(keyFile); err != nil {
		t.Fatal(err)
	}
	before := contents(t, root)
	r := serverRunInit(t, dataDir, keyFile, "alice", harness.Address)
	refused(t, r, "", "the Root Key file "+keyFile+" is missing; the sealed data needs "+id)
	if c := changed(before, contents(t, root)); len(c) > 0 {
		t.Errorf("the refusal changed %v", c)
	}
	// The key's directory read-only, where no key would be made anyway, the
	// refusal still names the Root Key the sealed data needs.
	if err := os.Chmod(filepath.Dir(keyFile), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(keyFile), 0o700) })
	r = serverRunInit(t, dataDir, keyFile, "alice", harness.Address)
	refused(t, r, "", "the Root Key file "+keyFile+" is missing; the sealed data needs "+id)
}

// An address server.init refuses, here one too long for a Join Code to
// carry with its port, stops the first start with that refusal. The Root
// Key it wrote stays, and the next start, with a good address, initialises
// on it, so a fix costs one more start (P1 spec: the container image).
func TestServerRunStopsOnARefusedInitialisationAndKeepsItsRootKey(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dataDir, keyFile := filepath.Join(root, "data"), filepath.Join(root, "root.key")
	label := strings.Repeat("a", 63)
	long := label + "." + label + "." + label + "." + label[:61] // a 253-byte name, 258 bytes with its port
	r := serverRunInit(t, dataDir, keyFile, "alice", long)
	written, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatalf("the refused initialisation left no Root Key: %v", err)
	}
	_, key, _ := strings.Cut(strings.TrimSpace(string(written)), ":")
	refused(t, r, key, "a Join Code cannot carry the Server Address")
	if !strings.Contains(r.Stderr, "at most 255") {
		t.Errorf("stderr %q does not say how long an address a Join Code carries", r.Stderr)
	}

	r = serverRunInit(t, dataDir, keyFile, "alice", harness.Address)
	if r.Status != 0 || !strings.Contains(r.Stderr, `msg="koko server initialised"`) {
		t.Fatalf("the next start: exit status %d, stderr %q", r.Status, strings.ReplaceAll(r.Stderr, string(written), "<root key>"))
	}
	if again, _ := os.ReadFile(keyFile); string(again) != string(written) {
		t.Error("the next start made another Root Key")
	}
}

// Once initialised, a koko server does not use --owner and --address: when
// either differs from the record it logs a warning naming koko server set
// address=, starts, and keeps the record; equal values log nothing (P1
// spec: the container image, story 16).
func TestServerRunInitialisedAlreadyKeepsItsRecord(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dataDir, keyFile := filepath.Join(root, "data"), filepath.Join(root, "root.key")
	newKeyFile(t, keyFile)
	initialise(t, dataDir, keyFile)

	for _, tc := range []struct {
		name, owner, address string
		warns                []string
	}{
		{"another Owner and address", "bob", "koko.example.com:443", []string{
			"level=WARN", "koko server set address=<host[:port]>",
			"owner=bob recorded_owner=" + harness.Owner, "address=koko.example.com:443 recorded_address=" + harness.Address,
		}},
		{"another address", harness.Owner, "koko.example.com", []string{
			"level=WARN", "koko server set address=<host[:port]>", "address=koko.example.com recorded_address=" + harness.Address,
		}},
		{"the same", harness.Owner, harness.Address, nil},
	} {
		r := serverRunInit(t, dataDir, keyFile, tc.owner, tc.address)
		if r.Status != 0 || !strings.Contains(r.Stderr, "initialised=true") {
			t.Fatalf("%s: exit status %d, stderr %q", tc.name, r.Status, r.Stderr)
		}
		for _, want := range tc.warns {
			if !strings.Contains(r.Stderr, want) {
				t.Errorf("%s: the log does not hold %q:\n%s", tc.name, want, r.Stderr)
			}
		}
		if tc.warns == nil && strings.Contains(r.Stderr, "level=WARN") {
			t.Errorf("%s: the log warns:\n%s", tc.name, r.Stderr)
		}
		if strings.Contains(r.Stderr, `msg="koko server initialised"`) {
			t.Errorf("%s: an initialised koko server initialised again:\n%s", tc.name, r.Stderr)
		}
	}

	h := harness.Start(t, harness.NoInit(), harness.DataDir(dataDir), harness.RootKeyFile(keyFile))
	if s := parseServer(t, h.Koko("server", "show", "--json")); s.Address != harness.Address {
		t.Errorf("the Server Address is %s, want the recorded %s", s.Address, harness.Address)
	}
	users := h.Koko("user", "ls")
	if users.Status != 0 || !strings.Contains(users.Stdout, harness.Owner) || strings.Contains(users.Stdout, "bob") {
		t.Errorf("user ls = %q, want the recorded Owner, %s, alone", users.Stdout, harness.Owner)
	}
}

// Without --owner, a missing Root Key is refused as before, and the refusal
// says that a fresh koko server makes its own when started with KOKO_OWNER
// and KOKO_ADDRESS.
func TestServerRunWithoutOwnerNamesTheSettingsThatMakeARootKey(t *testing.T) {
	t.Parallel()
	keyFile := filepath.Join(t.TempDir(), "root.key")
	r := serverRun(t, t.TempDir(), keyFile)
	refused(t, r, "", "the Root Key file "+keyFile+" is missing")
	if !strings.Contains(r.Stderr, "KOKO_OWNER") || !strings.Contains(r.Stderr, "KOKO_ADDRESS") {
		t.Errorf("stderr %q does not name KOKO_OWNER and KOKO_ADDRESS", r.Stderr)
	}
}

// A directory start-up cannot write, where it must make the Root Key, is
// refused before anything is written, naming it and the uid and gid the
// koko server runs as (P1 spec: the container image, story 9).
func TestServerRunRefusesARootKeyDirectoryItCannotWrite(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root writes a read-only directory")
	}
	root := t.TempDir()
	readOnly := filepath.Join(root, "read-only")
	if err := os.Mkdir(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) })
	ids := fmt.Sprintf("uid %d and gid %d", os.Geteuid(), os.Getegid())
	for _, keyFile := range []string{filepath.Join(readOnly, "root.key"), filepath.Join(readOnly, "keys", "root.key")} {
		before := files(t, root)
		r := harness.Run(t, cli.Runner{ServerRun: harness.ServerStart(t)}, nil, "server", "run", "--json",
			"--data-dir", filepath.Join(root, "data"), "--root-key-file", keyFile, "--owner", "alice", "--address", harness.Address)
		msg := r.WantError(t, 1, "internal_error")
		for _, want := range []string{readOnly + ", where it would make the Root Key file " + keyFile, ids} {
			if !strings.Contains(msg, want) {
				t.Errorf("message %q does not name %q", msg, want)
			}
		}
		if after := files(t, root); !slices.Equal(after, before) {
			t.Errorf("the refused start wrote files: %v, before %v", after, before)
		}
	}
}

// --owner and --address belong to koko server run, which stays in the
// irreversible tier (ADR 0019): with them it makes a Root Key and
// initialises the koko server.
func TestServerRunWithItsInitialisationSettingsIsIrreversible(t *testing.T) {
	t.Parallel()
	for _, c := range cli.Tree() {
		if c.Path() != "server run" {
			continue
		}
		if c.Tier != cli.Irreversible {
			t.Errorf("koko server run is in the %v tier, want irreversible", c.Tier)
		}
		var envs []string
		for _, f := range c.Flags {
			envs = append(envs, f.Env)
		}
		for _, env := range []string{"KOKO_OWNER", "KOKO_ADDRESS"} {
			if !slices.Contains(envs, env) {
				t.Errorf("koko server run takes no setting from %s", env)
			}
		}
		return
	}
	t.Error("the command tree lacks koko server run")
}
