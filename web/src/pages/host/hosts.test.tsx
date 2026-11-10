import { fireEvent, screen, within } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";

import { AttrList } from "../../api/attr";
import { type Host, HostAdd, HostList, type HostListRequest, HostShow } from "../../api/host";
import { SpaceList } from "../../api/space";
import { expectAccessible } from "../../test/axe";
import { type FakeApi, fakeApi } from "../../test/fakeApi";
import { setViewportWidth } from "../../test/media";
import { actAs, renderAt, tableRows } from "../../test/pages";
import {
  addFilter,
  alice,
  attributes,
  bob,
  enumValue,
  host,
  offered,
  openColumns,
  query,
  service,
  tokens,
  visibleText,
} from "./records.fixture";

// The Hosts list on Astryx's table-filter pattern, against the fake API: one
// test for each action of the page inventory (docs/subsystems/web-shell.md),
// its PowerSearch tokens, sorting, columns and Retired Hosts all kept in the
// page's URL, and its detail pane.

const web1 = host("web1", {
  attributes: { provider: "hetzner", region: "fsn1", price: 4.5, currency: "EUR", expires_at: "2026-12-01", cores: "8" },
  note: "Ask ops before a reboot.",
});
const db1 = host("db1", { space: "bob", state: "registered", attributes: { provider: "vultr", price: 12 } });
const old1 = host("web1", { id: "host_old1", state: "retired", attributes: { provider: "hetzner" } });

/**
 * hosts answers attr.list, space.list for the form that adds a Host,
 * host.list with the Hosts given, the Retired ones only under all, and
 * host.show with each of them.
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
  api.on(HostShow, ({ host: named }) => {
    const found = list.find((h) => h.id === named) ?? web1;
    return { host: found, services: found === web1 ? [service("nginx", { ports: ["443/tcp"] })] : [] };
  });
}

/** lastList is the last host.list request. */
function lastList(api: FakeApi): HostListRequest {
  return api.requests(HostList).at(-1) as HostListRequest;
}

function headers(): string[] {
  return within(screen.getByRole("table", { name: "Hosts" }))
    .getAllByRole("columnheader")
    .map(visibleText);
}

/** table waits for the table of Hosts, which shows again once host.list answers each new view. */
function table(): Promise<HTMLElement> {
  return screen.findByRole("table", { name: "Hosts" });
}

const filter = "Filter Hosts";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllEnvs();
});

test("a Member sees the Hosts host.list answers them in the default columns, each linking to its page", async () => {
  const api = fakeApi();
  actAs(api, bob);
  hosts(api, [db1, web1]);

  renderAt("/hosts");

  await table();
  expect(headers()).toEqual(["name", "space", "state", "address", "provider", "region", "price", "expires_at"]);
  expect(tableRows("Hosts")).toEqual([
    ["db1", "bob", "registered", "db1.example.com", "vultr", "", "12", ""],
    ["web1", "ops", "managed", "web1.example.com", "hetzner", "fsn1", "4.5 EUR", "2026-12-01"],
  ]);
  expect(api.requests(HostList)).toEqual([{}]);
  expect(screen.getByRole("link", { name: "web1" }).getAttribute("href")).toBe("/hosts/web1");
  expect(screen.getByRole("heading", { level: 1, name: "Hosts" })).toBeDefined();
  const nav = screen.getByRole("navigation", { name: "Pages" });
  expect(within(nav).getByRole("link", { name: "Hosts" }).getAttribute("aria-current")).toBe("page");
  await expectAccessible();
});

test("a term of each type added as a token goes into the URL as the CLI writes it, and comes back as a token", async () => {
  // Astryx times its popovers by the clock, so it runs on from the time set.
  vi.useFakeTimers({ toFake: ["Date"], shouldAdvanceTime: true });
  vi.setSystemTime(new Date("2026-11-06T20:00:00Z")); // already 2026-11-07 in Shanghai
  vi.stubEnv("TZ", "Asia/Shanghai");
  const api = fakeApi();
  actAs(api, alice);
  hosts(api, [web1]);
  const first = renderAt("/hosts");
  await table();

  await addFilter(filter, "expires_at", "<=", "+30d");
  await addFilter(filter, "price", ">", "10");
  await addFilter(filter, "currency", "=", "EUR");
  await addFilter(filter, "region", "lacks");
  await addFilter(filter, "name", "!=", "db1");

  const written = ["expires_at.le=+30d", "price.gt=10", "currency=EUR", "region.lacks", "name!=db1"];
  expect(query()).toEqual(written.map((f) => ["filter", f]));
  const filters = [
    { key: "expires_at", op: "le", value: "2026-12-07" },
    { key: "price", op: "gt", value: 10 },
    { key: "currency", op: "eq", value: "EUR" },
    { key: "region", op: "lacks" },
    { key: "name", op: "ne", value: "db1" },
  ];
  await table();
  expect(lastList(api)).toEqual({ filters });

  // The bookmark, opened on a later day, keeps its relative date.
  const bookmark = window.location.pathname + window.location.search;
  first.unmount();
  vi.setSystemTime(new Date("2026-11-08T20:00:00Z"));
  renderAt(bookmark);
  await table();

  expect(tokens(filter)).toEqual([
    "expires_at: <= +30d (2026-12-09)",
    "price: > 10",
    "currency: = EUR",
    "region: lacks",
    "name: != db1",
  ]);
  expect(lastList(api)).toEqual({ filters: [{ ...filters[0], value: "2026-12-09" }, ...filters.slice(1)] });
  expect(query()).toEqual(written.map((f) => ["filter", f]));

  fireEvent.click(screen.getByRole("button", { name: "Remove price: > 10" }));
  expect(query()).toEqual(["expires_at.le=+30d", "currency=EUR", "region.lacks", "name!=db1"].map((f) => ["filter", f]));
});

test("each key offers only the comparisons its type takes, and an enum its values", async () => {
  const api = fakeApi();
  actAs(api, alice);
  hosts(api, [web1]);
  renderAt("/hosts");
  await table();

  const equality = ["=", "!=", "has", "lacks"];
  const ordered = ["=", "!=", "<", "<=", ">", ">=", "has", "lacks"];
  for (const [key, ops] of [
    ["name", equality],
    ["state", equality],
    ["cores", equality],
    ["currency", equality],
    ["port", ordered],
    ["price", ordered],
    ["expires_at", ordered],
  ] as const) {
    expect(await offered(filter, key), key).toEqual([key, ...ops.map((op) => `${key} ${op}`)]);
  }

  const input = screen.getByRole("combobox", { name: filter });
  fireEvent.focus(input);
  fireEvent.change(input, { target: { value: "billing_cycle" } });
  fireEvent.click(await screen.findByRole("option", { name: "billing_cycle !=" }));
  await screen.findByRole("button", { name: "Apply" });
  fireEvent.click(enumValue());
  expect((await screen.findAllByRole("option")).map((o) => o.textContent)).toEqual(attributes[5]?.values);
});

test("a value its key does not take is refused, saying so, and the URL kept", async () => {
  const api = fakeApi();
  actAs(api, alice);
  hosts(api, [web1]);
  renderAt("/hosts?filter=region=fsn1");
  await table();

  const input = screen.getByRole("combobox", { name: filter });
  fireEvent.focus(input);
  fireEvent.change(input, { target: { value: "expires_at" } });
  fireEvent.click(await screen.findByRole("option", { name: "expires_at =" }));
  fireEvent.change(await screen.findByRole("textbox", { name: "Date" }), { target: { value: "tomorrow" } });
  expect(screen.getByText("tomorrow is not a date. Write a date such as 2026-12-01, today or +30d.")).toBeDefined();
  await expectAccessible();
  fireEvent.click(screen.getByRole("button", { name: "Apply" }));

  expect(
    await screen.findByText("expires_at=tomorrow is not a filter the list takes: its value is not of its key's type."),
  ).toBeDefined();
  await expectAccessible();
  expect(query()).toEqual([["filter", "region=fsn1"]]);

  // A number has at most 6 digits after the point (ADR 0029).
  await addFilter(filter, "price", ">=", "1.0000001");
  expect(
    await screen.findByText("price.ge=1.0000001 is not a filter the list takes: its value is not of its key's type."),
  ).toBeDefined();
  expect(query()).toEqual([["filter", "region=fsn1"]]);
});

test("a term as written in the URL stays so when another token is added beside it", async () => {
  const api = fakeApi();
  actAs(api, alice);
  hosts(api, [web1]);
  renderAt("/hosts?filter=price.gt=10.50&filter=expires_at.le=%2B30d");
  await table();

  await addFilter(filter, "region", "lacks");

  expect(query()).toEqual(["price.gt=10.50", "expires_at.le=+30d", "region.lacks"].map((f) => ["filter", f]));
});

test("a click on a column header sorts by it, and a Shift-click adds a further key", async () => {
  const api = fakeApi();
  actAs(api, alice);
  hosts(api, [web1]);
  renderAt("/hosts");
  await table();
  const header = async (key: string) => within(await table()).findByRole("button", { name: key });

  fireEvent.click(await header("price"));
  expect(query()).toEqual([["sort", "price"]]);
  fireEvent.click(await header("price"));
  expect(query()).toEqual([["sort", "price.desc"]]);
  fireEvent.click(await header("expires_at"), { shiftKey: true });
  expect(query()).toEqual([
    ["sort", "price.desc"],
    ["sort", "expires_at"],
  ]);
  expect((await screen.findByRole("columnheader", { name: /^price/ })).getAttribute("aria-sort")).toBe("descending");
  expect(lastList(api)).toEqual({ sort: [{ key: "price", desc: true }, { key: "expires_at" }] });

  fireEvent.click(await header("price"), { shiftKey: true });
  expect(query()).toEqual([
    ["sort", "price"],
    ["sort", "expires_at"],
  ]);

  // A click sorts by the key alone, as it was first, though it sorts already.
  fireEvent.click(await header("price"));
  expect(query()).toEqual([["sort", "price"]]);

  fireEvent.click(await header("name"));
  expect(query()).toEqual([["sort", "name"]]);
  await table();
  expect(lastList(api)).toEqual({ sort: [{ key: "name" }] });
});

test("the column options show any Attribute key and hide a column, in the URL", async () => {
  const api = fakeApi();
  actAs(api, alice);
  hosts(api, [web1]);
  const first = renderAt("/hosts");
  await table();

  fireEvent.click(within(await table()).getByRole("button", { name: "region" }));
  expect(query()).toEqual([["sort", "region"]]);
  await table();

  const options = within(await openColumns());
  const names = (prefix: string) =>
    options
      .getAllByRole("button")
      .map((b) => b.getAttribute("aria-label") ?? "")
      .filter((label) => label.startsWith(prefix))
      .map((label) => label.slice(prefix.length));
  expect(names("Hide ")).toEqual(["space", "state", "address", "provider", "region", "price", "expires_at"]);
  expect(names("Show ")).toEqual([
    "id",
    "port",
    "account",
    "relay",
    "host_key",
    "provider_account",
    "currency",
    "billing_cycle",
    "auto_renew",
    "cores",
  ]);
  await expectAccessible();

  fireEvent.click(options.getByRole("button", { name: "Show cores" }));
  fireEvent.click(options.getByRole("button", { name: "Hide region" }));
  fireEvent.click(options.getByRole("button", { name: "Show port" }));

  // A column hidden no longer sorts the list.
  const columns = ["name", "space", "state", "address", "provider", "price", "expires_at", "cores", "port"];
  expect(query()).toEqual([["columns", columns.slice(1).join(",")]]);
  await table();
  expect(lastList(api)).toEqual({});
  expect(headers()).toEqual(columns);
  expect(tableRows("Hosts")).toEqual([
    ["web1", "ops", "managed", "web1.example.com", "hetzner", "4.5 EUR", "2026-12-01", "8", "22"],
  ]);

  const bookmark = window.location.pathname + window.location.search;
  first.unmount();
  renderAt(bookmark);
  await table();
  expect(headers()).toEqual(columns);
});

test("Retired Hosts show only when asked for, and open by their id", async () => {
  const api = fakeApi();
  actAs(api, alice);
  hosts(api, [web1, old1]);
  renderAt("/hosts");
  await table();
  expect(tableRows("Hosts").map((r) => r.slice(0, 3))).toEqual([["web1", "ops", "managed"]]);

  fireEvent.click(screen.getByRole("switch", { name: "Show Retired Hosts" }));

  await screen.findByText("retired");
  expect(query()).toEqual([["all", "1"]]);
  expect(lastList(api)).toEqual({ all: true });
  const links = within(await table()).getAllByRole("link");
  expect(links.map((a) => a.getAttribute("href"))).toEqual(["/hosts/web1", "/hosts/host_old1"]);

  fireEvent.click(links[1]!);
  const pane = await screen.findByRole("region", { name: "Details of web1" });
  expect(within(pane).getByRole("link", { name: "Open page" }).getAttribute("href")).toBe("/hosts/host_old1");
});

test("a term state=retired asks for the Retired Hosts while it is there", async () => {
  const api = fakeApi();
  actAs(api, alice);
  hosts(api, [web1, old1]);
  renderAt("/hosts");
  await table();
  const showRetired = () => {
    const box = screen.getByRole("switch", { name: "Show Retired Hosts" }) as HTMLInputElement;
    return [box.checked, box.getAttribute("aria-disabled") === "true"];
  };

  await addFilter(filter, "state", "=", "retired");

  await screen.findByText("retired");
  expect(query()).toEqual([["filter", "state=retired"]]);
  expect(lastList(api)).toEqual({ filters: [{ key: "state", op: "eq", value: "retired" }], all: true });
  expect(showRetired()).toEqual([true, true]);

  fireEvent.click(screen.getByRole("button", { name: "Remove state: = retired" }));

  expect(query()).toEqual([]);
  await table();
  expect(lastList(api)).toEqual({});
  expect(showRetired()).toEqual([false, false]);
});

test("a URL holding a term, sort key or column the list does not take says which and lists nothing", async () => {
  const api = fakeApi();
  actAs(api, alice);
  hosts(api, [web1]);
  renderAt("/hosts?filter=note=x&filter=price.gt=ten&filter=__proto__=x&sort=created_at&columns=space,Bad,name,space");

  const fault = await screen.findByText(
    "The address of this page holds note=x, price.gt=ten, __proto__=x, created_at, Bad, name, space, which the list of Hosts does not take.",
  );
  expect(fault.closest('[role="alert"]')).not.toBeNull();
  expect(screen.queryByRole("table")).toBeNull();
  expect(api.requests(HostList)).toEqual([]);
  await expectAccessible();

  fireEvent.click(screen.getByRole("link", { name: "Show the default view" }));
  await table();
  expect(window.location.search).toBe("");
});

test("bookmarks kept before the redesign open the same view", async () => {
  const api = fakeApi();
  actAs(api, alice);
  hosts(api, [web1, old1]);

  renderAt("/hosts?filter=expires_at.le%3D2026-12-31&filter=region.lacks&sort=price.desc&sort=name&columns=space,price&all=1");

  await table();
  expect(tokens(filter)).toEqual(["expires_at: <= 2026-12-31", "region: lacks"]);
  expect(headers()).toEqual(["name", "space", "price"]);
  expect((screen.getByRole("switch", { name: "Show Retired Hosts" }) as HTMLInputElement).checked).toBe(true);
  expect(lastList(api)).toEqual({
    filters: [
      { key: "expires_at", op: "le", value: "2026-12-31" },
      { key: "region", op: "lacks" },
    ],
    sort: [{ key: "price", desc: true }, { key: "name" }],
    all: true,
  });
  expect(screen.getByRole("columnheader", { name: /^price/ }).getAttribute("aria-sort")).toBe("descending");
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

  await table();
  expect(tableRows("Hosts")).toEqual([
    ["app1", "gw1"],
    ["app2", "A Relay you cannot see"],
    ["gw1", ""],
  ]);
});

test("a Host selected opens beside the list with what host.show answers, and its page a click away", async () => {
  const api = fakeApi();
  actAs(api, bob);
  hosts(api, [db1, web1]);
  renderAt("/hosts");
  await table();

  fireEvent.click(screen.getByRole("link", { name: "web1" }));

  const pane = within(await screen.findByRole("region", { name: "Details of web1" }));
  expect(window.location.pathname).toBe("/hosts");
  expect(await pane.findByText("Ask ops before a reboot.")).toBeDefined();
  expect(pane.getByText("Space ops · host_web1")).toBeDefined();
  expect(pane.getByRole("link", { name: /nginx/ }).getAttribute("href")).toBe("/services/web1/nginx");
  expect(pane.getByRole("link", { name: "Open page" }).getAttribute("href")).toBe("/hosts/web1");
  expect(api.requests(HostShow)).toEqual([{ host: "host_web1" }]);
  await expectAccessible();

  // A click on another row selects it.
  fireEvent.click(screen.getByText("db1.example.com"));
  expect(await screen.findByRole("region", { name: "Details of db1" })).toBeDefined();

  fireEvent.click(screen.getByRole("button", { name: "Close the details" }));
  expect(screen.queryByRole("region", { name: /^Details of/ })).toBeNull();

  fireEvent.click(screen.getByRole("link", { name: "web1" }));
  fireEvent.click(await screen.findByRole("link", { name: "Open page" }));
  expect(await screen.findByRole("heading", { level: 1, name: "web1" })).toBeDefined();
  expect(window.location.pathname).toBe("/hosts/web1");
});

test("on a phone the detail pane, the column options and Add Host are bottom sheets", async () => {
  setViewportWidth(390);
  const api = fakeApi();
  actAs(api, bob);
  hosts(api, [web1]);
  renderAt("/hosts");
  await table();

  fireEvent.click(screen.getByRole("link", { name: "web1" }));
  const sheet = await screen.findByRole("dialog", { name: "Details of web1" });
  expect(within(sheet).getByRole("link", { name: "Open page" })).toBeDefined();
  expect(screen.queryByRole("region", { name: "Details of web1" })).toBeNull();
  await expectAccessible();
  fireEvent.click(within(sheet).getByRole("button", { name: "Close the details" }));

  fireEvent.click(screen.getByRole("button", { name: "Columns" }));
  expect(await screen.findByRole("dialog", { name: "Columns" })).toBeDefined();
  fireEvent.keyDown(screen.getByRole("dialog", { name: "Columns" }), { key: "Escape" });

  fireEvent.click(screen.getByRole("button", { name: "Add Host" }));
  const add = await screen.findByRole("dialog", { name: "Add Host" });
  expect(within(add).getByRole("textbox", { name: "name" })).toBeDefined();
  await expectAccessible();
});

test("a Host added sends only the fields given, in the caller's Personal Space by default, and never asks", async () => {
  const api = fakeApi();
  actAs(api, bob);
  hosts(api, []);
  const added = host("web9", { state: "registered", space: "bob" });
  api.on(HostAdd, () => ({ hosts: [added] }));
  api.on(HostShow, () => ({ host: added, services: [] }));
  const first = renderAt("/hosts");
  await screen.findByRole("heading", { name: "No Host matches this view." });
  fireEvent.click(screen.getByRole("button", { name: "Add Host" }));
  const add = within(await screen.findByRole("dialog", { name: "Add Host" }));
  await expectAccessible();
  const type = (label: string, value: string) =>
    fireEvent.change(add.getByRole("textbox", { name: label }), { target: { value } });

  type("name", "web9");
  type("address", "203.0.113.9");
  expect(add.getByRole("button", { name: "Add" }).hasAttribute("disabled")).toBe(true);
  type("account", "root");
  fireEvent.click(add.getByRole("button", { name: "Add" }));

  expect(await screen.findByRole("heading", { level: 1, name: "web9" })).toBeDefined();
  expect(window.location.pathname).toBe("/hosts/web9");
  expect(api.requests(HostAdd)).toEqual([
    { hosts: [{ name: "web9", space: "bob", address: "203.0.113.9", account: "root" }] },
  ]);
  first.unmount();

  renderAt("/hosts");
  await screen.findByRole("heading", { name: "No Host matches this view." });
  fireEvent.click(screen.getByRole("button", { name: "Add Host" }));
  const again = within(await screen.findByRole("dialog", { name: "Add Host" }));
  for (const [label, value] of [
    ["name", "web8"],
    ["address", "web8.example.com"],
    ["port", "2222"],
    ["account", "deploy"],
    ["relay", "gw1"],
  ] as const) {
    fireEvent.change(again.getByRole("textbox", { name: label }), { target: { value } });
  }
  fireEvent.click(again.getByRole("combobox", { name: "space" }));
  fireEvent.click(await screen.findByRole("option", { name: "ops" }));
  fireEvent.click(again.getByRole("button", { name: "Add" }));
  await vi.waitFor(() => expect(api.requests(HostAdd)).toHaveLength(2));
  expect(api.requests(HostAdd)[1]).toEqual({
    hosts: [{ name: "web8", space: "ops", address: "web8.example.com", port: 2222, account: "deploy", relay: "gw1" }],
  });
  expect(screen.queryByRole("alertdialog")).toBeNull();
});

test("Add Host's Cancel sends nothing and gives the focus back to its button", async () => {
  const api = fakeApi();
  actAs(api, bob);
  hosts(api, []);
  renderAt("/hosts");
  await screen.findByRole("heading", { name: "No Host matches this view." });
  const open = screen.getByRole("button", { name: "Add Host" });
  open.focus();
  fireEvent.click(open);
  const add = within(await screen.findByRole("dialog", { name: "Add Host" }));

  fireEvent.click(add.getByRole("button", { name: "Cancel" }));

  await vi.waitFor(() => expect(screen.queryByRole("dialog", { name: "Add Host" })).toBeNull());
  await vi.waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Add Host" })));
  expect(api.requests(HostAdd)).toEqual([]);
});
