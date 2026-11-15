package passkey

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/passkey/webauthn"
	"github.com/cieons/koko/internal/server/signin"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/server/user"
	"github.com/cieons/koko/internal/shared/api"
)

// The purposes of a ceremony's flow.
const (
	purposeSignIn = "signin"
	purposeAdd    = "add"
)

// challengeSize is how many random bytes a ceremony's challenge holds.
const challengeSize = 32

// ceremony is what a flow holds: what its answer must match.
type ceremony struct {
	purpose string
	// rpID and origin are the Web URL's host and the Web URL when the flow
	// began; an answer after the Web URL changed is refused.
	rpID, origin string
	challenge    []byte
	// userID and userName are, for adding a passkey, the User it is made
	// for.
	userID, userName string
	// signIn is the Sign-in a passkey is added from, and link the Sign-in
	// Link it is added through: one of the two, for adding one.
	signIn, link string
}

// The COSE algorithms koko takes, in the order it offers them.
var algorithms = []int{-8, -7, -257}

// refused is every refusal of a passkey's answer, the same bytes whatever
// failed.
func refused() error {
	return api.CodePasskeyRefused.Errorf("the passkey was refused: try again, or sign in another way")
}

// relyingParty returns the Web URL's host, the RP ID, and the Web URL, the
// origin, as tx reads them; a koko server without a Web URL has neither,
// and refuses every ceremony.
func relyingParty(ctx context.Context, tx store.Tx) (rpID, origin string, err error) {
	webURL, _, err := deployment.WebURL(ctx, tx)
	if err != nil {
		return "", "", err
	}
	u, err := url.Parse(webURL)
	if webURL == "" || err != nil {
		return "", "", api.CodeWebURLUnset.Errorf("a passkey is made for the Web URL, which is not set")
	}
	return u.Hostname(), webURL, nil
}

// now is the time of a ceremony's request on the koko server's clock.
func (s *Service) now() time.Time {
	return s.clock().UTC().Truncate(time.Second)
}

// newChallenge returns a ceremony's challenge.
func newChallenge() []byte {
	b := make([]byte, challengeSize)
	_, _ = rand.Read(b) // crypto/rand.Read never fails
	return b
}

var b64 = base64.RawURLEncoding

// BeginSignIn begins signing in with a passkey from a: it holds a flow, and
// answers it with the options for navigator.credentials.get(), which ask
// the authenticator for any of its passkeys for the Web URL's host. It
// counts for no rate limit, and the flows' bounds keep what it holds.
func (s *Service) BeginSignIn(ctx context.Context, a signin.Attempt) (api.AuthPasskeySignInBeginResponse, error) {
	var c ceremony
	err := s.store.Read(ctx, func(tx store.Tx) (err error) {
		c.rpID, c.origin, err = relyingParty(ctx, tx)
		return err
	})
	if err != nil {
		return api.AuthPasskeySignInBeginResponse{}, err
	}
	c.purpose, c.challenge = purposeSignIn, newChallenge()
	return api.AuthPasskeySignInBeginResponse{
		Flow: s.flows.Begin(a.Address, c),
		Options: api.PasskeyRequestOptions{
			Challenge:        b64.EncodeToString(c.challenge),
			RPID:             c.rpID,
			Timeout:          api.PasskeyTimeout,
			UserVerification: "required",
			AllowCredentials: []api.PasskeyDescriptor{},
		},
	}, nil
}

// SignIn signs a browser in with a passkey's answer to the flow req names,
// which it takes, verified or not, starting a Sign-in made with the
// passkey. Its counter, backup state and Last used are stored as the
// Sign-in starts. Every refusal is api.CodePasskeyRefused, the same bytes
// whether the flow, the passkey, its User, its host or the answer failed,
// and counts as a failure for the client's address; an address past its
// limit is refused with a *signin.Limited before anything is read.
func (s *Service) SignIn(ctx context.Context, req api.AuthPasskeySignInRequest, a signin.Attempt) (_ signin.Started, err error) {
	end, err := s.signins.BeginAttempt(a)
	if err != nil {
		return signin.Started{}, err
	}
	var userName string
	failed := false
	defer func() {
		if failed {
			s.refusedLog(a, "signin", userName)
		}
		end(failed)
	}()
	c, ok := s.flows.Take(req.Flow)
	answer, decoded := assertion(req.Credential)
	if !ok || c.purpose != purposeSignIn || !decoded {
		failed = true
		return signin.Started{}, refused()
	}
	var stored record
	err = s.store.Read(ctx, func(tx store.Tx) (err error) {
		stored, err = s.forSignIn(ctx, tx, c, answer.RawID)
		return err
	})
	if err != nil {
		failed = isRefused(err)
		return signin.Started{}, err
	}
	userName = stored.userName
	verified, err := webauthn.Assert(webauthn.Ceremony{RPID: c.rpID, Origin: c.origin, Challenge: c.challenge}, answer, stored.Passkey)
	if errors.Is(err, webauthn.ErrCounterWentBack) {
		s.log.Warn("passkey counter went back", "user", stored.userName, "passkey", stored.id, "address", a.Address)
	}
	if err != nil {
		failed = true
		return signin.Started{}, refused()
	}
	var started signin.Started
	err = s.api.Do(ctx, func(ctx context.Context, tx store.Tx) error {
		// The passkey may have been removed, its User Removed, the Web URL
		// changed, or another answer of the same passkey stored, while it
		// was verified.
		current, err := s.forSignIn(ctx, tx, c, answer.RawID)
		if err != nil {
			return err
		}
		if current.id != stored.id || current.SignCount != stored.SignCount {
			return refused()
		}
		_, err = tx.ExecContext(ctx, "UPDATE passkeys SET sign_count = ?, backed_up = ?, last_used_at = ? WHERE id = ?",
			int64(verified.SignCount), verified.BackedUp, op.Now(ctx).Format(time.RFC3339), stored.id)
		if err != nil {
			return fmt.Errorf("record the use of the passkey %s: %w", stored.id, err)
		}
		u, err := user.ByName(ctx, tx, stored.userName)
		if err != nil {
			return err
		}
		started, err = s.signins.Start(ctx, tx, u, a, api.MethodPasskey, stored.id)
		return err
	})
	if err != nil {
		failed = isRefused(err)
		return signin.Started{}, err
	}
	return started, nil
}

// record is a Passkey as the verifier checks an answer against it, with its
// id and its User's name.
type record struct {
	webauthn.Passkey
	id, userName string
}

// forSignIn returns the Passkey whose credential id is credentialID, as tx
// reads it, for a sign-in of the flow c: one of a User not Removed, made
// for the Web URL's host, which c began under. Any other is refused.
func (s *Service) forSignIn(ctx context.Context, tx store.Tx, c ceremony, credentialID []byte) (record, error) {
	rpID, origin, err := relyingParty(ctx, tx)
	if err != nil || rpID != c.rpID || origin != c.origin {
		return record{}, refused()
	}
	var r record
	var signCount int64
	err = tx.QueryRowContext(ctx, `SELECT p.id, u.id, u.name, p.credential_id, p.public_key, p.algorithm, p.rp_id,
			p.sign_count, p.backup_eligible, p.backed_up
		FROM passkeys p JOIN users u ON u.id = p.user_id
		WHERE p.credential_id = ? AND p.rp_id = ? AND u.removed_at IS NULL`, credentialID, c.rpID).
		Scan(&r.id, &r.UserID, &r.userName, &r.CredentialID, &r.PublicKey, &r.Algorithm, &r.RPID,
			&signCount, &r.BackupEligible, &r.BackedUp)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return record{}, refused()
	case err != nil:
		return record{}, fmt.Errorf("read the passkey: %w", err)
	}
	r.SignCount = uint32(signCount)
	return r, nil
}

// BeginAdd begins adding a passkey from a: through the Sign-in Link whose
// secret req.Link is, or, without one, from the live Sign-in whose cookie
// carries token. It holds a flow, and answers it with the options for
// navigator.credentials.create() for the User, which exclude their
// passkeys for the Web URL's host, spending nothing. A refused link counts
// as a failure for the client's address.
func (s *Service) BeginAdd(ctx context.Context, req api.AuthPasskeyAddBeginRequest, token string, a signin.Attempt) (_ api.AuthPasskeyAddBeginResponse, err error) {
	end, err := s.signins.BeginAttempt(a)
	if err != nil {
		return api.AuthPasskeyAddBeginResponse{}, err
	}
	var l signin.Link
	defer func() { end(req.Link != "" && s.signins.LinkRefused(l, err, a)) }()
	c := ceremony{purpose: purposeAdd, challenge: newChallenge()}
	var exclude []api.PasskeyDescriptor
	err = s.store.Read(ctx, func(tx store.Tx) (err error) {
		if c.rpID, c.origin, err = relyingParty(ctx, tx); err != nil {
			return err
		}
		var u api.User
		if req.Link != "" {
			if l, err = s.signins.CheckLink(ctx, tx, req.Link, s.now()); err != nil {
				return err
			}
			u, c.link = l.User, l.ID
		} else {
			si, err := s.signins.Live(ctx, tx, token, s.now())
			if err != nil {
				return err
			}
			if u, err = user.ByName(ctx, tx, si.User); err != nil {
				return err
			}
			c.signIn = si.ID
		}
		c.userID, c.userName = u.ID, u.Name
		exclude, err = credentialsOf(ctx, tx, u.ID, c.rpID)
		return err
	})
	if err != nil {
		return api.AuthPasskeyAddBeginResponse{}, err
	}
	params := make([]api.PasskeyParam, len(algorithms))
	for i, alg := range algorithms {
		params[i] = api.PasskeyParam{Type: "public-key", Alg: alg}
	}
	return api.AuthPasskeyAddBeginResponse{
		Flow: s.flows.Begin(a.Address, c),
		Options: api.PasskeyCreationOptions{
			RP:                 api.PasskeyRP{ID: c.rpID, Name: "koko"},
			User:               api.PasskeyUser{ID: b64.EncodeToString([]byte(c.userID)), Name: c.userName, DisplayName: c.userName},
			Challenge:          b64.EncodeToString(c.challenge),
			PubKeyCredParams:   params,
			Timeout:            api.PasskeyTimeout,
			ExcludeCredentials: exclude,
			AuthenticatorSelection: api.PasskeySelection{
				ResidentKey: "required", RequireResidentKey: true, UserVerification: "required",
			},
			Attestation: "none",
		},
	}, nil
}

// credentialsOf returns the credential ids of the passkeys of the User
// userID for the host rpID, which a new one's creation excludes.
func credentialsOf(ctx context.Context, tx store.Tx, userID, rpID string) ([]api.PasskeyDescriptor, error) {
	rows, err := tx.QueryContext(ctx, "SELECT credential_id FROM passkeys WHERE user_id = ? AND rp_id = ? ORDER BY added_at, rowid", userID, rpID)
	if err != nil {
		return nil, fmt.Errorf("read the passkeys of %s: %w", userID, err)
	}
	defer func() { _ = rows.Close() }()
	found := []api.PasskeyDescriptor{}
	for rows.Next() {
		var id []byte
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read the passkeys of %s: %w", userID, err)
		}
		found = append(found, api.PasskeyDescriptor{Type: "public-key", ID: b64.EncodeToString(id)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the passkeys of %s: %w", userID, err)
	}
	return found, nil
}

// Added is a passkey Add recorded, and the Sign-in it started, through a
// Sign-in Link alone.
type Added struct {
	User    api.User
	Passkey api.Passkey
	// Started is the Sign-in a Sign-in Link signed this browser in with;
	// nil for a passkey added from a Sign-in, which goes on.
	Started *signin.Started
}

// Add records a passkey from its answer to the flow req names, which it
// takes, verified or not. Through a Sign-in Link it spends the link once
// the answer verifies, names the passkey with the browser's label, and
// signs this browser in, ending the User's other Sign-ins; from a Sign-in,
// which the cookie carrying token must still be, it keeps the Sign-in and
// names the passkey req.Name, or the browser's label. Every refusal of the
// answer, or of the flow, is api.CodePasskeyRefused and counts as a failure
// for the client's address, as a refused link does.
func (s *Service) Add(ctx context.Context, req api.AuthPasskeyAddRequest, token string, a signin.Attempt) (_ Added, err error) {
	end, err := s.signins.BeginAttempt(a)
	if err != nil {
		return Added{}, err
	}
	var c ceremony
	var l signin.Link
	defer func() {
		failed := isRefused(err)
		if failed {
			s.refusedLog(a, "add", c.userName)
		}
		end(failed || c.link != "" && s.signins.LinkRefused(l, err, a))
	}()
	c, ok := s.flows.Take(req.Flow)
	reg, decoded := registration(req.Credential)
	if !ok || c.purpose != purposeAdd || !decoded {
		return Added{}, refused()
	}
	name := signin.Label(a.UserAgent)
	switch {
	case c.link != "" && req.Name != "":
		return Added{}, api.CodeInvalidRequest.Errorf("a passkey made through a Sign-in Link takes the browser's name, and no name of its own")
	case req.Name != "":
		if name, err = checkName(req.Name); err != nil {
			return Added{}, err
		}
	}
	made, err := webauthn.Register(webauthn.Ceremony{RPID: c.rpID, Origin: c.origin, Challenge: c.challenge}, []byte(c.userID), reg)
	if err != nil {
		return Added{}, refused()
	}
	var added Added
	err = s.api.Do(ctx, func(ctx context.Context, tx store.Tx) error {
		now := op.Now(ctx)
		// The Web URL may have changed, the Sign-in ended or the User been
		// Removed, while the person made the passkey.
		rpID, origin, err := relyingParty(ctx, tx)
		if err != nil || rpID != c.rpID || origin != c.origin {
			return refused()
		}
		addedWith := c.link
		if c.signIn != "" {
			si, err := s.signins.Live(ctx, tx, token, now)
			if err != nil {
				return err
			}
			if si.ID != c.signIn {
				return refused()
			}
			addedWith = si.ID
		}
		var held bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM passkeys WHERE credential_id = ?)", made.CredentialID).Scan(&held); err != nil {
			return fmt.Errorf("read the passkeys: %w", err)
		}
		if held {
			return refused()
		}
		id := store.NewID(idPrefix)
		_, err = tx.ExecContext(ctx, `INSERT INTO passkeys (id, user_id, credential_id, public_key, algorithm, rp_id, name,
				sign_count, backup_eligible, backed_up, added_with, added_at)
			SELECT ?, id, ?, ?, ?, ?, ?, ?, ?, ?, ?, ? FROM users WHERE id = ? AND removed_at IS NULL`,
			id, made.CredentialID, made.PublicKey, made.Algorithm, made.RPID, name,
			int64(made.SignCount), made.BackupEligible, made.BackedUp, addedWith, now.Format(time.RFC3339), c.userID)
		if err != nil {
			return fmt.Errorf("record a passkey of %s: %w", c.userName, err)
		}
		if c.link != "" {
			var started signin.Started
			if l, started, err = s.signins.SpendLink(ctx, tx, c.link, a, api.MethodPasskey, id); err != nil {
				return err
			}
			added.Started = &started
		}
		found, err := read(ctx, tx, "p.id = ?", id)
		if err != nil {
			return err
		}
		if len(found) == 0 {
			// The User was Removed meanwhile.
			return refused()
		}
		if added.User, err = user.ByName(ctx, tx, c.userName); err != nil {
			return err
		}
		added.Passkey = found[0]
		afterCommit(ctx, s.log, "passkey added", "user", c.userName, "passkey", id, "name", name, "host", made.RPID,
			"added_with", addedWith, "address", a.Address)
		return nil
	})
	if err != nil {
		return Added{}, err
	}
	return added, nil
}

// checkName checks a passkey's name and returns it trimmed: 1 to
// api.MaxPasskeyNameLength characters, and no control character.
func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	if n == 0 || n > api.MaxPasskeyNameLength || strings.ContainsFunc(name, unicode.IsControl) {
		return "", api.CodeInvalidName.Errorf("a passkey's name is 1 to %d characters, and no control character", api.MaxPasskeyNameLength)
	}
	return name, nil
}

// isRefused tells whether err is a passkey's refusal, which counts as a
// failure for the client's address.
func isRefused(err error) bool {
	e, ok := errors.AsType[*api.Error](err)
	return ok && e.Code == api.CodePasskeyRefused.Name
}

// refusedLog logs a passkey refused in the ceremony named, from a,
// naming its User where one is known: never a value from the answer.
func (s *Service) refusedLog(a signin.Attempt, ceremony, userName string) {
	attrs := []any{"reason", api.CodePasskeyRefused.Name, "ceremony", ceremony, "address", a.Address}
	if userName != "" {
		attrs = append(attrs, "user", userName)
	}
	s.log.Info("sign-in refused", attrs...)
}

// decode reads base64url, with or without padding, as a browser writes
// it.
func decode(s string) ([]byte, bool) {
	b, err := b64.DecodeString(strings.TrimRight(s, "="))
	return b, err == nil
}

// assertion reads a sign-in's answer as the verifier takes it; one whose
// binary members are not base64url is refused.
func assertion(c api.PasskeyAssertion) (webauthn.Assertion, bool) {
	var a webauthn.Assertion
	a.ID, a.Type = c.ID, c.Type
	ok := true
	for _, f := range []struct {
		to   *webauthn.Bytes
		from string
	}{
		{&a.RawID, c.RawID},
		{&a.Response.ClientDataJSON, c.Response.ClientDataJSON},
		{&a.Response.AuthenticatorData, c.Response.AuthenticatorData},
		{&a.Response.Signature, c.Response.Signature},
		{&a.Response.UserHandle, c.Response.UserHandle},
	} {
		b, decoded := decode(f.from)
		*f.to, ok = b, ok && decoded
	}
	return a, ok
}

// registration reads a creation's answer as the verifier takes it; one
// whose binary members are not base64url is refused.
func registration(c api.PasskeyRegistration) (webauthn.Registration, bool) {
	var r webauthn.Registration
	r.ID, r.Type = c.ID, c.Type
	rawID, ok1 := decode(c.RawID)
	clientData, ok2 := decode(c.Response.ClientDataJSON)
	attestation, ok3 := decode(c.Response.AttestationObject)
	r.RawID, r.Response.ClientDataJSON, r.Response.AttestationObject = rawID, clientData, attestation
	return r, ok1 && ok2 && ok3
}
