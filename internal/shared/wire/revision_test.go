package wire_test

import (
	"bytes"
	"flag"
	"fmt"
	"testing"

	"github.com/cieons/koko/internal/harness/frozen"
	"github.com/cieons/koko/internal/shared/wire"
)

var update = flag.Bool("update", false, "add new revisions to testdata/channels.golden")

// A CLI speaks the lower of its own revision of a channel and the koko
// server's, which a report that lists none gives as 1.
func TestACLISpeaksTheLowerRevisionOfAChannel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		revisions map[string]int
		own, want int
	}{
		{nil, 1, 1},
		{nil, 2, 1},
		{map[string]int{wire.ChannelTunnel: 2}, 1, 1},
		{map[string]int{wire.ChannelTunnel: 2}, 3, 2},
		{map[string]int{wire.ChannelTunnel: 3}, 3, 3},
		{map[string]int{wire.ChannelWeb: 3}, 2, 1},
	} {
		r := wire.Report{Version: "v0.5.0", Revisions: tc.revisions}
		if got := r.ChannelRevision(wire.ChannelTunnel, tc.own); got != tc.want {
			t.Errorf("a CLI at revision %d against the revisions %v speaks tunnel@koko at %d, want %d", tc.own, tc.revisions, got, tc.want)
		}
	}
}

// A payload of revision 1 ends as it always has; from revision 2 it ends
// with its revision as a uint32.
func TestAPayloadFromRevisionTwoEndsWithItsRevision(t *testing.T) {
	t.Parallel()
	var id [wire.TunnelIDSize]byte
	for name, payload := range map[string]func(rev int) []byte{
		"open":   func(rev int) []byte { return wire.TunnelOpen(rev, "web1") },
		"resume": func(rev int) []byte { return wire.TunnelResume(rev, id, 7) },
		"web":    func(rev int) []byte { return wire.WebPayload(rev, 8080) },
	} {
		one, two := payload(1), payload(2)
		if want := append(bytes.Clone(one), 0, 0, 0, 2); !bytes.Equal(two, want) {
			t.Errorf("%s at revision 2 is % x, want revision 1's then 00 00 00 02: % x", name, two, want)
		}
	}
}

// Every revision of each channel's payloads is recorded, with what the koko
// server reads from each, and a recorded revision never changes: a change
// to a payload's encoding or its reading without a raise of the channel's
// revision fails here, and -update records only a new revision (ADR 0049).
// The frames of tunnel@koko's stream are recorded in package resumable.
func TestEveryChannelsPayloadsAreRecordedByRevision(t *testing.T) {
	t.Parallel()
	id := [wire.TunnelIDSize]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	var sections []frozen.Section
	for rev := 1; rev <= wire.TunnelRevision; rev++ {
		open, resume := wire.TunnelOpen(rev, "web1.koko"), wire.TunnelResume(rev, id, 70000)
		sections = append(sections, frozen.Section{
			Name: fmt.Sprintf("%s %d", wire.ChannelTunnel, rev),
			Text: fmt.Sprintf("open web1.koko: % x\n  read as: %s\nresume 000102030405060708090a0b0c0d0e0f 70000: % x\n  read as: %s",
				open, parsed(wire.ParseTunnelPayload(open)), resume, parsed(wire.ParseTunnelPayload(resume))),
		})
	}
	for rev := 1; rev <= wire.WebRevision; rev++ {
		web := wire.WebPayload(rev, 8080)
		sections = append(sections, frozen.Section{
			Name: fmt.Sprintf("%s %d", wire.ChannelWeb, rev),
			Text: fmt.Sprintf("port 8080: % x\n  read as: %s", web, parsed(wire.ParseWebPayload(web))),
		})
	}
	frozen.Check(t, "channels.golden", sections, *update)
}

// parsed says what the koko server reads from a payload: what it carries,
// or why it refuses it.
func parsed[T any](v T, err error) string {
	if err != nil {
		return "refused: " + err.Error()
	}
	return fmt.Sprintf("%+v", v)
}
