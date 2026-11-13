import { act, configure, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { AttrList } from "../../api/attr";
import { CodeNameTaken } from "../../api/errors";
import { CodeUnknownHost, type Host, HostList, HostShow } from "../../api/host";
import { SecretList } from "../../api/secret";
import {
  CodeUnknownService,
  type Service,
  ServiceAdd,
  ServiceRemove,
  ServiceRename,
  ServiceSet,
  ServiceShow,
} from "../../api/service";
import { SpaceList } from "../../api/space";
import { expectAccessible } from "../../test/axe";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { clickOutside } from "../../test/controls";
import { actAs, alerts, renderAt, statuses } from "../../test/pages";
import { alice, attributes, host, service, spaces, writes } from "./records.fixture";

// A Service's panel over its Host's page, against the fake API: what
// service.show answers, Save as one service.set then service.rename, Delete
// asking first, Add Service as one service.add, a Retired Host's Service
// read-only, and the redirect from the page a released koko served.

// A test here renders the whole page and drives several overlays in turn;
// its limits are ceilings for a slow runner, not waits.
vi.setConfig({ testTimeout: 20_000 });
configure({ asyncUtilTimeout: 5_000 });

const web1 = host("web1");
const old = host("old", { id: "host_9", state: "retired" });
const nginx = service("nginx", {
  ports: ["80/tcp", "443/tcp"],
  attributes: { tier: "edge" },
  note: "Serves the shop.",
});

/**
 * served answers host.show and service.show for the Hosts and Services
 * given, by the Host's name or id, and the reads the page makes.
 */
function served(api: FakeApi, hosts: Host[], services: Service[]): void {
  const find = (named: string) => hosts.find((h) => h.id === named || (h.name === named && h.state !== "retired"));
  const ref = (h: Host) => (h.state === "retired" ? h.id : h.name);
  api.on(HostShow, ({ host: named }) => {
    const shown = find(named);
    if (!shown) {
      throw refusal(CodeUnknownHost, 404);
    }
    return {
      host: shown,
      path: shown.relay === null ? [] : [shown.relay],
      services: services.filter((s) => s.host === ref(shown)).map(({ note: _, ...s }) => s),
    };
  });
  api.on(ServiceShow, ({ host: named, name }) => {
    const on = find(named);
    const shown = on && services.find((s) => s.host === ref(on) && s.name === name);
    if (!shown) {
      throw refusal(CodeUnknownService, 404);
    }
    return { service: shown };
  });
  api.on(HostList, () => ({ hosts }));
  api.on(SpaceList, () => ({ spaces }));
  api.on(AttrList, () => ({ attributes }));
  api.on(SecretList, () => ({ secrets: [] }));
}

/** type types value into the text field named name. */
function type(name: string, value: string): void {
  fireEvent.change(screen.getByRole("textbox", { name }), { target: { value } });
}

/** openPanel opens the Service named name from its row on the Host's page, and waits for its fields. */
async function openPanel(name: string): Promise<HTMLElement> {
  const row = await screen.findByRole("link", { name: new RegExp(`^${name}`) });
  act(() => row.focus());
  fireEvent.click(row);
  const panel = await screen.findByRole("dialog", { name });
  await within(panel).findByRole("textbox", { name: "Ports" });
  return panel;
}

test("a Service's panel opens from its row with its name, ports, Attributes and Note, and Escape gives the focus back to the row", async () => {
  const api = fakeApi();
  actAs(api, alice);
  served(api, [web1], [nginx]);

  renderAt("/hosts/web1");
  const panel = await openPanel("nginx");
  expect(window.location.pathname).toBe("/hosts/web1/services/nginx");
  expect(api.requests(ServiceShow)).toEqual([{ host: "host_web1", name: "nginx" }]);
  expect(panel.textContent).toContain("Service on web1");
  expect((within(panel).getByRole("textbox", { name: "Name" }) as HTMLInputElement).value).toBe("nginx");
  expect((within(panel).getByRole("textbox", { name: "Ports" }) as HTMLInputElement).value).toBe("80/tcp, 443/tcp");
  expect((within(panel).getByRole("combobox", { name: "Attribute key" }) as HTMLInputElement).value).toBe("tier");
  expect((within(panel).getByRole("textbox", { name: "Value of tier" }) as HTMLInputElement).value).toBe("edge");
  expect((within(panel).getByRole("textbox", { name: "Note" }) as HTMLInputElement).value).toBe("Serves the shop.");
  await expectAccessible();

  fireEvent.keyDown(panel, { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(window.location.pathname).toBe("/hosts/web1");
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("link", { name: /^nginx/ })));
  expect(writes(api)).toEqual([]);
});

test("a click beside a Service's panel closes it while nothing in it has changed, the focus back on its row, but not once a field is typed in", async () => {
  const api = fakeApi();
  actAs(api, alice);
  served(api, [web1], [nginx]);

  renderAt("/hosts/web1");
  await openPanel("nginx");
  await clickOutside();
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("link", { name: /^nginx/ })));

  const panel = await openPanel("nginx");
  type("Ports", "443/tcp");
  await clickOutside();
  expect(screen.getByRole("dialog", { name: "nginx" })).toBe(panel);
  expect(writes(api)).toEqual([]);
});

test("Save sends one service.set for what changed, with unset for what was emptied, then service.rename for the name", async () => {
  const api = fakeApi();
  actAs(api, alice);
  served(api, [web1], [nginx]);
  api.on(ServiceSet, () => ({ service: nginx }));
  api.on(ServiceRename, ({ new_name }) => ({ service: { ...nginx, name: new_name } }));

  renderAt("/hosts/web1/services/nginx");
  const panel = await screen.findByRole("dialog", { name: "nginx" });
  await within(panel).findByRole("textbox", { name: "Ports" });
  type("Name", "proxy");
  type("Ports", "443");
  type("Value of tier", "front");
  type("Note", "");
  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));

  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(api.requests(ServiceSet)).toEqual([
    { host: "host_web1", name: "nginx", ports: ["443"], attributes: { tier: "front" }, unset: ["note"] },
  ]);
  expect(api.requests(ServiceRename)).toEqual([{ host: "host_web1", name: "nginx", new_name: "proxy" }]);
  expect(writes(api)).toEqual(["/v1/service.set", "/v1/service.rename"]);
  expect(window.location.pathname).toBe("/hosts/web1");
  expect(await screen.findByText("proxy is saved.")).toBeDefined();
});

test("a Service saved with its rename refused says what was saved, shows the refusal in place, and sends only the rename again", async () => {
  const api = fakeApi();
  actAs(api, alice);
  served(api, [web1], [nginx, service("proxy")]);
  api.on(ServiceSet, () => ({ service: nginx }));
  api.on(ServiceRename, ({ new_name }) => {
    if (new_name === "proxy") {
      throw refusal(CodeNameTaken, 409);
    }
    return { service: { ...nginx, name: new_name } };
  });

  renderAt("/hosts/web1/services/nginx");
  const panel = await screen.findByRole("dialog", { name: "nginx" });
  await within(panel).findByRole("textbox", { name: "Ports" });
  type("Name", "proxy");
  type("Ports", "8443/tcp");
  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));

  await waitFor(() => expect(alerts()).toContain("This name is already in use."));
  expect(statuses()).toContain("The changes other than the name are saved; the name is still nginx.");
  expect(screen.getByRole("dialog", { name: "nginx" })).toBeDefined();
  await expectAccessible();

  type("Name", "edge");
  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(api.requests(ServiceSet)).toEqual([{ host: "host_web1", name: "nginx", ports: ["8443/tcp"] }]);
  expect(api.requests(ServiceRename)).toEqual([
    { host: "host_web1", name: "nginx", new_name: "proxy" },
    { host: "host_web1", name: "nginx", new_name: "edge" },
  ]);
});

test("Save clears the ports and an Attribute through unset, and with nothing changed closes, sending nothing", async () => {
  const api = fakeApi();
  actAs(api, alice);
  served(api, [web1], [nginx]);
  api.on(ServiceSet, () => ({ service: nginx }));

  renderAt("/hosts/web1/services/nginx");
  let panel = await screen.findByRole("dialog", { name: "nginx" });
  await within(panel).findByRole("textbox", { name: "Ports" });
  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(writes(api)).toEqual([]);
  expect(screen.queryByText("nginx is saved.")).toBeNull();

  fireEvent.click(await screen.findByRole("link", { name: /^nginx/ }));
  panel = await screen.findByRole("dialog", { name: "nginx" });
  await within(panel).findByRole("textbox", { name: "Ports" });
  type("Ports", "");
  fireEvent.click(within(panel).getByRole("button", { name: "Remove the Attribute tier" }));
  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(api.requests(ServiceSet)).toEqual([{ host: "host_web1", name: "nginx", unset: ["ports", "tier"] }]);
});

test("Save refuses a wrong port or name before any call", async () => {
  const api = fakeApi();
  actAs(api, alice);
  served(api, [web1], [nginx]);

  renderAt("/hosts/web1/services/nginx");
  const panel = await screen.findByRole("dialog", { name: "nginx" });
  await within(panel).findByRole("textbox", { name: "Ports" });
  type("Name", "Nginx");
  type("Ports", "443, 70000, 53/sctp");
  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));

  expect(
    await screen.findByText("Use 1 to 63 lowercase letters, digits and hyphens, starting and ending with a letter or a digit."),
  ).toBeDefined();
  expect(
    screen.getByText("Write each port as a number from 1 to 65535, followed by /tcp, /udp or nothing for TCP."),
  ).toBeDefined();
  await expectAccessible();
  expect(writes(api)).toEqual([]);
});

test("Delete asks first, then sends service.remove and goes back to the Host's page", async () => {
  const api = fakeApi();
  actAs(api, alice);
  served(api, [web1], [nginx]);
  api.on(ServiceRemove, () => ({ service: nginx }));

  renderAt("/hosts/web1/services/nginx");
  const panel = await screen.findByRole("dialog", { name: "nginx" });
  await within(panel).findByRole("textbox", { name: "Ports" });
  fireEvent.click(within(panel).getByRole("button", { name: "Delete" }));
  let confirm = await screen.findByRole("alertdialog", { name: "Delete nginx" });
  expect(confirm.textContent).toContain(
    "The record of nginx on web1 is deleted with its Attributes and its Note. Nothing on the Host itself changes.",
  );
  await expectAccessible();
  fireEvent.click(within(confirm).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  expect(writes(api)).toEqual([]);

  fireEvent.click(within(panel).getByRole("button", { name: "Delete" }));
  confirm = await screen.findByRole("alertdialog", { name: "Delete nginx" });
  const entries = window.history.length;
  fireEvent.click(within(confirm).getByRole("button", { name: "Delete" }));
  await waitFor(() => expect(window.location.pathname).toBe("/hosts/web1"));
  expect(window.history.length).toBe(entries);
  expect(api.requests(ServiceRemove)).toEqual([{ host: "host_web1", name: "nginx" }]);
  expect(await screen.findByText("nginx is deleted.")).toBeDefined();
});

test("Add Service sends one service.add by the Host's id, with only what is given, and opens the new Service's panel", async () => {
  const api = fakeApi();
  actAs(api, alice);
  const services: Service[] = [];
  served(api, [web1], services);
  api.on(ServiceAdd, ({ name, ports = [], attributes: given = {} }) => {
    const added = service(name, { ports, attributes: given });
    services.push(added);
    return { service: added };
  });

  renderAt("/hosts/web1");
  expect(await screen.findByText("No Service is recorded on this Host.")).toBeDefined();
  fireEvent.click(screen.getByRole("button", { name: "Add Service" }));
  const panel = await screen.findByRole("dialog", { name: "Add Service" });
  await waitFor(() => expect(document.activeElement).toBe(within(panel).getByRole("textbox", { name: "Name" })));
  await expectAccessible();
  type("Name", "redis");
  type("Ports", "6379, 6380/udp");
  fireEvent.click(within(panel).getByRole("button", { name: "Add Service" }));

  const opened = await screen.findByRole("dialog", { name: "redis" });
  expect(window.location.pathname).toBe("/hosts/web1/services/redis");
  expect(api.requests(ServiceAdd)).toEqual([{ host: "host_web1", name: "redis", ports: ["6379", "6380/udp"] }]);
  expect(await screen.findByText("redis is recorded.")).toBeDefined();
  expect(await within(opened).findByRole("textbox", { name: "Ports" })).toBeDefined();
  expect(within(screen.getByRole("region", { name: "Services" })).getByRole("link", { name: /^redis/ })).toBeDefined();

  fireEvent.keyDown(opened, { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Add Service" })));
});

test("a Service on a Retired Host, reached through the Host's id, shows read-only and offers no action", async () => {
  const api = fakeApi();
  actAs(api, alice);
  served(api, [old], [service("nginx", { host: "host_9", ports: ["443/tcp"], attributes: { tier: "edge" } })]);

  renderAt("/hosts/host_9/services/nginx");
  const panel = await screen.findByRole("dialog", { name: "nginx" });
  expect(await within(panel).findByText("Its Host is Retired, so this Service cannot be changed.")).toBeDefined();
  expect(api.requests(ServiceShow)).toEqual([{ host: "host_9", name: "nginx" }]);
  expect(panel.textContent).toContain("443/tcp");
  expect(panel.textContent).toContain("tieredge");
  expect(panel.textContent).toContain("No Note is recorded.");
  expect(within(panel).queryByRole("textbox")).toBeNull();
  expect(within(panel).queryByRole("button", { name: "Save" })).toBeNull();
  expect(within(panel).queryByRole("button", { name: "Delete" })).toBeNull();
  await expectAccessible();
});

test("a Service the Host has not shows the refusal in its panel", async () => {
  const api = fakeApi();
  actAs(api, alice);
  served(api, [web1], []);

  renderAt("/hosts/web1/services/gone");
  const panel = await screen.findByRole("dialog", { name: "gone" });
  await waitFor(() => expect(alerts()).toContain("The Host has no Service of that name."));
  expect(within(panel).getByRole("button", { name: "Try again" })).toBeDefined();
  await expectAccessible();
});

test("/services/<host>/<service>, where a released koko showed a Service, goes to its panel on the Host's page, keeping its query", async () => {
  const api = fakeApi();
  actAs(api, alice);
  served(api, [web1], [nginx]);

  const entries = window.history.length;
  renderAt("/services/web1/nginx?from=bookmark");
  expect(await screen.findByRole("dialog", { name: "nginx" })).toBeDefined();
  expect(window.location.pathname + window.location.search).toBe("/hosts/web1/services/nginx?from=bookmark");
  expect(window.history.length).toBe(entries);
});
