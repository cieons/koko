package server_test

import (
	"context"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/build"
	"github.com/cieons/koko/internal/shared/compat"
	"github.com/cieons/koko/internal/shared/wire"
)

// The koko server reports its build's version, every API operation, the
// operations and channels above revision 1, and the oldest koko it works with, the same
// before and after initialisation, so a CLI can tell which commands and
// fields this koko server lacks, and whether it is too old for it.
func TestTheReportListsEveryOperation(t *testing.T) {
	var want []string
	revisions := map[string]int{}
	for _, o := range api.Operations() {
		want = append(want, o.Info().Name)
		if rev := o.Info().Revision; rev > 1 {
			revisions[o.Info().Name] = rev
		}
	}
	for channel, rev := range wire.ChannelRevisions() {
		if rev > 1 {
			revisions[channel] = rev
		}
	}
	slices.Sort(want)
	for name, opts := range map[string][]harness.Option{
		"initialised":   nil,
		"uninitialised": {harness.NoInit()},
	} {
		t.Run(name, func(t *testing.T) {
			h := harness.Start(t, opts...)
			conn, err := serverconn.DialSocket(t.Context(), h.Socket())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			r, err := conn.Report(ctx)
			if err != nil {
				t.Fatalf("read the report: %v", err)
			}
			if r.Version != build.Read().Version {
				t.Errorf("reported version %q, want the build's %q", r.Version, build.Read().Version)
			}
			if r.Oldest != compat.Oldest {
				t.Errorf("reported the oldest koko it works with as %q, want compat.Oldest, %q", r.Oldest, compat.Oldest)
			}
			if !slices.Equal(r.Operations, want) {
				t.Errorf("reported operations %v, want every operation: %v", r.Operations, want)
			}
			if !maps.Equal(r.Revisions, revisions) {
				t.Errorf("reported revisions %v, want those above 1: %v", r.Revisions, revisions)
			}
		})
	}
}
