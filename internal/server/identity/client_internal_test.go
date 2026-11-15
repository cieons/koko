package identity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testClient is newClient's client over srv's trusted transport, logging
// to the returned buffer, each call bounded by call.
func testClient(srv *httptest.Server, call time.Duration) (*http.Client, *bytes.Buffer) {
	var log bytes.Buffer
	return newClient(srv.Client(), call, time.Second, slog.New(slog.NewTextHandler(&log, nil))), &log
}

// get reads the answer of a GET of url through c whole.
func get(c *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

// The client calls https:// alone, and follows a redirect to http:// no
// further.
func TestTheClientCallsHTTPSOnly(t *testing.T) {
	t.Parallel()
	var plainCalls atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { plainCalls.Add(1) }))
	t.Cleanup(plain.Close)
	srv := httptest.NewTLSServer(http.RedirectHandler(plain.URL, http.StatusFound))
	t.Cleanup(srv.Close)
	c, _ := testClient(srv, time.Second)
	for _, url := range []string{plain.URL, srv.URL} {
		if _, err := get(c, url); !errors.Is(err, errNotHTTPS) {
			t.Errorf("GET %s failed with %v, want errNotHTTPS", url, err)
		}
	}
	if n := plainCalls.Load(); n != 0 {
		t.Errorf("the plain HTTP server was called %d times, want none", n)
	}
}

// The client follows at most 3 redirects.
func TestTheClientFollowsAtMostThreeRedirects(t *testing.T) {
	t.Parallel()
	// /n redirects to /n-1, and /0 answers.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/"))
		if n > 0 {
			http.Redirect(w, r, fmt.Sprintf("/%d", n-1), http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "here")
	}))
	t.Cleanup(srv.Close)
	c, _ := testClient(srv, time.Second)
	if body, err := get(c, srv.URL+"/3"); err != nil || string(body) != "here" {
		t.Errorf("3 redirects gave %q %v, want the answer", body, err)
	}
	if _, err := get(c, srv.URL+"/4"); !errors.Is(err, errTooManyRedirects) {
		t.Errorf("4 redirects failed with %v, want errTooManyRedirects", err)
	}
}

// The client reads an answer of up to 1 MiB, and fails on a longer one
// rather than cut it short.
func TestTheClientReadsAnswersUpTo1MiB(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/"))
		_, _ = w.Write(bytes.Repeat([]byte("a"), n))
	}))
	t.Cleanup(srv.Close)
	c, _ := testClient(srv, 5*time.Second)
	if body, err := get(c, fmt.Sprintf("%s/%d", srv.URL, maxAnswer)); err != nil || len(body) != maxAnswer {
		t.Errorf("an answer of 1 MiB read %d bytes, %v, want it whole", len(body), err)
	}
	if _, err := get(c, fmt.Sprintf("%s/%d", srv.URL, maxAnswer+1)); !errors.Is(err, errTooLarge) {
		t.Errorf("an answer of 1 MiB and a byte failed with %v, want errTooLarge", err)
	}
}

// A call the provider does not answer in time fails, and a run of such
// calls is logged once, again after an answer.
func TestACallWithoutAnAnswerIsLoggedOncePerRun(t *testing.T) {
	t.Parallel()
	var silent atomic.Bool
	silent.Store(true)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if silent.Load() {
			<-r.Context().Done()
		}
	}))
	t.Cleanup(srv.Close)
	c, log := testClient(srv, 100*time.Millisecond)
	const line = `msg="the identity provider could not be reached"`
	for range 2 {
		if _, err := get(c, srv.URL); err == nil {
			t.Fatal("a call the provider did not answer succeeded")
		}
	}
	if n := strings.Count(log.String(), line); n != 1 {
		t.Errorf("two calls without an answer logged %d lines, want 1:\n%s", n, log)
	}
	silent.Store(false)
	if _, err := get(c, srv.URL); err != nil {
		t.Fatal(err)
	}
	silent.Store(true)
	_, _ = get(c, srv.URL)
	if n := strings.Count(log.String(), line); n != 2 {
		t.Errorf("a call without an answer after an answer logged %d lines in all, want 2:\n%s", n, log)
	}
}

// fakeIssuer serves a discovery document for its own URL followed by /,
// counting its requests, or answers 503 while down.
type fakeIssuer struct {
	srv   *httptest.Server
	calls atomic.Int32
	down  atomic.Bool
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	f := &fakeIssuer{}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f.calls.Add(1)
		if f.down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		u := f.srv.URL + "/"
		_, _ = fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q,"response_types_supported":["code"]}`,
			u, u+"authorize", u+"token", u+"jwks")
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// fakeClock is a clock a test moves.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// A discovery is kept for 24 hours from its fetch; after a failure, it is
// fetched again no sooner than a minute later; and one a set found is kept
// from then on, until the provider is removed.
func TestADiscoveryIsKeptAsTheRulesSay(t *testing.T) {
	t.Parallel()
	f := newFakeIssuer(t)
	issuer := f.srv.URL + "/"
	clock := &fakeClock{now: time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)}
	c, _ := testClient(f.srv, time.Second)
	k := &discoveries{client: c, clock: clock.Now}
	step := func(what string, fetches int32, wantErr bool) {
		t.Helper()
		before := f.calls.Load()
		p, err := k.get(t.Context(), issuer)
		if got := f.calls.Load() - before; got != fetches {
			t.Errorf("%s fetched discovery %d times, want %d", what, got, fetches)
		}
		switch {
		case wantErr && err == nil:
			t.Errorf("%s succeeded, want a failure", what)
		case !wantErr && (err != nil || p == nil || p.Endpoint().TokenURL != issuer+"token"):
			t.Errorf("%s gave %v %v, want the issuer's provider", what, p, err)
		}
	}
	step("the first flow after a restart", 1, false)
	clock.advance(discoveryKept - time.Second)
	step("a flow within 24 hours", 0, false)
	clock.advance(time.Second)
	step("a flow 24 hours after the fetch", 1, false)
	f.down.Store(true)
	clock.advance(discoveryKept)
	step("a flow once the provider is down", 1, true)
	f.down.Store(false)
	clock.advance(retryAfter - time.Second)
	step("a flow within a minute of the failure", 0, true)
	clock.advance(time.Second)
	step("a flow a minute after the failure", 1, false)

	k.forget()
	step("a flow once the provider is removed", 1, false)
	d, err := discover(t.Context(), c, issuer)
	if err != nil {
		t.Fatal(err)
	}
	k.forget()
	k.keep(issuer, d)
	before := f.calls.Load()
	if _, err := k.get(t.Context(), issuer); err != nil || f.calls.Load() != before {
		t.Errorf("a flow after a set fetched discovery again: %v", err)
	}
}
