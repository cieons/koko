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

// A Secret's name also takes '_', and may end in '-' or '_'; it starts with
// a letter or a digit (P0 spec: Secrets, story 4).
func TestCheckSecretAcceptsSecretNames(t *testing.T) {
	for _, s := range []string{
		"a",
		"0",
		"db-password",
		"db_password",
		"api-key_2",
		"key-",
		"key_",
		strings.Repeat("a", 63),
	} {
		if err := name.CheckSecret(s); err != nil {
			t.Errorf("CheckSecret(%q) = %v, want nil", s, err)
		}
	}
}

func TestCheckSecretRefusesEverythingElse(t *testing.T) {
	for _, s := range []string{
		"",
		strings.Repeat("a", 64),
		"-key",
		"_key",
		"DB-password",
		"db.password",
		"db/password",
		"db password",
		"db=password",
		"é",
	} {
		if err := name.CheckSecret(s); err == nil {
			t.Errorf("CheckSecret(%q) = nil, want an error", s)
		}
	}
}
