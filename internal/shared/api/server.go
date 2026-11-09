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
	// JoinCode is the Owner's first Join Code, issued over the Unix socket
	// as device.add issues one there, for the Owner's first device.
	JoinCode JoinCode `json:"join_code"`
}

// Server is the koko server's deployment, as server.show and server.set
// return it. It names the Root Key's file and id, never the key (ADR 0018).
type Server struct {
	// Address is the Server Address, as stored: without a port, its port is
	// ListenPort.
	Address string `json:"address"`
	// ListenPort is the TCP port the koko server listens on.
	ListenPort int `json:"listen_port"`
	// HostKeyFingerprint is the SHA-256 fingerprint of the host key devices
	// pin from their Join Codes, as OpenSSH prints it.
	HostKeyFingerprint string `json:"host_key_fingerprint"`
	// HostKey is that host key itself, written as in authorized_keys
	// without a comment ("ssh-ed25519 AAAA..."): the Gateway presents it
	// inside every Tunnel, so koko ssh setup writes it into koko's
	// known_hosts on the koko server's own machine, where nothing joins.
	HostKey string `json:"host_key"`
	// Version is the koko server's version, as `koko version` names its
	// own: a release such as v0.3.0, a pseudo-version, or "(devel)".
	Version string `json:"version"`
	// RootKeyFile is the Root Key's file as the Owner keeps it: under
	// systemd, the file the koko unit hands the service, /etc/koko/root.key.
	RootKeyFile string `json:"root_key_file"`
	// RootKeyID is the id the Root Key's file names, so the Owner can tell
	// which copy fits.
	RootKeyID string `json:"root_key_id"`
	// DataDir is the data directory, which holds koko.db.
	DataDir string `json:"data_dir"`
	// WebURL is the Web URL, the https:// origin the Owner's reverse proxy
	// serves the web UI at, which every request on the web listener is
	// checked against; null while it is not set.
	WebURL *string `json:"web_url"`
	// WebListen is the address the web listener listens on, plain HTTP for
	// the reverse proxy alone; null while it is off, as it is unless kokod
	// run is given --web-listen.
	WebListen *string `json:"web_listen"`
}

// ServerShowRequest asks for the koko server's deployment.
type ServerShowRequest struct{}

// ServerShowResponse is the koko server's deployment.
type ServerShowResponse struct {
	Server Server `json:"server"`
}

// ServerSetRequest changes the deployment settings it names, and only those.
type ServerSetRequest struct {
	// Address, when given, is the new Server Address, host or host:port
	// (package serveraddr). Join Codes issued before keep the address they
	// carry.
	Address *string `json:"address,omitzero"`
	// WebURL, when given, is the new Web URL, an https:// origin (package
	// weburl), checked on the web listener's next request.
	WebURL *string `json:"web_url,omitzero"`
	// Unset clears these settings: only "web_url", which turns the web
	// listener's answers into a page naming koko server set web_url=.
	Unset []string `json:"unset,omitempty"`
}

// ServerSetResponse is the koko server's deployment as server.set left it.
type ServerSetResponse struct {
	Server Server `json:"server"`
}

// The server operations.
var (
	// ServerInit initialises a fresh koko server: it makes the koko key and
	// the host key, sealed under the Root Key, creates the Owner with their
	// Personal Space, stores the Server Address, and issues the Owner's first
	// Join Code. It answers only the Local caller, and is the one operation
	// that answers before initialisation; once the koko server is
	// initialised it is refused with CodeAlreadyInitialised.
	ServerInit = Op[ServerInitRequest, ServerInitResponse]{Name: "server.init", Write: true}
	// ServerShow returns the koko server's deployment: its Server Address,
	// listen port, host key, version, Root Key's file and id, and data
	// directory.
	ServerShow = Op[ServerShowRequest, ServerShowResponse]{Name: "server.show"}
	// ServerSet changes the deployment settings the request names: the
	// Server Address, for the Join Codes issued afterwards, and the Web URL,
	// for the web listener's next request. It answers only
	// the Local caller, over the Unix socket, and never a browser tab of
	// koko web there, so no device and no browser tab can move where
	// devices connect. A Server Address a Join Code cannot carry is refused
	// with CodeInvalidAddress, and a Web URL that is not an https:// origin
	// with CodeInvalidWebURL.
	ServerSet = Op[ServerSetRequest, ServerSetResponse]{Name: "server.set", Write: true}
)

var serverOperations = []Operation{
	ServerInit,
	ServerShow,
	ServerSet,
}

// The server area's codes.
var (
	// CodeAlreadyInitialised: the koko server is initialised already, so its
	// Owner and its keys stay as they are.
	CodeAlreadyInitialised = Code{"already_initialised", http.StatusConflict}
	// CodeInvalidAddress: a Server Address is not host or host:port.
	CodeInvalidAddress = Code{"invalid_address", http.StatusBadRequest}
	// CodeInvalidWebURL: a Web URL is not an https:// origin,
	// https://<host>[:<port>] with no path.
	CodeInvalidWebURL = Code{"invalid_web_url", http.StatusBadRequest}
)

var serverCodes = []Code{
	CodeAlreadyInitialised,
	CodeInvalidAddress,
	CodeInvalidWebURL,
}
