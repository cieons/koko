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
// to Attach the page to a Workspace's SSH Session, with the page's box in
// cells, or to Resume one of its attachments from the offset the page has
// received of its stream (spec #213, "The terminal socket and attaching").
// A tab is opened and closed on the Workspace socket, so protocol 1's open
// and end are no longer asked for here.
type TerminalFirst struct {
	Protocol int    `json:"protocol"`
	Attach   string `json:"attach,omitzero"`
	Cols     uint32 `json:"cols,omitzero"`
	Rows     uint32 `json:"rows,omitzero"`
	Resume   string `json:"resume,omitzero"`
	Offset   uint64 `json:"offset,omitzero"`
}

// ErrTerminalFirst is the error for a first message that asks for no one
// thing, or for it in a form the protocol does not take.
var ErrTerminalFirst = errors.New("malformed first message")

// Check refuses a first message of this protocol that asks for nothing, for
// both things, for an attach at a size out of range, or names an SSH
// Session or an attachment by anything but its id.
func (f TerminalFirst) Check() error {
	switch {
	case (f.Attach == "") == (f.Resume == ""):
		return fmt.Errorf("%w: it asks for neither or both of attach and resume", ErrTerminalFirst)
	case f.Attach != "":
		if f.Cols < 1 || f.Cols > TerminalMaxSide || f.Rows < 1 || f.Rows > TerminalMaxSide {
			return fmt.Errorf("%w: an attach at %dx%d, each side being 1 to %d", ErrTerminalFirst, f.Cols, f.Rows, TerminalMaxSide)
		}
		_, err := ParseTerminalID(f.Attach)
		return err
	default:
		_, err := ParseTerminalID(f.Resume)
		return err
	}
}

// ParseTerminalID reads an id as the web terminal's messages name an SSH
// Session, an attachment or a tab: 32 lowercase hex characters, as logs
// write a Tunnel's id.
func ParseTerminalID(s string) ([TunnelIDSize]byte, error) {
	var id [TunnelIDSize]byte
	if len(s) != hex.EncodedLen(TunnelIDSize) {
		return id, fmt.Errorf("%w: an id of %d characters, not %d", ErrTerminalFirst, len(s), hex.EncodedLen(TunnelIDSize))
	}
	if _, err := hex.Decode(id[:], []byte(s)); err != nil || hex.EncodeToString(id[:]) != s {
		return id, fmt.Errorf("%w: an id that is not lowercase hex", ErrTerminalFirst)
	}
	return id, nil
}

// TerminalAnswer is the answer to a first message: one text message of JSON
// in one of three shapes. An attach is answered with the new attachment's
// id (Attachment), a resume with the received offset of the koko server's
// end (Offset), and either may be Refused with a code and a Message,
// perhaps empty, for the person. After a refusal the WebSocket closes with
// status 1008 (policy violation).
type TerminalAnswer struct {
	Attachment string  `json:"attachment,omitzero"`
	Offset     *uint64 `json:"offset,omitzero"`
	Refused    string  `json:"refused,omitzero"`
	Message    *string `json:"message,omitzero"`
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
	// TerminalSSHSessionLimit: the Workspace holds the most tabs koko
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
	// TerminalResumeRefused: the koko server holds no attachment of the
	// caller's by the id, or its SSH Session has ended, as the Message says
	// when the koko server still knows why.
	TerminalResumeRefused = "resume_refused"
	// TerminalUnknownTunnel: an attach named no live SSH Session of the
	// caller's User.
	TerminalUnknownTunnel = "unknown_tunnel"
	// TerminalSSHSessionEnded: an attach named an SSH Session of the
	// caller's User that has ended.
	TerminalSSHSessionEnded = "ssh_session_ended"
)
