package cli_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/timing"
)

// toldOf counts the changes the koko server told the Device Session id of,
// as its log says.
func toldOf(h *harness.Harness, id string) int {
	n := 0
	for line := range strings.Lines(h.Log()) {
		if strings.Contains(line, `msg="told devices the Hosts they see changed"`) && strings.Contains(line, id) {
			n++
		}
	}
	return n
}

// told counts the changes the koko server told any device of.
func told(h *harness.Harness) int {
	return strings.Count(h.Log(), `msg="told devices the Hosts they see changed"`)
}

// When a change names a Host a User can see, the koko server tells the
// Device Mux on each of that User's devices, which rewrites the bare names
// within seconds, long before its BareNameRefresh: a Host recorded in the
// web UI comes, one moved out of the User's Spaces and one Retired go, and
// a Space the User is added to or taken out of brings its Hosts or takes
// them away. A change in a Space the User cannot act in tells their
// devices nothing.
func TestTheDeviceMuxRewritesTheBareNamesWhenTheHostsItsUserSeesChange(t *testing.T) {
	t.Parallel()
	h := startWebListener(t)
	h.Koko("space", "add", "ops").WantSuccess(t)
	h.Koko("space", "add", "staging").WantSuccess(t)
	h.Koko("space", "member", "add", "ops", "bob").WantSuccess(t)
	addHost(t, h.Koko, "db1", "space=ops")
	addHost(t, h.Koko, "web9", "space=staging")
	alice := h.SignIn(harness.Owner)

	// No refresh falls due during the test: what rewrites the names is the
	// notice.
	bob := harness.NewMuxDevice(t, timing.Timings{BareNameRefresh: time.Hour})
	bob.Koko("join", deviceAdd(t, h, "laptop", "--user", "bob").Code).WantSuccess(t)
	bob.Koko("status").WantSuccess(t)
	waitLog(t, bob, `msg="refreshed the bare names"`)
	wantBareNames(t, bob, "after bob's join", "db1")
	bobID := readClientConfig(t, bob).DeviceSession

	steps := []struct {
		what string
		do   func()
		want []string
	}{{
		what: "alice records web1 in ops in the web UI",
		do: func() {
			a := alice.Do(http.MethodPost, api.HostAdd.Path(), `{"hosts": [{"name": "web1", "address": "203.0.113.10", "account": "deploy", "space": "ops"}]}`)
			if a.StatusCode != http.StatusOK {
				t.Fatalf("host.add in the web UI answered %d: %s", a.StatusCode, a.Body)
			}
		},
		want: []string{"db1", "web1"},
	}, {
		what: "alice renames web1 to web2",
		do:   func() { h.Koko("host", "rename", "web1", "web2").WantSuccess(t) },
		want: []string{"db1", "web2"},
	}, {
		what: "alice moves web2 to staging",
		do:   func() { h.Koko("host", "move", "web2", "staging").WantSuccess(t) },
		want: []string{"db1"},
	}, {
		what: "alice adds bob to staging",
		do:   func() { h.Koko("space", "member", "add", "staging", "bob").WantSuccess(t) },
		want: []string{"db1", "web2", "web9"},
	}, {
		what: "alice retires web9",
		do:   func() { h.Koko("host", "retire", "web9").WantSuccess(t) },
		want: []string{"db1", "web2"},
	}, {
		what: "alice removes bob from staging",
		do:   func() { h.Koko("space", "member", "rm", "staging", "bob").WantSuccess(t) },
		want: []string{"db1"},
	}, {
		what: "alice deletes db1",
		do:   func() { h.Koko("host", "rm", "db1").WantSuccess(t) },
		want: []string{},
	}}
	for _, step := range steps {
		step.do()
		eventually(t, fmt.Sprintf("after %s, bob's koko ssh_config holds %q", step.what, step.want), func() bool {
			return slices.Equal(bareNames(t, bob), step.want)
		})
	}

	// A Host recorded in staging, which bob no longer acts in, is told to
	// alice's device alone.
	aliceDevice := harness.NewMuxDevice(t, timing.Timings{BareNameRefresh: time.Hour})
	aliceDevice.Koko("join", deviceAdd(t, h, "desk").Code).WantSuccess(t)
	aliceDevice.Koko("status").WantSuccess(t)
	aliceID := readClientConfig(t, aliceDevice).DeviceSession
	before, bobBefore, aliceBefore := told(h), toldOf(h, bobID), toldOf(h, aliceID)
	addHost(t, h.Koko, "web3", "space=staging")
	eventually(t, "the koko server tells of web3", func() bool { return told(h) > before })
	if n := toldOf(h, bobID) - bobBefore; n != 0 {
		t.Errorf("the koko server told bob's device of a Host in a Space he cannot act in")
	}
	if toldOf(h, aliceID) == aliceBefore {
		t.Errorf("the koko server did not tell alice's device of web3")
	}
	eventually(t, "alice's koko ssh_config holds web3", func() bool {
		return slices.Contains(bareNames(t, aliceDevice), "web3")
	})
}

// After a drop, the Device Mux rewrites the bare names on its next Device
// Connection, since a notice sent while it had none is lost, and hears the
// notices that come on that one.
func TestTheDeviceMuxHearsTheNoticesOfItsNextDeviceConnection(t *testing.T) {
	t.Parallel()
	h, fwd, d := muxDevice(t, timing.Timings{BareNameRefresh: time.Hour})
	// The next answer the koko server sends, space ls's, is cut with the
	// Device Connection; the call goes again on the next one.
	fwd.CutWhen(func() bool { return true })
	d.Koko("space", "ls", "--json").WantSuccess(t)
	waitLogs(t, d, `msg="device connection opened"`, 2)
	waitLogs(t, d, `msg="refreshed the bare names"`, 2)

	addHost(t, h.Koko, "web1")
	eventually(t, "the Device Mux writes the bare name web1 on its next Device Connection's notice", func() bool {
		return slices.Equal(bareNames(t, d), []string{"web1"})
	})
}
