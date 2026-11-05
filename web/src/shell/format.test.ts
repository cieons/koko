import { expect, test } from "vitest";

import { formatDate } from "./format";

test("a date shows as YYYY-MM-DD: a calendar date as stored, an API time as its UTC date", () => {
  expect(formatDate("2026-12-01")).toBe("2026-12-01");
  expect(formatDate("2026-11-05T23:30:00Z")).toBe("2026-11-05");
});

test("a value that is not a date shows as stored", () => {
  expect(formatDate("")).toBe("");
  expect(formatDate("next spring")).toBe("next spring");
  expect(formatDate("2026-12-01x")).toBe("2026-12-01x");
});
