package web

import (
	"cmp"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"

	"github.com/cieons/koko/internal/server/gateway"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/wire"
)

// The web terminal's files endpoints (P1 spec: a terminal in the web UI,
// "Files over SFTP"; ADR 0053) are the web front's own, under
// api.FilesPrefix on both ways in, as /auth/ is on the listener, so the CLI
// stays the one programmatic surface (ADR 0006). Each names a web
// terminal's SSH Session by its id: the web front checks the cookie as for
// the terminal's WebSocket (terminalWay), and the Gateway that the caller
// is the SSH Session's own, and hands over its SFTP client on the Host
// (gateway.Files), so the web front never holds a Host connection.

func isFiles(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, api.FilesPrefix)
}

// loggedPath is a request's path as a log line names it: a download's
// token, which its GET's path carries, never reaches the log.
func loggedPath(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, api.FilesDownloadPath+"/") {
		return api.FilesDownloadPath + "/<token>"
	}
	return r.URL.Path
}

// filesEndpoint is one files endpoint: how it reads a request's body, and
// how it answers.
type filesEndpoint struct {
	read  func(r *http.Request) ([]byte, error)
	serve func(r *http.Request, c gateway.TerminalCaller, body []byte) (any, error)
}

// serveFiles answers a request under api.FilesPrefix that came in by way:
// a download's GET (serveDownload), or else, in this order: an unknown endpoint, another method, a POST from another
// Origin (bad_origin), before anything else is read, then a page that is no
// one, as the terminal's WebSocket refuses it; then the body, read whole: a
// JSON endpoint's application/json of at most api.MaxFilesBody bytes, or a
// chunk. Every answer is JSON, which no cache keeps; neither the body, the
// query nor the cookie reaches the log.
func (f *Front) serveFiles(w http.ResponseWriter, r *http.Request, way terminalWay) {
	w.Header().Set("Cache-Control", "no-store")
	if token, ok := strings.CutPrefix(r.URL.Path, api.FilesDownloadPath+"/"); ok {
		f.serveDownload(w, r, way, token)
		return
	}
	var e filesEndpoint
	switch r.URL.Path {
	case api.FilesListPath:
		e = filesEndpoint{readJSON, filesJSON(f.filesList)}
	case api.FilesUploadPath:
		e = filesEndpoint{readJSON, filesJSON(f.filesUpload)}
	case api.FilesChunkPath:
		e = filesEndpoint{readChunk, f.filesChunk}
	case api.FilesFinishPath:
		e = filesEndpoint{readJSON, filesJSON(f.filesFinish)}
	case api.FilesCancelPath:
		e = filesEndpoint{readJSON, filesJSON(f.filesCancel)}
	case api.FilesDownloadPath:
		e = filesEndpoint{readJSON, filesJSON(f.filesDownload)}
	default:
		f.refuseAPI(w, r, api.CodeUnknownOperation.Errorf("no endpoint is served at %s", r.URL.Path))
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		f.refuseAPI(w, r, api.CodeMethodNotAllowed.Errorf("%s is not allowed: %s takes a POST", r.Method, r.URL.Path))
		return
	}
	if !way.origin(r.Header.Get("Origin")) {
		f.log.Debug("web files request refused", "reason", "origin", "client", clientAddress(r))
		f.refuseAPI(w, r, api.CodeBadOrigin.Errorf("this request came from no page of this koko server, so it was refused"))
		return
	}
	c, refused := way.caller(r.Context())
	if refused != nil {
		f.refuseAPI(w, r, refusalError(*refused))
		return
	}
	body, err := e.read(r)
	if err != nil {
		f.refuseAPI(w, r, err)
		return
	}
	resp, err := e.serve(r, c, body)
	if err != nil {
		f.refuseAPI(w, r, err)
		return
	}
	writeJSON(w, resp)
}

// readJSON reads a JSON endpoint's body: application/json of at most
// api.MaxFilesBody bytes.
func readJSON(r *http.Request) ([]byte, error) {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		return nil, api.CodeUnsupportedMediaType.Errorf("the request body must be application/json")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, api.MaxFilesBody+1))
	if err != nil {
		return nil, api.CodeInvalidRequest.Errorf("read the request body: %v", err)
	}
	if len(body) > api.MaxFilesBody {
		return nil, api.CodeRequestTooLarge.Errorf("the request body is over the limit of %d KiB of %s", api.MaxFilesBody>>10, r.URL.Path)
	}
	return body, nil
}

// filesJSON is a JSON files endpoint that handle answers: it decodes the
// body into handle's request, refusing a member it does not know.
func filesJSON[Req, Resp any](handle func(c gateway.TerminalCaller, req Req) (Resp, error)) func(r *http.Request, c gateway.TerminalCaller, body []byte) (any, error) {
	return func(r *http.Request, c gateway.TerminalCaller, body []byte) (any, error) {
		req, err := decodeAuth[Req](r, body)
		if err != nil {
			return nil, err
		}
		return handle(c, req)
	}
}

// refusalError is a terminal refusal of a page that is no one, such as
// signin_required, as the API's error object a files call is answered with.
func refusalError(a wire.TerminalAnswer) error {
	message := ""
	if a.Message != nil {
		message = *a.Message
	}
	return &api.Error{Code: a.Refused, Message: message}
}

// hostFiles returns the files of the web terminal's SSH Session that tunnel, its
// id, names for the caller c (gateway.Files); an id that is no SSH Session's
// names none of c's.
func (f *Front) hostFiles(c gateway.TerminalCaller, tunnel string) (*gateway.Files, error) {
	id, err := wire.ParseTerminalID(tunnel)
	if err != nil {
		return nil, api.CodeUnknownTunnel.Errorf("this browser has no open SSH Session with this id")
	}
	return f.gateway.Files(c, id)
}

// filesList lists the directory req names on the Host, or the Host
// Account's home for an empty path: its path as the Host resolves it, and
// at most api.MaxListed of its entries, directories first, then by name,
// the rest counted.
func (f *Front) filesList(c gateway.TerminalCaller, req api.FilesListRequest) (api.FilesListResponse, error) {
	if req.Path != "" && !path.IsAbs(req.Path) {
		return api.FilesListResponse{}, api.CodeInvalidRequest.Errorf("path must be absolute, or empty for the Host Account's home")
	}
	files, err := f.hostFiles(c, req.Tunnel)
	if err != nil {
		return api.FilesListResponse{}, err
	}
	dir, err := files.SFTP.RealPath(cmp.Or(req.Path, "."))
	if err != nil {
		return api.FilesListResponse{}, hostError(files, err)
	}
	infos, err := files.SFTP.ReadDir(dir)
	if err != nil {
		return api.FilesListResponse{}, hostError(files, err)
	}
	entries := make([]api.FileEntry, 0, len(infos))
	for _, fi := range infos {
		entries = append(entries, api.FileEntry{
			Name: fi.Name(), Kind: fileKind(fi.Mode()), Size: fi.Size(),
			Modified: fi.ModTime().UTC().Format(time.RFC3339),
		})
	}
	slices.SortFunc(entries, func(a, b api.FileEntry) int {
		if (a.Kind == "dir") != (b.Kind == "dir") {
			if a.Kind == "dir" {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	more := max(len(entries)-api.MaxListed, 0)
	return api.FilesListResponse{Path: dir, Entries: entries[:len(entries)-more], More: more}, nil
}

// fileKind is how a listing names an entry of mode: a link is not followed.
func fileKind(mode fs.FileMode) string {
	switch {
	case mode&fs.ModeSymlink != 0:
		return "link"
	case mode.IsDir():
		return "dir"
	case mode.IsRegular():
		return "file"
	}
	return "other"
}

// hostError is what a files call answers when an SFTP request on files
// failed with err: ssh_session_ended once the SSH Session has ended, which
// cuts its requests; host_refused, with the Host's message, for a status
// the Host answered; err itself otherwise, as the koko server's failure.
func hostError(files *gateway.Files, err error) error {
	if ended := files.Err(); ended != nil {
		return ended
	}
	if e, ok := errors.AsType[*api.Error](err); ok {
		return e
	}
	if message := hostMessage(err); message != "" {
		return api.CodeHostRefused.Errorf("%s", message)
	}
	return err
}

// hostMessage is the message of the status an SFTP request failed with,
// as the Host sent it, or "" for a failure that is no status. The client
// turns two statuses into errors of its own, whose message is OpenSSH's.
func hostMessage(err error) string {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "No such file"
	case errors.Is(err, os.ErrPermission):
		return "Permission denied"
	}
	s, ok := errors.AsType[*sftp.StatusError](err)
	if !ok {
		return ""
	}
	// StatusError keeps the Host's message to itself, quoted in its text:
	// sftp: "<message>" (<code>).
	rest, _ := strings.CutPrefix(s.Error(), "sftp: ")
	if quoted, err := strconv.QuotedPrefix(rest); err == nil {
		if message, err := strconv.Unquote(quoted); err == nil && message != "" {
			return message
		}
	}
	return "the Host refused the request"
}
