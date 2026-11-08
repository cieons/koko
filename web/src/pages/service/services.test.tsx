import { fireEvent, screen, within } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { type AttrKey, AttrList } from "../../api/attr";
import { HostShow } from "../../api/host";
import {
  type Service,
  ServiceList,
  ServiceRemove,
  ServiceRename,
  ServiceSet,
  ServiceShow,
  type ServiceShowRequest,
} from "../../api/service";
import type { User } from "../../api/user";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { actAs, renderAt, tableRows } from "../../test/pages";

// The Services list and a Service's page, against the fake API: the list's
// filters, sorting and columns in the URL as on the Hosts list, and a
// Service's actions, where only deleting asks first.

const bob: User = { id: "user_2", name: "bob", role: "member", created_at: "2026-11-02T09:00:00Z" };

const attributes: AttrKey[] = [
  { key: "provider", type: "text", preset: true, in_use: false },
  { key: "price", type: "number", min: 0, preset: true, in_use: false },
  { key: "kind", type: "text", preset: false, in_use: true },
];

function service(host: string, name: string, fields: Partial<Service> = {}): Service {
  return {
    id: `service_${host}_${name}`,
    name,
    host,
    ports: [],
    attributes: {},
    created_at: "2026-11-01T09:00:00Z",
    updated_at: "2026-11-04T09:00:00Z",
    ...fields,
  };
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

function query(): string[][] {
  return [...new URLSearchParams(window.location.search).entries()];
}

function headers(): string[] {
  return within(screen.getByRole("table", { name: "Services" }))
    .getAllByRole("columnheader")
    .map((th) => th.textContent ?? "");
}

test("the Services list shows Host, name and ports, takes the same filters and sorting, and keeps them in the URL", async () => {
  const api = fakeApi();
  actAs(api, bob);
  services(api, [pg, nginx]);
  const first = renderAt("/services");

  await screen.findByRole("table", { name: "Services" });
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
  const keys = within(screen.getByLabelText("Key"))
    .getAllByRole("option")
    .map((o) => o.textContent);
  expect(keys).toEqual(["Pick a key", "id", "name", "host", "provider", "price", "kind"]);

  fireEvent.change(screen.getByLabelText("Key"), { target: { value: "host" } });
  fireEvent.change(screen.getByLabelText("Value"), { target: { value: "web1" } });
  fireEvent.click(screen.getByRole("button", { name: "Add filter" }));
  fireEvent.click(await screen.findByRole("button", { name: "name" }));
  fireEvent.click(within(screen.getByRole("group", { name: "Columns" })).getByRole("checkbox", { name: "kind" }));

  expect(query()).toEqual([
    ["filter", "host=web1"],
    ["sort", "name"],
    ["columns", "ports,kind"],
  ]);
  await screen.findByRole("table", { name: "Services" });
  expect(api.requests(ServiceList).at(-1)).toEqual({
    filters: [{ key: "host", op: "eq", value: "web1" }],
    sort: [{ key: "name" }],
  });
  expect(headers()).toEqual(["host", "name ▲", "ports", "kind"]);

  // A URL holding a key, a sort or a column the list does not take lists nothing.
  first.unmount();
  const listed = api.requests(ServiceList).length;
  renderAt("/services?filter=state=managed&sort=ports&columns=name&all=1");
  expect((await screen.findByRole("alert")).textContent).toContain("state=managed, ports, name");
  expect(api.requests(ServiceList)).toHaveLength(listed);
});

test("a Service's page shows its fields, Attributes and Note", async () => {
  const api = fakeApi();
  actAs(api, bob);
  services(api, [nginx]);

  renderAt("/services/web1/nginx");

  expect(await screen.findByRole("heading", { level: 1, name: "web1/nginx" })).toBeDefined();
  const fields = within(screen.getByRole("group", { name: "Fields" }))
    .getAllByRole("term")
    .map((dt) => [dt.textContent, dt.nextElementSibling?.textContent]);
  expect(fields).toEqual([
    ["host", "web1"],
    ["name", "nginx"],
    ["id", "service_web1_nginx"],
    ["ports", "80/tcp, 443/tcp"],
    ["created_at", "2026-11-01"],
    ["updated_at", "2026-11-04"],
  ]);
  expect(tableRows("Attributes")).toEqual([["kind", "proxy"]]);
  expect(screen.getByLabelText("Note").textContent).toBe("Terminates TLS.");
  expect(api.requests(ServiceShow)).toEqual([{ host: "web1", name: "nginx" }]);
});

test("a Service's edit form sends only what changed, clearing its ports when emptied, and rename never asks", async () => {
  const api = fakeApi();
  actAs(api, bob);
  const proxy = service("web1", "proxy");
  services(api, [nginx, proxy]);
  api.on(ServiceSet, () => ({ service: nginx }));
  api.on(ServiceRename, () => ({ service: proxy }));
  renderAt("/services/web1/nginx");

  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  const edit = within(await screen.findByRole("region", { name: "Edit web1/nginx" }));
  const type = (label: string, value: string) => fireEvent.change(edit.getByLabelText(label), { target: { value } });
  type("ports", "");
  type("kind", "edge");
  fireEvent.click(edit.getByRole("button", { name: "Save" }));
  await vi.waitFor(() => expect(screen.queryByRole("region", { name: "Edit web1/nginx" })).toBeNull());
  expect(api.requests(ServiceSet)).toEqual([
    { host: "web1", name: "nginx", attributes: { kind: "edge" }, unset: ["ports"] },
  ]);

  fireEvent.click(screen.getByRole("button", { name: "Edit" }));
  const again = within(await screen.findByRole("region", { name: "Edit web1/nginx" }));
  fireEvent.change(again.getByLabelText("ports"), { target: { value: "8080 8443/tcp" } });
  fireEvent.click(again.getByRole("button", { name: "Save" }));
  await vi.waitFor(() => expect(api.requests(ServiceSet)).toHaveLength(2));
  expect(api.requests(ServiceSet)[1]).toEqual({ host: "web1", name: "nginx", ports: ["8080", "8443/tcp"] });

  // Ports that split to none clear them: service.set refuses "ports": [].
  await vi.waitFor(() => expect(screen.queryByRole("region", { name: "Edit web1/nginx" })).toBeNull());
  fireEvent.click(screen.getByRole("button", { name: "Edit" }));
  const third = within(await screen.findByRole("region", { name: "Edit web1/nginx" }));
  fireEvent.change(third.getByLabelText("ports"), { target: { value: " , " } });
  fireEvent.click(third.getByRole("button", { name: "Save" }));
  await vi.waitFor(() => expect(api.requests(ServiceSet)).toHaveLength(3));
  expect(api.requests(ServiceSet)[2]).toEqual({ host: "web1", name: "nginx", unset: ["ports"] });

  fireEvent.click(await screen.findByRole("button", { name: "Rename" }));
  const rename = within(screen.getByRole("region", { name: "Rename web1/nginx" }));
  fireEvent.change(rename.getByLabelText("New name"), { target: { value: "proxy" } });
  fireEvent.click(rename.getByRole("button", { name: "Rename" }));
  expect(await screen.findByRole("heading", { level: 1, name: "web1/proxy" })).toBeDefined();
  expect(window.location.pathname).toBe("/services/web1/proxy");
  expect(api.requests(ServiceRename)).toEqual([{ host: "web1", name: "nginx", new_name: "proxy" }]);
  expect(screen.queryByRole("dialog")).toBeNull();
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
  const dialog = screen.getByRole("dialog", { name: "Delete web1/nginx?" });
  expect(dialog.textContent).toContain("web1/nginx is deleted for good, with its ports, Attributes and Note.");
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  expect(api.requests(ServiceRemove)).toEqual([]);

  fireEvent.click(screen.getByRole("button", { name: "Delete" }));
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }));

  await vi.waitFor(() => expect(window.location.pathname).toBe("/hosts/web1"));
  expect(api.requests(ServiceRemove)).toEqual([{ host: "web1", name: "nginx" }]);
});

test("a Service on a Retired Host is read-only", async () => {
  const api = fakeApi();
  actAs(api, bob);
  services(api, [service("host_old1", "nginx")]);

  renderAt("/services/host_old1/nginx");

  expect(await screen.findByText(/Its Host is Retired/)).toBeDefined();
  expect(screen.queryAllByRole("button").map((b) => b.textContent)).toEqual([]);
  expect(screen.getByRole("link", { name: "host_old1" }).getAttribute("href")).toBe("/hosts/host_old1");
});
