// Package device is the device area of the koko server: the Join Codes a
// User's new devices join with, and the Device Sessions they become (P0
// spec: people and Spaces). It issues the codes, keeping only a hash of each
// code's secret, so a copy of the database lets nobody join; it answers
// what a device's key is and whether a code's secret joins it; and it lists
// and revokes Device Sessions, announcing each revocation on the
// access-change signal.
package device

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"time"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/user"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/joincode"
	"github.com/cieons/koko/internal/shared/name"
)

// idPrefix starts every Join Code's id, as in joincode_3f9k2m7q.
const idPrefix = "joincode"

// secretLabel comes before a Join Code's secret in the hash the koko server
// keeps of it, so that hash means nothing else.
const secretLabel = "koko Join Code secret\x00"

// Target is the koko server a Join Code joins.
type Target struct {
	// Address is the Server Address with its port written.
	Address string
	// Fingerprint is the SHA-256 of the koko server's host key in SSH wire
	// format.
	Fingerprint [sha256.Size]byte
}

// Issuer issues Join Codes for the koko server its target names. It is the
// one way a Join Code is issued: device.add, user.invite and initialisation
// all issue through it.
type Issuer struct {
	target func(context.Context, store.Tx) (Target, error)
}

// NewIssuer returns an Issuer of Join Codes for the koko server target
// names, which it reads inside each call's transaction.
func NewIssuer(target func(context.Context, store.Tx) (Target, error)) *Issuer {
	return &Issuer{target: target}
}

// Register adds the device operations to the API, issuing with issuer. Each
// revocation is announced on changes and logged to log once it commits.
func Register(a *op.API, issuer *Issuer, changes *access.Signal, log *slog.Logger) {
	a.Handle(api.DeviceAdd, issuer.add)
	a.Handle(api.DeviceList, list)
	a.Handle(api.DeviceRevoke, revoker{changes: changes, log: log}.revoke)
}

func (i *Issuer) add(ctx context.Context, tx store.Tx, caller op.Caller, req api.DeviceAddRequest) (api.DeviceAddResponse, error) {
	jc, err := i.Issue(ctx, tx, caller, req)
	if err != nil {
		return api.DeviceAddResponse{}, err
	}
	return api.DeviceAddResponse{JoinCode: jc}, nil
}

// Issue issues, as caller, a Join Code for one new device of the User
// req.User names, or of the caller's own User when it names none, and binds
// the device name req.Device to it, or device-<n> when it names none. Only
// the Owner or an Administrator names another User, whom they must be able
// to act on (access.Identity.MayActOn). The code is valid from the call's
// time for as long as req.ValidFor chooses, 15 minutes by default
// (api.Validity). It is returned once: the koko server keeps only the hash
// of its secret.
//
// The caller's own User is its Device Session's, or the Owner for the Local
// caller (ADR 0008). The issuer is recorded: the caller's Device Session or
// Sign-in, or the Unix socket.
func (i *Issuer) Issue(ctx context.Context, tx store.Tx, caller op.Caller, req api.DeviceAddRequest) (api.JoinCode, error) {
	validity, err := api.Validity(req.ValidFor)
	if err != nil {
		return api.JoinCode{}, err
	}
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.JoinCode{}, err
	}
	u := who.User
	if req.User != "" && req.User != u.Name {
		if !who.Administers() {
			return api.JoinCode{}, api.CodeOwnerOnly.Errorf("only the Owner or an Administrator issues a Join Code for another User's device; name no User to issue one for your own")
		}
		if u, err = user.Active(ctx, tx, req.User); err != nil {
			return api.JoinCode{}, err
		}
		if err := who.MayActOn(u, "issues a Join Code for"); err != nil {
			return api.JoinCode{}, err
		}
	}
	now := op.Now(ctx)
	device, err := deviceName(ctx, tx, u, req.Device, now)
	if err != nil {
		return api.JoinCode{}, err
	}
	target, err := i.target(ctx, tx)
	if err != nil {
		return api.JoinCode{}, fmt.Errorf("read what a Join Code names: %w", err)
	}
	c := joincode.Code{Address: target.Address, Fingerprint: target.Fingerprint}
	_, _ = rand.Read(c.Secret[:]) // crypto/rand.Read never fails
	code, err := c.Encode()
	if err != nil {
		return api.JoinCode{}, api.CodeInvalidAddress.Errorf("cannot issue a Join Code for the Server Address: %v", err)
	}
	jc := api.JoinCode{
		ID:        store.NewID(idPrefix),
		Code:      code,
		User:      u.Name,
		Device:    device,
		ExpiresAt: now.Add(validity).Format(time.RFC3339),
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO join_codes (id, user_id, secret_hash, device, issued_by, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`, jc.ID, u.ID, secretHash(c.Secret), jc.Device, who.Issuer(), jc.ExpiresAt)
	if err != nil {
		return api.JoinCode{}, fmt.Errorf("record a Join Code for %s's device %q: %w", u.Name, jc.Device, err)
	}
	return jc, nil
}

// deviceName checks the device name given for a new device of u, or picks
// device-<n> when none is given, n counting up from one past the number of
// Device Sessions u has ever had. A name is taken while one of u's live
// Device Sessions or valid Join Codes holds it, so two live codes never
// collide.
func deviceName(ctx context.Context, tx store.Tx, u api.User, given string, now time.Time) (string, error) {
	if given != "" {
		if err := name.Check(given); err != nil {
			return "", api.CodeInvalidName.Errorf("invalid device name: %v", err)
		}
	}
	held, err := heldNames(ctx, tx, u, now)
	if err != nil {
		return "", err
	}
	if given != "" {
		if held[given] {
			return "", api.CodeNameTaken.Errorf("%s already has a device or a valid Join Code named %q", u.Name, given)
		}
		return given, nil
	}
	var joined int
	err = tx.QueryRowContext(ctx, "SELECT count(*) FROM device_sessions WHERE user_id = ?", u.ID).Scan(&joined)
	if err != nil {
		return "", fmt.Errorf("count the devices %s has had: %w", u.Name, err)
	}
	for n := joined + 1; ; n++ {
		if candidate := fmt.Sprintf("device-%d", n); !held[candidate] {
			return candidate, nil
		}
	}
}

// heldNames returns the device names u's live Device Sessions and valid Join
// Codes hold at now.
func heldNames(ctx context.Context, tx store.Tx, u api.User, now time.Time) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT device FROM device_sessions WHERE user_id = ?1 AND revoked_at IS NULL
		UNION SELECT device FROM join_codes
		WHERE user_id = ?1 AND used_at IS NULL AND voided_at IS NULL AND expires_at > ?2`, u.ID, now.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("read the device names %s holds: %w", u.Name, err)
	}
	defer func() { _ = rows.Close() }()
	held := map[string]bool{}
	for rows.Next() {
		var device string
		if err := rows.Scan(&device); err != nil {
			return nil, fmt.Errorf("read the device names %s holds: %w", u.Name, err)
		}
		held[device] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the device names %s holds: %w", u.Name, err)
	}
	return held, nil
}

// secretHash is what the koko server keeps of a Join Code: the SHA-256 of
// its secret, behind secretLabel. The secret's 128 random bits make a slow
// hash needless.
func secretHash(secret [joincode.SecretSize]byte) []byte {
	sum := sha256.Sum256(append([]byte(secretLabel), secret[:]...))
	return sum[:]
}
