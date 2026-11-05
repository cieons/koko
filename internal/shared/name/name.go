// Package name holds the rules for names. Users, Spaces and devices take a
// lowercase DNS label (Check); a Secret's name also takes '_' (CheckSecret).
// No name holds '/', which qualified names such as <space>/<secret> use, or
// '=', which key=value words use.
package name

import (
	"errors"
	"fmt"
)

// MaxLen is the longest name: a DNS label's 63 characters.
const MaxLen = 63

// Check reports why s is not a name, or nil when it is one: 1 to 63 of a-z,
// 0-9 and '-', not starting or ending with '-'. Such a name holds no '_',
// which every record id holds.
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

// CheckSecret reports why s is not a Secret's name, or nil when it is one: 1
// to 63 of a-z, 0-9, '-' and '_', starting with a letter or a digit (P0
// spec: Secrets). Such a name is safe in a shell and in a URL.
func CheckSecret(s string) error {
	switch {
	case s == "":
		return errors.New("a Secret's name cannot be empty")
	case len(s) > MaxLen:
		return fmt.Errorf("%q is longer than %d characters", s, MaxLen)
	case s[0] == '-' || s[0] == '_':
		return fmt.Errorf("%q starts with %q: a Secret's name starts with a letter or a digit", s, s[0])
	}
	for _, r := range s {
		if !isNameRune(r) && r != '_' {
			return fmt.Errorf("%q holds %q: a Secret's name uses only a-z, 0-9, '-' and '_'", s, r)
		}
	}
	return nil
}

func isNameRune(r rune) bool {
	return 'a' <= r && r <= 'z' || '0' <= r && r <= '9' || r == '-'
}
