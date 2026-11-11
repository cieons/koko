package installer

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/build"
	"github.com/cieons/koko/internal/shared/compat"
	"github.com/cieons/koko/internal/shared/install"
	"github.com/cieons/koko/internal/shared/release"
)

// requestLimit bounds the request: {"version": "vX.Y.Z"} and no more.
const requestLimit = 256

// ErrNoRequest is kokod upgrade run with no request, as by hand: nothing
// was asked for, so nothing is done or recorded.
var ErrNoRequest = errors.New("no upgrade was asked for: " + RequestFile + " is not there, and kokod upgrade runs when the koko server asks for an upgrade, as koko server upgrade and the web UI do")

// Upgrade is kokod upgrade, the upgrade helper, which koko-upgrade.service
// runs as root when the koko server asks for an upgrade (ADR 0048). It
// takes from the request nothing but the version of a release newer than
// the kokod installed, records that the upgrade runs, downloads that
// release's kokod and checks it, keeps the kokod installed as
// kokod.previous, and runs the new one's kokod install, the upgrade. When
// that fails, it stops the koko service, puts the database's snapshot back
// when the new kokod migrated it, and installs the previous kokod again. It
// records the outcome in OutcomeFile, and returns it; an error only when it
// could record none, such as for ErrNoRequest.
func (in *Installer) Upgrade(ctx context.Context) (Outcome, error) {
	if !in.Machine.RunsAsRoot() {
		return Outcome{}, install.Refuse(install.Unsupported, "kokod upgrade replaces kokod and restarts the koko service, so it runs as root, as koko-upgrade.service runs it: ask for an upgrade with koko server upgrade or the web UI")
	}
	o := Outcome{StartedAt: in.now()}
	to, err := in.readRequest()
	if errors.Is(err, ErrNoRequest) {
		return Outcome{}, err
	}
	o.To = to
	installed, berr := in.Machine.Build(in.path(BinaryPath))
	o.From = installed.Version
	var refused error
	switch {
	case berr != nil:
		refused = fmt.Errorf("read the build of the kokod installed at %s: %w; nothing was changed", BinaryPath, berr)
	case err != nil:
		refused = fmt.Errorf("the upgrade request was refused: %w; nothing was changed", err)
	case !compat.Newer(o.To, o.From):
		refused = fmt.Errorf("the upgrade request was refused: %s is not newer than the kokod installed, %s, and the helper installs only a newer release; nothing was changed", o.To, o.From)
	}
	o.State = UpgradeRunning
	if refused != nil {
		o.State, o.Message, o.FinishedAt = UpgradeFailed, refused.Error(), in.now()
	}
	// The request goes once the outcome says what became of it, whatever
	// that is, so that it starts the helper only once, and server.upgrade,
	// which refuses while either says an upgrade is on, never finds
	// neither meanwhile.
	if err := errors.Join(in.writeOutcome(o), in.removeRequest()); err != nil || refused != nil {
		return o, err
	}

	dir, err := os.MkdirTemp(in.path(UpgradeDir), "download-") // mode 0700
	if err != nil {
		return in.failed(o, fmt.Errorf("make a directory to download kokod %s into: %w; nothing was changed", o.To, err))
	}
	defer func() { _ = os.RemoveAll(dir) }()
	src := in.Releases
	src.Build = in.Machine.Build
	platform := in.Machine.Platform()
	want := release.Want{Info: build.Info{Path: install.KokodPackage, Version: o.To, Platform: platform}}
	file, err := src.Fetch(ctx, o.To, install.KokodAsset(platform), dir, want)
	if err != nil {
		return in.failed(o, fmt.Errorf("%w; nothing was changed", err))
	}
	if err := in.keepPrevious(); err != nil {
		return in.failed(o, fmt.Errorf("%w; nothing was changed", err))
	}

	if err := in.runInstall(ctx, file); err != nil {
		failure := fmt.Sprintf("kokod %s did not install: %v", o.To, err)
		// Putting the previous kokod back goes on when the helper is asked
		// to stop, as when systemd stops it: a koko server left without
		// its kokod is worse than a stop that waits.
		restored, err := in.putBack(context.WithoutCancel(ctx), o.To)
		if err != nil {
			o.State, o.Message = UpgradeFailedNotRestored, fmt.Sprintf(
				"%s; and putting kokod %s back failed: %v: see why with journalctl -u koko -u koko-upgrade", failure, o.From, err)
			return in.finish(o)
		}
		o.State, o.Message = UpgradeFailed, failure+"; "+restored
		return in.finish(o)
	}
	o.State = UpgradeSucceeded
	return in.finish(o)
}

// readRequest reads the koko server's request, and returns the release it
// names. It takes the request only when it is a regular file, not a link,
// owned by the koko user and at most requestLimit bytes, which holds a
// release's tag and nothing else. Upgrade removes it, whatever it holds.
func (in *Installer) readRequest() (string, error) {
	path := in.path(RequestFile)
	link, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNoRequest
	}
	f, err := openRequest(path)
	switch {
	case link != nil && link.Mode()&fs.ModeSymlink != 0:
		if f != nil {
			_ = f.Close()
		}
		return "", fmt.Errorf("%s is a symbolic link, not a file", RequestFile)
	case err != nil:
		return "", fmt.Errorf("read %s: %w", RequestFile, err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("read %s: %w", RequestFile, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", RequestFile)
	}
	owner, err := in.Machine.FileOwner(path, info)
	if err != nil {
		return "", fmt.Errorf("find who owns %s: %w", RequestFile, err)
	}
	if owner != User {
		return "", fmt.Errorf("%s is owned by %s, not by %s, the koko server's user", RequestFile, owner, User)
	}
	b, err := io.ReadAll(io.LimitReader(f, requestLimit+1))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", RequestFile, err)
	}
	if len(b) > requestLimit {
		return "", fmt.Errorf("%s is over %d bytes", RequestFile, requestLimit)
	}
	var req UpgradeRequest
	if err := json.Unmarshal(b, &req, json.RejectUnknownMembers(true)); err != nil {
		return "", fmt.Errorf("%s does not read as a request: %w", RequestFile, err)
	}
	if !(build.Info{Version: req.Version}).Release() {
		return "", fmt.Errorf("%s names %q, which is not a release's tag", RequestFile, req.Version)
	}
	return req.Version, nil
}

// removeRequest removes whatever is at RequestFile, a link itself rather
// than what it names.
func (in *Installer) removeRequest() error {
	if err := os.Remove(in.path(RequestFile)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", RequestFile, err)
	}
	return nil
}

// UpgradeRequest is the koko server's request for an upgrade, as it writes
// it to RequestFile.
type UpgradeRequest struct {
	// Version is the tag of the release to upgrade to, such as v0.5.0.
	Version string `json:"version"`
}

// keepPrevious copies the kokod installed to kokod.previous, which the
// helper installs again when the new one does not start.
func (in *Installer) keepPrevious() error {
	f, err := os.Open(in.path(BinaryPath))
	if err != nil {
		return fmt.Errorf("keep the kokod installed: %w", err)
	}
	defer func() { _ = f.Close() }()
	return in.writeFile(previousFile, 0o755, "root", f)
}

// runInstall runs `kokod install --json` from file, which upgrades the koko
// server, as the install line does. What it prints on stderr goes to the
// helper's, and its refusal becomes the error.
func (in *Installer) runInstall(ctx context.Context, file string) error {
	var stdout, stderr bytes.Buffer
	status, err := in.Machine.Run(ctx, file, []string{"install", "--json"}, &stdout, &stderr)
	if err != nil {
		return err
	}
	if status == 0 {
		return nil
	}
	var refused api.ErrorResponse
	if err := json.Unmarshal(stderr.Bytes(), &refused); err == nil && refused.Error.Message != "" {
		return errors.New(refused.Error.Message)
	}
	if in.Stderr != nil {
		_, _ = in.Stderr.Write(stderr.Bytes())
	}
	return fmt.Errorf("kokod install exited with status %d", status)
}

// putBack puts the previous kokod back after the new one did not install:
// it stops the koko service, so that Restart=on-failure cannot bring the new
// kokod up meanwhile, and resets its start limit; puts the database's snapshot back when the new kokod
// migrated it, which is safe because a kokod that never opened its socket
// served nothing; and installs the previous kokod again, which starts the
// koko service. It says what it did.
func (in *Installer) putBack(ctx context.Context, to string) (string, error) {
	if err := in.systemctl(ctx, "stop", UnitName); err != nil {
		return "", err
	}
	// A kokod that exits at its start is restarted until systemd's start
	// limit refuses any further start, the previous kokod's included.
	if err := in.systemctl(ctx, "reset-failed", UnitName); err != nil {
		return "", err
	}
	snapshot, moved, err := store.PutBack(ctx, in.path(DataDir), ".failed-"+to)
	if err != nil {
		return "", fmt.Errorf("put the database back in %s: %w", DataDir, err)
	}
	if err := in.runInstall(ctx, in.path(previousFile)); err != nil {
		return "", err
	}
	if snapshot == "" {
		return "the previous kokod runs again", nil
	}
	return fmt.Sprintf("the previous kokod runs again, on the database as it was before kokod %s migrated it (%s), which is kept as %s",
		to, snapshot, moved), nil
}

// failed records o as failed for err, before the upgrade began.
func (in *Installer) failed(o Outcome, err error) (Outcome, error) {
	o.State, o.Message = UpgradeFailed, err.Error()
	return in.finish(o)
}

// finish records o as the upgrade's final outcome.
func (in *Installer) finish(o Outcome) (Outcome, error) {
	o.FinishedAt = in.now()
	return o, in.writeOutcome(o)
}

// writeOutcome writes o to OutcomeFile, mode 0644, which kokod reads.
func (in *Installer) writeOutcome(o Outcome) error {
	b, err := json.Marshal(o)
	if err != nil {
		return fmt.Errorf("encode the outcome: %w", err)
	}
	return in.writeFile(OutcomeFile, 0o644, "root", strings.NewReader(string(b)+"\n"))
}

// now is the time the outcome records, RFC 3339 in UTC.
func (in *Installer) now() string {
	return time.Now().UTC().Format(time.RFC3339)
}
