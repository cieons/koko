// Package webauthn is koko's own WebAuthn verifier (ADR 0061): it checks a
// browser's answer when a User makes a passkey (Register) and when one signs
// in with it (Assert), as WebAuthn Level 3's ceremonies do for what koko asks
// of an authenticator (#228, "Passkeys"). It is pure: it reads no store and
// no clock. Its caller finds the passkey an answer names, keeps what Register
// and Assert return, and answers every refusal with passkey_refused.
//
// It takes three algorithms (EdDSA on Ed25519, ES256 on P-256, RS256),
// requires user presence and user verification, and takes attestation
// `none`, with the statement of any other format read past unverified,
// since koko trusts no authenticator's maker. A passkey records its owner's
// User id, which a sign-in's userHandle must be, so that no caller supplies
// what the userHandle is compared with. A Ceremony with a challenge under
// 16 bytes, or no RP ID or origin, is refused, so that a flow its caller
// failed to find checks nothing by mistake. Every refusal is ErrRefused,
// whose message quotes nothing from the answer; a signature counter that
// went back is ErrCounterWentBack, which is ErrRefused too.
//
// # Tested against go-webauthn
//
// github.com/go-webauthn/webauthn is this package's test oracle, imported
// only by its tests, so it is in go.mod but in no binary. A differential
// fuzz target, FuzzAgainstGoWebAuthn, makes ceremonies with
// harness.Authenticator, mutates them, and has both verifiers judge each.
// koko accepts nothing the library, configured with koko's policy, refuses:
// it checks the answer's own type and id as the library does, and its CBOR
// reader nests containers no deeper than the library's decoder, 5 levels.
// Where koko refuses what the library accepts, the reason is one of the
// rules koko adds, each computed in the test from the input:
//
//   - the origin is exactly the Web URL, where the library folds case and
//     drops a default port, a path and userinfo;
//   - the client data is JSON as encoding/json/v2 reads it: its names match
//     exactly, and a duplicate name or invalid UTF-8 refuses it, where the
//     library's encoding/json takes all three;
//   - the attestation object's keys match exactly, where the library's CBOR
//     decoder folds case;
//   - CBOR map keys are integers or text strings, where the library's
//     decoder takes byte strings, null, undefined, floats and booleans in a
//     value it decodes as any, and one CBOR value holds at most 1,024 items;
//   - a COSE_Key's kty, alg and crv are CBOR integers, where the library's
//     decoder takes a simple value for an integer too;
//   - the extension outputs are a map keyed by text strings, where the
//     library's decoder takes null and undefined for the map or a key;
//   - an RSA key's modulus is 2,048 to 8,192 bits and its exponent 65,537;
//   - the key is checked when the passkey is made, where the library checks
//     it only at its first use (the test parses it with the library's own
//     parser): an EC2 point must be on P-256, an RSA key within the bounds
//     above, and an Ed25519 key 32 bytes long, its point unchecked until a
//     signature, which a bad point fails;
//   - a counter that did not go up is refused, where the library sets
//     CloneWarning.
//
// The library verifies attestation statements other than `none`, which koko
// reads past. The fuzz target makes only `none` ceremonies, and judges one a
// mutation gave another format as the library judges it made `none`. The
// recorded registrations in testdata cover the other formats.
//
// go test runs the fuzz targets' seed corpora. A longer run, which no check
// starts unasked, is a person's to start:
//
//	go test ./internal/server/passkey/webauthn -run '^$' -fuzz '^FuzzAgainstGoWebAuthn$' -fuzztime 10m
//	go test ./internal/server/passkey/webauthn -run '^$' -fuzz '^FuzzCBOR$' -fuzztime 10m
package webauthn

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
)

// ErrRefused is every refusal Register and Assert return, whatever the step:
// koko answers each with passkey_refused. Its message quotes nothing.
var ErrRefused = errors.New("passkey refused")

// ErrCounterWentBack is Assert's refusal of a signature counter that did not
// go up, which suggests a copied authenticator. It is ErrRefused too.
var ErrCounterWentBack = fmt.Errorf("%w: its signature counter went back", ErrRefused)

// A refusal is the error Register and Assert return: ErrRefused, or
// ErrCounterWentBack, with the check that failed for this package's tests.
type refusal struct {
	// check names the check that failed, never a value from the answer.
	check string
	err   error
}

func (r *refusal) Error() string { return r.err.Error() }
func (r *refusal) Unwrap() error { return r.err }

func refuse(check string) error { return &refusal{check: check, err: ErrRefused} }

// A Ceremony is what an answer must match: the relying party koko asked the
// authenticator for, and the challenge it gave.
type Ceremony struct {
	// RPID is the Web URL's host without its port, such as
	// koko.example.com.
	RPID string
	// Origin is the Web URL itself, such as https://koko.example.com, which
	// the answer's origin must equal exactly.
	Origin string
	// Challenge is the challenge koko gave the browser, of minChallenge
	// bytes at least.
	Challenge []byte
}

// minChallenge is the shortest challenge a Ceremony may hold, as WebAuthn
// Level 3 13.4.3 asks; koko gives 32 bytes.
const minChallenge = 16

// checkCeremony refuses a Ceremony that would check nothing: a short or
// absent challenge, or no RP ID or origin, as from a flow its caller did
// not find.
func checkCeremony(c Ceremony) error {
	if len(c.Challenge) < minChallenge || c.RPID == "" || c.Origin == "" {
		return refuse("ceremony")
	}
	return nil
}

// checkCredential checks an answer's own members, as the library does: its
// type is public-key, and its id is rawId in base64url.
func checkCredential(id, typ string, rawID []byte) error {
	if typ != "public-key" {
		return refuse("credential type")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil || len(decoded) == 0 || !bytes.Equal(decoded, rawID) {
		return refuse("credential id")
	}
	return nil
}

// Bytes is binary data that JSON carries as base64url, as a browser's
// PublicKeyCredential.toJSON() writes it without padding.
type Bytes []byte

// UnmarshalJSON reads a base64url string, with or without padding, as the
// library reads one.
func (b *Bytes) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	v, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
	if err != nil {
		return errors.New("not base64url")
	}
	*b = v
	return nil
}

// A Registration is a browser's answer to navigator.credentials.create(),
// as its toJSON() gives it; members koko does not read are left out.
type Registration struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	RawID    Bytes  `json:"rawId"`
	Response struct {
		ClientDataJSON    Bytes `json:"clientDataJSON"`
		AttestationObject Bytes `json:"attestationObject"`
	} `json:"response"`
}

// An Assertion is a browser's answer to navigator.credentials.get(), as its
// toJSON() gives it; members koko does not read are left out.
type Assertion struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	RawID    Bytes  `json:"rawId"`
	Response struct {
		ClientDataJSON    Bytes `json:"clientDataJSON"`
		AuthenticatorData Bytes `json:"authenticatorData"`
		Signature         Bytes `json:"signature"`
		UserHandle        Bytes `json:"userHandle"`
	} `json:"response"`
}

// A Passkey is what koko keeps of one: what Register returns, and what
// Assert checks an answer against.
type Passkey struct {
	// RPID is the host it was made for.
	RPID string
	// UserID is its owner's User id, the user.id koko gave the
	// authenticator, which a sign-in's userHandle must be.
	UserID       []byte
	CredentialID []byte
	// PublicKey is its COSE_Key as the authenticator gave it, and Algorithm
	// that key's COSE algorithm: EdDSA, ES256 or RS256.
	PublicKey []byte
	Algorithm int
	// SignCount is the authenticator's signature counter at its last use, 0
	// for one that keeps none.
	SignCount uint32
	// BackupEligible is its backup eligibility (BE), fixed when it is made;
	// BackedUp its backup state (BS) at its last use.
	BackupEligible bool
	BackedUp       bool
}

// The flags of authenticator data (WebAuthn Level 3, 6.1).
const (
	flagUP = 1 << 0
	flagUV = 1 << 2
	flagBE = 1 << 3
	flagBS = 1 << 4
	flagAT = 1 << 6
	flagED = 1 << 7
)

// maxCredentialID is the longest credential id WebAuthn allows.
const maxCredentialID = 1023

// Register verifies a browser's answer to a passkey's creation for c, made
// for the User whose id is user, and returns the new passkey. The caller
// checks that no passkey holds its credential id already.
func Register(c Ceremony, user []byte, r Registration) (Passkey, error) {
	return register(c, user, r, true)
}

// register is Register, with user verification required only when
// requireUV is set: the W3C test vectors, most of which were made without
// it, reach it through export_test.go.
func register(c Ceremony, user []byte, r Registration, requireUV bool) (Passkey, error) {
	if err := checkCeremony(c); err != nil {
		return Passkey{}, err
	}
	if len(user) == 0 {
		return Passkey{}, refuse("ceremony")
	}
	if err := checkCredential(r.ID, r.Type, r.RawID); err != nil {
		return Passkey{}, err
	}
	if err := checkClientData(c, r.Response.ClientDataJSON, "webauthn.create"); err != nil {
		return Passkey{}, err
	}

	obj, err := decodeCBOR(r.Response.AttestationObject)
	if err != nil || !keyedByText(obj) {
		return Passkey{}, refuse("attestation object")
	}
	format, ok := obj.value(textKey("fmt"))
	if !ok || format.major != majorText {
		return Passkey{}, refuse("attestation format")
	}
	// A statement of `none` is an empty map, or, as go-webauthn takes it,
	// absent, null or undefined; any other format's is read past.
	if string(format.str) == "none" {
		if stmt, ok := obj.value(textKey("attStmt")); ok && !isEmptyStatement(stmt) {
			return Passkey{}, refuse("none with a statement")
		}
	}
	raw, ok := obj.value(textKey("authData"))
	if !ok || raw.major != majorBytes {
		return Passkey{}, refuse("authData member")
	}

	ad, err := checkAuthData(c, raw.str, requireUV)
	if err != nil {
		return Passkey{}, err
	}
	if ad.flags&flagAT == 0 {
		return Passkey{}, refuse("attested credential data")
	}
	if len(ad.credentialID) == 0 || !bytes.Equal(ad.credentialID, r.RawID) {
		return Passkey{}, refuse("credential id")
	}
	key, err := parseKey(ad.publicKey)
	if err != nil {
		return Passkey{}, refuse("public key")
	}
	return Passkey{
		RPID:           c.RPID,
		UserID:         bytes.Clone(user),
		CredentialID:   bytes.Clone(ad.credentialID),
		PublicKey:      bytes.Clone(ad.publicKey),
		Algorithm:      key.alg,
		SignCount:      ad.signCount,
		BackupEligible: ad.flags&flagBE != 0,
		BackedUp:       ad.flags&flagBS != 0,
	}, nil
}

// keyedByText reports whether it is a map whose keys are all text strings.
func keyedByText(it item) bool {
	if it.major != majorMap {
		return false
	}
	for i := 0; i < len(it.items); i += 2 {
		if it.items[i].major != majorText {
			return false
		}
	}
	return true
}

// The encodings of the CBOR simple values null and undefined, which have no
// other: a float of the same argument is another value.
const (
	cborNull      = 0xf6
	cborUndefined = 0xf7
)

// isEmptyStatement reports whether stmt is an empty attestation statement.
func isEmptyStatement(stmt item) bool {
	if len(stmt.raw) == 1 && (stmt.raw[0] == cborNull || stmt.raw[0] == cborUndefined) {
		return true
	}
	return stmt.major == majorMap && len(stmt.items) == 0
}

// Assert verifies a browser's sign-in answer for c against p, the passkey
// its RawID names, which the caller found. The answer's userHandle must be
// p's UserID. It returns p with the counter and backup state the answer
// carries, to keep. A counter that did not go up is ErrCounterWentBack.
func Assert(c Ceremony, a Assertion, p Passkey) (Passkey, error) {
	return assert(c, a, p, true)
}

// assert is Assert, with user verification required only when requireUV is
// set, for the W3C test vectors through export_test.go.
func assert(c Ceremony, a Assertion, p Passkey, requireUV bool) (Passkey, error) {
	if err := checkCeremony(c); err != nil {
		return Passkey{}, err
	}
	if err := checkCredential(a.ID, a.Type, a.RawID); err != nil {
		return Passkey{}, err
	}
	if len(a.RawID) == 0 || !bytes.Equal(a.RawID, p.CredentialID) || p.RPID != c.RPID {
		return Passkey{}, refuse("passkey")
	}
	if len(a.Response.UserHandle) == 0 || !bytes.Equal(a.Response.UserHandle, p.UserID) {
		return Passkey{}, refuse("user handle")
	}
	if err := checkClientData(c, a.Response.ClientDataJSON, "webauthn.get"); err != nil {
		return Passkey{}, err
	}
	ad, err := checkAuthData(c, a.Response.AuthenticatorData, requireUV)
	if err != nil {
		return Passkey{}, err
	}
	if (ad.flags&flagBE != 0) != p.BackupEligible {
		return Passkey{}, refuse("backup eligibility changed")
	}

	key, err := parseKey(p.PublicKey)
	if err != nil || key.alg != p.Algorithm {
		return Passkey{}, refuse("stored key")
	}
	clientDataHash := sha256.Sum256(a.Response.ClientDataJSON)
	signed := append(bytes.Clone(a.Response.AuthenticatorData), clientDataHash[:]...)
	if !key.verify(signed, a.Response.Signature) {
		return Passkey{}, refuse("signature")
	}

	if (ad.signCount != 0 || p.SignCount != 0) && ad.signCount <= p.SignCount {
		return Passkey{}, &refusal{check: "counter", err: ErrCounterWentBack}
	}
	p.SignCount = ad.signCount
	p.BackedUp = ad.flags&flagBS != 0
	return p, nil
}

// clientData is what koko reads of collected client data (WebAuthn Level
// 3, 5.8.1).
type clientData struct {
	Type         string `json:"type"`
	Challenge    string `json:"challenge"`
	Origin       string `json:"origin"`
	CrossOrigin  bool   `json:"crossOrigin"`
	TopOrigin    string `json:"topOrigin"`
	TokenBinding *struct {
		Status string `json:"status"`
	} `json:"tokenBinding"`
}

// checkClientData checks the client data of an answer to a ceremony of
// type ceremony: the type, the challenge as base64url, the origin exactly,
// no cross-origin call and no top origin. A token binding, which WebAuthn no
// longer uses, must carry a status it defined, as the library requires.
func checkClientData(c Ceremony, raw []byte, ceremony string) error {
	var cd clientData
	if err := json.Unmarshal(raw, &cd); err != nil {
		return refuse("client data")
	}
	if cd.Type != ceremony {
		return refuse("client data type")
	}
	want := base64.RawURLEncoding.EncodeToString(c.Challenge)
	if subtle.ConstantTimeCompare([]byte(cd.Challenge), []byte(want)) != 1 {
		return refuse("challenge")
	}
	if cd.Origin != c.Origin {
		return refuse("origin")
	}
	if cd.CrossOrigin || cd.TopOrigin != "" {
		return refuse("cross-origin")
	}
	if tb := cd.TokenBinding; tb != nil && tb.Status != "present" && tb.Status != "supported" && tb.Status != "not-supported" {
		return refuse("token binding")
	}
	return nil
}

// authData is what koko reads of authenticator data (WebAuthn Level 3,
// 6.1).
type authData struct {
	rpIDHash  []byte
	flags     byte
	signCount uint32
	// credentialID and publicKey are the attested credential data's, when
	// the AT flag is set.
	credentialID []byte
	publicKey    []byte
}

// checkAuthData reads authenticator data and checks it for c: the RP ID
// hash, user presence, user verification when requireUV is set, and no
// backup state without backup eligibility.
func checkAuthData(c Ceremony, raw []byte, requireUV bool) (authData, error) {
	ad, err := parseAuthData(raw)
	if err != nil {
		return authData{}, refuse("authenticator data")
	}
	rpIDHash := sha256.Sum256([]byte(c.RPID))
	if !bytes.Equal(ad.rpIDHash, rpIDHash[:]) {
		return authData{}, refuse("RP ID hash")
	}
	if ad.flags&flagUP == 0 {
		return authData{}, refuse("user presence")
	}
	if requireUV && ad.flags&flagUV == 0 {
		return authData{}, refuse("user verification")
	}
	if ad.flags&flagBS != 0 && ad.flags&flagBE == 0 {
		return authData{}, refuse("backup state without eligibility")
	}
	return ad, nil
}

var errAuthData = errors.New("not authenticator data")

// parseAuthData reads authenticator data: the attested credential data when
// the AT flag announces it, a map of extension outputs read past when the
// ED flag does, and nothing after.
func parseAuthData(raw []byte) (authData, error) {
	if len(raw) < 37 {
		return authData{}, errAuthData
	}
	ad := authData{
		rpIDHash:  raw[:32],
		flags:     raw[32],
		signCount: binary.BigEndian.Uint32(raw[33:37]),
	}
	rest := raw[37:]
	if ad.flags&flagAT != 0 {
		// The AAGUID, then the credential id's length and the id.
		if len(rest) < 18 {
			return authData{}, errAuthData
		}
		n := int(binary.BigEndian.Uint16(rest[16:18]))
		rest = rest[18:]
		if n > maxCredentialID || len(rest) < n {
			return authData{}, errAuthData
		}
		ad.credentialID, rest = rest[:n], rest[n:]
		key, after, err := decodeFirst(rest)
		if err != nil {
			return authData{}, errAuthData
		}
		ad.publicKey, rest = key.raw, after
	}
	if ad.flags&flagED != 0 {
		ext, after, err := decodeFirst(rest)
		if err != nil || !keyedByText(ext) {
			return authData{}, errAuthData
		}
		rest = after
	}
	if len(rest) != 0 {
		return authData{}, errAuthData
	}
	return ad, nil
}
