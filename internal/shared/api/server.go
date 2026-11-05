package api

import "net/http"

// ServerInitRequest initialises a fresh koko server: it names the Owner and
// sets the Server Address.
type ServerInitRequest struct {
	// Owner is the Owner's User name. It has no default, because a User's
	// name never changes.
	Owner string `json:"owner"`
	// Address is the Server Address, host or host:port (package serveraddr).
	// Without a port, its port is the koko server's listen port.
	Address string `json:"address"`
}

// ServerInitResponse is what initialisation set and made.
type ServerInitResponse struct {
	// Address is the Server Address, as the request gave it.
	Address string `json:"address"`
	// HostKeyFingerprint is the SHA-256 fingerprint of the koko server's host
	// key, as OpenSSH prints it: "SHA256:" and 43 characters of base64.
	HostKeyFingerprint string `json:"host_key_fingerprint"`
}

// The server operations.
var (
	// ServerInit initialises a fresh koko server: it makes the koko key and
	// the host key, sealed under the Root Key, creates the Owner with their
	// Personal Space, and stores the Server Address. It answers only the
	// Local caller, and is the one operation that answers before
	// initialisation; once the koko server is initialised it is refused with
	// CodeAlreadyInitialised.
	ServerInit = Op[ServerInitRequest, ServerInitResponse]{Name: "server.init", Write: true}
)

var serverOperations = []Operation{
	ServerInit,
}

// The server area's codes.
var (
	// CodeAlreadyInitialised: the koko server is initialised already, so its
	// Owner and its keys stay as they are.
	CodeAlreadyInitialised = Code{"already_initialised", http.StatusConflict}
	// CodeInvalidAddress: a Server Address is not host or host:port.
	CodeInvalidAddress = Code{"invalid_address", http.StatusBadRequest}
)

var serverCodes = []Code{
	CodeAlreadyInitialised,
	CodeInvalidAddress,
}
