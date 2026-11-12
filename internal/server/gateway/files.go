package gateway

import (
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
	f    *terminalFiles
	log  *slog.Logger
}

// The direction and the outcome of a file transferred, as its log line
// names them.
const (
	Upload   = "upload"
	Download = "download"

	TransferDone      = "done"
	TransferCancelled = "cancelled"
	TransferFailed    = "failed"
)

// Transferred logs that a file moved, once its transfer ended: its
// direction, Upload or Download, its absolute path on the Host (a
// download's as asked, an upload's final one), the bytes moved, and the
// outcome, with err for TransferFailed. The line names the SSH Session as
// its other lines do, and holds no token and no content.
func (f *Files) Transferred(direction, path string, bytes int64, outcome string, err error) {
	t := f.t
	attrs := slices.Concat(t.named(), []any{"user", t.user}, callerAttrs(t.caller), []any{
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

// Files returns the files of the web terminal's SSH Session id of the caller
// c, opening its SFTP client on the first call, or why not: unknown_tunnel
// for an id that names no live web terminal SSH Session of c's,
// ssh_session_ended for one that has ended, as the Gateway still knows,
// sftp_unavailable for a Host that refuses the sftp subsystem, and
// host_refused for one that refuses its channel. It works while the
// WebSocket is Detached.
func (g *Gateway) Files(c TerminalCaller, id [wire.TunnelIDSize]byte) (*Files, error) {
	key := callerKey{id: c.Caller.ID(), run: c.Run}
	g.mu.Lock()
	t := g.ownLocked(tunnelID(id), key, clientBrowser)
	var tf *terminalFiles
	if t != nil {
		tf = t.files
	}
	e, kept := g.ended[tunnelID(id)]
	kept = kept && e.key == key && e.client == clientBrowser && time.Now().Before(e.until)
	g.mu.Unlock()
	switch {
	case t != nil && tf != nil:
	case kept:
		return nil, api.CodeSSHSessionEnded.Errorf("this SSH Session has ended, and with it its file transfers")
	default:
		return nil, api.CodeUnknownTunnel.Errorf("this browser has no open SSH Session with this id")
	}
	client, err := g.sftpOf(t, tf)
	if err != nil {
		return nil, err
	}
	return &Files{SFTP: client, t: t, f: tf, log: g.log}, nil
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
	client, err := sftp.NewClientPipe(ch, ch)
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
