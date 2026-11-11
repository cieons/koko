package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/transport"
	"github.com/cieons/koko/internal/server/updatecheck"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/joincode"
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
}

// registerSettings registers server.show, for every caller, server.set,
// for the Local caller only: no device, the Owner's own included, can move
// where devices connect, and server.check_update, for the Owner.
func registerSettings(a *op.API, s *settings) {
	a.Handle(api.ServerShow, s.show)
	a.Handle(api.ServerSet, s.set, op.LocalOnly)
	a.Handle(api.ServerCheckUpdate, s.checkUpdate)
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
	if err := setWebURL(ctx, tx, req); err != nil {
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

// updateCheckJSON is the update check as the API answers it: the setting
// on, and what the checks found, r.
func updateCheckJSON(on bool, r updatecheck.Result) api.UpdateCheck {
	u := api.UpdateCheck{Enabled: on, Latest: nonEmpty(r.Latest), Error: nonEmpty(r.Error)}
	if !r.CheckedAt.IsZero() {
		u.CheckedAt = new(r.CheckedAt.UTC().Format(time.RFC3339))
	}
	return u
}

// setWebURL sets or clears the Web URL as req names it: web_url is the one
// setting --unset clears, since a koko server always has a Server Address.
func setWebURL(ctx context.Context, tx store.Tx, req api.ServerSetRequest) error {
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
	}, nil
}

// nonEmpty is s, or nil when it is empty: a setting null while unset.
func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
