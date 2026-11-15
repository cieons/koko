package webauthn_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"testing"

	"github.com/cieons/koko/internal/server/passkey/webauthn"
)

// The W3C vectors' relying party.
const (
	vectorRPID   = "example.org"
	vectorOrigin = "https://example.org"
)

// A w3cVector is one of testdata/w3c-level3.json: a registration and the
// assertion made with it, every value in hex.
type w3cVector struct {
	Name              string `json:"name"`
	Algorithm         string `json:"algorithm"`
	Challenge         string `json:"challenge"`
	ClientDataJSON    string `json:"clientDataJSON"`
	AttestationObject string `json:"attestationObject"`
	CredentialID      string `json:"credentialId"`
	Assertion         struct {
		Challenge           string `json:"challenge"`
		ClientDataJSON      string `json:"clientDataJSON"`
		AuthenticatorData   string `json:"authenticatorData"`
		Signature           string `json:"signature"`
		CredentialPublicKey string `json:"credentialPublicKey"`
	} `json:"assertion"`
}

func unhex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func readJSON(t testing.TB, file string, v any) {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
}

// otherwise returns c for another RP ID, another origin and another
// challenge, each with the check that refuses it.
func otherwise(c webauthn.Ceremony) map[string]webauthn.Ceremony {
	rp, o, ch := c, c, c
	rp.RPID = "example.com"
	o.Origin = "https://example.com"
	ch.Challenge = append(bytes.Clone(c.Challenge), 0)
	return map[string]webauthn.Ceremony{"RP ID hash": rp, "origin": o, "challenge": ch}
}

func TestTheW3CVectors(t *testing.T) {
	t.Parallel()
	var vectors []w3cVector
	readJSON(t, "testdata/w3c-level3.json", &vectors)
	if len(vectors) != 14 {
		t.Fatalf("read %d vectors, want 14", len(vectors))
	}
	// What koko refuses of them, and at which check.
	refusedAt := map[string]string{
		"NoneES256CrossOrigin": "cross-origin",
		"NoneES256TopOrigin":   "cross-origin",
		"PackedES384WithMDS":   "public key",
		"PackedES512WithMDS":   "public key",
	}
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			c := webauthn.Ceremony{RPID: vectorRPID, Origin: vectorOrigin, Challenge: unhex(t, v.Challenge)}
			var r webauthn.Registration
			r.RawID = unhex(t, v.CredentialID)
			r.ID, r.Type = b64.EncodeToString(r.RawID), "public-key"
			r.Response.ClientDataJSON = unhex(t, v.ClientDataJSON)
			r.Response.AttestationObject = unhex(t, v.AttestationObject)

			ca := webauthn.Ceremony{RPID: vectorRPID, Origin: vectorOrigin, Challenge: unhex(t, v.Assertion.Challenge)}
			var as webauthn.Assertion
			as.ID, as.Type, as.RawID = r.ID, r.Type, r.RawID
			as.Response.ClientDataJSON = unhex(t, v.Assertion.ClientDataJSON)
			as.Response.AuthenticatorData = unhex(t, v.Assertion.AuthenticatorData)
			as.Response.Signature = unhex(t, v.Assertion.Signature)
			// The vectors carry no userHandle, which no signature covers.
			as.Response.UserHandle = user

			if check, ok := refusedAt[v.Name]; ok {
				_, err := webauthn.RegisterWithoutUV(c, user, r)
				refused(t, err, check)
				p := webauthn.Passkey{
					RPID: vectorRPID, UserID: user, CredentialID: r.RawID, Algorithm: webauthn.ES256,
					PublicKey:      unhex(t, v.Assertion.CredentialPublicKey),
					BackupEligible: as.Response.AuthenticatorData[32]&flagBE != 0,
				}
				if check == "public key" {
					check = "stored key"
				}
				_, err = webauthn.AssertWithoutUV(ca, as, p)
				refused(t, err, check)
				return
			}

			p, err := webauthn.RegisterWithoutUV(c, user, r)
			if err != nil {
				t.Fatalf("register: %v (%s)", err, webauthn.Check(err))
			}
			if !bytes.Equal(p.CredentialID, r.RawID) || !bytes.Equal(p.PublicKey, unhex(t, v.Assertion.CredentialPublicKey)) {
				t.Fatal("the passkey is not the vector's")
			}
			// Unless the vector was made with user verification, Register
			// refuses it for that alone.
			if _, err := webauthn.Register(c, user, r); madeWithUV(t, r.Response.AttestationObject) {
				if err != nil {
					t.Fatalf("register: %v (%s)", err, webauthn.Check(err))
				}
			} else {
				refused(t, err, "user verification")
			}
			for check, other := range otherwise(c) {
				_, err := webauthn.RegisterWithoutUV(other, user, r)
				refused(t, err, check)
			}

			if _, err := webauthn.AssertWithoutUV(ca, as, p); err != nil {
				t.Fatalf("assert: %v (%s)", err, webauthn.Check(err))
			}
			if _, err := webauthn.Assert(ca, as, p); as.Response.AuthenticatorData[32]&flagUV != 0 {
				if err != nil {
					t.Fatalf("assert: %v (%s)", err, webauthn.Check(err))
				}
			} else {
				refused(t, err, "user verification")
			}
			for check, other := range otherwise(ca) {
				// A passkey recorded for the other RP ID, so that the
				// answer's RP ID hash is what refuses it.
				q := p
				q.RPID = other.RPID
				_, err := webauthn.AssertWithoutUV(other, as, q)
				refused(t, err, check)
			}
		})
	}
}

// madeWithUV reports whether the authenticator data in an attestation
// object has the UV flag set. It finds it after the authData key, as a
// byte string of one or two bytes' length.
func madeWithUV(t testing.TB, obj []byte) bool {
	t.Helper()
	_, after, ok := bytes.Cut(obj, []byte("\x68authData"))
	if !ok || len(after) < 3 {
		t.Fatal("no authData")
	}
	switch after[0] {
	case 0x58:
		after = after[2:]
	case 0x59:
		after = after[3:]
	default:
		t.Fatalf("authData's head %x", after[0])
	}
	return after[32]&flagUV != 0
}

// A recording is one of testdata/go-webauthn-registrations.json: a real
// authenticator's answer to a creation, in base64url.
type recording struct {
	Name              string         `json:"name"`
	RPID              string         `json:"rpId"`
	Origin            string         `json:"origin"`
	Challenge         webauthn.Bytes `json:"challenge"`
	RawID             webauthn.Bytes `json:"rawId"`
	ClientDataJSON    webauthn.Bytes `json:"clientDataJSON"`
	AttestationObject webauthn.Bytes `json:"attestationObject"`
}

func TestRealAuthenticatorsRegistrations(t *testing.T) {
	t.Parallel()
	var recordings []recording
	readJSON(t, "testdata/go-webauthn-registrations.json", &recordings)
	if len(recordings) != 4 {
		t.Fatalf("read %d registrations, want 4", len(recordings))
	}
	for _, rec := range recordings {
		t.Run(rec.Name, func(t *testing.T) {
			c := webauthn.Ceremony{RPID: rec.RPID, Origin: rec.Origin, Challenge: rec.Challenge}
			var r webauthn.Registration
			r.ID, r.Type, r.RawID = b64.EncodeToString(rec.RawID), "public-key", rec.RawID
			r.Response.ClientDataJSON = rec.ClientDataJSON
			r.Response.AttestationObject = rec.AttestationObject
			p, err := webauthn.RegisterWithoutUV(c, user, r)
			if err != nil {
				t.Fatalf("%v (%s)", err, webauthn.Check(err))
			}
			if p.Algorithm != webauthn.ES256 || !bytes.Equal(p.CredentialID, rec.RawID) {
				t.Fatalf("got %+v", p)
			}
			for check, other := range otherwise(c) {
				_, err := webauthn.RegisterWithoutUV(other, user, r)
				refused(t, err, check)
			}
		})
	}
}
