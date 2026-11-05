// Package record holds the record's value rules, shared by the koko server
// and the CLI so that both judge a value alike (P0 spec: the record): a
// Host's address, port and Host Account, Attribute keys, the Preset
// Attributes and the four types of values (parsing a value, writing it and
// comparing two), and the Note. The koko server checks every rule on every
// write, whichever client sent it; the CLI checks them first, to refuse a
// bad command line before any call.
package record

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// DefaultPort is a Host's port when none is given.
const DefaultPort = 22

// The limits of the record's values, in bytes of UTF-8.
const (
	// MaxText is the longest free-text Attribute value: 1 KiB.
	MaxText = 1 << 10
	// MaxNote is the longest Note: 64 KiB.
	MaxNote = 64 << 10
	// MaxAccount is the longest Host Account: a Unix user name's 32.
	MaxAccount = 32
	// maxDNSName is the longest DNS name, without a trailing dot.
	maxDNSName = 253
	// maxKey is the longest Attribute key.
	maxKey = 63
)

// reserved are the keys an Attribute cannot take: the name of every field of
// a Host and of a Service, so that a key=value word or a filter never
// confuses a field with an Attribute.
var reserved = []string{
	"id", "name", "space", "address", "port", "account", "relay", "state", "host_key",
	"host", "ports", "attributes", "note", "created_at", "updated_at",
}

// Required are the fields a Host always has, with no default to fall back
// on, so --unset never clears them: a port cleared goes back to DefaultPort.
var Required = []string{"address", "account"}

// Reserved tells whether key is the name of a field, which no Attribute key
// may be.
func Reserved(key string) bool {
	return slices.Contains(reserved, key)
}

// CheckKey reports why key is not an Attribute key, or nil when it is one:
// snake_case, 1 to 63 of a-z, 0-9 and '_', starting with a letter, and not
// the name of a field. No key holds '.', which filters such as key.le= use.
func CheckKey(key string) error {
	switch {
	case key == "":
		return errors.New("an Attribute key cannot be empty")
	case len(key) > maxKey:
		return fmt.Errorf("the Attribute key %q is longer than %d characters", key, maxKey)
	case key[0] < 'a' || key[0] > 'z':
		return fmt.Errorf("the Attribute key %q does not start with a letter a-z", key)
	case Reserved(key):
		return fmt.Errorf("%q is the name of a field, which no Attribute key may be", key)
	}
	for _, r := range key {
		if !isKeyRune(r) {
			return fmt.Errorf("the Attribute key %q holds %q: a key is snake_case, of a-z, 0-9 and '_'", key, r)
		}
	}
	return nil
}

// isKeyRune, isLabelRune and isAccountRune tell whether r may stand in an
// Attribute key, a label of a DNS name and a Host Account.
func isKeyRune(r rune) bool {
	return 'a' <= r && r <= 'z' || '0' <= r && r <= '9' || r == '_'
}

func isLabelRune(r rune) bool {
	return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || r == '-'
}

func isAccountRune(r rune) bool {
	return isLabelRune(r) || r == '.' || r == '_'
}

// checkText reports why value is not free text, the kind of every Attribute
// key that is not a Preset's, or nil when it is: 1 byte to 1 KiB of UTF-8 on
// one line, with no control characters, so that it prints as it is on a
// terminal. Longer or multi-line text belongs in the Note.
func checkText(value string) error {
	switch {
	case value == "":
		return errors.New("a value cannot be empty")
	case len(value) > MaxText:
		return fmt.Errorf("the value is longer than the %d bytes of free text: longer text belongs in the Note", MaxText)
	case !utf8.ValidString(value):
		return errors.New("the value is not UTF-8")
	}
	if r, ok := control(value, nil); ok {
		return fmt.Errorf("the value holds the control character %q: free text is one line, and more lines belong in the Note", r)
	}
	return nil
}

// CheckNote reports why note is not a Note, or nil when it is one: 1 byte to
// 64 KiB of UTF-8, on as many lines as it likes, with no control character
// but tab, line feed and carriage return. An empty Note is refused, so a
// pipe from a command that failed never clears one; clearing is explicit.
func CheckNote(note string) error {
	switch {
	case note == "":
		return errors.New("a Note cannot be empty")
	case len(note) > MaxNote:
		return fmt.Errorf("the Note is longer than the %d bytes a Note holds", MaxNote)
	case !utf8.ValidString(note):
		return errors.New("the Note is not UTF-8")
	}
	if r, ok := control(note, []rune{'\t', '\n', '\r'}); ok {
		return fmt.Errorf("the Note holds the control character %q", r)
	}
	return nil
}

// control returns the first control character in s that allowed does not
// hold.
func control(s string, allowed []rune) (rune, bool) {
	for _, r := range s {
		if unicode.IsControl(r) && !slices.Contains(allowed, r) {
			return r, true
		}
	}
	return 0, false
}

// Address returns a Host's address in the one form koko stores, or why s is
// not one: an IP address without a zone, canonical (2001:db8::1), or a DNS
// name in lowercase, whose last label is not all digits, so a mistyped IPv4
// address is never taken for one.
func Address(s string) (string, error) {
	if ip, err := netip.ParseAddr(s); err == nil {
		if ip.Zone() != "" {
			return "", fmt.Errorf("the address %q names a zone, which koko does not take", s)
		}
		return ip.String(), nil
	}
	switch {
	case s == "":
		return "", errors.New("an address cannot be empty")
	case len(s) > maxDNSName:
		return "", fmt.Errorf("the address %q is longer than the %d characters of a DNS name", s, maxDNSName)
	}
	labels := strings.Split(s, ".")
	for _, l := range labels {
		if err := checkLabel(l); err != nil {
			return "", fmt.Errorf("the address %q is neither an IP address nor a DNS name: %w", s, err)
		}
	}
	if _, err := strconv.ParseUint(labels[len(labels)-1], 10, 64); err == nil {
		return "", fmt.Errorf("the address %q is neither an IP address nor a DNS name, whose last label is not all digits", s)
	}
	return strings.ToLower(s), nil
}

// checkLabel reports why l is not a label of a DNS name.
func checkLabel(l string) error {
	switch {
	case l == "":
		return errors.New("it has an empty label")
	case len(l) > 63:
		return fmt.Errorf("the label %q is longer than 63 characters", l)
	case l[0] == '-' || l[len(l)-1] == '-':
		return fmt.Errorf("the label %q starts or ends with '-'", l)
	}
	for _, r := range l {
		if !isLabelRune(r) {
			return fmt.Errorf("the label %q holds %q", l, r)
		}
	}
	return nil
}

// CheckPort reports why port is not a Host's port, or nil when it is one: 1
// to 65535.
func CheckPort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("the port %d is not between 1 and 65535", port)
	}
	return nil
}

// ParsePort reads a port written in decimal, as key=value gives it.
func ParsePort(s string) (int, error) {
	port, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("the port %q is not a whole number", s)
	}
	return port, CheckPort(port)
}

// CheckAccount reports why s is not a Host Account, or nil when it is one:
// a user name as Linux's useradd takes one, 1 to 32 of a-z, A-Z, 0-9, '.',
// '_' and '-', not starting with '-'. There is no default: koko never
// guesses one such as root.
func CheckAccount(s string) error {
	switch {
	case s == "":
		return errors.New("a Host Account cannot be empty")
	case len(s) > MaxAccount:
		return fmt.Errorf("the Host Account %q is longer than %d characters", s, MaxAccount)
	case s[0] == '-':
		return fmt.Errorf("the Host Account %q starts with '-'", s)
	}
	for _, r := range s {
		if !isAccountRune(r) {
			return fmt.Errorf("the Host Account %q holds %q: a Host Account uses a-z, A-Z, 0-9, '.', '_' and '-'", s, r)
		}
	}
	return nil
}
