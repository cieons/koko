package server_test

import (
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server/updatecheck"
	"github.com/cieons/koko/internal/shared/api"
)

// checkFailedLog is what the koko server logs when its check for a new
// release begins to fail.
const checkFailedLog = "the update check could not find koko's latest release"

// startChecking starts a koko server whose check for a new release finds
// releases at a fake GitHub serving v0.5.0, whose releases/latest redirect
// names it when latest is true and is a 404 otherwise.
func startChecking(t *testing.T, latest bool) (*harness.Harness, *harness.Releases) {
	t.Helper()
	rel := harness.NewMachine(t).Release("v0.5.0")
	if latest {
		rel.Latest()
	}
	return harness.Start(t, harness.UpdateCheck(rel)), rel
}

// updateCheck returns the update check as server.show answers it.
func updateCheck(t *testing.T, h *harness.Harness) api.UpdateCheck {
	t.Helper()
	resp, err := h.Call(api.ServerShow, api.ServerShowRequest{})
	if err != nil {
		t.Fatal(err)
	}
	return resp.Server.UpdateCheck
}

// waitChecked waits until server.show answers a check that ended at
// checkedAt, which a check the clock started ends at, and returns the
// update check it answers.
func waitChecked(t *testing.T, h *harness.Harness, checkedAt time.Time) api.UpdateCheck {
	t.Helper()
	want := checkedAt.UTC().Truncate(time.Second).Format(time.RFC3339)
	deadline := time.Now().Add(10 * time.Second)
	for {
		u := updateCheck(t, h)
		if u.CheckedAt != nil && *u.CheckedAt == want {
			return u
		}
		if time.Now().After(deadline) {
			t.Fatalf("server.show answers %+v; want a check that ended at %s", u, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// wantWaits fails the test unless the koko server waits on its clock
// exactly for want: when its next check comes by itself.
func wantWaits(t *testing.T, h *harness.Harness, want ...time.Time) {
	t.Helper()
	if got := h.Waits(); !slices.EqualFunc(got, want, time.Time.Equal) {
		t.Errorf("the koko server waits for %v; want %v", got, want)
	}
}

// wantLatestRequests fails the test unless the fake GitHub received n
// requests, each a HEAD of the releases/latest redirect carrying nothing
// about the deployment: no query, cookie or body, and Go's default
// User-Agent.
func wantLatestRequests(t *testing.T, rel *harness.Releases, n int) {
	t.Helper()
	got := rel.Received()
	if len(got) != n {
		t.Fatalf("the fake GitHub received %d requests, %+v; want %d", len(got), got, n)
	}
	for _, r := range got {
		if r.Method != http.MethodHead || r.Path != harness.LatestPath() || r.Query != "" || r.Body != 0 {
			t.Errorf("the fake GitHub received %s %s?%s with %d bytes of body; want HEAD %s alone", r.Method, r.Path, r.Query, r.Body, harness.LatestPath())
		}
		if c := r.Header.Values("Cookie"); len(c) > 0 {
			t.Errorf("the request carried the cookies %q", c)
		}
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "Go-http-client/") {
			t.Errorf("the request's User-Agent is %q, not Go's default", ua)
		}
		for k := range r.Header {
			if !slices.Contains([]string{"User-Agent", "Accept-Encoding"}, k) {
				t.Errorf("the request carried the header %s: %q", k, r.Header.Values(k))
			}
		}
	}
}

// The koko server checks for a new release by itself a random 2 to 5
// minutes after it starts, then 6 hours after the last check of any kind,
// one the Owner asked for included, each time with one HEAD of the
// releases/latest redirect that says nothing about the deployment.
func TestTheUpdateCheckComesAFewMinutesAfterStartThenEvery6Hours(t *testing.T) {
	t.Parallel()
	h, rel := startChecking(t, true)
	start := h.Now()
	if u := updateCheck(t, h); !u.Enabled || u.CheckedAt != nil || u.Latest != nil || u.Error != nil {
		t.Errorf("before any check, server.show answers %+v; want it on with nothing found", u)
	}
	waits := h.Waits()
	if len(waits) != 1 || waits[0].Before(start.Add(updatecheck.FirstMin)) || waits[0].After(start.Add(updatecheck.FirstMax)) {
		t.Fatalf("the koko server waits for %v; want one check between %s and %s", waits, start.Add(updatecheck.FirstMin), start.Add(updatecheck.FirstMax))
	}
	first := waits[0]

	h.SetNow(first.Add(-time.Nanosecond))
	wantLatestRequests(t, rel, 0)
	h.SetNow(first)
	u := waitChecked(t, h, first)
	wantLatestRequests(t, rel, 1)
	if u.Latest == nil || *u.Latest != "v0.5.0" || u.Error != nil || !u.Enabled {
		t.Errorf("after the first check, server.show answers %+v; want v0.5.0 found", u)
	}
	wantWaits(t, h, first.Add(updatecheck.Every))

	// The Owner checks at once an hour later: the next check by itself
	// comes 6 hours after that one.
	h.Advance(time.Hour)
	asked := h.Now()
	resp, err := h.Call(api.ServerCheckUpdate, api.ServerCheckUpdateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	wantLatestRequests(t, rel, 2)
	if got := resp.UpdateCheck; got.Latest == nil || *got.Latest != "v0.5.0" || *got.CheckedAt != asked.Format(time.RFC3339) {
		t.Errorf("server.check_update answered %+v; want v0.5.0 found at %s", got, asked)
	}
	if resp.Version == "" {
		t.Error("server.check_update answered no version of the koko server")
	}
	wantWaits(t, h, asked.Add(updatecheck.Every))
	h.Advance(updatecheck.Every - time.Nanosecond)
	wantLatestRequests(t, rel, 2)
	h.Advance(time.Nanosecond)
	waitChecked(t, h, asked.Add(updatecheck.Every))
	wantLatestRequests(t, rel, 3)
}

// A check that fails is shown in server.show, logged once while checks go
// on failing, and tried again only 6 hours later.
func TestAFailedUpdateCheckIsShownAndNotRetriedSooner(t *testing.T) {
	t.Parallel()
	h, rel := startChecking(t, false)
	first := h.Waits()[0]
	h.SetNow(first)
	u := waitChecked(t, h, first)
	if u.Error == nil || !strings.Contains(*u.Error, harness.LatestPath()) || u.Latest != nil {
		t.Errorf("after a failed check, server.show answers %+v; want the failure, naming %s", u, harness.LatestPath())
	}
	wantWaits(t, h, first.Add(updatecheck.Every))
	wantLatestRequests(t, rel, 1)

	h.Advance(updatecheck.Every)
	waitChecked(t, h, first.Add(updatecheck.Every))
	wantLatestRequests(t, rel, 2)
	if n := strings.Count(h.Log(), checkFailedLog); n != 1 {
		t.Errorf("two checks in a row failed, and the koko server logged it %d times; want once:\n%s", n, h.Log())
	}

	rel.Latest()
	h.Advance(updatecheck.Every)
	u = waitChecked(t, h, first.Add(2*updatecheck.Every))
	if u.Error != nil || u.Latest == nil || *u.Latest != "v0.5.0" {
		t.Errorf("after a check that succeeded, server.show answers %+v; want v0.5.0 and no error", u)
	}
}

// With update_check off, the koko server sends nothing by itself and
// forgets what it found; the Owner can still check at once, and turned on
// again it checks a few minutes later, as after a start.
func TestTheUpdateCheckOffSendsNothingByItself(t *testing.T) {
	t.Parallel()
	h, rel := startChecking(t, true)
	if _, err := h.Call(api.ServerCheckUpdate, api.ServerCheckUpdateRequest{}); err != nil {
		t.Fatal(err)
	}
	resp, err := h.Call(api.ServerSet, api.ServerSetRequest{UpdateCheck: new(false)})
	if err != nil {
		t.Fatal(err)
	}
	if u := resp.Server.UpdateCheck; u.Enabled || u.CheckedAt != nil || u.Latest != nil {
		t.Errorf("server.set answered %+v; want it off, with nothing found", u)
	}
	if u := updateCheck(t, h); u.Enabled || u.CheckedAt != nil || u.Latest != nil {
		t.Errorf("server.show answers %+v; want it off, with nothing found", u)
	}
	wantWaits(t, h)
	h.Advance(7 * updatecheck.Every)
	wantLatestRequests(t, rel, 1)

	asked := h.Now()
	check, err := h.Call(api.ServerCheckUpdate, api.ServerCheckUpdateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	wantLatestRequests(t, rel, 2)
	if u := check.UpdateCheck; u.Enabled || u.Latest == nil || *u.Latest != "v0.5.0" || *u.CheckedAt != asked.Format(time.RFC3339) {
		t.Errorf("server.check_update with the check off answered %+v; want v0.5.0 found, and it off", u)
	}
	if u := updateCheck(t, h); u.Latest == nil || *u.Latest != "v0.5.0" {
		t.Errorf("after the Owner checked, server.show answers %+v; want v0.5.0", u)
	}
	wantWaits(t, h)

	on := h.Now()
	if _, err := h.Call(api.ServerSet, api.ServerSetRequest{UpdateCheck: new(true)}); err != nil {
		t.Fatal(err)
	}
	if u := updateCheck(t, h); !u.Enabled {
		t.Errorf("server.show answers %+v; want it on", u)
	}
	if w := h.Waits(); len(w) != 1 || w[0].Before(on.Add(updatecheck.FirstMin)) || w[0].After(on.Add(updatecheck.FirstMax)) {
		t.Errorf("turned on, the koko server waits for %v; want one check between %s and %s", w, on.Add(updatecheck.FirstMin), on.Add(updatecheck.FirstMax))
	}
}

// Callers who ask while a check is in flight share it: one request.
func TestChecksAtOnceWhileOneIsInFlightShareIt(t *testing.T) {
	t.Parallel()
	h, rel := startChecking(t, true)
	held, release := rel.HoldLatest()
	answers := make([]api.ServerCheckUpdateResponse, 2)
	errs := make([]error, 2)
	var calls sync.WaitGroup
	calls.Go(func() { answers[0], errs[0] = h.Call(api.ServerCheckUpdate, api.ServerCheckUpdateRequest{}) })
	select {
	case <-held:
	case <-time.After(10 * time.Second):
		release()
		t.Fatalf("the first check sent no request within 10 s; the koko server logged:\n%s", h.Log())
	}
	calls.Go(func() { answers[1], errs[1] = h.Call(api.ServerCheckUpdate, api.ServerCheckUpdateRequest{}) })
	// The second caller has joined the check in flight once the koko
	// server logs it; a request of its own would reach the fake GitHub.
	for deadline := time.Now().Add(10 * time.Second); !strings.Contains(h.Log(), "the update check waits for the one in flight"); {
		select {
		case <-held:
			release()
			t.Fatal("a check asked for while one was in flight sent a request of its own")
		default:
		}
		if time.Now().After(deadline) {
			release()
			t.Fatalf("the second check neither joined the one in flight nor sent a request within 10 s; the koko server logged:\n%s", h.Log())
		}
		time.Sleep(time.Millisecond)
	}
	release()
	calls.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
	wantLatestRequests(t, rel, 1)
	if !reflect.DeepEqual(answers[0], answers[1]) || answers[0].UpdateCheck.Latest == nil {
		t.Errorf("the two calls answered %+v and %+v; want one check's answer, v0.5.0", answers[0], answers[1])
	}
}

// A check the Owner asks for that fails is refused with
// update_check_failed, naming what failed, and server.show shows it.
func TestACheckAtOnceThatFailsIsUpdateCheckFailed(t *testing.T) {
	t.Parallel()
	h, rel := startChecking(t, false)
	_, err := h.Call(api.ServerCheckUpdate, api.ServerCheckUpdateRequest{})
	e, ok := errors.AsType[*api.Error](err)
	if !ok || e.Code != api.CodeUpdateCheckFailed.Name || !strings.Contains(e.Message, harness.LatestPath()) {
		t.Fatalf("server.check_update failed with %v; want update_check_failed naming %s", err, harness.LatestPath())
	}
	wantLatestRequests(t, rel, 1)
	if u := updateCheck(t, h); u.Error == nil || *u.Error != e.Message {
		t.Errorf("server.show answers %+v; want the error %q", u, e.Message)
	}
}
