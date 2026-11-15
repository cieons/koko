package webauthn

import "errors"

// RegisterWithoutUV is Register with user verification not required, for
// the test vectors made without it.
func RegisterWithoutUV(c Ceremony, user []byte, r Registration) (Passkey, error) {
	return register(c, user, r, false)
}

// AssertWithoutUV is Assert with user verification not required, for the
// test vectors made without it.
func AssertWithoutUV(c Ceremony, a Assertion, p Passkey) (Passkey, error) {
	return assert(c, a, p, false)
}

// Check names the check whose failure err is, or "" when err is no
// refusal.
func Check(err error) string {
	if r, ok := errors.AsType[*refusal](err); ok {
		return r.check
	}
	return ""
}
