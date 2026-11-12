package sshhost

import (
	"encoding/binary"
	"io"
	"sync"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SFTP lets a session ask for the "sftp" subsystem, which the Host serves
// in the test process with github.com/pkg/sftp's server, on the real files
// of the test's machine, starting in the Host Account's home directory, as
// OpenSSH's sftp-server does.
func SFTP() Option {
	return func(s *settings) { s.sftp = true }
}

// SFTPFailingWrites serves the "sftp" subsystem as SFTP does, but answers
// every write with SSH_FX_FAILURE, as sftp-server answers one on a full
// disk; a file is still created, read, renamed and removed.
func SFTPFailingWrites() Option {
	return func(s *settings) { s.sftp, s.sftpFailingWrites = true, true }
}

// serveSFTP serves SFTP on the session channel ch in the test process, in
// the Host Account's home directory, until the client closes it or the
// connection ends, then sends the exit status 0 and closes the channel, as
// sftp-server's end does.
func (h *Host) serveSFTP(ch ssh.Channel) {
	defer func() { _ = ch.Close() }()
	var rwc io.ReadWriteCloser = ch
	if h.settings.sftpFailingWrites {
		rwc = h.refuseWrites(ch)
	}
	s, err := sftp.NewServer(rwc, sftp.WithServerWorkingDirectory(h.home))
	if err != nil {
		h.t.Errorf("the in-process Host cannot serve SFTP: %v", err)
		return
	}
	_ = s.Serve() // ends with the channel, as the session does
	sendExitStatus(ch, 0)
}

// SFTP's packet types and status code that writeRefuser reads and sends
// (draft-ietf-secsh-filexfer-02).
const (
	fxpWrite   = 6
	fxpStatus  = 101
	fxFailure  = 4
	maxPacket  = 1 << 20
	packetHead = 4
)

// writeRefuser stands between the SFTP client on ch and the server: it
// answers each write itself with SSH_FX_FAILURE and passes every other
// packet on, through a pipe the server reads. What the server writes goes
// to ch a whole packet at a time, so its answers and the refusals never
// interleave.
type writeRefuser struct {
	ch ssh.Channel
	r  *io.PipeReader

	mu  sync.Mutex
	out []byte
}

// refuseWrites returns the writeRefuser of ch, passing the client's packets
// on until ch ends.
func (h *Host) refuseWrites(ch ssh.Channel) *writeRefuser {
	pr, pw := io.Pipe()
	w := &writeRefuser{ch: ch, r: pr}
	h.wg.Go(func() { pw.CloseWithError(w.pass(pw)) })
	return w
}

// pass reads the client's packets off ch, refusing each write and writing
// every other packet into pw, until ch fails.
func (w *writeRefuser) pass(pw *io.PipeWriter) error {
	for {
		var head [packetHead]byte
		if _, err := io.ReadFull(w.ch, head[:]); err != nil {
			return err
		}
		n := binary.BigEndian.Uint32(head[:])
		if n > maxPacket {
			return io.ErrShortBuffer
		}
		packet := make([]byte, n)
		if _, err := io.ReadFull(w.ch, packet); err != nil {
			return err
		}
		if len(packet) >= 5 && packet[0] == fxpWrite {
			// The status answers the write's request id, its bytes 1 to 4.
			status := ssh.Marshal(struct {
				Type      byte
				ID, Code  uint32
				Msg, Lang string
			}{fxpStatus, binary.BigEndian.Uint32(packet[1:5]), fxFailure, "Failure", ""})
			if _, err := w.send(binary.BigEndian.AppendUint32(nil, uint32(len(status))), status); err != nil {
				return err
			}
			continue
		}
		if _, err := pw.Write(append(head[:], packet...)); err != nil {
			return err
		}
	}
}

// send writes parts to ch as one packet, in turn with the server's.
func (w *writeRefuser) send(parts ...[]byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var b []byte
	for _, p := range parts {
		b = append(b, p...)
	}
	return w.ch.Write(b)
}

func (w *writeRefuser) Read(p []byte) (int, error) {
	return w.r.Read(p)
}

// Write takes what the server writes, and sends each packet once it is
// whole.
func (w *writeRefuser) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.out = append(w.out, p...)
	for len(w.out) >= packetHead {
		n := packetHead + int(binary.BigEndian.Uint32(w.out))
		if len(w.out) < n {
			break
		}
		if _, err := w.ch.Write(w.out[:n]); err != nil {
			return 0, err
		}
		w.out = w.out[n:]
	}
	return len(p), nil
}

func (w *writeRefuser) Close() error {
	_ = w.r.Close()
	return w.ch.Close()
}
