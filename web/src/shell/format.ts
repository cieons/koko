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
