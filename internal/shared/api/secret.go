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
// ending in _base64, so text and binary travel alike (ADR 0028). Only the
// requests of secret.add and secret.set, and secret.show's response, hold
// one.
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

// SecretSetRequest replaces the value of a Secret its Space holds.
type SecretSetRequest struct {
	Space string `json:"space"`
	Name  string `json:"name"`
	// Value is the new value, 1 byte to 64 KiB, in standard base64
	// (RFC 4648, padded).
	Value SecretValue `json:"value_base64"`
}

// SecretSetResponse is the Secret secret.set changed, without its value.
type SecretSetResponse struct {
	Secret Secret `json:"secret"`
}

// SecretRenameRequest gives a Secret a new name within its Space.
type SecretRenameRequest struct {
	Space string `json:"space"`
	Name  string `json:"name"`
	// NewName is a name the Space does not hold.
	NewName string `json:"new_name"`
}

// SecretRenameResponse is the Secret under its new name, without its value.
type SecretRenameResponse struct {
	Secret Secret `json:"secret"`
}

// SecretMoveRequest moves a Secret, keeping its name, to another Space.
type SecretMoveRequest struct {
	Space string `json:"space"`
	Name  string `json:"name"`
	// NewSpace names the Space to move it to, which must not hold its name.
	NewSpace string `json:"new_space"`
}

// SecretMoveResponse is the Secret in its new Space, without its value.
type SecretMoveResponse struct {
	Secret Secret `json:"secret"`
}

// SecretRemoveRequest names a Secret to delete.
type SecretRemoveRequest struct {
	Space string `json:"space"`
	Name  string `json:"name"`
}

// SecretRemoveResponse is the Secret secret.remove deleted, as it was,
// without its value.
type SecretRemoveResponse struct {
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
	// SecretSet replaces a Secret's value, sealed afresh; the last call
	// wins. A name the Space does not hold is refused with
	// CodeUnknownSecret, never added, and the value is checked as
	// SecretAdd checks it.
	SecretSet = Op[SecretSetRequest, SecretSetResponse]{Name: "secret.set", Write: true}
	// SecretRename gives a Secret a new name within its Space, leaving its
	// sealed value as it is. A name the Space holds, its own included, is
	// refused with CodeNameTaken.
	SecretRename = Op[SecretRenameRequest, SecretRenameResponse]{Name: "secret.rename", Write: true}
	// SecretMove moves a Secret, keeping its name and its sealed value, to
	// another Space; the caller must be able to act in both, and a NewSpace
	// they cannot act in is refused with CodeUnknownSpace too. A NewSpace
	// that holds the name is refused with CodeNameTaken; moving a Secret to
	// its own Space changes nothing.
	SecretMove = Op[SecretMoveRequest, SecretMoveResponse]{Name: "secret.move", Write: true}
	// SecretRemove deletes a Secret and its value, for good.
	SecretRemove = Op[SecretRemoveRequest, SecretRemoveResponse]{Name: "secret.remove", Write: true}
)

var secretOperations = []Operation{
	SecretList,
	SecretShow,
	SecretAdd,
	SecretSet,
	SecretRename,
	SecretMove,
	SecretRemove,
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
