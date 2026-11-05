package cli

import (
	"reflect"
	"testing"

	"github.com/cieons/koko/internal/shared/api"
)

// Each secret command sits in the tier P0 spec: Secrets gives it, so an
// agent's harness can allow secret ls by prefix and ask before secret show.
func TestSecretCommandsSitInTheirTiers(t *testing.T) {
	want := map[string]Tier{
		"secret ls":     Read,
		"secret add":    Reversible,
		"secret set":    Reversible,
		"secret rename": Reversible,
		"secret move":   Reversible,
		"secret show":   Irreversible,
		"secret rm":     Irreversible,
	}
	for _, c := range Tree() {
		if c.Noun != "secret" {
			continue
		}
		tier, ok := want[c.Path()]
		switch {
		case !ok:
			t.Errorf("koko %s is a secret command this test gives no tier", c.Path())
		case c.Tier != tier:
			t.Errorf("koko %s is %s, want %s", c.Path(), c.Tier, tier)
		}
		delete(want, c.Path())
	}
	for path := range want {
		t.Errorf("koko %s is not in the command tree", path)
	}
}

// secret.show is the one operation, and secret show the one command, that
// answers with a Secret value, in the tier that prints a credential: one
// harness rule asks before any value is shown, and no flag of another
// command adds one (P0 spec: Secrets, stories 28 and 30).
func TestOnlySecretShowAnswersWithAValue(t *testing.T) {
	value, printed := reflect.TypeFor[api.SecretValue](), reflect.TypeFor[secretValueJSON]()
	for _, o := range api.Operations() {
		info := o.Info()
		if got := holds(info.Response, value, map[reflect.Type]bool{}); got != (info.Name == api.SecretShow.Name) {
			t.Errorf("the response of %s holds a Secret value: %v", info.Name, got)
		}
	}
	printing := 0
	for _, c := range Tree() {
		answers := c.Op != nil && holds(c.Op.Info().Response, value, map[reflect.Type]bool{})
		prints := c.Run.Shape() != nil && holds(c.Run.Shape(), printed, map[reflect.Type]bool{})
		if !answers && !prints {
			continue
		}
		printing++
		if c.Path() != "secret show" || c.Tier != Irreversible {
			t.Errorf("koko %s (%s) gets or prints a Secret value", c.Path(), c.Tier)
		}
	}
	if printing != 1 {
		t.Errorf("%d commands get a Secret value, want secret show alone", printing)
	}
}
