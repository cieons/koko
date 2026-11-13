import { configure, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { type Secret, SecretList, SecretMove, SecretRemove, SecretShow } from "../../api/secret";
import { type Space, SpaceList } from "../../api/space";
import type { User } from "../../api/user";
import { expectAccessible } from "../../test/axe";
import { searchAndChoose } from "../../test/controls";
import { batchRefusal, type FakeApi, fakeApi } from "../../test/fakeApi";
import { actAs, alerts, renderAt } from "../../test/pages";

// The Secrets page's Select mode against the fake API
// (docs/subsystems/secrets.md, "The web UI's Secrets page"): the mode
// entered and left, rows, a range and a Space ticked, the selection kept
// while the view changes, Move and Delete each in one dialog and one batch
// call, and a batch the koko server refused.

vi.setConfig({ testTimeout: 20_000 });
configure({ asyncUtilTimeout: 5_000 });

const bob: User = { id: "user_2", name: "bob", role: "member", created_at: "2026-11-02T09:00:00Z" };

const spaces: Space[] = [
  { id: "space_1", name: "bob", personal: true },
  { id: "space_2", name: "ops", personal: false },
  { id: "space_3", name: "web", personal: false },
];

function secret(space: string, name: string): Secret {
  return {
    id: `secret_${space}_${name}`,
    space,
    name,
    created_at: "2026-11-04T09:00:00Z",
    updated_at: "2026-11-05T09:00:00Z",
  };
}

const stored = [
  secret("bob", "token"),
  secret("ops", "api-key"),
  secret("ops", "db-password"),
  secret("ops", "tls-key"),
  secret("web", "tls-key"),
];

/** selecting renders the Secrets page as bob over the stored Secrets, with the Select mode on. */
async function selecting(): Promise<FakeApi> {
  const api = fakeApi();
  actAs(api, bob);
  api.on(SpaceList, () => ({ spaces }));
  api.on(SecretList, () => ({ secrets: stored }));
  renderAt("/secrets");
  await screen.findByRole("button", { name: "Show the value of ops/api-key" });
  fireEvent.click(screen.getByRole("button", { name: "Select" }));
  await screen.findByRole("region", { name: "Actions on the selected Secrets" });
  return api;
}

/** bar is the Select mode's bar, or null when the mode is off. */
function bar(): HTMLElement | null {
  return screen.queryByRole("region", { name: "Actions on the selected Secrets" });
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

/** clickRow clicks the row of the Secret named name, <space>/<secret>, beside its checkbox. */
function clickRow(name: string, shift = false): void {
  const row = screen.getByRole("checkbox", { name: `Select ${name}` }).closest("li");
  fireEvent.click(row as HTMLElement, { shiftKey: shift });
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
    .map((item) => item.textContent ?? "");
}

test("Select starts the mode: a row shows no Copy, Show or menu and toggles on a click, and Done leaves it with nothing selected", async () => {
  const api = await selecting();

  expect(screen.queryByRole("button", { name: /^Show the value of/ })).toBeNull();
  expect(screen.queryByRole("button", { name: /^Copy/ })).toBeNull();
  expect(screen.queryByRole("button", { name: /^More actions/ })).toBeNull();
  expect(summary()).toBe("0 selected");
  for (const name of ["Move", "Delete"]) {
    expect(within(bar() as HTMLElement).getByRole<HTMLButtonElement>("button", { name }).disabled).toBe(true);
  }
  await expectAccessible();

  clickRow("ops/api-key");
  expect(summary()).toBe("1 selected");
  expect(ticked()).toEqual(["Select ops/api-key"]);
  clickRow("ops/api-key");
  expect(summary()).toBe("0 selected");
  tick("Select bob/token");
  expect(ticked()).toEqual(["Select the 1 Secret of bob", "Select bob/token"]);
  await expectAccessible();

  fireEvent.click(within(bar() as HTMLElement).getByRole("button", { name: "Done" }));
  await waitFor(() => expect(bar()).toBeNull());
  expect(screen.getByRole("button", { name: "Show the value of ops/api-key" })).toBeDefined();
  expect(screen.queryAllByRole("checkbox")).toEqual([]);
  fireEvent.click(screen.getByRole("button", { name: "Select" }));
  await waitFor(() => expect(summary()).toBe("0 selected"));
  expect(api.requests(SecretShow)).toEqual([]);
});

test("a Shift-click ticks the range from the row ticked last, across Spaces, and a Space's heading ticks its rows", async () => {
  await selecting();

  clickRow("bob/token");
  clickRow("ops/db-password", true);
  expect(ticked()).toEqual([
    "Select the 1 Secret of bob",
    "Select bob/token",
    "Select ops/api-key",
    "Select ops/db-password",
  ]);
  expect(screen.getByRole<HTMLInputElement>("checkbox", { name: "Select the 3 Secrets of ops" }).indeterminate).toBe(true);

  tick("Select the 3 Secrets of ops");
  expect(summary()).toBe("4 selected");
  tick("Select the 3 Secrets of ops");
  expect(ticked()).toEqual(["Select the 1 Secret of bob", "Select bob/token"]);
});

test("Select all ticks what the view shows, and the selection is kept across a search and the Space select, the bar counting those not shown", async () => {
  await selecting();

  expect(within(bar() as HTMLElement).getByRole("button", { name: "Select all 5" })).toBeDefined();
  clickRow("bob/token");
  fireEvent.change(screen.getByRole("searchbox", { name: "Search Secrets" }), { target: { value: "tls" } });
  await waitFor(() => expect(summary()).toBe("1 selected1 not in this view"));

  fireEvent.click(within(bar() as HTMLElement).getByRole("button", { name: "Select all 2 matching" }));
  expect(summary()).toBe("3 selected1 not in this view");
  expect(within(bar() as HTMLElement).queryByRole("button", { name: /^Select all/ })).toBeNull();

  await searchAndChoose("Space", "we", "web");
  await waitFor(() => expect(summary()).toBe("3 selected2 not in this view"));
  expect(ticked()).toEqual(["Select the 1 Secret of web", "Select web/tls-key"]);

  fireEvent.change(screen.getByRole("searchbox", { name: "Search Secrets" }), { target: { value: "" } });
  await searchAndChoose("Space", "all", "All Spaces");
  await waitFor(() => expect(summary()).toBe("3 selected"));
  act("Delete");
  const dialog = await screen.findByRole("alertdialog", { name: "Delete 3 Secrets" });
  expect(listed(dialog, /^Secrets/)).toEqual(["bob/token", "ops/tls-key", "web/tls-key"]);
});

test("Move asks once for the Space, leaves out the Secrets in it already, and moves the rest in one call, which ends the mode", async () => {
  const api = await selecting();
  api.on(SecretMove, () => ({ secrets: [] }));
  clickRow("bob/token");
  clickRow("ops/api-key");
  clickRow("ops/db-password");

  act("Move");
  const dialog = await screen.findByRole("dialog", { name: "Move 3 Secrets" });
  expect(dialog.textContent).toContain("Only the Members of the Space they move to, the Owner and the Administrators can then show them.");
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Move 3 Secrets" }));
  expect(await screen.findByText("Choose a Space.")).toBeDefined();

  await searchAndChoose("Move to", "", "ops", ["bobPersonal Space", "ops", "web"]);
  await screen.findByRole("dialog", { name: "Move 1 Secret" });
  expect(listed(dialog, /^Secrets/)).toEqual(["bob/token"]);
  expect(dialog.textContent).toContain("2 Secrets are in ops already and stay there.");
  const reads = api.requests(SecretList).length;
  fireEvent.click(within(dialog).getByRole("button", { name: "Move 1 Secret" }));

  expect(await screen.findByText("1 Secret is now in ops.")).toBeDefined();
  expect(api.requests(SecretMove)).toEqual([{ new_space: "ops", secrets: [{ space: "bob", name: "token" }] }]);
  await waitFor(() => expect(bar()).toBeNull());
  await waitFor(() => expect(api.requests(SecretList).length).toBeGreaterThan(reads));
  fireEvent.click(screen.getByRole("button", { name: "Select" }));
  await waitFor(() => expect(summary()).toBe("0 selected"));
});

test("Delete states what is lost and, for more than one, waits for their number to be typed", async () => {
  const api = await selecting();
  api.on(SecretRemove, () => ({ secrets: [] }));
  clickRow("ops/api-key");

  act("Delete");
  let dialog = await screen.findByRole("alertdialog", { name: "Delete 1 Secret" });
  expect(within(dialog).queryByRole("textbox")).toBeNull();
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());

  clickRow("web/tls-key");
  act("Delete");
  dialog = await screen.findByRole("alertdialog", { name: "Delete 2 Secrets" });
  expect(dialog.textContent).toContain("koko keeps no earlier value, so a deleted Secret cannot be restored.");
  expect(listed(dialog, /^Secrets/)).toEqual(["ops/api-key", "web/tls-key"]);
  const confirm = within(dialog).getByRole<HTMLButtonElement>("button", { name: "Delete 2 Secrets" });
  expect(confirm.disabled).toBe(true);
  await expectAccessible();
  fireEvent.change(within(dialog).getByRole("textbox", { name: "Type 2 to confirm" }), { target: { value: "2" } });
  expect(confirm.disabled).toBe(false);
  fireEvent.click(confirm);

  expect(await screen.findByText("2 Secrets are deleted.")).toBeDefined();
  expect(api.requests(SecretRemove)).toEqual([
    {
      secrets: [
        { space: "ops", name: "api-key" },
        { space: "web", name: "tls-key" },
      ],
    },
  ]);
  await waitFor(() => expect(bar()).toBeNull());
});

test("a refused batch changed nothing: the dialog names each Secret at fault with its reason and sends the rest", async () => {
  const api = await selecting();
  api.on(SecretMove, ({ secrets = [] }) => {
    const taken = secrets.flatMap((s, index) => (s.name === "tls-key" ? [index] : []));
    if (taken.length > 0) {
      throw batchRefusal(Object.fromEntries(taken.map((index) => [index, "name_taken"])));
    }
    return { secrets: [] };
  });
  clickRow("ops/api-key");
  clickRow("ops/tls-key");
  clickRow("web/tls-key");

  act("Move");
  let dialog = await screen.findByRole("dialog", { name: "Move 3 Secrets" });
  await searchAndChoose("Move to", "", "bobPersonal Space");
  fireEvent.click(within(dialog).getByRole("button", { name: "Move 3 Secrets" }));

  dialog = await screen.findByRole("dialog", { name: "No Secret was moved" });
  expect(alerts().join(" ")).toContain("It refused this one because of 2 of its 3 records, so nothing was changed.");
  expect(listed(dialog, "Records at fault")).toEqual([
    "ops/tls-keyThis name is already in use.",
    "web/tls-keyThis name is already in use.",
  ]);
  await expectAccessible();

  // Cancel leaves the selection as it was.
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(summary()).toBe("3 selected");

  act("Move");
  dialog = await screen.findByRole("dialog", { name: "Move 3 Secrets" });
  await searchAndChoose("Move to", "", "bobPersonal Space");
  fireEvent.click(within(dialog).getByRole("button", { name: "Move 3 Secrets" }));
  dialog = await screen.findByRole("dialog", { name: "No Secret was moved" });
  fireEvent.click(within(dialog).getByRole("button", { name: "Move the other Secret" }));

  expect(await screen.findByText("1 Secret is now in bob.")).toBeDefined();
  expect(api.requests(SecretMove).at(-1)).toEqual({ new_space: "bob", secrets: [{ space: "ops", name: "api-key" }] });
  await waitFor(() => expect(bar()).toBeNull());
});
