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
	// FilesUploadPath starts an upload with a FilesUploadRequest, answering
	// a FilesUploadResponse.
	FilesUploadPath = FilesPrefix + "upload"
	// FilesChunkPath takes one chunk of an upload, at most MaxFilesChunk
	// bytes of application/octet-stream, as a POST to
	// FilesChunkPath?upload=<id>&offset=<n>, answering a
	// FilesChunkResponse.
	FilesChunkPath = FilesPrefix + "chunk"
	// FilesFinishPath finishes an upload with a FilesFinishRequest,
	// answering a FilesFinishResponse.
	FilesFinishPath = FilesPrefix + "finish"
	// FilesCancelPath cancels an upload with a FilesCancelRequest,
	// answering a FilesCancelResponse.
	FilesCancelPath = FilesPrefix + "cancel"
	// FilesDownloadPath gives a download's token for a
	// FilesDownloadRequest, answering a FilesDownloadResponse; a GET of
	// FilesDownloadPath/<token> answers the file's bytes.
	FilesDownloadPath = FilesPrefix + "download"
	// MaxFilesBody is the largest JSON body a files endpoint reads.
	MaxFilesBody = 4 << 10
	// MaxListed is the most entries a listing holds; the rest are counted.
	MaxListed = 5000
	// MaxFilesChunk is the largest chunk of an upload: under the 1 MiB
	// body that nginx takes by default.
	MaxFilesChunk = 512 << 10
	// MaxUploads is how many uploads one SSH Session runs at once; the
	// page queues the rest.
	MaxUploads = 4
)

// The files endpoints that take and answer JSON, with their types.
var (
	// FilesList lists the directory a path names on the Host, following
	// links, or the Host Account's home for an empty path.
	FilesList = AuthEndpoint[FilesListRequest, FilesListResponse]{Path: FilesListPath}
	// FilesUpload starts an upload into a directory on the Host: a hidden
	// partial file beside its name, which FilesFinish renames into place.
	// A name that exists is refused with CodeFileExists unless the request
	// replaces it, and a fifth upload of one SSH Session with
	// CodeTooManyUploads.
	FilesUpload = AuthEndpoint[FilesUploadRequest, FilesUploadResponse]{Path: FilesUploadPath}
	// FilesFinish renames an upload's partial file onto its name once every
	// byte has come, and answers the same again when sent again.
	FilesFinish = AuthEndpoint[FilesFinishRequest, FilesFinishResponse]{Path: FilesFinishPath}
	// FilesCancel cancels an upload, removing its partial file.
	FilesCancel = AuthEndpoint[FilesCancelRequest, FilesCancelResponse]{Path: FilesCancelPath}
	// FilesDownload gives a token for a regular file on the Host, links
	// followed; anything else, such as a FIFO, is refused with
	// CodeNotAFile. A GET of FilesDownloadPath/<token>, once, within 60
	// seconds, by the same caller, answers the file's bytes as an
	// attachment, which the browser saves with its own progress and Cancel.
	FilesDownload = AuthEndpoint[FilesDownloadRequest, FilesDownloadResponse]{Path: FilesDownloadPath}
)

// FilesEndpoints returns every files endpoint that takes and answers JSON.
func FilesEndpoints() []AuthEndpointInfo {
	return []AuthEndpointInfo{FilesList.Info(), FilesUpload.Info(), FilesFinish.Info(), FilesCancel.Info(), FilesDownload.Info()}
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

// FilesUploadRequest starts an upload of a file of Size bytes, named Name,
// into the directory Dir on the Host of the web terminal's SSH Session
// Tunnel.
type FilesUploadRequest struct {
	Tunnel string `json:"tunnel"`
	// Dir is absolute.
	Dir string `json:"dir"`
	// Name is the file's name in Dir: not empty, "." or "..", and without
	// "/".
	Name string `json:"name"`
	Size int64  `json:"size"`
	// Replace replaces a file of that name, which is otherwise refused.
	Replace bool `json:"replace,omitzero"`
}

// FilesUploadResponse names the upload started.
type FilesUploadResponse struct {
	// Upload is the upload's id, which its chunks, finish and cancel name.
	Upload string `json:"upload"`
}

// FilesChunkResponse is how much of an upload has come: the bytes from
// its start, the chunk just sent among them, or a chunk sent again not
// written twice.
type FilesChunkResponse struct {
	Received int64 `json:"received"`
}

// FilesFinishRequest names the upload to finish.
type FilesFinishRequest struct {
	Upload string `json:"upload"`
}

// FilesFinishResponse is the file an upload made on the Host.
type FilesFinishResponse struct {
	// Path is the file's absolute path.
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// FilesCancelRequest names the upload to cancel.
type FilesCancelRequest struct {
	Upload string `json:"upload"`
}

// FilesCancelResponse answers a cancel.
type FilesCancelResponse struct{}

// FilesDownloadRequest names a file to download from the Host of the web
// terminal's SSH Session Tunnel.
type FilesDownloadRequest struct {
	Tunnel string `json:"tunnel"`
	// Path is absolute.
	Path string `json:"path"`
}

// FilesDownloadResponse is a download's token, and the file's name and
// size.
type FilesDownloadResponse struct {
	// Token is the download's token, 32 lowercase hex characters, for a
	// GET of FilesDownloadPath/<token>.
	Token string `json:"token"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
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
	// CodeFileExists: an upload's name exists in its directory and the
	// upload does not replace it.
	CodeFileExists = Code{"file_exists", http.StatusConflict}
	// CodeBadOffset: a chunk would leave a gap in its upload, or an upload
	// was finished before every byte had come.
	CodeBadOffset = Code{"bad_offset", http.StatusConflict}
	// CodeUnknownUpload: no live upload of this caller has the id.
	CodeUnknownUpload = Code{"unknown_upload", http.StatusNotFound}
	// CodeTooManyUploads: the SSH Session already runs MaxUploads uploads;
	// the page queues the rest.
	CodeTooManyUploads = Code{"too_many_uploads", http.StatusTooManyRequests}
	// CodeNotAFile: a download named something other than a regular file,
	// links followed, such as a directory or a FIFO.
	CodeNotAFile = Code{"not_a_file", http.StatusConflict}
	// CodeUnknownDownload: no unspent download token of this caller is the
	// one given.
	CodeUnknownDownload = Code{"unknown_download", http.StatusNotFound}
)

var filesCodes = []Code{
	CodeUnknownTunnel,
	CodeSSHSessionEnded,
	CodeSFTPUnavailable,
	CodeHostRefused,
	CodeFileExists,
	CodeBadOffset,
	CodeUnknownUpload,
	CodeTooManyUploads,
	CodeNotAFile,
	CodeUnknownDownload,
}
