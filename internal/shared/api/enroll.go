package api

import (
	"fmt"
	"log/slog"
	"net/http"
)

// EnrollInputMaxSize is the longest password or private key Enroll takes,
// in bytes: 64 KiB, far more than any key needs.
const EnrollInputMaxSize = 64 << 10

// EnrollInput is a password or a private key supplied to Enroll, which the
// koko server logs in to the Host with once and never stores or logs. JSON
// carries one as standard base64, under a name ending in _base64, since a
// password is bytes and need not be UTF-8 (as a Secret's value, ADR 0028).
//
// It prints as its length through fmt, and logs as its length through slog's
// text handler, so an error or a log line that formats one by mistake leaks
// nothing.
type EnrollInput []byte

// Format prints the input as its length, whatever the verb.
func (v EnrollInput) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprintf(f, "<Enroll input, %d bytes>", len(v))
}

// LogValue logs the input as its length.
func (v EnrollInput) LogValue() slog.Value {
	return slog.StringValue(fmt.Sprintf("<Enroll input, %d bytes>", len(v)))
}

// HostEnrollRequest Enrolls a Host: the koko server logs in to it once as
// its Host Account with the password or the private key given, never with
// the koko key, checks its host key, and appends the koko key to the Host
// Account's authorized_keys.
type HostEnrollRequest struct {
	// Host names the Host, by its name or its id.
	Host string `json:"host"`
	// Password is the Host Account's password. Exactly one of Password and
	// PrivateKey is given.
	Password EnrollInput `json:"password_base64,omitzero"`
	// PrivateKey is a private key the Host Account accepts, unencrypted, in
	// a form golang.org/x/crypto/ssh parses: OpenSSH, PKCS #1, PKCS #8 or
	// SEC 1, in PEM. koko host enroll --key decrypts an encrypted key on the
	// device, so its passphrase never leaves it.
	PrivateKey EnrollInput `json:"private_key_base64,omitzero"`
	// HostKey, when given, is the SHA-256 fingerprint ("SHA256:...") of the
	// only host key Enroll accepts, which it then pins in place of any pin
	// the Host has.
	HostKey *string `json:"host_key,omitzero"`
	// ReplaceHostKey pins whatever host key the Host offers, in place of
	// the pinned one, with the login as its only proof: a fresh trust on
	// first use. It is not given with HostKey.
	ReplaceHostKey bool `json:"replace_host_key,omitzero"`
}

// HostEnrollResponse is the Host Enroll left Managed, with its pinned host
// key.
type HostEnrollResponse struct {
	Host Host `json:"host"`
}

// HostEnroll makes a Registered Host Managed: it pins the host key the Host
// offers and appends the koko key to its Host Account's authorized_keys,
// changing nothing else there. A Host with a pin must offer that key;
// host_key or replace_host_key pins a new one, which is also the only way to
// Enroll a Managed Host again. The login runs before the write's
// transaction, so a slow Host holds up no other write.
var HostEnroll = Op[HostEnrollRequest, HostEnrollResponse]{Name: "host.enroll", Write: true}

var enrollOperations = []Operation{
	HostEnroll,
}

// Enroll's codes.
var (
	// CodeUnexpectedHostKey: the Host offered a host key other than the
	// pinned one, or the one host_key names, so koko did not log in; the
	// message holds both fingerprints and the two ways on, host_key and
	// replace_host_key.
	CodeUnexpectedHostKey = Code{"unexpected_host_key", http.StatusConflict}
	// CodeLoginRefused: the Host refused the password or the private key
	// for its Host Account.
	CodeLoginRefused = Code{"login_refused", http.StatusForbidden}
	// CodeHostUnreachable: the koko server could not connect to the Host,
	// after retrying the network errors.
	CodeHostUnreachable = Code{"host_unreachable", http.StatusBadGateway}
	// CodeKeyNotAdded: koko logged in to the Host but could not append the
	// koko key to the Host Account's authorized_keys, such as for an
	// account without a shell; the Host stays as it was in koko's record.
	CodeKeyNotAdded = Code{"key_not_added", http.StatusBadGateway}
	// CodeAlreadyManaged: the Host is Managed, and the call gives neither
	// host_key nor replace_host_key, which alone Enroll a Managed Host
	// again.
	CodeAlreadyManaged = Code{"already_managed", http.StatusConflict}
	// CodeHostChanged: the Host's Host Account, state or pinned host key
	// changed while Enroll logged in to it, so Enroll recorded nothing,
	// though the koko key may be in the authorized_keys of the Host Account
	// it logged in as; it may run again.
	CodeHostChanged = Code{"host_changed", http.StatusConflict}
)

var enrollCodes = []Code{
	CodeUnexpectedHostKey,
	CodeLoginRefused,
	CodeHostUnreachable,
	CodeKeyNotAdded,
	CodeAlreadyManaged,
	CodeHostChanged,
}
