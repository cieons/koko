package harness

import (
	"cmp"
	"crypto"
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
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
)

// The COSE algorithms an Authenticator makes passkeys of.
const (
	coseEdDSA = -8
	coseES256 = -7
	coseRS256 = -257
)

// Counter is how an Authenticator's signature counter moves.
type Counter int

const (
	// CounterSteps adds one for every signature, the passkey's creation
	// included.
	CounterSteps Counter = iota
	// CounterZero keeps 0, as a synced passkey does.
	CounterZero
	// CounterGoesBack gives each signature a counter one below the last
	// one's, never below 0.
	CounterGoesBack
)

// A Statement is an attestation an Authenticator answers a creation with in
// place of `none`.
type Statement struct {
	// Format is the attestation format, such as packed.
	Format string
	// CBOR is the attestation statement, encoded.
	CBOR []byte
}

// Unsigned is an answer's client data and authenticator data before the
// Authenticator signs them, which an Authenticator's Tamper may change.
type Unsigned struct {
	ClientDataJSON    []byte
	AuthenticatorData []byte
}

// An Authenticator is a browser and a passkey authenticator together, in
// software: it answers a koko server's options for
// navigator.credentials.create() and .get(), given as the JSON forms of
// WebAuthn Level 3 (PublicKeyCredentialCreationOptionsJSON and
// PublicKeyCredentialRequestOptionsJSON), with what the browser's
// PublicKeyCredential.toJSON() gives. It holds one key of each of the three
// algorithms, made at first use, which every passkey of that algorithm
// shares; its RS256 key is the one every Authenticator in the process
// shares, since making one takes long. Its fields are the knobs a test
// turns; the zero value of each is a well-behaved browser and
// authenticator.
type Authenticator struct {
	// Origin is the page's origin, which the browser puts in the client
	// data: the Web URL, or another origin to answer from the wrong one.
	Origin string
	// Algorithm, when set, is the COSE algorithm of the passkeys it makes
	// (-8, -7 or -257); otherwise the first offered of the three.
	Algorithm int
	// SkipUV leaves the UV flag unset, as an authenticator that did not
	// check who is there.
	SkipUV bool
	// NotDiscoverable makes passkeys a get() with no allowCredentials does
	// not find, and whose answers carry no userHandle.
	NotDiscoverable bool
	// Counter is how its signature counter moves.
	Counter Counter
	// BackupEligible and BackedUp are the BE and BS flags it sets.
	BackupEligible bool
	BackedUp       bool
	// Statement, when set, is the attestation it answers a creation with,
	// kept whatever koko asked for.
	Statement *Statement
	// RPID, when set, is the RP ID it hashes in place of the one asked
	// for.
	RPID string
	// Type, when set, is the client data's type in place of the
	// ceremony's.
	Type string
	// CrossOrigin answers as from a cross-origin iframe: crossOrigin true,
	// and a topOrigin.
	CrossOrigin bool
	// Tamper, when set, may change each answer before it is signed.
	Tamper func(*Unsigned)

	keys        map[int]crypto.Signer
	credentials []*heldPasskey
}

// A heldPasskey is one passkey an Authenticator made.
type heldPasskey struct {
	id      []byte
	rpID    string
	user    []byte
	alg     int
	counter uint32
	// discoverable is whether a get() with no allowCredentials finds it.
	discoverable bool
}

// NewAuthenticator returns an Authenticator answering from origin.
func NewAuthenticator(origin string) *Authenticator {
	return &Authenticator{Origin: origin}
}

// creationOptions is what an Authenticator reads of
// PublicKeyCredentialCreationOptionsJSON.
type creationOptions struct {
	RP struct {
		ID string `json:"id"`
	} `json:"rp"`
	User struct {
		ID string `json:"id"`
	} `json:"user"`
	Challenge        string `json:"challenge"`
	PubKeyCredParams []struct {
		Type string `json:"type"`
		Alg  int    `json:"alg"`
	} `json:"pubKeyCredParams"`
	ExcludeCredentials []struct {
		ID string `json:"id"`
	} `json:"excludeCredentials"`
}

// requestOptions is what an Authenticator reads of
// PublicKeyCredentialRequestOptionsJSON.
type requestOptions struct {
	Challenge        string `json:"challenge"`
	RPID             string `json:"rpId"`
	AllowCredentials []struct {
		ID string `json:"id"`
	} `json:"allowCredentials"`
}

var base64url = base64.RawURLEncoding

// Create answers creation options with a new passkey, as
// navigator.credentials.create() and toJSON() do. Like a browser, it
// refuses options whose RP ID does not match its origin, that offer none of
// its algorithms, or that exclude a passkey it holds.
func (a *Authenticator) Create(options []byte) ([]byte, error) {
	var o creationOptions
	if err := json.Unmarshal(options, &o); err != nil {
		return nil, fmt.Errorf("read the creation options: %w", err)
	}
	if err := a.checkRPID(o.RP.ID); err != nil {
		return nil, err
	}
	for _, c := range o.ExcludeCredentials {
		if a.find(c.ID, o.RP.ID) != nil {
			return nil, errors.New("InvalidStateError: a passkey it holds is excluded")
		}
	}
	alg := 0
	for _, p := range o.PubKeyCredParams {
		if p.Type == "public-key" && (p.Alg == a.Algorithm || a.Algorithm == 0 && slices.Contains([]int{coseEdDSA, coseES256, coseRS256}, p.Alg)) {
			alg = p.Alg
			break
		}
	}
	if alg == 0 {
		return nil, errors.New("NotSupportedError: no algorithm it makes is offered")
	}
	user, err := base64url.DecodeString(o.User.ID)
	if err != nil || len(user) == 0 {
		return nil, errors.New("TypeError: user.id is not base64url")
	}
	key, err := a.key(alg)
	if err != nil {
		return nil, err
	}

	id := make([]byte, 32)
	rand.Read(id)
	c := &heldPasskey{id: id, rpID: o.RP.ID, user: user, alg: alg, discoverable: !a.NotDiscoverable}
	a.credentials = append(a.credentials, c)

	attested := slices.Concat(make([]byte, 16), binary.BigEndian.AppendUint16(nil, uint16(len(id))), id, coseKey(alg, key.Public()))
	u := Unsigned{
		ClientDataJSON:    a.clientData("webauthn.create", o.Challenge),
		AuthenticatorData: a.authData(o.RP.ID, a.next(c), authFlagAT, attested),
	}
	if a.Tamper != nil {
		a.Tamper(&u)
	}
	format, stmt := "none", []byte{0xa0}
	if a.Statement != nil {
		format, stmt = a.Statement.Format, a.Statement.CBOR
	}
	// The keys in CTAP2's canonical order: shorter first.
	obj := slices.Concat(cborHead(5, 3),
		cborText("fmt"), cborText(format),
		cborText("attStmt"), stmt,
		cborText("authData"), cborBytes(u.AuthenticatorData))

	return json.Marshal(map[string]any{
		"id":    base64url.EncodeToString(id),
		"rawId": base64url.EncodeToString(id),
		"type":  "public-key",
		"response": map[string]any{
			"clientDataJSON":    base64url.EncodeToString(u.ClientDataJSON),
			"attestationObject": base64url.EncodeToString(obj),
			"transports":        []string{"internal"},
		},
		"authenticatorAttachment": "platform",
		"clientExtensionResults":  map[string]any{},
	})
}

// Get answers request options with a signature of a passkey it holds, as
// navigator.credentials.get() and toJSON() do: one in allowCredentials, or
// with none listed the newest discoverable one for the RP ID. Like a
// browser, it refuses options whose RP ID does not match its origin, and
// fails when it holds no such passkey.
func (a *Authenticator) Get(options []byte) ([]byte, error) {
	var o requestOptions
	if err := json.Unmarshal(options, &o); err != nil {
		return nil, fmt.Errorf("read the request options: %w", err)
	}
	if err := a.checkRPID(o.RPID); err != nil {
		return nil, err
	}
	var c *heldPasskey
	if len(o.AllowCredentials) == 0 {
		for _, cand := range slices.Backward(a.credentials) {
			if cand.discoverable && cand.rpID == o.RPID {
				c = cand
				break
			}
		}
	}
	for _, allowed := range o.AllowCredentials {
		if c = a.find(allowed.ID, o.RPID); c != nil {
			break
		}
	}
	if c == nil {
		return nil, errors.New("NotAllowedError: no passkey it holds answers")
	}
	key, err := a.key(c.alg)
	if err != nil {
		return nil, err
	}

	u := Unsigned{
		ClientDataJSON:    a.clientData("webauthn.get", o.Challenge),
		AuthenticatorData: a.authData(o.RPID, a.next(c), 0, nil),
	}
	if a.Tamper != nil {
		a.Tamper(&u)
	}
	clientDataHash := sha256.Sum256(u.ClientDataJSON)
	sig, err := signAnswer(key, c.alg, slices.Concat(u.AuthenticatorData, clientDataHash[:]))
	if err != nil {
		return nil, err
	}

	response := map[string]any{
		"clientDataJSON":    base64url.EncodeToString(u.ClientDataJSON),
		"authenticatorData": base64url.EncodeToString(u.AuthenticatorData),
		"signature":         base64url.EncodeToString(sig),
	}
	if c.discoverable {
		response["userHandle"] = base64url.EncodeToString(c.user)
	}
	return json.Marshal(map[string]any{
		"id":                      base64url.EncodeToString(c.id),
		"rawId":                   base64url.EncodeToString(c.id),
		"type":                    "public-key",
		"response":                response,
		"authenticatorAttachment": "platform",
		"clientExtensionResults":  map[string]any{},
	})
}

// checkRPID refuses an RP ID that is not the origin's host or a suffix of
// it, as a browser does.
func (a *Authenticator) checkRPID(rpID string) error {
	u, err := url.Parse(a.Origin)
	if err != nil {
		return fmt.Errorf("read the origin: %w", err)
	}
	if host := u.Hostname(); rpID == "" || host != rpID && !strings.HasSuffix(host, "."+rpID) {
		return errors.New("SecurityError: the RP ID is not the origin's host")
	}
	return nil
}

func (a *Authenticator) find(id, rpID string) *heldPasskey {
	raw, err := base64url.DecodeString(id)
	if err != nil {
		return nil
	}
	for _, c := range a.credentials {
		if string(c.id) == string(raw) && c.rpID == rpID {
			return c
		}
	}
	return nil
}

// next moves c's counter as a.Counter says and returns it.
func (a *Authenticator) next(c *heldPasskey) uint32 {
	switch a.Counter {
	case CounterZero:
		c.counter = 0
	case CounterGoesBack:
		c.counter = max(c.counter, 1) - 1
	default:
		c.counter++
	}
	return c.counter
}

// clientData returns the client data, its members in the order browsers
// write them (WebAuthn Level 3, 5.8.1.2).
func (a *Authenticator) clientData(ceremony, challenge string) []byte {
	cd := struct {
		Type        string `json:"type"`
		Challenge   string `json:"challenge"`
		Origin      string `json:"origin"`
		CrossOrigin bool   `json:"crossOrigin"`
		TopOrigin   string `json:"topOrigin,omitempty"`
	}{cmp.Or(a.Type, ceremony), challenge, a.Origin, a.CrossOrigin, ""}
	if a.CrossOrigin {
		cd.TopOrigin = "https://elsewhere.example.net"
	}
	out, err := json.Marshal(cd)
	if err != nil {
		panic(err)
	}
	return out
}

// The flags of authenticator data.
const (
	authFlagUP = 1 << 0
	authFlagUV = 1 << 2
	authFlagBE = 1 << 3
	authFlagBS = 1 << 4
	authFlagAT = 1 << 6
)

func (a *Authenticator) authData(rpID string, counter uint32, flags byte, attested []byte) []byte {
	hash := sha256.Sum256([]byte(cmp.Or(a.RPID, rpID)))
	flags |= authFlagUP
	if !a.SkipUV {
		flags |= authFlagUV
	}
	if a.BackupEligible {
		flags |= authFlagBE
	}
	if a.BackedUp {
		flags |= authFlagBS
	}
	return slices.Concat(hash[:], []byte{flags}, binary.BigEndian.AppendUint32(nil, counter), attested)
}

func (a *Authenticator) key(alg int) (crypto.Signer, error) {
	if k, ok := a.keys[alg]; ok {
		return k, nil
	}
	var k crypto.Signer
	var err error
	switch alg {
	case coseEdDSA:
		_, k, err = ed25519.GenerateKey(rand.Reader)
	case coseES256:
		k, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case coseRS256:
		k, err = sharedRSAKey()
	default:
		return nil, fmt.Errorf("make a key of algorithm %d: not one it makes", alg)
	}
	if err != nil {
		return nil, fmt.Errorf("make a key of algorithm %d: %w", alg, err)
	}
	if a.keys == nil {
		a.keys = map[int]crypto.Signer{}
	}
	a.keys[alg] = k
	return k, nil
}

// sharedRSAKey is the RS256 key every Authenticator holds.
var sharedRSAKey = sync.OnceValues(func() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 2048)
})

func signAnswer(key crypto.Signer, alg int, data []byte) ([]byte, error) {
	if alg == coseEdDSA {
		return key.Sign(rand.Reader, data, crypto.Hash(0))
	}
	digest := sha256.Sum256(data)
	return key.Sign(rand.Reader, digest[:], crypto.SHA256)
}

// coseKey encodes pub as a COSE_Key of alg, its labels in CTAP2's canonical
// order.
func coseKey(alg int, pub crypto.PublicKey) []byte {
	switch k := pub.(type) {
	case ed25519.PublicKey:
		return slices.Concat(cborHead(5, 4),
			cborInt(1), cborInt(1), cborInt(3), cborInt(alg),
			cborInt(-1), cborInt(6), cborInt(-2), cborBytes(k))
	case *ecdsa.PublicKey:
		point, err := k.Bytes()
		if err != nil {
			panic(err)
		}
		return slices.Concat(cborHead(5, 5),
			cborInt(1), cborInt(2), cborInt(3), cborInt(alg),
			cborInt(-1), cborInt(1), cborInt(-2), cborBytes(point[1:33]), cborInt(-3), cborBytes(point[33:]))
	case *rsa.PublicKey:
		return slices.Concat(cborHead(5, 4),
			cborInt(1), cborInt(3), cborInt(3), cborInt(alg),
			cborInt(-1), cborBytes(k.N.Bytes()), cborInt(-2), cborBytes([]byte{1, 0, 1}))
	default:
		panic(fmt.Sprintf("no COSE_Key for %T", pub))
	}
}

// cborHead encodes a CBOR item's head: its major type and argument, in the
// shortest form.
func cborHead(major byte, n uint64) []byte {
	m := major << 5
	switch {
	case n < 24:
		return []byte{m | byte(n)}
	case n <= 0xff:
		return []byte{m | 24, byte(n)}
	case n <= 0xffff:
		return binary.BigEndian.AppendUint16([]byte{m | 25}, uint16(n))
	case n <= 0xffffffff:
		return binary.BigEndian.AppendUint32([]byte{m | 26}, uint32(n))
	default:
		return binary.BigEndian.AppendUint64([]byte{m | 27}, n)
	}
}

func cborInt(n int) []byte {
	if n < 0 {
		return cborHead(1, uint64(-1-n))
	}
	return cborHead(0, uint64(n))
}

func cborBytes(b []byte) []byte { return append(cborHead(2, uint64(len(b))), b...) }
func cborText(s string) []byte  { return append(cborHead(3, uint64(len(s))), s...) }
