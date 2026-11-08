import { expect, test } from "vitest";

import cases from "../../../../internal/client/cli/testdata/relative-dates.json";
import { resolveDate } from "./reldate";

// The relative dates, through the table the CLI's S1 tests run too
// (TestRelativeDatesResolveInTheCallersTimeZone), so the two resolvers
// cannot drift.

test.each(cases)("$case: $date at $now in $tz", ({ now, tz, date, want }) => {
  expect(resolveDate(date, new Date(now), tz)).toBe(want ?? undefined);
});
