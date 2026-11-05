package api_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/shared/api"
)

// A Secret value prints as its length through fmt, whatever the verb, and
// logs as its length through slog's text handler, the koko server's, alone
// or inside a struct, so formatting one by mistake leaks nothing.
func TestASecretValuePrintsAsItsLength(t *testing.T) {
	v := api.SecretValue("s3cret-value")
	held := struct{ Value api.SecretValue }{v}
	var printed []string
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d"} {
		printed = append(printed, fmt.Sprintf(verb, v), fmt.Sprintf(verb, held))
	}
	var log bytes.Buffer
	slog.New(slog.NewTextHandler(&log, nil)).Info("store", "value", v, "request", held)
	slog.New(slog.NewJSONHandler(&log, nil)).Info("store", "value", v)
	printed = append(printed, log.String())
	forms := []string{"s3cret", "73336372", "115 51", base64.StdEncoding.EncodeToString(v)[:12]}
	for _, p := range printed {
		for _, form := range forms {
			if strings.Contains(p, form) {
				t.Errorf("printed %q, which holds the value as %q", p, form)
			}
		}
	}
	if got := printed[0]; got != "<Secret value, 12 bytes>" {
		t.Errorf("%%v printed %q, want the length", got)
	}
}
