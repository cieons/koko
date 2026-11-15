package webauthn_test

// This file alone imports github.com/go-webauthn/webauthn, koko's test
// oracle (ADR 0061), so the library is in go.mod and in no binary.

import (
	"bytes"
	"encoding/binary"
	jsonv1 "encoding/json"
	"encoding/json/v2"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
	gowebauthn "github.com/go-webauthn/webauthn/webauthn"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/passkey/webauthn"
)

// The library, configured with koko's policy: the RP ID and the one origin,
// discoverable passkeys with user verification required, no attestation
// asked for, no cross-origin calls and no top origins.
func oracle(t testing.TB) *gowebauthn.WebAuthn {
	t.Helper()
	w, err := gowebauthn.New(&gowebauthn.Config{
		RPID:                  rpID,
		RPDisplayName:         "koko",
		RPOrigins:             []string{origin},
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationRequired,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// The algorithms koko offers, as the library's parameters.
var offered = []protocol.CredentialParameter{
	{Type: protocol.PublicKeyCredentialType, Algorithm: webauthncose.AlgEdDSA},
	{Type: protocol.PublicKeyCredentialType, Algorithm: webauthncose.AlgES256},
	{Type: protocol.PublicKeyCredentialType, Algorithm: webauthncose.AlgRS256},
}

// oracleUser is the User the library's ceremonies are for.
type oracleUser struct{ credentials []gowebauthn.Credential }

func (oracleUser) WebAuthnID() []byte                             { return user }
func (oracleUser) WebAuthnName() string                           { return "alice" }
func (oracleUser) WebAuthnDisplayName() string                    { return "alice" }
func (u oracleUser) WebAuthnCredentials() []gowebauthn.Credential { return u.credentials }

// An answer is a browser's answer, in parts, which a mutation changes after
// it is signed. It is encoded as the JSON both verifiers read.
type answer struct {
	id, typ                                                                            string
	rawID, clientDataJSON, attestationObject, authenticatorData, signature, userHandle []byte
}

func (a answer) json(t testing.TB) []byte {
	response := map[string]any{"clientDataJSON": b64.EncodeToString(a.clientDataJSON)}
	if a.attestationObject != nil {
		response["attestationObject"] = b64.EncodeToString(a.attestationObject)
	} else {
		response["authenticatorData"] = b64.EncodeToString(a.authenticatorData)
		response["signature"] = b64.EncodeToString(a.signature)
		if a.userHandle != nil {
			response["userHandle"] = b64.EncodeToString(a.userHandle)
		}
	}
	return mustJSON(t, map[string]any{
		"id":       a.id,
		"rawId":    b64.EncodeToString(a.rawID),
		"type":     a.typ,
		"response": response,
	})
}

// readAnswer reads the Authenticator's JSON into parts.
func readAnswer(t testing.TB, data []byte) answer {
	var v struct {
		ID       string         `json:"id"`
		Type     string         `json:"type"`
		RawID    webauthn.Bytes `json:"rawId"`
		Response struct {
			ClientDataJSON    webauthn.Bytes  `json:"clientDataJSON"`
			AttestationObject webauthn.Bytes  `json:"attestationObject"`
			AuthenticatorData webauthn.Bytes  `json:"authenticatorData"`
			Signature         webauthn.Bytes  `json:"signature"`
			UserHandle        *webauthn.Bytes `json:"userHandle"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	a := answer{
		id: v.ID, typ: v.Type, rawID: v.RawID, clientDataJSON: v.Response.ClientDataJSON, attestationObject: v.Response.AttestationObject,
		authenticatorData: v.Response.AuthenticatorData, signature: v.Response.Signature,
	}
	if v.Response.UserHandle != nil {
		a.userHandle = *v.Response.UserHandle
	}
	return a
}

// A stored passkey, as each verifier keeps it.
type stored struct {
	koko webauthn.Passkey
	lib  gowebauthn.Credential
}

// A mutation spoils one part of a ceremony, with arg saying how. before
// changes the answer before it is signed, after the answer once signed,
// and store the passkey a sign-in is checked against.
type mutation struct {
	name   string
	before func(u *harness.Unsigned, arg []byte)
	after  func(a *answer, arg []byte)
	store  func(s *stored, arg []byte)
	// knob sets one of the Authenticator's.
	knob func(a *harness.Authenticator, arg []byte)
}

// at returns arg's byte i, or 0.
func at(arg []byte, i int) byte {
	if i < len(arg) {
		return arg[i]
	}
	return 0
}

// pos returns a position in a slice of n bytes from arg's first two.
func pos(arg []byte, n int) int {
	if n == 0 {
		return 0
	}
	return int(binary.BigEndian.Uint16([]byte{at(arg, 0), at(arg, 1)})) % n
}

// xorAt changes the byte of b at pos(arg) by arg[2], or by 1 for none.
func xorAt(b []byte, arg []byte) {
	if len(b) > 0 {
		b[pos(arg, len(b))] ^= max(at(arg, 2), 1)
	}
}

// clientDataNames are the client data members a mutation sets, by arg[0].
var clientDataNames = []string{"type", "challenge", "origin", "crossOrigin", "topOrigin", "tokenBinding"}

// Mutations of either ceremony, then those of a registration (registerOnly)
// and of a sign-in (assertOnly), numbered in turn by a fuzz input's op.
var (
	bothCeremonies = []mutation{
		{name: "flags", before: func(u *harness.Unsigned, arg []byte) { u.AuthenticatorData[32] ^= at(arg, 0) }},
		{name: "counter", before: func(u *harness.Unsigned, arg []byte) {
			copy(u.AuthenticatorData[33:37], []byte{at(arg, 0), at(arg, 1), at(arg, 2), at(arg, 3)})
		}},
		{name: "RP ID hash", before: func(u *harness.Unsigned, arg []byte) { xorAt(u.AuthenticatorData[:32], arg) }},
		{name: "authenticator data appended", before: func(u *harness.Unsigned, arg []byte) {
			u.AuthenticatorData = append(u.AuthenticatorData, arg...)
		}},
		{name: "authenticator data cut", before: func(u *harness.Unsigned, arg []byte) {
			u.AuthenticatorData = u.AuthenticatorData[:len(u.AuthenticatorData)-1-pos(arg, len(u.AuthenticatorData))]
		}},
		{name: "client data member", before: func(u *harness.Unsigned, arg []byte) {
			var cd map[string]any
			if err := json.Unmarshal(u.ClientDataJSON, &cd); err != nil {
				panic(err)
			}
			name := clientDataNames[int(at(arg, 0))%len(clientDataNames)]
			value := strings.ToValidUTF8(string(arg[min(1, len(arg)):]), "?")
			switch {
			case len(arg) <= 1:
				delete(cd, name)
			case name == "crossOrigin":
				cd[name] = arg[1]&1 == 1
			case name == "tokenBinding":
				cd[name] = map[string]any{"status": value}
			default:
				cd[name] = value
			}
			out, err := json.Marshal(cd, json.Deterministic(true))
			if err != nil {
				panic(err)
			}
			u.ClientDataJSON = out
		}},
		{name: "client data byte", before: func(u *harness.Unsigned, arg []byte) { xorAt(u.ClientDataJSON, arg) }},
		{name: "raw id", after: func(a *answer, arg []byte) {
			// The answer's id stays rawId's, so that the change is to the
			// credential id alone; "outer id" spoils the id.
			xorAt(a.rawID, arg)
			a.id = b64.EncodeToString(a.rawID)
		}},
	}
	registerOnly = []mutation{
		{name: "key byte", before: func(u *harness.Unsigned, arg []byte) {
			n := int(binary.BigEndian.Uint16(u.AuthenticatorData[53:55]))
			xorAt(u.AuthenticatorData[55+n:], arg)
		}},
		{name: "credential id length", before: func(u *harness.Unsigned, arg []byte) {
			xorAt(u.AuthenticatorData[53:55], arg)
		}},
		{name: "attestation object byte", after: func(a *answer, arg []byte) { xorAt(a.attestationObject, arg) }},
		{name: "attestation object appended", after: func(a *answer, arg []byte) {
			a.attestationObject = append(a.attestationObject, arg...)
		}},
		{name: "none statement", knob: func(a *harness.Authenticator, arg []byte) {
			a.Statement = &harness.Statement{Format: "none", CBOR: arg}
		}},
		extensions, outerType, outerID,
	}
	assertOnly = []mutation{
		{name: "signature byte", after: func(a *answer, arg []byte) { xorAt(a.signature, arg) }},
		{name: "signature cut", after: func(a *answer, arg []byte) {
			a.signature = a.signature[:len(a.signature)-1-pos(arg, len(a.signature))]
		}},
		{name: "user handle", after: func(a *answer, arg []byte) {
			if len(arg) == 0 {
				a.userHandle = nil
			} else {
				a.userHandle = arg
			}
		}},
		{name: "stored key byte", store: func(s *stored, arg []byte) {
			xorAt(s.koko.PublicKey, arg)
			s.lib.PublicKey = s.koko.PublicKey
		}},
		{name: "stored counter", store: func(s *stored, arg []byte) {
			n := binary.BigEndian.Uint32([]byte{at(arg, 0), at(arg, 1), at(arg, 2), at(arg, 3)})
			s.koko.SignCount, s.lib.Authenticator.SignCount = n, n
		}},
		{name: "stored backup eligibility", store: func(s *stored, _ []byte) {
			s.koko.BackupEligible = !s.koko.BackupEligible
			s.lib.Flags.BackupEligible = s.koko.BackupEligible
		}},
		extensions, outerType, outerID,
	}
)

// extensions sets the ED flag and appends arg as the extension outputs. It
// is last in each ceremony's list, so the inputs kept in testdata/fuzz keep
// their mutation.
var extensions = mutation{name: "extensions", before: func(u *harness.Unsigned, arg []byte) {
	u.AuthenticatorData[32] |= flagED
	u.AuthenticatorData = append(u.AuthenticatorData, arg...)
}}

// outerType and outerID spoil the answer's own type and id, which no
// signature covers. They follow extensions in each list.
var (
	outerType = mutation{name: "outer type", after: func(a *answer, arg []byte) {
		a.typ = strings.ToValidUTF8(string(arg), "?")
	}}
	outerID = mutation{name: "outer id", after: func(a *answer, arg []byte) {
		switch {
		case len(arg) == 0:
			a.id = ""
		case arg[0] == '=':
			a.id += strings.ToValidUTF8(string(arg), "?")
		default:
			id := []byte(a.id)
			xorAt(id, arg)
			a.id = strings.ToValidUTF8(string(id), "?")
		}
	}}
)

// noMutation is the op of a ceremony left as it is.
const noMutation = 255

func mutations(ceremony uint8) []mutation {
	if ceremony%2 == 0 {
		return append(bothCeremonies[:len(bothCeremonies):len(bothCeremonies)], registerOnly...)
	}
	return append(bothCeremonies[:len(bothCeremonies):len(bothCeremonies)], assertOnly...)
}

// The ceremonies a fuzz input names.
const (
	ceremonyRegister = 0
	ceremonyAssert   = 1
)

var algorithms = []int{webauthn.EdDSA, webauthn.ES256, webauthn.RS256}

// FuzzAgainstGoWebAuthn makes a `none` ceremony with harness.Authenticator,
// spoils it with one mutation, and has koko's verifier and go-webauthn judge
// it. koko must never accept what the library refuses; where it refuses
// what the library accepts, one of the rules koko adds must explain it
// (kokoStricter).
func FuzzAgainstGoWebAuthn(f *testing.F) {
	seeds := map[string][][]byte{
		"flags":                       {{flagUP}, {flagUV}, {flagBE}, {flagBS}, {flagBE | flagBS}, {flagAT}, {flagED}, {1 << 1}, {1 << 5}},
		"counter":                     {{0, 0, 0, 0}, {0, 0, 0, 1}, {0, 0, 0, 2}, {0xff, 0xff, 0xff, 0xff}},
		"RP ID hash":                  {{0, 0, 1}, {0, 31, 0x80}},
		"authenticator data appended": {{0}, {0xa0}, {0xf6}},
		"authenticator data cut":      {{0, 0}, {0, 1}, {0, 40}},
		"client data member": {
			{0, 'w', 'e', 'b', 'a', 'u', 't', 'h', 'n', '.', 'g', 'e', 't'},
			{0, 'w', 'e', 'b', 'a', 'u', 't', 'h', 'n', '.', 'c', 'r', 'e', 'a', 't', 'e'},
			{0},
			{1},
			{1, 'A', 'A', 'A', 'A'},
			{2},
			append([]byte{2}, "HTTPS://KOKO.EXAMPLE.COM"...),
			append([]byte{2}, "https://koko.example.com:443"...),
			append([]byte{2}, "https://koko.example.com/"...),
			append([]byte{2}, "https://alice@koko.example.com"...),
			append([]byte{2}, "http://koko.example.com"...),
			append([]byte{2}, "https://koko.example.com.evil.example"...),
			{3, 1},
			{3, 0},
			append([]byte{4}, harness.WebURL...),
			{4, 0},
			append([]byte{5}, "present"...), append([]byte{5}, "bogus"...),
			{5, 0},
		},
		"client data byte": {{0, 2, 0x20}, {0, 10, 1}, {0, 0, 0x7b}, {0xff, 0xff, 1}},
		"raw id":           {{0, 0, 1}, {0, 31, 0x80}},
		// For an RS256 key, 11 is the modulus's first byte and 271 the
		// exponent's last.
		"key byte":                    {{0, 0, 1}, {0, 1, 1}, {0, 2, 1}, {0, 3, 2}, {0, 4, 1}, {0, 6, 2}, {0, 7, 1}, {0, 9, 0x40}, {0, 11, 0x80}, {0x01, 0x0f, 2}, {0xff, 0xff, 2}},
		"credential id length":        {{0, 0, 1}, {0, 1, 1}, {0, 1, 0x20}},
		"attestation object byte":     {{0, 0, 1}, {0, 1, 1}, {0, 5, 0x40}, {0, 12, 1}, {0, 20, 1}, {0, 24, 1}},
		"attestation object appended": {{0}, {0xa0}},
		"none statement":              {{0xa1, 0x63, 's', 'i', 'g', 0x40}, {0xa0}, {0x80}, {0xf6}, {0x40}},
		"signature byte":              {{0, 0, 1}, {0, 5, 0x80}, {0xff, 0xff, 1}},
		"signature cut":               {{0, 0}, {0, 1}},
		"user handle":                 {{}, []byte("user_another")},
		"stored key byte":             {{0, 0, 1}, {0, 2, 1}, {0, 4, 1}, {0, 9, 0x40}, {0, 11, 0x80}, {0x01, 0x0f, 2}, {0xff, 0xff, 1}},
		"stored counter":              {{0, 0, 0, 0}, {0, 0, 0, 1}, {0, 0, 0, 2}, {0xff, 0xff, 0xff, 0xff}},
		"stored backup eligibility":   {{}},
		"extensions": {
			{0xa0},
			append(append([]byte{0xa1, 0x6b}, "credProtect"...), 0x02),
			{0xa1, 0x01, 0x02},
			{0x80},
			{0xa0, 0x00},
			// 5 containers nested, and 6: the limit of both verifiers.
			{0xa1, 0x61, 'a', 0x81, 0x81, 0x81, 0x81, 0x00},
			{0xa1, 0x61, 'a', 0x81, 0x81, 0x81, 0x81, 0x81, 0x00},
			// Map keys other than integers and text, nested (#230's review).
			{0xa1, 0x61, 'a', 0xa1, 0x81, 0x00, 0x00},
			{0xa1, 0x61, 'a', 0xa1, 0xa0, 0x00},
			{0xa1, 0x61, 'a', 0xa1, 0xf6, 0x00},
			{0xa1, 0x61, 'a', 0xa1, 0xf9, 0x3c, 0x00, 0x00},
			{0xa1, 0x61, 'a', 0xa1, 0x41, 0x01, 0x00},
			// 1,025 items.
			append([]byte{0xa1, 0x61, 'a', 0x99, 0x04, 0x00}, make([]byte, 1024)...),
		},
		"outer type": {{}, []byte("public-key "), []byte("Public-Key"), []byte("private-key")},
		"outer id":   {{}, {0, 0, 1}, {0, 5, 0x20}, {'='}, {'=', '='}},
	}
	for ceremony := range uint8(2) {
		for alg := range uint8(len(algorithms)) {
			f.Add(ceremony, alg, uint8(noMutation), []byte{})
			for op, m := range mutations(ceremony) {
				for _, arg := range seeds[m.name] {
					f.Add(ceremony, alg, uint8(op), arg)
				}
			}
		}
	}
	lib := oracle(f)
	f.Fuzz(func(t *testing.T, ceremony, alg, op uint8, arg []byte) {
		ms := mutations(ceremony)
		var m mutation
		if int(op) < len(ms) {
			m = ms[op]
		} else if op != noMutation {
			t.Skip("no such mutation")
		}
		a := harness.NewAuthenticator(origin)
		a.Algorithm = algorithms[int(alg)%len(algorithms)]
		if ceremony%2 == ceremonyRegister {
			judgeRegistration(t, lib, a, m, arg)
		} else {
			judgeAssertion(t, lib, a, m, arg)
		}
	})
}

// spoil sets m's changes on a for its next answer.
func spoil(a *harness.Authenticator, m mutation, arg []byte) {
	if m.knob != nil {
		m.knob(a, arg)
	}
	if m.before != nil {
		a.Tamper = func(u *harness.Unsigned) { m.before(u, arg) }
	}
}

func judgeRegistration(t *testing.T, lib *gowebauthn.WebAuthn, a *harness.Authenticator, m mutation, arg []byte) {
	spoil(a, m, arg)
	challenge := random(32)
	out, err := a.Create(creationOptions(t, challenge))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	ans := readAnswer(t, out)
	if m.after != nil {
		m.after(&ans, arg)
	}
	body := ans.json(t)

	var r webauthn.Registration
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("koko cannot read the registration: %v", err)
	}
	p, kokoErr := webauthn.Register(webauthn.Ceremony{RPID: rpID, Origin: origin, Challenge: challenge}, user, r)

	cred, libErr := judgeWithLibrary(lib, body, challenge)
	if kokoErr == nil && libErr != nil {
		// koko reads past a statement of a format other than `none`, which
		// the library verifies, so it judges such an answer as `none`.
		if none, ok := asNone(t, ans); ok {
			cred, libErr = judgeWithLibrary(lib, none.json(t), challenge)
		}
	}

	t.Logf("register, %s: koko %q, library %v", m.name, webauthn.Check(kokoErr), libErr)
	switch {
	case kokoErr == nil && libErr != nil:
		t.Fatalf("%s: koko took a registration the library refused: %v", m.name, libErr)
	case kokoErr != nil && libErr == nil:
		if rule := kokoStricter(m, arg, ans.clientDataJSON, ans.attestationObject, cred.PublicKey); rule == "" {
			t.Fatalf("%s: koko refused (%s) a registration the library took, and no rule of koko's explains it", m.name, webauthn.Check(kokoErr))
		}
	case kokoErr == nil:
		if !bytes.Equal(p.CredentialID, cred.ID) || p.SignCount != cred.Authenticator.SignCount ||
			p.BackupEligible != cred.Flags.BackupEligible || p.BackedUp != cred.Flags.BackupState {
			t.Fatalf("%s: koko kept %+v, the library %+v", m.name, p, cred)
		}
	}
}

// judgeWithLibrary has the library judge a registration. It checks a key
// only at its first use, so this checks it too, with the library's own
// parser.
func judgeWithLibrary(lib *gowebauthn.WebAuthn, body, challenge []byte) (*gowebauthn.Credential, error) {
	cred, err := registerWithLibrary(lib, body, challenge)
	if err != nil {
		return nil, err
	}
	if _, err := webauthncose.ParsePublicKey(cred.PublicKey); err != nil {
		return nil, err
	}
	return cred, nil
}

// asNone returns a with an attestation object of a format other than
// `none` made `none`, as the library reads it, and false when it reads no
// such object.
func asNone(t testing.TB, a answer) (answer, bool) {
	var obj struct {
		Format   string `cbor:"fmt"`
		AuthData []byte `cbor:"authData"`
	}
	if webauthncbor.Unmarshal(a.attestationObject, &obj) != nil || obj.Format == "none" {
		return answer{}, false
	}
	none, err := webauthncbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": obj.AuthData})
	if err != nil {
		t.Fatal(err)
	}
	a.attestationObject = none
	return a, true
}

func registerWithLibrary(lib *gowebauthn.WebAuthn, body, challenge []byte) (*gowebauthn.Credential, error) {
	parsed, err := protocol.ParseCredentialCreationResponseBytes(body)
	if err != nil {
		return nil, err
	}
	return lib.CreateCredential(oracleUser{}, gowebauthn.SessionData{
		Challenge:        b64.EncodeToString(challenge),
		RelyingPartyID:   rpID,
		UserID:           user,
		UserVerification: protocol.VerificationRequired,
		CredParams:       offered,
	}, parsed)
}

func judgeAssertion(t *testing.T, lib *gowebauthn.WebAuthn, a *harness.Authenticator, m mutation, arg []byte) {
	// A passkey both verifiers took.
	challenge := random(32)
	out, err := a.Create(creationOptions(t, challenge))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	body := readAnswer(t, out).json(t)
	var r webauthn.Registration
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatal(err)
	}
	p, err := webauthn.Register(webauthn.Ceremony{RPID: rpID, Origin: origin, Challenge: challenge}, user, r)
	if err != nil {
		t.Fatalf("koko refused the passkey: %v", err)
	}
	cred, err := registerWithLibrary(lib, body, challenge)
	if err != nil {
		t.Fatalf("the library refused the passkey: %v", err)
	}
	s := stored{koko: p, lib: *cred}
	if m.store != nil {
		m.store(&s, arg)
	}

	spoil(a, m, arg)
	challenge = random(32)
	out, err = a.Get(requestOptions(t, challenge))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	ans := readAnswer(t, out)
	if m.after != nil {
		m.after(&ans, arg)
	}
	body = ans.json(t)

	var as webauthn.Assertion
	if err := json.Unmarshal(body, &as); err != nil {
		t.Fatalf("koko cannot read the assertion: %v", err)
	}
	got, kokoErr := webauthn.Assert(webauthn.Ceremony{RPID: rpID, Origin: origin, Challenge: challenge}, as, s.koko)

	var libCred *gowebauthn.Credential
	parsed, libErr := protocol.ParseCredentialRequestResponseBytes(body)
	if libErr == nil {
		_, libCred, libErr = lib.ValidatePasskeyLogin(func(rawID, userHandle []byte) (gowebauthn.User, error) {
			// As koko's caller finds the passkey, and Assert its User.
			if !bytes.Equal(rawID, s.lib.ID) || !bytes.Equal(userHandle, user) {
				return nil, errors.New("no such passkey")
			}
			return oracleUser{credentials: []gowebauthn.Credential{s.lib}}, nil
		}, gowebauthn.SessionData{
			Challenge:        b64.EncodeToString(challenge),
			RelyingPartyID:   rpID,
			UserVerification: protocol.VerificationRequired,
		}, parsed)
	}
	// koko refuses where the library flags a clone.
	cloned := libErr == nil && libCred.Authenticator.CloneWarning

	t.Logf("assert, %s: koko %q, library %v, clone %v", m.name, webauthn.Check(kokoErr), libErr, cloned)
	switch {
	case errors.Is(kokoErr, webauthn.ErrCounterWentBack):
		if !cloned {
			t.Fatalf("%s: koko refused the counter where the library flagged no clone (%v)", m.name, libErr)
		}
	case kokoErr == nil && (libErr != nil || cloned):
		t.Fatalf("%s: koko took a sign-in the library refused: %v, clone %v", m.name, libErr, cloned)
	case kokoErr != nil && libErr == nil:
		if rule := kokoStricter(m, arg, ans.clientDataJSON, nil, s.lib.PublicKey); rule == "" {
			t.Fatalf("%s: koko refused (%s) a sign-in the library took, and no rule of koko's explains it", m.name, webauthn.Check(kokoErr))
		}
	case kokoErr == nil:
		if got.SignCount != libCred.Authenticator.SignCount || got.BackedUp != libCred.Flags.BackupState {
			t.Fatalf("%s: koko kept %+v, the library %+v", m.name, got, libCred)
		}
	}
}

// kokoStricter names the rule koko adds to the library's that refuses an
// answer with this client data and key, or "" for none. Each is computed
// here from the input, not by koko's code:
//
//   - the origin is exactly the Web URL, where the library folds case and
//     drops a default port, a path and userinfo;
//   - the client data is JSON as encoding/json/v2 reads it, whose names
//     match exactly and which refuses a duplicate name and invalid UTF-8,
//     where the library's encoding/json takes all three;
//   - the attestation object's keys match exactly, where the library's CBOR
//     decoder folds case;
//   - a COSE_Key's kty, alg and crv are CBOR integers, where the library's
//     decoder takes a simple value for an integer too;
//   - the extension outputs are a map keyed by text strings, where the
//     library's decoder takes null and undefined for the map or a key;
//   - CBOR map keys are integers or text strings, where the library's
//     decoder takes byte strings, null, undefined, floats and booleans
//     inside a value it decodes as any, and one CBOR value holds at most
//     1,024 items;
//   - an RSA key's modulus is 2,048 to 8,192 bits and its exponent 65,537.
func kokoStricter(m mutation, arg, clientDataJSON, attestationObject, key []byte) string {
	// The client data as the library reads it.
	var cd struct {
		Origin string `json:"origin"`
	}
	if jsonv1.Unmarshal(clientDataJSON, &cd) == nil && cd.Origin != origin {
		return "origin"
	}
	var members map[string]any
	if json.Unmarshal(clientDataJSON, &members) != nil {
		return "client data JSON"
	}
	for name := range members {
		for _, known := range clientDataNames {
			if name != known && strings.EqualFold(name, known) {
				return "client data JSON"
			}
		}
	}
	var obj map[string]webauthncbor.RawMessage
	if attestationObject != nil && webauthncbor.Unmarshal(attestationObject, &obj) == nil {
		for name := range obj {
			for _, known := range []string{"fmt", "attStmt", "authData"} {
				if name != known && strings.EqualFold(name, known) {
					return "attestation object keys"
				}
			}
		}
	}
	if m.name == extensions.name {
		var outputs map[any]any
		if _, err := webauthncbor.UnmarshalFirst(arg, &outputs); err == nil {
			if outputs == nil {
				return "extension outputs"
			}
			for name := range outputs {
				if _, ok := name.(string); !ok {
					return "extension output names"
				}
			}
		}
	}
	for _, data := range [][]byte{attestationObject, key, extensionOutputs(m, arg)} {
		if rule := cborStricter(data); rule != "" {
			return rule
		}
	}
	var params map[int64]webauthncbor.RawMessage
	if webauthncbor.Unmarshal(key, &params) == nil {
		for _, label := range []int64{1, 3, -1} {
			// Major type 0 or 1: an unsigned or a negative integer. The
			// RSA modulus under -1 is a byte string, and no simple value.
			if v := params[label]; len(v) > 0 && v[0]>>5 == 7 {
				return "COSE_Key integers"
			}
		}
	}
	if k, err := webauthncose.ParsePublicKey(key); err == nil {
		if rk, ok := k.(webauthncose.RSAPublicKeyData); ok {
			bits := new(big.Int).SetBytes(rk.Modulus).BitLen()
			e := new(big.Int).SetBytes(rk.Exponent)
			if bits < 2048 || bits > 8192 || e.Cmp(big.NewInt(65537)) != 0 {
				return "RSA key size or exponent"
			}
		}
	}
	return ""
}

// extensionOutputs returns the extension outputs a mutation appends, or nil.
func extensionOutputs(m mutation, arg []byte) []byte {
	if m.name == extensions.name {
		return arg
	}
	return nil
}

// cborStricter names the rule of koko's CBOR reader that refuses data the
// library's decoder takes, or "" for none: a map key that is no integer or
// text string, or more than 1,024 items.
func cborStricter(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	opts := cbor.DecOptions{
		DupMapKey:       cbor.DupMapKeyEnforcedAPF,
		MaxNestedLevels: 5,
		IndefLength:     cbor.IndefLengthForbidden,
		TagsMd:          cbor.TagsForbidden,
	}
	library, err := opts.DecMode()
	if err != nil {
		panic(err)
	}
	var v any
	if _, err := library.UnmarshalFirst(data, &v); err != nil {
		return ""
	}
	// The library's decoder makes a byte string key a string, so it is
	// found by refusing one.
	opts.MapKeyByteString = cbor.MapKeyByteStringForbidden
	noBytes, err := opts.DecMode()
	if err != nil {
		panic(err)
	}
	var w any
	if _, err := noBytes.UnmarshalFirst(data, &w); err != nil {
		return "CBOR map keys"
	}
	items, oddKey := 0, false
	var walk func(any)
	walk = func(v any) {
		items++
		switch v := v.(type) {
		case map[any]any:
			for k, e := range v {
				items++
				switch k.(type) {
				case string, int64, uint64:
				default:
					oddKey = true
				}
				walk(e)
			}
		case []any:
			for _, e := range v {
				walk(e)
			}
		}
	}
	walk(v)
	switch {
	case oddKey:
		return "CBOR map keys"
	case items > 1024:
		return "CBOR items"
	}
	return ""
}
