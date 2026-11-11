import { expect, test } from "vitest";

import { compareValues, groupBy } from "./group";
import type { KeyType } from "./view";

// The Hosts view's Group by orders its subheadings as host.list sorts
// (record.Type.Compare in internal/shared/record/value.go): numbers by
// value, dates by the calendar, an enum's values in their declared order
// and text in natural order, then the records that lack the key.

const sorted = (type: KeyType, values: (string | number)[]) => [...values].sort((a, b) => compareValues(type, a, b));

test("numbers sort by value, not as text", () => {
  expect(sorted({ type: "number" }, [10.5, 9, "100", 0.25])).toEqual([0.25, 9, 10.5, "100"]);
});

test("dates sort by the calendar", () => {
  expect(sorted({ type: "date" }, ["2027-01-01", "2026-12-01", "2026-02-28"])).toEqual([
    "2026-02-28",
    "2026-12-01",
    "2027-01-01",
  ]);
});

test("an enum's values sort in their declared order", () => {
  expect(sorted({ type: "enum", values: ["monthly", "quarterly", "yearly"] }, ["yearly", "monthly", "quarterly"])).toEqual(
    ["monthly", "quarterly", "yearly"],
  );
});

test("text sorts in natural order, runs of digits by value and the rest byte by byte, as the koko server does", () => {
  expect(sorted({ type: "text" }, ["node10", "node9", "Node", "node", "node09", "nodé", "relay-node"])).toEqual([
    "Node",
    "node",
    "node09",
    "node9",
    "node10",
    "nodé",
    "relay-node",
  ]);
});

test("groupBy gives a group per value in its type's order, then the records without the key", () => {
  const records = [
    { name: "a", price: 12 },
    { name: "b" },
    { name: "c", price: 4.5 },
    { name: "d", price: 12 },
  ];
  const groups = groupBy(records, (r) => r.price, { type: "number" });
  expect(groups.map((g) => [g.value, g.records.map((r) => r.name)])).toEqual([
    [4.5, ["c"]],
    [12, ["a", "d"]],
    [undefined, ["b"]],
  ]);
});
