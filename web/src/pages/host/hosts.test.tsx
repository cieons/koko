import { configure, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { ServiceList } from "../../api/service";
import { expectAccessible } from "../../test/axe";
import { expectChosen, searchAndChoose } from "../../test/controls";
import { fakeApi } from "../../test/fakeApi";
import { setViewportWidth } from "../../test/media";
import { actAs, renderAt } from "../../test/pages";
import {
  alice,
  bob,
  cells,
  chips,
  headers,
  host,
  lastRequest,
  listRequests,
  pick,
  query,
  records,
  selectButton,
  service,
  visibleText,
} from "./records.fixture";

// The Hosts page against the fake API (docs/subsystems/record-pages.md):
// one test for each action of the page inventory's Hosts table
// (docs/subsystems/web-shell.md), each row of the design's table under "The
// URL keeps koko's grammar", a bookmark from before, and the list with 0, 3
// and 300 Hosts. Filter's every comparison is in filter.test.tsx, the
// Services view in services.test.tsx.

// A test here renders the whole page and drives several overlays in turn;
// its limits are ceilings for a slow runner, not waits: each step waits for
// its own outcome, and the first render of a file's page takes longest.
vi.setConfig({ testTimeout: 20_000 });
configure({ asyncUtilTimeout: 5_000 });

const web1 = host("web1", {
  attributes: {
    provider: "hetzner",
    region: "fsn1",
    price: 4.5,
    currency: "EUR",
    billing_cycle: "monthly",
    expires_at: "2026-12-01",
  },
  note: "Ask ops before a reboot.",
});
const db1 = host("db1", { state: "registered", port: 2222, attributes: { provider: "vultr", price: 12 } });
const build1 = host("build1", { space: "dev", account: "ci", attributes: { cores: "8" } });
const old1 = host("web1", { id: "host_old1", state: "retired", address: "192.0.2.99" });
const nginx = service("nginx", { host: "web1", ports: ["443/tcp"] });
const postgres = service("postgres", { host: "db1", ports: ["5432/tcp"] });
const three = { hosts: [db1, web1, build1, old1], services: [nginx, postgres] };

/** groups are the names of the Space headings, in order. */
function groups(): string[] {
  return screen.getAllByRole("heading", { level: 2 }).map(visibleText);
}

test("the Hosts host.list answers, grouped by Space, a panel each in the default columns, each row a link to its Host", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, three);

  renderAt("/hosts");

  await screen.findByRole("table", { name: "ops" });
  expect(screen.getByText("3 Hosts in 2 Spaces, running 2 Services.")).toBeDefined();
  expect(groups()).toEqual(["dev1", "ops2"]);
  expect(headers("ops")).toEqual(["Name", "Provider and region", "Address", "Price", "Expires", "State"]);
  expect(cells("ops")).toEqual([
    ["db1postgres", "vultr", "db1.example.com:2222", "12", "", "Registered"],
    ["web1nginx", "hetzner · fsn1", "web1.example.com", "4.5 EUR monthly", "2026-12-01", "Managed"],
  ]);
  expect(cells("dev")).toEqual([["build1", "", "build1.example.com", "", "", "Managed"]]);
  expect(screen.getByRole("link", { name: "web1" }).getAttribute("href")).toBe("/hosts/web1");
  expect(screen.getByRole("tab", { name: /^Hosts/ }).getAttribute("aria-selected")).toBe("true");
  expect(screen.getAllByRole("tab").map(visibleText)).toEqual(["Hosts3", "Services2"]);
  expect(listRequests(api)).toEqual([{ with_note: true }]);
  await waitFor(() => expect(document.title).toBe("Hosts · koko"));
  await expectAccessible();
});

test("Provider and region puts its separator only between a provider and a region", async () => {
  const api = fakeApi();
  actAs(api, alice);
  const regionOnly = host("db1", { attributes: { region: "hk" } });
  const providerOnly = host("web1", { attributes: { provider: "vultr" } });
  records(api, { hosts: [regionOnly, providerOnly] });

  renderAt("/hosts");

  await screen.findByRole("table", { name: "ops" });
  expect(cells("ops").map((row) => row[1])).toEqual(["hk", "vultr"]);
});

test("with every Host Retired, the page is not empty: its controls show the Retired Hosts", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, { hosts: [old1] });

  renderAt("/hosts");

  expect(await screen.findByText("No Host matches this view.")).toBeDefined();
  expect(screen.queryByText("No Host is recorded yet")).toBeNull();
  expect(screen.getByText("0 Hosts in 0 Spaces, running 0 Services.")).toBeDefined();
  await pick("State", "All, with Retired");
  expect(await screen.findByRole("link", { name: "web1" })).toBeDefined();
});

test("a Member sees the Hosts of their Spaces, and no Space select with one Space", async () => {
  const api = fakeApi();
  actAs(api, bob);
  records(api, { hosts: [web1], spaces: [{ id: "space_2", name: "ops", personal: false }] });

  renderAt("/hosts");

  await screen.findByRole("table", { name: "ops" });
  expect(screen.queryByRole("combobox", { name: "Space" })).toBeNull();
  expect(screen.getByText("1 Host in 1 Space, running 0 Services.")).toBeDefined();
});

test("with no Host, the page says none is recorded and offers Add Host", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, { hosts: [] });

  renderAt("/hosts");

  expect(await screen.findByRole("heading", { level: 2, name: "No Host is recorded yet" })).toBeDefined();
  expect(screen.getAllByRole("button", { name: "Add Host" })).toHaveLength(2);
  expect(screen.queryByRole("tablist")).toBeNull();
  await expectAccessible();
});

test("300 Hosts list as 3 do, in their Spaces' panels", async () => {
  const api = fakeApi();
  actAs(api, alice);
  const many = Array.from({ length: 300 }, (_, i) =>
    host(`web${String(i).padStart(3, "0")}`, { space: i % 3 === 0 ? "dev" : "ops", attributes: { price: i } }),
  );
  records(api, { hosts: many });

  renderAt("/hosts");

  await screen.findByRole("table", { name: "ops" });
  expect(cells("dev")).toHaveLength(100);
  expect(cells("ops")).toHaveLength(200);
  expect(screen.getByText("300 Hosts in 2 Spaces, running 0 Services.")).toBeDefined();
  await expectAccessible();
}, 30_000);

test("the search narrows the list in the browser by name, address, Space, Host Account, Note, Attribute and Service, replacing the history entry", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, three);
  renderAt("/hosts");
  await screen.findByRole("table", { name: "ops" });
  const entries = window.history.length;
  const search = screen.getByRole("searchbox", { name: "Search Hosts" });

  const found = (q: string) => {
    fireEvent.change(search, { target: { value: q } });
    return screen.queryAllByRole("table").flatMap((t) => cells(t.getAttribute("aria-label") ?? "").map((r) => r[0]));
  };

  expect(found("DB1")).toEqual(["db1postgres"]);
  expect(found("web1.example")).toEqual(["web1nginx"]);
  expect(found("dev")).toEqual(["build1"]);
  expect(found("ci")).toEqual(["build1"]);
  expect(found("reboot")).toEqual(["web1nginx"]);
  expect(found("fsn1")).toEqual(["web1nginx"]);
  expect(found("postgres")).toEqual(["db1postgres"]);
  expect(found("443")).toEqual(["web1nginx"]);
  expect(query()).toEqual([["q", "443"]]);
  expect(window.history.length).toBe(entries);
  expect(screen.getByRole("status").textContent).toBe("1 of 3 Hosts");
  expect(listRequests(api)).toEqual([{ with_note: true }]);

  fireEvent.change(search, { target: { value: "nothing" } });
  expect(await screen.findByText("No Host matches this view.")).toBeDefined();
  await expectAccessible();
  fireEvent.click(screen.getByRole("button", { name: "Clear the search and filters" }));
  await screen.findByRole("table", { name: "ops" });
  expect(window.location.search).toBe("");
});

test("the state select writes the state term and all=1, and a Retired Host opens by its id", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, three);
  renderAt("/hosts");
  await screen.findByRole("table", { name: "ops" });

  const choices = [
    ["Managed", [["filter", "state=managed"]], { filters: [{ key: "state", op: "eq", value: "managed" }] }],
    ["Registered", [["filter", "state=registered"]], { filters: [{ key: "state", op: "eq", value: "registered" }] }],
    [
      "Retired",
      [
        ["filter", "state=retired"],
        ["all", "1"],
      ],
      { filters: [{ key: "state", op: "eq", value: "retired" }], all: true },
    ],
    ["All, with Retired", [["all", "1"]], { all: true }],
    ["In use", [], {}],
  ] as const;
  for (const [choice, url, request] of choices) {
    await pick("State", choice);
    expect(query(), choice).toEqual(url);
    await waitFor(() => expect(listRequests(api).at(-1)).toEqual({ ...request, with_note: true }));
    expect(chips(), choice).toEqual([]);
  }

  await pick("State", "Retired");
  await screen.findByRole("link", { name: "web1" });
  expect(cells("ops")).toEqual([["web1", "", "192.0.2.99", "", "", "Retired"]]);
  expect(screen.getByRole("link", { name: "web1" }).getAttribute("href")).toBe("/hosts/host_old1");
});

test("a state term the select cannot show leaves it on In use and shows as a chip; a bookmarked state=retired asks for all", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, three);
  const first = renderAt("/hosts?filter=state!%3Dmanaged");
  await screen.findByRole("table", { name: "ops" });

  expect(selectButton("State").textContent).toContain("In use");
  expect(chips()).toEqual(["state ≠ managed"]);
  first.unmount();

  renderAt("/hosts?filter=state%3Dretired");
  await screen.findByRole("link", { name: "web1" });
  expect(selectButton("State").textContent).toContain("Retired");
  expect(listRequests(api).at(-1)).toEqual({
    filters: [{ key: "state", op: "eq", value: "retired" }],
    all: true,
    with_note: true,
  });
});

test("the Space select writes a space term, shows one built in Filter, and leaves a term it cannot show as a chip", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, three);
  const first = renderAt("/hosts");
  await screen.findByRole("table", { name: "ops" });
  const space = screen.getByRole("combobox", { name: "Space" }) as HTMLInputElement;
  await expectChosen(space, "All Spaces");

  await searchAndChoose("Space", "op", "ops");
  expect(query()).toEqual([["filter", "space=ops"]]);
  await waitFor(() =>
    expect(listRequests(api).at(-1)).toEqual({ filters: [{ key: "space", op: "eq", value: "ops" }], with_note: true }),
  );
  expect(chips()).toEqual([]);

  await searchAndChoose("Space", "all", "All Spaces");
  expect(query()).toEqual([]);
  first.unmount();

  renderAt("/hosts?filter=space%3Ddev&filter=space!%3Dops");
  await screen.findByRole("table", { name: "dev" });
  await expectChosen("Space", "All Spaces");
  expect(chips()).toEqual(["space = dev", "space ≠ ops"]);
});

test("a header sorts by its column, again turns it round, and a Shift-click adds a further key", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, three);
  renderAt("/hosts");
  await screen.findByRole("table", { name: "ops" });
  const header = async (name: string, description?: string) =>
    within(await screen.findByRole("table", { name: "ops" })).getByRole("button", { name, description });

  fireEvent.click(await header("Price"));
  expect(query()).toEqual([["sort", "price"]]);
  await waitFor(() => expect(listRequests(api).at(-1)).toEqual({ sort: [{ key: "price" }], with_note: true }));
  expect((await header("Price")).closest("th")?.getAttribute("aria-sort")).toBe("ascending");

  fireEvent.click(await header("Price"));
  expect(query()).toEqual([["sort", "price.desc"]]);

  fireEvent.click(await header("Name"), { shiftKey: true });
  expect(query()).toEqual([
    ["sort", "price.desc"],
    ["sort", "name"],
  ]);
  await waitFor(() =>
    expect(listRequests(api).at(-1)).toEqual({ sort: [{ key: "price", desc: true }, { key: "name" }], with_note: true }),
  );
  expect((await header("Price", "Sort key 1, descending")).closest("th")?.getAttribute("aria-sort")).toBe("descending");
  expect(await header("Name", "Sort key 2, ascending")).toBeDefined();
  await expectAccessible();
});

test("the columns button shows a column, last, and hides one, which no longer sorts the list", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, three);
  renderAt("/hosts?sort=price");
  await screen.findByRole("table", { name: "ops" });

  fireEvent.click(screen.getByRole("button", { name: "Columns" }));
  const dialog = await screen.findByRole("dialog", { name: "Columns" });
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("checkbox", { name: "Region" }));
  expect(query()).toEqual([
    ["sort", "price"],
    ["columns", "provider,address,price,expires_at,state,region"],
  ]);
  await waitFor(() =>
    expect(headers("ops")).toEqual(["Name", "Provider", "Address", "Price", "Expires", "State", "Region"]),
  );
  expect(cells("ops")[1]).toEqual(["web1nginx", "hetzner", "web1.example.com", "4.5 EUR monthly", "2026-12-01", "Managed", "fsn1"]);

  fireEvent.click(within(dialog).getByRole("checkbox", { name: "Price" }));
  expect(query()).toEqual([["columns", "provider,address,expires_at,state,region"]]);
});

test("the relay column names each Host's Relay, or says the caller cannot see it", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, {
    hosts: [
      host("db1", { relay: { id: "host_web1", name: "web1" } }),
      host("db2", { relay: { id: null, name: null } }),
      host("web1"),
    ],
  });

  renderAt("/hosts?columns=relay");

  await screen.findByRole("table", { name: "ops" });
  expect(headers("ops")).toEqual(["Name", "Relay"]);
  expect(cells("ops")).toEqual([
    ["db1", "web1"],
    ["db2", "A Relay you cannot see"],
    ["web1", ""],
  ]);
});

test("a URL the list does not take names the term at fault, calls nothing for it, and goes to the default view", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, three);

  renderAt("/hosts?filter=note%3Dx&sort=price&sort=price&columns=name");

  expect(
    await screen.findByText(
      "The address of this page holds note=x, price, name, which the list of Hosts does not take.",
    ),
  ).toBeDefined();
  expect(listRequests(api)).toEqual([]);
  await expectAccessible();
  fireEvent.click(screen.getByRole("button", { name: "Go to the default view" }));
  await screen.findByRole("table", { name: "ops" });
  expect(window.location.search).toBe("");
});

test("a bookmark from before the redesign opens the same view: its terms, sort keys, columns and all=1", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, three);

  renderAt(
    "/hosts?filter=provider%3Dhetzner&filter=price.gt%3D10.50&sort=price.desc&sort=name&columns=space,state,address,provider,region&all=1",
  );

  await screen.findByRole("table", { name: "ops" });
  expect(listRequests(api)).toEqual([
    {
      filters: [
        { key: "provider", op: "eq", value: "hetzner" },
        { key: "price", op: "gt", value: 10.5 },
      ],
      sort: [{ key: "price", desc: true }, { key: "name" }],
      all: true,
      with_note: true,
    },
  ]);
  expect(headers("ops")).toEqual(["Name", "Space", "State", "Address", "Provider", "Region"]);
  expect(chips()).toEqual(["provider = hetzner", "price > 10.50"]);
  expect(selectButton("State").textContent).toContain("All, with Retired");
});

test("/services goes to the Services view of the Hosts page, keeping its query, in place of its history entry", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, three);
  const entries = window.history.length;

  renderAt("/services?filter=name%3Dnginx&sort=host");

  await screen.findByRole("table", { name: "Services" });
  expect(window.location.pathname).toBe("/hosts");
  expect(query()).toEqual([
    ["filter", "name=nginx"],
    ["sort", "host"],
    ["view", "services"],
  ]);
  expect(window.history.length).toBe(entries);
  expect(lastRequest(api, ServiceList)).toEqual({ filters: [{ key: "name", op: "eq", value: "nginx" }], sort: [{ key: "host" }] });
});

test("on a phone each Host is one link of three lines, and the columns button is not offered", async () => {
  setViewportWidth(390);
  const api = fakeApi();
  actAs(api, alice);
  records(api, three);

  renderAt("/hosts");

  const list = await screen.findByRole("list", { name: "ops" });
  expect(within(list).getAllByRole("link").map(visibleText)).toEqual([
    "db1Registereddb1.example.com:2222vultr",
    "web1Managedweb1.example.comhetzner · fsn12026-12-01",
  ]);
  expect(screen.queryByRole("button", { name: "Columns" })).toBeNull();
  await expectAccessible();
});
