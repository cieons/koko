import type { Language } from "../i18n/catalogues";

/**
 * formatDate shows a date as YYYY-MM-DD, in every language: a calendar date
 * as stored, and a time the API returns (RFC 3339 in UTC) as its UTC date,
 * the date the CLI prints at its start. A value that is neither shows as
 * stored.
 */
export function formatDate(value: string): string {
  return /^(\d{4}-\d{2}-\d{2})(?:T|$)/.exec(value)?.[1] ?? value;
}

/** DurationFormat is Intl.DurationFormat (ES2025), which TypeScript's ES2023 library does not declare. */
interface DurationFormat {
  format(duration: { days: number; hours: number }): string;
}

const { DurationFormat } = Intl as unknown as {
  DurationFormat: new (language: Language, options: { style: "long" }) => DurationFormat;
};

/**
 * formatTimeLeft shows how long something shown once stays valid, from a
 * whole number of seconds: as minutes and seconds, such as 14:59, for an
 * hour or less, and otherwise in days and whole hours in the language,
 * such as "6 days, 23 hours", leaving out a part that is zero.
 */
export function formatTimeLeft(seconds: number, language: Language): string {
  if (seconds <= 3600) {
    return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
  }
  const hours = Math.floor(seconds / 3600);
  return new DurationFormat(language, { style: "long" }).format({ days: Math.floor(hours / 24), hours: hours % 24 });
}

/**
 * formatAgo shows how long ago a time the API returns (RFC 3339) was, in the
 * language, such as "3 days ago" or "yesterday": in minutes for its first
 * hour, in hours for its first day, then in days, months once days say
 * less, and years after two. A time to come reads as this minute.
 */
export function formatAgo(value: string, language: Language, now = Date.now()): string {
  const relative = new Intl.RelativeTimeFormat(language, { numeric: "auto" });
  // A time ahead of this browser's clock, as when the koko server's runs
  // ahead, reads as now rather than as one to come.
  const seconds = Math.min(0, Math.round((Date.parse(value) - now) / 1000));
  const days = Math.round(seconds / 86_400);
  if (Math.abs(seconds) < 3600) {
    return relative.format(Math.round(seconds / 60), "minute");
  }
  if (Math.abs(seconds) < 86_400) {
    return relative.format(Math.round(seconds / 3600), "hour");
  }
  if (Math.abs(days) < 60) {
    return relative.format(days, "day");
  }
  if (Math.abs(days) < 730) {
    return relative.format(Math.round(days / 30.44), "month");
  }
  return relative.format(Math.round(days / 365.25), "year");
}
