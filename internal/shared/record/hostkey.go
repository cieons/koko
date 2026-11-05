package record

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// fingerprintPrefix starts a host key's SHA-256 fingerprint, as OpenSSH
// prints it.
const fingerprintPrefix = "SHA256:"

// CheckHostKey reports why s is not a host key's fingerprint as the host_key
// field holds it and --host-key takes it, or nil when it is one: "SHA256:"
// and the 43 characters of unpadded base64 that ssh-keygen -lf prints.
func CheckHostKey(s string) error {
	digest, ok := strings.CutPrefix(s, fingerprintPrefix)
	if !ok {
		return errors.New("a host key's fingerprint starts with SHA256:, as ssh-keygen -lf prints it")
	}
	if b, err := base64.RawStdEncoding.Strict().DecodeString(digest); err != nil || len(b) != sha256.Size {
		return errors.New("a host key's fingerprint is SHA256: and 43 characters of base64, as ssh-keygen -lf prints it")
	}
	return nil
}
