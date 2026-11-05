package server_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/build"
)

// The koko server reports its build's version and every API operation, the
// same before and after initialisation, so a CLI can tell which commands
// this koko server lacks.
func TestTheReportListsEveryOperation(t *testing.T) {
	var want []string
	for _, o := range api.Operations() {
		want = append(want, o.Info().Name)
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
			if !slices.Equal(r.Operations, want) {
				t.Errorf("reported operations %v, want every operation: %v", r.Operations, want)
			}
		})
	}
}
