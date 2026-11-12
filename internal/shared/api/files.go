package api

import "net/http"

// The web terminal's files endpoints, the web front's own under
// /terminal/files/ on the web listener and on web@koko, apart from the API
// (P1 spec: a terminal in the web UI, "Files over SFTP"; ADR 0053). Each
// names a web terminal's SSH Session by its id, and moves files on its Host
// over SFTP, as the Host Account. A JSON one takes and answers JSON, at most
// MaxFilesBody bytes, from a page of the koko server.
const (
	// FilesPrefix starts the path of every files endpoint.
	FilesPrefix = "/terminal/files/"
	// FilesListPath lists a directory on the Host with a FilesListRequest,
	// answering a FilesListResponse.
	FilesListPath = FilesPrefix + "list"
	// MaxFilesBody is the largest JSON body a files endpoint reads.
	MaxFilesBody = 4 << 10
	// MaxListed is the most entries a listing holds; the rest are counted.
	MaxListed = 5000
)

// The files endpoints that take and answer JSON, with their types.
var (
	// FilesList lists the directory a path names on the Host, following
	// links, or the Host Account's home for an empty path.
	FilesList = AuthEndpoint[FilesListRequest, FilesListResponse]{Path: FilesListPath}
)

// FilesEndpoints returns every files endpoint that takes and answers JSON.
func FilesEndpoints() []AuthEndpointInfo {
	return []AuthEndpointInfo{FilesList.Info()}
}

// FilesListRequest names a directory on the Host of the web terminal's SSH
// Session Tunnel, its id.
type FilesListRequest struct {
	Tunnel string `json:"tunnel"`
	// Path is absolute, or empty for the Host Account's home.
	Path string `json:"path"`
}

// FilesListResponse is a directory on the Host.
type FilesListResponse struct {
	// Path is the directory's path as the Host resolved it, links, "."
	// and ".." followed.
	Path string `json:"path"`
	// Entries are at most MaxListed of its entries, directories first,
	// then by name, byte for byte; "." and ".." are not among them.
	Entries []FileEntry `json:"entries"`
	// More counts the entries past MaxListed, which are not listed.
	More int `json:"more"`
}

// FileEntry is one entry of a directory on the Host.
type FileEntry struct {
	Name string `json:"name"`
	// Kind is "file", "dir", "link" (a symbolic link, not followed) or
	// "other", such as a FIFO or a socket.
	Kind string `json:"kind"`
	// Size is its size in bytes, as the Host gives it.
	Size int64 `json:"size"`
	// Modified is when it last changed, RFC 3339 in UTC.
	Modified string `json:"modified"`
}

// The files area's codes.
var (
	// CodeUnknownTunnel: a files call named no live web terminal SSH
	// Session of this caller.
	CodeUnknownTunnel = Code{"unknown_tunnel", http.StatusNotFound}
	// CodeSSHSessionEnded: the web terminal's SSH Session ended, which
	// ends every transfer of it.
	CodeSSHSessionEnded = Code{"ssh_session_ended", http.StatusGone}
	// CodeSFTPUnavailable: the Host offers no SFTP, which koko moves files
	// through.
	CodeSFTPUnavailable = Code{"sftp_unavailable", http.StatusConflict}
	// CodeHostRefused: the Host refused a listing, a read or a write; the
	// message is the Host's.
	CodeHostRefused = Code{"host_refused", http.StatusForbidden}
)

var filesCodes = []Code{
	CodeUnknownTunnel,
	CodeSSHSessionEnded,
	CodeSFTPUnavailable,
	CodeHostRefused,
}
