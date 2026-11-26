package signin

import (
	"encoding/json/v2"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cieons/koko/internal/shared/api"
)

// At most two hashes run at once: a burst of sign-ins waits its turn. With
// two hashes held, the others never start; this waits for one to start for
// a short while, which with the limit in place never comes.
func TestAtMostTwoHashesRunAtOnce(t *testing.T) {
	t.Parallel()
	h := newHasher()
	var running atomic.Int32
	started := make(chan struct{}, 6)
	release := make(chan struct{})
	h.derive = func(_, _ []byte, _, _ uint32, _ uint8, keyLen uint32) []byte {
		running.Add(1)
		started <- struct{}{}
		<-release
		running.Add(-1)
		return make([]byte, keyLen)
	}
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			if _, err := h.hash(t.Context(), "a password long enough"); err != nil {
				t.Error(err)
			}
		})
	}
	defer wg.Wait()
	defer close(release)
	for range maxHashing {
		<-started
	}
	select {
	case <-started:
		t.Errorf("%d hashes ran at once, want at most %d", running.Load(), maxHashing)
	case <-time.After(200 * time.Millisecond):
	}
}

// A User-Agent names one of the common browsers and systems, or none, and
// the English label joins what it names. The web UI's browserName labels
// the same table (browserName.test.ts), so the two labels cannot drift.
func TestClientsOfUserAgents(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("testdata/clients.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		UserAgent string `json:"user_agent"`
		Browser   string `json:"browser"`
		System    string `json:"system"`
		Client    string `json:"client"`
	}
	if err := json.Unmarshal(b, &cases, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		browser, system := clientOf(c.UserAgent)
		if got, want := [3]string{browser, system, api.ClientLabel(browser, system)}, [3]string{c.Browser, c.System, c.Client}; got != want {
			t.Errorf("%q names the browser, the system and the label %q, want %q", c.UserAgent, got, want)
		}
	}
}

// The limits forget the oldest names and addresses past their capacity.
func TestTheLimitsAreBounded(t *testing.T) {
	t.Parallel()
	r := newRecent[int](3)
	for i, k := range []string{"a", "b", "c", "d"} {
		r.put(k, i+1)
	}
	if r.get("a") != 0 || r.get("d") != 4 || r.order.Len() != 3 {
		t.Errorf("after 4 puts into 3, a = %d, d = %d, len %d; want a forgotten", r.get("a"), r.get("d"), r.order.Len())
	}
}
