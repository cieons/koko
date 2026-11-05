// Package installer is `koko server install`: on a Linux machine with
// systemd, as root, it creates the koko user and the directories, writes the
// Root Key, the binary and the systemd unit, starts the service and
// initialises the koko server over its Unix socket (P0 spec: install and
// run, ADR 0020).
//
// It reaches the machine only through a Machine: a root directory prefixed to
// every path it reads or writes, and the few things only the machine can
// answer or do, such as running systemctl. Tests give it a temporary root and
// a fake.
package installer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/install"
	"github.com/cieons/koko/internal/shared/portowner"
	"github.com/cieons/koko/internal/shared/timing"
	"github.com/cieons/koko/internal/shared/wire"
)

// The koko server's fixed paths on its machine (P0 spec: install and run),
// as the machine names them, before the Machine's root is prefixed.
const (
	// BinaryPath is where the koko binary runs from.
	BinaryPath = "/usr/local/bin/koko"
	// UnitPath is the koko service's systemd unit.
	UnitPath = "/etc/systemd/system/" + UnitName
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
	// minSystemd is the oldest systemd whose LoadCredential= the unit can
	// use.
	minSystemd = 247
)

// Machine is everything install reaches on the machine beyond its files.
type Machine interface {
	// Root is prefixed to every path install reads or writes: "/" on a
	// real machine.
	Root() string
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
}

// Installer installs the koko server on a machine.
type Installer struct {
	Machine Machine
	// Binary is the koko binary to install: the one running.
	Binary string
	// Timings is the timing-constants hook, for tests only: its zero value
	// is the product's timings.
	Timings timing.Timings
}

// Install installs, starts and initialises the koko server, calling
// initialise once its Unix socket is there, and reports what the Owner needs
// next. Every check comes before the first write, so a refusal
// (*install.Refusal) leaves the machine as it was. Until initialisation has
// succeeded, running it again is safe and finishes the job.
func (in *Installer) Install(ctx context.Context, req install.Request, initialise install.Initialise) (install.Report, error) {
	address, err := in.check(ctx, req)
	if err != nil {
		return install.Report{}, err
	}
	key, err := in.write(ctx, req.Port)
	if err != nil {
		return install.Report{}, err
	}
	if err := in.start(ctx); err != nil {
		return install.Report{}, err
	}
	resp, err := initialise(ctx, api.ServerInitRequest{Owner: req.Owner, Address: address})
	if err != nil {
		return install.Report{}, fmt.Errorf("initialise the koko server: %w", err)
	}
	return install.Report{Init: resp, RootKeyFile: RootKeyFile, RootKeyID: key.ID(), Port: req.Port}, nil
}

// path is the real path of a path on the machine.
func (in *Installer) path(p string) string {
	return filepath.Join(in.Machine.Root(), p)
}

// check refuses, before anything is written, a machine that cannot run the
// koko server, a koko server initialised already, a port another process
// holds and a Server Address devices could not reach. It returns the Server
// Address to initialise with.
func (in *Installer) check(ctx context.Context, req install.Request) (string, error) {
	m := in.Machine
	version, err := m.SystemdVersion(ctx)
	if err != nil {
		return "", fmt.Errorf("find systemd's version, which must be %d or later: %w", minSystemd, err)
	}
	if version < minSystemd {
		return "", refuse(install.Unsupported, "systemd %d is too old: the koko server needs systemd %d or later, for LoadCredential= (Debian 11, Ubuntu 22.04, RHEL 9 or later)",
			version, minSystemd)
	}
	if err := in.checkFilesystem(); err != nil {
		return "", err
	}
	if err := in.checkNotInitialised(ctx); err != nil {
		return "", err
	}
	if req.Owner == "" {
		return "", refuse(install.Usage, "the koko server here is not initialised yet: name its Owner with --owner <user>")
	}
	switch p, err := m.PortOwner(ctx, req.Port); {
	case err == nil:
		return "", refuse(install.PortTaken, "port %d is taken by %s: stop it, or run koko server install again with --port <port>", req.Port, p)
	case !errors.Is(err, portowner.ErrNotFound):
		return "", fmt.Errorf("check that port %d is free: %w", req.Port, err)
	}
	address, err := in.address(ctx, req.Address)
	if err != nil {
		return "", err
	}
	// A Root Key file that is there is reused, so it must load: a damaged
	// one is left as it is, never replaced.
	if _, err := seal.Load(in.path(RootKeyFile)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return address, nil
}

// checkFilesystem refuses a data directory on a network filesystem, where
// SQLite's WAL does not work. Before the data directory exists, the
// filesystem it will be made on is checked.
func (in *Installer) checkFilesystem() error {
	dir := DataDir
	for {
		_, err := os.Stat(in.path(dir))
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrNotExist) || dir == "/" {
			return fmt.Errorf("find the filesystem of %s: %w", DataDir, err)
		}
		dir = filepath.Dir(dir)
	}
	fsType, err := in.Machine.FilesystemType(in.path(dir))
	if err != nil {
		return fmt.Errorf("find the filesystem of %s: %w", DataDir, err)
	}
	if networkFilesystem(fsType) {
		return refuse(install.Unsupported, "the data directory %s would be on %s, a network filesystem, where SQLite's WAL does not work: put %s on a local filesystem",
			DataDir, fsType, DataDir)
	}
	return nil
}

// checkNotInitialised reads koko.db as it is, before the service runs, and
// refuses a koko server initialised already: its Owner and its keys, sealed
// under its Root Key, stay as they are, and no Root Key is written over
// them.
func (in *Installer) checkNotInitialised(ctx context.Context) error {
	var initialised bool
	var sealedBy string
	err := store.Peek(ctx, in.path(DataDir), func(tx store.Tx) (err error) {
		if initialised, err = deployment.Initialised(ctx, tx); err != nil {
			return err
		}
		sealedBy, err = deployment.SealedBy(ctx, tx)
		return err
	})
	if err != nil {
		return fmt.Errorf("find whether the koko server in %s is initialised: %w", DataDir, err)
	}
	if initialised {
		return refuse(install.Initialised, "the koko server here is initialised already: %s holds its Owner and its keys, sealed under %s, which stay as they are; issue a Join Code with koko device add",
			DataDir, sealedBy)
	}
	return nil
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
		return "", refuse(install.Usage, "cannot find this machine's IP on its default route (%v): give the Server Address with --address <host[:port]>", err)
	}
	if !isPublic(ip) {
		return "", refuse(install.Usage, "this machine's IP on its default route, %s, is not public, so devices elsewhere cannot reach it: give the Server Address with --address <host[:port]>, such as a DNS name for this machine",
			ip)
	}
	if ip.Is6() {
		return "[" + ip.String() + "]", nil
	}
	return ip.String(), nil
}

// write creates the koko user and the directories, then writes the Root Key
// if it is missing, the binary and the unit, each with its owner and mode,
// and returns the Root Key.
func (in *Installer) write(ctx context.Context, port int) (*seal.RootKey, error) {
	if err := in.Machine.AddUser(ctx, User); err != nil {
		return nil, fmt.Errorf("create the %s user: %w", User, err)
	}
	if err := in.dir(KeyDir, 0o700, "root"); err != nil {
		return nil, err
	}
	if err := in.dir(DataDir, 0o700, User); err != nil {
		return nil, err
	}
	key, err := in.rootKey()
	if err != nil {
		return nil, err
	}
	// The unit runs the binary at its fixed path, so it is in place before
	// the service starts.
	if err := in.binary(); err != nil {
		return nil, err
	}
	unit := Unit(UnitSettings{Port: port})
	if err := in.writeFile(UnitPath, 0o644, "root", strings.NewReader(unit)); err != nil {
		return nil, err
	}
	return key, nil
}

// rootKey reuses the Root Key's file when it is there, which check has
// loaded, and writes a new Root Key when it is missing. A koko server not
// initialised yet holds nothing sealed, so no Root Key is ever written over
// sealed data.
func (in *Installer) rootKey() (*seal.RootKey, error) {
	path := in.path(RootKeyFile)
	switch _, err := os.Stat(path); {
	case errors.Is(err, fs.ErrNotExist):
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

// binary puts the running koko binary at its fixed path, unless it runs
// from there already.
func (in *Installer) binary() error {
	src, err := os.Open(in.Binary)
	if err != nil {
		return fmt.Errorf("read the koko binary %s: %w", in.Binary, err)
	}
	defer func() { _ = src.Close() }()
	srcInfo, err := src.Stat()
	if err != nil {
		return fmt.Errorf("read the koko binary %s: %w", in.Binary, err)
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
// binary, then waits for its Unix socket.
func (in *Installer) start(ctx context.Context) error {
	for _, args := range [][]string{{"daemon-reload"}, {"enable", UnitName}, {"restart", UnitName}} {
		if err := in.Machine.Systemctl(ctx, args...); err != nil {
			return fmt.Errorf("systemctl %s: %w", strings.Join(args, " "), err)
		}
	}
	return in.waitForSocket(ctx)
}

// socketPoll is how often install looks for the koko server's socket.
const socketPoll = 50 * time.Millisecond

// waitForSocket waits until the koko service has opened its Unix socket, for
// as long as timing.InstallSocketWait. A koko server that refuses to start
// never opens it, and the journal says why.
func (in *Installer) waitForSocket(ctx context.Context) error {
	wait := in.Timings.Resolve().InstallSocketWait
	socket := in.path(wire.SocketPath)
	deadline := time.Now().Add(wait)
	for {
		if info, err := os.Stat(socket); err == nil && info.Mode().Type() == fs.ModeSocket {
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

// refuse makes a Refusal with a formatted message, saying that nothing was
// written, which holds for every refusal.
func refuse(reason install.Reason, format string, args ...any) error {
	return &install.Refusal{Reason: reason, Message: fmt.Sprintf(format, args...) + "; nothing was written"}
}
