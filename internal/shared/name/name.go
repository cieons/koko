// Package name holds the rule for the names of Users, Spaces and devices: a
// lowercase DNS label. No name holds '/', which qualified names such as
// <space>/<secret> use, or '_', which every record id holds.
package name

import (
	"errors"
	"fmt"
)

// MaxLen is the longest name: a DNS label's 63 characters.
const MaxLen = 63

// Check reports why s is not a name, or nil when it is one: 1 to 63 of a-z,
// 0-9 and '-', not starting or ending with '-'.
func Check(s string) error {
	switch {
	case s == "":
		return errors.New("a name cannot be empty")
	case len(s) > MaxLen:
		return fmt.Errorf("%q is longer than %d characters", s, MaxLen)
	case s[0] == '-' || s[len(s)-1] == '-':
		return fmt.Errorf("%q starts or ends with '-'", s)
	}
	for _, r := range s {
		if !isNameRune(r) {
			return fmt.Errorf("%q holds %q: a name uses only a-z, 0-9 and '-'", s, r)
		}
	}
	return nil
}

func isNameRune(r rune) bool {
	return 'a' <= r && r <= 'z' || '0' <= r && r <= '9' || r == '-'
}
