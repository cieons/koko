package wire_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/shared/wire"
)

// A Workspace request asks for exactly one of open, reopen and close, with
// its number, a tab by its id and an open at a size in range; the
// liveness message asks for nothing. Anything else is refused.
func TestAWorkspaceRequestIsChecked(t *testing.T) {
	t.Parallel()
	tab := strings.Repeat("ab", 16)
	for _, r := range []wire.WorkspaceRequest{
		{},
		{Request: 1, Open: "web1", Cols: 80, Rows: 24},
		{Request: 2, Reopen: tab, Cols: 1, Rows: wire.TerminalMaxSide},
		{Request: 3, Close: tab},
	} {
		if err := r.Check(); err != nil {
			t.Errorf("%+v was refused: %v", r, err)
		}
	}
	for _, r := range []wire.WorkspaceRequest{
		{Request: 1},
		{Open: "web1", Cols: 80, Rows: 24},
		{Request: 1, Open: "web1", Close: tab, Cols: 80, Rows: 24},
		{Request: 1, Open: "web1", Cols: 0, Rows: 24},
		{Request: 1, Open: "web1", Cols: 80, Rows: wire.TerminalMaxSide + 1},
		{Request: 1, Reopen: tab},
		{Request: 1, Reopen: "web1", Cols: 80, Rows: 24},
		{Request: 1, Close: strings.ToUpper(tab)},
		{Request: 1, Close: tab, Cols: 80, Rows: 24},
	} {
		if err := r.Check(); !errors.Is(err, wire.ErrWorkspaceRequest) {
			t.Errorf("%+v was checked with %v, want ErrWorkspaceRequest", r, err)
		}
	}
}

// A terminal socket's first message asks for one attach, at a size in
// range and to an SSH Session by its id, or one resume of an attachment.
func TestATerminalFirstMessageIsChecked(t *testing.T) {
	t.Parallel()
	id := strings.Repeat("0f", 16)
	for _, f := range []wire.TerminalFirst{
		{Protocol: wire.TerminalProtocol, Attach: id, Cols: 80, Rows: 24},
		{Protocol: wire.TerminalProtocol, Resume: id, Offset: 4096},
	} {
		if err := f.Check(); err != nil {
			t.Errorf("%+v was refused: %v", f, err)
		}
	}
	for _, f := range []wire.TerminalFirst{
		{Protocol: wire.TerminalProtocol},
		{Protocol: wire.TerminalProtocol, Attach: id, Resume: id, Cols: 80, Rows: 24},
		{Protocol: wire.TerminalProtocol, Attach: id},
		{Protocol: wire.TerminalProtocol, Attach: "web1", Cols: 80, Rows: 24},
		{Protocol: wire.TerminalProtocol, Resume: id[:31]},
	} {
		if err := f.Check(); !errors.Is(err, wire.ErrTerminalFirst) {
			t.Errorf("%+v was checked with %v, want ErrTerminalFirst", f, err)
		}
	}
}
