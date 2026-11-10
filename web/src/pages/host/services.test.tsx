import { configure, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { ServiceList } from "../../api/service";
import { expectAccessible } from "../../test/axe";
import { searchAndChoose } from "../../test/controls";
import { fakeApi } from "../../test/fakeApi";
import { setViewportWidth } from "../../test/media";
import { actAs, renderAt } from "../../test/pages";
import {
  alice,
  cells,
  headers,
  host,
  lastRequest,
  query,
  records,
  service,
  visibleText,
} from "./records.fixture";

// The Services view of the Hosts page (view=services) against the fake API:
// one test for each action of the page inventory's Services table
// (docs/subsystems/web-shell.md), and its rows of the design's URL table.

// A test here renders the whole page and drives several overlays in turn;
// its limits are ceilings for a slow runner, not waits: each step waits for
// its own outcome, and the first render of a file's page takes longest.
vi.setConfig({ testTimeout: 20_000 });
configure({ asyncUtilTimeout: 5_000 });

const hosts = [host("web1"), host("db1"), host("build1", { space: "dev" })];
const services = [
  service("nginx", { host: "web1", ports: ["80/tcp", "443/tcp"], attributes: { cores: "4" } }),
  service("postgres", { host: "db1", ports: ["5432/tcp"] }),
  service("runner", { host: "build1" }),
];

test("the Services service.list answers, one panel, each row opening the Service on its Host's page", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, { hosts, services });

  renderAt("/hosts?view=services");

  await screen.findByRole("table", { name: "Services" });
  expect(screen.getByRole("tab", { name: /^Services/ }).getAttribute("aria-selected")).toBe("true");
  expect(headers("Services")).toEqual(["Name", "Host", "Ports"]);
  expect(cells("Services")).toEqual([
    ["nginx", "web1", "80/tcp, 443/tcp"],
    ["postgres", "db1", "5432/tcp"],
    ["runner", "build1", "Listens on no port"],
  ]);
  expect(screen.getByRole("link", { name: "nginx" }).getAttribute("href")).toBe("/hosts/web1/services/nginx");
  expect(screen.getAllByRole("link", { name: "web1" })[0]?.getAttribute("href")).toBe("/hosts/web1");
  expect(lastRequest(api, ServiceList)).toEqual({});
  expect(screen.queryByRole("button", { name: /State/ })).toBeNull();
  await expectAccessible();
});

test("the Services tab keeps the search and the Space, and drops the other view's terms, sort keys and columns", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, { hosts, services });
  renderAt("/hosts?filter=space%3Dops&filter=provider.has&sort=price&columns=region&q=n");
  await screen.findByRole("table", { name: "ops" });

  fireEvent.click(screen.getByRole("tab", { name: /^Services/ }));

  await screen.findByRole("table", { name: "Services" });
  expect(query()).toEqual([
    ["space", "ops"],
    ["q", "n"],
    ["view", "services"],
  ]);
  expect(cells("Services").map((r) => r[0])).toEqual(["nginx"]);

  fireEvent.click(screen.getByRole("tab", { name: /^Hosts/ }));
  await screen.findByRole("table", { name: "ops" });
  expect(query()).toEqual([
    ["filter", "space=ops"],
    ["q", "n"],
  ]);
});

test("the Space select narrows the Services by their Host's Space in the browser, as space=, since service.list takes no space key", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, { hosts, services });
  renderAt("/hosts?view=services");
  await screen.findByRole("table", { name: "Services" });

  await searchAndChoose("Space", "de", "dev");

  expect(query()).toEqual([
    ["space", "dev"],
    ["view", "services"],
  ]);
  await waitFor(() => expect(cells("Services")).toEqual([["runner", "build1", "Listens on no port"]]));
  expect(api.requests(ServiceList).every((r) => JSON.stringify(r) === "{}")).toBe(true);
  expect(screen.getByRole("status").textContent).toBe("1 of 3 Services");
});

test("the search matches a Service by its name, its Host and its ports", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, { hosts, services });
  renderAt("/hosts?view=services");
  await screen.findByRole("table", { name: "Services" });
  const search = screen.getByRole("searchbox", { name: "Search Services" });

  for (const [q, found] of [
    ["POST", ["postgres"]],
    ["build", ["runner"]],
    ["443", ["nginx"]],
  ] as const) {
    fireEvent.change(search, { target: { value: q } });
    expect(cells("Services").map((r) => r[0]), q).toEqual(found);
  }
  fireEvent.change(search, { target: { value: "nothing" } });
  expect(await screen.findByText("No Service matches this view.")).toBeDefined();
  await expectAccessible();
});

test("Filter, a header and the columns button write the Services view's terms, sort keys and columns, and the bookmark reopens it", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, { hosts, services });
  const first = renderAt("/hosts?view=services");
  await screen.findByRole("table", { name: "Services" });

  fireEvent.click(screen.getByRole("button", { name: "Filter" }));
  await screen.findByRole("dialog", { name: "Add a filter" });
  await searchAndChoose("Field or Attribute", "cor", "cores");
  fireEvent.change(screen.getByRole("textbox", { name: "Value" }), { target: { value: "4" } });
  fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
  const table = await screen.findByRole("table", { name: "Services" });

  fireEvent.click(within(table).getByRole("button", { name: "Host" }));
  await screen.findByRole("table", { name: "Services" });
  fireEvent.click(screen.getByRole("button", { name: "Columns" }));
  fireEvent.click(within(await screen.findByRole("dialog", { name: "Columns" })).getByRole("checkbox", { name: "cores" }));

  const written = [
    ["filter", "cores=4"],
    ["sort", "host"],
    ["columns", "ports,cores"],
    ["view", "services"],
  ];
  expect(query()).toEqual(written);
  await waitFor(() =>
    expect(api.requests(ServiceList).filter((r) => JSON.stringify(r) !== "{}").at(-1)).toEqual({
      filters: [{ key: "cores", op: "eq", value: "4" }],
      sort: [{ key: "host" }],
    }),
  );
  const bookmark = window.location.pathname + window.location.search;
  first.unmount();

  renderAt(bookmark);
  await screen.findByRole("table", { name: "Services" });
  expect(headers("Services")).toEqual(["Name", "Host", "Ports", "cores"]);
  expect(cells("Services")[0]).toEqual(["nginx", "web1", "80/tcp, 443/tcp", "4"]);
  // The ports show, but neither filter nor sort the list.
  expect(within(screen.getByRole("table", { name: "Services" })).queryByRole("button", { name: "Ports" })).toBeNull();
  expect(query()).toEqual(written);
});

test("a Services view the list does not take names the part at fault and goes to the default view", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, { hosts, services });

  renderAt("/hosts?view=services&sort=ports&columns=host");

  expect(
    await screen.findByText("The address of this page holds ports, host, which the list of Services does not take."),
  ).toBeDefined();
  fireEvent.click(screen.getByRole("button", { name: "Go to the default view" }));
  await screen.findByRole("table", { name: "Services" });
  expect(query()).toEqual([["view", "services"]]);
});

test("with no Service recorded, the view says so and where one is added", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, { hosts, services: [] });

  renderAt("/hosts?view=services");

  expect(await screen.findByRole("heading", { level: 2, name: "No Service is recorded yet" })).toBeDefined();
  await expectAccessible();
});

test("on a phone each Service is one link: its name, its Host and its ports", async () => {
  setViewportWidth(390);
  const api = fakeApi();
  actAs(api, alice);
  records(api, { hosts, services });

  renderAt("/hosts?view=services");

  const list = await screen.findByRole("list", { name: "Services" });
  expect(within(list).getAllByRole("link").map(visibleText)).toEqual([
    "nginxon web180/tcp, 443/tcp",
    "postgreson db15432/tcp",
    "runneron build1Listens on no port",
  ]);
  await expectAccessible();
});
