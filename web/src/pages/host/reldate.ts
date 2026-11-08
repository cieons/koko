// Dates in a filter, as the CLI reads them (internal/client/cli/reldate.go):
// YYYY-MM-DD, today, or a relative form such as +30d, -2w, +3m or +1y. The
// API compares only absolute dates, so the page resolves a relative one in
// the browser's time zone before each call, and keeps it as written in the
// URL, so a bookmarked "expires within 30 days" view stays relative.

/** relative is a relative date's form: a sign, at most 6 digits and a unit. */
const relative = /^([+-])(\d{1,6})([dwmy])$/;

/** absolute is a date's written form, YYYY-MM-DD. */
const absolute = /^(\d{4})-(\d{2})-(\d{2})$/;

/**
 * resolveDate returns s as an absolute date, YYYY-MM-DD, or undefined when
 * s is no date: s itself when it is a calendar date from 0001-01-01 to
 * 9999-12-31, or today or a relative date counted from now's date in
 * timeZone, an IANA name. A month or a year that lands past the end of its
 * month clamps to the month's last day, so +1m from 2026-01-31 is
 * 2026-02-28.
 */
export function resolveDate(s: string, now: Date, timeZone: string): string | undefined {
  const written = absolute.exec(s);
  if (written) {
    const [y, m, d] = written.slice(1).map(Number) as [number, number, number];
    return y >= 1 && formatDate(utcDate(y, m - 1, d)) === s ? s : undefined;
  }
  if (s !== "today" && !relative.test(s)) {
    return undefined;
  }
  const [y, m, d] = dateIn(now, timeZone);
  if (s === "today") {
    return formatDate(utcDate(y, m, d));
  }
  const [, sign, count, unit] = relative.exec(s) as unknown as [string, string, string, string];
  const n = sign === "-" ? -Number(count) : Number(count);
  const date =
    unit === "d"
      ? utcDate(y, m, d + n)
      : unit === "w"
        ? utcDate(y, m, d + 7 * n)
        : addMonths(y, m, d, unit === "m" ? n : 12 * n);
  const year = date.getUTCFullYear();
  return year >= 1 && year <= 9999 ? formatDate(date) : undefined;
}

/** browserTimeZone is the time zone the browser shows local times in. */
export function browserTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone;
}

/** dateIn is now's calendar date in timeZone: its year, month from 0, and day. */
function dateIn(now: Date, timeZone: string): [number, number, number] {
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone,
    year: "numeric",
    month: "numeric",
    day: "numeric",
  }).formatToParts(now);
  const part = (type: Intl.DateTimeFormatPartTypes) => Number(parts.find((p) => p.type === type)?.value);
  return [part("year"), part("month") - 1, part("day")];
}

/**
 * utcDate is the date y-m-d at midnight UTC, a day or a month out of range
 * carrying into the next, as Go's time.Date does. Unlike Date.UTC, it takes
 * the years 0 to 99 as they are.
 */
function utcDate(y: number, m: number, d: number): Date {
  const date = new Date(0);
  date.setUTCFullYear(y, m, d);
  return date;
}

/** addMonths is the date n months from y-m-d, clamped to the last day of the month it lands in. */
function addMonths(y: number, m: number, d: number, n: number): Date {
  const first = utcDate(y, m + n, 1);
  const last = utcDate(first.getUTCFullYear(), first.getUTCMonth() + 1, 0).getUTCDate();
  return utcDate(first.getUTCFullYear(), first.getUTCMonth(), Math.min(d, last));
}

/** formatDate writes a date at midnight UTC as YYYY-MM-DD. */
function formatDate(date: Date): string {
  const pad = (n: number, width: number) => String(n).padStart(width, "0");
  return `${pad(date.getUTCFullYear(), 4)}-${pad(date.getUTCMonth() + 1, 2)}-${pad(date.getUTCDate(), 2)}`;
}
