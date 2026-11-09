// Package installer is `kokod install` and `kokod uninstall`, the machine's
// part of `koko server install` and `koko server uninstall` (ADR 0041): on a
// Linux machine with systemd, as root, install creates the koko user and the
// directories, writes the Root Key, kokod and the systemd unit, starts the
// service and waits for its Unix socket, and reports what initialising it
// needs, which koko server install then does over the socket; on a koko
// server initialised already, it is the upgrade. Uninstall stops the service
// and removes the unit and kokod, and keeps the data (P0 spec: install and
// run, ADR 0020).
//
// It reaches the machine only through a Machine: a root directory prefixed to
// every path it reads or writes, and the few things only the machine can
// answer or do, such as running systemctl. Tests give it a temporary root and
// a fake.
package installer

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/filesystem"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/install"
	"github.com/cieons/koko/internal/shared/portowner"
	"github.com/cieons/koko/internal/shared/serveraddr"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// The koko server's fixed paths on its machine (P0 spec: install and run),
// as the machine names them, before the Machine's root is prefixed.
const (
	// BinaryPath is where kokod runs from.
	BinaryPath = install.KokodPath
	// UnitPath is the koko service's systemd unit.
	UnitPath = "/etc/systemd/system/" + UnitName
	// dropInDir holds the unit's drop-ins, where systemctl edit koko writes
	// them; they are the Owner's, and install reads them only.
	dropInDir = UnitPath + ".d"
	// DataDir is the data directory; the unit's StateDirectory= names it.
	DataDir = "/var/lib/koko"
	// KeyDir holds the Root Key's file, readable by root only.
	KeyDir = "/etc/koko"
	// RootKeyFile is the Root Key's file, which the unit's LoadCredential=
	// hands the service.
	RootKeyFile = KeyDir + "/root.key"
)

const (
	// UnitName is the koko service's systemd unit.
	UnitName = "koko.service"
	// User is the static system user the koko service runs as.
	User = "koko"
	// defaultPort is the TCP port a fresh koko server listens on unless
	// --port names another (ADR 0036).
	defaultPort = 7822
	// minSystemd is the oldest systemd whose LoadCredential= the unit can
	// use.
	minSystemd = 247
)

// Machine is everything install reaches on the machine beyond its files.
type Machine interface {
	// Root is prefixed to every path install reads or writes: "/" on a
	// real machine.
	Root() string
	// RunsAsRoot tells whether this process runs as root, which install
	// and uninstall need.
	RunsAsRoot() bool
	// SystemdVersion returns systemd's version, such as 252.
	SystemdVersion(ctx context.Context) (int, error)
	// FilesystemType returns the type of the filesystem holding path, which
	// exists, as the kernel names it: "ext4", "nfs4".
	FilesystemType(path string) (string, error)
	// PortOwner names the process listening on TCP port, or returns
	// portowner.ErrNotFound when none does.
	PortOwner(ctx context.Context, port int) (portowner.Process, error)
	// SourceIP returns the source IP of the machine's default route, which
	// it finds without asking anyone else.
	SourceIP(ctx context.Context) (netip.Addr, error)
	// AddUser creates the static system user name with a group of its own,
	// and does nothing when it exists.
	AddUser(ctx context.Context, name string) error
	// Chown makes path owned by the user name and that user's group.
	Chown(path, name string) error
	// Systemctl runs systemctl with args.
	Systemctl(ctx context.Context, args ...string) error
	// ServiceShow returns the koko service's property as systemd has its
	// unit loaded, drop-ins included, as systemctl show --value prints it:
	// "MainPID" is its main process's ID, 0 when it does not run, and
	// "Environment" its Environment= assignments, separated by spaces and
	// quoted as systemd quotes them, none when systemd has no koko unit.
	ServiceShow(ctx context.Context, property string) (string, error)
}

// Installer installs the koko server on a machine.
type Installer struct {
	Machine Machine
	// Binary is the kokod file to install: the one running.
	Binary string
	// Timings is the timing-constants hook, for tests only: its zero value
	// is the product's timings.
	Timings timing.Timings
}

// Install installs and starts the koko server, waits for its Unix socket,
// and reports what the Owner needs next: on a fresh koko server, what
// initialising it over the socket needs, which koko server install does. On
// a koko server initialised already it is the upgrade instead: it writes
// kokod and the unit again and restarts the service, which keeps its
// record. Every check comes before the first write, so a refusal leaves the
// machine as it was. Until initialisation has succeeded, running it again is
// safe and finishes the job.
func (in *Installer) Install(ctx context.Context, req install.Request) (install.Report, error) {
	p, err := in.check(ctx, req)
	if err != nil {
		return install.Report{}, err
	}
	key, err := in.write(ctx, p)
	if err != nil {
		return install.Report{}, err
	}
	if err := in.start(ctx); err != nil {
		return install.Report{}, err
	}
	report := install.Report{RootKeyFile: RootKeyFile, RootKeyID: key.ID(), Port: p.port}
	d := p.deployed
	if d == nil {
		report.Initialise = install.NewInitialise(req, p.address)
		return report, nil
	}
	report.Address, report.HostKeyFingerprint = d.address, d.fingerprint
	// A Server Address without a port of its own follows the listen port,
	// which devices that joined before cannot know of.
	if moved := serveraddr.WithPort(d.address, p.port); moved != d.address && p.port != d.listenedOn {
		report.DevicesMustConnect = moved
	}
	return report, nil
}

// plan is what the checks found install is to do.
type plan struct {
	// port is the TCP port the koko server is to listen on.
	port int
	// address is the Server Address a fresh koko server is to be
	// initialised with.
	address string
	// deployed is the koko server initialised already, which install
	// upgrades; nil for a fresh one.
	deployed *deployed
}

// deployed is what install reads of a koko server initialised already.
type deployed struct {
	// address is its Server Address, as stored.
	address string
	// sealedBy is the id of the Root Key its sealed keys need.
	sealedBy string
	// fingerprint is its host key's SHA-256 fingerprint.
	fingerprint string
	// listenedOn is the port the koko service listened on before this run,
	// or 0 when that is unknown: systemd had no koko unit.
	listenedOn int
}

// path is the real path of a path on the machine.
func (in *Installer) path(p string) string {
	return filepath.Join(in.Machine.Root(), p)
}

// check refuses, before anything is written, a machine that cannot run the
// koko server, a port another process holds, and on a fresh koko server a
// Server Address devices could not reach. It returns what install is to do.
func (in *Installer) check(ctx context.Context, req install.Request) (plan, error) {
	if err := in.checkRoot("install"); err != nil {
		return plan{}, err
	}
	// systemd runs the machine when it has made this directory, as
	// sd_booted(3) tells; in a container whose init is something else,
	// systemctl may still be there, and would fail only at daemon-reload,
	// after the writes. The image runs the koko server there instead.
	if info, err := os.Stat(in.path(install.SystemdRunDir)); err != nil || !info.IsDir() {
		return plan{}, install.NoSystemd()
	}
	version, err := in.Machine.SystemdVersion(ctx)
	if err != nil {
		return plan{}, fmt.Errorf("find systemd's version, which must be %d or later: %w", minSystemd, err)
	}
	if version < minSystemd {
		return plan{}, install.Refuse(install.Unsupported, "systemd %d is too old: the koko server needs systemd %d or later, for LoadCredential= (Debian 11, Ubuntu 22.04, RHEL 9 or later)",
			version, minSystemd)
	}
	if err := in.checkFilesystem(); err != nil {
		return plan{}, err
	}
	d, err := in.findDeployed(ctx)
	if err != nil {
		return plan{}, err
	}
	if d != nil {
		return in.checkUpgrade(ctx, req, d)
	}
	if req.Owner == "" {
		return plan{}, install.Refuse(install.Usage, "the koko server here is not initialised yet: name its Owner with --owner <user>")
	}
	port := cmp.Or(req.Port, defaultPort)
	if err := in.checkPort(ctx, port); err != nil {
		return plan{}, err
	}
	address, err := in.address(ctx, req.Address)
	if err != nil {
		return plan{}, err
	}
	// A Root Key file that is there is reused, so it must load: a damaged
	// one is left as it is, never replaced.
	if _, err := seal.Load(in.path(RootKeyFile)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return plan{}, err
	}
	return plan{port: port, address: address}, nil
}

// checkRoot refuses a process that is not root, which could make neither
// the koko user nor the koko server's files; verb is install's or
// uninstall's.
func (in *Installer) checkRoot(verb string) error {
	if in.Machine.RunsAsRoot() {
		return nil
	}
	return install.Refuse(install.Unsupported, "kokod %s changes the machine's services, files and users, so it runs as root: run sudo koko server %s", verb, verb)
}

// checkUpgrade checks the upgrade of d, a koko server initialised already:
// it takes neither --owner nor --address, keeps the port the koko service
// listens on unless --port names another, and needs the Root Key d's keys
// are sealed under.
func (in *Installer) checkUpgrade(ctx context.Context, req install.Request, d *deployed) (plan, error) {
	if err := refuseInitFlags(req); err != nil {
		return plan{}, err
	}
	var err error
	if d.fingerprint, err = in.openKeys(ctx, d.sealedBy); err != nil {
		return plan{}, err
	}
	// The port the koko service listens on now, as systemd has its unit
	// loaded, drop-ins included, or 0 when systemd has no koko unit, as
	// after koko server uninstall.
	if d.listenedOn, err = in.servicePort(ctx); err != nil {
		return plan{}, err
	}
	// A drop-in that sets the port overrides the unit at the restart, so
	// the koko server listens there whatever --port says. Uninstall keeps
	// drop-ins, and the port one set then was the one listened on.
	set, dropIn, err := in.dropInListen()
	if err != nil {
		return plan{}, err
	}
	if set != 0 {
		if req.Port != 0 && req.Port != set {
			return plan{}, install.Refuse(install.Usage, "the drop-in %s sets KOKO_LISTEN for the koko service, so it would listen on port %d whatever --port says: change the port there (systemctl edit koko), or take KOKO_LISTEN out of it and run koko server install again with --port %d",
				dropIn, set, req.Port)
		}
		d.listenedOn = cmp.Or(d.listenedOn, set)
	}
	port := cmp.Or(set, req.Port, d.listenedOn, defaultPort)
	if err := in.checkPort(ctx, port); err != nil {
		return plan{}, err
	}
	return plan{port: port, deployed: d}, nil
}

// dropInListen returns the port the koko unit's drop-ins in dropInDir set
// with KOKO_LISTEN, the last in the order systemd reads them winning, and
// the drop-in that sets it; 0 and "" when none does. It reads them itself,
// so that it finds them when systemd has no koko unit loaded.
func (in *Installer) dropInListen() (port int, dropIn string, err error) {
	files, err := filepath.Glob(in.path(dropInDir + "/*.conf"))
	if err != nil {
		return 0, "", fmt.Errorf("find the koko unit's drop-ins in %s: %w", dropInDir, err)
	}
	for _, file := range files { // in lexical order, as systemd reads them
		name := dropInDir + "/" + filepath.Base(file)
		text, err := os.ReadFile(file)
		if err != nil {
			return 0, "", fmt.Errorf("read the koko unit's drop-in %s: %w", name, err)
		}
		set, err := dropInPort(string(text))
		if err != nil {
			return 0, "", fmt.Errorf("the drop-in %s sets the koko service to %w; nothing was written", name, err)
		}
		if set != 0 {
			port, dropIn = set, name
		}
	}
	return port, dropIn, nil
}

// servicePort returns the port the koko service listens on, from the last
// KOKO_LISTEN in its environment as systemd has its unit loaded, drop-ins
// included, or 0 when there is none, as when systemd has no koko unit.
func (in *Installer) servicePort(ctx context.Context) (int, error) {
	env, err := in.Machine.ServiceShow(ctx, "Environment")
	if err != nil {
		return 0, fmt.Errorf("read the koko service's environment from systemd: %w", err)
	}
	port, err := envPort(env)
	if err != nil {
		return 0, fmt.Errorf("systemd has the koko service %w; nothing was written", err)
	}
	return port, nil
}

// servicePID returns the koko service's main process ID, or 0 when it does
// not run.
func (in *Installer) servicePID(ctx context.Context) (int, error) {
	v, err := in.Machine.ServiceShow(ctx, "MainPID")
	if err != nil {
		return 0, fmt.Errorf("read the koko service's main PID from systemd: %w", err)
	}
	pid, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("systemd shows the koko service's main PID as %q", v)
	}
	return pid, nil
}

// refuseInitFlags refuses --owner and --address on a koko server initialised
// already: only initialisation takes them, so an upgrade never changes the
// Owner or the Server Address.
func refuseInitFlags(req install.Request) error {
	var flags, next []string
	if req.Owner != "" {
		flags = append(flags, "--owner")
		next = append(next, "issue a Join Code with koko device add")
	}
	if req.Address != "" {
		flags = append(flags, "--address")
		next = append(next, "change the Server Address with koko server set address=<host[:port]>")
	}
	if len(flags) == 0 {
		return nil
	}
	return install.Refuse(install.Initialised, "the koko server here is initialised already, so install upgrades it, and an upgrade takes neither --owner nor --address: run koko server install again without %s; %s",
		strings.Join(flags, " and "), strings.Join(next, ", and "))
}

// checkPort refuses a port another process listens on. The koko service's
// own process may hold it, as an initialised koko server does: the restart
// frees it.
func (in *Installer) checkPort(ctx context.Context, port int) error {
	p, err := in.Machine.PortOwner(ctx, port)
	switch {
	case errors.Is(err, portowner.ErrNotFound):
		return nil
	case err != nil:
		return fmt.Errorf("check that port %d is free: %w", port, err)
	}
	pid, err := in.servicePID(ctx)
	if err != nil {
		return fmt.Errorf("find whether the koko service holds port %d: %w", port, err)
	}
	if p.PID != 0 && p.PID == pid {
		return nil
	}
	return install.Refuse(install.PortTaken, "port %d is taken by %s: stop it, or run koko server install again with --port <port>", port, p)
}

// checkFilesystem refuses a data directory on a network filesystem, where
// SQLite's WAL does not work. Before the data directory exists, the
// filesystem it will be made on is checked.
func (in *Installer) checkFilesystem() error {
	dir, err := filesystem.Nearest(in.path(DataDir))
	if err != nil {
		return fmt.Errorf("find the filesystem of %s: %w", DataDir, err)
	}
	fsType, err := in.Machine.FilesystemType(dir)
	if err != nil {
		return fmt.Errorf("find the filesystem of %s: %w", DataDir, err)
	}
	if filesystem.Network(fsType) {
		return install.Refuse(install.Unsupported, "the data directory %s would be on %s, a network filesystem, where SQLite's WAL does not work: put %s on a local filesystem",
			DataDir, fsType, DataDir)
	}
	return nil
}

// findDeployed reads koko.db as it is, before the service runs or beside
// one that runs, and returns the koko server initialised there, or nil when
// it is not initialised. It refuses a koko.db at a schema this koko
// predates, which the koko server it installs would refuse to start on, as
// when an older koko is installed to go back before the snapshot is put
// back.
func (in *Installer) findDeployed(ctx context.Context) (*deployed, error) {
	var d *deployed
	var tooNew error
	err := store.Peek(ctx, in.path(DataDir), func(tx store.Tx) error {
		var version int
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
			return err
		}
		if tooNew = store.CheckVersion(in.path(DataDir), version); tooNew != nil {
			return nil
		}
		ok, err := deployment.Initialised(ctx, tx)
		if err != nil || !ok {
			return err
		}
		d = &deployed{}
		if d.address, err = deployment.Address(ctx, tx); err != nil {
			return err
		}
		d.sealedBy, err = deployment.SealedBy(ctx, tx)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("find whether the koko server in %s is initialised: %w", DataDir, err)
	}
	if tooNew != nil {
		return nil, fmt.Errorf("%s/%s: %w; nothing was written", DataDir, store.FileName, tooNew)
	}
	return d, nil
}

// openKeys loads the Root Key and opens the koko server's sealed keys with
// it, as the restarted koko server will, and returns the host key's
// fingerprint. It refuses a Root Key file that is missing, does not load or
// is not the one sealedBy names, and leaves it as it is: no Root Key is ever
// written over sealed data.
func (in *Installer) openKeys(ctx context.Context, sealedBy string) (string, error) {
	key, err := seal.Load(in.path(RootKeyFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("the Root Key file %s is missing, and the sealed data in %s needs %s: put back the copy of the Root Key file that names %s, then run koko server install again; nothing was written",
			RootKeyFile, DataDir, sealedBy, sealedBy)
	case err != nil:
		return "", fmt.Errorf("%w; the sealed data in %s needs %s; nothing was written", err, DataDir, sealedBy)
	case key.ID() != sealedBy:
		return "", fmt.Errorf("the Root Key file %s names %s, but the sealed data in %s needs %s: put back the copy of the Root Key file that names %s, then run koko server install again; nothing was written",
			RootKeyFile, key.ID(), DataDir, sealedBy, sealedBy)
	}
	var keys *deployment.Keys
	err = store.Peek(ctx, in.path(DataDir), func(tx store.Tx) (err error) {
		keys, err = deployment.Open(ctx, tx, key)
		return err
	})
	if err == nil && keys == nil {
		err = errors.New("it is no longer initialised")
	}
	if err != nil {
		return "", fmt.Errorf("open the koko server's keys in %s with the Root Key file %s: %w; nothing was written", DataDir, RootKeyFile, err)
	}
	return keys.HostKeyFingerprint(), nil
}

// address returns the Server Address: the one given, or else the default
// route's source IP when it is public.
func (in *Installer) address(ctx context.Context, given string) (string, error) {
	if given != "" {
		return given, nil
	}
	ip, err := in.Machine.SourceIP(ctx)
	ip = ip.Unmap()
	if err != nil {
		return "", install.Refuse(install.Usage, "cannot find this machine's IP on its default route (%v): give the Server Address with --address <host[:port]>", err)
	}
	if !isPublic(ip) {
		return "", install.Refuse(install.Usage, "this machine's IP on its default route, %s, is not public, so devices elsewhere cannot reach it: give the Server Address with --address <host[:port]>, such as a DNS name for this machine",
			ip)
	}
	if ip.Is6() {
		return "[" + ip.String() + "]", nil
	}
	return ip.String(), nil
}

// write creates the koko user and the directories, then writes the Root Key
// if it is missing on a fresh koko server, kokod and the unit, each
// with its owner and mode, and returns the Root Key.
func (in *Installer) write(ctx context.Context, p plan) (*seal.RootKey, error) {
	if err := in.Machine.AddUser(ctx, User); err != nil {
		return nil, fmt.Errorf("create the %s user: %w", User, err)
	}
	if err := in.dir(KeyDir, 0o700, "root"); err != nil {
		return nil, err
	}
	if err := in.dataDir(); err != nil {
		return nil, err
	}
	key, err := in.rootKey(p.deployed != nil)
	if err != nil {
		return nil, err
	}
	// The unit runs kokod at its fixed path, so it is in place before the
	// service starts.
	if err := in.binary(); err != nil {
		return nil, err
	}
	unit := Unit(UnitSettings{Port: p.port})
	if err := in.writeFile(UnitPath, 0o644, "root", strings.NewReader(unit)); err != nil {
		return nil, err
	}
	return key, nil
}

// dataDir makes the data directory, 0700 and the koko user's, when it is
// missing. One there already, such as a copy put back, gets its mode, and
// its owner from the unit's StateDirectory= at the start: systemd gives the
// directory and everything in it to the koko user when the directory is not
// that user's, so a chown of the directory alone would leave what is in it
// as the copy left it.
func (in *Installer) dataDir() error {
	info, err := os.Lstat(in.path(DataDir))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return in.dir(DataDir, 0o700, User)
	case err != nil:
		return fmt.Errorf("read %s: %w", DataDir, err)
	case !info.IsDir():
		return fmt.Errorf("create %s: it is there, but not a directory", DataDir)
	}
	if err := os.Chmod(in.path(DataDir), 0o700); err != nil {
		return fmt.Errorf("set the mode of %s: %w", DataDir, err)
	}
	return nil
}

// rootKey reuses the Root Key's file when it is there, which check has
// loaded, and writes a new Root Key when it is missing, unless the koko
// server's data is sealed already: no Root Key is ever written over sealed
// data.
func (in *Installer) rootKey(sealed bool) (*seal.RootKey, error) {
	path := in.path(RootKeyFile)
	switch _, err := os.Stat(path); {
	case errors.Is(err, fs.ErrNotExist) && !sealed:
		if err := in.writeFile(RootKeyFile, 0o600, "root", bytes.NewReader(seal.NewRootKey())); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, fmt.Errorf("read the Root Key file %s: %w", RootKeyFile, err)
	default:
		if err := in.own(RootKeyFile, 0o600, "root"); err != nil {
			return nil, err
		}
	}
	return seal.Load(path)
}

// binary puts the running kokod at its fixed path, unless it runs from
// there already, as when koko server install runs the kokod installed.
func (in *Installer) binary() error {
	src, err := os.Open(in.Binary)
	if err != nil {
		return fmt.Errorf("read the kokod file %s: %w", in.Binary, err)
	}
	defer func() { _ = src.Close() }()
	srcInfo, err := src.Stat()
	if err != nil {
		return fmt.Errorf("read the kokod file %s: %w", in.Binary, err)
	}
	if dstInfo, err := os.Stat(in.path(BinaryPath)); err == nil && os.SameFile(srcInfo, dstInfo) {
		return in.own(BinaryPath, 0o755, "root")
	}
	return in.writeFile(BinaryPath, 0o755, "root", src)
}

// dir makes the directory p, with mode and owner, making any parent it lacks
// as a system directory (0755) would be.
func (in *Installer) dir(p string, mode fs.FileMode, owner string) error {
	real := in.path(p)
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(p), err)
	}
	if err := os.Mkdir(real, mode); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("create %s: %w", p, err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return fmt.Errorf("create %s: %w", p, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("create %s: it is there, but not a directory", p)
	}
	return in.own(p, mode, owner)
}

// own gives the file p, which koko owns, its mode and owner, whatever the
// umask made it or a copy put back left it.
func (in *Installer) own(p string, mode fs.FileMode, owner string) error {
	if err := os.Chmod(in.path(p), mode); err != nil {
		return fmt.Errorf("set the mode of %s: %w", p, err)
	}
	if err := in.Machine.Chown(in.path(p), owner); err != nil {
		return fmt.Errorf("make %s owned by %s: %w", p, owner, err)
	}
	return nil
}

// writeFile writes p from r through a temporary file beside it, which takes
// its mode and owner and is synced before it is renamed into place, so p is
// never seen half-written or with another owner, and a running binary is
// replaced, never changed in place.
func (in *Installer) writeFile(p string, mode fs.FileMode, owner string, r io.Reader) (err error) {
	real := in.path(p)
	dir := filepath.Dir(real)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(real)+".*")
	if err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := io.Copy(tmp, r); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	if err := in.Machine.Chown(tmp.Name(), owner); err != nil {
		return fmt.Errorf("write %s owned by %s: %w", p, owner, err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	if err := os.Rename(tmp.Name(), real); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	return nil
}

// syncDir syncs the directory dir, so that a rename into it lasts.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}

// start makes systemd load the unit, enables the service and restarts it, so
// that a koko service an earlier run left running takes the new unit and
// kokod, then waits for its Unix socket.
func (in *Installer) start(ctx context.Context) error {
	for _, args := range [][]string{{"daemon-reload"}, {"enable", UnitName}, {"restart", UnitName}} {
		if err := in.systemctl(ctx, args...); err != nil {
			return err
		}
	}
	return in.waitForSocket(ctx)
}

// systemctl runs systemctl with args, naming them when it fails.
func (in *Installer) systemctl(ctx context.Context, args ...string) error {
	if err := in.Machine.Systemctl(ctx, args...); err != nil {
		return fmt.Errorf("systemctl %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// socketPoll is how often install tries the koko server's socket.
const socketPoll = 50 * time.Millisecond

// waitForSocket waits until the koko service accepts a connection on its
// Unix socket, whose file is there a moment before, for as long as
// timing.InstallSocketWait. A koko server that refuses to start never opens
// it, and the journal says why.
func (in *Installer) waitForSocket(ctx context.Context) error {
	wait := in.Timings.Resolve().InstallSocketWait
	socket := in.path(wire.SocketPath)
	deadline := time.Now().Add(wait)
	d := net.Dialer{Deadline: deadline}
	for {
		if conn, err := d.DialContext(ctx, "unix", socket); err == nil {
			_ = conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the koko service did not open its socket %s within %v: see why with systemctl status koko and journalctl -u koko, then run koko server install again",
				wire.SocketPath, wait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(socketPoll):
		}
	}
}
