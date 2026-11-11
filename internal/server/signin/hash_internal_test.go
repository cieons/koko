package signin

import (
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
// the English label joins what it names.
func TestClientsOfUserAgents(t *testing.T) {
	t.Parallel()
	for ua, want := range map[string][3]string{
		"Mozilla/5.0 (iPhone; CPU iPhone OS 26_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.1 Mobile/15E148 Safari/604.1": {"Safari", "iPhone", "Safari on iPhone"},
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36":                         {"Chrome", "Windows", "Chrome on Windows"},
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36 Edg/142.0.0.0":           {"Edge", "Windows", "Edge on Windows"},
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.1 Safari/605.1.15":                   {"Safari", "macOS", "Safari on macOS"},
		"Mozilla/5.0 (Linux; Android 16; Pixel 9) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Mobile Safari/537.36":                   {"Chrome", "Android", "Chrome on Android"},
		"Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0":                                                                  {"Firefox", "Linux", "Firefox on Linux"},
		"Mozilla/5.0 (iPad; CPU OS 26_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/142.0 Mobile/15E148 Safari/604.1":           {"Chrome", "iPad", "Chrome on iPad"},
		"Firefox/140.0": {"Firefox", "", "Firefox"},
		"curl/8.17.0":   {"", "", "Browser"},
		"":              {"", "", "Browser"},
	} {
		browser, system := clientOf(ua)
		if got := [3]string{browser, system, api.ClientLabel(browser, system)}; got != want {
			t.Errorf("%q names the browser, the system and the label %q, want %q", ua, got, want)
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
