/**
 * formatDate shows a date as YYYY-MM-DD, in every language: a calendar date
 * as stored, and a time the API returns (RFC 3339 in UTC) as its UTC date,
 * the date the CLI prints at its start. A value that is neither shows as
 * stored.
 */
export function formatDate(value: string): string {
  return /^(\d{4}-\d{2}-\d{2})(?:T|$)/.exec(value)?.[1] ?? value;
}
