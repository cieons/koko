package cli

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cieons/koko/internal/shared/record"
)

// relativeDateForms names the relative dates, for messages.
const relativeDateForms = "today, or a sign, a whole number and d, w, m or y, such as +30d, -2w, +3m or +1y"

// maxRelativeDigits is the most digits a relative date's count has, which
// keeps its arithmetic far from overflowing.
const maxRelativeDigits = 6

// resolveDate returns s as an absolute date, YYYY-MM-DD: s itself when it is
// not relative, or a relative date resolved against today, which it asks
// for only then. A relative date is today, or a count of days (+30d), weeks
// (-2w), months (+3m) or years (+1y) from it; a month or a year that lands
// past the end of its month clamps to the month's last day, so +1m from
// 2026-01-31 is 2026-02-28. The API compares only absolute dates, so the
// CLI resolves every relative one before any call.
func resolveDate(s string, today func() (time.Time, error)) (string, error) {
	sign := s[:min(len(s), 1)]
	if s != "today" && sign != "+" && sign != "-" {
		return s, nil
	}
	now, err := today()
	if err != nil {
		return "", err
	}
	y, m, d := now.Date()
	if s == "today" {
		return date(y, m, d), nil
	}
	count, unit := s[1:max(1, len(s)-1)], s[len(s)-1]
	if count == "" || len(count) > maxRelativeDigits || strings.Trim(count, "0123456789") != "" {
		return "", usagef("%q is not a relative date: write %s, with at most %d digits", s, relativeDateForms, maxRelativeDigits)
	}
	n, err := strconv.Atoi(count)
	if err != nil {
		return "", usagef("%q is not a relative date: %v", s, err)
	}
	if sign == "-" {
		n = -n
	}
	var t time.Time
	switch unit {
	case 'd':
		t = time.Date(y, m, d+n, 0, 0, 0, 0, time.UTC)
	case 'w':
		t = time.Date(y, m, d+7*n, 0, 0, 0, 0, time.UTC)
	case 'm':
		t = addMonths(y, m, d, n)
	case 'y':
		t = addMonths(y, m, d, 12*n)
	default:
		return "", usagef("%q is not a relative date: write %s", s, relativeDateForms)
	}
	if t.Year() < 1 || t.Year() > 9999 {
		return "", usagef("%s from %s is %s, outside the dates from 0001-01-01 to 9999-12-31", s, date(y, m, d), t.Format(time.DateOnly))
	}
	return t.Format(time.DateOnly), nil
}

// addMonths returns the date n months from y-m-d, clamped to the last day of
// the month it lands in.
func addMonths(y int, m time.Month, d, n int) time.Time {
	first := time.Date(y, m+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	return time.Date(first.Year(), first.Month(), min(d, last), 0, 0, 0, 0, time.UTC)
}

func date(y int, m time.Month, d int) string {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
}

// today returns now in the caller's time zone, whose date a relative date
// counts from: the clock hook's now, or the system clock's.
func (in *Input) today() (time.Time, error) {
	loc, err := in.inv.location()
	if err != nil {
		return time.Time{}, err
	}
	now := time.Now
	if in.runner.Clock != nil {
		now = in.runner.Clock
	}
	return now().In(loc), nil
}

// location returns the caller's time zone as Go reads TZ for a process
// (package time): TZ unset is the machine's local zone, TZ empty or UTC is
// UTC, and any other TZ names a zone, after an optional ':', by its IANA
// name or by the path of its zoneinfo file. Where Go falls back to UTC for a
// zone it cannot load, the CLI refuses: a date resolved in the wrong zone
// is a day off.
func (inv *Invocation) location() (*time.Location, error) {
	tz, ok := inv.lookupEnv("TZ")
	if !ok {
		return time.Local, nil
	}
	name := strings.TrimPrefix(tz, ":")
	var (
		loc *time.Location
		err error
	)
	switch {
	case name == "", name == "UTC":
		return time.UTC, nil
	case strings.HasPrefix(name, "/"):
		var b []byte
		if b, err = os.ReadFile(name); err == nil {
			loc, err = time.LoadLocationFromTZData(name, b)
		}
	default:
		loc, err = time.LoadLocation(name)
	}
	if err != nil {
		return nil, usagef("TZ=%s names no time zone koko can load, so it cannot tell today's date there: %v", tz, unwrapPath(err))
	}
	return loc, nil
}

// parseValue reads s, a value written on the command line for a key whose
// values are of type t, resolving a relative date first.
func (in *Input) parseValue(t record.Type, s string) (record.Value, error) {
	if t.Kind() == record.KindDate {
		var err error
		if s, err = resolveDate(s, in.today); err != nil {
			return record.Value{}, err
		}
	}
	return t.Parse(s)
}
