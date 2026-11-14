package api

import (
	"bytes"
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

// EnrollPassword is the password Enroll takes from b, read whole from stdin
// or a Secret: b less one line ending at its end, which echo adds and no
// password typed at ssh's prompt can hold.
func EnrollPassword(b []byte) []byte {
	return bytes.TrimSuffix(bytes.TrimSuffix(b, []byte("\n")), []byte("\r"))
}

// HostEnrollRequest Enrolls a Host: the koko server logs in to it once as
// its Host Account with the password or the private key given, or held in
// the Secret named, never with the koko key, checks its host key, and
// appends the koko key to the Host Account's authorized_keys.
type HostEnrollRequest struct {
	// Host names the Host, by its name or its id.
	Host string `json:"host"`
	// Password is the Host Account's password. Exactly one of Password,
	// PrivateKey and Secret is given.
	Password EnrollInput `json:"password_base64,omitzero"`
	// PrivateKey is a private key the Host Account accepts, in a form
	// golang.org/x/crypto/ssh parses: OpenSSH, PKCS #1, PKCS #8 or SEC 1,
	// in PEM. One that is encrypted comes with its Passphrase. koko host
	// enroll --key decrypts an encrypted key on the device, so its
	// passphrase never leaves it; the web UI sends both.
	PrivateKey EnrollInput `json:"private_key_base64,omitzero"`
	// Passphrase opens PrivateKey when it is encrypted, and is given with
	// an encrypted PrivateKey alone. The koko server decrypts the key with
	// it once, and keeps and logs neither.
	Passphrase EnrollInput `json:"passphrase_base64,omitzero"`
	// Secret names a Secret that holds the Host Account's password or a
	// private key it accepts. The koko server reads it on its own side, for
	// the caller, who must be able to act in its Space, so the value never
	// crosses the API. A value that begins with a PEM header ("-----BEGIN ")
	// is a private key, taken as PrivateKey is; any other is a password, as
	// EnrollPassword reads one.
	Secret *SecretRef `json:"secret,omitzero"`
	// HostKey, when given, is the SHA-256 fingerprint ("SHA256:...") of the
	// only host key Enroll accepts, which it then pins in place of any pin
	// the Host has.
	HostKey *string `json:"host_key,omitzero"`
	// ReplaceHostKey pins whatever host key the Host offers, in place of
	// the pinned one, with the login as its only proof: a fresh trust on
	// first use. It is not given with HostKey.
	ReplaceHostKey bool `json:"replace_host_key,omitzero"`
}

// SecretRef names a Secret, by its Space and its name, as the secret
// operations name one: in host.enroll's secret, and in the batches of
// secret.move and secret.remove. Both are left out when empty only so that
// the batches' fields, added at their revision 2, follow the rule for such
// fields (Op.Added); a Secret named has both.
type SecretRef struct {
	Space string `json:"space,omitempty"`
	Name  string `json:"name,omitempty"`
}

// String is the Secret's name as a command takes it, <space>/<secret>.
func (s SecretRef) String() string {
	return s.Space + "/" + s.Name
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
// Enroll a Managed Host again. A Secret is read as secret.show reads it, so
// one in a Space the caller cannot act in is refused as one that does not
// exist, with CodeUnknownSpace, whatever the caller can do on the Host. The
// login runs before the write's transaction, so a slow Host holds up no
// other write.
var HostEnroll = Op[HostEnrollRequest, HostEnrollResponse]{
	Name: "host.enroll", Write: true, Revision: 2, Added: map[string]int{"passphrase_base64": 2},
}

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
	// after retrying the network errors, or could not log in to its Relay
	// with the koko key, which the message says without naming a Relay the
	// caller cannot see.
	CodeHostUnreachable = Code{"host_unreachable", http.StatusBadGateway}
	// CodeKeyNotAdded: koko logged in to the Host but could not append the
	// koko key to the Host Account's authorized_keys, such as for an
	// account without a shell; the Host stays as it was in koko's record.
	CodeKeyNotAdded = Code{"key_not_added", http.StatusBadGateway}
	// CodeAlreadyManaged: the Host is Managed, and the call gives neither
	// host_key nor replace_host_key, which alone Enroll a Managed Host
	// again.
	CodeAlreadyManaged = Code{"already_managed", http.StatusConflict}
	// CodeHostChanged: the Host's Host Account, state, pinned host key or
	// Relay changed while Enroll logged in to it, so Enroll recorded nothing,
	// though the koko key may be in the authorized_keys of the Host Account
	// it logged in as; it may run again.
	CodeHostChanged = Code{"host_changed", http.StatusConflict}
	// CodePassphraseNeeded: private_key_base64 is encrypted, and the call
	// gives no passphrase_base64 to open it.
	CodePassphraseNeeded = Code{"passphrase_needed", http.StatusBadRequest}
	// CodeWrongPassphrase: passphrase_base64 does not open the encrypted
	// private_key_base64.
	CodeWrongPassphrase = Code{"wrong_passphrase", http.StatusBadRequest}
	// CodeKeyTooCostly: the encrypted private_key_base64 asks for more
	// rounds of its KDF than Enroll runs (256), which a key made by
	// ssh-keygen with its default, or with -a 100, never does.
	CodeKeyTooCostly = Code{"key_too_costly", http.StatusBadRequest}
)

var enrollCodes = []Code{
	CodeUnexpectedHostKey,
	CodeLoginRefused,
	CodeHostUnreachable,
	CodeKeyNotAdded,
	CodeAlreadyManaged,
	CodeHostChanged,
	CodePassphraseNeeded,
	CodeWrongPassphrase,
	CodeKeyTooCostly,
}
