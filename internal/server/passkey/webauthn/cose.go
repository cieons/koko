package webauthn

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
	"math/big"
)

// The COSE algorithms koko offers an authenticator (RFC 9053, RFC 8812),
// which a passkey's Algorithm holds.
const (
	EdDSA = -8
	ES256 = -7
	RS256 = -257
)

// COSE_Key parameters and values (RFC 9052 7, RFC 9053 7, RFC 8230 4).
const (
	coseKty = 1
	coseAlg = 3
	// The key-type-specific parameters: crv, x and y for OKP and EC2; n and
	// e for RSA.
	coseCrv = -1
	coseX   = -2
	coseY   = -3
	coseN   = -1
	coseE   = -2

	ktyOKP = 1
	ktyEC2 = 2
	ktyRSA = 3

	crvP256    = 1
	crvEd25519 = 6
)

// The RSA keys koko takes: a modulus of minRSABits to maxRSABits, and the
// exponent rsaExponent.
const (
	minRSABits  = 2048
	maxRSABits  = 8192
	rsaExponent = 65537
)

var errKey = errors.New("not a COSE_Key koko takes")

// A publicKey is a passkey's key, read from its COSE_Key.
type publicKey struct {
	alg     int
	ed25519 ed25519.PublicKey
	ecdsa   *ecdsa.PublicKey
	rsa     *rsa.PublicKey
}

// parseKey reads a COSE_Key of one of the three algorithms whose key type
// and curve agree with it: OKP Ed25519 for EdDSA, EC2 on P-256 for ES256,
// and RSA with a modulus of 2,048 to 8,192 bits and the exponent 65,537 for
// RS256. Other parameters are read past.
func parseKey(raw []byte) (publicKey, error) {
	k, err := decodeCBOR(raw)
	if err != nil || k.major != majorMap {
		return publicKey{}, errKey
	}
	kty, ok1 := intParam(k, coseKty)
	alg, ok2 := intParam(k, coseAlg)
	if !ok1 || !ok2 {
		return publicKey{}, errKey
	}
	switch {
	case kty == ktyOKP && alg == EdDSA:
		crv, ok := intParam(k, coseCrv)
		x, okX := bytesParam(k, coseX)
		if !ok || crv != crvEd25519 || !okX || len(x) != ed25519.PublicKeySize {
			return publicKey{}, errKey
		}
		return publicKey{alg: EdDSA, ed25519: ed25519.PublicKey(x)}, nil
	case kty == ktyEC2 && alg == ES256:
		crv, ok := intParam(k, coseCrv)
		x, okX := bytesParam(k, coseX)
		y, okY := bytesParam(k, coseY)
		if !ok || crv != crvP256 || !okX || !okY || len(x) != 32 || len(y) != 32 {
			return publicKey{}, errKey
		}
		point := append(append([]byte{4}, x...), y...)
		pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), point)
		if err != nil {
			return publicKey{}, errKey
		}
		return publicKey{alg: ES256, ecdsa: pub}, nil
	case kty == ktyRSA && alg == RS256:
		n, okN := bytesParam(k, coseN)
		e, okE := bytesParam(k, coseE)
		if !okN || !okE {
			return publicKey{}, errKey
		}
		modulus := new(big.Int).SetBytes(n)
		if bits := modulus.BitLen(); bits < minRSABits || bits > maxRSABits ||
			new(big.Int).SetBytes(e).Cmp(big.NewInt(rsaExponent)) != 0 {
			return publicKey{}, errKey
		}
		return publicKey{alg: RS256, rsa: &rsa.PublicKey{N: modulus, E: rsaExponent}}, nil
	default:
		return publicKey{}, errKey
	}
}

// verify reports whether sig is the key's signature over data: Ed25519,
// ECDSA P-256 with SHA-256 in DER, or RSASSA-PKCS1-v1_5 with SHA-256.
func (k publicKey) verify(data, sig []byte) bool {
	switch k.alg {
	case EdDSA:
		return ed25519.Verify(k.ed25519, data, sig)
	case ES256:
		digest := sha256.Sum256(data)
		return ecdsa.VerifyASN1(k.ecdsa, digest[:], sig)
	case RS256:
		digest := sha256.Sum256(data)
		return rsa.VerifyPKCS1v15(k.rsa, crypto.SHA256, digest[:], sig) == nil
	default:
		return false
	}
}

func intParam(k item, label int64) (int64, bool) {
	v, ok := k.value(intKey(label))
	if !ok {
		return 0, false
	}
	return v.int()
}

func bytesParam(k item, label int64) ([]byte, bool) {
	v, ok := k.value(intKey(label))
	if !ok || v.major != majorBytes {
		return nil, false
	}
	return v.str, true
}
