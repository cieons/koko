package api

import (
	"fmt"
	"log/slog"
	"net/http"
)

// SecretMaxSize is the longest Secret value, in bytes: 64 KiB, which fits a
// private key, a certificate or a kubeconfig. A longer value is refused,
// never stored cut short.
const SecretMaxSize = 64 << 10

// SecretValue is a Secret's value: opaque bytes, 1 to SecretMaxSize of them,
// which koko stores sealed and never decodes. JSON carries one as standard
// base64 (RFC 4648, section 4, padded), whatever its bytes, under a name
// ending in _base64, so text and binary travel alike (ADR 0028). Only
// secret.add's request and secret.show's response hold one.
//
// It prints as its length through fmt, and logs as its length through slog's
// text handler, the koko server's, so an error or a log line that formats
// one by mistake leaks nothing. A JSON encoder writes it as base64, as the
// API must.
type SecretValue []byte

// Format prints the value as its length, whatever the verb.
func (v SecretValue) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprintf(f, "<Secret value, %d bytes>", len(v))
}

// LogValue logs the value as its length.
func (v SecretValue) LogValue() slog.Value {
	return slog.StringValue(fmt.Sprintf("<Secret value, %d bytes>", len(v)))
}

// Secret is a Secret as the API names it: its name in its Space, and never
// its value.
type Secret struct {
	ID string `json:"id"`
	// Space is the name of the Space that holds it.
	Space string `json:"space"`
	// Name is unique within its Space only: 1 to 63 of a-z, 0-9, '-' and
	// '_', starting with a letter or a digit.
	Name string `json:"name"`
	// CreatedAt is when it was added, as RFC 3339 in UTC to the second.
	CreatedAt string `json:"created_at"`
	// UpdatedAt is when it last changed, as RFC 3339 in UTC to the second.
	UpdatedAt string `json:"updated_at"`
}

// SecretListRequest asks for the Secrets of one Space, or of every Space the
// caller can act in.
type SecretListRequest struct {
	// Space names the one Space to list. Empty means every Space the caller
	// can act in.
	Space string `json:"space,omitempty"`
}

// SecretListResponse holds the Secrets listed, by Space name, then by name,
// without their values.
type SecretListResponse struct {
	Secrets []Secret `json:"secrets"`
}

// SecretShowRequest names a Secret to show, by its Space and its name.
type SecretShowRequest struct {
	Space string `json:"space"`
	Name  string `json:"name"`
}

// SecretShowResponse is the Secret secret.show names, with its value: the
// one response that holds a value.
type SecretShowResponse struct {
	Secret Secret `json:"secret"`
	// Value is the value as stored, in standard base64 (RFC 4648, padded).
	Value SecretValue `json:"value_base64"`
}

// SecretAddRequest stores a new Secret under a name its Space does not hold.
type SecretAddRequest struct {
	Space string `json:"space"`
	Name  string `json:"name"`
	// Value is the value to store, 1 byte to 64 KiB, in standard base64
	// (RFC 4648, padded).
	Value SecretValue `json:"value_base64"`
}

// SecretAddResponse is the Secret secret.add stored, without its value.
type SecretAddResponse struct {
	Secret Secret `json:"secret"`
}

// The secret operations. Each names its Space as space.show does: a Space
// the caller cannot act in is refused exactly as one that does not exist,
// with CodeUnknownSpace.
var (
	// SecretList lists Secrets without their values: those of one Space, or
	// of every Space the caller can act in.
	SecretList = Op[SecretListRequest, SecretListResponse]{Name: "secret.list"}
	// SecretShow shows a Secret with its value. A name the Space does not
	// hold is refused with CodeUnknownSecret, and a sealed value that does
	// not open with CodeSecretUnreadable.
	SecretShow = Op[SecretShowRequest, SecretShowResponse]{Name: "secret.show"}
	// SecretAdd stores a new Secret, its value sealed. A name the Space
	// holds is refused with CodeNameTaken, an empty value with
	// CodeInvalidRequest, and one over SecretMaxSize with
	// CodeValueTooLarge.
	SecretAdd = Op[SecretAddRequest, SecretAddResponse]{Name: "secret.add", Write: true}
)

var secretOperations = []Operation{
	SecretList,
	SecretShow,
	SecretAdd,
}

// The secret area's codes.
var (
	// CodeUnknownSecret: the Space holds no Secret with the name given.
	CodeUnknownSecret = Code{"unknown_secret", http.StatusNotFound}
	// CodeValueTooLarge: a Secret value is over SecretMaxSize; koko never
	// stores one cut short.
	CodeValueTooLarge = Code{"value_too_large", http.StatusRequestEntityTooLarge}
	// CodeSecretUnreadable: a Secret's sealed value does not open under the
	// Root Key, for its own record: the database was edited or damaged. The
	// error names the Secret, never a value.
	CodeSecretUnreadable = Code{"secret_unreadable", http.StatusInternalServerError}
)

var secretCodes = []Code{
	CodeUnknownSecret,
	CodeValueTooLarge,
	CodeSecretUnreadable,
}
