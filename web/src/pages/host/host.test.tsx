import { act, configure, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";

import { AttrList } from "../../api/attr";
import { CodeNameTaken } from "../../api/errors";
import {
  CodeHostWasManaged,
  CodeRelayInUse,
  CodeUnknownHost,
  type Host,
  HostList,
  HostMove,
  HostRemove,
  HostRename,
  HostRetire,
  HostSet,
  HostShow,
} from "../../api/host";
import { type Secret, SecretList, SecretShow } from "../../api/secret";
import type { Service } from "../../api/service";
import { SpaceList } from "../../api/space";
import { navigate } from "../../shell/router";
import { expectAccessible } from "../../test/axe";
import { clipboard } from "../../test/clipboard";
import { chooseRowAction, searchAndChoose } from "../../test/controls";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { setViewportWidth } from "../../test/media";
import { actAs, alerts, renderAt } from "../../test/pages";
import { alice, attributes, bob, host, pick, service, spaces, writes } from "./records.fixture";

// A Host's page, against the fake API: what host.show answers, how to
// reach the Host, its Attributes, Services, the Secrets of its Space and
// its Note; and each of its actions, naming the Host by its id.

// A test here renders the whole page and drives several overlays in turn;
// its limits are ceilings for a slow runner, not waits: each step waits for
// its own outcome.
vi.setConfig({ testTimeout: 20_000 });
configure({ asyncUtilTimeout: 5_000 });

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllEnvs();
});

const fingerprint = "SHA256:uNiVztksCsDhcc0u9e8BujQXVUpKZIDTMczCvj3tD2s";

const web1 = host("web1", {
  host_key: fingerprint,
  attributes: {
    provider: "hetzner",
    region: "fsn1",
    price: 4.5,
    currency: "EUR",
    billing_cycle: "monthly",
    expires_at: "2026-12-01",
    auto_renew: "yes",
    cores: "8",
  },
  note: "Ask ops before a reboot.",
  created_at: "2026-11-01T09:00:00Z",
});

const db1 = host("db1", {
  state: "registered",
  port: 2222,
  relay: { id: "host_web1", name: "web1" },
  attributes: { billing_cycle: "yearly", expires_at: "2026-12-01", provider: "hetzner", cores: "8" },
  note: "Primary database.",
});

function secret(name: string, space = "ops"): Secret {
  return { id: `secret_${name}`, space, name, created_at: "2026-11-04T09:00:00Z", updated_at: "2026-11-05T09:00:00Z" };
}

/** Recorded is what the fake koko server holds for a Host's page. */
interface Recorded {
  hosts: Host[];
  services?: Service[];
  secrets?: Secret[];
}

/**
 * served answers host.show for each Host, by its name or its id, with its
 * Services, and the reads the page and its forms make. A Host it does not
 * hold answers unknown_host.
 */
function served(api: FakeApi, { hosts, services = [], secrets = [] }: Recorded): void {
  api.on(HostShow, ({ host: named }) => {
    const shown = hosts.find((h) => h.id === named || (h.name === named && h.state !== "retired"));
    if (!shown) {
      throw refusal(CodeUnknownHost, 404);
    }
    const ref = shown.state === "retired" ? shown.id : shown.name;
    return { host: shown, services: services.filter((s) => s.host === ref) };
  });
  api.on(HostList, () => ({ hosts: hosts.filter((h) => h.state !== "retired") }));
  api.on(SpaceList, () => ({ spaces }));
  api.on(AttrList, () => ({ attributes }));
  api.on(SecretList, ({ space }) => ({ secrets: secrets.filter((s) => s.space === space) }));
}

/** openHost renders the page at path and waits for the Host named name, with its state mark after its name. */
async function openHost(path: string, name: string): Promise<void> {
  renderAt(path);
  await screen.findByRole("heading", { level: 1, name: new RegExp(`^${name}(Managed|Registered|Retired)$`) });
}

/** fact is the value shown under the label in the region named region. */
function fact(region: string, label: string): string {
  const term = within(screen.getByRole("region", { name: region })).getByText(label, { selector: "dt" });
  return term.nextElementSibling?.textContent ?? "";
}

/** menuItem is the item named name of the page's More actions menu, which must be open. */
function menuItem(name: string): HTMLElement {
  return within(screen.getByRole("menu")).getByRole("menuitem", { name });
}

/** openMore opens the More actions menu of the Host named name. */
async function openMore(name: string): Promise<void> {
  fireEvent.click(screen.getByRole("button", { name: `More actions for ${name}` }));
  await screen.findByRole("menu");
}

/** type types value into the text field named name. */
function type(name: string, value: string): void {
  fireEvent.change(screen.getByRole("textbox", { name }), { target: { value } });
}

test("a Managed Host's page: the breadcrumb, how to reach it, its pin, Attributes, Services, Note, the Secrets of its Space, and when it was recorded", async () => {
  vi.useFakeTimers({ toFake: ["Date"], shouldAdvanceTime: true });
  vi.setSystemTime(new Date("2026-11-08T09:00:00Z"));
  const api = fakeApi();
  actAs(api, alice);
  const writeText = clipboard();
  served(api, {
    hosts: [web1, db1],
    services: [service("nginx", { ports: ["80/tcp", "443/tcp"] }), service("cron")],
    secrets: ["ops-mail", "db-password", "web1-tls", "backup", "deploy-key", "web1-root"].map((n) => secret(n)),
  });

  await openHost("/hosts/web1", "web1");
  await waitFor(() => expect(document.title).toBe("web1 · koko"));
  expect(api.requests(HostShow)).toEqual([{ host: "web1" }]);
  const breadcrumb = screen.getByRole("navigation", { name: "Breadcrumb" });
  expect(within(breadcrumb).getByRole("link", { name: "Hosts" }).getAttribute("href")).toBe("/hosts");
  expect(within(breadcrumb).getByRole("link", { name: "ops" }).getAttribute("href")).toBe("/hosts?filter=space%3Dops");
  expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("web1Managed");

  const reach = screen.getByRole("region", { name: "Reaching it" });
  expect(within(reach).getByRole("group", { name: "SSH command" }).textContent).toContain("ssh web1");
  expect(reach.textContent).toContain("koko logs in as deploy");
  fireEvent.click(within(reach).getByRole("button", { name: "Copy" }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith("ssh web1"));
  expect(fact("Reaching it", "Address")).toBe("web1.example.com");
  expect(fact("Reaching it", "Port")).toBe("22");
  expect(fact("Reaching it", "Host Account")).toBe("deploy");
  expect(within(reach).queryByText("Relay")).toBeNull();
  expect(fact("Reaching it", "Pinned host key")).toBe(fingerprint);

  // The purchase first, the price with its currency, then every other key; values as stored.
  const attrs = screen.getByRole("region", { name: "Attributes" });
  expect([...attrs.querySelectorAll("dt")].map((d) => d.textContent)).toEqual([
    "Provider",
    "Region",
    "Price",
    "Billing cycle",
    "Expires",
    "Renews automatically",
    "cores",
  ]);
  expect(fact("Attributes", "Price")).toBe("4.5 EUR");
  expect(fact("Attributes", "Billing cycle")).toBe("monthly");
  expect(fact("Attributes", "Expires")).toBe("2026-12-01");

  const services = within(screen.getByRole("region", { name: "Services" }));
  expect(services.getByRole("link", { name: /nginx/ }).getAttribute("href")).toBe("/hosts/web1/services/nginx");
  expect(services.getByRole("link", { name: /nginx/ }).textContent).toBe("nginx80/tcp  443/tcp");
  expect(services.getByRole("link", { name: /cron/ }).textContent).toBe("cronListens on no port");
  expect(services.getByRole("button", { name: "Add Service" })).toBeDefined();

  // Four of the Space's Secrets, those that name the Host first, and All 6.
  const secrets = await screen.findByRole("region", { name: "Secrets in ops" });
  expect(within(secrets).getAllByRole("listitem").map((li) => li.querySelector("p")?.textContent)).toEqual([
    "web1-tls",
    "web1-root",
    "ops-mail",
    "db-password",
  ]);
  expect(within(secrets).getByRole("link", { name: "All 6" }).getAttribute("href")).toBe("/secrets?space=ops");
  expect(api.requests(SecretList)).toEqual([{ space: "ops" }]);

  expect(within(screen.getByRole("region", { name: "Note" })).getByText("Ask ops before a reboot.")).toBeDefined();
  expect(screen.getByText(/^Recorded 2026-11-01, changed 7 days ago/).textContent).toContain("host_web1");
  await expectAccessible();
});

test("a Secret on a Host's page is copied and shown as on the Secrets page", async () => {
  const api = fakeApi();
  actAs(api, alice);
  const writeText = clipboard();
  served(api, { hosts: [web1], secrets: [secret("web1-tls")] });
  api.on(SecretShow, () => ({ secret: secret("web1-tls"), value_base64: "dGxzLWtleQ==" }));

  await openHost("/hosts/web1", "web1");
  const secrets = await screen.findByRole("region", { name: "Secrets in ops" });
  expect(within(secrets).queryByRole("link", { name: /^All/ })).toBeNull();
  fireEvent.click(within(secrets).getByRole("button", { name: "Copy the value of ops/web1-tls" }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith("tls-key"));
  fireEvent.click(within(secrets).getByRole("button", { name: "Show the value of ops/web1-tls" }));
  expect((await within(secrets).findByRole("group", { name: "Value of ops/web1-tls" })).textContent).toContain("tls-key");
  expect(within(secrets).getByRole("button", { name: "More actions for ops/web1-tls" })).toBeDefined();
  await expectAccessible();
});

test("a Registered Host's page copies koko host enroll, names a Relay you cannot see, says it is not pinned, and offers Delete", async () => {
  const api = fakeApi();
  actAs(api, bob);
  const writeText = clipboard();
  const unseen = host("db2", { state: "registered", relay: { id: null, name: null } });
  served(api, { hosts: [unseen] });

  await openHost("/hosts/db2", "db2");
  const reach = screen.getByRole("region", { name: "Reaching it" });
  expect(reach.textContent).toContain("koko cannot log in to this Host yet.");
  fireEvent.click(within(within(reach).getByRole("group", { name: "Enroll command" })).getByRole("button", { name: "Copy" }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith("koko host enroll db2"));
  expect(fact("Reaching it", "Relay")).toBe("A Relay you cannot see");
  expect(fact("Reaching it", "Pinned host key")).toBe("Not pinned");
  expect(screen.getByRole("region", { name: "Attributes" }).textContent).toContain("No Attribute is recorded.");
  expect(await screen.findByText("ops holds no Secret.")).toBeDefined();

  await openMore("db2");
  expect(menuItem("Delete")).toBeDefined();
  await expectAccessible();
});

test("a Host's Relay is a link to the Relay's page by its id, and a pinned Host offers no Delete", async () => {
  const api = fakeApi();
  actAs(api, alice);
  served(api, { hosts: [web1, { ...db1, host_key: fingerprint }] });

  await openHost("/hosts/db1", "db1");
  expect(within(screen.getByRole("region", { name: "Reaching it" })).getByRole("link", { name: "web1" }).getAttribute("href")).toBe(
    "/hosts/host_web1",
  );
  await openMore("db1");
  expect(within(screen.getByRole("menu")).getAllByRole("menuitem").map((i) => i.textContent)).toEqual([
    "Rename",
    "Move to another Space",
    "Retire",
  ]);
});

test("a Host no one can see shows as one that does not exist", async () => {
  const api = fakeApi();
  actAs(api, bob);
  served(api, { hosts: [] });

  renderAt("/hosts/web9");
  expect(await screen.findByRole("heading", { level: 1, name: "No such Host" })).toBeDefined();
  expect(screen.getByText("No Host you can see has the name or id web9.")).toBeDefined();
  expect(screen.getByRole("link", { name: "Go to Hosts" }).getAttribute("href")).toBe("/hosts");
  await expectAccessible();
});

/** openEdit opens Edit on the page and waits for its form's fields. */
async function openEdit(name: string): Promise<HTMLElement> {
  fireEvent.click(screen.getByRole("button", { name: "Edit" }));
  const panel = await screen.findByRole("dialog", { name: `Edit ${name}` });
  await within(panel).findByRole("textbox", { name: "Address" });
  return panel;
}

test("Edit sends one host.set with only what changed, a relative date resolved, and the port, the Relay and an enum cleared through unset", async () => {
  vi.useFakeTimers({ toFake: ["Date"], shouldAdvanceTime: true });
  vi.setSystemTime(new Date("2026-11-06T20:00:00Z")); // already 2026-11-07 in Shanghai
  vi.stubEnv("TZ", "Asia/Shanghai");
  const api = fakeApi();
  actAs(api, alice);
  served(api, { hosts: [web1, db1] });
  api.on(HostSet, ({ host: id }) => ({ host: id === "host_db1" ? db1 : web1 }));

  await openHost("/hosts/db1", "db1");
  const panel = await openEdit("db1");
  expect(within(panel).queryByRole("textbox", { name: "Name" })).toBeNull();
  expect(within(panel).queryByRole("combobox", { name: "Space" })).toBeNull();
  expect((within(panel).getByRole("textbox", { name: "Port" }) as HTMLInputElement).value).toBe("2222");
  expect((within(panel).getByRole("combobox", { name: "Relay" }) as HTMLInputElement).value).toBe("web1");
  expect((within(panel).getByRole("textbox", { name: "Value of cores" }) as HTMLInputElement).value).toBe("8");
  await expectAccessible();

  type("Port", "");
  await searchAndChoose(within(panel).getByRole("combobox", { name: "Relay" }), "none", "None, reached directly");
  await pick("Billing cycle", "None");
  type("Expires", "+1y");
  expect(screen.getByText("Today this is 2027-11-07.")).toBeDefined();
  type("Value of cores", "16");
  type("Note", "Primary database, moved to fsn1.");
  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));

  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit db1" })).toBeNull());
  expect(api.requests(HostSet)).toEqual([
    {
      host: "host_db1",
      attributes: { expires_at: "2027-11-07", cores: "16" },
      note: "Primary database, moved to fsn1.",
      unset: ["port", "relay", "billing_cycle"],
    },
  ]);
  expect(await screen.findByText("db1 is saved.")).toBeDefined();
  expect(api.requests(HostShow)).toHaveLength(2);
});

test("Edit refuses a wrong value before the call, sends nothing when nothing changed, and keeps the form as typed on a refusal", async () => {
  const api = fakeApi();
  actAs(api, alice);
  served(api, { hosts: [web1] });
  api.on(HostSet, () => {
    throw refusal("invalid_relay");
  });

  await openHost("/hosts/web1", "web1");
  let panel = await openEdit("web1");
  type("Price", "-1");
  type("Port", "70000");
  type("Address", "");
  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));
  expect(await screen.findByText("This is not a value price takes.")).toBeDefined();
  expect(screen.getByText("A port is a number from 1 to 65535.")).toBeDefined();
  expect(screen.getByText("This field is required.")).toBeDefined();
  await expectAccessible();
  expect(writes(api)).toEqual([]);

  fireEvent.click(within(panel).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  panel = await openEdit("web1");
  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(writes(api)).toEqual([]);

  panel = await openEdit("web1");
  type("Address", "192.0.2.10");
  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));
  await waitFor(() =>
    expect(alerts()).toContain(
      "That Host cannot be a Relay here: a Relay is a Managed Host other than this one, and is exactly one hop.",
    ),
  );
  expect(api.requests(HostSet)).toEqual([{ host: "host_web1", address: "192.0.2.10" }]);
  expect((screen.getByRole("textbox", { name: "Address" }) as HTMLInputElement).value).toBe("192.0.2.10");
});

test("Rename sends host.rename by the Host's id, and the page moves to the new name in place of its history entry", async () => {
  const api = fakeApi();
  actAs(api, alice);
  served(api, { hosts: [web1] });
  api.on(HostRename, ({ new_name }) => {
    if (new_name === "db1") {
      throw refusal(CodeNameTaken, 409);
    }
    return { host: { ...web1, name: new_name } };
  });

  await openHost("/hosts/web1", "web1");
  const entries = window.history.length;
  await chooseRowAction("More actions for web1", "Rename");
  const dialog = await screen.findByRole("dialog", { name: "Rename web1" });
  await expectAccessible();
  type("New name", "Web 2");
  fireEvent.click(within(dialog).getByRole("button", { name: "Rename" }));
  expect(
    await screen.findByText("Use 1 to 63 lowercase letters, digits and hyphens, starting and ending with a letter or a digit."),
  ).toBeDefined();
  type("New name", "db1");
  fireEvent.click(within(dialog).getByRole("button", { name: "Rename" }));
  await waitFor(() => expect(alerts()).toContain("This name is already in use."));

  type("New name", "web2");
  fireEvent.click(within(dialog).getByRole("button", { name: "Rename" }));
  await waitFor(() => expect(window.location.pathname).toBe("/hosts/web2"));
  expect(window.history.length).toBe(entries);
  expect(api.requests(HostRename)).toEqual([
    { host: "host_web1", new_name: "db1" },
    { host: "host_web1", new_name: "web2" },
  ]);
  expect(await screen.findByText("Renamed to web2.")).toBeDefined();
});

test("Move picks the Space in a searchable choice, sends host.move by the Host's id, and the page loads again", async () => {
  const api = fakeApi();
  actAs(api, alice);
  served(api, { hosts: [web1] });
  api.on(HostMove, ({ space }) => ({ host: { ...web1, space } }));

  await openHost("/hosts/web1", "web1");
  await chooseRowAction("More actions for web1", "Move to another Space");
  const dialog = await screen.findByRole("dialog", { name: "Move web1" });
  fireEvent.click(within(dialog).getByRole("button", { name: "Move" }));
  expect(await screen.findByText("Choose the Space to move it to.")).toBeDefined();
  await searchAndChoose("Move to", "e", "dev", ["alicePersonal Space", "dev"]);
  fireEvent.click(within(dialog).getByRole("button", { name: "Move" }));

  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(api.requests(HostMove)).toEqual([{ host: "host_web1", space: "dev" }]);
  expect(await screen.findByText("web1 is now in dev.")).toBeDefined();
  expect(api.requests(HostShow)).toHaveLength(2);
});

test("Retire asks first, saying what follows, shows relay_in_use in place, then moves to the Host's id", async () => {
  const api = fakeApi();
  actAs(api, alice);
  served(api, { hosts: [web1] });
  let inUse = true;
  api.on(HostRetire, () => {
    if (inUse) {
      throw refusal(CodeRelayInUse, 409);
    }
    return { host: { ...web1, state: "retired" } };
  });

  await openHost("/hosts/web1", "web1");
  const menu = screen.getByRole("button", { name: "More actions for web1" });
  await chooseRowAction("More actions for web1", "Retire");
  let dialog = await screen.findByRole("alertdialog", { name: "Retire web1" });
  expect(dialog.textContent).toContain("under its id, host_web1. Retiring cannot be undone.");
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(document.activeElement).toBe(menu));
  expect(writes(api)).toEqual([]);

  await chooseRowAction("More actions for web1", "Retire");
  dialog = await screen.findByRole("alertdialog", { name: "Retire web1" });
  fireEvent.click(within(dialog).getByRole("button", { name: "Retire" }));
  await waitFor(() =>
    expect(alerts()).toContain("The Host is the Relay of a Host that is not Retired, so it cannot be Retired yet."),
  );

  inUse = false;
  const entries = window.history.length;
  fireEvent.click(within(dialog).getByRole("button", { name: "Retire" }));
  await waitFor(() => expect(window.location.pathname).toBe("/hosts/host_web1"));
  expect(window.history.length).toBe(entries);
  expect(api.requests(HostRetire)).toEqual([{ host: "host_web1" }, { host: "host_web1" }]);
  expect(await screen.findByText("web1 is Retired.")).toBeDefined();
});

test("Delete, offered with no pin, asks first with the Services it takes, shows host_was_managed in place, then goes back to the Hosts", async () => {
  const api = fakeApi();
  actAs(api, alice);
  served(api, { hosts: [db1], services: [service("pg", { host: "db1" }), service("exporter", { host: "db1" })] });
  let managed = true;
  api.on(HostRemove, () => {
    if (managed) {
      throw refusal(CodeHostWasManaged, 409);
    }
    return { host: db1 };
  });

  await openHost("/hosts/db1", "db1");
  const menu = screen.getByRole("button", { name: "More actions for db1" });
  await chooseRowAction("More actions for db1", "Delete");
  fireEvent.click(within(await screen.findByRole("alertdialog", { name: "Delete db1" })).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(document.activeElement).toBe(menu));
  expect(writes(api)).toEqual([]);

  await chooseRowAction("More actions for db1", "Delete");
  const dialog = await screen.findByRole("alertdialog", { name: "Delete db1" });
  expect(dialog.textContent).toContain(
    "db1 is deleted permanently, with its Attributes, its Note and its 2 Services. This cannot be undone.",
  );
  fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));
  await waitFor(() =>
    expect(alerts()).toContain("The Host has been Managed, so koko keeps its record and never deletes it."),
  );
  await expectAccessible();

  managed = false;
  const entries = window.history.length;
  fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));
  await waitFor(() => expect(window.location.pathname).toBe("/hosts"));
  expect(window.history.length).toBe(entries);
  expect(api.requests(HostRemove)).toEqual([{ host: "host_db1" }, { host: "host_db1" }]);
});

test("a Retired Host's page: Rename and Retire disabled, no Add Service, Edit of its Attributes and Note alone, Move, and Delete with no pin", async () => {
  const api = fakeApi();
  actAs(api, alice);
  const old = host("old", { id: "host_9", state: "retired", attributes: { provider: "hetzner" } });
  served(api, { hosts: [old], services: [service("nginx", { host: "host_9" })] });
  api.on(HostSet, () => ({ host: old }));
  api.on(HostMove, ({ space }) => ({ host: { ...old, space } }));
  api.on(HostRemove, () => ({ host: old }));

  await openHost("/hosts/host_9", "old");
  expect(screen.getByRole("region", { name: "Reaching it" }).textContent).toContain("This Host is Retired.");
  const services = screen.getByRole("region", { name: "Services" });
  expect(within(services).queryByRole("button", { name: "Add Service" })).toBeNull();
  expect(within(services).getByRole("link", { name: /nginx/ }).getAttribute("href")).toBe("/hosts/host_9/services/nginx");

  await openMore("old");
  expect(menuItem("Rename").getAttribute("aria-disabled")).toBe("true");
  expect(menuItem("Retire").getAttribute("aria-disabled")).toBe("true");
  expect(menuItem("Move to another Space").getAttribute("aria-disabled")).toBeNull();
  expect(menuItem("Delete").getAttribute("aria-disabled")).toBeNull();
  await expectAccessible();
  fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());

  const panel = await openEdit("old");
  expect(panel.textContent).toContain("A Retired Host takes changes to its Attributes and its Note only.");
  for (const name of ["Address", "Host Account", "Port"]) {
    expect((within(panel).getByRole("textbox", { name }) as HTMLInputElement).disabled).toBe(true);
  }
  type("Region", "fsn1");
  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(api.requests(HostSet)).toEqual([{ host: "host_9", attributes: { region: "fsn1" } }]));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

  await chooseRowAction("More actions for old", "Move to another Space");
  await searchAndChoose("Move to", "de", "dev");
  fireEvent.click(within(screen.getByRole("dialog", { name: "Move old" })).getByRole("button", { name: "Move" }));
  await waitFor(() => expect(api.requests(HostMove)).toEqual([{ host: "host_9", space: "dev" }]));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

  await chooseRowAction("More actions for old", "Delete");
  fireEvent.click(within(await screen.findByRole("alertdialog", { name: "Delete old" })).getByRole("button", { name: "Delete" }));
  await waitFor(() => expect(window.location.pathname).toBe("/hosts"));
  expect(api.requests(HostRemove)).toEqual([{ host: "host_9" }]);
});

test("Edit switching Renews automatically off sends no, and on again yes", async () => {
  const api = fakeApi();
  actAs(api, alice);
  served(api, { hosts: [web1, db1] });
  api.on(HostSet, ({ host: id }) => ({ host: id === "host_db1" ? db1 : web1 }));

  await openHost("/hosts/web1", "web1");
  let panel = await openEdit("web1");
  fireEvent.click(within(panel).getByRole("switch", { name: "Renews automatically" }));
  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

  act(() => navigate("/hosts/db1"));
  await screen.findByRole("heading", { level: 1, name: "db1Registered" });
  panel = await openEdit("db1");
  fireEvent.click(within(panel).getByRole("switch", { name: "Renews automatically" }));
  fireEvent.click(within(panel).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(api.requests(HostSet)).toHaveLength(2));
  expect(api.requests(HostSet)).toEqual([
    { host: "host_web1", attributes: { auto_renew: "no" } },
    { host: "host_db1", attributes: { auto_renew: "yes" } },
  ]);
});

test("a Host's page that does not load says why, with Try again, and a Space's Secrets that do not load say so in their panel", async () => {
  const api = fakeApi();
  actAs(api, alice);
  served(api, { hosts: [web1] });
  api.on(SecretList, () => {
    throw refusal("internal", 500);
  });
  api.drop(1);

  renderAt("/hosts/web1");
  fireEvent.click(await screen.findByRole("button", { name: "Try again" }));
  await openHost("/hosts/web1", "web1");
  const secrets = screen.getByRole("region", { name: "Secrets in ops" });
  expect(await within(secrets).findByRole("button", { name: "Try again" })).toBeDefined();
  await expectAccessible();
});

test("on a phone the page stacks its panels, and Edit comes up from the bottom", async () => {
  setViewportWidth(390);
  const api = fakeApi();
  actAs(api, alice);
  served(api, { hosts: [web1], services: [service("nginx")] });

  await openHost("/hosts/web1", "web1");
  await screen.findByRole("region", { name: "Secrets in ops" });
  await expectAccessible();
  const panel = await openEdit("web1");
  expect(panel.closest("[data-placement]")?.getAttribute("data-placement")).toBe("bottom");
  await expectAccessible();
});

test("Escape closes Edit, sending nothing, and gives the focus back to Edit", async () => {
  const api = fakeApi();
  actAs(api, alice);
  served(api, { hosts: [web1] });

  await openHost("/hosts/web1", "web1");
  const edit = screen.getByRole("button", { name: "Edit" });
  act(() => edit.focus());
  const panel = await openEdit("web1");
  fireEvent.keyDown(panel, { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  await waitFor(() => expect(document.activeElement).toBe(edit));
  expect(writes(api)).toEqual([]);
});
