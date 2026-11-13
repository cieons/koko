package gateway

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/hostconn"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/wire"
)

// A web terminal's files (P1 spec: a terminal in the web UI, "Files over
// SFTP"; ADR 0053) move over SFTP on the SSH Session's own Host connection,
// as the Host Account, so the web front never holds a Host connection: Files
// hands it the SSH Session's SFTP client, one sftp subsystem channel opened
// at the first call and shared by every call after it, whether the
// WebSocket is attached or Detached. Once the SSH Session ends, its files
// end too: the web front's end hooks run while the Host connection lives,
// so that an upload's partial file is removed, then the SFTP client and the
// connection close, which cuts every transfer still running.

// Files is the files of a web terminal's SSH Session on its Host.
type Files struct {
	// SFTP is the SSH Session's SFTP client, shared by every call.
	SFTP *sftp.Client
	t    *tunnel
	// caller is who asked for the files, whom the log names as moving them.
	caller op.Caller
	f      *terminalFiles
	log    *slog.Logger
}

// Direction is which way a file moved, as its log line names it.
type Direction string

// The directions.
const (
	Upload   Direction = "upload"
	Download Direction = "download"
)

// Outcome is how a file's transfer ended, as its log line names it.
type Outcome string

// The outcomes.
const (
	TransferDone      Outcome = "done"
	TransferCancelled Outcome = "cancelled"
	TransferFailed    Outcome = "failed"
)

// Transferred logs that a file moved, once its transfer ended: its
// direction, Upload or Download, its absolute path on the Host (a
// download's as asked, an upload's final one), the bytes moved, and the
// outcome, with err for TransferFailed. The line names the SSH Session as
// its other lines do, and the caller who moved the file, and holds no token
// and no content.
func (f *Files) Transferred(direction Direction, path string, bytes int64, outcome Outcome, err error) {
	t := f.t
	attrs := slices.Concat(t.named(), []any{"user", t.user}, callerAttrs(f.caller), []any{
		"host", t.hostName, "host_id", t.hostID,
		"direction", direction, "path", path, "bytes", bytes, "outcome", outcome,
	})
	if err != nil {
		attrs = append(attrs, "err", err)
	}
	f.log.Info("file transferred", attrs...)
}

// Done is closed once the SSH Session has ended, which ends its files.
func (f *Files) Done() <-chan struct{} {
	return f.f.done
}

// Err returns ssh_session_ended once the SSH Session has ended, and nil
// while it lives.
func (f *Files) Err() error {
	select {
	case <-f.f.done:
		return sessionEnded(f.t.hostName)
	default:
		return nil
	}
}

// AtEnd has fn run once the SSH Session ends, while its Host connection
// still lives, unless stop is called first, which says whether it stopped
// it. An end waits for fn up to timing.StopNotice, then closes the Host
// connection, which fails what fn still waits on. Once the SSH Session has
// ended, fn never runs and stop says false.
func (f *Files) AtEnd(fn func()) (stop func() bool) {
	tf := f.f
	tf.mu.Lock()
	defer tf.mu.Unlock()
	if tf.ended {
		return func() bool { return false }
	}
	id := tf.next
	tf.next++
	tf.atEnd[id] = fn
	return func() bool {
		tf.mu.Lock()
		defer tf.mu.Unlock()
		_, ok := tf.atEnd[id]
		delete(tf.atEnd, id)
		return ok
	}
}

// terminalFiles is the state of a web terminal's files: its Host
// connection, the shared SFTP client once open, and the end hooks.
type terminalFiles struct {
	host *hostconn.Conn
	// wait bounds how long the end waits for its hooks; wg is the Gateway's,
	// which Close waits for.
	wait time.Duration
	wg   *sync.WaitGroup

	// opening is held while the sftp subsystem opens, so one open serves
	// every call that comes meanwhile. The end never takes it: closing the
	// Host connection fails an open that waits on the Host.
	opening sync.Mutex

	mu     sync.Mutex
	client *sftp.Client
	// requested is set once the sftp subsystem was asked for, which the log
	// tells once.
	requested bool
	ended     bool
	done      chan struct{}
	atEnd     map[uint64]func()
	next      uint64
	endOnce   sync.Once
}

func (g *Gateway) newTerminalFiles(host *hostconn.Conn) *terminalFiles {
	return &terminalFiles{host: host, wait: g.timings.StopNotice, wg: &g.wg, done: make(chan struct{}), atEnd: map[uint64]func(){}}
}

// Files returns the files of the web terminal's SSH Session id, for the
// caller c, opening its SFTP client on the first call, or why not: any live
// caller of the SSH Session's User may, from any page (spec #213, "The
// page"). It refuses with unknown_tunnel an id that names no live SSH
// Session of the caller's User's Workspace, with ssh_session_ended one that
// names its tab's last, which has ended, with sftp_unavailable a Host that
// refuses the sftp subsystem, and with host_refused one that refuses its
// channel. It works with no page attached.
func (g *Gateway) Files(c TerminalCaller, id [wire.TunnelIDSize]byte) (*Files, error) {
	u, refused := g.userOf(c)
	if refused != nil {
		return nil, &api.Error{Code: refused.Refused, Message: *refused.Message}
	}
	// A Workspace not held has no SSH Session, and asking makes none.
	var s *termSession
	ended := false
	if w := g.workspaces.held(u.ID); w != nil {
		s, ended = w.sessionOf(tunnelID(id))
	}
	var tf *terminalFiles
	if s != nil {
		g.mu.Lock()
		tf = s.t.files
		g.mu.Unlock()
	}
	switch {
	case tf != nil:
	case ended:
		return nil, api.CodeSSHSessionEnded.Errorf("this SSH Session has ended, and with it its file transfers")
	default:
		return nil, api.CodeUnknownTunnel.Errorf("this User has no open SSH Session with this id")
	}
	client, err := g.sftpOf(s.t, tf)
	if err != nil {
		return nil, err
	}
	return &Files{SFTP: client, t: s.t, caller: c.Caller, f: tf, log: g.log}, nil
}

// sftpOf returns the SFTP client of the web terminal t, whose files are tf,
// opening it once none is open: at the first call, or after the Host ended
// the last one.
func (g *Gateway) sftpOf(t *tunnel, tf *terminalFiles) (*sftp.Client, error) {
	tf.opening.Lock()
	defer tf.opening.Unlock()
	tf.mu.Lock()
	client, ended, requested := tf.client, tf.ended || t.ctx.Err() != nil, tf.requested
	tf.requested = true
	tf.mu.Unlock()
	switch {
	case ended:
		return nil, sessionEnded(t.hostName)
	case client != nil:
		return client, nil
	case !requested:
		g.log.Info("ssh session request", "tunnel", t.id, "client", t.client, "host", t.hostName, "request", "subsystem", "subsystem", "sftp")
	}
	client, err := g.openSFTP(tf.host, t.hostName)
	tf.mu.Lock()
	defer tf.mu.Unlock()
	if tf.ended || t.ctx.Err() != nil {
		if client != nil {
			_ = client.Close()
		}
		return nil, sessionEnded(t.hostName)
	}
	if err != nil {
		return nil, err
	}
	tf.client = client
	g.wg.Go(func() {
		// The Host may end the subsystem, as when its sftp-server exits:
		// the next call opens another.
		_ = client.Wait()
		tf.mu.Lock()
		defer tf.mu.Unlock()
		if tf.client == client {
			tf.client = nil
		}
	})
	return client, nil
}

// openSFTP opens the sftp subsystem on a session channel of the Host
// connection conn to the Host named hostName, and an SFTP client on it.
func (g *Gateway) openSFTP(conn *hostconn.Conn, hostName string) (*sftp.Client, error) {
	ch, reqs, err := conn.OpenChannel("session", nil)
	if e, ok := errors.AsType[*ssh.OpenChannelError](err); ok {
		return nil, api.CodeHostRefused.Errorf("%s refused a session for SFTP: %s", hostName, e.Message)
	}
	if err != nil {
		return nil, fmt.Errorf("open a session channel for SFTP on %s: %w", hostName, err)
	}
	g.wg.Go(func() { ssh.DiscardRequests(reqs) })
	// sftp-server's stderr would otherwise fill the channel's window.
	g.wg.Go(func() { _, _ = io.Copy(io.Discard, ch.Stderr()) })
	ok, err := ch.SendRequest("subsystem", true, ssh.Marshal(struct{ Name string }{"sftp"}))
	switch {
	case err != nil:
		_ = ch.Close()
		return nil, fmt.Errorf("ask %s for the sftp subsystem: %w", hostName, err)
	case !ok:
		_ = ch.Close()
		return nil, api.CodeSFTPUnavailable.Errorf("%s offers no SFTP, which koko moves files through", hostName)
	}
	client, err := sftp.NewClientPipe(ch, &privateCreates{ch: ch})
	if err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("start SFTP on %s: %w", hostName, err)
	}
	return client, nil
}

// sessionEnded is the refusal of a files call, or the cut of a transfer, of
// a web terminal's SSH Session to the Host named hostName that has ended.
func sessionEnded(hostName string) error {
	return api.CodeSSHSessionEnded.Errorf("the SSH Session to %s has ended, and with it its file transfers", hostName)
}

// end ends the web terminal's files once its SSH Session ends, before its
// Host connection closes: it refuses every later call, runs the end hooks
// and waits for them up to wait, then closes the SFTP client. It runs once.
func (tf *terminalFiles) end() {
	tf.endOnce.Do(func() {
		tf.mu.Lock()
		tf.ended = true
		close(tf.done)
		hooks := slices.Collect(maps.Values(tf.atEnd))
		clear(tf.atEnd)
		client := tf.client
		tf.client = nil
		tf.mu.Unlock()

		finished := make(chan struct{}, len(hooks))
		for _, fn := range hooks {
			tf.wg.Go(func() {
				defer func() { finished <- struct{}{} }()
				fn()
			})
		}
		late := time.NewTimer(tf.wait)
		defer late.Stop()
	wait:
		for range hooks {
			select {
			case <-finished:
			case <-late.C:
				break wait
			}
		}
		if client != nil {
			_ = client.Close()
		}
	})
}

// privateCreates is the channel ch as the SFTP client writes to it: it gives
// each exclusive create, an SSH_FXP_OPEN with SSH_FXF_CREAT and SSH_FXF_EXCL
// that sends no attributes, the permissions 0600, so the Host makes the
// file private as it creates it and no other account can open it first.
// pkg/sftp's open sends no attributes, and koko creates exclusively only an
// upload's partial file. Every other packet passes as it is, whole.
type privateCreates struct {
	ch io.WriteCloser

	mu  sync.Mutex
	buf []byte
}

// The SFTP values privateCreates reads and writes
// (draft-ietf-secsh-filexfer-02).
const (
	fxpOpen        = 3
	fxfCreatExcl   = 0x08 | 0x20
	attrPermission = 0x04
	privateMode    = 0o600
)

// Write takes what the SFTP client writes, a packet in one or more parts,
// and sends each packet once it is whole.
func (p *privateCreates) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buf = append(p.buf, b...)
	for len(p.buf) >= 4 {
		n := 4 + int(binary.BigEndian.Uint32(p.buf))
		if len(p.buf) < n {
			break
		}
		if _, err := p.ch.Write(privateCreate(p.buf[:n])); err != nil {
			return 0, err
		}
		p.buf = p.buf[n:]
	}
	return len(b), nil
}

func (p *privateCreates) Close() error {
	return p.ch.Close()
}

// privateCreate returns the packet pkt, its length first, with the
// permissions 0600 added when it is an exclusive create without
// attributes: uint32 length, byte type, uint32 id, string path, uint32
// flags, then the attributes' uint32 flags, 0 for none.
func privateCreate(pkt []byte) []byte {
	if len(pkt) < 13 || pkt[4] != fxpOpen {
		return pkt
	}
	end := 13 + int(binary.BigEndian.Uint32(pkt[9:13]))
	if end < 13 || len(pkt) != end+8 ||
		binary.BigEndian.Uint32(pkt[end:])&fxfCreatExcl != fxfCreatExcl || binary.BigEndian.Uint32(pkt[end+4:]) != 0 {
		return pkt
	}
	out := binary.BigEndian.AppendUint32(slices.Clone(pkt[:end+4]), attrPermission)
	out = binary.BigEndian.AppendUint32(out, privateMode)
	binary.BigEndian.PutUint32(out, uint32(len(out)-4))
	return out
}
