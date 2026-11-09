package cli

import (
	"strconv"
	"strings"
	"time"

	"github.com/cieons/koko/internal/shared/api"
)

// validFlag is --valid, which sets how long the code or link a command
// issues stays valid (ADR 0044); what names that code or link, such as "the
// Join Code". A command that takes it reads it with Input.validFor.
func validFlag(what string) Flag {
	return Flag{
		Name:    "valid",
		Value:   "duration",
		Summary: "how long " + what + " stays valid: a whole number of minutes, hours or days from 1m to 7d, such as 12h; 15m if not given",
	}
}

// validityUnits are the units --valid takes, by their letter.
var validityUnits = map[string]time.Duration{"m": time.Minute, "h": time.Hour, "d": 24 * time.Hour}

// validFor reads --valid as a request's valid_for, in seconds: nil when it
// is not given, so the koko server's default applies. Anything but a whole
// number of minutes, hours or days from api.MinValidity to api.MaxValidity
// is refused before any call.
func (in *Input) validFor() (*int, error) {
	given := in.Flag("valid")
	if given == "" {
		return nil, nil
	}
	d, ok := parseValidity(given)
	if !ok {
		return nil, usagef("--valid takes a whole number of minutes, hours or days from 1m to 7d, such as 30m, 12h or 7d, not %q", given)
	}
	return new(int(d / time.Second)), nil
}

// parseValidity reads a validity written as --valid takes it, such as 30m,
// 12h or 7d, and says whether it is one within api.MinValidity to
// api.MaxValidity.
func parseValidity(s string) (time.Duration, bool) {
	unit, ok := validityUnits[s[len(s)-1:]]
	digits := s[:len(s)-1]
	if !ok || digits == "" || strings.Trim(digits, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n > int(api.MaxValidity/unit) {
		return 0, false
	}
	d := time.Duration(n) * unit
	return d, d >= api.MinValidity
}
