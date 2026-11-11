package harness

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/server"
	"github.com/cieons/koko/internal/server/installer"
	"github.com/cieons/koko/internal/server/kokod"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/build"
	"github.com/cieons/koko/internal/shared/install"
	"github.com/cieons/koko/internal/shared/portowner"
	"github.com/cieons/koko/internal/shared/release"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// SourceIP is a fake machine's default route's source IP until a test
// changes it: a public address, from a range kept for documentation (RFC
// 5737), so it reaches no one.
const SourceIP = "203.0.113.7"

// NoSocketWait is how long install waits, through the timing hook, for the
// socket of a koko service that never opens it (Machine.NoSocket), in place
// of timing.InstallSocketWait. No wait would see that socket, so a short one
// fails no slower machine; every other run waits the product's time.
const NoSocketWait = 200 * time.Millisecond

// servicePID is the process ID a fake machine's systemd gives the koko
// service while it runs.
const servicePID = 4242

// dropInDir holds the koko unit's drop-ins, as systemctl edit koko writes
// them.
const dropInDir = installer.UnitPath + ".d"

// Machine is a fake machine for `koko server install` and `uninstall`, the
// system seam of P0 spec: install and run: a temporary directory that stands
// for its root, and fakes of everything else koko and kokod install ask of
// it, which record each call. It is the cli.Kokod koko runs on: it states
// the build of each kokod file it makes, by the file's bytes, so that a copy
// keeps its build, and runs a kokod file as kokod's command line in process.
// Its systemctl runs the koko unit as systemd would, with the unit's own
// settings and the paths under the root: `kokod run` in process, as an
// in-process koko server. Everything it starts stops when the test ends.
//
// A test sets what the machine answers before install runs; the zero values
// of the fields NewMachine sets mean a machine without them.
type Machine struct {
	// OSArch is the machine's platform, as GOOS/GOARCH.
	OSArch string
	// NotRoot makes koko run as a user who is not root.
	NotRoot bool
	// Systemd is systemd's version.
	Systemd int
	// SystemdErr, when set, is how reading systemd's version fails.
	SystemdErr error
	// Source is the default route's source IP; the zero Addr means the
	// machine has no default route.
	Source netip.Addr
	// FSType is the type of the filesystem the data directory is on.
	FSType string
	// Listening holds the process listening on each TCP port, beside the
	// koko service, which listens on its unit's port while it runs.
	Listening map[int]portowner.Process
	// StartDelay is how long the koko service takes to open its socket once
	// systemctl has started it.
	StartDelay time.Duration
	// ListenDelay is how long the koko service's socket file is there
	// before the koko server accepts connections on it: the moment between
	// the bind and the listen of its start-up, which a loaded machine
	// stretches.
	ListenDelay time.Duration
	// NoSocket makes the koko service start but never open its socket, as
	// one whose start-up refuses does under Restart=on-failure. Install
	// then waits NoSocketWait for it.
	NoSocket bool

	t    testing.TB
	root string
	home string
	// kokod is the kokod file koko server install is given with --kokod,
	// outside the root.
	kokod string
	// tmp is the TMPDIR koko runs with, outside the root.
	tmp string
	// builds are the builds of the files the machine made, by their bytes.
	builds map[string]build.Info
	// newReleaseFiles counts the kokod files NewRelease made.
	newReleaseFiles int
	// releases is the fake GitHub koko downloads from, once Release made
	// one.
	releases *Releases
	// exe is the real path of koko's own file on the machine, once KokoAt
	// made one.
	exe string
	// ran are the kokod command lines run on the machine, in order.
	ran []string

	mu sync.Mutex
	// self is the build of koko on the machine.
	self    build.Info
	calls   []Call
	owners  []owned
	users   []string
	loaded  *unitFile
	enabled bool
	service *Process
	// running is the unit the koko service runs, as loaded when it started.
	running *unitFile
	runs    []*Process
	// behaviours are how the koko service runs each kokod file the
	// machine made that is not as a kokod runs, by the file's bytes.
	behaviours map[string]behaviour
	// helper is the upgrade helper's units as loaded, nil while systemd
	// has none.
	helper *helperUnits
	// helperEnabled tells whether the helper's path unit is enabled, and
	// watch, while it is started, stops it.
	helperEnabled bool
	watch         *pathWatch
	// helperLog is what every run of the upgrade helper printed, and
	// helperRuns how many runs have ended.
	helperLog  strings.Builder
	helperRuns int
}

// behaviour is how the koko service runs a kokod file that does not run as
// a kokod does.
type behaviour struct {
	// noSocket: the service starts, and never opens its socket.
	noSocket bool
	// migrates: before that, it migrates koko.db one schema version beyond
	// this koko's, snapshot first, as a newer kokod does at its start.
	migrates bool
}

// helperUnits is what the fake systemd reads of the upgrade helper's path
// unit and service.
type helperUnits struct {
	// pathExists is the file whose presence starts the service.
	pathExists string
	// execStart is the service's command line.
	execStart []string
}

// pathWatch is the upgrade helper's path unit while it is started: it runs
// the helper's service each time its file is there.
type pathWatch struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// Call is one thing install asked of the machine, with the files under its
// root at that moment, as the machine names them.
type Call struct {
	What  string
	Files []string
}

// owned is a file the machine was asked to give an owner, known by its
// FileInfo so that it keeps its owner across a rename, as a chown does.
type owned struct {
	info  os.FileInfo
	owner string
}

// unitFile is what the fake systemd reads of the koko unit and its
// drop-ins.
type unitFile struct {
	execStart   []string
	env         []string
	credentials [][2]string // name, path
	user        string
	runtimeDirs []string
	stateDirs   []string
}

var (
	_ installer.Machine = (*Machine)(nil)
	_ cli.Kokod         = (*Machine)(nil)
)

// skeleton is what a fresh machine's root holds: the system directories
// install writes into, and /run/systemd/system, which tells that systemd
// runs the machine.
var skeleton = []string{"/etc/systemd/system", "/usr/local/bin", "/var/lib", "/run/systemd/system"}

// NewMachine makes a fresh fake machine: a root holding only the system
// directories, linux/amd64 with koko, of the test binary's build, running
// as root, systemd 252, an ext4 filesystem, SourceIP on the default route
// and no port taken, with a kokod file of koko's build outside its root, for
// --kokod, and no release to download from.
func NewMachine(t testing.TB) *Machine {
	t.Helper()
	m := &Machine{
		OSArch:    "linux/amd64",
		Systemd:   252,
		Source:    netip.MustParseAddr(SourceIP),
		FSType:    "ext4",
		Listening: map[int]portowner.Process{},
		t:         t,
		// The socket under the root must fit macOS's 104 bytes.
		root:       ShortTempDir(t),
		home:       t.TempDir(),
		tmp:        t.TempDir(),
		builds:     map[string]build.Info{},
		behaviours: map[string]behaviour{},
		self:       build.Read(),
	}
	m.kokod = m.NewKokod(m.kokodBuild())
	for _, dir := range skeleton {
		if err := os.MkdirAll(m.Path(dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(m.stop)
	return m
}

// KokodBuild is the build of a kokod of this koko's build: the test
// binary's, which koko server install compares a kokod's build with, naming
// kokod's main package, for linux/amd64, a fresh machine's platform.
func KokodBuild() build.Info {
	b := build.Read()
	b.Path, b.Platform = install.KokodPackage, "linux/amd64"
	return b
}

// kokodBuild is the build of a kokod of the build of koko on the machine,
// for linux/amd64, a fresh machine's platform.
func (m *Machine) kokodBuild() build.Info {
	b := KokodBuild()
	m.mu.Lock()
	b.Version, b.Revision = m.self.Version, m.self.Revision
	m.mu.Unlock()
	return b
}

// Koko runs one koko command line in process, as root on this machine under
// a temporary HOME and TMPDIR, with empty stdin: `koko server install` and
// `uninstall` act on it, downloading from the fake GitHub Release made, and
// every other command reaches the koko service's socket under its root.
func (m *Machine) Koko(args ...string) Result {
	m.t.Helper()
	return m.KokoEnv(nil, args...)
}

// KokoEnv runs one koko command line on the machine, as Koko does, with the
// variables env beside the machine's own, such as KOKO_IMAGE.
func (m *Machine) KokoEnv(env []string, args ...string) Result {
	m.t.Helper()
	r := cli.Runner{Kokod: m, SocketPath: m.SocketPath()}
	if m.releases != nil {
		r.Releases = m.releases.hook()
	}
	if exe := m.exe; exe != "" {
		r.Executable = func() (string, error) { return exe, nil }
	}
	return Run(m.t, r, append([]string{"HOME=" + m.home, "TMPDIR=" + m.tmp}, env...), args...)
}

// Home is the HOME koko runs with on the machine, outside its root.
func (m *Machine) Home() string {
	return m.home
}

// KokoAt makes koko on the machine run from a file at the real path file,
// such as one under Home, which it writes, making its directory: a koko of
// the build of koko on the machine, for its platform, mode 0755. It returns
// the file's bytes.
func (m *Machine) KokoAt(file string) []byte {
	m.t.Helper()
	b := m.Self()
	b.Path, b.Platform = release.KokoPackage, m.OSArch
	content, err := os.ReadFile(m.NewKokod(b))
	if err != nil {
		m.t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		m.t.Fatal(err)
	}
	if err := os.WriteFile(file, content, 0o755); err != nil {
		m.t.Fatal(err)
	}
	m.exe = file
	return content
}

// TempDir is the TMPDIR koko runs with on the machine, outside its root.
func (m *Machine) TempDir() string {
	return m.tmp
}

// Kokod is the kokod file to give koko server install with --kokod, outside
// the machine's root: one of the build of koko on the machine.
func (m *Machine) Kokod() string {
	return m.kokod
}

// NewKokod makes a file outside the machine's root that the machine states
// is of the build b, and returns its path: a kokod of another build, or,
// with another b.Path, another program.
func (m *Machine) NewKokod(b build.Info) string {
	m.t.Helper()
	m.mu.Lock()
	n := len(m.builds) + 1
	m.mu.Unlock()
	return m.newFile(b, fmt.Sprintf("#!/bin/sh\n# %s %s of %s, file %d\n", b.Path, b.Version, m.t.Name(), n))
}

// newFile writes text to a new file outside the root, stated as the build
// b, and returns its path.
func (m *Machine) newFile(b build.Info, text string) string {
	path := filepath.Join(m.t.TempDir(), filepath.Base(b.Path))
	if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
		m.t.Fatal(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.builds[text] = b
	return path
}

// Path is the real path of path on the machine, under its root.
func (m *Machine) Path(path string) string {
	return filepath.Join(m.root, path)
}

// SocketPath is the koko server's socket on the machine.
func (m *Machine) SocketPath() string {
	return m.Path(wire.SocketPath)
}

// NewRelease makes the kokod file Kokod names from now on a new one, of
// koko's build too, as a newer release's kokod beside its koko is, and
// returns its bytes.
func (m *Machine) NewRelease() []byte {
	m.t.Helper()
	m.newReleaseFiles++
	text := fmt.Sprintf("#!/bin/sh\n# kokod of %s, release %d\n", m.t.Name(), m.newReleaseFiles+1)
	m.kokod = m.newFile(m.kokodBuild(), text)
	return []byte(text)
}

// Ran returns the kokod command lines run on the machine so far, in order.
func (m *Machine) Ran() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.ran)
}

// Self is the build of koko on the machine: the test binary's, unless
// Release stated another.
func (m *Machine) Self() build.Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.self
}

// Platform is the machine's OSArch.
func (m *Machine) Platform() string {
	return m.OSArch
}

// RunsAsRoot tells whether koko, and the kokod it runs, run as root on the
// machine: unless NotRoot.
func (m *Machine) RunsAsRoot() bool {
	return !m.NotRoot
}

// Build is the build the machine states for the file at the real path file,
// by its bytes; a file it did not make, such as one written by hand, is no
// Go binary.
func (m *Machine) Build(file string) (build.Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buildLocked(file)
}

// buildLocked is Build, with m.mu held.
func (m *Machine) buildLocked(file string) (build.Info, error) {
	text, err := os.ReadFile(file)
	if err != nil {
		return build.Info{}, err
	}
	b, ok := m.builds[string(text)]
	if !ok {
		return build.Info{}, errors.New("unrecognized file format")
	}
	return b, nil
}

// Run runs the kokod file at the real path file as kokod's command line in
// process, on this machine, as root: kokod install puts that file at
// /usr/local/bin/kokod. It runs only a kokod the machine made.
func (m *Machine) Run(ctx context.Context, file string, args []string, stdout, stderr io.Writer) (int, error) {
	if b, err := m.Build(file); err != nil || b.Path != install.KokodPackage {
		return 0, fmt.Errorf("the fake machine runs only the kokod files it made, not %s", file)
	}
	m.mu.Lock()
	m.ran = append(m.ran, strings.Join(args, " "))
	broken := m.NoSocket || m.behaviourOf(file).noSocket
	m.mu.Unlock()
	p := kokod.Program{Machine: m, Binary: func() (string, error) { return file, nil }}
	if broken {
		p.Timings = timing.Timings{InstallSocketWait: NoSocketWait}
	}
	if m.releases != nil {
		p.Releases = m.releases.source()
		p.Releases.Build = m.Build
	}
	return p.Run(ctx, kokod.Invocation{Args: args, Stdout: stdout, Stderr: stderr}), nil
}

// Fail makes the koko service, from now on, start and never open its socket
// when it runs the kokod file whose bytes are content, such as a release's
// kokod (Releases.Content), as one whose start-up refuses does.
func (m *Machine) Fail(content []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b := m.behaviours[string(content)]
	b.noSocket = true
	m.behaviours[string(content)] = b
}

// behaviourOf is how the koko service runs the kokod file at the real path
// file, with m.mu held: as a kokod runs, unless the machine made it to run
// otherwise.
func (m *Machine) behaviourOf(file string) behaviour {
	text, err := os.ReadFile(file)
	if err != nil {
		return behaviour{}
	}
	return m.behaviours[string(text)]
}

// Calls returns what install asked of the machine so far, in order.
func (m *Machine) Calls() []Call {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.calls)
}

// Owner returns the user the machine was last asked to make path's owner,
// or "" when it was never asked.
func (m *Machine) Owner(path string) string {
	info, err := os.Stat(m.Path(path))
	if err != nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, o := range m.owners {
		if os.SameFile(o.info, info) {
			return o.owner
		}
	}
	return ""
}

// Users returns the users install created.
func (m *Machine) Users() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.users)
}

// Enabled tells whether the koko service is enabled.
func (m *Machine) Enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled
}

// Running tells whether the koko service runs.
func (m *Machine) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.service == nil {
		return false
	}
	select {
	case <-m.service.done:
		return false
	default:
		return true
	}
}

// Log returns what every run of the koko service has logged.
func (m *Machine) Log() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var b strings.Builder
	for _, p := range m.runs {
		b.WriteString(p.stderr.String())
	}
	return b.String()
}

// WaitLog waits until what every run of the koko service has logged holds
// substr n times, and returns it: the koko server logs its start just after
// its socket accepts, which is all install waits for. It fails the test
// when 10 s pass first.
func (m *Machine) WaitLog(substr string, n int) string {
	m.t.Helper()
	deadline := time.Now().Add(waitFor)
	for {
		log := m.Log()
		if strings.Count(log, substr) >= n {
			return log
		}
		if time.Now().After(deadline) {
			m.t.Fatalf("the koko service did not log %q %d times within %v:\n%s", substr, n, waitFor, log)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Files lists every file and directory under the machine's root, as the
// machine names them, in lexical order.
func (m *Machine) Files() []string {
	m.t.Helper()
	var files []string
	err := filepath.WalkDir(m.root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != m.root {
			files = append(files, m.machinePath(path))
		}
		return nil
	})
	if err != nil {
		m.t.Errorf("list the files under the machine's root: %v", err)
	}
	return files
}

// record notes a call and the files under the root when it came.
func (m *Machine) record(what string) {
	files := m.Files()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, Call{What: what, Files: files})
}

// machinePath is the path on the machine of a real path under its root.
func (m *Machine) machinePath(path string) string {
	rel, err := filepath.Rel(m.root, path)
	if err != nil {
		return path
	}
	return "/" + filepath.ToSlash(rel)
}

// Root is the temporary directory that stands for the machine's root.
func (m *Machine) Root() string {
	return m.root
}

func (m *Machine) SystemdVersion(context.Context) (int, error) {
	m.record("systemd --version")
	if m.SystemdErr != nil {
		return 0, m.SystemdErr
	}
	return m.Systemd, nil
}

func (m *Machine) FilesystemType(path string) (string, error) {
	m.record("filesystem " + m.machinePath(path))
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return m.FSType, nil
}

func (m *Machine) PortOwner(_ context.Context, port int) (portowner.Process, error) {
	m.record(fmt.Sprintf("port %d", port))
	if p, ok := m.Listening[port]; ok {
		return p, nil
	}
	if m.serviceListens(port) {
		return portowner.Process{PID: servicePID, Name: "koko", User: installer.User}, nil
	}
	return portowner.Process{}, portowner.ErrNotFound
}

// serviceListens tells whether the koko service runs with its unit's
// listen address, or its web listener's, on port, as an initialised koko
// server listens. One not initialised yet listens on no TCP port for
// devices, which install takes the same way: the restart leaves the port
// free.
func (m *Machine) serviceListens(port int) bool {
	if !m.Running() {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	listen := map[string]string{}
	for _, kv := range m.running.env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			listen[k] = v
		}
	}
	for _, k := range []string{"KOKO_LISTEN", "KOKO_WEB_LISTEN"} {
		if _, p, ok := strings.CutLast(listen[k], ":"); ok && p == strconv.Itoa(port) {
			return true
		}
	}
	return false
}

// ServiceShow shows, as systemctl show does, the koko service's main PID,
// servicePID while it runs and 0 otherwise, and the Environment=
// assignments of its loaded unit, drop-ins included, separated by spaces,
// each in double quotes when it holds a character a shell would take, as
// systemd's shell_maybe_quote does; none when systemd has no koko unit.
func (m *Machine) ServiceShow(_ context.Context, property string) (string, error) {
	m.record("show " + installer.UnitName + " " + property)
	switch property {
	case "MainPID":
		if m.Running() {
			return strconv.Itoa(servicePID), nil
		}
		return "0", nil
	case "Environment":
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.loaded == nil {
			return "", nil
		}
		var shown []string
		for _, kv := range m.loaded.env {
			if strings.ContainsAny(kv, " \t\"'\\$`()<>|&;!*?[") {
				kv = `"` + kv + `"`
			}
			shown = append(shown, kv)
		}
		return strings.Join(shown, " "), nil
	}
	return "", fmt.Errorf("the fake systemctl show does not know %s", property)
}

func (m *Machine) SourceIP(context.Context) (netip.Addr, error) {
	m.record("source ip")
	if !m.Source.IsValid() {
		return netip.Addr{}, errors.New("connect: network is unreachable")
	}
	return m.Source, nil
}

func (m *Machine) AddUser(_ context.Context, name string) error {
	m.record("useradd " + name)
	m.mu.Lock()
	defer m.mu.Unlock()
	if !slices.Contains(m.users, name) {
		m.users = append(m.users, name)
	}
	return nil
}

func (m *Machine) Chown(path, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.chown(path, name)
}

// chown records name as the owner of the file at the real path, with m.mu
// held.
func (m *Machine) chown(path, name string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	m.owners = slices.DeleteFunc(m.owners, func(o owned) bool { return os.SameFile(o.info, info) })
	m.owners = append(m.owners, owned{info: info, owner: name})
	return nil
}

// FileOwner names the owner the machine was last asked to give the file
// info describes, at the real path path. A file it was never asked about is
// the koko service's user's in the koko service's runtime directory, where
// only the koko service writes, and root's anywhere else, as koko and kokod
// run as root on the machine.
func (m *Machine) FileOwner(path string, info fs.FileInfo) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, o := range m.owners {
		if os.SameFile(o.info, info) {
			return o.owner, nil
		}
	}
	if m.loaded != nil {
		for _, d := range m.loaded.runtimeDirs {
			if filepath.Dir(path) == m.Path("/run/"+d) {
				return m.loaded.user, nil
			}
		}
	}
	return "root", nil
}

// ownerOf returns the owner recorded for the file at the real path, or ""
// when there is none, with m.mu held.
func (m *Machine) ownerOf(path string) string {
	info, err := os.Lstat(path)
	if err != nil {
		return ""
	}
	for _, o := range m.owners {
		if os.SameFile(o.info, info) {
			return o.owner
		}
	}
	return ""
}

// Systemctl does what systemd would with the koko unit: daemon-reload loads
// the unit file and its drop-ins as they are then, enable and disable
// enable it and undo that, and restart, start and stop run the loaded unit.
// Like systemd with Type=exec, starting returns once the binary runs, before
// the koko server opens its socket.
func (m *Machine) Systemctl(_ context.Context, args ...string) error {
	m.record("systemctl " + strings.Join(args, " "))
	if len(args) == 3 && args[1] == "--now" && args[2] == installer.UpgradePathName {
		return m.helperPath(args[0])
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(args) == 1 && args[0] == "daemon-reload" {
		return m.reload()
	}
	if len(args) != 2 || args[1] != installer.UnitName {
		return fmt.Errorf("the fake systemctl does not know %q", args)
	}
	if m.loaded == nil {
		return fmt.Errorf("unit %s not found", installer.UnitName)
	}
	switch args[0] {
	case "enable":
		m.enabled = true
		return nil
	case "disable":
		m.enabled = false
		return nil
	case "start":
		if m.service != nil {
			return nil
		}
		return m.startService()
	case "restart":
		m.stopService()
		return m.startService()
	case "stop":
		m.stopService()
		return nil
	}
	return fmt.Errorf("the fake systemctl does not know %q", args)
}

// helperPath does what systemctl enable --now and disable --now do with the
// upgrade helper's path unit: enable and start it, or stop and disable it.
// While it is started, its file being there runs the helper's service.
func (m *Machine) helperPath(verb string) error {
	m.mu.Lock()
	switch {
	case m.helper == nil:
		m.mu.Unlock()
		return fmt.Errorf("unit %s not found", installer.UpgradePathName)
	case verb == "enable":
		m.helperEnabled = true
		if m.watch == nil {
			m.watch = m.startWatch()
		}
		m.mu.Unlock()
		return nil
	case verb == "disable":
		w := m.watch
		m.helperEnabled, m.watch = false, nil
		m.mu.Unlock()
		w.stop()
		return nil
	}
	m.mu.Unlock()
	return fmt.Errorf("the fake systemctl does not know %s --now %s", verb, installer.UpgradePathName)
}

// HelperEnabled tells whether the upgrade helper's path unit is enabled.
func (m *Machine) HelperEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.helperEnabled
}

// pathPoll is how often the fake systemd looks for the path unit's file,
// which systemd watches with inotify.
const pathPoll = 10 * time.Millisecond

// startWatch starts the helper's path unit, with m.mu held: each time the
// file its loaded unit names is there, it runs the helper's service, as
// PathExists= does, one run at a time.
func (m *Machine) startWatch() *pathWatch {
	ctx, cancel := context.WithCancel(context.Background())
	w := &pathWatch{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(w.done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(pathPoll):
			}
			m.mu.Lock()
			h := m.helper
			m.mu.Unlock()
			if h == nil {
				continue
			}
			if _, err := os.Lstat(m.Path(h.pathExists)); err != nil {
				continue
			}
			m.runHelper(ctx, h)
		}
	}()
	return w
}

// stop stops the path unit, and waits for a run of the helper's service it
// started to end, which ctx ending cuts short.
func (w *pathWatch) stop() {
	if w == nil {
		return
	}
	w.cancel()
	<-w.done
}

// runHelper runs the helper's service once, as root, and notes what it
// printed and that it ran.
func (m *Machine) runHelper(ctx context.Context, h *helperUnits) {
	var out bytes.Buffer
	if len(h.execStart) == 0 {
		out.WriteString("the unit has no ExecStart\n")
	} else if status, err := m.Run(ctx, m.Path(h.execStart[0]), h.execStart[1:], &out, &out); err != nil {
		fmt.Fprintf(&out, "%s did not run: %v\n", h.execStart[0], err)
	} else {
		fmt.Fprintf(&out, "%s exited with status %d\n", installer.UpgradeServiceName, status)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.helperLog.Write(out.Bytes())
	m.helperRuns++
}

// HelperRuns returns how many runs of the upgrade helper have ended.
func (m *Machine) HelperRuns() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.helperRuns
}

// helperWait bounds WaitHelper: a run installs one or two kokod files,
// each waiting for the koko service's socket.
const helperWait = time.Minute

// WaitHelper waits until n runs of the upgrade helper, which its path unit
// starts, have ended, and returns what every run printed. It fails the
// test when helperWait passes first.
func (m *Machine) WaitHelper(n int) string {
	m.t.Helper()
	deadline := time.Now().Add(helperWait)
	for {
		m.mu.Lock()
		runs, log := m.helperRuns, m.helperLog.String()
		m.mu.Unlock()
		if runs >= n {
			return log
		}
		if time.Now().After(deadline) {
			m.t.Fatalf("the upgrade helper did not end %d runs within %v:\n%s", n, helperWait, log)
		}
		time.Sleep(pathPoll)
	}
}

// reload loads the koko unit file as it is now, then its drop-ins in the
// order of their names, whose Environment= lines come after the unit's and
// so override them, or forgets the unit when it is gone; and the upgrade
// helper's units, or forgets them.
func (m *Machine) reload() error {
	if err := m.reloadHelper(); err != nil {
		return err
	}
	text, err := os.ReadFile(m.Path(installer.UnitPath))
	if errors.Is(err, fs.ErrNotExist) {
		m.loaded = nil
		return nil
	}
	if err != nil {
		return err
	}
	dropIns, err := filepath.Glob(m.Path(dropInDir + "/*.conf"))
	if err != nil {
		return err
	}
	slices.Sort(dropIns)
	u := &unitFile{}
	for _, file := range dropIns {
		more, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		text = append(append(text, '\n'), more...)
	}
	for line := range strings.Lines(string(text)) {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.HasPrefix(key, "#") {
			continue
		}
		switch key {
		case "ExecStart":
			u.execStart = strings.Fields(value)
		case "Environment":
			u.env = append(u.env, value)
		case "LoadCredential":
			name, path, _ := strings.Cut(value, ":")
			u.credentials = append(u.credentials, [2]string{name, path})
		case "User":
			u.user = value
		case "RuntimeDirectory":
			u.runtimeDirs = append(u.runtimeDirs, value)
		case "StateDirectory":
			u.stateDirs = append(u.stateDirs, value)
		}
	}
	m.loaded = u
	return nil
}

// reloadHelper loads the upgrade helper's path unit and service as they are
// now, or forgets them when either is gone.
func (m *Machine) reloadHelper() error {
	path, err := os.ReadFile(m.Path(installer.UpgradePathUnit))
	if errors.Is(err, fs.ErrNotExist) {
		m.helper = nil
		return nil
	}
	if err != nil {
		return err
	}
	service, err := os.ReadFile(m.Path(installer.UpgradeServiceUnit))
	if errors.Is(err, fs.ErrNotExist) {
		m.helper = nil
		return nil
	}
	if err != nil {
		return err
	}
	h := &helperUnits{}
	for line := range strings.Lines(string(path) + "\n" + string(service)) {
		key, value, _ := strings.Cut(strings.TrimSpace(line), "=")
		switch key {
		case "PathExists":
			h.pathExists = value
		case "Unit":
			if value != installer.UpgradeServiceName {
				return fmt.Errorf("the fake systemd starts only %s from %s, not %s", installer.UpgradeServiceName, installer.UpgradePathName, value)
			}
		case "ExecStart":
			h.execStart = strings.Fields(value)
		}
	}
	m.helper = h
	return nil
}

// stateDirectory makes the unit's state directory dir as systemd's
// StateDirectory= does at each start: it gives it, and everything in it,
// to the unit's user, but only when the directory itself is not that
// user's already, in which case systemd leaves what is in it as it is.
func (m *Machine) stateDirectory(dir, user string) error {
	path := m.Path("/var/lib/" + dir)
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	if m.ownerOf(path) == user {
		return nil
	}
	return filepath.WalkDir(path, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return m.chown(p, user)
	})
}

// startService runs the loaded unit, failing as systemd fails a start when
// its binary does not run or a credential's file is missing.
func (m *Machine) startService() error {
	u := m.loaded
	if len(u.execStart) == 0 {
		return errors.New("the unit has no ExecStart")
	}
	if info, err := os.Stat(m.Path(u.execStart[0])); err != nil || info.Mode()&0o111 == 0 {
		return fmt.Errorf("job for %s failed: %s does not run (status=203/EXEC)", installer.UnitName, u.execStart[0])
	}
	creds := m.t.TempDir()
	for _, c := range u.credentials {
		b, err := os.ReadFile(m.Path(c[1]))
		if err != nil {
			return fmt.Errorf("job for %s failed: load the credential %s (status=243/CREDENTIALS): %w", installer.UnitName, c[0], err)
		}
		if err := os.WriteFile(filepath.Join(creds, c[0]), b, 0o400); err != nil {
			return err
		}
	}
	for _, d := range u.runtimeDirs {
		if err := os.MkdirAll(m.Path("/run/"+d), 0o755); err != nil {
			return err
		}
	}
	for _, d := range u.stateDirs {
		if err := m.stateDirectory(d, u.user); err != nil {
			return err
		}
	}
	// kokod run, or a koko from before kokod (v0.1.0, v0.2.0), whose
	// koko server run took the same flags.
	if args := u.execStart[1:]; !slices.Equal(args, []string{"run"}) && !slices.Equal(args, []string{"server", "run"}) {
		return fmt.Errorf("the fake systemd runs kokod run or koko server run, not %q", u.execStart)
	}
	// The unit's own settings, then what systemd adds. The koko server
	// listens on a loopback port of the test's choosing, never the unit's,
	// and so does its web listener when the unit turns it on.
	env := append(slices.Clone(u.env), "CREDENTIALS_DIRECTORY="+creds, "KOKO_LISTEN=127.0.0.1:0")
	if slices.ContainsFunc(u.env, func(kv string) bool { return strings.HasPrefix(kv, "KOKO_WEB_LISTEN=") }) {
		env = append(env, "KOKO_WEB_LISTEN=127.0.0.1:0")
	}
	b := m.behaviourOf(m.Path(u.execStart[0]))
	version := ""
	if bi, err := m.buildLocked(m.Path(u.execStart[0])); err == nil {
		version = bi.Version
	}
	noSocket, delay, listenDelay := m.NoSocket || b.noSocket, m.StartDelay, m.ListenDelay
	releases := noReleases
	if m.releases != nil {
		releases = m.releases.source()
	}
	serve := func(ctx context.Context, c server.Config) error {
		// A kokod migrates at its start, before its socket opens, so a stop
		// waits for the migration: the process does not end within it.
		if b.migrates {
			if err := migrateBeyond(context.WithoutCancel(ctx), m.Path(c.DataDir)); err != nil {
				return err
			}
		}
		if noSocket {
			<-ctx.Done()
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
		if listenDelay > 0 {
			if err := holdUnlistenedSocket(ctx, m.SocketPath(), listenDelay); err != nil {
				return err
			}
			if ctx.Err() != nil {
				return nil
			}
		}
		c.DataDir = m.Path(c.DataDir)
		c.Filesystems, c.ProcessUIDs, c.SocketPath = LocalDisk{}, NotRoot, m.SocketPath()
		// Its check for a new release waits on a clock that never moves,
		// so the requests a test counts on the fake GitHub are koko's own.
		c.UpdateClock, c.Releases = StillClock{}, releases
		c.Upgrade.HelperUnit, c.Upgrade.OutcomeFile = m.Path(c.Upgrade.HelperUnit), m.Path(c.Upgrade.OutcomeFile)
		c.Upgrade.RequestFile = m.Path(c.Upgrade.RequestFile)
		// It reports the version of the kokod file it runs, as a real one
		// reports its own build's.
		c.Version = version
		return server.Run(ctx, c)
	}
	p := kokod.Program{Serve: serve}
	m.service = RunKokodBackground(m.t, p, env, "run")
	m.running = u
	m.runs = append(m.runs, m.service)
	return nil
}

// migrateBeyond migrates koko.db in dataDir one schema version beyond this
// koko's, as a newer kokod does at its start: it snapshots it first, as
// koko.db.before-<n>, mode 0600, keeping no other snapshot, as the store
// does, then records the new version.
func migrateBeyond(ctx context.Context, dataDir string) error {
	next := store.SchemaVersion() + 1
	older, err := filepath.Glob(filepath.Join(dataDir, store.FileName+".before-*"))
	if err != nil {
		return err
	}
	for _, file := range older {
		if err := os.Remove(file); err != nil {
			return err
		}
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, store.FileName))
	if err != nil {
		return err
	}
	snapshot := filepath.Join(dataDir, fmt.Sprintf("%s.before-%d", store.FileName, next))
	_, err = db.ExecContext(ctx, "VACUUM INTO ?", snapshot)
	if err == nil {
		_, err = db.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", next))
	}
	if err := errors.Join(err, db.Close()); err != nil {
		return fmt.Errorf("migrate %s beyond this koko's schema: %w", store.FileName, err)
	}
	return os.Chmod(snapshot, 0o600)
}

// holdUnlistenedSocket keeps a socket file at path that accepts no
// connection, as the koko server's is between its bind and its listen,
// until wait has passed or ctx ends, then removes it for the koko server to
// listen there. The file is made beside path and renamed into place once
// its listener is closed, so path never accepts one.
func holdUnlistenedSocket(ctx context.Context, path string, wait time.Duration) error {
	made := filepath.Join(filepath.Dir(path), "unlistened")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: made, Net: "unix"})
	if err != nil {
		return err
	}
	ln.SetUnlinkOnClose(false)
	if err := ln.Close(); err != nil {
		return err
	}
	if err := os.Rename(made, path); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
	case <-time.After(wait):
	}
	return os.Remove(path)
}

// stopService stops the koko service, if it runs, and removes its runtime
// directories, as systemd does.
func (m *Machine) stopService() {
	if m.service == nil {
		return
	}
	m.service.Stop()
	for _, d := range m.running.runtimeDirs {
		_ = os.RemoveAll(m.Path("/run/" + d))
	}
	m.service, m.running = nil, nil
}

// stop stops the upgrade helper's path unit, and a run of the helper it
// started, then the koko service, when the test ends.
func (m *Machine) stop() {
	m.mu.Lock()
	w := m.watch
	m.watch = nil
	m.mu.Unlock()
	w.stop()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopService()
}
