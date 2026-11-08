import { fireEvent, screen, within } from "@testing-library/react";
import { afterEach, expect, onTestFinished, test, vi } from "vitest";

import { type AttrKey, AttrList } from "../../api/attr";
import {
  type Host,
  HostList,
  HostMove,
  HostRemove,
  HostRename,
  HostRetire,
  HostSet,
  HostShow,
  type HostShowRequest,
} from "../../api/host";
import { type Service, ServiceAdd } from "../../api/service";
import { SpaceList } from "../../api/space";
import type { User } from "../../api/user";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { actAs, renderAt, tableRows } from "../../test/pages";

// A Host's page and the Host actions, against the fake API: what the page
// shows, which actions ask first, and edit forms that send only what changed.

const bob: User = { id: "user_2", name: "bob", role: "member", created_at: "2026-11-02T09:00:00Z" };

const attributes: AttrKey[] = [
  { key: "provider", type: "text", preset: true, in_use: true },
  { key: "region", type: "text", preset: true, in_use: true },
  { key: "price", type: "number", min: 0, preset: true, in_use: true },
  { key: "currency", type: "enum", values: ["CNY", "EUR", "USD"], preset: true, in_use: true },
  { key: "expires_at", type: "date", preset: true, in_use: true },
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
    host_key: "SHA256:qXk2iP0nTd1mXbV6oZp3yR7cLw9sUe4fGh5jKa8bNc0",
    attributes: {},
    created_at: "2026-11-01T09:00:00Z",
    updated_at: "2026-11-03T10:00:00Z",
    ...fields,
  };
}

function service(name: string, fields: Partial<Service> = {}): Service {
  return {
    id: `service_${name}`,
    name,
    host: "web1",
    ports: [],
    attributes: {},
    created_at: "2026-11-01T09:00:00Z",
    updated_at: "2026-11-01T09:00:00Z",
    ...fields,
  };
}

const web1 = host("web1", {
  relay: { id: "host_gw1", name: "gw1" },
  attributes: { provider: "hetzner", region: "fsn1", price: 4.5, currency: "EUR" },
  note: "Rebuilt on 2026-10-30.\nAsk ops before a reboot.",
});
const nginx = service("nginx", { ports: ["80/tcp", "443/tcp"] });
const wg = service("wg", { ports: ["51820/udp"] });

/**
 * showing answers host.show with each Host given, by name or id, and its
 * Services, and attr.list and space.list for the forms.
 */
function showing(api: FakeApi, ...hosts: [Host, Service[]][]): void {
  api.on(AttrList, () => ({ attributes }));
  api.on(SpaceList, () => ({
    spaces: [
      { id: "space_1", name: "bob", personal: true, created_at: "2026-11-02T09:00:00Z" },
      { id: "space_2", name: "ops", personal: false, created_at: "2026-11-02T09:00:00Z" },
    ],
  }));
  api.on(HostShow, ({ host: named }: HostShowRequest) => {
    const found = hosts.find(([h]) => h.id === named || (h.name === named && h.state !== "retired"));
    if (!found) {
      throw refusal("unknown_host", 404);
    }
    return { host: found[0], services: found[1] };
  });
}

/** fields are the page's fields, as key and value. */
function fields(): string[][] {
  const list = screen.getByRole("group", { name: "Fields" });
  return within(list)
    .getAllByRole("term")
    .map((dt) => [dt.textContent ?? "", dt.nextElementSibling?.textContent ?? ""]);
}

/** clipboard stands in for the browser's clipboard for one test. */
function clipboard() {
  const writeText = vi.fn((_: string) => Promise.resolve());
  Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
  onTestFinished(() => {
    Reflect.deleteProperty(navigator, "clipboard");
  });
  return writeText;
}

/** writes are the paths of every write the page sent, in order. */
function writes(api: FakeApi): string[] {
  return api.calls.filter((c) => c.headers["idempotency-key"] !== undefined).map((c) => c.path);
}

/** form is the open form titled title. */
function form(title: string) {
  return within(screen.getByRole("region", { name: title }));
}

function type(scope: ReturnType<typeof form>, label: string, value: string): void {
  fireEvent.change(scope.getByLabelText(label), { target: { value } });
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllEnvs();
});

test("a Host's page shows its fields, Attributes, Note, Services and host key fingerprint", async () => {
  const api = fakeApi();
  actAs(api, bob);
  showing(api, [web1, [nginx, wg]]);

  renderAt("/hosts/web1");

  expect(await screen.findByRole("heading", { level: 1, name: "web1" })).toBeDefined();
  expect(fields()).toEqual([
    ["id", "host_web1"],
    ["space", "ops"],
    ["state", "managed"],
    ["address", "web1.example.com"],
    ["port", "22"],
    ["account", "deploy"],
    ["relay", "gw1"],
    ["host_key", "SHA256:qXk2iP0nTd1mXbV6oZp3yR7cLw9sUe4fGh5jKa8bNc0"],
    ["created_at", "2026-11-01"],
    ["updated_at", "2026-11-03"],
  ]);
  expect(screen.getByRole("link", { name: "gw1" }).getAttribute("href")).toBe("/hosts/host_gw1");
  expect(tableRows("Attributes")).toEqual([
    ["currency", "EUR"],
    ["price", "4.5"],
    ["provider", "hetzner"],
    ["region", "fsn1"],
  ]);
  expect(screen.getByLabelText("Note").textContent).toBe("Rebuilt on 2026-10-30.\nAsk ops before a reboot.");
  expect(tableRows("Services")).toEqual([
    ["nginx", "80/tcp, 443/tcp"],
    ["wg", "51820/udp"],
  ]);
  expect(screen.getByRole("link", { name: "nginx" }).getAttribute("href")).toBe("/services/web1/nginx");
  expect(screen.queryByText(/koko host enroll/)).toBeNull();
  expect(api.requests(HostShow)).toEqual([{ host: "web1" }]);
});

test("a Registered Host shows koko host enroll <host> to copy; a Relay unseen and no pin show as such", async () => {
  const writeText = clipboard();
  const api = fakeApi();
  actAs(api, bob);
  const db1 = host("db1", { state: "registered", host_key: null, relay: { id: null, name: null } });
  showing(api, [db1, []]);

  renderAt("/hosts/db1");

  const command = await screen.findByText("koko host enroll db1");
  expect(command.tagName).toBe("CODE");
  fireEvent.click(screen.getByRole("button", { name: "Copy" }));
  expect(await screen.findByText("Copied.")).toBeDefined();
  expect(writeText).toHaveBeenCalledWith("koko host enroll db1");
  expect(fields().filter(([key]) => key === "relay" || key === "host_key")).toEqual([
    ["relay", "(one you cannot see)"],
    ["host_key", "none"],
  ]);
  expect(screen.getByText("No Attributes.")).toBeDefined();
  expect(screen.getByText("No Note.")).toBeDefined();
  expect(screen.getByText("No Services.")).toBeDefined();
});

test("a Host no one can see says so", async () => {
  const api = fakeApi();
  actAs(api, bob);
  showing(api);

  renderAt("/hosts/nope");

  expect((await screen.findByRole("alert")).textContent).toBe("No Host you can see has that name.");
});

test("the edit form sends only the fields that changed, clears what was emptied, and never asks", async () => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-11-08T12:00:00Z"));
  vi.stubEnv("TZ", "UTC");
  const api = fakeApi();
  actAs(api, bob);
  showing(api, [web1, []]);
  api.on(HostSet, (request) => ({ host: { ...web1, address: request.address ?? web1.address } }));
  renderAt("/hosts/web1");
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  const edit = within(await screen.findByRole("region", { name: "Edit web1" }));

  type(edit, "address", "web2.example.com");
  type(edit, "price", "ten");
  expect(edit.getByRole("alert").textContent).toContain("price");
  expect(edit.getByRole("button", { name: "Save" }).hasAttribute("disabled")).toBe(true);
  type(edit, "price", "5");
  type(edit, "region", "");
  type(edit, "New Attribute key", "expires_at");
  type(edit, "Its value", "+1y");
  type(edit, "Note", "");
  // A field set back as it was is no change.
  type(edit, "account", "root");
  type(edit, "account", "deploy");
  fireEvent.click(edit.getByRole("button", { name: "Save" }));

  await vi.waitFor(() => expect(screen.queryByRole("region", { name: "Edit web1" })).toBeNull());
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(api.requests(HostSet)).toEqual([
    {
      host: "host_web1",
      address: "web2.example.com",
      attributes: { price: 5, expires_at: "2027-11-08" },
      unset: ["region", "note"],
    },
  ]);
  expect(api.requests(HostShow)).toHaveLength(2);
});

test("an unchanged edit form cannot be saved, and the port, Relay and an enum clear when emptied", async () => {
  const api = fakeApi();
  actAs(api, bob);
  showing(api, [{ ...web1, port: 2222 }, []]);
  api.on(HostSet, () => ({ host: web1 }));
  renderAt("/hosts/web1");
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  const edit = within(await screen.findByRole("region", { name: "Edit web1" }));
  expect(edit.getByRole("button", { name: "Save" }).hasAttribute("disabled")).toBe(true);

  type(edit, "port", "0");
  expect(edit.getByRole("button", { name: "Save" }).hasAttribute("disabled")).toBe(true);
  type(edit, "port", "");
  type(edit, "relay", "");
  type(edit, "currency", "");
  type(edit, "address", "");
  expect(edit.getByRole("button", { name: "Save" }).hasAttribute("disabled")).toBe(true);
  type(edit, "address", "web1.example.com");
  fireEvent.click(edit.getByRole("button", { name: "Save" }));

  await vi.waitFor(() => expect(api.requests(HostSet)).toHaveLength(1));
  expect(api.requests(HostSet)).toEqual([{ host: "host_web1", unset: ["port", "relay", "currency"] }]);
});

test("rename moves the page to the new name and move to another Space, neither asking", async () => {
  const api = fakeApi();
  actAs(api, bob);
  const web2 = { ...web1, id: "host_web1", name: "web2" };
  showing(api, [web1, []], [web2, []]);
  api.on(HostRename, () => ({ host: web2 }));
  api.on(HostMove, () => ({ host: { ...web2, space: "bob" } }));
  renderAt("/hosts/web1");

  fireEvent.click(await screen.findByRole("button", { name: "Rename" }));
  type(form("Rename web1"), "New name", "web2");
  fireEvent.click(form("Rename web1").getByRole("button", { name: "Rename" }));
  expect(await screen.findByRole("heading", { level: 1, name: "web2" })).toBeDefined();
  expect(window.location.pathname).toBe("/hosts/web2");
  expect(api.requests(HostRename)).toEqual([{ host: "host_web1", new_name: "web2" }]);

  fireEvent.click(screen.getByRole("button", { name: "Move" }));
  const move = form("Move web2");
  await move.findByRole("option", { name: "bob" });
  expect(move.queryByRole("option", { name: "ops" })).toBeNull();
  fireEvent.change(move.getByLabelText("To Space"), { target: { value: "bob" } });
  fireEvent.click(move.getByRole("button", { name: "Move" }));
  await vi.waitFor(() => expect(api.requests(HostMove)).toEqual([{ host: "host_web1", space: "bob" }]));
  expect(screen.queryByRole("dialog")).toBeNull();
});

test("retire asks first, stating the consequence, and the page then names the Host by its id", async () => {
  const api = fakeApi();
  actAs(api, bob);
  const retired = { ...web1, state: "retired" };
  // Once Retired, its id names it, and its name no longer does.
  showing(api, [retired, [{ ...nginx, host: "host_web1" }]], [web1, [nginx]]);
  api.on(HostRetire, () => ({ host: retired }));
  renderAt("/hosts/web1");

  fireEvent.click(await screen.findByRole("button", { name: "Retire" }));
  const dialog = screen.getByRole("dialog", { name: "Retire web1?" });
  expect(dialog.textContent).toContain(
    "web1 leaves the list of Hosts and gives up its name, which a new Host may then take",
  );
  expect(dialog.textContent).toContain("named only by its id, host_web1, from then on");
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(writes(api)).toEqual([]);

  fireEvent.click(screen.getByRole("button", { name: "Retire" }));
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Retire" }));

  expect(await screen.findByText("(Retired)")).toBeDefined();
  expect(window.location.pathname).toBe("/hosts/host_web1");
  expect(api.requests(HostRetire)).toEqual([{ host: "host_web1" }]);
});

test("a Retired Host takes only changes to its Attributes and Note, and moves; its Services are read-only", async () => {
  const api = fakeApi();
  actAs(api, bob);
  const old = host("web1", { id: "host_old1", state: "retired", attributes: { provider: "hetzner" } });
  showing(api, [old, [{ ...nginx, host: "host_old1" }]]);
  renderAt("/hosts/host_old1");

  await screen.findByText("(Retired)");
  const actions = screen.getAllByRole("button").map((b) => b.textContent);
  expect(actions).toEqual(["Edit", "Move"]);
  expect(screen.getByRole("link", { name: "nginx" }).getAttribute("href")).toBe("/services/host_old1/nginx");
  expect(screen.queryByRole("region", { name: "Add a Service" })).toBeNull();

  fireEvent.click(screen.getByRole("button", { name: "Edit" }));
  const edit = within(await screen.findByRole("region", { name: "Edit web1" }));
  expect(edit.queryByLabelText("address")).toBeNull();
  expect(edit.queryByLabelText("relay")).toBeNull();
});

test("delete is offered only for a Host never Managed, asks first, and goes back to the Hosts", async () => {
  const api = fakeApi();
  actAs(api, bob);
  const db1 = host("db1", { state: "registered", host_key: null });
  showing(api, [web1, []], [db1, [service("pg", { host: "db1" }), service("redis", { host: "db1" })]]);
  api.on(HostRemove, () => ({ host: db1 }));
  api.on(AttrList, () => ({ attributes }));
  api.on(HostList, () => ({ hosts: [web1] }));
  const first = renderAt("/hosts/web1");
  await screen.findByRole("heading", { level: 1, name: "web1" });
  expect(screen.queryByRole("button", { name: "Delete" })).toBeNull();
  first.unmount();

  renderAt("/hosts/db1");
  fireEvent.click(await screen.findByRole("button", { name: "Delete" }));
  const dialog = screen.getByRole("dialog", { name: "Delete db1?" });
  expect(dialog.textContent).toContain(
    "db1 is deleted for good, with its Attributes, its Note and every Service on it, 2 in all.",
  );
  fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));

  expect(await screen.findByRole("heading", { level: 1, name: "Hosts" })).toBeDefined();
  expect(api.requests(HostRemove)).toEqual([{ host: "host_db1" }]);
});

test("a Service added on a Host's page is recorded with its ports, and the page moves to it", async () => {
  const api = fakeApi();
  actAs(api, bob);
  showing(api, [web1, []]);
  api.on(ServiceAdd, (request) => ({ service: service(request.name, { ports: request.ports ?? [] }) }));
  renderAt("/hosts/web1");
  const add = within(await screen.findByRole("region", { name: "Add a Service" }));

  type(add, "name", "nginx");
  type(add, "ports", "80, 443/tcp");
  fireEvent.click(add.getByRole("button", { name: "Add Service" }));

  await vi.waitFor(() => expect(window.location.pathname).toBe("/services/web1/nginx"));
  expect(api.requests(ServiceAdd)).toEqual([{ host: "host_web1", name: "nginx", ports: ["80", "443/tcp"] }]);
});

test("a Relay the caller cannot see shows as such in the edit form, and emptying it clears it", async () => {
  const api = fakeApi();
  actAs(api, bob);
  showing(api, [host("app1", { relay: { id: null, name: null } }), []]);
  api.on(HostSet, () => ({ host: web1 }));
  renderAt("/hosts/app1");
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  const edit = within(await screen.findByRole("region", { name: "Edit app1" }));

  expect((edit.getByLabelText("relay") as HTMLInputElement).value).toBe("(one you cannot see)");
  type(edit, "relay", "");
  fireEvent.click(edit.getByRole("button", { name: "Save" }));

  await vi.waitFor(() => expect(api.requests(HostSet)).toEqual([{ host: "host_app1", unset: ["relay"] }]));
});

test("an open edit form keeps what was typed when the page reloads a record someone else changed", async () => {
  const api = fakeApi();
  actAs(api, bob);
  let current = web1;
  api.on(AttrList, () => ({ attributes }));
  api.on(HostShow, () => ({ host: current, services: [] }));
  renderAt("/hosts/web1");
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  const edit = within(await screen.findByRole("region", { name: "Edit web1" }));
  type(edit, "address", "web2.example.com");

  current = { ...web1, account: "root", updated_at: "2026-11-08T10:00:00Z" };
  fireEvent.focus(window);

  await screen.findByText("root");
  expect((edit.getByLabelText("address") as HTMLInputElement).value).toBe("web2.example.com");
});

test("deleting a Host with no Services says so without a count", async () => {
  const api = fakeApi();
  actAs(api, bob);
  showing(api, [host("db1", { state: "registered", host_key: null }), []]);
  renderAt("/hosts/db1");

  fireEvent.click(await screen.findByRole("button", { name: "Delete" }));

  expect(screen.getByRole("dialog").textContent).toContain(
    "db1 is deleted for good, with its Attributes and its Note. Nothing brings it back.",
  );
});
