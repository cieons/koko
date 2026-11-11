import { act, configure, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";

import { expectAccessible } from "../../test/axe";
import { fakeApi } from "../../test/fakeApi";
import { clickOutside, offeredOptions, searchAndChoose } from "../../test/controls";
import { actAs, renderAt } from "../../test/pages";
import {
  alice,
  chips,
  host,
  listRequests,
  pick,
  query,
  records,
  selectButton,
} from "./records.fixture";

// Filter on the Hosts page: every comparison of every type built in its
// popover goes into the URL as the CLI writes the term, and comes back from
// the URL as the same chip and the same filter sent to host.list; a
// relative date stays as written and is resolved on the day the view
// opens, in the browser's time zone; a value its key does not take is
// refused, naming the term.

// A test here renders the whole page and drives several overlays in turn;
// its limits are ceilings for a slow runner, not waits: each step waits for
// its own outcome, and the first render of a file's page takes longest.
vi.setConfig({ testTimeout: 20_000 });
configure({ asyncUtilTimeout: 5_000 });

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllEnvs();
});

const hosts = { hosts: [host("web1", { attributes: { price: 4.5 } })] };

/** build opens Filter and adds the term of key, the comparison named op and, when given, value. */
async function build(key: string, op: string, value?: string): Promise<void> {
  fireEvent.click(screen.getByRole("button", { name: "Filter" }));
  await screen.findByRole("dialog", { name: "Add a filter" });
  await searchAndChoose("Field or Attribute", key, key);
  if (op !== "is") {
    await pick("Comparison", op);
  }
  if (value !== undefined) {
    if (key === "state") {
      await pick("Value", value);
    } else if (key === "currency") {
      await searchAndChoose("Value", value, value);
    } else {
      fireEvent.change(await screen.findByRole("textbox", { name: "Value" }), { target: { value } });
    }
  }
  fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add a filter" })).toBeNull());
}

/**
 * cases are a term of every comparison of every type: text (name), an enum
 * (state, of few values, and currency, of many, between them), a number
 * (price) and a date (expires_at), with the term as the URL writes it, its chip and the
 * filter host.list is sent, on 2026-11-07 in Shanghai.
 */
const cases = [
  ["name", "is", "web1", "name=web1", "name = web1", { key: "name", op: "eq", value: "web1" }],
  ["name", "is not", "db1", "name!=db1", "name ≠ db1", { key: "name", op: "ne", value: "db1" }],
  ["name", "has a value", undefined, "name.has", "name has a value", { key: "name", op: "has" }],
  ["name", "has no value", undefined, "name.lacks", "name has no value", { key: "name", op: "lacks" }],
  ["state", "is", "managed", "state=managed", "state = managed", { key: "state", op: "eq", value: "managed" }],
  ["state", "is not", "retired", "state!=retired", "state ≠ retired", { key: "state", op: "ne", value: "retired" }],
  ["currency", "is", "EUR", "currency=EUR", "currency = EUR", { key: "currency", op: "eq", value: "EUR" }],
  ["currency", "is not", "USD", "currency!=USD", "currency ≠ USD", { key: "currency", op: "ne", value: "USD" }],
  ["state", "has no value", undefined, "state.lacks", "state has no value", { key: "state", op: "lacks" }],
  ["currency", "has a value", undefined, "currency.has", "currency has a value", { key: "currency", op: "has" }],
  ["price", "is", "4.5", "price=4.5", "price = 4.5", { key: "price", op: "eq", value: 4.5 }],
  ["price", "is not", "0", "price!=0", "price ≠ 0", { key: "price", op: "ne", value: 0 }],
  ["price", "is less than", "10", "price.lt=10", "price < 10", { key: "price", op: "lt", value: 10 }],
  ["price", "is at most", "10.50", "price.le=10.50", "price ≤ 10.50", { key: "price", op: "le", value: 10.5 }],
  ["price", "is more than", "1", "price.gt=1", "price > 1", { key: "price", op: "gt", value: 1 }],
  ["price", "is at least", "2", "price.ge=2", "price ≥ 2", { key: "price", op: "ge", value: 2 }],
  ["price", "has a value", undefined, "price.has", "price has a value", { key: "price", op: "has" }],
  ["price", "has no value", undefined, "price.lacks", "price has no value", { key: "price", op: "lacks" }],
  ["expires_at", "is", "today", "expires_at=today", "expires_at = today (2026-11-07)", { key: "expires_at", op: "eq", value: "2026-11-07" }],
  ["expires_at", "is not", "2026-12-01", "expires_at!=2026-12-01", "expires_at ≠ 2026-12-01", { key: "expires_at", op: "ne", value: "2026-12-01" }],
  ["expires_at", "is less than", "-2w", "expires_at.lt=-2w", "expires_at < -2w (2026-10-24)", { key: "expires_at", op: "lt", value: "2026-10-24" }],
  ["expires_at", "is at most", "+30d", "expires_at.le=+30d", "expires_at ≤ +30d (2026-12-07)", { key: "expires_at", op: "le", value: "2026-12-07" }],
  ["expires_at", "is more than", "+3m", "expires_at.gt=+3m", "expires_at > +3m (2027-02-07)", { key: "expires_at", op: "gt", value: "2027-02-07" }],
  ["expires_at", "is at least", "+1y", "expires_at.ge=+1y", "expires_at ≥ +1y (2027-11-07)", { key: "expires_at", op: "ge", value: "2027-11-07" }],
  ["expires_at", "has a value", undefined, "expires_at.has", "expires_at has a value", { key: "expires_at", op: "has" }],
  ["expires_at", "has no value", undefined, "expires_at.lacks", "expires_at has no value", { key: "expires_at", op: "lacks" }],
] as const;

test("every comparison of every type goes from Filter into the URL as the CLI writes it, and back from the URL, a relative date resolved on a later day too", async () => {
  vi.useFakeTimers({ toFake: ["Date"], shouldAdvanceTime: true });
  vi.setSystemTime(new Date("2026-11-06T20:00:00Z")); // already 2026-11-07 in Shanghai
  vi.stubEnv("TZ", "Asia/Shanghai");
  const api = fakeApi();
  actAs(api, alice);
  records(api, hosts);
  const first = renderAt("/hosts");
  await screen.findByRole("table", { name: "ops" });

  for (const [key, op, value, written] of cases) {
    await build(key, op, value);
    expect(query().at(-1), written).toEqual(["filter", written]);
  }
  await screen.findByRole("table", { name: "ops" });
  expect(chips()).toEqual(cases.map((c) => c[4]));
  expect(listRequests(api).at(-1)).toEqual({ filters: cases.map((c) => c[5]), with_note: true });

  // The bookmark, opened two days later, keeps its relative dates as written.
  const bookmark = window.location.pathname + window.location.search;
  first.unmount();
  vi.setSystemTime(new Date("2026-11-08T20:00:00Z"));
  renderAt(bookmark);
  await screen.findByRole("table", { name: "ops" });

  expect(query()).toEqual(cases.map((c) => ["filter", c[3]]));
  expect(chips().filter((c) => c.startsWith("expires_at"))).toEqual([
    "expires_at = today (2026-11-09)",
    "expires_at ≠ 2026-12-01",
    "expires_at < -2w (2026-10-26)",
    "expires_at ≤ +30d (2026-12-09)",
    "expires_at > +3m (2027-02-09)",
    "expires_at ≥ +1y (2027-11-09)",
    "expires_at has a value",
    "expires_at has no value",
  ]);
  const filters = (listRequests(api).at(-1) as { filters: unknown[] }).filters;
  expect(filters.slice(18, 24)).toEqual([
    { key: "expires_at", op: "eq", value: "2026-11-09" },
    { key: "expires_at", op: "ne", value: "2026-12-01" },
    { key: "expires_at", op: "lt", value: "2026-10-26" },
    { key: "expires_at", op: "le", value: "2026-12-09" },
    { key: "expires_at", op: "gt", value: "2027-02-09" },
    { key: "expires_at", op: "ge", value: "2027-11-09" },
  ]);

  // A chip's button takes its term away, and the others stay as written.
  fireEvent.click(screen.getByRole("button", { name: "Remove the filter price < 10" }));
  expect(query()).toEqual(cases.filter((c) => c[3] !== "price.lt=10").map((c) => ["filter", c[3]]));
}, 60_000);

test("a term as written stays so beside one added", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, hosts);
  renderAt("/hosts?filter=price.gt%3D10.50");
  await screen.findByRole("table", { name: "ops" });

  await build("name", "is", "web1");

  expect(query()).toEqual([
    ["filter", "price.gt=10.50"],
    ["filter", "name=web1"],
  ]);
});

test("each key offers only the comparisons its type takes, an enum its values, and a date the date it resolves to today", async () => {
  vi.useFakeTimers({ toFake: ["Date"], shouldAdvanceTime: true });
  vi.setSystemTime(new Date("2026-11-06T20:00:00Z"));
  vi.stubEnv("TZ", "Asia/Shanghai");
  const api = fakeApi();
  actAs(api, alice);
  records(api, hosts);
  renderAt("/hosts");
  await screen.findByRole("table", { name: "ops" });
  fireEvent.click(screen.getByRole("button", { name: "Filter" }));
  await screen.findByRole("dialog", { name: "Add a filter" });
  await expectAccessible();

  const comparisons = async (key: string) => {
    await searchAndChoose("Field or Attribute", key, key);
    fireEvent.click(selectButton("Comparison"));
    await screen.findByRole("listbox");
    const offered = offeredOptions();
    fireEvent.keyDown(screen.getByRole("listbox"), { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
    return offered;
  };
  const equality = ["is", "is not", "has a value", "has no value"];
  const ordered = ["is", "is not", "is less than", "is at most", "is more than", "is at least", "has a value", "has no value"];
  expect(await comparisons("cores")).toEqual(equality);
  expect(await comparisons("billing_cycle")).toEqual(equality);
  expect(await comparisons("port")).toEqual(ordered);
  expect(await comparisons("expires_at")).toEqual(ordered);

  const date = await screen.findByRole("textbox", { name: "Value" });
  fireEvent.change(date, { target: { value: "+30d" } });
  expect(screen.getByText("Today this is 2026-12-07.")).toBeDefined();
  fireEvent.click(screen.getByRole("button", { name: "Open the calendar" }));
  const grid = await screen.findByRole("grid");
  await expectAccessible();
  const day = within(grid)
    .getAllByRole("button")
    .find((b) => b.textContent === "15");
  if (!day) {
    throw new Error("no 15th in the calendar");
  }
  fireEvent.click(day);
  await waitFor(() => expect((date as HTMLInputElement).value).toBe("2026-12-15"));

  await searchAndChoose("Field or Attribute", "billing", "billing_cycle");
  fireEvent.click(selectButton("Value"));
  await screen.findByRole("listbox");
  expect(offeredOptions()).toEqual(["monthly", "quarterly", "semiannual", "yearly", "biennial", "triennial"]);
});

test("a value its key does not take is refused in the popover, naming the term, and the URL is kept", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, hosts);
  renderAt("/hosts?filter=region%3Dfsn1");
  await screen.findByRole("table", { name: "ops" });

  fireEvent.click(screen.getByRole("button", { name: "Filter" }));
  await screen.findByRole("dialog", { name: "Add a filter" });
  await searchAndChoose("Field or Attribute", "expires", "expires_at");
  const date = await screen.findByRole("textbox", { name: "Value" });
  fireEvent.change(date, { target: { value: "tomorrow" } });
  fireEvent.click(screen.getByRole("button", { name: "Add filter" }));

  expect(
    await screen.findByText("expires_at=tomorrow is not a filter this list takes: the value is not of its key's type."),
  ).toBeDefined();
  expect(document.activeElement).toBe(date);
  await expectAccessible();

  await searchAndChoose("Field or Attribute", "price", "price");
  await pick("Comparison", "is more than");
  fireEvent.change(screen.getByRole("textbox", { name: "Value" }), { target: { value: "1.1234567" } });
  fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
  expect(
    await screen.findByText("price.gt=1.1234567 is not a filter this list takes: the value is not of its key's type."),
  ).toBeDefined();

  fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
  expect(query()).toEqual([["filter", "region=fsn1"]]);
});

test("an outside click closes Filter, after a key is chosen in it too, and a select's list, the focus going back to what opened each", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, hosts);
  renderAt("/hosts");
  await screen.findByRole("table", { name: "ops" });
  const filter = screen.getByRole("button", { name: "Filter" });

  act(() => filter.focus());
  fireEvent.click(filter);
  await screen.findByRole("dialog", { name: "Add a filter" });
  await clickOutside();
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add a filter" })).toBeNull());
  await waitFor(() => expect(document.activeElement).toBe(filter));

  // The focus stays in the field the key was chosen in, which the click
  // takes it from and the popover gives it back to.
  fireEvent.click(filter);
  await screen.findByRole("dialog", { name: "Add a filter" });
  await searchAndChoose("Field or Attribute", "price", "price");
  await clickOutside();
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add a filter" })).toBeNull());
  await waitFor(() => expect(document.activeElement).toBe(filter));
  expect(query()).toEqual([]);

  const state = selectButton("State");
  act(() => state.focus());
  fireEvent.click(state);
  await screen.findByRole("listbox");
  await clickOutside();
  await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
  await waitFor(() => expect(document.activeElement).toBe(state));
  expect(state.textContent).toContain("In use");
});
