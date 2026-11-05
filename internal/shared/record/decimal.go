package record

import (
	"cmp"
	"fmt"
	"strings"
)

// The limits of an exact decimal. Within them every decimal is exactly one
// JavaScript number, which prints back as the same decimal, so the web UI
// carries a number as a TypeScript number without changing it: at most 15
// digits keep it within a binary float's 15 exact decimal digits, at most 6
// after the point keep it from 0.000001 up, and so in the plain form a
// JavaScript number prints with no exponent.
const (
	MaxDecimalDigits   = 15
	MaxDecimalFraction = 6
)

// ParseDecimal returns s, an exact decimal, in its one form, or why it is not
// one. s is plain decimal: an optional '-', digits, and optionally '.' and
// more digits, with no '+' and no exponent; at most MaxDecimalDigits digits,
// and at most MaxDecimalFraction of them after the point, not counting the
// zeros the one form drops. The one form has no leading zeros (but the 0 of
// a decimal below 1), no trailing zeros after the point, no point with
// nothing after it, and no '-' before 0: 1.10 is 1.1, and -0 is 0.
func ParseDecimal(s string) (string, error) {
	digits, negative := strings.CutPrefix(s, "-")
	whole, fraction, point := strings.Cut(digits, ".")
	if whole == "" || point && fraction == "" || !allDigits(whole) || !allDigits(fraction) {
		return "", fmt.Errorf("%q is not a decimal number: write digits, a '-' before them for one below 0, and a '.' and more digits for a fraction, such as 9.5", s)
	}
	whole, fraction = strings.TrimLeft(whole, "0"), strings.TrimRight(fraction, "0")
	switch {
	case len(whole)+len(fraction) > MaxDecimalDigits:
		return "", fmt.Errorf("%s has more than the %d digits a number holds", s, MaxDecimalDigits)
	case len(fraction) > MaxDecimalFraction:
		return "", fmt.Errorf("%s has more than the %d digits after the point a number holds", s, MaxDecimalFraction)
	}
	d := cmp.Or(whole, "0")
	if fraction != "" {
		d += "." + fraction
	}
	if negative && d != "0" {
		d = "-" + d
	}
	return d, nil
}

func allDigits(s string) bool {
	for i := range len(s) {
		if !isDigit(s[i]) {
			return false
		}
	}
	return true
}

// compareDecimal compares two decimals in the form ParseDecimal returns, by
// value.
func compareDecimal(a, b string) int {
	ma, negA := strings.CutPrefix(a, "-")
	mb, negB := strings.CutPrefix(b, "-")
	switch {
	case negA && !negB:
		return -1
	case !negA && negB:
		return 1
	case negA:
		return compareMagnitude(mb, ma)
	}
	return compareMagnitude(ma, mb)
}

// compareMagnitude compares two decimals at least 0 in their one form: the
// longer whole part is larger, and so, past equal whole parts, is the
// fraction that sorts later, since neither ends in a zero.
func compareMagnitude(a, b string) int {
	wa, fa, _ := strings.Cut(a, ".")
	wb, fb, _ := strings.Cut(b, ".")
	return cmp.Or(cmp.Compare(len(wa), len(wb)), strings.Compare(wa, wb), strings.Compare(fa, fb))
}
