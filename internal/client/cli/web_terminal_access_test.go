package cli_test

import (
	"errors"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/resumable"
)

// A Web URL change ends the web terminals that came through the web
// listener, whose pages it signs out, and none through koko web, whose page
// is at koko web's own address.
func TestAWebURLChangeKeepsKokoWebsTerminals(t *testing.T) {
	t.Parallel()
	h := startWebListener(t)
	tunnelHost(t, h, "web1")
	signedIn := h.SignIn(harness.Owner).TerminalPage().Open("web1", 80, 24)
	term := webTerminalPage(t, startWeb(t, h)).Open("web1", 80, 24)
	term.Type([]byte("echo ready; exec cat\n"))
	term.WaitOutput("ready\n")

	h.Koko("server", "set", "web_url=https://koko.example.org").WantSuccess(t)
	const notice = "the Web URL changed; this SSH Session is closed. Sign in at the new address."
	if e, ok := errors.AsType[*resumable.PeerClosedError](signedIn.WaitEnd()); !ok || e.Reason != notice {
		t.Errorf("the signed-in page's terminal ended with %v, want the koko server's close saying %q", signedIn.Err(), notice)
	}
	term.Type([]byte("still\n"))
	term.WaitOutput("ready\nstill\n")
}
