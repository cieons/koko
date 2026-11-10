import { expect, test } from "vitest";

import { formatAgo, formatDate, formatTimeLeft } from "./format";

test("time left shows as minutes and seconds for an hour or less", () => {
  expect(formatTimeLeft(3600, "en")).toBe("60:00");
  expect(formatTimeLeft(899, "en")).toBe("14:59");
  expect(formatTimeLeft(1, "zh-CN")).toBe("0:01");
});

test("time left beyond an hour shows in days and whole hours, in the language", () => {
  expect(formatTimeLeft(7 * 86400, "en")).toBe("7 days");
  expect(formatTimeLeft(7 * 86400 - 1, "en")).toBe("6 days, 23 hours");
  expect(formatTimeLeft(3601, "en")).toBe("1 hour");
  expect(formatTimeLeft(2 * 3600 + 59 * 60, "en")).toBe("2 hours");
  expect(formatTimeLeft(7 * 86400 - 1, "zh-CN")).toBe("6天23小时");
});

test("a date shows as YYYY-MM-DD: a calendar date as stored, an API time as its UTC date", () => {
  expect(formatDate("2026-12-01")).toBe("2026-12-01");
  expect(formatDate("2026-11-05T23:30:00Z")).toBe("2026-11-05");
});

test("how long ago a time was shows in the language, in the unit that says most", () => {
  const now = Date.parse("2026-11-10T12:00:00Z");
  expect(formatAgo("2026-11-10T11:59:40Z", "en", now)).toBe("this minute");
  expect(formatAgo("2026-11-10T11:55:00Z", "en", now)).toBe("5 minutes ago");
  expect(formatAgo("2026-11-10T09:00:00Z", "en", now)).toBe("3 hours ago");
  expect(formatAgo("2026-11-09T12:00:00Z", "en", now)).toBe("yesterday");
  expect(formatAgo("2026-11-07T12:00:00Z", "zh-CN", now)).toBe("3天前");
  expect(formatAgo("2026-08-10T12:00:00Z", "en", now)).toBe("3 months ago");
  expect(formatAgo("2023-11-10T12:00:00Z", "en", now)).toBe("3 years ago");
  // A koko server whose clock runs ahead.
  expect(formatAgo("2026-11-10T12:03:00Z", "en", now)).toBe("this minute");
});

test("a value that is not a date shows as stored", () => {
  expect(formatDate("")).toBe("");
  expect(formatDate("next spring")).toBe("next spring");
  expect(formatDate("2026-12-01x")).toBe("2026-12-01x");
});
