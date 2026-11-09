import { fireEvent, screen, within } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { type AttrKey, AttrList } from "../../api/attr";
import { HostShow } from "../../api/host";
import {
  type Service,
  ServiceList,
  type ServiceListRequest,
  ServiceRemove,
  ServiceRename,
  ServiceSet,
  ServiceShow,
  type ServiceShowRequest,
} from "../../api/service";
import { expectAccessible } from "../../test/axe";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { actAs, findAlert, renderAt, tableRows } from "../../test/pages";
import {
  actions,
  addFilter,
  bob,
  metadata,
  offered,
  openColumns,
  query,
  service as record,
  tokens,
  visibleText,
} from "../host/records.fixture";

// The Services list and a Service's page, against the fake API, one test
// for each action of the page inventory (docs/subsystems/web-shell.md): the
// list's PowerSearch tokens, sorting and columns in the URL as on the Hosts
// list, its detail pane, and a Service's actions, where only deleting asks
// first.

const attributes: AttrKey[] = [
  { key: "provider", type: "text", preset: true, in_use: false },
  { key: "price", type: "number", min: 0, preset: true, in_use: false },
  { key: "kind", type: "text", preset: false, in_use: true },
];

function service(host: string, name: string, fields: Partial<Service> = {}): Service {
  return record(name, { id: `service_${host}_${name}`, host, updated_at: "2026-11-04T09:00:00Z", ...fields });
}

const nginx = service("web1", "nginx", {
  ports: ["80/tcp", "443/tcp"],
  attributes: { kind: "proxy" },
  note: "Terminates TLS.",
});
const pg = service("db1", "pg", { ports: ["5432/tcp"] });

/** services answers attr.list, service.list with the Services given, and service.show with each of them. */
function services(api: FakeApi, list: Service[]): void {
  api.on(AttrList, () => ({ attributes }));
  api.on(ServiceList, () => ({ services: list }));
  api.on(ServiceShow, ({ host, name }: ServiceShowRequest) => {
    const found = list.find((s) => s.host === host && s.name === name);
    if (!found) {
      throw refusal("unknown_service", 404);
    }
    return { service: found };
  });
}

function headers(): string[] {
  return within(screen.getByRole("table", { name: "Services" }))
    .getAllByRole("columnheader")
    .map(visibleText);
}

function table(): Promise<HTMLElement> {
  return screen.findByRole("table", { name: "Services" });
}

async function dialog(title: string) {
  return within(await screen.findByRole("dialog", { name: title }));
}

const filter = "Filter Services";

test("the Services list shows Host, name and ports, each linking to its page", async () => {
  const api = fakeApi();
  actAs(api, bob);
  services(api, [pg, nginx]);

  renderAt("/services");

  await table();
  expect(headers()).toEqual(["host", "name", "ports"]);
  expect(tableRows("Services")).toEqual([
    ["db1", "pg", "5432/tcp"],
    ["web1", "nginx", "80/tcp, 443/tcp"],
  ]);
  expect(screen.getByRole("link", { name: "nginx" }).getAttribute("href")).toBe("/services/web1/nginx");
  expect(screen.getByRole("link", { name: "web1" }).getAttribute("href")).toBe("/hosts/web1");
  expect(api.requests(ServiceList)).toEqual([{}]);
  // The ports, a set, neither filter nor sort the list.
  expect(within(screen.getByRole("columnheader", { name: "ports" })).queryByRole("button")).toBeNull();
  await expectAccessible();
});

test("the Services list takes the Hosts list's tokens, sorting and columns, and keeps them in the URL", async () => {
  const api = fakeApi();
  actAs(api, bob);
  services(api, [pg, nginx]);
  const first = renderAt("/services");
  await table();

  expect(await offered(filter, "host")).toEqual(["host", "host =", "host !=", "host has", "host lacks"]);
  await addFilter(filter, "host", "=", "web1");
  fireEvent.click(within(await table()).getByRole("button", { name: "name" }));
  await table();
  const options = within(await openColumns());
  await expectAccessible();
  fireEvent.click(options.getByRole("button", { name: "Show kind" }));

  expect(query()).toEqual([
    ["filter", "host=web1"],
    ["sort", "name"],
    ["columns", "ports,kind"],
  ]);
  await table();
  expect(api.requests(ServiceList).at(-1) as ServiceListRequest).toEqual({
    filters: [{ key: "host", op: "eq", value: "web1" }],
    sort: [{ key: "name" }],
  });
  expect(headers()).toEqual(["host", "name", "ports", "kind"]);
  expect(screen.getByRole("columnheader", { name: /^name/ }).getAttribute("aria-sort")).toBe("ascending");

  // A bookmark from before the redesign opens the same view.
  const bookmark = window.location.pathname + window.location.search;
  first.unmount();
  renderAt(bookmark);
  await table();
  expect(tokens(filter)).toEqual(["host: = web1"]);
  expect(headers()).toEqual(["host", "name", "ports", "kind"]);

  // A column hidden no longer sorts the list.
  fireEvent.click(within(await openColumns()).getByRole("button", { name: "Hide ports" }));
  expect(query()).toEqual([
    ["filter", "host=web1"],
    ["sort", "name"],
    ["columns", "kind"],
  ]);
  await table();
  expect(headers()).toEqual(["host", "name", "kind"]);
});

test("a URL holding a key, a sort or a column the list does not take says which and lists nothing", async () => {
  const api = fakeApi();
  actAs(api, bob);
  services(api, [nginx]);

  renderAt("/services?filter=state=managed&sort=ports&columns=name&all=1");

  expect(
    await screen.findByText("The address of this page holds state=managed, ports, name, which the list of Services does not take."),
  ).toBeDefined();
  expect(api.requests(ServiceList)).toEqual([]);
  await expectAccessible();
  fireEvent.click(screen.getByRole("link", { name: "Show the default view" }));
  await table();
  expect(window.location.search).toBe("");
});

test("a Service selected opens beside the list with what service.show answers, and its page a click away", async () => {
  const api = fakeApi();
  actAs(api, bob);
  services(api, [pg, nginx]);
  renderAt("/services");
  await table();

  fireEvent.click(screen.getByRole("link", { name: "nginx" }));

  const pane = within(await screen.findByRole("region", { name: "Details of web1/nginx" }));
  expect(await pane.findByText("Terminates TLS.")).toBeDefined();
  expect(pane.getByRole("link", { name: "Open page" }).getAttribute("href")).toBe("/services/web1/nginx");
  expect(api.requests(ServiceShow)).toEqual([{ host: "web1", name: "nginx" }]);
  await expectAccessible();

  fireEvent.click(pane.getByRole("link", { name: "Open page" }));
  expect(await screen.findByRole("heading", { level: 1, name: "web1/nginx" })).toBeDefined();
});

test("a Service's page shows its fields, Attributes and its Note in the rail", async () => {
  const api = fakeApi();
  actAs(api, bob);
  services(api, [nginx]);

  renderAt("/services/web1/nginx");

  expect(await screen.findByRole("heading", { level: 1, name: "web1/nginx" })).toBeDefined();
  expect(metadata("Fields")).toEqual([
    ["host", "web1"],
    ["name", "nginx"],
    ["id", "service_web1_nginx"],
    ["ports", "80/tcp, 443/tcp"],
    ["created_at", "2026-11-01"],
    ["updated_at", "2026-11-04"],
  ]);
  expect(metadata("Attributes")).toEqual([["kind", "proxy"]]);
  expect(within(screen.getByRole("region", { name: "Note" })).getByText("Terminates TLS.")).toBeDefined();
  expect(api.requests(ServiceShow)).toEqual([{ host: "web1", name: "nginx" }]);
  await expectAccessible();
});

test("a Service's edit dialog sends only what changed, clearing its ports when emptied, and rename never asks", async () => {
  const api = fakeApi();
  actAs(api, bob);
  const proxy = service("web1", "proxy");
  services(api, [nginx, proxy]);
  api.on(ServiceSet, () => ({ service: nginx }));
  api.on(ServiceRename, () => ({ service: proxy }));
  renderAt("/services/web1/nginx");
  const edit = async () => {
    fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
    const opened = await dialog("Edit web1/nginx");
    await opened.findByRole("textbox", { name: "ports" });
    return opened;
  };
  const closed = () => vi.waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit web1/nginx" })).toBeNull());
  const type = (scope: Awaited<ReturnType<typeof dialog>>, label: string, value: string) =>
    fireEvent.change(scope.getByRole("textbox", { name: label }), { target: { value } });

  const first = await edit();
  await expectAccessible();
  type(first, "ports", "");
  type(first, "kind", "edge");
  type(first, "Note", "Terminates TLS for the app.");
  fireEvent.click(first.getByRole("button", { name: "Save" }));
  await closed();
  expect(api.requests(ServiceSet)).toEqual([
    { host: "web1", name: "nginx", attributes: { kind: "edge" }, note: "Terminates TLS for the app.", unset: ["ports"] },
  ]);

  const again = await edit();
  type(again, "ports", "8080 8443/tcp");
  fireEvent.click(again.getByRole("button", { name: "Save" }));
  await closed();
  expect(api.requests(ServiceSet)[1]).toEqual({ host: "web1", name: "nginx", ports: ["8080", "8443/tcp"] });

  // Ports that split to none clear them: service.set refuses "ports": [].
  const third = await edit();
  type(third, "ports", " , ");
  fireEvent.click(third.getByRole("button", { name: "Save" }));
  await closed();
  expect(api.requests(ServiceSet)[2]).toEqual({ host: "web1", name: "nginx", unset: ["ports"] });

  fireEvent.click(screen.getByRole("button", { name: "Rename" }));
  const rename = await dialog("Rename web1/nginx");
  await expectAccessible();
  type(rename, "New name", "proxy");
  fireEvent.click(rename.getByRole("button", { name: "Rename" }));
  expect(await screen.findByRole("heading", { level: 1, name: "web1/proxy" })).toBeDefined();
  expect(window.location.pathname).toBe("/services/web1/proxy");
  expect(api.requests(ServiceRename)).toEqual([{ host: "web1", name: "nginx", new_name: "proxy" }]);
  expect(screen.queryByRole("alertdialog")).toBeNull();
});

test("deleting a Service asks first, stating the consequence, and goes back to its Host", async () => {
  const api = fakeApi();
  actAs(api, bob);
  services(api, [nginx]);
  api.on(ServiceRemove, () => ({ service: nginx }));
  api.on(HostShow, () => {
    throw refusal("unknown_host", 404);
  });
  renderAt("/services/web1/nginx");

  fireEvent.click(await screen.findByRole("button", { name: "Delete" }));
  const asked = screen.getByRole("alertdialog", { name: "Delete web1/nginx" });
  expect(asked.textContent).toContain("web1/nginx is deleted permanently, with its ports, Attributes and Note.");
  await expectAccessible();
  fireEvent.click(within(asked).getByRole("button", { name: "Cancel" }));
  expect(api.requests(ServiceRemove)).toEqual([]);

  fireEvent.click(screen.getByRole("button", { name: "Delete" }));
  fireEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Delete" }));

  await vi.waitFor(() => expect(window.location.pathname).toBe("/hosts/web1"));
  expect(api.requests(ServiceRemove)).toEqual([{ host: "web1", name: "nginx" }]);
});

test("a Service on a Retired Host is read-only", async () => {
  const api = fakeApi();
  actAs(api, bob);
  services(api, [service("host_old1", "nginx")]);

  renderAt("/services/host_old1/nginx");

  expect(await screen.findByText(/Its Host is Retired/)).toBeDefined();
  expect(screen.getByRole("heading", { level: 1, name: "host_old1/nginx" })).toBeDefined();
  expect(actions()).toEqual([]);
  await expectAccessible();
  expect(metadata("Fields")[0]).toEqual(["host", "host_old1"]);
  expect(within(screen.getByRole("main")).getAllByRole("link", { name: "host_old1" })[0]?.getAttribute("href")).toBe(
    "/hosts/host_old1",
  );
});

test("a Service no one can see says so", async () => {
  const api = fakeApi();
  actAs(api, bob);
  services(api, []);

  renderAt("/services/web1/nope");

  expect(await findAlert()).toBe("The Host has no Service of that name.");
});
