import { fireEvent, screen, within } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";

import { type AttrKey, AttrList } from "../../api/attr";
import { type Host, HostAdd, HostList, type HostListRequest, HostShow } from "../../api/host";
import { SpaceList } from "../../api/space";
import type { User } from "../../api/user";
import { type FakeApi, fakeApi } from "../../test/fakeApi";
import { actAs, renderAt, tableRows } from "../../test/pages";

// The Hosts list, against the fake API: its filter builder, sorting,
// columns and Retired Hosts, all kept in the page's URL.

const alice: User = { id: "user_1", name: "alice", role: "owner", created_at: "2026-11-01T09:00:00Z" };
const bob: User = { id: "user_2", name: "bob", role: "member", created_at: "2026-11-02T09:00:00Z" };

/** attributes are what attr.list answers: the Presets, then a key in use. */
const attributes: AttrKey[] = [
  { key: "provider", type: "text", preset: true, in_use: true },
  { key: "provider_account", type: "text", preset: true, in_use: false },
  { key: "region", type: "text", preset: true, in_use: true },
  { key: "price", type: "number", min: 0, preset: true, in_use: true },
  { key: "currency", type: "enum", values: ["CNY", "EUR", "USD"], preset: true, in_use: true },
  {
    key: "billing_cycle",
    type: "enum",
    values: ["monthly", "quarterly", "semiannual", "yearly", "biennial", "triennial"],
    preset: true,
    in_use: false,
  },
  { key: "expires_at", type: "date", preset: true, in_use: true },
  { key: "auto_renew", type: "enum", values: ["no", "yes"], preset: true, in_use: false },
  { key: "cores", type: "text", preset: false, in_use: true },
];

function host(name: string, fields: Partial<Host> = {}): Host {
  return {
    id: `host_${name}`,
    name,
    space: "ops",
    address: `${name}.example.com`,
    port: 22,
    account: "deploy",
    relay: null,
    state: "managed",
    host_key: null,
    attributes: {},
    created_at: "2026-11-01T09:00:00Z",
    updated_at: "2026-11-01T09:00:00Z",
    ...fields,
  };
}

const web1 = host("web1", {
  attributes: { provider: "hetzner", region: "fsn1", price: 4.5, currency: "EUR", expires_at: "2026-12-01", cores: "8" },
});
const db1 = host("db1", { space: "bob", state: "registered", attributes: { provider: "vultr", price: 12 } });
const old1 = host("web1", { id: "host_old1", state: "retired", attributes: { provider: "hetzner" } });

/**
 * hosts answers attr.list, space.list for the form that adds a Host, and
 * host.list with the Hosts given, the Retired ones only under all.
 */
function hosts(api: FakeApi, list: Host[]): void {
  api.on(AttrList, () => ({ attributes }));
  api.on(SpaceList, () => ({
    spaces: [
      { id: "space_1", name: "bob", personal: true, created_at: "2026-11-02T09:00:00Z" },
      { id: "space_2", name: "ops", personal: false, created_at: "2026-11-02T09:00:00Z" },
    ],
  }));
  api.on(HostList, ({ all }) => ({ hosts: list.filter((h) => all || h.state !== "retired") }));
}

/** lastList is the last host.list request. */
function lastList(api: FakeApi): HostListRequest {
  return api.requests(HostList).at(-1) as HostListRequest;
}

/** query is the page's URL query, decoded, as name=value pairs. */
function query(): string[][] {
  return [...new URLSearchParams(window.location.search).entries()];
}

function choose(label: string, value: string): void {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
}

function options(label: string): string[] {
  return within(screen.getByLabelText(label))
    .getAllByRole("option")
    .map((o) => o.textContent ?? "");
}

function headers(): string[] {
  return within(screen.getByRole("table", { name: "Hosts" }))
    .getAllByRole("columnheader")
    .map((th) => th.textContent ?? "");
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllEnvs();
});

test("a Member sees the Hosts host.list answers them in the default columns, each linking to its page", async () => {
  const api = fakeApi();
  actAs(api, bob);
  hosts(api, [db1, web1]);

  renderAt("/hosts");

  expect(await screen.findByRole("table", { name: "Hosts" })).toBeDefined();
  expect(headers()).toEqual(["name", "space", "state", "address", "provider", "region", "price", "expires_at"]);
  expect(tableRows("Hosts")).toEqual([
    ["db1", "bob", "registered", "db1.example.com", "vultr", "", "12", ""],
    ["web1", "ops", "managed", "web1.example.com", "hetzner", "fsn1", "4.5 EUR", "2026-12-01"],
  ]);
  expect(api.requests(HostList)).toEqual([{}]);
  expect(screen.getByRole("link", { name: "web1" }).getAttribute("href")).toBe("/hosts/web1");
  const nav = screen.getByRole("navigation", { name: "Pages" });
  expect(within(nav).getByRole("link", { name: "Hosts" }).getAttribute("aria-current")).toBe("page");
});

test("a filter built in three steps goes into the URL and comes back from it, its relative date as written", async () => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-11-06T20:00:00Z")); // already 2026-11-07 in Shanghai
  vi.stubEnv("TZ", "Asia/Shanghai");
  const api = fakeApi();
  actAs(api, alice);
  hosts(api, [web1]);
  const first = renderAt("/hosts");
  await screen.findByRole("table", { name: "Hosts" });

  choose("Key", "expires_at");
  choose("Comparison", "le");
  choose("Date", "+30d");
  fireEvent.click(screen.getByRole("button", { name: "Add filter" }));

  choose("Key", "price");
  choose("Comparison", "gt");
  choose("Value", "10");
  fireEvent.click(screen.getByRole("button", { name: "Add filter" }));

  choose("Key", "currency");
  choose("Comparison", "eq");
  choose("Value", "EUR");
  fireEvent.click(screen.getByRole("button", { name: "Add filter" }));

  choose("Key", "region");
  choose("Comparison", "lacks");
  fireEvent.click(screen.getByRole("button", { name: "Add filter" }));

  const written = ["expires_at.le=+30d", "price.gt=10", "currency=EUR", "region.lacks"];
  expect(query()).toEqual(written.map((f) => ["filter", f]));
  const filters = [
    { key: "expires_at", op: "le", value: "2026-12-07" },
    { key: "price", op: "gt", value: 10 },
    { key: "currency", op: "eq", value: "EUR" },
    { key: "region", op: "lacks" },
  ];
  expect(lastList(api)).toEqual({ filters });

  // The bookmark, opened on a later day, keeps its relative date.
  const bookmark = window.location.pathname + window.location.search;
  first.unmount();
  vi.setSystemTime(new Date("2026-11-08T20:00:00Z"));
  renderAt(bookmark);
  await screen.findByRole("table", { name: "Hosts" });

  const terms = screen.getByRole("list", { name: "Filters" });
  expect(within(terms).getAllByRole("listitem").map((li) => li.textContent)).toEqual([
    "expires_at <= +30d (2026-12-09)×",
    "price > 10×",
    "currency = EUR×",
    "region lacks×",
  ]);
  expect(lastList(api)).toEqual({ filters: [{ ...filters[0], value: "2026-12-09" }, ...filters.slice(1)] });
  expect(query()).toEqual(written.map((f) => ["filter", f]));

  fireEvent.click(screen.getByRole("button", { name: "Remove the filter price > 10" }));
  expect(query()).toEqual(["expires_at.le=+30d", "currency=EUR", "region.lacks"].map((f) => ["filter", f]));
});

test("each type offers its comparisons, an enum its values, and a date a picker or a relative form", async () => {
  const api = fakeApi();
  actAs(api, alice);
  hosts(api, [web1]);
  renderAt("/hosts");
  await screen.findByRole("table", { name: "Hosts" });

  const keys = options("Key");
  expect(keys.slice(0, 10)).toEqual([
    "Pick a key",
    "id",
    "name",
    "space",
    "address",
    "port",
    "account",
    "relay",
    "state",
    "host_key",
  ]);
  expect(keys.slice(10)).toEqual([...attributes.map((a) => a.key)]);

  const equality = ["=", "!=", "has", "lacks"];
  const ordered = ["=", "!=", "<", "<=", ">", ">=", "has", "lacks"];
  for (const [key, offered] of [
    ["name", equality],
    ["state", equality],
    ["cores", equality],
    ["currency", equality],
    ["port", ordered],
    ["price", ordered],
    ["expires_at", ordered],
  ] as const) {
    choose("Key", key);
    expect(options("Comparison"), key).toEqual(offered);
  }

  choose("Key", "billing_cycle");
  choose("Comparison", "ne");
  expect(options("Value")).toEqual(["Pick a value", ...(attributes[5]?.values ?? [])]);
  choose("Key", "state");
  expect(options("Value")).toEqual(["Pick a value", "registered", "managed", "retired"]);

  // A value of the wrong type cannot be added.
  choose("Key", "price");
  choose("Comparison", "ge");
  choose("Value", "-1");
  expect(screen.getByRole("button", { name: "Add filter" }).hasAttribute("disabled")).toBe(true);
  choose("Value", "ten");
  expect(screen.getByRole("button", { name: "Add filter" }).hasAttribute("disabled")).toBe(true);

  choose("Key", "expires_at");
  choose("Comparison", "eq");
  choose("Date", "tomorrow");
  expect(screen.getByRole("button", { name: "Add filter" }).hasAttribute("disabled")).toBe(true);
  choose("Pick a date", "2026-12-01");
  expect((screen.getByLabelText("Date") as HTMLInputElement).value).toBe("2026-12-01");
  fireEvent.click(screen.getByRole("button", { name: "Add filter" }));

  expect(query()).toEqual([["filter", "expires_at=2026-12-01"]]);
  expect(lastList(api)).toEqual({ filters: [{ key: "expires_at", op: "eq", value: "2026-12-01" }] });
});

test("a click on a column header sorts by it, and a Shift-click adds a further key", async () => {
  const api = fakeApi();
  actAs(api, alice);
  hosts(api, [web1]);
  renderAt("/hosts");
  await screen.findByRole("table", { name: "Hosts" });
  // The table shows again once host.list answers the new order.
  const header = (key: string) => screen.findByRole("button", { name: key });

  fireEvent.click(await header("price"));
  expect(query()).toEqual([["sort", "price"]]);
  fireEvent.click(await header("price"));
  expect(query()).toEqual([["sort", "price.desc"]]);
  fireEvent.click(await header("expires_at"), { shiftKey: true });
  expect(query()).toEqual([
    ["sort", "price.desc"],
    ["sort", "expires_at"],
  ]);
  expect((await screen.findByRole("columnheader", { name: "price" })).getAttribute("aria-sort")).toBe("descending");
  expect(lastList(api)).toEqual({ sort: [{ key: "price", desc: true }, { key: "expires_at" }] });

  fireEvent.click(await header("price"), { shiftKey: true });
  expect(query()).toEqual([
    ["sort", "price"],
    ["sort", "expires_at"],
  ]);

  fireEvent.click(await header("name"));
  expect(query()).toEqual([["sort", "name"]]);
  await header("name");
  expect(lastList(api)).toEqual({ sort: [{ key: "name" }] });
});

test("the column picker adds any Attribute key and takes a column away, in the URL", async () => {
  const api = fakeApi();
  actAs(api, alice);
  hosts(api, [web1]);
  const first = renderAt("/hosts");
  await screen.findByRole("table", { name: "Hosts" });

  const picker = screen.getByRole("group", { name: "Columns" });
  expect(
    within(picker)
      .getAllByRole("checkbox")
      .map((c) => [c.getAttribute("name"), (c as HTMLInputElement).checked]),
  ).toEqual([
    ["space", true],
    ["state", true],
    ["address", true],
    ["provider", true],
    ["region", true],
    ["price", true],
    ["expires_at", true],
    ["id", false],
    ["port", false],
    ["account", false],
    ["relay", false],
    ["host_key", false],
    ["provider_account", false],
    ["currency", false],
    ["billing_cycle", false],
    ["auto_renew", false],
    ["cores", false],
  ]);

  fireEvent.click(screen.getByRole("button", { name: "region" }));
  expect(query()).toEqual([["sort", "region"]]);

  fireEvent.click(within(picker).getByRole("checkbox", { name: "cores" }));
  fireEvent.click(within(picker).getByRole("checkbox", { name: "region" }));
  fireEvent.click(within(picker).getByRole("checkbox", { name: "port" }));

  // A column hidden no longer sorts the list.
  const columns = ["name", "space", "state", "address", "provider", "price", "expires_at", "cores", "port"];
  expect(query()).toEqual([["columns", columns.slice(1).join(",")]]);
  await screen.findByRole("table", { name: "Hosts" });
  expect(lastList(api)).toEqual({});
  expect(headers()).toEqual(columns);
  expect(tableRows("Hosts")).toEqual([
    ["web1", "ops", "managed", "web1.example.com", "hetzner", "4.5 EUR", "2026-12-01", "8", "22"],
  ]);

  const bookmark = window.location.pathname + window.location.search;
  first.unmount();
  renderAt(bookmark);
  await screen.findByRole("table", { name: "Hosts" });
  expect(headers()).toEqual(columns);
});

test("Retired Hosts show only when asked for, and link by their id", async () => {
  const api = fakeApi();
  actAs(api, alice);
  hosts(api, [web1, old1]);
  renderAt("/hosts");
  await screen.findByRole("table", { name: "Hosts" });
  expect(tableRows("Hosts").map((r) => r.slice(0, 3))).toEqual([["web1", "ops", "managed"]]);

  fireEvent.click(screen.getByRole("checkbox", { name: "Show Retired Hosts" }));

  await screen.findByRole("cell", { name: "retired" });
  expect(query()).toEqual([["all", "1"]]);
  expect(lastList(api)).toEqual({ all: true });
  const links = within(screen.getByRole("table", { name: "Hosts" })).getAllByRole("link");
  expect(links.map((a) => a.getAttribute("href"))).toEqual(["/hosts/web1", "/hosts/host_old1"]);
});

test("a filter on state=retired asks for the Retired Hosts while it is there", async () => {
  const api = fakeApi();
  actAs(api, alice);
  hosts(api, [web1, old1]);
  renderAt("/hosts");
  await screen.findByRole("table", { name: "Hosts" });
  const showRetired = () => screen.getByRole("checkbox", { name: "Show Retired Hosts" }) as HTMLInputElement;

  choose("Key", "state");
  choose("Comparison", "eq");
  choose("Value", "retired");
  fireEvent.click(screen.getByRole("button", { name: "Add filter" }));

  await screen.findByRole("cell", { name: "retired" });
  expect(query()).toEqual([["filter", "state=retired"]]);
  expect(lastList(api)).toEqual({ filters: [{ key: "state", op: "eq", value: "retired" }], all: true });
  expect([showRetired().checked, showRetired().disabled]).toEqual([true, true]);

  fireEvent.click(screen.getByRole("button", { name: "Remove the filter state = retired" }));

  expect(query()).toEqual([]);
  await screen.findByRole("table", { name: "Hosts" });
  expect(lastList(api)).toEqual({});
  expect([showRetired().checked, showRetired().disabled]).toEqual([false, false]);
});

test("a URL holding a term, sort key or column the list does not take says so and lists nothing", async () => {
  const api = fakeApi();
  actAs(api, alice);
  hosts(api, [web1]);
  renderAt("/hosts?filter=note=x&filter=price.gt=ten&filter=__proto__=x&sort=created_at&columns=space,Bad,name,space");

  const alert = (await screen.findByText(/^This page's address holds/)).closest<HTMLElement>('[role="alert"]')!;
  expect(alert.textContent).toContain("note=x, price.gt=ten, __proto__=x, created_at, Bad, name, space");
  expect(screen.queryByRole("table")).toBeNull();
  expect(api.requests(HostList)).toEqual([]);

  fireEvent.click(within(alert).getByRole("link", { name: "Show the default view" }));
  await screen.findByRole("table", { name: "Hosts" });
  expect(window.location.search).toBe("");
});

test("the relay column names a Host's Relay, or says it is one the caller cannot see", async () => {
  const api = fakeApi();
  actAs(api, bob);
  hosts(api, [
    host("app1", { relay: { id: "host_gw1", name: "gw1" } }),
    host("app2", { relay: { id: null, name: null } }),
    host("gw1"),
  ]);

  renderAt("/hosts?columns=relay");

  await screen.findByRole("table", { name: "Hosts" });
  expect(tableRows("Hosts")).toEqual([
    ["app1", "gw1"],
    ["app2", "(one you cannot see)"],
    ["gw1", ""],
  ]);
});

test("a Host added sends only the fields given, in the caller's Personal Space by default, and never asks", async () => {
  const api = fakeApi();
  actAs(api, bob);
  hosts(api, []);
  const added = host("web9", { state: "registered", space: "bob" });
  api.on(HostAdd, () => ({ hosts: [added] }));
  api.on(HostShow, () => ({ host: added, services: [] }));
  const first = renderAt("/hosts");
  fireEvent.click(await screen.findByRole("button", { name: "Add a Host" }));
  const add = within(await screen.findByRole("region", { name: "Add a Host" }));
  await add.findByRole("option", { name: "ops" });
  const type = (label: string, value: string) => fireEvent.change(add.getByLabelText(label), { target: { value } });

  type("name", "web9");
  type("address", "203.0.113.9");
  expect(add.getByRole("button", { name: "Add Host" }).hasAttribute("disabled")).toBe(true);
  type("account", "root");
  fireEvent.click(add.getByRole("button", { name: "Add Host" }));

  expect(await screen.findByText("koko host enroll web9")).toBeDefined();
  expect(window.location.pathname).toBe("/hosts/web9");
  expect(api.requests(HostAdd)).toEqual([
    { hosts: [{ name: "web9", space: "bob", address: "203.0.113.9", account: "root" }] },
  ]);

  first.unmount();
  renderAt("/hosts");
  fireEvent.click(await screen.findByRole("button", { name: "Add a Host" }));
  const again = within(await screen.findByRole("region", { name: "Add a Host" }));
  await again.findByRole("option", { name: "ops" });
  for (const [label, value] of [
    ["name", "web8"],
    ["space", "ops"],
    ["address", "web8.example.com"],
    ["port", "2222"],
    ["account", "deploy"],
    ["relay", "gw1"],
  ] as const) {
    fireEvent.change(again.getByLabelText(label), { target: { value } });
  }
  fireEvent.click(again.getByRole("button", { name: "Add Host" }));
  await vi.waitFor(() => expect(api.requests(HostAdd)).toHaveLength(2));
  expect(api.requests(HostAdd)[1]).toEqual({
    hosts: [{ name: "web8", space: "ops", address: "web8.example.com", port: 2222, account: "deploy", relay: "gw1" }],
  });
  expect(screen.queryByRole("alertdialog")).toBeNull();
});
