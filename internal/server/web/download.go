package web

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/gateway"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/wire"
)

// A download (files.go) goes in two steps, so that the file's path never
// enters a URL, and so never the reverse proxy's log, and a cross-site page
// gets nothing from a GET: the page posts the path and gets a token, then
// the browser GETs the token, as a download link, and saves the bytes with
// its own progress and Cancel, so a large file never sits in the page's
// memory. Only regular files are taken, links followed, so a FIFO or
// /dev/zero never streams for ever.

// downloadLifetime is how long a download's token stays good, on the koko
// server's clock: the page follows it at once.
const downloadLifetime = 60 * time.Second

// downloadBuffer is how much of a download is read from the Host at once:
// pkg/sftp reads a large buffer in concurrent requests.
const downloadBuffer = 256 << 10

// downloads are the download tokens the web front has given and not yet
// seen spent, by the SHA-256 of each, so the tokens themselves are kept
// nowhere, and the downloads streaming, which a change that ends their
// caller cuts (cutEnded).
type downloads struct {
	mu        sync.Mutex
	byHash    map[[sha256.Size]byte]download
	streaming map[*streaming]struct{}
}

func newDownloads() *downloads {
	return &downloads{byHash: map[[sha256.Size]byte]download{}, streaming: map[*streaming]struct{}{}}
}

// streaming is a download streaming for its caller, and how to cut it.
type streaming struct {
	caller op.Caller
	cut    context.CancelCauseFunc
}

// errCallerEnded cuts a download whose caller a change ended, as its log
// line says.
var errCallerEnded = errors.New("its caller has ended, and the download with it")

// stream keeps the download s streaming until the call it returns.
func (ds *downloads) stream(s *streaming) (done func()) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.streaming[s] = struct{}{}
	return func() {
		ds.mu.Lock()
		defer ds.mu.Unlock()
		delete(ds.streaming, s)
	}
}

// cutEnded cuts every download streaming for a caller the committed change
// c ended: a Device Session it Revoked, a Sign-in it ended, and when it
// changed the Web URL, every Sign-in's, whose page is signed out. A token
// not yet spent needs no cut: spending it reads the caller.
func (ds *downloads) cutEnded(c access.Change) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	for s := range ds.streaming {
		if slices.Contains(c.Revoked, s.caller.DeviceSession) || slices.Contains(c.SignIns, s.caller.SignIn) ||
			c.WebURL && s.caller.SignIn != "" {
			s.cut(errCallerEnded)
		}
	}
}

// download is what a token is good for: the file path names on the Host
// of the web terminal's SSH Session tunnel, for the caller key, until
// expires.
type download struct {
	key     pageKey
	tunnel  [wire.TunnelIDSize]byte
	path    string
	expires time.Time
}

// give keeps d under a new token, which it returns, forgetting the tokens
// past their time at now.
func (ds *downloads) give(d download, now time.Time) string {
	token := randomHex(16)
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.forgetLocked(now)
	ds.byHash[sha256.Sum256([]byte(token))] = d
	return token
}

// spend returns what token is good for, when it is the caller key's and
// still good at now, and forgets it: a token is good once. Another
// caller's attempt spends nothing.
func (ds *downloads) spend(token string, key pageKey, now time.Time) (download, bool) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.forgetLocked(now)
	h := sha256.Sum256([]byte(token))
	d, ok := ds.byHash[h]
	if !ok || d.key != key {
		return download{}, false
	}
	delete(ds.byHash, h)
	return d, true
}

func (ds *downloads) forgetLocked(now time.Time) {
	maps.DeleteFunc(ds.byHash, func(_ [sha256.Size]byte, d download) bool { return !now.Before(d.expires) })
}

// filesDownload gives a token for the regular file req names on the Host,
// links followed, which a GET of api.FilesDownloadPath/<token> downloads.
func (f *Front) filesDownload(c gateway.TerminalCaller, req api.FilesDownloadRequest) (api.FilesDownloadResponse, error) {
	if !path.IsAbs(req.Path) {
		return api.FilesDownloadResponse{}, api.CodeInvalidRequest.Errorf("path must be absolute")
	}
	files, err := f.hostFiles(c, req.Tunnel)
	if err != nil {
		return api.FilesDownloadResponse{}, err
	}
	fi, err := regularFile(files, req.Path)
	if err != nil {
		return api.FilesDownloadResponse{}, err
	}
	id, _ := wire.ParseTerminalID(req.Tunnel) // hostFiles parsed it
	now := f.clock()
	token := f.downloads.give(download{key: keyOf(c), tunnel: id, path: req.Path, expires: now.Add(downloadLifetime)}, now)
	return api.FilesDownloadResponse{Token: token, Name: path.Base(req.Path), Size: fi.Size()}, nil
}

// regularFile returns the file p names on the Host, links followed, when
// it is a regular file, and not_a_file otherwise.
func regularFile(files *gateway.Files, p string) (fs.FileInfo, error) {
	fi, err := files.SFTP.Stat(p)
	if err != nil {
		return nil, hostError(files, err)
	}
	if !fi.Mode().IsRegular() {
		return nil, notAFile(p)
	}
	return fi, nil
}

// notAFile refuses the download of p, which is no regular file.
func notAFile(p string) error {
	return api.CodeNotAFile.Errorf("%s is not a regular file, which alone koko downloads", p)
}

// serveDownload answers a GET of api.FilesDownloadPath/<token>, which came
// in by way: the caller as for every files call, then the token, spent
// once it is found the caller's and good, then the file's bytes, streamed
// as an attachment with its length. A failure before the bytes is the
// error object; one while they stream cuts the answer short, which the
// browser shows as a failed download, as does a change that ends the
// caller (cutEnded). The token reaches no log line (loggedPath).
func (f *Front) serveDownload(w http.ResponseWriter, r *http.Request, way terminalWay, token string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		f.refuseAPI(w, r, api.CodeMethodNotAllowed.Errorf("%s is not allowed: a download takes a GET", r.Method))
		return
	}
	c, refused := way.caller(r.Context())
	if refused != nil {
		f.refuseAPI(w, r, refusalError(*refused))
		return
	}
	// Among the downloads streaming from the caller's read on, so that a
	// change that ends the caller from then on cuts it, the Gateway's read
	// in Files being the read after: no window between the two lets one
	// through.
	ctx, cut := context.WithCancelCause(r.Context())
	defer cut(nil)
	defer f.downloads.stream(&streaming{caller: c.Caller, cut: cut})()
	d, ok := f.downloads.spend(token, keyOf(c), f.clock())
	if !ok {
		f.refuseAPI(w, r, api.CodeUnknownDownload.Errorf("this download link has expired, was used already, or is not this browser's"))
		return
	}
	files, err := f.gateway.Files(c, d.tunnel)
	if err != nil {
		f.refuseAPI(w, r, err)
		return
	}
	// Checked again before the open, which on a FIFO would wait for a
	// writer and hold the SFTP channel every call shares.
	if _, err := regularFile(files, d.path); err != nil {
		f.refuseAPI(w, r, err)
		return
	}
	file, err := files.SFTP.Open(d.path)
	if err != nil {
		f.refuseAPI(w, r, hostError(files, err))
		return
	}
	defer func() { _ = file.Close() }()
	fi, err := file.Stat()
	if err == nil && !fi.Mode().IsRegular() {
		err = notAFile(d.path)
	}
	switch {
	case errors.Is(context.Cause(ctx), errCallerEnded):
		// Cut before a byte went: refused as the caller now is.
		f.refuseAPI(w, r, f.endedCaller(r, way))
		return
	case err != nil:
		f.refuseAPI(w, r, hostError(files, err))
		return
	}
	// From here a cut closes the file, which fails its read, and fails a
	// write to a browser that reads nothing, which would hold it otherwise;
	// before, the refusal above answers it.
	rc := http.NewResponseController(w)
	defer context.AfterFunc(ctx, func() {
		if errors.Is(context.Cause(ctx), errCallerEnded) {
			_ = file.Close()
			_ = rc.SetWriteDeadline(time.Now())
		}
	})()
	size := fi.Size()
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", attachment(path.Base(d.path)))
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
	moved, outcome, err := copyDownload(w, file, size)
	if outcome == gateway.TransferDone {
		// The last bytes wait in the answer's buffer: done only once they
		// went, which a cut may stop.
		if err := rc.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
			outcome = gateway.TransferCancelled
		}
	}
	switch ended := files.Err(); {
	case outcome != gateway.TransferDone && errors.Is(context.Cause(ctx), errCallerEnded):
		outcome, err = gateway.TransferFailed, errCallerEnded
	case outcome == gateway.TransferFailed && ended != nil:
		err = ended
	}
	files.Transferred(gateway.Download, d.path, moved, outcome, err)
}

// endedCaller is the refusal of a download whose caller ended before its
// first byte: the caller's own refusal, read again, or, for a page on the
// web listener whose Sign-in lives on at an old Web URL, signin_required.
func (f *Front) endedCaller(r *http.Request, way terminalWay) error {
	if _, refused := way.caller(r.Context()); refused != nil {
		return refusalError(*refused)
	}
	return api.CodeSignInRequired.Errorf("this page's Sign-in no longer reaches this koko server: sign in again")
}

// copyDownload copies size bytes of file to w, and returns how many it
// moved and how it ended: done, cancelled when w failed, as when the
// browser's Cancel closed the request, or failed with why when the file
// did, or ended early.
func copyDownload(w io.Writer, file io.Reader, size int64) (int64, gateway.Outcome, error) {
	buf := make([]byte, min(size, downloadBuffer))
	var moved int64
	for moved < size {
		n, err := file.Read(buf[:min(int64(len(buf)), size-moved)])
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				return moved, gateway.TransferCancelled, nil
			}
			moved += int64(n)
		}
		switch {
		case errors.Is(err, io.EOF) && moved < size:
			return moved, gateway.TransferFailed, fmt.Errorf("the file ended at %d of its %d bytes", moved, size)
		case err != nil && !errors.Is(err, io.EOF):
			return moved, gateway.TransferFailed, err
		}
	}
	return moved, gateway.TransferDone, nil
}

// attachment is the Content-Disposition of a download named name (RFC
// 6266): filename* carries the name in UTF-8 (RFC 8187), and filename an
// ASCII stand-in for a browser that reads no filename*, each character
// outside printable ASCII, a quote or a backslash replaced by "_".
func attachment(name string) string {
	var ascii, encoded strings.Builder
	for _, r := range name {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			ascii.WriteByte('_')
		} else {
			ascii.WriteRune(r)
		}
	}
	for _, b := range []byte(name) {
		if attrChar(b) {
			encoded.WriteByte(b)
		} else {
			fmt.Fprintf(&encoded, "%%%02X", b)
		}
	}
	return `attachment; filename="` + ascii.String() + `"; filename*=UTF-8''` + encoded.String()
}

// attrChar tells whether b is an attr-char of RFC 8187, which a value of
// filename* carries unencoded.
func attrChar(b byte) bool {
	switch {
	case 'a' <= b && b <= 'z', 'A' <= b && b <= 'Z', '0' <= b && b <= '9':
		return true
	}
	return strings.IndexByte("!#$&+-.^_`|~", b) >= 0
}
