package identity

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/server/signin"
	"github.com/cieons/koko/internal/shared/api"
)

// A callback's refusal sends a page only a code the page words: one of the
// callback's own as it is, any other coded refusal, such as a discovery no
// longer valid, as provider_failed at the step discovery, and an error
// without a code as internal_error.
func TestACallbackRefusesOnlyWithItsOwnCodes(t *testing.T) {
	t.Parallel()
	var log bytes.Buffer
	s := &Service{log: slog.New(slog.NewTextHandler(&log, nil))}
	f := oidcFlow{purpose: api.OIDCPurposeLink, link: "passwordlink_aaaaaaaa"}
	for err, want := range map[error]string{
		api.CodeIdentityTaken.Errorf("taken"):             "/link?refused=identity_taken",
		api.CodeInvalidProvider.Errorf("no longer valid"): "/link?refused=provider_failed",
		errors.New("the store failed"):                    "/link?refused=internal_error",
		api.CodeProviderUnreachable.Errorf("no answer"):   "/link?refused=provider_unreachable",
	} {
		if got := s.refuse(f, signin.Attempt{Address: "192.0.2.1"}, err, "").Location; got != want {
			t.Errorf("%v went to %s, want %s", err, got, want)
		}
	}
	if !strings.Contains(log.String(), "reason=provider_failed purpose=link cause=invalid_provider step=discovery") {
		t.Errorf("the log does not name the cause:\n%s", log.String())
	}
}
