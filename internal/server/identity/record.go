package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// providerPrefix is the prefix of the Identity Provider's record ids.
const providerPrefix = "idprovider"

// provider is the Identity Provider's record, its client secret opened.
type provider struct {
	id, issuer, clientID, name, setAt string
	secret                            string
}

// answer is p as the API answers it, without its secret.
func (p *provider) answer() api.IdentityProvider {
	return api.IdentityProvider{Issuer: p.issuer, ClientID: p.clientID, Name: p.name, ClientSecretSet: p.secret != "", SetAt: p.setAt}
}

// readProvider returns the Identity Provider, its secret opened with
// rootKey, or nil while none is configured.
func readProvider(ctx context.Context, tx store.Tx, rootKey *seal.RootKey) (*provider, error) {
	var p provider
	var sealed []byte
	err := tx.QueryRowContext(ctx, "SELECT id, issuer, client_id, name, secret, set_at FROM identity_provider").
		Scan(&p.id, &p.issuer, &p.clientID, &p.name, &sealed, &p.setAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("read the Identity Provider: %w", err)
	}
	secret, err := rootKey.Open(p.id, sealed)
	if err != nil {
		return nil, fmt.Errorf("open the client secret of the Identity Provider %s: %w", p.id, err)
	}
	p.secret = string(secret)
	return &p, nil
}

// writeProvider records p as the Identity Provider, in place of any, under
// a new id, its secret sealed with rootKey for that id, set at now by the
// Device Session or Sign-in setBy, nil for the Unix socket.
func writeProvider(ctx context.Context, tx store.Tx, rootKey *seal.RootKey, p *provider, now time.Time, setBy *string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM identity_provider"); err != nil {
		return fmt.Errorf("replace the Identity Provider: %w", err)
	}
	p.id, p.setAt = store.NewID(providerPrefix), now.Format(time.RFC3339)
	_, err := tx.ExecContext(ctx, `INSERT INTO identity_provider (one, id, issuer, client_id, name, secret, set_at, set_by)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?)`, p.id, p.issuer, p.clientID, p.name, rootKey.Seal(p.id, []byte(p.secret)), p.setAt, setBy)
	if err != nil {
		return fmt.Errorf("record the Identity Provider: %w", err)
	}
	return nil
}

// deleteProvider removes the Identity Provider's record.
func deleteProvider(ctx context.Context, tx store.Tx) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM identity_provider"); err != nil {
		return fmt.Errorf("remove the Identity Provider: %w", err)
	}
	return nil
}

// merge returns the Identity Provider req makes of current, nil while none
// is configured, checking each setting req names: a first set names all
// four, refused with invalid_request naming those missing.
func merge(current *provider, req api.ServerOIDCSetRequest) (*provider, error) {
	var p provider
	if current != nil {
		p = *current
	} else {
		var missing []string
		for _, f := range []struct {
			name string
			set  bool
		}{
			{"issuer", req.Issuer != nil},
			{"client_id", req.ClientID != nil},
			{"client_secret", req.ClientSecret != nil},
			{"name", req.Name != nil},
		} {
			if !f.set {
				missing = append(missing, f.name)
			}
		}
		if len(missing) > 0 {
			return nil, api.CodeInvalidRequest.Errorf("no Identity Provider is set yet, so its first set names issuer, client_id, client_secret and name: %s missing",
				strings.Join(missing, ", "))
		}
	}
	if req.Issuer != nil {
		if err := checkIssuer(*req.Issuer); err != nil {
			return nil, err
		}
		p.issuer = *req.Issuer
	}
	if req.ClientID != nil {
		if err := checkText("client_id", *req.ClientID, api.MaxClientIDLength); err != nil {
			return nil, err
		}
		p.clientID = *req.ClientID
	}
	if req.ClientSecret != nil {
		if err := checkText("client_secret", *req.ClientSecret, api.MaxClientSecretLength); err != nil {
			return nil, err
		}
		p.secret = *req.ClientSecret
	}
	if req.Name != nil {
		if err := checkName(*req.Name); err != nil {
			return nil, err
		}
		p.name = *req.Name
	}
	return &p, nil
}

// checkIssuer refuses an issuer koko does not take: one that is not
// https:// with a host, or holds a query, a fragment, user information, a
// space or a byte outside printable ASCII, or is over MaxIssuerLength bytes.
// It is kept as given, since discovery and every ID token must name it
// byte for byte.
func checkIssuer(issuer string) error {
	refuse := func(why string) error {
		return api.CodeInvalidProvider.Errorf("issuer is %q, which %s: an issuer is an https:// URL, a path allowed, such as https://idp.example.com/application/o/koko/",
			issuer, why)
	}
	switch {
	case issuer == "":
		return api.CodeInvalidProvider.Errorf("issuer cannot be empty: an issuer is an https:// URL, a path allowed, such as https://idp.example.com/application/o/koko/")
	case len(issuer) > api.MaxIssuerLength:
		return api.CodeInvalidProvider.Errorf("issuer is %d bytes long, over the %d an issuer may be", len(issuer), api.MaxIssuerLength)
	case strings.IndexFunc(issuer, func(r rune) bool { return r <= ' ' || r >= 0x7f }) >= 0:
		return refuse("holds a space or a character outside printable ASCII")
	case !strings.HasPrefix(issuer, "https://"):
		return refuse("does not start with https://")
	case strings.ContainsAny(issuer, "?#"):
		return refuse("holds a query or a fragment")
	}
	u, err := url.Parse(issuer)
	switch {
	case err != nil:
		return refuse("does not parse as a URL")
	case u.Host == "" || u.Hostname() == "":
		return refuse("names no host")
	case u.User != nil:
		return refuse("holds user information")
	}
	return nil
}

// checkText refuses a setting field that is empty, over max bytes, or holds
// a control character, such as the line ending a file of the client
// secret may have kept.
func checkText(field, value string, max int) error {
	switch {
	case value == "":
		return api.CodeInvalidProvider.Errorf("%s cannot be empty", field)
	case len(value) > max:
		return api.CodeInvalidProvider.Errorf("%s is %d bytes long, over the %d it may be", field, len(value), max)
	case strings.IndexFunc(value, unicode.IsControl) >= 0:
		return api.CodeInvalidProvider.Errorf("%s holds a control character, such as a line ending", field)
	}
	return nil
}

// checkName refuses a button's name that is not 1 to MaxProviderNameLength
// characters, holds a control character, or starts or ends with a space.
func checkName(name string) error {
	n := utf8.RuneCountInString(name)
	switch {
	case n == 0 || n > api.MaxProviderNameLength:
		return api.CodeInvalidProvider.Errorf("name is %d characters long, and a name is 1 to %d, such as Authentik", n, api.MaxProviderNameLength)
	case strings.IndexFunc(name, unicode.IsControl) >= 0:
		return api.CodeInvalidProvider.Errorf("name holds a control character")
	case strings.TrimSpace(name) != name:
		return api.CodeInvalidProvider.Errorf("name %q starts or ends with a space", name)
	}
	return nil
}
