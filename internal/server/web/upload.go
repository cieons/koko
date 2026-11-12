package web

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"

	"github.com/cieons/koko/internal/server/gateway"
	"github.com/cieons/koko/internal/shared/api"
)

// An upload (files.go) writes a file into a directory on the Host in
// chunks, as the page sends them, into a hidden partial file beside its
// name, which the finish renames into place, so the name never holds half
// a file. A chunk the page sends again after a drop is not written twice,
// and a gap is refused. The partial file goes when the page cancels, when
// no chunk comes for timing.UploadIdle, and when the SSH Session ends
// while its Host connection lives (gateway.Files.AtEnd). The uploads are
// held in memory: one that outlives the koko server's process leaves its
// partial file under its hidden name.

// pageKey is whose an upload or a download is: the caller's id
// (op.Caller.ID) and, for the Local caller, the run of koko web.
type pageKey struct {
	id, run string
}

func keyOf(c gateway.TerminalCaller) pageKey {
	return pageKey{id: c.Caller.ID(), run: c.Run}
}

// uploads are every upload the web front holds, by its id.
type uploads struct {
	// idle is how long an upload waits for its next call
	// (timing.UploadIdle).
	idle time.Duration

	mu   sync.Mutex
	byID map[string]*upload
	// running counts, by the SSH Session's id, its uploads still taking
	// chunks, of which it runs at most api.MaxUploads.
	running map[string]int
}

func newUploads(idle time.Duration) *uploads {
	return &uploads{idle: idle, byID: map[string]*upload{}, running: map[string]int{}}
}

// The states of an upload.
const (
	// uploadRunning: it takes chunks.
	uploadRunning = iota
	// uploadDone: its file is in place; a finish sent again is answered
	// the same.
	uploadDone
	// uploadEnded: it ended with its SSH Session, which every later call
	// of it is told.
	uploadEnded
)

// upload is one upload. Its id, owner and file are fixed once it starts;
// mu guards the rest, and is held across a request to the Host, never
// while a page's body is read.
type upload struct {
	id     string
	key    pageKey
	tunnel string
	files  *gateway.Files
	// final is the file's path, partial the hidden one written meanwhile.
	final, partial string
	size           int64
	replace        bool
	all            *uploads

	mu       sync.Mutex
	state    int
	file     *sftp.File
	received int64
	// done is a finished upload's answer, and ended why a call of one
	// that ended with its SSH Session is refused.
	done      api.FilesFinishResponse
	ended     error
	idle      *time.Timer
	stopAtEnd func() bool
}

// filesUpload starts an upload of req into its directory on the Host: it
// refuses a bad name, a name that exists unless req replaces it, and a
// fifth upload of the SSH Session, then creates the partial file
// .<name>.koko-upload-<8 hex>, exclusively, and makes it private before
// any byte is written.
func (f *Front) filesUpload(c gateway.TerminalCaller, req api.FilesUploadRequest) (api.FilesUploadResponse, error) {
	switch {
	case req.Name == "" || req.Name == "." || req.Name == ".." || strings.ContainsAny(req.Name, "/\x00"):
		return api.FilesUploadResponse{}, api.CodeInvalidName.Errorf("a file's name must not be empty, . or .., nor hold /")
	case !path.IsAbs(req.Dir):
		return api.FilesUploadResponse{}, api.CodeInvalidRequest.Errorf("dir must be absolute")
	case req.Size < 0:
		return api.FilesUploadResponse{}, api.CodeInvalidRequest.Errorf("size must not be negative")
	}
	files, err := f.hostFiles(c, req.Tunnel)
	if err != nil {
		return api.FilesUploadResponse{}, err
	}
	if !f.uploads.take(req.Tunnel) {
		return api.FilesUploadResponse{}, api.CodeTooManyUploads.Errorf("this SSH Session already runs %d uploads, the most koko allows at once", api.MaxUploads)
	}
	u := &upload{
		id: randomHex(16), key: keyOf(c), tunnel: req.Tunnel, files: files,
		final:   path.Join(req.Dir, req.Name),
		partial: path.Join(req.Dir, "."+req.Name+".koko-upload-"+randomHex(4)),
		size:    req.Size, replace: req.Replace, all: f.uploads,
	}
	if err := u.create(); err != nil {
		f.uploads.give(req.Tunnel)
		return api.FilesUploadResponse{}, err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	f.uploads.add(u)
	u.idle = time.AfterFunc(u.idleFor(), u.idleOver)
	u.stopAtEnd = files.AtEnd(u.sessionEnded)
	if err := files.Err(); err != nil {
		// Ended before the hook was set, which then never runs.
		u.endLocked(gateway.TransferFailed, err)
		f.uploads.remove(u)
		return api.FilesUploadResponse{}, err
	}
	return api.FilesUploadResponse{Upload: u.id}, nil
}

// create refuses a name that exists unless u replaces it, then creates
// u's partial file, exclusively, private to the Host Account.
func (u *upload) create() error {
	c := u.files.SFTP
	if !u.replace {
		if err := u.refuseExisting(); err != nil {
			return err
		}
	}
	file, err := c.OpenFile(u.partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return hostError(u.files, err)
	}
	// pkg/sftp's open sends no mode: the Host gives the new, empty file its
	// own, which this narrows before any byte is written.
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = c.Remove(u.partial)
		return hostError(u.files, err)
	}
	u.file = file
	return nil
}

// refuseExisting refuses u with file_exists when its name exists.
func (u *upload) refuseExisting() error {
	_, err := u.files.SFTP.Lstat(u.final)
	switch {
	case err == nil:
		dir, name := path.Split(u.final)
		return api.CodeFileExists.Errorf("%s exists in %s", name, path.Clean(dir))
	case errors.Is(err, fs.ErrNotExist):
		return nil
	}
	return hostError(u.files, err)
}

// filesChunk takes one chunk of an upload: the bytes of the request, at
// most api.MaxFilesChunk of application/octet-stream, at the offset its
// query names. The body is read whole before the upload is touched, so a
// page that sends slowly holds nothing of it.
func (f *Front) filesChunk(r *http.Request, c gateway.TerminalCaller, body []byte) (any, error) {
	q := r.URL.Query()
	offset, err := strconv.ParseInt(q.Get("offset"), 10, 64)
	if err != nil || offset < 0 {
		return nil, api.CodeInvalidRequest.Errorf("offset must be a byte count")
	}
	u, err := f.uploads.of(c, q.Get("upload"))
	if err != nil {
		return nil, err
	}
	return u.write(offset, body)
}

// write writes data at offset into u's partial file, and answers how much
// has come: a chunk at an offset already received is answered so and not
// written again; one that would leave a gap, or run past the size, is
// refused.
func (u *upload) write(offset int64, data []byte) (api.FilesChunkResponse, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	switch {
	case u.state == uploadEnded:
		return api.FilesChunkResponse{}, u.ended
	case offset < u.received:
		return api.FilesChunkResponse{Received: u.received}, nil
	case offset > u.received:
		return api.FilesChunkResponse{}, api.CodeBadOffset.Errorf("%d bytes of this upload have come; a chunk at %d would leave a gap", u.received, offset)
	case offset+int64(len(data)) > u.size:
		return api.FilesChunkResponse{}, api.CodeInvalidRequest.Errorf("the chunk runs past the upload's size of %d bytes", u.size)
	}
	u.idle.Reset(u.idleFor())
	if len(data) > 0 {
		if _, err := u.file.WriteAt(data, offset); err != nil {
			// Some of it may be written: the page sends it again, at the
			// same offset.
			return api.FilesChunkResponse{}, hostError(u.files, err)
		}
		u.received += int64(len(data))
	}
	return api.FilesChunkResponse{Received: u.received}, nil
}

// filesFinish finishes an upload once every byte has come: it closes the
// partial file, gives it the mode of the file it replaces, or 0644, and
// renames it onto its name, with posix-rename@openssh.com where the Host
// offers it, so a replace is one step, or else by removing the old file
// first. A finish sent again is answered the same.
func (f *Front) filesFinish(c gateway.TerminalCaller, req api.FilesFinishRequest) (api.FilesFinishResponse, error) {
	u, err := f.uploads.of(c, req.Upload)
	if err != nil {
		return api.FilesFinishResponse{}, err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	switch {
	case u.state == uploadDone:
		return u.done, nil
	case u.state == uploadEnded:
		return api.FilesFinishResponse{}, u.ended
	case u.received != u.size:
		return api.FilesFinishResponse{}, api.CodeBadOffset.Errorf("%d of this upload's %d bytes have come", u.received, u.size)
	}
	u.idle.Reset(u.idleFor())
	if err := u.place(); err != nil {
		return api.FilesFinishResponse{}, err
	}
	u.done = api.FilesFinishResponse{Path: u.final, Size: u.size}
	u.state = uploadDone
	u.stopAtEnd()
	u.all.give(u.tunnel)
	u.files.Transferred(gateway.Upload, u.final, u.size, gateway.TransferDone, nil)
	return u.done, nil
}

// place closes u's partial file and renames it onto u's name. A failure
// leaves the upload running, so the page may finish it again or cancel it.
// u.mu is held.
func (u *upload) place() error {
	c := u.files.SFTP
	if u.file != nil {
		err := u.file.Close()
		u.file = nil
		if err != nil {
			return hostError(u.files, err)
		}
	}
	mode := fs.FileMode(0o644)
	if u.replace {
		if old, err := c.Stat(u.final); err == nil {
			mode = old.Mode().Perm()
		}
	} else if err := u.refuseExisting(); err != nil {
		return err
	}
	if err := c.Chmod(u.partial, mode); err != nil {
		return hostError(u.files, err)
	}
	var err error
	switch _, posix := c.HasExtension("posix-rename@openssh.com"); {
	case u.replace && posix:
		err = c.PosixRename(u.partial, u.final)
	case u.replace:
		if err = c.Remove(u.final); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return hostError(u.files, err)
		}
		err = c.Rename(u.partial, u.final)
	default:
		// SFTP's rename does not replace a file, so one made meanwhile
		// is kept.
		err = c.Rename(u.partial, u.final)
	}
	if err != nil {
		return hostError(u.files, err)
	}
	return nil
}

// filesCancel cancels an upload, removing its partial file. Cancelling one
// that is done or ended does nothing more.
func (f *Front) filesCancel(c gateway.TerminalCaller, req api.FilesCancelRequest) (api.FilesCancelResponse, error) {
	u, err := f.uploads.of(c, req.Upload)
	if err != nil {
		return api.FilesCancelResponse{}, err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.state == uploadRunning {
		u.endLocked(gateway.TransferCancelled, nil)
	}
	u.all.remove(u)
	return api.FilesCancelResponse{}, nil
}

// idleOver gives u up once no call of it came for timing.UploadIdle,
// removing its partial file if it still runs, and forgets it.
func (u *upload) idleOver() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.state == uploadRunning {
		u.endLocked(gateway.TransferFailed, fmt.Errorf("no chunk came for %v", u.idleFor()))
	}
	u.all.remove(u)
}

// sessionEnded ends u with its SSH Session, while the Host connection
// lives, removing its partial file if it still runs; every later call of
// it is told the SSH Session ended, until it is forgotten.
func (u *upload) sessionEnded() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.state != uploadRunning {
		return
	}
	u.endLocked(gateway.TransferFailed, errors.New("the SSH Session ended"))
	u.state, u.ended = uploadEnded, u.files.Err()
}

// endLocked ends u, which runs, as outcome, with err for a failure: it
// closes and removes the partial file, logs the transfer and frees its
// place among the SSH Session's uploads. u.mu is held.
func (u *upload) endLocked(outcome string, err error) {
	if u.file != nil {
		_ = u.file.Close()
		u.file = nil
	}
	// The Host connection may be gone, which leaves the file under its
	// hidden name, never its real one.
	_ = u.files.SFTP.Remove(u.partial)
	u.state, u.ended = uploadEnded, api.CodeUnknownUpload.Errorf("this upload has ended")
	u.stopAtEnd()
	u.all.give(u.tunnel)
	u.files.Transferred(gateway.Upload, u.final, u.received, outcome, err)
}

// idleFor is how long u waits for its next call.
func (u *upload) idleFor() time.Duration {
	return u.all.idle
}

// take takes one of the SSH Session tunnel's places for a running upload,
// and says whether one was free.
func (us *uploads) take(tunnel string) bool {
	us.mu.Lock()
	defer us.mu.Unlock()
	if us.running[tunnel] >= api.MaxUploads {
		return false
	}
	us.running[tunnel]++
	return true
}

// give gives back a place take took.
func (us *uploads) give(tunnel string) {
	us.mu.Lock()
	defer us.mu.Unlock()
	if us.running[tunnel]--; us.running[tunnel] <= 0 {
		delete(us.running, tunnel)
	}
}

func (us *uploads) add(u *upload) {
	us.mu.Lock()
	defer us.mu.Unlock()
	us.byID[u.id] = u
}

func (us *uploads) remove(u *upload) {
	us.mu.Lock()
	defer us.mu.Unlock()
	if us.byID[u.id] == u {
		delete(us.byID, u.id)
	}
	u.idle.Stop()
}

// of returns the upload id names, when it is the caller c's.
func (us *uploads) of(c gateway.TerminalCaller, id string) (*upload, error) {
	us.mu.Lock()
	defer us.mu.Unlock()
	if u := us.byID[id]; u != nil && u.key == keyOf(c) {
		return u, nil
	}
	return nil, api.CodeUnknownUpload.Errorf("this browser has no upload with this id")
}

// readChunk reads a chunk's body: application/octet-stream of at most
// api.MaxFilesChunk bytes.
func readChunk(r *http.Request) ([]byte, error) {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/octet-stream" {
		return nil, api.CodeUnsupportedMediaType.Errorf("a chunk must be application/octet-stream")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, api.MaxFilesChunk+1))
	if err != nil {
		return nil, api.CodeInvalidRequest.Errorf("read the chunk: %v", err)
	}
	if len(body) > api.MaxFilesChunk {
		return nil, api.CodeRequestTooLarge.Errorf("a chunk is at most %d KiB", api.MaxFilesChunk>>10)
	}
	return body, nil
}

// randomHex is n random bytes in lowercase hex.
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // never fails
	return hex.EncodeToString(b)
}
