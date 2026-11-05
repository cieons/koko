// Package device is the device area of the koko server: the Join Codes a
// User's new devices join with (P0 spec: people and Spaces). It issues them,
// and keeps only a hash of each code's secret, so a copy of the database
// lets nobody join.
package device

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"time"

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

// Register adds the device operations to the API, issuing with issuer.
func Register(a *op.API, issuer *Issuer) {
	a.Handle(api.DeviceAdd, issuer.add)
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
// the device name req.Device to it, or device-<n> when it names none. The
// code is valid for joincode.Lifetime from the call's time. It is returned
// once: the koko server keeps only the hash of its secret.
//
// Every caller is the Local caller until Device Sessions exist, and the
// Local caller's own User is the Owner (ADR 0008); the Unix socket is
// recorded as the issuer.
func (i *Issuer) Issue(ctx context.Context, tx store.Tx, caller op.Caller, req api.DeviceAddRequest) (api.JoinCode, error) {
	var u api.User
	var err error
	if req.User == "" {
		u, err = user.Owner(ctx, tx)
	} else {
		u, err = user.ByName(ctx, tx, req.User)
	}
	if err != nil {
		return api.JoinCode{}, err
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
		ExpiresAt: now.Add(joincode.Lifetime).Format(time.RFC3339),
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO join_codes (id, user_id, secret_hash, device, issued_by, expires_at)
		VALUES (?, ?, ?, ?, NULL, ?)`, jc.ID, u.ID, secretHash(c.Secret), jc.Device, jc.ExpiresAt)
	if err != nil {
		return api.JoinCode{}, fmt.Errorf("record a Join Code for %s's device %q: %w", u.Name, jc.Device, err)
	}
	return jc, nil
}

// deviceName checks the device name given for a new device of u, or picks
// device-<n> when none is given. A name is taken while one of u's valid Join
// Codes holds it, so two live codes never collide.
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
	// n starts one past the number of Device Sessions u has ever had: none
	// yet, before any device has joined.
	for n := 1; ; n++ {
		if candidate := fmt.Sprintf("device-%d", n); !held[candidate] {
			return candidate, nil
		}
	}
}

// heldNames returns the device names u's valid Join Codes hold at now.
func heldNames(ctx context.Context, tx store.Tx, u api.User, now time.Time) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT device FROM join_codes
		WHERE user_id = ? AND used_at IS NULL AND voided_at IS NULL AND expires_at > ?`, u.ID, now.Format(time.RFC3339))
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
