package wire

import (
	"errors"
	"fmt"
)

// The Workspace socket carries a User's web terminal tabs to each page of
// theirs, and the page's requests to open, reopen and close them (spec
// #213, "The Workspace socket"). Every message either way is one text
// message of JSON. The page's first is {"protocol": 2}, TerminalProtocol,
// answered with the Workspace, or refused as a terminal socket's first
// message is (TerminalRefusal).

// WorkspaceMaxMessage is the most bytes one message from the page holds.
const WorkspaceMaxMessage = 4 << 10

// WorkspaceFirst is the page's first message on the Workspace socket.
type WorkspaceFirst struct {
	Protocol int `json:"protocol"`
}

// WorkspaceRequest is a message from the page after the first: a request
// to open a tab to a Host, to open a new SSH Session in an ended tab, or to
// close a tab, each numbered by the page, which the answer carries back;
// or nothing, {}, which only says the page lives. Open and Reopen carry the
// page's box in cells, the new pty's size.
type WorkspaceRequest struct {
	Request uint64 `json:"request,omitzero"`
	Open    string `json:"open,omitzero"`
	Reopen  string `json:"reopen,omitzero"`
	Close   string `json:"close,omitzero"`
	Cols    uint32 `json:"cols,omitzero"`
	Rows    uint32 `json:"rows,omitzero"`
}

// ErrWorkspaceRequest is the error for a message from the page that is no
// Workspace request.
var ErrWorkspaceRequest = errors.New("malformed Workspace request")

// Check refuses a request that asks for none or several of open, reopen
// and close, carries no request number, opens at a size out of range, or
// names a tab by anything but its id. The liveness message, which asks for
// nothing and carries no number, passes.
func (r WorkspaceRequest) Check() error {
	asked := 0
	for _, s := range []string{r.Open, r.Reopen, r.Close} {
		if s != "" {
			asked++
		}
	}
	switch {
	case asked == 0 && r == WorkspaceRequest{}:
		return nil
	case asked != 1:
		return fmt.Errorf("%w: a Workspace request asks for %d of open, reopen and close, not one", ErrWorkspaceRequest, asked)
	case r.Request == 0:
		return fmt.Errorf("%w: a Workspace request without its number", ErrWorkspaceRequest)
	case r.Close != "":
		if r.Cols != 0 || r.Rows != 0 {
			return fmt.Errorf("%w: a close with a size", ErrWorkspaceRequest)
		}
		return workspaceID(r.Close)
	}
	if r.Cols < 1 || r.Cols > TerminalMaxSide || r.Rows < 1 || r.Rows > TerminalMaxSide {
		return fmt.Errorf("%w: an open at %dx%d, each side being 1 to %d", ErrWorkspaceRequest, r.Cols, r.Rows, TerminalMaxSide)
	}
	if r.Reopen != "" {
		return workspaceID(r.Reopen)
	}
	return nil
}

// workspaceID refuses a tab's id that is not one.
func workspaceID(s string) error {
	if _, err := ParseTerminalID(s); err != nil {
		return fmt.Errorf("%w: %w", ErrWorkspaceRequest, err)
	}
	return nil
}

// Workspace is the whole Workspace, as the koko server sends it first and
// again after every change: its tabs in order.
type Workspace struct {
	Tabs []WorkspaceTab `json:"tabs"`
}

// The states of a tab.
const (
	// TabStateStarting: its SSH Session is being admitted, dialled and its shell
	// started.
	TabStateStarting = "starting"
	// TabStateRunning: its SSH Session runs, and Session names it.
	TabStateRunning = "running"
	// TabStateEnded: its SSH Session ended, as Ended says.
	TabStateEnded = "ended"
	// TabStateRefused: its SSH Session did not start, as Refused says.
	TabStateRefused = "refused"
	// TabStateRestarted: the koko server's restart ended its SSH Session, and no
	// new one has been asked for.
	TabStateRestarted = "restarted"
)

// WorkspaceTab is one tab: its id, its Host's name now (or its label, for
// a Host the User can no longer see), its label, and its state.
type WorkspaceTab struct {
	ID       string      `json:"id"`
	Host     string      `json:"host"`
	Label    string      `json:"label"`
	Session  string      `json:"session,omitzero"`
	State    string      `json:"state"`
	OpenedAt string      `json:"opened_at,omitzero"`
	Ended    *TabEnded   `json:"ended,omitzero"`
	Refused  *TabRefused `json:"refused,omitzero"`
}

// TabEnded is how a tab's SSH Session ended: the reason the log gives, the
// notice the page shows, if any, and how the shell ended, when it did.
type TabEnded struct {
	Reason     string  `json:"reason"`
	Notice     string  `json:"notice,omitzero"`
	ExitStatus *uint32 `json:"exit_status,omitzero"`
	ExitSignal string  `json:"exit_signal,omitzero"`
}

// TabRefused is why a tab's SSH Session did not start: the code and
// message an open would be refused with.
type TabRefused struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WorkspaceAnswer answers one request: the tab an open or reopen added or
// started, or nothing more for a close, or the refusal with its code and
// message.
type WorkspaceAnswer struct {
	Request uint64  `json:"request"`
	Tab     string  `json:"tab,omitzero"`
	Refused string  `json:"refused,omitzero"`
	Message *string `json:"message,omitzero"`
}

// WorkspaceUnknownTab refuses a reopen of a tab the Workspace no longer
// holds, as when another page closed it just before.
const WorkspaceUnknownTab = "unknown_tab"

// WorkspaceStopping is the koko server's last message as it stops.
type WorkspaceStopping struct {
	Stopping string `json:"stopping"`
}
