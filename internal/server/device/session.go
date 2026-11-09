package device

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/user"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/name"
)

// list is device.list: the caller's own Device Sessions, or for the Owner
// every User's or the one User's req.User names; Revoked ones only with
// req.All.
func list(ctx context.Context, tx store.Tx, caller op.Caller, req api.DeviceListRequest) (api.DeviceListResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.DeviceListResponse{}, err
	}
	of := req.User
	switch {
	case !who.IsOwner():
		if of != "" && of != who.User.Name {
			return api.DeviceListResponse{}, api.CodeOwnerOnly.Errorf("only the Owner lists another User's Device Sessions; name no User to list your own")
		}
		of = who.User.Name
	case of != "":
		if _, err := user.ByName(ctx, tx, of); err != nil {
			return api.DeviceListResponse{}, err
		}
	}
	sessions, err := access.DeviceSessions(ctx, tx, "(?1 = '' OR u.name = ?1) AND (?2 OR d.revoked_at IS NULL)", of, req.All)
	if err != nil {
		return api.DeviceListResponse{}, err
	}
	return api.DeviceListResponse{DeviceSessions: sessions}, nil
}

// revoker answers device.revoke, announcing each revocation on changes and
// logging it to log once it commits.
type revoker struct {
	changes *access.Signal
	log     *slog.Logger
}

// revoke is device.revoke. In one transaction it sets the Device Session's
// revoked time, which keeps the record and its key for ever, and voids the
// valid Join Codes it issued; the Device Sessions it added stay. A Member
// revokes only their own: another User's is answered as an unknown one.
// Once it commits, the revocation is logged and the access-change signal
// names the Device Session, so its Device Connections close.
func (r revoker) revoke(ctx context.Context, tx store.Tx, caller op.Caller, req api.DeviceRevokeRequest) (api.DeviceRevokeResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.DeviceRevokeResponse{}, err
	}
	of := cmp.Or(req.User, who.User.Name)
	if err := name.Check(of); err != nil {
		return api.DeviceRevokeResponse{}, api.CodeInvalidName.Errorf("invalid User name: %v", err)
	}
	if err := name.Check(req.Device); err != nil {
		return api.DeviceRevokeResponse{}, api.CodeInvalidName.Errorf("invalid device name: %v", err)
	}
	named := of + "/" + req.Device
	unknown := api.CodeUnknownDevice.Errorf("no live Device Session is named %q", named)
	if of != who.User.Name && !who.IsOwner() {
		return api.DeviceRevokeResponse{}, unknown
	}
	found, err := access.DeviceSessions(ctx, tx, "u.name = ? AND d.device = ? AND d.revoked_at IS NULL", of, req.Device)
	if err != nil {
		return api.DeviceRevokeResponse{}, err
	}
	if len(found) == 0 {
		return api.DeviceRevokeResponse{}, unknown
	}
	d := found[0]
	if !req.Last {
		var ownersLast bool
		err := tx.QueryRowContext(ctx, `SELECT u.role = ?2 AND NOT EXISTS (SELECT 1 FROM device_sessions o
				WHERE o.user_id = d.user_id AND o.id != d.id AND o.revoked_at IS NULL)
			FROM device_sessions d JOIN users u ON u.id = d.user_id WHERE d.id = ?1`, d.ID, api.RoleOwner).Scan(&ownersLast)
		if err != nil {
			return api.DeviceRevokeResponse{}, fmt.Errorf("tell whether %s is the Owner's last Device Session: %w", named, err)
		}
		if ownersLast {
			return api.DeviceRevokeResponse{}, api.CodeLastDeviceSession.Errorf(
				"%s is the Owner's last live Device Session: once it is revoked, only koko device add on the koko server's own machine adds a device for the Owner; to revoke it anyway, confirm with --last",
				named)
		}
	}
	now := op.Now(ctx).Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, "UPDATE device_sessions SET revoked_at = ? WHERE id = ?", now, d.ID); err != nil {
		return api.DeviceRevokeResponse{}, fmt.Errorf("revoke %s: %w", named, err)
	}
	voided, err := tx.ExecContext(ctx, `UPDATE join_codes SET voided_at = ?1
		WHERE issued_by = ?2 AND used_at IS NULL AND voided_at IS NULL AND expires_at > ?1`, now, d.ID)
	if err != nil {
		return api.DeviceRevokeResponse{}, fmt.Errorf("void the Join Codes %s issued: %w", named, err)
	}
	codes, err := voided.RowsAffected()
	if err != nil {
		return api.DeviceRevokeResponse{}, fmt.Errorf("count the Join Codes %s issued: %w", named, err)
	}
	by := who.By()
	op.AfterTx(ctx, func(committed bool) {
		if committed {
			r.log.Info("device revoked", "user", d.User, "device", d.Device, "device_session", d.ID,
				"revoked_by", by, "join_codes_voided", codes)
		}
	})
	r.changes.Publish(ctx, access.Change{Revoked: []string{d.ID}})
	d.RevokedAt = now
	return api.DeviceRevokeResponse{DeviceSession: d}, nil
}
