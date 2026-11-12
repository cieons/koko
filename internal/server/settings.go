package server

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/installer"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/server/updatecheck"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/build"
	"github.com/cieons/koko/internal/shared/compat"
	"github.com/cieons/koko/internal/shared/joincode"
	"github.com/cieons/koko/internal/shared/release"
	"github.com/cieons/koko/internal/shared/serveraddr"
)

// settings serves the server area's operations on the koko server's
// deployment (P0 spec: install and run): server.show reads it, server.set
// changes the Server Address, the Web URL and the update_check setting, and
// server.check_update checks for a new koko release at once (ADR 0048).
// They act on what the process loaded and runs with, so the process
// registers them.
type settings struct {
	// rootKey is the Root Key the process loaded; only its id is shown.
	rootKey *seal.RootKey
	// rootKeyFile is the Root Key's file as the Owner keeps it: the file
	// the process read it from, or the one that file is a copy of.
	rootKeyFile string
	dataDir     string
	version     string
	// listenPort is the TCP port the koko server listens on.
	listenPort func() int
	// webListen is the address the web listener listens on, "" while it
	// is off.
	webListen string
	// updates checks for a new koko release.
	updates *updatecheck.Checker
	// upgrade is where it learns how it is upgraded.
	upgrade Upgrade
	// changes is the access-change signal, which a change of the Web URL is
	// published on, so that the web listener's terminals end.
	changes *access.Signal
	log     *slog.Logger
}

// registerSettings registers server.show, for every caller, server.set,
// for the Local caller only: no device, the Owner's own included, can move
// where devices connect, and server.check_update and server.upgrade, for
// the Owner.
func registerSettings(a *op.API, s *settings) {
	a.Handle(api.ServerShow, s.show)
	a.Handle(api.ServerSet, s.set, op.LocalOnly)
	a.Handle(api.ServerCheckUpdate, s.checkUpdate)
	a.HandlePrepared(api.ServerUpgrade, s.prepareUpgrade, s.upgradeTo)
}

func (s *settings) show(ctx context.Context, tx store.Tx, caller op.Caller, _ api.ServerShowRequest) (api.ServerShowResponse, error) {
	// Every caller may, but each is resolved, so a Revoked Device Session is
	// refused.
	if _, err := access.Who(ctx, tx, caller); err != nil {
		return api.ServerShowResponse{}, err
	}
	srv, err := s.read(ctx, tx)
	if err != nil {
		return api.ServerShowResponse{}, err
	}
	return api.ServerShowResponse{Server: srv}, nil
}

// set changes the settings req names, and only those. It refuses a browser
// tab: koko web on the koko server's machine reaches the API over the Unix
// socket as the Local caller, yet no browser tab may change where devices
// connect. A Server Address takes effect for the Join Codes issued
// afterwards, so it must fit in one, with the listen port written when it
// names none; a code issued before keeps its own. A Web URL, or its
// clearing, takes effect on the web listener's next request.
func (s *settings) set(ctx context.Context, tx store.Tx, _ op.Caller, req api.ServerSetRequest) (api.ServerSetResponse, error) {
	if _, web := transport.WebPortFrom(ctx); web {
		return api.ServerSetResponse{}, api.CodeLocalOnly.Errorf(
			"server.set answers koko on the koko server's machine, never a browser tab: run koko server set there")
	}
	if req.Address != nil {
		if err := deployment.SetAddress(ctx, tx, *req.Address); err != nil {
			return api.ServerSetResponse{}, err
		}
		// After the write, which a refusal rolls back.
		if err := checkJoinCodeAddress(*req.Address, s.listenPort()); err != nil {
			return api.ServerSetResponse{}, err
		}
	}
	if err := s.setWebURL(ctx, tx, req); err != nil {
		return api.ServerSetResponse{}, err
	}
	turnedOff := false
	if on := req.UpdateCheck; on != nil {
		was, err := deployment.UpdateCheck(ctx, tx)
		if err != nil {
			return api.ServerSetResponse{}, err
		}
		turnedOff = was && !*on
		if err := deployment.SetUpdateCheck(ctx, tx, *on); err != nil {
			return api.ServerSetResponse{}, err
		}
		// The checker follows the setting once it is stored.
		op.AfterTx(ctx, func(committed bool) {
			if committed {
				s.updates.SetOn(*on)
			}
		})
	}
	srv, err := s.read(ctx, tx)
	if err != nil {
		return api.ServerSetResponse{}, err
	}
	if turnedOff {
		// As the checker is once it follows the setting: turned off, it
		// forgets what it found.
		srv.UpdateCheck = updateCheckJSON(false, updatecheck.Result{})
	}
	return api.ServerSetResponse{Server: srv}, nil
}

// checkUpdate checks for a new koko release at once, for the Owner, with
// the update_check setting on or off, since the Owner asked. A check that
// fails is refused with update_check_failed, naming what failed.
func (s *settings) checkUpdate(ctx context.Context, tx store.Tx, caller op.Caller, _ api.ServerCheckUpdateRequest) (api.ServerCheckUpdateResponse, error) {
	if _, err := access.OwnerOnly(ctx, tx, caller, "checks for a new koko release"); err != nil {
		return api.ServerCheckUpdateResponse{}, err
	}
	on, err := deployment.UpdateCheck(ctx, tx)
	if err != nil {
		return api.ServerCheckUpdateResponse{}, err
	}
	r, err := s.updates.Check(ctx)
	switch {
	case ctx.Err() != nil, errors.Is(err, updatecheck.ErrClosed):
		// The caller has gone, or the koko server is stopping: no check
		// failed.
		return api.ServerCheckUpdateResponse{}, errors.Join(err, ctx.Err())
	case err != nil:
		return api.ServerCheckUpdateResponse{}, api.CodeUpdateCheckFailed.Errorf("%v", err)
	}
	return api.ServerCheckUpdateResponse{UpdateCheck: updateCheckJSON(on, r), Version: s.version}, nil
}

// prepareUpgrade decides, outside the write's transaction, what
// server.upgrade asks the upgrade helper for (ADR 0048): the Owner only; a
// koko server the helper upgrades; the release req names, or the latest
// the check found, or one a check made now finds, which may take a request
// to GitHub; and a release newer than this koko server's.
func (s *settings) prepareUpgrade(ctx context.Context, read op.Read, caller op.Caller, req api.ServerUpgradeRequest) (string, error) {
	err := read(func(tx store.Tx) error {
		_, err := access.OwnerOnly(ctx, tx, caller, "upgrades the koko server")
		return err
	})
	if err != nil {
		return "", err
	}
	way, err := s.upgrade.way()
	if err != nil {
		return "", err
	}
	switch way {
	case api.UpgradeWayImage:
		// The image's tag, which KOKO_VERSION names, has no v.
		version := "<the new release's version, such as 0.5.0>"
		if v := cmp.Or(req.Version, s.updates.Result().Latest); v != "" {
			version = strings.TrimPrefix(v, "v")
		}
		return "", api.CodeUpgradeUnsupported.Errorf(
			"this koko server runs in the container image, which a new image upgrades: set KOKO_VERSION=%s in .env beside its Compose file, then run docker compose pull && docker compose up -d", version)
	case api.UpgradeWayNone:
		return "", api.CodeUpgradeUnsupported.Errorf(
			"this koko server runs without the upgrade helper, which koko server install sets up with systemd: replace kokod and restart it")
	}
	to := req.Version
	if to == "" {
		if to, err = s.latest(ctx); err != nil {
			return "", err
		}
	}
	return to, s.checkNewer(to, req.Version == "")
}

// upgradeTo asks the upgrade helper to upgrade the koko server to the
// release prepareUpgrade chose: it checks again who asks, refuses a second
// upgrade while one is asked for or runs, then writes the request the
// helper's path unit waits for, through a temporary file renamed into
// place, and logs who asked.
func (s *settings) upgradeTo(ctx context.Context, tx store.Tx, caller op.Caller, _ api.ServerUpgradeRequest, to string) (api.ServerUpgradeResponse, error) {
	who, err := access.OwnerOnly(ctx, tx, caller, "upgrades the koko server")
	if err != nil {
		return api.ServerUpgradeResponse{}, err
	}
	if err := s.upgrade.checkIdle(op.Now(ctx)); err != nil {
		return api.ServerUpgradeResponse{}, err
	}
	if err := s.upgrade.request(to); err != nil {
		return api.ServerUpgradeResponse{}, err
	}
	s.log.Info("asked the upgrade helper to upgrade the koko server", "from", s.version, "to", to, "user", who.User.Name, "asked_by", who.By())
	return api.ServerUpgradeResponse{From: s.version, To: to, State: api.UpgradeRequested}, nil
}

// latest is the tag of koko's latest release, as the last check found it,
// or as a check made now finds it.
func (s *settings) latest(ctx context.Context) (string, error) {
	if latest := s.updates.Result().Latest; latest != "" {
		return latest, nil
	}
	r, err := s.updates.Check(ctx)
	switch {
	case ctx.Err() != nil, errors.Is(err, updatecheck.ErrClosed):
		return "", errors.Join(err, ctx.Err())
	case err != nil:
		return "", api.CodeUpdateCheckFailed.Errorf("%v", err)
	}
	return r.Latest, nil
}

// checkNewer refuses a version that is not a release's tag newer than this
// koko server's: the helper only ever goes forward, and going back is the
// install line with KOKO_VERSION. latest says the version is the latest
// release, which the Owner did not name.
func (s *settings) checkNewer(to string, latest bool) error {
	if !(build.Info{Version: to}).Release() {
		return api.CodeInvalidRequest.Errorf("version is %q, which is not a release's tag, such as v0.5.0", to)
	}
	switch {
	case compat.Newer(to, s.version):
		return nil
	case latest:
		return api.CodeInvalidRequest.Errorf("the koko server runs %s, and the latest release is %s: there is no newer release to upgrade to", s.version, to)
	}
	return api.CodeInvalidRequest.Errorf("%s is not newer than this koko server, %s, and an upgrade only goes forward: to go back, run the install line as root on its machine with KOKO_VERSION=%s: %s",
		to, s.version, to, goBackLine(to))
}

// goBackLine is the install line that installs the release version, as
// root, which is how the koko server goes back (ADR 0026).
func goBackLine(version string) string {
	return "curl -fsSL " + release.GitHub + "/latest/download/install.sh | sudo env KOKO_VERSION=" + version + " sh"
}

// abandonedAfter is how long after it started an outcome that still says
// running is taken as abandoned, as by a helper killed or a machine
// rebooted mid-upgrade: the helper's own steps end well within it, the
// download within 10 minutes and each install within its wait for the
// socket.
const abandonedAfter = 30 * time.Minute

// checkIdle refuses a second upgrade while one is asked for, its request
// still there, or runs, as its outcome says, unless that outcome is older
// than abandonedAfter at now.
func (u Upgrade) checkIdle(now time.Time) error {
	switch _, err := os.Lstat(u.RequestFile); {
	case err == nil:
		return api.CodeUpgradeRunning.Errorf("an upgrade of the koko server is asked for already, and its helper has not taken it yet: koko server show says how it ends")
	case !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("look for an upgrade request: %w", err)
	}
	last, err := u.last()
	if err != nil {
		return err
	}
	if last == nil || last.State != api.UpgradeRunning {
		return nil
	}
	if started, err := time.Parse(time.RFC3339, last.StartedAt); err == nil && now.Sub(started) > abandonedAfter {
		return nil
	}
	return api.CodeUpgradeRunning.Errorf("the koko server's upgrade from %s to %s runs already: koko server show says how it ends", last.From, last.To)
}

// request writes the upgrade request for the release to, which starts the
// helper, through a temporary file beside it renamed into place, so that
// the helper never reads it half-written.
func (u Upgrade) request(to string) (err error) {
	b, err := json.Marshal(installer.UpgradeRequest{Version: to})
	if err != nil {
		return fmt.Errorf("encode the upgrade request: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(u.RequestFile), "."+filepath.Base(u.RequestFile)+".*")
	if err != nil {
		return fmt.Errorf("write the upgrade request: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write the upgrade request: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write the upgrade request: %w", err)
	}
	if err := os.Rename(tmp.Name(), u.RequestFile); err != nil {
		return fmt.Errorf("write the upgrade request: %w", err)
	}
	return nil
}

// updateCheckJSON is the update check as the API answers it: the setting
// on, and what the checks found, r.
func updateCheckJSON(on bool, r updatecheck.Result) api.UpdateCheck {
	u := api.UpdateCheck{Enabled: on, Latest: nonEmpty(r.Latest), Error: nonEmpty(r.Error)}
	if !r.CheckedAt.IsZero() {
		u.CheckedAt = new(r.CheckedAt.UTC().Format(time.RFC3339))
	}
	return u
}

// setWebURL sets or clears the Web URL as req names it, and once the call
// commits, the access-change signal says it changed, when it did, so that
// every terminal that came through the web listener ends: a page there is
// signed out, its cookie the old address's.
func (s *settings) setWebURL(ctx context.Context, tx store.Tx, req api.ServerSetRequest) error {
	before, _, err := deployment.WebURL(ctx, tx)
	if err != nil {
		return err
	}
	if err := writeWebURL(ctx, tx, req); err != nil {
		return err
	}
	after, _, err := deployment.WebURL(ctx, tx)
	if err != nil {
		return err
	}
	if after != before {
		s.changes.Publish(ctx, access.Change{WebURL: true})
	}
	return nil
}

// writeWebURL sets or clears the Web URL as req names it: web_url is the one
// setting --unset clears, since a koko server always has a Server Address.
func writeWebURL(ctx context.Context, tx store.Tx, req api.ServerSetRequest) error {
	for _, k := range req.Unset {
		switch {
		case k == "address":
			return api.CodeInvalidRequest.Errorf("the Server Address cannot be cleared: a koko server always has one")
		case k != "web_url":
			return api.CodeInvalidRequest.Errorf("server.set has no setting %q to clear; it clears web_url", k)
		case req.WebURL != nil:
			return api.CodeInvalidRequest.Errorf("web_url is both set and cleared")
		}
		if err := deployment.SetWebURL(ctx, tx, ""); err != nil {
			return err
		}
	}
	if req.WebURL == nil {
		return nil
	}
	if *req.WebURL == "" {
		return api.CodeInvalidWebURL.Errorf("the Web URL cannot be empty: to clear it, unset web_url")
	}
	return deployment.SetWebURL(ctx, tx, *req.WebURL)
}

// checkJoinCodeAddress refuses, with invalid_address, a Server Address a
// Join Code cannot carry, with listenPort written when it names no port.
func checkJoinCodeAddress(address string, listenPort int) error {
	if err := joincode.CheckAddress(serveraddr.WithPort(address, listenPort)); err != nil {
		return api.CodeInvalidAddress.Errorf("a Join Code cannot carry the Server Address: %v", err)
	}
	return nil
}

// read returns the koko server's deployment, as stored and as the process
// runs it. It names the Root Key's file and id, never the key.
func (s *settings) read(ctx context.Context, tx store.Tx) (api.Server, error) {
	address, err := deployment.Address(ctx, tx)
	if err != nil {
		return api.Server{}, err
	}
	hostKey, err := deployment.HostKey(ctx, tx, s.rootKey)
	if err != nil {
		return api.Server{}, err
	}
	webURL, _, err := deployment.WebURL(ctx, tx)
	if err != nil {
		return api.Server{}, err
	}
	updateCheck, err := deployment.UpdateCheck(ctx, tx)
	if err != nil {
		return api.Server{}, err
	}
	way, err := s.upgrade.way()
	if err != nil {
		return api.Server{}, err
	}
	last, err := s.upgrade.last()
	if err != nil {
		return api.Server{}, err
	}
	return api.Server{
		Address:            address,
		ListenPort:         s.listenPort(),
		HostKeyFingerprint: ssh.FingerprintSHA256(hostKey),
		HostKey:            strings.TrimSpace(string(ssh.MarshalAuthorizedKey(hostKey))),
		Version:            s.version,
		RootKeyFile:        s.rootKeyFile,
		RootKeyID:          s.rootKey.ID(),
		DataDir:            s.dataDir,
		WebURL:             nonEmpty(webURL),
		WebListen:          nonEmpty(s.webListen),
		UpdateCheck:        updateCheckJSON(updateCheck, s.updates.Result()),
		UpgradeWay:         way,
		LastUpgrade:        last,
	}, nil
}

// way says how the koko server is upgraded: in place through the upgrade
// helper while its path unit is there, by a new image in the container
// image, and by hand otherwise.
func (u Upgrade) way() (string, error) {
	if u.HelperUnit != "" {
		_, err := os.Stat(u.HelperUnit)
		switch {
		case err == nil:
			return api.UpgradeWaySystemd, nil
		case !errors.Is(err, fs.ErrNotExist):
			return "", fmt.Errorf("look for the upgrade helper: %w", err)
		}
	}
	if u.Image != "" {
		return api.UpgradeWayImage, nil
	}
	return api.UpgradeWayNone, nil
}

// last is the outcome of the last upgrade through the helper, nil while
// there is none.
func (u Upgrade) last() (*api.LastUpgrade, error) {
	if u.OutcomeFile == "" {
		return nil, nil
	}
	o, err := installer.ReadOutcome(u.OutcomeFile)
	if o == nil || err != nil {
		return nil, err
	}
	return &api.LastUpgrade{
		State: o.State, From: o.From, To: o.To, StartedAt: o.StartedAt,
		FinishedAt: nonEmpty(o.FinishedAt), Message: nonEmpty(o.Message),
	}, nil
}

// nonEmpty is s, or nil when it is empty: a setting null while unset.
func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
