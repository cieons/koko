package webauthn_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json/v2"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/passkey/webauthn"
)

// The relying party of these tests: harness.WebURL's.
const (
	rpID   = "koko.example.com"
	origin = harness.WebURL
)

// user is the User id koko gives the authenticator, as bytes.
var user = []byte("user_7Hq2kX9mWb")

var b64 = base64.RawURLEncoding

func random(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// creationOptions are the options koko gives navigator.credentials.create()
// (#228, "What koko asks of an authenticator").
func creationOptions(t testing.TB, challenge []byte) []byte {
	return mustJSON(t, map[string]any{
		"rp":        map[string]any{"id": rpID, "name": "koko"},
		"user":      map[string]any{"id": b64.EncodeToString(user), "name": "alice", "displayName": "alice"},
		"challenge": b64.EncodeToString(challenge),
		"pubKeyCredParams": []map[string]any{
			{"type": "public-key", "alg": webauthn.EdDSA},
			{"type": "public-key", "alg": webauthn.ES256},
			{"type": "public-key", "alg": webauthn.RS256},
		},
		"timeout":                300000,
		"excludeCredentials":     []any{},
		"authenticatorSelection": map[string]any{"residentKey": "required", "userVerification": "required"},
		"attestation":            "none",
	})
}

// requestOptions are the options koko gives navigator.credentials.get(),
// with allow as allowCredentials, which koko leaves empty.
func requestOptions(t testing.TB, challenge []byte, allow ...[]byte) []byte {
	creds := []map[string]any{}
	for _, id := range allow {
		creds = append(creds, map[string]any{"type": "public-key", "id": b64.EncodeToString(id)})
	}
	return mustJSON(t, map[string]any{
		"challenge":        b64.EncodeToString(challenge),
		"rpId":             rpID,
		"allowCredentials": creds,
		"userVerification": "required",
		"timeout":          300000,
	})
}

// create has a answer a creation, and returns the ceremony and the answer.
func create(t testing.TB, a *harness.Authenticator) (webauthn.Ceremony, webauthn.Registration) {
	t.Helper()
	return createWith(t, a, random(32))
}

// createWith is create with the challenge given.
func createWith(t testing.TB, a *harness.Authenticator, challenge []byte) (webauthn.Ceremony, webauthn.Registration) {
	t.Helper()
	out, err := a.Create(creationOptions(t, challenge))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var r webauthn.Registration
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("read the registration: %v", err)
	}
	return webauthn.Ceremony{RPID: rpID, Origin: origin, Challenge: challenge}, r
}

// made has a make a passkey that Register takes, and returns it.
func made(t testing.TB, a *harness.Authenticator) webauthn.Passkey {
	t.Helper()
	c, r := create(t, a)
	p, err := webauthn.Register(c, user, r)
	if err != nil {
		t.Fatalf("register: %v (%s)", err, webauthn.Check(err))
	}
	return p
}

// get has a answer a sign-in, and returns the ceremony and the answer.
func get(t testing.TB, a *harness.Authenticator, allow ...[]byte) (webauthn.Ceremony, webauthn.Assertion) {
	t.Helper()
	return getWith(t, a, random(32), allow...)
}

// getWith is get with the challenge given.
func getWith(t testing.TB, a *harness.Authenticator, challenge []byte, allow ...[]byte) (webauthn.Ceremony, webauthn.Assertion) {
	t.Helper()
	out, err := a.Get(requestOptions(t, challenge, allow...))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var as webauthn.Assertion
	if err := json.Unmarshal(out, &as); err != nil {
		t.Fatalf("read the assertion: %v", err)
	}
	return webauthn.Ceremony{RPID: rpID, Origin: origin, Challenge: challenge}, as
}

// refused fails t unless err is a refusal by check, with the one message
// every refusal has.
func refused(t testing.TB, err error, check string) {
	t.Helper()
	if !errors.Is(err, webauthn.ErrRefused) {
		t.Fatalf("got %v, want a refusal by %q", err, check)
	}
	if got := webauthn.Check(err); got != check {
		t.Fatalf("refused by %q, want %q", got, check)
	}
	if check != "counter" && err.Error() != webauthn.ErrRefused.Error() {
		t.Fatalf("the refusal says %q, want %q", err, webauthn.ErrRefused)
	}
}

func TestRegisterTakesEachAlgorithm(t *testing.T) {
	t.Parallel()
	for _, alg := range []int{webauthn.EdDSA, webauthn.ES256, webauthn.RS256} {
		a := harness.NewAuthenticator(origin)
		a.Algorithm = alg
		a.BackupEligible, a.BackedUp = true, true
		c, r := create(t, a)
		p, err := webauthn.Register(c, user, r)
		if err != nil {
			t.Fatalf("algorithm %d: %v (%s)", alg, err, webauthn.Check(err))
		}
		if p.Algorithm != alg || p.RPID != rpID || !bytes.Equal(p.CredentialID, r.RawID) ||
			p.SignCount != 1 || !p.BackupEligible || !p.BackedUp || len(p.PublicKey) == 0 {
			t.Fatalf("algorithm %d: got %+v", alg, p)
		}

		c, as := get(t, a)
		got, err := webauthn.Assert(c, as, p)
		if err != nil {
			t.Fatalf("algorithm %d: assert: %v (%s)", alg, err, webauthn.Check(err))
		}
		if got.SignCount != 2 || !got.BackedUp {
			t.Fatalf("algorithm %d: assert gave %+v", alg, got)
		}
	}
}

// tamper returns an Authenticator whose answers edit changes before they
// are signed.
func tamper(edit func(u *harness.Unsigned)) *harness.Authenticator {
	a := harness.NewAuthenticator(origin)
	a.Tamper = edit
	return a
}

func setClientData(t testing.TB, key string, value any) func(*harness.Unsigned) {
	return func(u *harness.Unsigned) {
		var cd map[string]any
		if err := json.Unmarshal(u.ClientDataJSON, &cd); err != nil {
			t.Fatal(err)
		}
		cd[key] = value
		u.ClientDataJSON = mustJSON(t, cd)
	}
}

func setFlags(set, clear byte) func(*harness.Unsigned) {
	return func(u *harness.Unsigned) { u.AuthenticatorData[32] = u.AuthenticatorData[32]&^clear | set }
}

// The authenticator data flags.
const (
	flagUP = 1 << 0
	flagUV = 1 << 2
	flagBE = 1 << 3
	flagBS = 1 << 4
	flagAT = 1 << 6
	flagED = 1 << 7
)

// clientDataRefusals are the refusals of client data, the same at either
// ceremony.
func clientDataRefusals(t *testing.T) []struct {
	name  string
	a     *harness.Authenticator
	check string
} {
	wrongType := harness.NewAuthenticator(origin)
	wrongType.Type = "payment.get"
	otherPort := harness.NewAuthenticator("https://koko.example.com:8443")
	plainHTTP := harness.NewAuthenticator("http://koko.example.com")
	crossOrigin := harness.NewAuthenticator(origin)
	crossOrigin.CrossOrigin = true
	return []struct {
		name  string
		a     *harness.Authenticator
		check string
	}{
		{"not JSON", tamper(func(u *harness.Unsigned) { u.ClientDataJSON = []byte(`{"type":`) }), "client data"},
		{"a duplicate name", tamper(func(u *harness.Unsigned) {
			u.ClientDataJSON = append(bytes.TrimSuffix(u.ClientDataJSON, []byte("}")), []byte(`,"origin":"https://koko.example.com"}`)...)
		}), "client data"},
		{"another type", wrongType, "client data type"},
		{"another challenge", tamper(setClientData(t, "challenge", b64.EncodeToString(random(32)))), "challenge"},
		{"the challenge padded", nil, "challenge"},
		{"another port", otherPort, "origin"},
		{"another scheme", plainHTTP, "origin"},
		{"cross-origin", crossOrigin, "cross-origin"},
		{"a top origin alone", tamper(setClientData(t, "topOrigin", origin)), "cross-origin"},
		{"a token binding without a status", tamper(setClientData(t, "tokenBinding", map[string]any{})), "token binding"},
	}
}

// padChallenge makes a's client data carry the challenge with base64
// padding, which is not the challenge's base64url.
func padChallenge(t *testing.T) *harness.Authenticator {
	return tamper(func(u *harness.Unsigned) {
		var cd map[string]any
		if err := json.Unmarshal(u.ClientDataJSON, &cd); err != nil {
			t.Fatal(err)
		}
		cd["challenge"] = cd["challenge"].(string) + "="
		u.ClientDataJSON = mustJSON(t, cd)
	})
}

func TestRegisterRefusesClientData(t *testing.T) {
	t.Parallel()
	for _, tc := range clientDataRefusals(t) {
		t.Run(tc.name, func(t *testing.T) {
			a := tc.a
			if a == nil {
				a = padChallenge(t)
			}
			c, r := create(t, a)
			_, err := webauthn.Register(c, user, r)
			refused(t, err, tc.check)
		})
	}
}

func TestAssertRefusesClientData(t *testing.T) {
	t.Parallel()
	for _, tc := range clientDataRefusals(t) {
		t.Run(tc.name, func(t *testing.T) {
			a := tc.a
			if a == nil {
				a = padChallenge(t)
			}
			// The passkey is made untouched; only the sign-in is spoilt.
			edit, typ, o, cross := a.Tamper, a.Type, a.Origin, a.CrossOrigin
			a.Tamper, a.Type, a.Origin, a.CrossOrigin = nil, "", origin, false
			p := made(t, a)
			a.Tamper, a.Type, a.Origin, a.CrossOrigin = edit, typ, o, cross
			c, as := get(t, a)
			_, err := webauthn.Assert(c, as, p)
			refused(t, err, tc.check)
		})
	}
}

// replace returns data with old, which it must hold once, replaced by new.
func replace(t testing.TB, data []byte, old, new string) []byte {
	t.Helper()
	if bytes.Count(data, []byte(old)) != 1 {
		t.Fatalf("%q is not in the data once", old)
	}
	return bytes.Replace(data, []byte(old), []byte(new), 1)
}

func TestRegisterRefusesTheAttestationObject(t *testing.T) {
	t.Parallel()
	statement := func(format string, stmt []byte) *harness.Authenticator {
		a := harness.NewAuthenticator(origin)
		a.Statement = &harness.Statement{Format: format, CBOR: stmt}
		return a
	}
	tests := []struct {
		name  string
		a     *harness.Authenticator
		edit  func(t testing.TB, obj []byte) []byte
		check string
	}{
		{name: "trailing bytes", edit: func(_ testing.TB, obj []byte) []byte { return append(obj, 0) }, check: "attestation object"},
		{name: "not CBOR", edit: func(_ testing.TB, obj []byte) []byte { return obj[:len(obj)-1] }, check: "attestation object"},
		{name: "an array", edit: func(_ testing.TB, obj []byte) []byte { return append([]byte{0x83}, obj[1:]...) }, check: "attestation object"},
		{name: "a key that is no text", edit: func(t testing.TB, obj []byte) []byte { return replace(t, obj, "\x67attStmt", "\x47attStmt") }, check: "attestation object"},
		{name: "a duplicate key", edit: func(t testing.TB, obj []byte) []byte {
			// fmt twice, then the authData the authenticator wrote.
			_, authData, ok := bytes.Cut(obj, []byte("\x68authData"))
			if !ok {
				t.Fatal("no authData")
			}
			return slices.Concat([]byte{0xa3}, []byte("\x63fmt\x64none\x63fmt\x64none\x68authData"), authData)
		}, check: "attestation object"},
		{name: "no format", edit: func(t testing.TB, obj []byte) []byte { return replace(t, obj, "\x63fmt", "\x63fmu") }, check: "attestation format"},
		{name: "no authenticator data", edit: func(t testing.TB, obj []byte) []byte { return replace(t, obj, "authData", "authDatb") }, check: "authData member"},
		{name: "none with a statement", a: statement("none", []byte{0xa1, 0x63, 's', 'i', 'g', 0x40}), check: "none with a statement"},
		{name: "none with a statement that is no map", a: statement("none", []byte{0x80}), check: "none with a statement"},
		// A half float whose argument is null's, 22.
		{name: "none with a float for a statement", a: statement("none", []byte{0xf9, 0x00, 0x16}), check: "none with a statement"},
		{name: "none with false for a statement", a: statement("none", []byte{0xf4}), check: "none with a statement"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := orNew(tc.a)
			c, r := create(t, a)
			if tc.edit != nil {
				r.Response.AttestationObject = tc.edit(t, r.Response.AttestationObject)
			}
			_, err := webauthn.Register(c, user, r)
			refused(t, err, tc.check)
		})
	}
}

func orNew(a *harness.Authenticator) *harness.Authenticator {
	if a == nil {
		return harness.NewAuthenticator(origin)
	}
	return a
}

func TestRegisterReadsPastOtherStatements(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"packed", "tpm", "made-up"} {
		a := harness.NewAuthenticator(origin)
		// A packed statement whose signature is nonsense: koko verifies none.
		a.Statement = &harness.Statement{Format: format, CBOR: []byte{0xa2, 0x63, 'a', 'l', 'g', 0x26, 0x63, 's', 'i', 'g', 0x41, 0}}
		c, r := create(t, a)
		if _, err := webauthn.Register(c, user, r); err != nil {
			t.Fatalf("%s: %v (%s)", format, err, webauthn.Check(err))
		}
	}
}

func TestRegisterTakesAnEmptyNoneStatement(t *testing.T) {
	t.Parallel()
	// An empty map, null and undefined, as go-webauthn takes them.
	for _, stmt := range [][]byte{{0xa0}, {0xf6}, {0xf7}} {
		a := harness.NewAuthenticator(origin)
		a.Statement = &harness.Statement{Format: "none", CBOR: stmt}
		c, r := create(t, a)
		if _, err := webauthn.Register(c, user, r); err != nil {
			t.Fatalf("%x: %v (%s)", stmt, err, webauthn.Check(err))
		}
	}
	// And absent.
	c, r := create(t, harness.NewAuthenticator(origin))
	r.Response.AttestationObject = slices.Concat([]byte{0xa2}, replace(t, r.Response.AttestationObject[1:], "\x67attStmt\xa0", ""))
	if _, err := webauthn.Register(c, user, r); err != nil {
		t.Fatalf("absent: %v (%s)", err, webauthn.Check(err))
	}
}

func TestRegisterRefusesAuthenticatorData(t *testing.T) {
	t.Parallel()
	otherRP := harness.NewAuthenticator(origin)
	otherRP.RPID = "example.com"
	noUV := harness.NewAuthenticator(origin)
	noUV.SkipUV = true
	backedUpOnly := harness.NewAuthenticator(origin)
	backedUpOnly.BackedUp = true
	tests := []struct {
		name  string
		a     *harness.Authenticator
		check string
	}{
		{"too short", tamper(func(u *harness.Unsigned) { u.AuthenticatorData = u.AuthenticatorData[:36] }), "authenticator data"},
		{"another RP ID", otherRP, "RP ID hash"},
		{"no user presence", tamper(setFlags(0, flagUP)), "user presence"},
		{"no user verification", noUV, "user verification"},
		{"backed up without eligibility", backedUpOnly, "backup state without eligibility"},
		{"no attested credential data", tamper(func(u *harness.Unsigned) {
			u.AuthenticatorData = u.AuthenticatorData[:37]
			u.AuthenticatorData[32] &^= flagAT
		}), "attested credential data"},
		{"attested data unannounced", tamper(setFlags(0, flagAT)), "authenticator data"},
		{"extensions announced and absent", tamper(setFlags(flagED, 0)), "authenticator data"},
		{"extensions keyed by integers", tamper(func(u *harness.Unsigned) {
			u.AuthenticatorData[32] |= flagED
			u.AuthenticatorData = append(u.AuthenticatorData, 0xa1, 0x01, 0x02)
		}), "authenticator data"},
		{"trailing bytes", tamper(func(u *harness.Unsigned) { u.AuthenticatorData = append(u.AuthenticatorData, 0) }), "authenticator data"},
		{"a credential id longer than what follows", tamper(func(u *harness.Unsigned) {
			binary.BigEndian.PutUint16(u.AuthenticatorData[53:55], 1000)
		}), "authenticator data"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, r := create(t, tc.a)
			_, err := webauthn.Register(c, user, r)
			refused(t, err, tc.check)
		})
	}
}

func TestRegisterTakesCredentialIDsUpTo1023Bytes(t *testing.T) {
	t.Parallel()
	for _, n := range []int{1, 1023, 1024} {
		// The authenticator's id of 32 bytes, made n bytes long, which
		// rawId names too.
		a := tamper(func(u *harness.Unsigned) {
			ad := u.AuthenticatorData
			id := make([]byte, n)
			copy(id, ad[55:55+32])
			u.AuthenticatorData = slices.Concat(ad[:53], binary.BigEndian.AppendUint16(nil, uint16(n)), id, ad[55+32:])
		})
		c, r := create(t, a)
		r.RawID = append(make([]byte, 0, n), r.RawID[:min(n, 32)]...)
		r.RawID = append(r.RawID, make([]byte, n-len(r.RawID))...)
		r.ID = b64.EncodeToString(r.RawID)
		_, err := webauthn.Register(c, user, r)
		if n <= 1023 {
			if err != nil {
				t.Fatalf("%d bytes: %v (%s)", n, err, webauthn.Check(err))
			}
			continue
		}
		refused(t, err, "authenticator data")
	}
}

func TestRegisterReadsPastExtensions(t *testing.T) {
	t.Parallel()
	// credProtect: 2, as a security key making a discoverable passkey says.
	a := tamper(func(u *harness.Unsigned) {
		u.AuthenticatorData[32] |= flagED
		u.AuthenticatorData = append(u.AuthenticatorData, append([]byte{0xa1, 0x6b}, append([]byte("credProtect"), 0x02)...)...)
	})
	c, r := create(t, a)
	if _, err := webauthn.Register(c, user, r); err != nil {
		t.Fatalf("%v (%s)", err, webauthn.Check(err))
	}
}

func TestRegisterRefusesACredentialIDOtherThanRawID(t *testing.T) {
	t.Parallel()
	for name, edit := range map[string]func([]byte) []byte{
		"another":   func(id []byte) []byte { return append(slices.Clone(id[:len(id)-1]), id[len(id)-1]^1) },
		"truncated": func(id []byte) []byte { return id[:len(id)-1] },
		"absent":    func([]byte) []byte { return nil },
	} {
		c, r := create(t, harness.NewAuthenticator(origin))
		// The answer's id names the new rawId, so that the authenticator
		// data's credential id is what refuses it.
		r.RawID = edit(r.RawID)
		r.ID = b64.EncodeToString(r.RawID)
		_, err := webauthn.Register(c, user, r)
		if webauthn.Check(err) != "credential id" {
			t.Fatalf("%s: got %v (%s), want a refusal by the credential id", name, err, webauthn.Check(err))
		}
	}
	// An empty credential id, which rawId matches.
	c, r := create(t, tamper(func(u *harness.Unsigned) {
		binary.BigEndian.PutUint16(u.AuthenticatorData[53:55], 0)
		u.AuthenticatorData = slices.Delete(u.AuthenticatorData, 55, 55+32)
	}))
	r.RawID = nil
	_, err := webauthn.Register(c, user, r)
	refused(t, err, "credential id")
}

// A test's own CBOR writer, for COSE_Keys an authenticator would not make.
func head(major byte, n int) []byte {
	m := major << 5
	switch {
	case n < 24:
		return []byte{m | byte(n)}
	case n <= 0xff:
		return []byte{m | 24, byte(n)}
	default:
		return binary.BigEndian.AppendUint16([]byte{m | 25}, uint16(n))
	}
}

func cborInt(n int) []byte {
	if n < 0 {
		return head(1, -1-n)
	}
	return head(0, n)
}

func cborBytes(b []byte) []byte { return append(head(2, len(b)), b...) }

// coseMap encodes the integer labels and values of kv in turn: an int
// value as an integer, a []byte one as a byte string.
func coseMap(kv ...any) []byte {
	out := head(5, len(kv)/2)
	for _, v := range kv {
		switch v := v.(type) {
		case int:
			out = append(out, cborInt(v)...)
		case []byte:
			out = append(out, cborBytes(v)...)
		}
	}
	return out
}

// withKey has an authenticator put key in place of its passkey's COSE_Key.
func withKey(key []byte) *harness.Authenticator {
	return tamper(func(u *harness.Unsigned) {
		n := int(binary.BigEndian.Uint16(u.AuthenticatorData[53:55]))
		u.AuthenticatorData = append(u.AuthenticatorData[:55+n:55+n], key...)
	})
}

// modulus returns an RSA modulus of bits bits; no key need match it, since
// a passkey's creation is not signed with `none`.
func modulus(bits int) []byte {
	n := bytes.Repeat([]byte{0xff}, (bits+7)/8)
	n[0] = byte(0xff >> ((8 - bits%8) % 8))
	n[len(n)-1] |= 1
	return n
}

func TestRegisterChecksTheKey(t *testing.T) {
	t.Parallel()
	x := make([]byte, 32)
	x[31] = 9 // An Ed25519 point needs no check here: verifying refuses one off the curve.
	// The P-256 base point.
	gx := []byte{0x6b, 0x17, 0xd1, 0xf2, 0xe1, 0x2c, 0x42, 0x47, 0xf8, 0xbc, 0xe6, 0xe5, 0x63, 0xa4, 0x40, 0xf2, 0x77, 0x03, 0x7d, 0x81, 0x2d, 0xeb, 0x33, 0xa0, 0xf4, 0xa1, 0x39, 0x45, 0xd8, 0x98, 0xc2, 0x96}
	gy := []byte{0x4f, 0xe3, 0x42, 0xe2, 0xfe, 0x1a, 0x7f, 0x9b, 0x8e, 0xe7, 0xeb, 0x4a, 0x7c, 0x0f, 0x9e, 0x16, 0x2b, 0xce, 0x33, 0x57, 0x6b, 0x31, 0x5e, 0xce, 0xcb, 0xb6, 0x40, 0x68, 0x37, 0xbf, 0x51, 0xf5}
	offCurve := slices.Clone(gy)
	offCurve[31] ^= 1
	e := []byte{1, 0, 1}
	tests := []struct {
		name string
		key  []byte
		ok   bool
	}{
		{"Ed25519", coseMap(1, 1, 3, -8, -1, 6, -2, x), true},
		{"Ed25519 with extra parameters", coseMap(1, 1, 2, []byte("kid"), 3, -8, -1, 6, -2, x), true},
		{"OKP on another curve", coseMap(1, 1, 3, -8, -1, 7, -2, x), false},
		{"OKP with no curve", coseMap(1, 1, 3, -8, -2, x), false},
		{"OKP with a short x", coseMap(1, 1, 3, -8, -1, 6, -2, x[1:]), false},
		{"OKP with an algorithm not offered", coseMap(1, 1, 3, -19, -1, 6, -2, x), false},
		{"P-256", coseMap(1, 2, 3, -7, -1, 1, -2, gx, -3, gy), true},
		{"EC2 off the curve", coseMap(1, 2, 3, -7, -1, 1, -2, gx, -3, offCurve), false},
		{"EC2 on P-384", coseMap(1, 2, 3, -7, -1, 2, -2, gx, -3, gy), false},
		{"EC2 with no curve", coseMap(1, 2, 3, -7, -2, gx, -3, gy), false},
		{"EC2 with no y", coseMap(1, 2, 3, -7, -1, 1, -2, gx), false},
		{"EC2 with ES384", coseMap(1, 2, 3, -35, -1, 1, -2, gx, -3, gy), false},
		{"EC2 with EdDSA", coseMap(1, 2, 3, -8, -1, 1, -2, gx, -3, gy), false},
		{"OKP with ES256", coseMap(1, 1, 3, -7, -1, 6, -2, x), false},
		{"RSA of 2,048 bits", coseMap(1, 3, 3, -257, -1, modulus(2048), -2, e), true},
		{"RSA of 8,192 bits", coseMap(1, 3, 3, -257, -1, modulus(8192), -2, e), true},
		{"RSA of 2,048 bits after a zero byte", coseMap(1, 3, 3, -257, -1, append([]byte{0}, modulus(2048)...), -2, e), true},
		{"RSA of 2,047 bits", coseMap(1, 3, 3, -257, -1, modulus(2047), -2, e), false},
		{"RSA of 8,193 bits", coseMap(1, 3, 3, -257, -1, modulus(8193), -2, e), false},
		{"RSA with the exponent 3", coseMap(1, 3, 3, -257, -1, modulus(2048), -2, []byte{3}), false},
		{"RSA with no exponent", coseMap(1, 3, 3, -257, -1, modulus(2048)), false},
		{"RSA with PS256", coseMap(1, 3, 3, -37, -1, modulus(2048), -2, e), false},
		{"no key type", coseMap(3, -8, -1, 6, -2, x), false},
		{"no algorithm", coseMap(1, 1, -1, 6, -2, x), false},
		{"a symmetric key", coseMap(1, 4, 3, -8, -1, 6, -2, x), false},
		{"no map", cborBytes(x), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, r := create(t, withKey(tc.key))
			p, err := webauthn.Register(c, user, r)
			if tc.ok {
				if err != nil {
					t.Fatalf("%v (%s)", err, webauthn.Check(err))
				}
				if !bytes.Equal(p.PublicKey, tc.key) {
					t.Fatal("the key kept is not the one given")
				}
				return
			}
			refused(t, err, "public key")
		})
	}
}

// signIn has a make a passkey that Register takes, then answer a sign-in,
// with edit applied to the authenticator between the two.
func signIn(t testing.TB, a *harness.Authenticator, edit func(*harness.Authenticator)) (webauthn.Ceremony, webauthn.Assertion, webauthn.Passkey) {
	t.Helper()
	p := made(t, a)
	if edit != nil {
		edit(a)
	}
	c, as := get(t, a)
	return c, as, p
}

func TestAssertRefusesAnotherPasskey(t *testing.T) {
	t.Parallel()
	c, as, p := signIn(t, harness.NewAuthenticator(origin), nil)
	other := p
	other.CredentialID = random(32)
	_, err := webauthn.Assert(c, as, other)
	refused(t, err, "passkey")

	elsewhere := p
	elsewhere.RPID = "old.example.com"
	_, err = webauthn.Assert(c, as, elsewhere)
	refused(t, err, "passkey")

	// No rawId: its id names none either.
	as.RawID, as.ID = nil, ""
	p.CredentialID = nil
	_, err = webauthn.Assert(c, as, p)
	refused(t, err, "credential id")
}

func TestAssertRefusesTheUserHandle(t *testing.T) {
	t.Parallel()
	c, as, p := signIn(t, harness.NewAuthenticator(origin), nil)
	another := p
	another.UserID = []byte("user_another")
	_, err := webauthn.Assert(c, as, another)
	refused(t, err, "user handle")

	// A passkey that is not discoverable answers with no userHandle.
	a := harness.NewAuthenticator(origin)
	a.NotDiscoverable = true
	p = made(t, a)
	c, as = get(t, a, p.CredentialID)
	_, err = webauthn.Assert(c, as, p)
	refused(t, err, "user handle")
}

func TestAssertRefusesAuthenticatorData(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		edit  func(*harness.Authenticator)
		check string
	}{
		{"too short", func(a *harness.Authenticator) {
			a.Tamper = func(u *harness.Unsigned) { u.AuthenticatorData = u.AuthenticatorData[:36] }
		}, "authenticator data"},
		{"trailing bytes", func(a *harness.Authenticator) {
			a.Tamper = func(u *harness.Unsigned) { u.AuthenticatorData = append(u.AuthenticatorData, 0) }
		}, "authenticator data"},
		{"another RP ID", func(a *harness.Authenticator) { a.RPID = "example.com" }, "RP ID hash"},
		{"no user presence", func(a *harness.Authenticator) { a.Tamper = setFlags(0, flagUP) }, "user presence"},
		{"no user verification", func(a *harness.Authenticator) { a.SkipUV = true }, "user verification"},
		{"backed up without eligibility", func(a *harness.Authenticator) { a.BackedUp = true }, "backup state without eligibility"},
		{"eligible for backup when made without", func(a *harness.Authenticator) { a.BackupEligible = true }, "backup eligibility changed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, as, p := signIn(t, harness.NewAuthenticator(origin), tc.edit)
			_, err := webauthn.Assert(c, as, p)
			refused(t, err, tc.check)
		})
	}

	// Eligible when made, and not at the sign-in.
	a := harness.NewAuthenticator(origin)
	a.BackupEligible = true
	c, as, p := signIn(t, a, func(a *harness.Authenticator) { a.BackupEligible = false })
	_, err := webauthn.Assert(c, as, p)
	refused(t, err, "backup eligibility changed")
}

func TestAssertReadsPastExtensions(t *testing.T) {
	t.Parallel()
	c, as, p := signIn(t, harness.NewAuthenticator(origin), func(a *harness.Authenticator) {
		a.Tamper = func(u *harness.Unsigned) {
			u.AuthenticatorData[32] |= flagED
			u.AuthenticatorData = append(u.AuthenticatorData, 0xa0)
		}
	})
	if _, err := webauthn.Assert(c, as, p); err != nil {
		t.Fatalf("%v (%s)", err, webauthn.Check(err))
	}
}

func TestAssertRefusesTheStoredKey(t *testing.T) {
	t.Parallel()
	c, as, p := signIn(t, harness.NewAuthenticator(origin), nil)
	wrongAlg := p
	wrongAlg.Algorithm = webauthn.ES256
	_, err := webauthn.Assert(c, as, wrongAlg)
	refused(t, err, "stored key")

	broken := p
	broken.PublicKey = p.PublicKey[:len(p.PublicKey)-1]
	_, err = webauthn.Assert(c, as, broken)
	refused(t, err, "stored key")
}

func TestAssertRefusesTheSignature(t *testing.T) {
	t.Parallel()
	for _, alg := range []int{webauthn.EdDSA, webauthn.ES256, webauthn.RS256} {
		a := harness.NewAuthenticator(origin)
		a.Algorithm = alg
		c, as, p := signIn(t, a, nil)
		for name, sig := range map[string][]byte{
			"flipped":   append(slices.Clone(as.Response.Signature[:len(as.Response.Signature)-1]), as.Response.Signature[len(as.Response.Signature)-1]^1),
			"truncated": as.Response.Signature[:len(as.Response.Signature)-1],
			"absent":    nil,
		} {
			spoilt := as
			spoilt.Response.Signature = sig
			_, err := webauthn.Assert(c, spoilt, p)
			if webauthn.Check(err) != "signature" {
				t.Fatalf("algorithm %d, a signature %s: got %v (%s)", alg, name, err, webauthn.Check(err))
			}
		}
		// Signed over other client data than the answer carries.
		other := as
		other.Response.ClientDataJSON = replace(t, as.Response.ClientDataJSON, `"type"`, `"type" `)
		_, err := webauthn.Assert(c, other, p)
		refused(t, err, "signature")
		// Checked with another key of the same algorithm.
		q := p
		q.PublicKey = anotherKey(t, alg)
		_, err = webauthn.Assert(c, as, q)
		refused(t, err, "signature")
	}
}

func TestAssertChecksTheCounter(t *testing.T) {
	t.Parallel()
	steps := harness.NewAuthenticator(origin)
	c, as, p := signIn(t, steps, nil)
	q, err := webauthn.Assert(c, as, p)
	if err != nil || q.SignCount != 2 {
		t.Fatalf("a counter that went up: got %+v, %v", q, err)
	}
	// The same answer again: the counter did not go up.
	_, err = webauthn.Assert(c, as, q)
	refused(t, err, "counter")
	if !errors.Is(err, webauthn.ErrCounterWentBack) {
		t.Fatalf("got %v, want ErrCounterWentBack", err)
	}

	steps.Counter = harness.CounterGoesBack
	c, as = get(t, steps)
	_, err = webauthn.Assert(c, as, q)
	refused(t, err, "counter")

	// A synced passkey keeps 0, and is taken every time.
	zero := harness.NewAuthenticator(origin)
	zero.Counter = harness.CounterZero
	p = made(t, zero)
	for range 2 {
		c, as = get(t, zero)
		if p, err = webauthn.Assert(c, as, p); err != nil || p.SignCount != 0 {
			t.Fatalf("a counter kept at 0: got %+v, %v", p, err)
		}
	}
	// 0 after a counter above 0 went back.
	p.SignCount = 5
	c, as = get(t, zero)
	_, err = webauthn.Assert(c, as, p)
	refused(t, err, "counter")
}

// anotherKey returns the COSE_Key of a new key of alg.
func anotherKey(t testing.TB, alg int) []byte {
	t.Helper()
	switch alg {
	case webauthn.EdDSA:
		pub, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return coseMap(1, 1, 3, alg, -1, 6, -2, []byte(pub))
	case webauthn.ES256:
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		point, err := k.PublicKey.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return coseMap(1, 2, 3, alg, -1, 1, -2, point[1:33], -3, point[33:])
	default:
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		return coseMap(1, 3, 3, alg, -1, k.N.Bytes(), -2, []byte{1, 0, 1})
	}
}

// zeroCeremonies are Ceremonies that would check nothing, each with an
// Authenticator whose answer matches it, so that only the Ceremony's own
// check refuses it.
func zeroCeremonies(t *testing.T) []struct {
	name      string
	a         *harness.Authenticator
	challenge []byte
	edit      func(*webauthn.Ceremony)
} {
	emptyHash := sha256.Sum256(nil)
	return []struct {
		name      string
		a         *harness.Authenticator
		challenge []byte
		edit      func(*webauthn.Ceremony)
	}{
		{"no challenge", harness.NewAuthenticator(origin), nil, nil},
		{"a challenge of 15 bytes", harness.NewAuthenticator(origin), random(15), nil},
		{"no origin", tamper(func(u *harness.Unsigned) {
			var cd map[string]any
			if err := json.Unmarshal(u.ClientDataJSON, &cd); err != nil {
				t.Fatal(err)
			}
			delete(cd, "origin")
			u.ClientDataJSON = mustJSON(t, cd)
		}), random(32), func(c *webauthn.Ceremony) { c.Origin = "" }},
		{
			"no RP ID", tamper(func(u *harness.Unsigned) { copy(u.AuthenticatorData, emptyHash[:]) }),
			random(32), func(c *webauthn.Ceremony) { c.RPID = "" },
		},
	}
}

func TestRegisterRefusesACeremonyThatChecksNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range zeroCeremonies(t) {
		t.Run(tc.name, func(t *testing.T) {
			c, r := createWith(t, tc.a, tc.challenge)
			if tc.edit != nil {
				tc.edit(&c)
			}
			_, err := webauthn.Register(c, user, r)
			refused(t, err, "ceremony")
		})
	}
	c, r := create(t, harness.NewAuthenticator(origin))
	_, err := webauthn.Register(c, nil, r)
	refused(t, err, "ceremony")
}

func TestAssertRefusesACeremonyThatChecksNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range zeroCeremonies(t) {
		t.Run(tc.name, func(t *testing.T) {
			edit := tc.a.Tamper
			tc.a.Tamper = nil
			p := made(t, tc.a)
			tc.a.Tamper = edit
			c, as := getWith(t, tc.a, tc.challenge)
			if tc.edit != nil {
				tc.edit(&c)
				p.RPID = c.RPID
			}
			_, err := webauthn.Assert(c, as, p)
			refused(t, err, "ceremony")
		})
	}
}

func TestRegisterRefusesFormatAndAuthDataOfAnotherType(t *testing.T) {
	t.Parallel()
	c, r := create(t, harness.NewAuthenticator(origin))
	spoilt := r
	spoilt.Response.AttestationObject = replace(t, r.Response.AttestationObject, "\x63fmt\x64none", "\x63fmt\x44none")
	_, err := webauthn.Register(c, user, spoilt)
	refused(t, err, "attestation format")

	// authData an integer: the object up to its key, then 0.
	before, _, ok := bytes.Cut(r.Response.AttestationObject, []byte("\x68authData"))
	if !ok {
		t.Fatal("no authData")
	}
	spoilt.Response.AttestationObject = slices.Concat(before, []byte("\x68authData\x00"))
	_, err = webauthn.Register(c, user, spoilt)
	refused(t, err, "authData member")
}

func TestAssertRefusesNoUserHandleForAPasskeyOfNoUser(t *testing.T) {
	t.Parallel()
	a := harness.NewAuthenticator(origin)
	a.NotDiscoverable = true
	p := made(t, a)
	c, as := get(t, a, p.CredentialID)
	p.UserID = nil
	_, err := webauthn.Assert(c, as, p)
	refused(t, err, "user handle")
}

func TestAssertReturnsTheBackupState(t *testing.T) {
	t.Parallel()
	for _, made := range []bool{false, true} {
		a := harness.NewAuthenticator(origin)
		a.BackupEligible, a.BackedUp = true, made
		c, as, p := signIn(t, a, func(a *harness.Authenticator) { a.BackedUp = !made })
		got, err := webauthn.Assert(c, as, p)
		if err != nil {
			t.Fatalf("%v (%s)", err, webauthn.Check(err))
		}
		if p.BackedUp != made || got.BackedUp != !made {
			t.Fatalf("backed up when made %v: kept %v, then got %v", made, p.BackedUp, got.BackedUp)
		}
	}
}

func TestRegisterRefusesKeysOfOtherLabels(t *testing.T) {
	t.Parallel()
	x := make([]byte, 32)
	okp := coseMap(1, 1, 3, -8, -1, 6, -2, x)
	// A fifth parameter, labelled [0], {}, 1.0 or null, after the four.
	for _, label := range [][]byte{{0x81, 0x00}, {0xa0}, {0xf9, 0x3c, 0x00}, {0xf6}, {0x41, 0x01}} {
		key := slices.Concat([]byte{0xa5}, okp[1:], label, []byte{0x00})
		c, r := create(t, withKey(key))
		_, err := webauthn.Register(c, user, r)
		refused(t, err, "authenticator data")
	}
}

func TestTheCredentialsOwnMembersAreChecked(t *testing.T) {
	t.Parallel()
	a := harness.NewAuthenticator(origin)
	c, r := create(t, a)
	p, err := webauthn.Register(c, user, r)
	if err != nil {
		t.Fatalf("%v (%s)", err, webauthn.Check(err))
	}
	ca, as := get(t, a)
	tests := []struct {
		name, id, typ, check string
	}{
		{"no type", r.ID, "", "credential type"},
		{"another type", r.ID, "Public-Key", "credential type"},
		{"no id", "", "public-key", "credential id"},
		{"another id", b64.EncodeToString(random(32)), "public-key", "credential id"},
		{"an id with padding", r.ID + "=", "public-key", "credential id"},
		{"an id that is no base64url", r.ID[1:] + "*", "public-key", "credential id"},
	}
	for _, tc := range tests {
		spoilt := r
		spoilt.ID, spoilt.Type = tc.id, tc.typ
		_, err := webauthn.Register(c, user, spoilt)
		if webauthn.Check(err) != tc.check {
			t.Fatalf("register, %s: got %v (%s), want %s", tc.name, err, webauthn.Check(err), tc.check)
		}
		spoiltAs := as
		spoiltAs.ID, spoiltAs.Type = strings.Replace(tc.id, r.ID, as.ID, 1), tc.typ
		_, err = webauthn.Assert(ca, spoiltAs, p)
		if webauthn.Check(err) != tc.check {
			t.Fatalf("assert, %s: got %v (%s), want %s", tc.name, err, webauthn.Check(err), tc.check)
		}
	}
}
