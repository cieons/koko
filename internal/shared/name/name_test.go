package name_test

import (
	"strings"
	"testing"

	"github.com/cieons/koko/internal/shared/name"
)

func TestCheckAcceptsLowercaseDNSLabels(t *testing.T) {
	for _, s := range []string{
		"a",
		"ops",
		"web-1",
		"0",
		"9lives",
		strings.Repeat("a", 63),
	} {
		if err := name.Check(s); err != nil {
			t.Errorf("Check(%q) = %v, want nil", s, err)
		}
	}
}

func TestCheckRefusesEverythingElse(t *testing.T) {
	for _, s := range []string{
		"",
		strings.Repeat("a", 64),
		"Ops",
		"-ops",
		"ops-",
		"ops_1",
		"ops.prod",
		"a/b",
		"ops ",
		"é",
	} {
		if err := name.Check(s); err == nil {
			t.Errorf("Check(%q) = nil, want an error", s)
		}
	}
}
