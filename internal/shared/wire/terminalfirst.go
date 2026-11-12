package wire

import (
	"encoding/hex"
	"errors"
	"fmt"
)

// TerminalMaxFirst is the most bytes a web terminal's first message holds.
const TerminalMaxFirst = 4 << 10

// TerminalFirst is a web terminal's first message: one text message of JSON
// that the page sends once the WebSocket is upgraded, naming the terminal
// protocol it speaks (TerminalProtocol) and asking for exactly one thing:
// to Open an SSH Session to a Host at a size, to Resume one from the offset
// the page has received of its stream, or to End one (P1 spec: a terminal
// in the web UI, "The first message").
type TerminalFirst struct {
	Protocol int    `json:"protocol"`
	Open     string `json:"open,omitzero"`
	Cols     uint32 `json:"cols,omitzero"`
	Rows     uint32 `json:"rows,omitzero"`
	Resume   string `json:"resume,omitzero"`
	Offset   uint64 `json:"offset,omitzero"`
	End      string `json:"end,omitzero"`
}

// ErrTerminalFirst is the error for a first message that asks for no one
// thing, or for it in a form the protocol does not take.
var ErrTerminalFirst = errors.New("malformed first message")

// Check refuses a first message of this protocol that asks for nothing, for
// more than one thing, for an Open at a size out of range, or names an SSH
// Session by anything but its id.
func (f TerminalFirst) Check() error {
	asked := 0
	for _, s := range []string{f.Open, f.Resume, f.End} {
		if s != "" {
			asked++
		}
	}
	switch {
	case asked != 1:
		return fmt.Errorf("%w: it asks for %d of open, resume and end, not one", ErrTerminalFirst, asked)
	case f.Open != "":
		if f.Cols < 1 || f.Cols > TerminalMaxSide || f.Rows < 1 || f.Rows > TerminalMaxSide {
			return fmt.Errorf("%w: an open at %dx%d, each side being 1 to %d", ErrTerminalFirst, f.Cols, f.Rows, TerminalMaxSide)
		}
	case f.Resume != "":
		_, err := ParseTerminalID(f.Resume)
		return err
	default:
		_, err := ParseTerminalID(f.End)
		return err
	}
	return nil
}

// ParseTerminalID reads an SSH Session's id as a first message names it:
// 32 lowercase hex characters, as logs write a Tunnel's id.
func ParseTerminalID(s string) ([TunnelIDSize]byte, error) {
	var id [TunnelIDSize]byte
	if len(s) != hex.EncodedLen(TunnelIDSize) {
		return id, fmt.Errorf("%w: an SSH Session id of %d characters, not %d", ErrTerminalFirst, len(s), hex.EncodedLen(TunnelIDSize))
	}
	if _, err := hex.Decode(id[:], []byte(s)); err != nil || hex.EncodeToString(id[:]) != s {
		return id, fmt.Errorf("%w: an SSH Session id that is not lowercase hex", ErrTerminalFirst)
	}
	return id, nil
}

// TerminalAnswer is the answer to a first message: one text message of JSON
// in one of four shapes. An Open is answered with the new SSH Session's id
// (Tunnel), a Resume with the received offset of the koko server's end
// (Offset), an End with Ended, and any of them may be Refused with a code
// and a Message, perhaps empty, for the person. After a refusal the
// WebSocket closes with status 1008 (policy violation).
type TerminalAnswer struct {
	Tunnel  string  `json:"tunnel,omitzero"`
	Offset  *uint64 `json:"offset,omitzero"`
	Ended   bool    `json:"ended,omitzero"`
	Refused string  `json:"refused,omitzero"`
	Message *string `json:"message,omitzero"`
}

// TerminalRefusal is the answer that refuses a first message with code,
// carrying message, even when it is empty.
func TerminalRefusal(code, message string) TerminalAnswer {
	return TerminalAnswer{Refused: code, Message: &message}
}

// The codes a refused first message carries, besides the API's
// signin_required, cookie_required and revoked, which the web front answers
// with before the Gateway sees the message. The page tests them; the
// Message is what it shows.
const (
	// TerminalReloadPage: the message named another protocol than this
	// koko server's, so the page must be reloaded.
	TerminalReloadPage = "reload_page"
	// TerminalUnknownHost: no Host the caller can see has the name.
	TerminalUnknownHost = "unknown_host"
	// TerminalHostRegistered: the Host is Registered.
	TerminalHostRegistered = "host_registered"
	// TerminalRelayRegistered: the Host's Relay is not Managed.
	TerminalRelayRegistered = "relay_registered"
	// TerminalAccessRefused: the access check refused the caller.
	TerminalAccessRefused = "access_refused"
	// TerminalSSHSessionLimit: the caller holds the most SSH Sessions koko
	// allows it.
	TerminalSSHSessionLimit = "ssh_session_limit"
	// TerminalHostUnreachable: the Host, or its Relay, is down.
	TerminalHostUnreachable = "host_unreachable"
	// TerminalHostKeyMismatch: the Host's or its Relay's host key is not the
	// pinned one.
	TerminalHostKeyMismatch = "host_key_mismatch"
	// TerminalLoginRefused: the Host, or its Relay, refused the koko key.
	TerminalLoginRefused = "login_refused"
	// TerminalHostConnectionFailed: the Host connector failed otherwise.
	TerminalHostConnectionFailed = "host_connection_failed"
	// TerminalShellRefused: the Host refused the session, its pty or its
	// shell.
	TerminalShellRefused = "shell_refused"
	// TerminalResumeRefused: the koko server holds no SSH Session of the
	// caller's by the id, or it has ended, as the Message says when the
	// koko server still knows why.
	TerminalResumeRefused = "resume_refused"
)
