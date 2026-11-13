package main

import (
	"encoding/json/v2"
	"net"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
)

// The skew check's page opens a tab through a run of koko web, attaches to
// its SSH Session, gets the shell's answer to its line, and closes the tab,
// which the koko server logs as the SSH Session's end.
func TestItOpensATerminalThroughKokoWebAndClosesIt(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	host := sshhost.Start(t, sshhost.Password("hunter2"))
	if r := h.Koko("host", "add", "web1", "address="+host.Address(), "port="+strconv.Itoa(host.Port()), "account="+sshhost.Account, "--json"); r.Status != 0 {
		t.Fatalf("koko host add web1 exited %d: %s", r.Status, r.Stderr)
	}
	if r := h.KokoStdin("hunter2", "host", "enroll", "web1", "--password-stdin", "--json"); r.Status != 0 {
		t.Fatalf("koko host enroll web1 exited %d: %s", r.Status, r.Stderr)
	}
	_, port, err := net.SplitHostPort(harness.FreeAddr(t))
	if err != nil {
		t.Fatal(err)
	}
	var printed struct {
		Address string `json:"address"`
	}
	web := h.Background(nil, "web", "--port", port, "--no-open", "--json")
	if err := json.Unmarshal([]byte(web.WaitStdout("}\n")), &printed); err != nil {
		t.Fatalf("koko web printed no address: %v", err)
	}

	id, err := run(t.Context(), printed.Address, "web1")
	if err != nil {
		t.Fatal(err)
	}
	ended := regexp.MustCompile(`msg="ssh session ended" tunnel=` + regexp.QuoteMeta(id) + ` client=browser .* reason="tab closed"\n`)
	for deadline := time.Now().Add(10 * time.Second); !ended.MatchString(h.Log()); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the koko server logged no end of the SSH Session %s by its tab's close within 10 s:\n%s", id, h.Log())
		}
	}
}
