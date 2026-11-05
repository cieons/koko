package store_test

import (
	"regexp"
	"testing"

	"github.com/cieons/koko/internal/server/store"
)

func TestNewIDIsPrefixAndEightCrockfordCharacters(t *testing.T) {
	format := regexp.MustCompile(`^space_[0-9a-hjkmnp-tv-z]{8}$`)
	seen := map[string]bool{}
	for range 1000 {
		id := store.NewID("space")
		if !format.MatchString(id) {
			t.Fatalf("NewID = %q, want space_ and 8 lowercase Crockford base32 characters", id)
		}
		if seen[id] {
			t.Fatalf("NewID repeated %q within 1000 ids", id)
		}
		seen[id] = true
	}
}
