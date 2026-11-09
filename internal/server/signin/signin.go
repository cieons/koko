// Package signin is the signin area of the koko server: how a browser signs
// in to the web UI on the web listener (ADR 0043). It holds each User's one
// Password, its Argon2id hash sealed under the Root Key; the Password Links
// the Owner issues for a User to set it; and the Sign-ins, one per browser
// signed in. It serves password.link, password.remove, signin.list and
// signin.revoke, and the work of the web front's /auth/ endpoints (auth.go),
// with their rate limits; and it takes the user area's part in an Invite
// for the web and in removing a User.
package signin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/user"
	"github.com/cieons/koko/internal/shared/api"
)

// The prefixes of the area's record ids.
const (
	passwordPrefix = "password"
	linkPrefix     = "passwordlink"
	signInPrefix   = "signin"
)

// How a Password was last set, as passwords.set_how and the "password set"
// log line name it.
const (
	setByLink   = "link"
	setByChange = "change"
)

// Service is the signin area: its operations, the /auth/ endpoints' work,
// and its part in the user area's operations.
type Service struct {
	store   *store.Store
	rootKey *seal.RootKey
	// clock is the koko server's clock, which every /auth/ endpoint reads
	// its time on, as op.Now reads it for an operation.
	clock   func() time.Time
	log     *slog.Logger
	hasher  *hasher
	limiter *limiter
	// api runs the /auth/ endpoints' writes, as writes of the koko
	// server's own (op.API.Do); Register sets it.
	api *op.API
}

// New returns the signin area, whose records live in st, whose Password
// hashes are sealed under rootKey, which reads the time of each /auth/
// request on clock, and logs to log.
func New(st *store.Store, rootKey *seal.RootKey, clock func() time.Time, log *slog.Logger) *Service {
	return &Service{store: st, rootKey: rootKey, clock: clock, log: log, hasher: newHasher(), limiter: newLimiter()}
}

// Register adds the signin operations to the API.
func (s *Service) Register(a *op.API) {
	s.api = a
	a.Handle(api.PasswordLinkIssue, s.link)
	a.Handle(api.PasswordRemove, s.removePassword)
	a.Handle(api.SignInList, s.list)
	a.Handle(api.SignInRevoke, s.revoke)
}

// link is password.link: a Password Link for a User who is not Removed.
// Only the Owner issues one, and a Member is refused before the name is
// read.
func (s *Service) link(ctx context.Context, tx store.Tx, caller op.Caller, req api.PasswordLinkRequest) (api.PasswordLinkResponse, error) {
	who, err := access.OwnerOnly(ctx, tx, caller, "issues Password Links")
	if err != nil {
		return api.PasswordLinkResponse{}, err
	}
	u, err := user.Active(ctx, tx, req.User)
	if err != nil {
		return api.PasswordLinkResponse{}, err
	}
	l, err := s.IssueLink(ctx, tx, who, u, req.ValidFor)
	if err != nil {
		return api.PasswordLinkResponse{}, err
	}
	return api.PasswordLinkResponse{PasswordLink: l}, nil
}

// IssueLink issues, as who, a Password Link for u, valid from the call's
// time for as long as validFor chooses, 15 minutes by default, and voids u's
// earlier unused one, so only the newest works. It is refused with
// api.CodeWebURLUnset while the Web URL is not set, since the link would
// name no address. Once the call commits, the link is logged, without its
// secret. user.invite calls it for an Invite for the web.
func (s *Service) IssueLink(ctx context.Context, tx store.Tx, who access.Identity, u api.User, validFor *int) (api.PasswordLink, error) {
	validity, err := api.Validity(validFor)
	if err != nil {
		return api.PasswordLink{}, err
	}
	webURL, _, err := deployment.WebURL(ctx, tx)
	if err != nil {
		return api.PasswordLink{}, err
	}
	if webURL == "" {
		return api.PasswordLink{}, api.CodeWebURLUnset.Errorf("a Password Link names the Web URL, which is not set: set it with koko server set web_url=https://… on the koko server's machine")
	}
	now := op.Now(ctx)
	if _, err := voidLinks(ctx, tx, u.ID, now); err != nil {
		return api.PasswordLink{}, err
	}
	secret := rand.Text()
	l := api.PasswordLink{
		ID:        store.NewID(linkPrefix),
		User:      u.Name,
		Link:      webURL + "/password#" + secret,
		ExpiresAt: now.Add(validity).Format(time.RFC3339),
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO password_links (id, user_id, secret_hash, issued_by, issued_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`, l.ID, u.ID, linkHash(secret), who.Issuer(), now.Format(time.RFC3339), l.ExpiresAt)
	if err != nil {
		return api.PasswordLink{}, fmt.Errorf("record a Password Link for %s: %w", u.Name, err)
	}
	afterCommit(ctx, s.log, "password link issued", "user", u.Name, "password_link", l.ID,
		"expires_at", l.ExpiresAt, "issued_by", who.By())
	return l, nil
}

// removePassword is password.remove: it deletes a User's Password, ends
// their Sign-ins and voids their unused Password Link, keeping the User and
// their devices. Only the Owner removes one, and a Member is refused before
// the name is read.
func (s *Service) removePassword(ctx context.Context, tx store.Tx, caller op.Caller, req api.PasswordRemoveRequest) (api.PasswordRemoveResponse, error) {
	who, err := access.OwnerOnly(ctx, tx, caller, "removes Passwords")
	if err != nil {
		return api.PasswordRemoveResponse{}, err
	}
	u, err := user.ByName(ctx, tx, req.User)
	if err != nil {
		return api.PasswordRemoveResponse{}, err
	}
	res, err := tx.ExecContext(ctx, "DELETE FROM passwords WHERE user_id = ?", u.ID)
	if err != nil {
		return api.PasswordRemoveResponse{}, fmt.Errorf("remove the Password of %s: %w", u.Name, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return api.PasswordRemoveResponse{}, fmt.Errorf("remove the Password of %s: %w", u.Name, err)
	}
	if n == 0 {
		return api.PasswordRemoveResponse{}, api.CodeNoPassword.Errorf("%s has no Password", u.Name)
	}
	now := op.Now(ctx)
	if _, err := s.endAll(ctx, tx, u, api.EndPasswordRemoved, who.By(), ""); err != nil {
		return api.PasswordRemoveResponse{}, err
	}
	if _, err := voidLinks(ctx, tx, u.ID, now); err != nil {
		return api.PasswordRemoveResponse{}, err
	}
	afterCommit(ctx, s.log, "password removed", "user", u.Name, "removed_by", who.By())
	return api.PasswordRemoveResponse{User: u.Name}, nil
}

// list is signin.list: the caller's own Sign-ins, and everyone's for the
// Owner, of the one User req.User names when it names one; the ended ones
// only with req.All.
func (s *Service) list(ctx context.Context, tx store.Tx, caller op.Caller, req api.SignInListRequest) (api.SignInListResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.SignInListResponse{}, err
	}
	of := req.User
	switch {
	case !who.IsOwner() && of != "" && of != who.User.Name:
		// Another User's Sign-ins are none of a Member's: as for a User
		// with none, whether or not the name is a User's.
		return api.SignInListResponse{SignIns: []api.SignIn{}}, nil
	case !who.IsOwner():
		of = who.User.Name
	case of != "":
		if _, err := user.ByName(ctx, tx, of); err != nil {
			return api.SignInListResponse{}, err
		}
	}
	found, err := access.SignIns(ctx, tx, op.Now(ctx), "(?1 = '' OR u.name = ?1)", of)
	if err != nil {
		return api.SignInListResponse{}, err
	}
	resp := api.SignInListResponse{SignIns: []api.SignIn{}}
	for _, si := range found {
		if req.All || si.EndedAt == "" {
			resp.SignIns = append(resp.SignIns, si)
		}
	}
	return resp, nil
}

// revoke is signin.revoke: it ends the Sign-in req.ID names, the caller's
// own or, for the Owner, anyone's, and answers it as it left it. One that
// has ended already is answered as it is, unchanged. Another User's, for a
// Member, is refused as an id no Sign-in holds.
func (s *Service) revoke(ctx context.Context, tx store.Tx, caller op.Caller, req api.SignInRevokeRequest) (api.SignInRevokeResponse, error) {
	who, err := access.Who(ctx, tx, caller)
	if err != nil {
		return api.SignInRevokeResponse{}, err
	}
	now := op.Now(ctx)
	found, err := access.SignIns(ctx, tx, now, "s.id = ?1 AND (?2 OR u.id = ?3)", req.ID, who.IsOwner(), who.User.ID)
	if err != nil {
		return api.SignInRevokeResponse{}, err
	}
	if len(found) == 0 {
		return api.SignInRevokeResponse{}, api.CodeUnknownSignIn.Errorf("no Sign-in is %q", req.ID)
	}
	si := found[0]
	if si.EndedAt != "" {
		return api.SignInRevokeResponse{SignIn: si}, nil
	}
	if err := end(ctx, tx, si.ID, now, api.EndRevoked, who.By()); err != nil {
		return api.SignInRevokeResponse{}, err
	}
	afterCommit(ctx, s.log, "sign-in revoked", "user", si.User, "signin", si.ID, "revoked_by", who.By())
	si.EndedAt, si.EndReason = now.Format(time.RFC3339), api.EndRevoked
	return api.SignInRevokeResponse{SignIn: si}, nil
}

// Used tells whether u ever used koko in a browser: set a Password, which
// always signs a browser in, or had a Sign-in, ended ones included. The
// user area deletes only a User who never did, nor joined a device.
func (s *Service) Used(ctx context.Context, tx store.Tx, u api.User) (bool, error) {
	var used bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM passwords WHERE user_id = ?1)
		OR EXISTS (SELECT 1 FROM signins WHERE user_id = ?1)`, u.ID).Scan(&used)
	if err != nil {
		return false, fmt.Errorf("tell whether %s ever signed in: %w", u.Name, err)
	}
	return used, nil
}

// Delete deletes the Password Links of u, a User who never used koko, as the
// user area deletes them.
func (s *Service) Delete(ctx context.Context, tx store.Tx, u api.User) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM password_links WHERE user_id = ?", u.ID); err != nil {
		return fmt.Errorf("delete the Password Links of %s: %w", u.Name, err)
	}
	return nil
}

// Remove takes browser access from u as the user area Removes them, by
// being the caller named by: it ends every live Sign-in of theirs
// ("user_removed"), deletes their Password and voids their unused Password
// Links, and says how many Sign-ins and links it ended.
func (s *Service) Remove(ctx context.Context, tx store.Tx, u api.User, by string) (signInsEnded, linksVoided int64, err error) {
	if signInsEnded, err = s.endAll(ctx, tx, u, api.EndUserRemoved, by, ""); err != nil {
		return 0, 0, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM passwords WHERE user_id = ?", u.ID); err != nil {
		return 0, 0, fmt.Errorf("remove the Password of %s: %w", u.Name, err)
	}
	if linksVoided, err = voidLinks(ctx, tx, u.ID, op.Now(ctx)); err != nil {
		return 0, 0, err
	}
	return signInsEnded, linksVoided, nil
}

// endAll ends, at the call's time, every live Sign-in of u but keep's, for
// reason, by being the caller named by, logging each once the call
// commits, and says how many it ended.
func (s *Service) endAll(ctx context.Context, tx store.Tx, u api.User, reason, by, keep string) (int64, error) {
	now := op.Now(ctx)
	live, err := access.SignIns(ctx, tx, now, "u.id = ?1 AND s.ended_at IS NULL AND s.id != ?2", u.ID, keep)
	if err != nil {
		return 0, err
	}
	var ended int64
	for _, si := range live {
		if si.EndedAt != "" {
			// Expired already: it ended by its expiry, not by this call.
			continue
		}
		if err := end(ctx, tx, si.ID, now, reason, by); err != nil {
			return 0, err
		}
		ended++
		afterCommit(ctx, s.log, "sign-in ended", "user", u.Name, "signin", si.ID, "reason", reason, "ended_by", by)
	}
	return ended, nil
}

// end ends the Sign-in id at now for reason, by being the caller named by.
func end(ctx context.Context, tx store.Tx, id string, now time.Time, reason, by string) error {
	_, err := tx.ExecContext(ctx, `UPDATE signins SET ended_at = ?, end_reason = ?, ended_by = ?
		WHERE id = ? AND ended_at IS NULL`, now.Format(time.RFC3339), reason, by, id)
	if err != nil {
		return fmt.Errorf("end the Sign-in %s: %w", id, err)
	}
	return nil
}

// voidLinks voids, at now, every valid Password Link of the User userID,
// and says how many.
func voidLinks(ctx context.Context, tx store.Tx, userID string, now time.Time) (int64, error) {
	stamp := now.Format(time.RFC3339)
	res, err := tx.ExecContext(ctx, `UPDATE password_links SET voided_at = ?1
		WHERE user_id = ?2 AND used_at IS NULL AND voided_at IS NULL AND expires_at > ?1`, stamp, userID)
	if err != nil {
		return 0, fmt.Errorf("void the Password Links of %s: %w", userID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("void the Password Links of %s: %w", userID, err)
	}
	return n, nil
}

// checkPassword checks a new Password for u against the rule and returns it
// as it is hashed, in NFC: 15 to 128 Unicode code points, any characters,
// and not u's name.
func checkPassword(u api.User, password string) (string, error) {
	p := norm.NFC.String(password)
	n := utf8.RuneCountInString(p)
	switch {
	case n < api.MinPasswordLength || n > api.MaxPasswordLength:
		return "", api.CodeInvalidPassword.Errorf("a Password is %d to %d characters long, any characters; this one is %d",
			api.MinPasswordLength, api.MaxPasswordLength, n)
	case p == u.Name:
		return "", api.CodeInvalidPassword.Errorf("a Password is not the User's own name")
	}
	return p, nil
}

// Hash labels keep the secrets of a Password Link and of a Sign-in's cookie
// apart from every other hash koko keeps.
const (
	linkLabel   = "koko password link\x00"
	signInLabel = "koko sign-in\x00"
)

// linkHash is what the koko server keeps of a Password Link: the SHA-256 of
// its secret. The secret's 128 random bits make a slow hash needless.
func linkHash(secret string) []byte {
	sum := sha256.Sum256([]byte(linkLabel + secret))
	return sum[:]
}

// tokenHash is what the koko server keeps of a Sign-in's cookie: the
// SHA-256 of its 256-bit token.
func tokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(signInLabel + token))
	return sum[:]
}

// sealedPassword is a User's Password record: its row's id, which its hash
// is sealed for, and the hash as a PHC string.
type sealedPassword struct {
	id, phc string
}

// readPassword returns the Password of the User userID, unsealed, and
// whether they have one.
func (s *Service) readPassword(ctx context.Context, tx store.Tx, userID string) (sealedPassword, bool, error) {
	var p sealedPassword
	var sealed []byte
	err := tx.QueryRowContext(ctx, "SELECT id, sealed FROM passwords WHERE user_id = ?", userID).Scan(&p.id, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return sealedPassword{}, false, nil
	}
	if err != nil {
		return sealedPassword{}, false, fmt.Errorf("read the Password of %s: %w", userID, err)
	}
	phc, err := s.rootKey.Open(p.id, sealed)
	if err != nil {
		return sealedPassword{}, false, fmt.Errorf("open the Password %s: %w", p.id, err)
	}
	p.phc = string(phc)
	return p, true, nil
}

// writePassword sets the Password of the User userID to the hash phc, as
// set how at now, sealed under the Root Key for a new row id.
func (s *Service) writePassword(ctx context.Context, tx store.Tx, userID, phc, how string, now time.Time) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM passwords WHERE user_id = ?", userID); err != nil {
		return fmt.Errorf("replace the Password of %s: %w", userID, err)
	}
	id := store.NewID(passwordPrefix)
	_, err := tx.ExecContext(ctx, "INSERT INTO passwords (id, user_id, sealed, set_at, set_how) VALUES (?, ?, ?, ?, ?)",
		id, userID, s.rootKey.Seal(id, []byte(phc)), now.Format(time.RFC3339), how)
	if err != nil {
		return fmt.Errorf("record the Password of %s: %w", userID, err)
	}
	return nil
}

// rehash writes phc, a new hash of the same Password, in place of the hash
// of the Password record id, keeping when and how it was set.
func (s *Service) rehash(ctx context.Context, tx store.Tx, id, phc string) error {
	if _, err := tx.ExecContext(ctx, "UPDATE passwords SET sealed = ? WHERE id = ?", s.rootKey.Seal(id, []byte(phc)), id); err != nil {
		return fmt.Errorf("rehash the Password %s: %w", id, err)
	}
	return nil
}

// afterCommit logs msg with attrs to log once the call's transaction has
// committed, and never when it rolls back.
func afterCommit(ctx context.Context, log *slog.Logger, msg string, attrs ...any) {
	op.AfterTx(ctx, func(committed bool) {
		if committed {
			log.Info(msg, attrs...)
		}
	})
}
