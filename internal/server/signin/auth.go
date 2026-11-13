package signin

import (
	"cmp"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/user"
	"github.com/cieons/koko/internal/shared/api"
)

// usedGranularity is how often a Sign-in's Last used time moves at most.
const usedGranularity = time.Minute

// Attempt is who an /auth/ request, or a call on the web listener, came
// from: the client's address, which the rate limits count and the log
// names, and its User-Agent, which a new Sign-in's label is made from.
type Attempt struct {
	Address   string
	UserAgent string
}

// Started is a Sign-in a request began: the token its cookie carries, its
// User, and when it ends at the latest, which the cookie's Max-Age counts
// to.
type Started struct {
	// ID is the Sign-in's id.
	ID      string
	Token   string
	User    api.User
	Expires time.Time
}

// Limited is a refusal by the rate limits, api.CodeTooManyAttempts, with
// how long the client waits before it may try again.
type Limited struct {
	Err  *api.Error
	Wait time.Duration
}

func (l *Limited) Error() string { return l.Err.Error() }
func (l *Limited) Unwrap() error { return l.Err }

// limited is the refusal of an attempt that waits wait.
func limited(wait time.Duration) error {
	minutes := int((wait + time.Minute - 1) / time.Minute)
	return &Limited{Err: api.CodeTooManyAttempts.Errorf("too many attempts failed: try again in %d minute(s)", minutes), Wait: wait}
}

// wrongPassword is the refusal of a wrong name or a wrong Password, the
// same bytes for both.
func wrongPassword() error {
	return api.CodeWrongPassword.Errorf("the User name or the Password is wrong")
}

// now is the time of an /auth/ request on the koko server's clock, as
// op.Now reads it for a call.
func (s *Service) now() time.Time {
	return s.clock().UTC().Truncate(time.Second)
}

// SignIn signs a browser in with a User's name and Password, starting a
// Sign-in. A wrong name or Password is refused with api.CodeWrongPassword,
// the same bytes whether or not a User holds the name, has a Password or was
// Removed, after hashing once all the same. It counts as a failure for the
// client's address and for the name. An address or a name past its limit is
// refused with a *Limited before any hashing, a correct Password included.
func (s *Service) SignIn(ctx context.Context, req api.AuthSignInRequest, a Attempt) (_ Started, err error) {
	now := s.now()
	wait, end := s.limiter.begin(a.Address, req.Name, now)
	if wait > 0 {
		s.log.Info("sign-in refused", "reason", api.CodeTooManyAttempts.Name, "address", a.Address)
		return Started{}, limited(wait)
	}
	failed := false
	defer func() { end(failed) }()
	password := norm.NFC.String(req.Password)
	var u api.User
	var held bool
	var pw sealedPassword
	var has bool
	err = s.store.Read(ctx, func(tx store.Tx) (err error) {
		u, err = user.ByName(ctx, tx, req.Name)
		if _, coded := errors.AsType[*api.Error](err); coded {
			return nil
		}
		if err != nil {
			return err
		}
		held = true
		if u.RemovedAt != "" {
			return nil
		}
		pw, has, err = s.readPassword(ctx, tx, u.ID)
		return err
	})
	if err != nil {
		return Started{}, err
	}
	if !has {
		if err := s.hasher.spend(ctx, password); err != nil {
			return Started{}, err
		}
		failed = true
		return Started{}, s.signInFailed(req.Name, held, a)
	}
	match, stale, err := s.hasher.verify(ctx, pw.phc, password)
	if err != nil {
		return Started{}, err
	}
	if !match {
		failed = true
		return Started{}, s.signInFailed(req.Name, held, a)
	}
	var rehashed string
	if stale {
		if rehashed, err = s.hasher.hash(ctx, password); err != nil {
			return Started{}, err
		}
	}
	var started Started
	err = s.api.Do(ctx, func(ctx context.Context, tx store.Tx) (err error) {
		// The Password may have changed, or the User been Removed, while
		// it was hashed.
		current, has, err := s.readPassword(ctx, tx, u.ID)
		if err != nil {
			return err
		}
		if !has || current.id != pw.id {
			return wrongPassword()
		}
		if rehashed != "" {
			if err := s.rehash(ctx, tx, pw.id, rehashed); err != nil {
				return err
			}
		}
		started, err = s.start(ctx, tx, u, a)
		return err
	})
	if err != nil {
		return Started{}, err
	}
	s.limiter.resetName(u.Name)
	return started, nil
}

// signInFailed logs a failed sign-in with name from a, naming the name only
// when held says a User holds it, and returns its refusal.
func (s *Service) signInFailed(name string, held bool, a Attempt) error {
	attrs := []any{"reason", api.CodeWrongPassword.Name, "address", a.Address}
	if held {
		attrs = append(attrs, "user", name)
	}
	s.log.Info("sign-in refused", attrs...)
	return wrongPassword()
}

// start records a new Sign-in of u from a, at the call's time, and logs it
// once the call commits.
func (s *Service) start(ctx context.Context, tx store.Tx, u api.User, a Attempt) (Started, error) {
	now := op.Now(ctx)
	var b [32]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails
	token := base64.RawURLEncoding.EncodeToString(b[:])
	id := store.NewID(signInPrefix)
	expires := now.Add(access.SignInLifetime)
	browser, system := clientOf(a.UserAgent)
	stamp := now.Format(time.RFC3339)
	_, err := tx.ExecContext(ctx, `INSERT INTO signins
		(id, user_id, token_hash, browser, system, started_address, address, started_at, last_used_at, expires_at)
		VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?6, ?7, ?7, ?8)`,
		id, u.ID, tokenHash(token), browser, system, a.Address, stamp, expires.Format(time.RFC3339))
	if err != nil {
		return Started{}, fmt.Errorf("record a Sign-in of %s: %w", u.Name, err)
	}
	afterCommit(ctx, s.log, "signed in", "user", u.Name, "signin", id, "client", api.ClientLabel(browser, system), "address", a.Address)
	return Started{ID: id, Token: token, User: u, Expires: expires}, nil
}

// link is a Password Link as its record holds it, with its User.
type link struct {
	id   string
	user api.User
	// expiresAt is RFC 3339 in UTC to the second.
	expiresAt string
}

// readLink returns the valid Password Link whose secret is secret, at now.
// One used already is refused with api.CodeLinkUsed; one that expired, was
// voided, is a Removed User's or was never issued, with api.CodeLinkInvalid.
func readLink(ctx context.Context, tx store.Tx, secret string, now time.Time) (link, error) {
	var l link
	var removed, used, voided sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT l.id, u.id, u.name, `+access.Role+`, u.created_at, u.removed_at, l.expires_at, l.used_at, l.voided_at
		FROM password_links l JOIN users u ON u.id = l.user_id WHERE l.secret_hash = ?`, linkHash(secret)).
		Scan(&l.id, &l.user.ID, &l.user.Name, &l.user.Role, &l.user.CreatedAt, &removed, &l.expiresAt, &used, &voided)
	invalid := api.CodeLinkInvalid.Errorf("this Password Link has expired or is no longer valid: ask the Owner for a new one")
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return link{}, invalid
	case err != nil:
		return link{}, fmt.Errorf("read the Password Link: %w", err)
	case used.Valid:
		return l, api.CodeLinkUsed.Errorf("this Password Link was used already: ask the Owner for a new one")
	case voided.Valid, removed.Valid, l.expiresAt <= now.Format(time.RFC3339):
		return l, invalid
	}
	return l, nil
}

// Link answers whom the valid Password Link whose secret is secret is for,
// spending nothing. A refused link counts as a failed attempt for the
// client's address.
func (s *Service) Link(ctx context.Context, secret string, a Attempt) (api.AuthLinkResponse, error) {
	now := s.now()
	end, err := s.beginLink(a, now)
	if err != nil {
		return api.AuthLinkResponse{}, err
	}
	var l link
	err = s.store.Read(ctx, func(tx store.Tx) (err error) {
		l, err = readLink(ctx, tx, secret, now)
		return err
	})
	end(s.linkRefused(l, err, a))
	if err != nil {
		return api.AuthLinkResponse{}, err
	}
	return api.AuthLinkResponse{User: l.user.Name, ExpiresAt: l.expiresAt}, nil
}

// beginLink starts, at now, an attempt from a to spend or read a Password
// Link, which only its address's limit bounds, or refuses it.
func (s *Service) beginLink(a Attempt, now time.Time) (end func(failed bool), err error) {
	wait, end := s.limiter.begin(a.Address, "", now)
	if wait > 0 {
		s.log.Info("password link refused", "reason", api.CodeTooManyAttempts.Name, "address", a.Address)
		return nil, limited(wait)
	}
	return end, nil
}

// linkRefused tells whether err refuses the Password Link l, which counts as
// a failed attempt from a, and logs it when it does.
func (s *Service) linkRefused(l link, err error, a Attempt) bool {
	e, coded := errors.AsType[*api.Error](err)
	if !coded || (e.Code != api.CodeLinkUsed.Name && e.Code != api.CodeLinkInvalid.Name) {
		return false
	}
	attrs := []any{"reason", e.Code, "address", a.Address}
	if l.id != "" {
		attrs = append(attrs, "user", l.user.Name, "password_link", l.id)
	}
	s.log.Info("password link refused", attrs...)
	return true
}

// SetByLink sets a User's Password through their Password Link, spending
// it, and signs this browser in: every other Sign-in of the User ends
// ("password_changed"), their unused links are voided, and their name's
// count of failed sign-ins starts again. The new Password must follow the
// rule (api.CodeInvalidPassword); a refused link counts as a failed attempt
// for the client's address.
func (s *Service) SetByLink(ctx context.Context, secret, password string, a Attempt) (_ Started, err error) {
	now := s.now()
	end, err := s.beginLink(a, now)
	if err != nil {
		return Started{}, err
	}
	var l link
	defer func() { end(s.linkRefused(l, err, a)) }()
	err = s.store.Read(ctx, func(tx store.Tx) (err error) {
		l, err = readLink(ctx, tx, secret, now)
		return err
	})
	if err != nil {
		return Started{}, err
	}
	p, err := checkPassword(l.user, password)
	if err != nil {
		return Started{}, err
	}
	phc, err := s.hasher.hash(ctx, p)
	if err != nil {
		return Started{}, err
	}
	var started Started
	err = s.api.Do(ctx, func(ctx context.Context, tx store.Tx) (err error) {
		now := op.Now(ctx)
		// Another browser may have spent the link while it was hashed.
		if l, err = readLink(ctx, tx, secret, now); err != nil {
			return err
		}
		if err := s.writePassword(ctx, tx, l.user.ID, phc, setByLink, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE password_links SET used_at = ? WHERE id = ?", now.Format(time.RFC3339), l.id); err != nil {
			return fmt.Errorf("spend the Password Link %s: %w", l.id, err)
		}
		if _, err := voidLinks(ctx, tx, l.user.ID, now); err != nil {
			return err
		}
		if started, err = s.start(ctx, tx, l.user, a); err != nil {
			return err
		}
		if _, err := s.endAll(ctx, tx, l.user, api.EndPasswordChanged, started.ID, started.ID); err != nil {
			return err
		}
		afterCommit(ctx, s.log, "password set", "user", l.user.Name, "how", setByLink, "password_link", l.id,
			"signin", started.ID, "address", a.Address)
		return nil
	})
	if err != nil {
		return Started{}, err
	}
	s.limiter.resetName(l.user.Name)
	return started, nil
}

// Change changes the Password of the User the Sign-in whose cookie carries
// token is of, given their current one, keeping this Sign-in and ending
// their others ("password_changed"), and voiding their unused links. A
// wrong current Password is refused with api.CodeWrongPassword and counts
// as a failure for the client's address and for the User's name. Only the
// address's limit bounds it: a lockout of the name stops new sign-ins, not
// the Sign-ins open already.
func (s *Service) Change(ctx context.Context, token string, req api.AuthPasswordRequest, a Attempt) (api.User, error) {
	now := s.now()
	var si api.SignIn
	var u api.User
	var pw sealedPassword
	var has bool
	err := s.store.Read(ctx, func(tx store.Tx) (err error) {
		if si, err = liveByToken(ctx, tx, token, now); err != nil {
			return err
		}
		if u, err = user.ByName(ctx, tx, si.User); err != nil {
			return err
		}
		pw, has, err = s.readPassword(ctx, tx, u.ID)
		return err
	})
	if err != nil {
		return api.User{}, err
	}
	wait, end := s.limiter.begin(a.Address, "", now)
	if wait > 0 {
		s.log.Info("sign-in refused", "reason", api.CodeTooManyAttempts.Name, "address", a.Address, "user", u.Name, "signin", si.ID)
		return api.User{}, limited(wait)
	}
	failed := false
	defer func() { end(failed) }()
	p, err := checkPassword(u, req.Password)
	if err != nil {
		return api.User{}, err
	}
	current := norm.NFC.String(req.Current)
	match := false
	if has {
		if match, _, err = s.hasher.verify(ctx, pw.phc, current); err != nil {
			return api.User{}, err
		}
	}
	if !match {
		failed = true
		s.limiter.countNameFailure(u.Name, now)
		s.log.Info("sign-in refused", "reason", api.CodeWrongPassword.Name, "address", a.Address, "user", u.Name, "signin", si.ID)
		return api.User{}, api.CodeWrongPassword.Errorf("the current Password is wrong")
	}
	phc, err := s.hasher.hash(ctx, p)
	if err != nil {
		return api.User{}, err
	}
	err = s.api.Do(ctx, func(ctx context.Context, tx store.Tx) error {
		now := op.Now(ctx)
		// The Sign-in may have ended, or the Password changed, while it
		// was hashed.
		if _, err := liveByToken(ctx, tx, token, now); err != nil {
			return err
		}
		if latest, has, err := s.readPassword(ctx, tx, u.ID); err != nil || !has || latest.id != pw.id {
			return cmp.Or[error](err, api.CodeWrongPassword.Errorf("the current Password is wrong"))
		}
		if err := s.writePassword(ctx, tx, u.ID, phc, setByChange, now); err != nil {
			return err
		}
		if _, err := voidLinks(ctx, tx, u.ID, now); err != nil {
			return err
		}
		if _, err := s.endAll(ctx, tx, u, api.EndPasswordChanged, si.ID, si.ID); err != nil {
			return err
		}
		afterCommit(ctx, s.log, "password set", "user", u.Name, "how", setByChange, "signin", si.ID, "address", a.Address)
		return nil
	})
	if err != nil {
		return api.User{}, err
	}
	s.limiter.resetName(u.Name)
	return u, nil
}

// SignOut ends the Sign-in whose cookie carries token ("signed_out"), when
// it is live; otherwise it does nothing.
func (s *Service) SignOut(ctx context.Context, token string, a Attempt) error {
	if token == "" {
		return nil
	}
	return s.api.Do(ctx, func(ctx context.Context, tx store.Tx) error {
		si, err := liveByToken(ctx, tx, token, op.Now(ctx))
		if _, coded := errors.AsType[*api.Error](err); coded {
			return nil
		}
		if err != nil {
			return err
		}
		if err := s.end(ctx, tx, []string{si.ID}, op.Now(ctx), api.EndSignedOut, si.ID); err != nil {
			return err
		}
		afterCommit(ctx, s.log, "signed out", "user", si.User, "signin", si.ID, "address", a.Address)
		return nil
	})
}

// Check returns the id of the live Sign-in whose cookie carries token, for
// a call on the web listener, and moves its Last used time and address,
// at most once a minute. No token, or one of no live Sign-in, is refused
// with api.CodeSignInRequired. A Sign-in found expired is recorded as
// ended then ("expired"), and logged, the first time.
func (s *Service) Check(ctx context.Context, token string, a Attempt) (string, error) {
	now := s.now()
	var si api.SignIn
	var recordedEnded bool
	err := s.store.Read(ctx, func(tx store.Tx) (err error) {
		si, recordedEnded, err = byToken(ctx, tx, token, now)
		return err
	})
	if err != nil {
		return "", err
	}
	if si.EndedAt != "" {
		if !recordedEnded {
			if err := s.recordExpired(ctx, si); err != nil {
				return "", err
			}
		}
		return "", signInRequired()
	}
	last, err := time.Parse(time.RFC3339, si.LastUsedAt)
	if err != nil {
		return "", fmt.Errorf("read when the Sign-in %s was last used: %w", si.ID, err)
	}
	if now.Sub(last) >= usedGranularity {
		err := s.api.Do(ctx, func(ctx context.Context, tx store.Tx) error {
			now := op.Now(ctx)
			_, err := tx.ExecContext(ctx, `UPDATE signins SET last_used_at = ?1, address = ?2
				WHERE id = ?3 AND ended_at IS NULL AND last_used_at <= ?4`,
				now.Format(time.RFC3339), a.Address, si.ID, now.Add(-usedGranularity).Format(time.RFC3339))
			return err
		})
		if err != nil {
			return "", fmt.Errorf("record the use of the Sign-in %s: %w", si.ID, err)
		}
	}
	return si.ID, nil
}

// RecordExpiry records the Sign-in whose id is id as ended at its expiry,
// when it is found expired at the koko server's time and its record does
// not say so yet, as the Gateway finds one whose page is open: once that
// commits, the access-change signal names it, as for a cookie that comes
// back expired.
func (s *Service) RecordExpiry(ctx context.Context, id string) error {
	var found []api.SignIn
	err := s.store.Read(ctx, func(tx store.Tx) (err error) {
		found, err = access.SignIns(ctx, tx, s.now(), "s.id = ?", id)
		return err
	})
	if err != nil {
		return fmt.Errorf("read the Sign-in %s: %w", id, err)
	}
	if len(found) != 1 || found[0].EndReason != api.EndExpired {
		return nil
	}
	return s.recordExpired(ctx, found[0])
}

// recordExpired records si, found expired, as ended at its expiry, and once
// that commits logs it and has the access-change signal name it.
func (s *Service) recordExpired(ctx context.Context, si api.SignIn) error {
	return s.api.Do(ctx, func(ctx context.Context, tx store.Tx) error {
		res, err := tx.ExecContext(ctx, "UPDATE signins SET ended_at = ?, end_reason = ? WHERE id = ? AND ended_at IS NULL",
			si.EndedAt, api.EndExpired, si.ID)
		if err != nil {
			return fmt.Errorf("record the Sign-in %s expired: %w", si.ID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("record the Sign-in %s expired: %w", si.ID, err)
		}
		if n == 1 {
			afterCommit(ctx, s.log, "sign-in ended", "user", si.User, "signin", si.ID, "reason", api.EndExpired)
			// Its pages detach at once, rather than at the Gateway's next
			// sweep; the SSH Sessions it opened go on.
			s.changes.Publish(ctx, access.Change{SignIns: []string{si.ID}, SignInEnd: api.EndExpired})
		}
		return nil
	})
}

// signInRequired is the refusal of a call on the web listener without a
// live Sign-in.
func signInRequired() error {
	return api.CodeSignInRequired.Errorf("this browser is not signed in, or its Sign-in has ended: sign in again")
}

// byToken returns the Sign-in whose cookie carries token, as at now, and
// whether its record says it ended. No token, or one of no Sign-in, is
// refused with api.CodeSignInRequired.
func byToken(ctx context.Context, tx store.Tx, token string, now time.Time) (api.SignIn, bool, error) {
	if token == "" {
		return api.SignIn{}, false, signInRequired()
	}
	var id string
	var recordedEnded bool
	err := tx.QueryRowContext(ctx, "SELECT id, ended_at IS NOT NULL FROM signins WHERE token_hash = ?", tokenHash(token)).
		Scan(&id, &recordedEnded)
	if errors.Is(err, sql.ErrNoRows) {
		return api.SignIn{}, false, signInRequired()
	}
	if err != nil {
		return api.SignIn{}, false, fmt.Errorf("read the Sign-in: %w", err)
	}
	found, err := access.SignIns(ctx, tx, now, "s.id = ?", id)
	if err != nil || len(found) == 0 {
		return api.SignIn{}, false, cmp.Or[error](err, signInRequired())
	}
	return found[0], recordedEnded, nil
}

// liveByToken returns the live Sign-in whose cookie carries token, at now;
// any other is refused with api.CodeSignInRequired.
func liveByToken(ctx context.Context, tx store.Tx, token string, now time.Time) (api.SignIn, error) {
	si, _, err := byToken(ctx, tx, token, now)
	if err != nil {
		return api.SignIn{}, err
	}
	if si.EndedAt != "" {
		return api.SignIn{}, signInRequired()
	}
	return si, nil
}
