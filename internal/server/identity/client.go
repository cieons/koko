package identity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// The bounds of every call to the Identity Provider beside its time and its
// turn, which timing.ProviderCall and timing.ProviderTurn give (ADR 0060).
const (
	// maxRedirects is the most redirects one call follows.
	maxRedirects = 3
	// maxAnswer is the largest answer read, in bytes.
	maxAnswer = 1 << 20
	// maxInFlight is the most calls in flight at once, across the koko
	// server.
	maxInFlight = 4
)

// Answers that are not the provider's, or that are not ones koko takes:
// discovery reads them as a provider set wrongly, never as one that did not
// answer.
var (
	errNotHTTPS         = errors.New("koko calls the Identity Provider over https:// only")
	errTooManyRedirects = fmt.Errorf("the Identity Provider redirected more than %d times", maxRedirects)
	errTooLarge         = fmt.Errorf("the Identity Provider's answer is over %d bytes", maxAnswer)
)

// errNoTurn is a call that waited its whole turn while maxInFlight calls
// were in flight: the provider is not answering them.
var errNoTurn = fmt.Errorf("%d calls to the Identity Provider were in flight, and this one's turn did not come", maxInFlight)

// newClient returns the client of every call to the Identity Provider:
// base's transport, which tests replace to trust their in-process issuer,
// over https:// alone, following at most maxRedirects redirects, each call
// bounded by call, its redirects included, and its answer, headers and
// body, read up to maxAnswer bytes, with at most maxInFlight in flight, a
// call waiting up to turn for its turn. It logs once, to log, when a run of
// calls the provider does not answer, or answers with a server error,
// starts.
func newClient(base *http.Client, call, turn time.Duration, log *slog.Logger) *http.Client {
	rt := http.DefaultTransport
	if base != nil && base.Transport != nil {
		rt = base.Transport
	}
	if t, ok := rt.(*http.Transport); ok {
		t = t.Clone()
		t.MaxResponseHeaderBytes = maxAnswer
		rt = t
	}
	return &http.Client{
		Transport: &bounded{base: rt, call: call, turn: turn, turns: make(chan struct{}, maxInFlight), log: log},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return errTooManyRedirects
			}
			return nil
		},
	}
}

// bounded is the transport of newClient's client.
type bounded struct {
	base       http.RoundTripper
	call, turn time.Duration
	// turns holds one value for each call in flight.
	turns chan struct{}
	log   *slog.Logger
	// failing says the last call had no answer, so the next failure is not
	// logged again.
	mu      sync.Mutex
	failing bool
}

func (b *bounded) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" {
		return nil, fmt.Errorf("%w, not %s", errNotHTTPS, at(req.URL))
	}
	// A redirect's request carries the call's deadline on the request it
	// follows, which this transport gave the deadline; the first sets it
	// once it has its turn.
	ctx, cancel := req.Context(), context.CancelFunc(func() {})
	deadline, redirected := time.Time{}, req.Response != nil && req.Response.Request != nil
	if redirected {
		deadline, redirected = req.Response.Request.Context().Deadline()
	}
	if redirected {
		ctx, cancel = context.WithDeadline(ctx, deadline)
	}
	if err := b.take(ctx); err != nil {
		cancel()
		b.failed(req.URL, err)
		return nil, err
	}
	if !redirected {
		ctx, cancel = context.WithTimeout(ctx, b.call)
	}
	resp, err := b.base.RoundTrip(req.WithContext(ctx))
	if err != nil {
		cancel()
		<-b.turns
		if strings.Contains(err.Error(), "server response headers exceeded") {
			// net/http's refusal of MaxResponseHeaderBytes, which it
			// gives no error value of its own.
			err = fmt.Errorf("%w: %w", errTooLarge, err)
			b.answered()
			return nil, err
		}
		b.failed(req.URL, err)
		return nil, err
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		b.failed(req.URL, fmt.Errorf("it answered %s", resp.Status))
	} else {
		b.answered()
	}
	resp.Body = &answer{body: resp.Body, left: maxAnswer, done: sync.OnceFunc(func() {
		cancel()
		<-b.turns
	})}
	return resp, nil
}

// take waits up to b.turn for a call's turn.
func (b *bounded) take(ctx context.Context) error {
	wait := time.NewTimer(b.turn)
	defer wait.Stop()
	select {
	case b.turns <- struct{}{}:
		return nil
	case <-wait.C:
		return errNoTurn
	case <-ctx.Done():
		return ctx.Err()
	}
}

// failed logs a call to u that had no answer, or a server error, once per
// run of such calls.
func (b *bounded) failed(u *url.URL, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failing {
		return
	}
	b.failing = true
	b.log.Warn("the identity provider could not be reached", "url", at(u), "err", err)
}

// answered ends a run of calls that had no answer or a server error.
func (b *bounded) answered() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failing = false
}

// at is u without its query and fragment, as a log line or an error names
// it: a call's query may carry what is not for the log.
func at(u *url.URL) string {
	c := u.Clone()
	c.RawQuery, c.Fragment, c.RawFragment, c.User = "", "", "", nil
	return c.String()
}

// answer is a call's answer body: read up to its limit, after which it
// fails rather than ends, so a cut answer never passes for a whole one; and
// once closed, the call's turn goes to the next.
type answer struct {
	body io.ReadCloser
	left int64
	done func()
}

func (a *answer) Read(p []byte) (int, error) {
	if a.left <= 0 {
		// One byte more tells an answer of exactly the limit from a longer
		// one.
		var one [1]byte
		for {
			n, err := a.body.Read(one[:])
			switch {
			case n > 0:
				return 0, errTooLarge
			case err != nil:
				return 0, err
			}
		}
	}
	p = p[:min(int64(len(p)), a.left)]
	n, err := a.body.Read(p)
	a.left -= int64(n)
	return n, err
}

func (a *answer) Close() error {
	defer a.done()
	return a.body.Close()
}
