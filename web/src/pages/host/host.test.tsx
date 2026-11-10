import { fireEvent, screen, within } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";

import { AttrList } from "../../api/attr";
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
import { expectAccessible } from "../../test/axe";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { setViewportWidth } from "../../test/media";
import { actAs, findAlert, renderAt } from "../../test/pages";
import { actions, attributes, bob, clipboard, host as record, metadata, service, writes } from "./records.fixture";

// A Host's page on Astryx's detail-page pattern, against the fake API: one
// test for each action of the page inventory (docs/subsystems/web-shell.md),
// which actions ask first, and edit dialogs that send only what changed.

function host(name: string, fields: Partial<Host> = {}): Host {
  return record(name, {
    host_key: "SHA256:qXk2iP0nTd1mXbV6oZp3yR7cLw9sUe4fGh5jKa8bNc0",
    updated_at: "2026-11-03T10:00:00Z",
    ...fields,
  });
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

/** dialog waits for the dialog named title and is it. */
async function dialog(title: string) {
  return within(await screen.findByRole("dialog", { name: title }));
}

function type(scope: Awaited<ReturnType<typeof dialog>>, label: string, value: string): void {
  fireEvent.change(scope.getByRole("textbox", { name: label }), { target: { value } });
}

/** services are the text of each of the page's line items, its Services. */
function services(name: string): string[] {
  return within(screen.getByRole("list", { name: `Services of ${name}` }))
    .getAllByRole("listitem")
    .map((li) => li.textContent ?? "");
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllEnvs();
});

test("a Host's page shows its fields, Attributes, Services as its line items, host key and its Note in the rail", async () => {
  const api = fakeApi();
  actAs(api, bob);
  showing(api, [web1, [nginx, wg]]);

  renderAt("/hosts/web1");

  expect(await screen.findByRole("heading", { level: 1, name: "web1" })).toBeDefined();
  expect(metadata("Fields")).toEqual([
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
  expect(metadata("Attributes")).toEqual([
    ["currency", "EUR"],
    ["price", "4.5"],
    ["provider", "hetzner"],
    ["region", "fsn1"],
  ]);
  const rail = within(screen.getByRole("region", { name: "Note" }));
  expect(rail.getByText(/^Rebuilt on/).textContent).toBe("Rebuilt on 2026-10-30.\nAsk ops before a reboot.");
  expect(services("web1")).toEqual(["nginx80/tcp, 443/tcp", "wg51820/udp"]);
  expect(screen.getByRole("link", { name: /^nginx/ }).getAttribute("href")).toBe("/services/web1/nginx");
  expect(screen.getByText("Space ops")).toBeDefined();
  expect(within(screen.getByRole("main")).getByRole("link", { name: "Hosts" }).getAttribute("href")).toBe("/hosts");
  expect(actions()).toEqual(["Edit", "Rename", "Move", "Retire"]);
  expect(api.requests(HostShow)).toEqual([{ host: "web1" }]);
  await expectAccessible();
});

test("on a narrow screen the Note moves from the rail under the Attributes", async () => {
  setViewportWidth(390);
  const api = fakeApi();
  actAs(api, bob);
  showing(api, [web1, []]);

  renderAt("/hosts/web1");

  expect(await screen.findByText(/^Rebuilt on/)).toBeDefined();
  expect(screen.queryByRole("region", { name: "Note" })).toBeNull();
  await expectAccessible();
});

test("a Registered Host's Enroll shows koko host enroll <host> to copy; a Relay unseen and no pin show as such", async () => {
  const writeText = clipboard();
  const api = fakeApi();
  actAs(api, bob);
  const db1 = host("db1", { state: "registered", host_key: null, relay: { id: null, name: null } });
  showing(api, [db1, []]);
  renderAt("/hosts/db1");

  fireEvent.click(await screen.findByRole("button", { name: "Enroll" }));

  const enroll = await dialog("Enroll db1");
  const command = enroll.getByText("koko host enroll db1");
  expect(command.tagName).toBe("CODE");
  await expectAccessible();
  fireEvent.click(enroll.getByRole("button", { name: "Copy" }));
  expect(await enroll.findByText("Copied.")).toBeDefined();
  expect(writeText).toHaveBeenCalledWith("koko host enroll db1");
  fireEvent.click(enroll.getByRole("button", { name: "Done" }));

  expect(metadata("Fields").filter(([key]) => key === "relay" || key === "host_key")).toEqual([
    ["relay", "A Relay you cannot see"],
    ["host_key", "Not pinned"],
  ]);
  expect(screen.getByText("No Attributes.")).toBeDefined();
  expect(screen.getByText("No Note.")).toBeDefined();
  expect(screen.getByText("No Services.")).toBeDefined();
  expect(actions()).toEqual(["Enroll", "Edit", "Rename", "Move", "Retire", "Delete"]);
});

test("a Host no one can see says so", async () => {
  const api = fakeApi();
  actAs(api, bob);
  showing(api);

  renderAt("/hosts/nope");

  expect(await findAlert()).toBe("No Host you can see has that name.");
});

test("the edit dialog sends only the fields that changed, clears what was emptied, and never asks", async () => {
  vi.useFakeTimers({ toFake: ["Date"], shouldAdvanceTime: true });
  vi.setSystemTime(new Date("2026-11-08T12:00:00Z"));
  vi.stubEnv("TZ", "UTC");
  const api = fakeApi();
  actAs(api, bob);
  showing(api, [web1, []]);
  api.on(HostSet, (request) => ({ host: { ...web1, address: request.address ?? web1.address } }));
  renderAt("/hosts/web1");
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  const edit = await dialog("Edit web1");
  await edit.findByRole("textbox", { name: "address" });
  await expectAccessible();

  type(edit, "address", "web2.example.com");
  type(edit, "price", "ten");
  expect(edit.getByRole("alert").textContent).toContain("price");
  expect(edit.getByRole("button", { name: "Save" }).hasAttribute("disabled")).toBe(true);
  type(edit, "price", "5");
  type(edit, "region", "");
  type(edit, "New Attribute key", "expires_at");
  type(edit, "Value of the new Attribute", "+1y");
  type(edit, "Note", "");
  // A field set back as it was is no change.
  type(edit, "account", "root");
  type(edit, "account", "deploy");
  fireEvent.click(edit.getByRole("button", { name: "Save" }));

  await vi.waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit web1" })).toBeNull());
  expect(screen.queryByRole("alertdialog")).toBeNull();
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

test("an unchanged edit dialog cannot be saved, and the port, Relay and an enum clear when emptied", async () => {
  const api = fakeApi();
  actAs(api, bob);
  showing(api, [{ ...web1, port: 2222 }, []]);
  api.on(HostSet, () => ({ host: web1 }));
  renderAt("/hosts/web1");
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  const edit = await dialog("Edit web1");
  await edit.findByRole("textbox", { name: "address" });
  expect(edit.getByRole("button", { name: "Save" }).hasAttribute("disabled")).toBe(true);

  type(edit, "port", "0");
  expect(edit.getByRole("button", { name: "Save" }).hasAttribute("disabled")).toBe(true);
  type(edit, "port", "");
  type(edit, "relay", "");
  fireEvent.click(edit.getByRole("button", { name: /^Clear/ }));
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
  const rename = await dialog("Rename web1");
  type(rename, "New name", "web2");
  fireEvent.click(rename.getByRole("button", { name: "Rename" }));
  expect(await screen.findByRole("heading", { level: 1, name: "web2" })).toBeDefined();
  expect(window.location.pathname).toBe("/hosts/web2");
  expect(api.requests(HostRename)).toEqual([{ host: "host_web1", new_name: "web2" }]);

  fireEvent.click(screen.getByRole("button", { name: "Move" }));
  const move = await dialog("Move web2 to another Space");
  fireEvent.click(move.getByRole("combobox", { name: "Space" }));
  expect((await screen.findAllByRole("option")).map((o) => o.textContent)).toEqual(["bob"]);
  await expectAccessible();
  fireEvent.click(screen.getByRole("option", { name: "bob" }));
  fireEvent.click(move.getByRole("button", { name: "Move" }));
  await vi.waitFor(() => expect(api.requests(HostMove)).toEqual([{ host: "host_web1", space: "bob" }]));
  expect(screen.queryByRole("alertdialog")).toBeNull();
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
  const asked = screen.getByRole("alertdialog", { name: "Retire web1" });
  expect(asked.textContent).toContain(
    "web1 leaves the list of Hosts and gives up its name, which a new Host may then take",
  );
  expect(asked.textContent).toContain("under its id, host_web1");
  await expectAccessible();
  fireEvent.click(within(asked).getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("alertdialog")).toBeNull();
  expect(writes(api)).toEqual([]);

  fireEvent.click(screen.getByRole("button", { name: "Retire" }));
  fireEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Retire" }));

  await vi.waitFor(() => expect(window.location.pathname).toBe("/hosts/host_web1"));
  expect(await screen.findAllByText("retired")).toHaveLength(2); // its state's Badge and field
  expect(api.requests(HostRetire)).toEqual([{ host: "host_web1" }]);
});

test("a Retired Host takes only changes to its Attributes and Note, moves, and deletes with no pin; its Services are read-only", async () => {
  const api = fakeApi();
  actAs(api, bob);
  const old = host("web1", { id: "host_old1", state: "retired", attributes: { provider: "hetzner" } });
  showing(api, [old, [{ ...nginx, host: "host_old1" }]]);
  api.on(HostSet, () => ({ host: old }));
  api.on(HostMove, () => ({ host: { ...old, space: "bob" } }));
  renderAt("/hosts/host_old1");

  await screen.findByRole("heading", { level: 1, name: "web1" });
  expect(actions()).toEqual(["Edit", "Move"]);
  expect(screen.getByRole("link", { name: /^nginx/ }).getAttribute("href")).toBe("/services/host_old1/nginx");
  expect(screen.queryByRole("button", { name: "Add Service" })).toBeNull();
  await expectAccessible();

  fireEvent.click(screen.getByRole("button", { name: "Edit" }));
  const edit = await dialog("Edit web1");
  await edit.findByRole("textbox", { name: "provider" });
  expect(edit.queryByRole("textbox", { name: "address" })).toBeNull();
  expect(edit.queryByRole("textbox", { name: "relay" })).toBeNull();
  type(edit, "provider", "ovh");
  type(edit, "Note", "Kept for its logs.");
  fireEvent.click(edit.getByRole("button", { name: "Save" }));
  await vi.waitFor(() =>
    expect(api.requests(HostSet)).toEqual([
      { host: "host_old1", attributes: { provider: "ovh" }, note: "Kept for its logs." },
    ]),
  );

  fireEvent.click(await screen.findByRole("button", { name: "Move" }));
  const move = await dialog("Move web1 to another Space");
  fireEvent.click(move.getByRole("combobox", { name: "Space" }));
  fireEvent.click(await screen.findByRole("option", { name: "bob" }));
  fireEvent.click(move.getByRole("button", { name: "Move" }));
  await vi.waitFor(() => expect(api.requests(HostMove)).toEqual([{ host: "host_old1", space: "bob" }]));
});

test("a Retired Host with no pin offers Delete, by its id", async () => {
  const api = fakeApi();
  actAs(api, bob);
  const old = host("web1", { id: "host_old1", state: "retired", host_key: null });
  showing(api, [old, []]);
  api.on(HostRemove, () => ({ host: old }));
  api.on(HostList, () => ({ hosts: [] }));
  renderAt("/hosts/host_old1");

  fireEvent.click(await screen.findByRole("button", { name: "Delete" }));
  expect(actions()).toEqual(["Edit", "Move", "Delete"]);
  fireEvent.click(within(screen.getByRole("alertdialog", { name: "Delete web1" })).getByRole("button", { name: "Delete" }));

  await vi.waitFor(() => expect(window.location.pathname).toBe("/hosts"));
  expect(api.requests(HostRemove)).toEqual([{ host: "host_old1" }]);
});

test("delete is offered only for a Host never Managed, asks first, and goes back to the Hosts", async () => {
  const api = fakeApi();
  actAs(api, bob);
  const db1 = host("db1", { state: "registered", host_key: null });
  showing(api, [web1, []], [db1, [service("pg", { host: "db1" }), service("redis", { host: "db1" })]]);
  api.on(HostRemove, () => ({ host: db1 }));
  api.on(HostList, () => ({ hosts: [web1] }));
  const first = renderAt("/hosts/web1");
  await screen.findByRole("heading", { level: 1, name: "web1" });
  expect(screen.queryByRole("button", { name: "Delete" })).toBeNull();
  first.unmount();

  renderAt("/hosts/db1");
  fireEvent.click(await screen.findByRole("button", { name: "Delete" }));
  const asked = screen.getByRole("alertdialog", { name: "Delete db1" });
  expect(asked.textContent).toContain(
    "db1 is deleted permanently, with its Attributes, its Note and every Service on it, 2 in all.",
  );
  await expectAccessible();
  fireEvent.click(within(asked).getByRole("button", { name: "Delete" }));

  expect(await screen.findByRole("heading", { level: 1, name: "Hosts" })).toBeDefined();
  expect(window.location.pathname).toBe("/hosts");
  expect(api.requests(HostRemove)).toEqual([{ host: "host_db1" }]);
});

test("deleting a Host with no Services says so without a count", async () => {
  const api = fakeApi();
  actAs(api, bob);
  showing(api, [host("db1", { state: "registered", host_key: null }), []]);
  renderAt("/hosts/db1");

  fireEvent.click(await screen.findByRole("button", { name: "Delete" }));

  expect(screen.getByRole("alertdialog").textContent).toContain(
    "db1 is deleted permanently, with its Attributes and its Note. This cannot be undone.",
  );
});

test("a Service added on a Host's page is recorded with its ports, and the page moves to it", async () => {
  const api = fakeApi();
  actAs(api, bob);
  showing(api, [web1, []]);
  api.on(ServiceAdd, (request) => ({ service: service(request.name, { ports: request.ports ?? [] }) }));
  renderAt("/hosts/web1");
  fireEvent.click(await screen.findByRole("button", { name: "Add Service" }));
  const add = await dialog("Add Service");
  await expectAccessible();

  type(add, "name", "nginx");
  type(add, "ports", "80, 443/tcp");
  fireEvent.click(add.getByRole("button", { name: "Add" }));

  await vi.waitFor(() => expect(window.location.pathname).toBe("/services/web1/nginx"));
  expect(api.requests(ServiceAdd)).toEqual([{ host: "host_web1", name: "nginx", ports: ["80", "443/tcp"] }]);
});

test("a Relay the caller cannot see shows as such in the edit dialog, and emptying it clears it", async () => {
  const api = fakeApi();
  actAs(api, bob);
  showing(api, [host("app1", { relay: { id: null, name: null } }), []]);
  api.on(HostSet, () => ({ host: web1 }));
  renderAt("/hosts/app1");
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  const edit = await dialog("Edit app1");

  const relay = (await edit.findByRole("textbox", { name: "relay" })) as HTMLInputElement;
  expect(relay.value).toBe("A Relay you cannot see");
  type(edit, "relay", "");
  fireEvent.click(edit.getByRole("button", { name: "Save" }));

  await vi.waitFor(() => expect(api.requests(HostSet)).toEqual([{ host: "host_app1", unset: ["relay"] }]));
});

test("an open edit dialog keeps what was typed when the page reloads a record someone else changed", async () => {
  const api = fakeApi();
  actAs(api, bob);
  let current = web1;
  api.on(AttrList, () => ({ attributes }));
  api.on(HostShow, () => ({ host: current, services: [] }));
  renderAt("/hosts/web1");
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  const edit = await dialog("Edit web1");
  await edit.findByRole("textbox", { name: "address" });
  type(edit, "address", "web2.example.com");

  current = { ...web1, account: "root", updated_at: "2026-11-08T10:00:00Z" };
  fireEvent.focus(window);

  await vi.waitFor(() => expect(metadata("Fields")).toContainEqual(["account", "root"]));
  expect((edit.getByRole("textbox", { name: "address" }) as HTMLInputElement).value).toBe("web2.example.com");
});

test("a dialog's Cancel sends nothing and gives the focus back to the action's button", async () => {
  const api = fakeApi();
  actAs(api, bob);
  showing(api, [web1, []]);
  renderAt("/hosts/web1");
  const rename = await screen.findByRole("button", { name: "Rename" });
  rename.focus();
  fireEvent.click(rename);

  fireEvent.click((await dialog("Rename web1")).getByRole("button", { name: "Cancel" }));

  await vi.waitFor(() => expect(screen.queryByRole("dialog", { name: "Rename web1" })).toBeNull());
  await vi.waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Rename" })));
  expect(writes(api)).toEqual([]);
});
