import { configure, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { AttrList } from "../../api/attr";
import { HostList, HostMove, HostRemove, HostRetire, HostSet } from "../../api/host";
import { expectAccessible } from "../../test/axe";
import { searchAndChoose } from "../../test/controls";
import { batchRefusal, type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { setViewportWidth } from "../../test/media";
import { actAs, alerts, renderAt } from "../../test/pages";
import { alice, attributes, host, pick, records, visibleText, writes } from "./records.fixture";

// The Hosts list's Select mode against the fake API
// (docs/subsystems/record-pages.md, "The Select mode"): the mode entered and
// left, rows, ranges and groups ticked, the selection kept while the view
// changes, each action's one dialog and its one batch call, what a dialog
// leaves out, and a batch the koko server refused.

vi.setConfig({ testTimeout: 20_000 });
configure({ asyncUtilTimeout: 5_000 });

const build1 = host("build1", { space: "dev", attributes: { role: "node" } });
const db1 = host("db1", { state: "registered", attributes: { role: "db" } });
const web1 = host("web1", { host_key: "SHA256:web1", attributes: { role: "node" } });
const web2 = host("web2", { state: "registered" });
const web3 = host("web3", { state: "registered" });
const old1 = host("old1", { id: "host_old1", state: "retired", host_key: "SHA256:old1" });
const fleet = [build1, db1, web1, web2, web3, old1];

/** selecting renders the Hosts page at path over the fleet, with the Select mode on. */
async function selecting(path = "/hosts"): Promise<FakeApi> {
  const api = fakeApi();
  actAs(api, alice);
  records(api, { hosts: fleet });
  api.on(AttrList, () => ({ attributes: [...attributes, { key: "role", type: "text", preset: false, in_use: true }] }));
  renderAt(path);
  await screen.findByRole("link", { name: "build1" });
  fireEvent.click(screen.getByRole("button", { name: "Select" }));
  await screen.findByRole("region", { name: "Actions on the selected Hosts" });
  return api;
}

/** bar is the Select mode's bar, or null when the mode is off. */
function bar(): HTMLElement | null {
  return screen.queryByRole("region", { name: "Actions on the selected Hosts" });
}

/** summary is what the bar says of the selection: how many, and how many the view does not show. */
function summary(): string {
  return within(bar() as HTMLElement).getByRole("status").textContent ?? "";
}

/** act presses the bar's action named name. */
function act(name: string): void {
  fireEvent.click(within(bar() as HTMLElement).getByRole("button", { name }));
}

/** tick clicks the checkbox named name, with Shift held when shift. */
function tick(name: string, shift = false): void {
  fireEvent.click(screen.getByRole("checkbox", { name }), { shiftKey: shift });
}

/** clickRow clicks the row of the Host named name, beside its checkbox. */
function clickRow(name: string, shift = false): void {
  fireEvent.click(screen.getByRole("rowheader", { name: new RegExp(`^${name}`) }), { shiftKey: shift });
}

/** ticked are the names of the checkboxes ticked, in the page's order. */
function ticked(): string[] {
  return screen
    .getAllByRole<HTMLInputElement>("checkbox")
    .filter((box) => box.checked)
    .map((box) => box.getAttribute("aria-label") ?? "");
}

/** listed are the records a dialog's list named name holds. */
function listed(dialog: HTMLElement, name: string | RegExp): string[] {
  return within(within(dialog).getByRole("list", { name }))
    .getAllByRole("listitem")
    .map(visibleText);
}

/** listReads counts the host.list calls made so far. */
function listReads(api: FakeApi): number {
  return api.requests(HostList).length;
}

test("Select starts the mode: rows are no links and toggle, the bar's actions wait for a tick, and Done leaves it with nothing selected", async () => {
  const api = await selecting();

  expect(screen.queryByRole("link", { name: "web1" })).toBeNull();
  expect(summary()).toBe("0 selected");
  for (const name of ["Move", "Attribute", "Retire", "Delete"]) {
    expect(within(bar() as HTMLElement).getByRole<HTMLButtonElement>("button", { name }).disabled).toBe(true);
  }
  await expectAccessible();

  clickRow("web1");
  expect(summary()).toBe("1 selected");
  expect(ticked()).toEqual(["Select web1"]);
  expect(window.location.pathname).toBe("/hosts");
  expect(within(bar() as HTMLElement).getByRole<HTMLButtonElement>("button", { name: "Move" }).disabled).toBe(false);
  clickRow("web1");
  expect(summary()).toBe("0 selected");
  tick("Select db1");
  expect(ticked()).toEqual(["Select db1"]);
  await expectAccessible();

  fireEvent.click(within(bar() as HTMLElement).getByRole("button", { name: "Done" }));
  await waitFor(() => expect(bar()).toBeNull());
  expect(screen.getByRole("link", { name: "web1" }).getAttribute("href")).toBe("/hosts/web1");
  expect(screen.queryAllByRole("checkbox")).toEqual([]);
  fireEvent.click(screen.getByRole("button", { name: "Select" }));
  await waitFor(() => expect(summary()).toBe("0 selected"));
  // The Select button itself says Done while the mode is on, and leaves it too.
  fireEvent.click(screen.getAllByRole("button", { name: "Done" })[0] as HTMLElement);
  await waitFor(() => expect(bar()).toBeNull());
  expect(writes(api)).toEqual([]);
});

test("a Shift-click ticks the range from the row ticked last, and a table's header ticks its Space's rows", async () => {
  await selecting();

  clickRow("db1");
  clickRow("web2", true);
  expect(ticked()).toEqual(["Select db1", "Select web1", "Select web2"]);
  // A Shift-click on a checkbox does the same, and unticks a range from one unticked.
  tick("Select web3", true);
  expect(summary()).toBe("4 selected");
  expect(ticked()).toContain("Select the 4 Hosts of ops");
  tick("Select web1");
  tick("Select web3", true);
  expect(ticked()).toEqual(["Select db1"]);
  expect(screen.getByRole<HTMLInputElement>("checkbox", { name: "Select the 4 Hosts of ops" }).indeterminate).toBe(true);

  tick("Select the 4 Hosts of ops");
  expect(summary()).toBe("4 selected");
  tick("Select the 1 Host of dev");
  expect(summary()).toBe("5 selected");
  tick("Select the 4 Hosts of ops");
  expect(ticked()).toEqual(["Select the 1 Host of dev", "Select build1"]);
});

test("with Group by, a Space's heading and each subheading's table tick their rows", async () => {
  await selecting("/hosts?group=role");

  await screen.findByRole("table", { name: "ops, node" });
  tick("Select the 4 Hosts of ops");
  expect(summary()).toBe("4 selected");
  expect(ticked()).toEqual([
    "Select the 4 Hosts of ops",
    "Select the 1 Host of ops, db",
    "Select db1",
    "Select the 1 Host of ops, node",
    "Select web1",
    "Select the 2 Hosts of ops, No role",
    "Select web2",
    "Select web3",
  ]);
  tick("Select the 2 Hosts of ops, No role");
  expect(summary()).toBe("2 selected");
  expect(screen.getByRole<HTMLInputElement>("checkbox", { name: "Select the 4 Hosts of ops" }).indeterminate).toBe(true);
  // A range follows the order the page shows: db1, web1, then those without a role.
  clickRow("web2", true);
  expect(ticked()).toContain("Select web1");
  expect(ticked()).not.toContain("Select web3");
  await expectAccessible();
});

test("Select all ticks what the view shows, and the selection is kept across a search and a Filter, the bar counting those not shown", async () => {
  await selecting();

  expect(within(bar() as HTMLElement).getByRole("button", { name: "Select all 5" })).toBeDefined();
  clickRow("db1");
  fireEvent.change(screen.getByRole("searchbox", { name: "Search Hosts" }), { target: { value: "web" } });
  await waitFor(() => expect(summary()).toBe("1 selected1 not in this view"));

  fireEvent.click(within(bar() as HTMLElement).getByRole("button", { name: "Select all 3 matching" }));
  expect(summary()).toBe("4 selected1 not in this view");
  expect(within(bar() as HTMLElement).queryByRole("button", { name: /^Select all/ })).toBeNull();

  await pick("State", "Managed");
  await waitFor(() => expect(summary()).toBe("4 selected3 not in this view"));
  expect(ticked()).toEqual(["Select the 1 Host of ops", "Select web1"]);

  fireEvent.change(screen.getByRole("searchbox", { name: "Search Hosts" }), { target: { value: "" } });
  await pick("State", "In use");
  await waitFor(() => expect(summary()).toBe("4 selected"));
  expect(ticked()).toEqual(["Select the 4 Hosts of ops", "Select db1", "Select web1", "Select web2", "Select web3"]);
  fireEvent.click(within(bar() as HTMLElement).getByRole("button", { name: "Select all 5" }));
  expect(summary()).toBe("5 selected");

  // A selection gathered over several views goes to the dialog whole.
  fireEvent.change(screen.getByRole("searchbox", { name: "Search Hosts" }), { target: { value: "no such host" } });
  await waitFor(() => expect(summary()).toBe("5 selected5 not in this view"));
  act("Move");
  const dialog = await screen.findByRole("dialog", { name: "Move 5 Hosts" });
  expect(listed(dialog, /^Hosts/)).toEqual(["build1 · dev", "db1 · ops", "web1 · ops", "web2 · ops", "web3 · ops"]);
});

test("Move asks once for the Space, leaves out the Hosts in it already, and moves the rest in one call, which ends the mode", async () => {
  const api = await selecting();
  api.on(HostMove, ({ hosts = [], space }) => ({ hosts: fleet.filter((h) => hosts.includes(h.id)).map((h) => ({ ...h, space })) }));
  clickRow("build1");
  clickRow("db1");
  clickRow("web1");

  act("Move");
  const dialog = await screen.findByRole("dialog", { name: "Move 3 Hosts" });
  expect(dialog.textContent).toContain("Each Host moves with its Attributes, Note and Services, under the same name.");
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Move 3 Hosts" }));
  expect(await screen.findByText("Choose the Space to move it to.")).toBeDefined();
  expect(writes(api)).toEqual([]);

  await searchAndChoose("Move to", "", "dev", ["alicePersonal Space", "ops", "dev"]);
  await screen.findByRole("dialog", { name: "Move 2 Hosts" });
  expect(listed(dialog, /^Hosts/)).toEqual(["db1 · ops", "web1 · ops"]);
  expect(dialog.textContent).toContain("1 Host is in dev already and stays there.");
  await expectAccessible();
  const reads = listReads(api);
  fireEvent.click(within(dialog).getByRole("button", { name: "Move 2 Hosts" }));

  expect(await screen.findByText("2 Hosts are now in dev.")).toBeDefined();
  expect(api.requests(HostMove)).toEqual([{ hosts: ["host_db1", "host_web1"], space: "dev" }]);
  await waitFor(() => expect(bar()).toBeNull());
  expect(screen.queryByRole("dialog")).toBeNull();
  await waitFor(() => expect(listReads(api)).toBeGreaterThan(reads));
  fireEvent.click(screen.getByRole("button", { name: "Select" }));
  await waitFor(() => expect(summary()).toBe("0 selected"));
});

test("Attribute sets one key on every Host selected, saying whose value is replaced, in one call", async () => {
  const api = await selecting();
  api.on(HostSet, ({ hosts = [] }) => ({ hosts: fleet.filter((h) => hosts.includes(h.id)) }));
  clickRow("db1");
  clickRow("web2");

  act("Attribute");
  const dialog = await screen.findByRole("dialog", { name: "Set or remove an Attribute of 2 Hosts" });
  fireEvent.click(within(dialog).getByRole("button", { name: "Set on 2 Hosts" }));
  expect((await screen.findAllByText("This field is required.")).length).toBe(2);
  expect(writes(api)).toEqual([]);

  await searchAndChoose("Attribute key", "ro", "role");
  await screen.findByRole("dialog", { name: "Set role on 2 Hosts" });
  expect(dialog.textContent).toContain("1 of them has role now; its value is replaced.");
  fireEvent.change(within(dialog).getByRole("textbox", { name: "Value of role" }), { target: { value: "node" } });
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Set on 2 Hosts" }));

  expect(await screen.findByText("role is set on 2 Hosts.")).toBeDefined();
  expect(api.requests(HostSet)).toEqual([{ attributes: { role: "node" }, hosts: ["host_db1", "host_web2"] }]);
  await waitFor(() => expect(bar()).toBeNull());
});

test("Attribute removes a key from the Hosts that have it, leaving out those without it, and refuses a value its key does not take", async () => {
  const api = await selecting();
  api.on(HostSet, ({ hosts = [] }) => ({ hosts: fleet.filter((h) => hosts.includes(h.id)) }));
  clickRow("db1");
  clickRow("web1");
  clickRow("web2");

  act("Attribute");
  const dialog = await screen.findByRole("dialog", { name: "Set or remove an Attribute of 3 Hosts" });
  await searchAndChoose("Attribute key", "pri", "price");
  fireEvent.change(within(dialog).getByRole("textbox", { name: "Value of price" }), { target: { value: "cheap" } });
  fireEvent.click(within(dialog).getByRole("button", { name: "Set on 3 Hosts" }));
  expect(await screen.findByText("This is not a value price takes.")).toBeDefined();
  expect(writes(api)).toEqual([]);

  fireEvent.click(within(dialog).getByRole("radio", { name: "Remove an Attribute" }));
  await searchAndChoose("Attribute key", "", "role", ["role"]);
  await screen.findByRole("dialog", { name: "Remove role from 2 Hosts" });
  expect(listed(dialog, /^Hosts/)).toEqual(["db1 · ops", "web1 · ops"]);
  expect(dialog.textContent).toContain("1 Host has no role and is left out.");
  fireEvent.click(within(dialog).getByRole("button", { name: "Remove from 2 Hosts" }));

  expect(await screen.findByText("role is removed from 2 Hosts.")).toBeDefined();
  expect(api.requests(HostSet)).toEqual([{ unset: ["role"], hosts: ["host_db1", "host_web1"] }]);
});

test("Retire asks once, saying what follows, leaves out the Hosts Retired already, and retires the rest in one call", async () => {
  const api = await selecting("/hosts?all=1");
  api.on(HostRetire, ({ hosts = [] }) => ({ hosts: fleet.filter((h) => hosts.includes(h.id)) }));
  await screen.findByRole("rowheader", { name: /^old1/ });
  clickRow("old1");

  act("Retire");
  let dialog = await screen.findByRole("alertdialog", { name: "Retire 0 Hosts" });
  expect(dialog.textContent).toContain("Every Host selected is Retired already.");
  expect(within(dialog).queryByRole("button", { name: /^Retire/ })).toBeNull();
  fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
  await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  expect(summary()).toBe("1 selected");

  clickRow("db1");
  clickRow("web1");
  act("Retire");
  dialog = await screen.findByRole("alertdialog", { name: "Retire 2 Hosts" });
  expect(dialog.textContent).toContain("koko keeps each record, with its Attributes, Note, Services and pinned host key, under its id.");
  expect(listed(dialog, /^Hosts/)).toEqual(["db1 · ops", "web1 · ops"]);
  expect(dialog.textContent).toContain("1 Host is Retired already and is left out.");
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Retire 2 Hosts" }));

  expect(await screen.findByText("2 Hosts are Retired.")).toBeDefined();
  expect(api.requests(HostRetire)).toEqual([{ hosts: ["host_db1", "host_web1"] }]);
  await waitFor(() => expect(bar()).toBeNull());
});

test("Delete leaves out the Hosts that have been Managed and, for more than one, waits for their number to be typed", async () => {
  const api = await selecting();
  api.on(HostRemove, ({ hosts = [] }) => ({ hosts: fleet.filter((h) => hosts.includes(h.id)) }));
  clickRow("web1");

  act("Delete");
  let dialog = await screen.findByRole("alertdialog", { name: "Delete 0 Hosts" });
  expect(dialog.textContent).toContain("1 Host has been Managed, so koko keeps its record and never deletes it.");
  fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
  await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());

  clickRow("db1");
  clickRow("web2");
  act("Delete");
  dialog = await screen.findByRole("alertdialog", { name: "Delete 2 Hosts" });
  expect(dialog.textContent).toContain("Each Host is deleted permanently, with its Attributes, its Note and its Services.");
  expect(listed(dialog, /^Hosts/)).toEqual(["db1 · ops", "web2 · ops"]);
  expect(dialog.textContent).toContain("It is left out; retire it instead.");
  const confirm = within(dialog).getByRole<HTMLButtonElement>("button", { name: "Delete 2 Hosts" });
  expect(confirm.disabled).toBe(true);
  await expectAccessible();
  fireEvent.change(within(dialog).getByRole("textbox", { name: "Type 2 to confirm" }), { target: { value: "3" } });
  expect(confirm.disabled).toBe(true);
  fireEvent.change(within(dialog).getByRole("textbox", { name: "Type 2 to confirm" }), { target: { value: "2" } });
  expect(confirm.disabled).toBe(false);
  fireEvent.click(confirm);

  expect(await screen.findByText("2 Hosts are deleted.")).toBeDefined();
  expect(api.requests(HostRemove)).toEqual([{ hosts: ["host_db1", "host_web2"] }]);
  await waitFor(() => expect(bar()).toBeNull());
});

test("one Host is deleted without its number typed", async () => {
  const api = await selecting();
  api.on(HostRemove, () => ({ hosts: [db1] }));
  clickRow("db1");

  act("Delete");
  const dialog = await screen.findByRole("alertdialog", { name: "Delete 1 Host" });
  expect(within(dialog).queryByRole("textbox")).toBeNull();
  fireEvent.click(within(dialog).getByRole("button", { name: "Delete 1 Host" }));

  expect(await screen.findByText("1 Host is deleted.")).toBeDefined();
  expect(api.requests(HostRemove)).toEqual([{ hosts: ["host_db1"] }]);
});

test("a refused batch changed nothing: the dialog names each Host at fault with its reason and sends the rest", async () => {
  const api = await selecting();
  api.on(HostRetire, ({ hosts = [] }) => {
    if (hosts.includes("host_web1")) {
      throw batchRefusal({ [hosts.indexOf("host_web1")]: "relay_in_use" });
    }
    return { hosts: fleet.filter((h) => hosts.includes(h.id)) };
  });
  clickRow("db1");
  clickRow("web1");
  clickRow("web2");

  act("Retire");
  let dialog = await screen.findByRole("alertdialog", { name: "Retire 3 Hosts" });
  fireEvent.click(within(dialog).getByRole("button", { name: "Retire 3 Hosts" }));

  dialog = await screen.findByRole("alertdialog", { name: "No Host was Retired" });
  expect(alerts().join(" ")).toContain("It refused this one because of 1 of its 3 records, so nothing was changed.");
  expect(listed(dialog, "Records at fault")).toEqual([
    "web1The Host is the Relay of a Host that is not Retired, so it cannot be Retired yet.",
  ]);
  await expectAccessible();

  // Cancel leaves the selection as it was.
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  expect(summary()).toBe("3 selected");
  expect(ticked()).toEqual(["Select db1", "Select web1", "Select web2"]);

  act("Retire");
  dialog = await screen.findByRole("alertdialog", { name: "Retire 3 Hosts" });
  fireEvent.click(within(dialog).getByRole("button", { name: "Retire 3 Hosts" }));
  dialog = await screen.findByRole("alertdialog", { name: "No Host was Retired" });
  fireEvent.click(within(dialog).getByRole("button", { name: "Retire the other 2 Hosts" }));

  expect(await screen.findByText("2 Hosts are Retired.")).toBeDefined();
  expect(api.requests(HostRetire)).toEqual([
    { hosts: ["host_db1", "host_web1", "host_web2"] },
    { hosts: ["host_db1", "host_web1", "host_web2"] },
    { hosts: ["host_db1", "host_web2"] },
  ]);
  await waitFor(() => expect(bar()).toBeNull());
});

test("a batch refused for every Host offers only to close, and a refusal that names no Host shows in place", async () => {
  const api = await selecting();
  api.on(HostMove, ({ hosts = [] }) => {
    throw hosts.length > 1 ? batchRefusal({ 0: "unknown_host", 1: "forbidden" }) : refusal("forbidden", 403);
  });
  clickRow("db1");
  clickRow("web1");

  act("Move");
  let dialog = await screen.findByRole("dialog", { name: "Move 2 Hosts" });
  await searchAndChoose("Move to", "", "dev");
  fireEvent.click(within(dialog).getByRole("button", { name: "Move 2 Hosts" }));
  dialog = await screen.findByRole("dialog", { name: "No Host was moved" });
  expect(listed(dialog, "Records at fault").map((line) => line.slice(0, 4))).toEqual(["db1N", "web1"]);
  expect(within(dialog).queryByRole("button", { name: /^Move the other/ })).toBeNull();
  // The form's own close button, then the one at its foot.
  fireEvent.click(within(dialog).getAllByRole("button", { name: "Close" }).at(-1) as HTMLElement);
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(summary()).toBe("2 selected");

  clickRow("web1");
  act("Move");
  dialog = await screen.findByRole("dialog", { name: "Move 1 Host" });
  await searchAndChoose("Move to", "", "dev");
  fireEvent.click(within(dialog).getByRole("button", { name: "Move 1 Host" }));
  await waitFor(() => expect(alerts().length).toBeGreaterThan(0));
  expect(screen.getByRole("dialog", { name: "Move 1 Host" })).toBeDefined();
  expect(api.requests(HostMove).at(-1)).toEqual({ hosts: ["host_db1"], space: "dev" });
});

test("on a phone a tap anywhere on a row toggles it, and each Space's heading ticks its rows", async () => {
  setViewportWidth(390);
  const api = fakeApi();
  actAs(api, alice);
  records(api, { hosts: fleet });
  renderAt("/hosts");
  await screen.findByRole("link", { name: /^web1/ });
  fireEvent.click(screen.getByRole("button", { name: "Select" }));
  await screen.findByRole("region", { name: "Actions on the selected Hosts" });

  expect(screen.queryByRole("link", { name: /^web1/ })).toBeNull();
  fireEvent.click(screen.getByText("web1.example.com"));
  expect(summary()).toBe("1 selected");
  expect(ticked()).toEqual(["Select web1"]);
  tick("Select the 4 Hosts of ops");
  expect(summary()).toBe("4 selected");
  tick("Select web1");
  expect(summary()).toBe("3 selected");
  await expectAccessible();

  fireEvent.click(within(bar() as HTMLElement).getByRole("button", { name: "Done" }));
  await waitFor(() => expect(bar()).toBeNull());
  expect(screen.getByRole("link", { name: /^web1/ })).toBeDefined();
});
