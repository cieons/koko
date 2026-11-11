// Package updatecheck is the koko server's check for a new koko release
// (ADR 0048). A random 2 to 5 minutes after the koko server starts, then 6
// hours after the last check of any kind, it sends one HEAD to the
// releases/latest redirect, carrying nothing about the deployment, while the
// Owner's update_check setting is on. The Owner can also check at once,
// with the setting on or off, and callers that come while a check is in
// flight share it. The result is kept in memory, so a restart checks again.
package updatecheck

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/cieons/koko/internal/shared/release"
)

// The check's times (ADR 0048): the first a random FirstMin to FirstMax
// after start, so that restarts and many koko servers do not come at once,
// then Every after the last check of any kind.
const (
	FirstMin = 2 * time.Minute
	FirstMax = 5 * time.Minute
	Every    = 6 * time.Hour
)

// Clock is the clock the check reads and waits on: System on a machine, a
// fake in tests, which moves only when the test moves it.
type Clock interface {
	Now() time.Time
	// AfterFunc calls f in its own goroutine once d has passed on the
	// clock, and returns what stops that call, as time.AfterFunc does.
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

// System is the machine's clock.
var System Clock = systemClock{}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) AfterFunc(d time.Duration, f func()) func() bool {
	return time.AfterFunc(d, f).Stop
}

// Result is what the checks found so far.
type Result struct {
	// CheckedAt is when the last check ended, the zero Time before any.
	CheckedAt time.Time
	// Latest is the tag of koko's latest release, as the last check that
	// succeeded found it; "" before any.
	Latest string
	// Error is why the last check failed; "" when it succeeded.
	Error string
}

// Checker checks for a new release by itself while it is on, and at once
// when asked.
type Checker struct {
	source release.Source
	clock  Clock
	log    *slog.Logger
	// ctx is the checks' context, which Close cancels.
	ctx    context.Context
	cancel context.CancelFunc

	mu sync.Mutex
	// on mirrors the update_check setting.
	on     bool
	result Result
	// stop stops the next check by itself, nil while none is due.
	stop func() bool
	// flight is the check in flight, nil while none is.
	flight *flight
	// failing says the last check failed and was logged.
	failing bool
	closed  bool
	checks  sync.WaitGroup
}

// flight is one check, which every caller that comes while it runs shares.
type flight struct {
	done chan struct{}
	// asked says the Owner asked for the check, rather than the checker
	// making it by itself.
	asked  bool
	result Result
	err    error
}

// Start returns a Checker that finds releases at source and waits on clock,
// and that checks by itself, from a random FirstMin to FirstMax from now,
// while on, as the update_check setting is. Close stops it.
func Start(source release.Source, clock Clock, on bool, log *slog.Logger) *Checker {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Checker{source: source, clock: clock, log: log, ctx: ctx, cancel: cancel, on: on}
	if on {
		c.mu.Lock()
		c.scheduleFirst()
		c.mu.Unlock()
	}
	return c
}

// SetOn follows a change to the update_check setting. Turned on, the
// checker checks by itself again, first a random FirstMin to FirstMax from
// now, as at start. Turned off, it checks only when asked, and forgets what
// it found, so that no notice outlives the setting until the Owner checks
// at once.
func (c *Checker) SetOn(on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if on == c.on || c.closed {
		return
	}
	c.on = on
	c.stopNext()
	switch {
	case !on:
		c.result = Result{}
	case c.flight == nil:
		// A check in flight schedules the next itself when it ends.
		c.scheduleFirst()
	}
}

// Result returns what the checks found so far.
func (c *Checker) Result() Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.result
}

// ErrClosed is a check asked for once Close has begun, or cut short by it.
var ErrClosed = errors.New("the koko server is stopping")

// Check checks at once, whether the checker is on or off, sharing a check
// already in flight, and returns what it found. A check that fails returns
// why, as Result's Error records it too, or ErrClosed once Close has begun.
// ctx bounds only the wait: the check itself goes on for the callers that
// share it.
func (c *Checker) Check(ctx context.Context) (Result, error) {
	c.mu.Lock()
	f, err := c.begin(true)
	c.mu.Unlock()
	if err != nil {
		return Result{}, err
	}
	select {
	case <-f.done:
		return f.result, f.err
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

// begin returns the check in flight, starting one when none is; asked says
// the Owner asks for it. The caller holds mu.
func (c *Checker) begin(asked bool) (*flight, error) {
	if c.closed {
		return nil, ErrClosed
	}
	if f := c.flight; f != nil {
		c.log.Debug("the update check waits for the one in flight")
		f.asked = f.asked || asked
		return f, nil
	}
	f := &flight{done: make(chan struct{}), asked: asked}
	c.flight = f
	c.stopNext()
	c.checks.Go(func() { c.run(f) })
	return f, nil
}

// run checks once for the flight f, records what it found, and schedules
// the next check by itself, Every from now, while the checker is on. A
// check it made by itself that ends after the setting was turned off is
// forgotten, as what came before it was.
func (c *Checker) run(f *flight) {
	tag, err := c.source.Latest(c.ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	defer close(f.done)
	c.flight = nil
	switch {
	case c.closed:
		f.err = ErrClosed
		return
	case !c.on && !f.asked:
		f.result, f.err = c.result, err
		return
	}
	c.result.CheckedAt = c.clock.Now().UTC().Truncate(time.Second)
	switch {
	case err != nil:
		c.result.Error = err.Error()
		if !c.failing {
			c.log.Info("the update check could not find koko's latest release; it checks again in 6 hours, or at once when the Owner asks",
				"error", err)
		}
		c.failing = true
	default:
		c.result.Latest, c.result.Error = tag, ""
		if c.failing {
			c.log.Info("the update check found koko's latest release again", "latest", tag)
		}
		c.failing = false
	}
	f.result, f.err = c.result, err
	if c.on {
		c.stop = c.clock.AfterFunc(Every, c.due)
	}
}

// due is a check the checker makes by itself, while it is on.
func (c *Checker) due() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.on {
		_, _ = c.begin(false)
	}
}

// scheduleFirst schedules the first check by itself, a random FirstMin to
// FirstMax from now. The caller holds mu.
func (c *Checker) scheduleFirst() {
	c.stop = c.clock.AfterFunc(FirstMin+rand.N(FirstMax-FirstMin+1), c.due)
}

// stopNext stops the next check by itself. The caller holds mu.
func (c *Checker) stopNext() {
	if c.stop != nil {
		c.stop()
		c.stop = nil
	}
}

// Close stops the checker, and waits for a check in flight to end.
func (c *Checker) Close() {
	c.mu.Lock()
	c.closed = true
	c.stopNext()
	c.mu.Unlock()
	c.cancel()
	c.checks.Wait()
}
