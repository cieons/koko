package cli

import (
	"slices"
	"testing"

	"github.com/cieons/koko/internal/shared/api"
)

// A code means one thing: the CLI's own codes never reuse an API code.
func TestCLICodesAreApartFromAPICodes(t *testing.T) {
	t.Parallel()
	for _, c := range api.Codes() {
		if slices.Contains(cliCodes, c.Name) {
			t.Errorf("%s is both an API code and one of the CLI's own", c.Name)
		}
	}
}
