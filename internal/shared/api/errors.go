package api

import (
	"fmt"
	"net/http"
	"slices"
)

// Error is the error object of a failed call. Its code is stable: within a
// koko major version codes are only added, never renamed or removed.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return e.Message
}

// ErrorResponse is the body of a failed call: {"error": {"code": ..., "message": ...}}.
type ErrorResponse struct {
	Error Error `json:"error"`
}

// Code is a stable snake_case error code and the HTTP status the koko server
// answers it with.
type Code struct {
	Name   string
	Status int
}

// Errorf makes an error with this code and a formatted message.
func (c Code) Errorf(format string, args ...any) *Error {
	return &Error{Code: c.Name, Message: fmt.Sprintf(format, args...)}
}

// The codes of the API itself, which every area shares.
var (
	// CodeInvalidRequest: the body is not a JSON object of the request's type.
	CodeInvalidRequest = Code{"invalid_request", http.StatusBadRequest}
	// CodeUnknownField: the request holds a field the koko server does not
	// know, so a newer CLI's option is never silently dropped.
	CodeUnknownField = Code{"unknown_field", http.StatusBadRequest}
	// CodeUnknownOperation: no operation has this name.
	CodeUnknownOperation = Code{"unknown_operation", http.StatusNotFound}
	// CodeMethodNotAllowed: every operation is a POST.
	CodeMethodNotAllowed = Code{"method_not_allowed", http.StatusMethodNotAllowed}
	// CodeUnsupportedMediaType: every request body is application/json.
	CodeUnsupportedMediaType = Code{"unsupported_media_type", http.StatusUnsupportedMediaType}
	// CodeInternal: the koko server failed in a way it did not expect; its log says why.
	CodeInternal = Code{"internal_error", http.StatusInternalServerError}
	// CodeInvalidName: a name is not 1 to 63 of a-z, 0-9 and '-' (package name).
	CodeInvalidName = Code{"invalid_name", http.StatusBadRequest}
	// CodeNameTaken: a record of this kind already holds the name.
	CodeNameTaken = Code{"name_taken", http.StatusConflict}
	// CodeNotInitialised: the koko server is not initialised yet, so it
	// answers no operation but server.init.
	CodeNotInitialised = Code{"not_initialised", http.StatusConflict}
	// CodeLocalOnly: the operation answers only the Local caller, over the
	// koko server's Unix socket.
	CodeLocalOnly = Code{"local_only", http.StatusForbidden}
	// CodeOwnerOnly: only the Owner may do what the call asks, such as
	// issuing a Join Code for another User's device.
	CodeOwnerOnly = Code{"owner_only", http.StatusForbidden}
)

var commonCodes = []Code{
	CodeInvalidRequest,
	CodeUnknownField,
	CodeUnknownOperation,
	CodeMethodNotAllowed,
	CodeUnsupportedMediaType,
	CodeInternal,
	CodeInvalidName,
	CodeNameTaken,
	CodeNotInitialised,
	CodeLocalOnly,
	CodeOwnerOnly,
}

// codes lists every area's error codes after the common ones; an area that
// adds codes of its own adds its line.
var codes = [][]Code{
	commonCodes,
	serverCodes,
	userCodes,
	webCodes,
}

// Codes returns every error code the API answers with.
func Codes() []Code {
	return slices.Concat(codes...)
}

// LookupCode returns the code with this name, if the API has one.
func LookupCode(name string) (Code, bool) {
	i := slices.IndexFunc(Codes(), func(c Code) bool { return c.Name == name })
	if i < 0 {
		return Code{}, false
	}
	return Codes()[i], true
}
