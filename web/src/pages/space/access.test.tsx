import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test } from "vitest";

import { SpaceAdd, SpaceList, SpaceMemberAdd, SpaceMemberRemove, SpaceRemove, SpaceRename, SpaceShow } from "../../api/space";
import { type User, UserList } from "../../api/user";
import { navigate } from "../../shell/router";
import { expectAccessible } from "../../test/axe";
import { searchAndChoose } from "../../test/controls";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { setViewportWidth } from "../../test/media";
import { actAs, alerts, renderAt } from "../../test/pages";
import { alice, bob, serve, space, type World, world } from "./access.fixture";

// Access, against a fake koko server: for the Owner, the Users against the
// shared Spaces in one table, two lists on a phone, Add Space, and a Space's
// panel; for a Member, their Spaces, each panel read-only. The person's
// panel and Invite are in user/people.test.tsx.

const table = "Users and shared Spaces";

/** at renders path as caller, on their laptop, against w, and waits for Access. */
async function at(path: string, w: World = world(), caller: User = alice): Promise<FakeApi> {
  const api = fakeApi();
  actAs(api, caller, w.devices.find((d) => d.user === caller.name && d.device === "laptop"));
  serve(api, w, caller);
  renderAt(path);
  await screen.findByRole("heading", { level: 1, name: "Access" });
  return api;
}

/** rows are the table's rows: the person's cell, then each Space's, a ticked checkbox as ✓, an unticked one as ·, the lock as 🔒. */
async function rows(): Promise<string[][]> {
  const shown = await screen.findByRole("table", { name: table });
  return within(shown)
    .getAllByRole("row")
    .slice(1)
    .map((row) => [
      within(row).getByRole("rowheader").textContent ?? "",
      ...within(row)
        .getAllByRole("cell")
        .slice(0, -1)
        .map((cell) => {
          const box = within(cell).queryByRole("checkbox");
          if (box) {
            return (box as HTMLInputElement).checked ? "✓" : "·";
          }
          return within(cell).queryByRole("img") ? "🔒" : (cell.textContent ?? "");
        }),
    ]);
}

/** cell is the checkbox of user's membership of space. */
function cell(user: string, space: string): HTMLInputElement {
  return screen.getByRole("checkbox", { name: `${user} is a Member of ${space}` });
}

// The Owner's table.

test("the Owner sees the Users against the shared Spaces, each cell a membership and the Owner's cells locked", async () => {
  await at("/access");
  await rows();

  const headers = within(screen.getByRole("table", { name: table })).getAllByRole("columnheader");
  expect(headers.map((h) => h.textContent)).toEqual(["3 Users", "ops3 Hosts · 3 Secrets", "web1 Host · 2 Secrets"]);
  expect(within(headers[1] as HTMLElement).getByRole("link").getAttribute("href")).toBe("/access/spaces/ops");
  expect(await rows()).toEqual([
    ["aliceOwner1 device", "🔒", "🔒"],
    ["bob2 devices · Password set", "✓", "·"],
    ["carolInvited, not yet in", "·", "·"],
  ]);
  expect(screen.getByRole("img", { name: "The Owner sees ops" })).toBeDefined();
  expect(screen.getByText("The Owner sees every Space.")).toBeDefined();
  expect(screen.getByRole("link", { name: /^bob/ }).getAttribute("href")).toBe("/access/people/bob");
  expect(screen.getByRole("button", { name: "Invite" })).toBeDefined();
  expect(screen.getByRole("button", { name: "Add Space" })).toBeDefined();
  expect(
    within(screen.getByRole("navigation", { name: "Main" })).getByRole("link", { name: "Access" }),
  ).toHaveProperty("ariaCurrent", "page");
  await waitFor(() => expect(document.title).toBe("Access · koko"));
  await expectAccessible();
});

test("ticking a cell makes the User a Member at once, with a notice, and unticking takes them out", async () => {
  const api = await at("/access");
  await rows();

  fireEvent.click(cell("carol", "web"));
  expect(await screen.findByText("carol is now a Member of web.")).toBeDefined();
  expect(cell("carol", "web").checked).toBe(true);
  expect(api.requests(SpaceMemberAdd)).toEqual([{ space: "web", users: ["carol"] }]);

  fireEvent.click(cell("bob", "ops"));
  expect(await screen.findByText("bob is no longer a Member of ops.")).toBeDefined();
  expect(cell("bob", "ops").checked).toBe(false);
  expect(api.requests(SpaceMemberRemove)).toEqual([{ space: "ops", users: ["bob"] }]);
  await waitFor(async () => expect(await rows()).toEqual([
    ["aliceOwner1 device", "🔒", "🔒"],
    ["bob2 devices · Password set", "·", "·"],
    ["carolInvited, not yet in", "·", "✓"],
  ]));
});

test("a refused membership change says why, and the cell shows what the koko server holds", async () => {
  const api = await at("/access");
  await rows();
  api.on(SpaceMemberAdd, () => {
    throw refusal("user_removed", 409);
  });

  fireEvent.click(cell("carol", "ops"));

  await waitFor(() => expect(alerts()).toEqual(["That User was Removed."]));
  expect(cell("carol", "ops").checked).toBe(false);
  await expectAccessible();
});

test("Show Removed Users adds them, struck through, as all=1 in the URL, and a bookmark opens with them", async () => {
  const api = await at("/access");
  await rows();
  expect(screen.queryByText("dave")).toBeNull();

  fireEvent.click(screen.getByRole("switch", { name: "Show Removed Users" }));

  await waitFor(() => expect(screen.getByText("dave").className).toContain("line-through"));
  expect(window.location.search).toBe("?all=1");
  expect(api.requests(UserList)).toEqual([{}, { all: true }]);
  expect((await rows())[3]).toEqual(["daveRemoved", "–", "–"]);
  expect(screen.getByRole("columnheader", { name: "3 Users" })).toBeDefined();
});

test("the table lists the Owner first and the Removed Users last", async () => {
  const w = world();
  w.users = [...w.users].reverse();
  await at("/access?all=1", w);

  expect((await rows()).map(([person]) => person)).toEqual([
    "aliceOwner1 device",
    "carolInvited, not yet in",
    "bob2 devices · Password set",
    "daveRemoved",
  ]);
});

test("a bookmark of Access with all=1 shows the Removed Users", async () => {
  const api = await at("/access?all=1");

  expect((await rows()).map(([person]) => person)).toContain("daveRemoved");
  expect(api.requests(UserList)).toEqual([{ all: true }]);
  expect((screen.getByRole("switch", { name: "Show Removed Users" }) as HTMLInputElement).checked).toBe(true);
});

test("with no shared Space the table says so, and Add Space adds one", async () => {
  const w = world();
  w.spaces = w.spaces.filter((s) => s.personal);
  const api = await at("/access", w);
  expect(await screen.findByText(/^There is no shared Space yet\./)).toBeDefined();

  fireEvent.click(screen.getByRole("button", { name: "Add Space" }));
  const dialog = await screen.findByRole("dialog", { name: "Add Space" });
  fireEvent.click(within(dialog).getByRole("button", { name: "Add Space" }));
  expect(await within(dialog).findByText("Enter a name.")).toBeDefined();
  expect(api.requests(SpaceAdd)).toEqual([]);
  await expectAccessible();

  fireEvent.change(within(dialog).getByRole("textbox", { name: "Name" }), { target: { value: "db" } });
  fireEvent.click(within(dialog).getByRole("button", { name: "Add Space" }));

  expect(await screen.findByText("The Space db is added.")).toBeDefined();
  expect(api.requests(SpaceAdd)).toEqual([{ name: "db" }]);
  expect(await screen.findByRole("columnheader", { name: /^db/ })).toBeDefined();
  expect(screen.queryByRole("dialog")).toBeNull();
});

test("a refused Add Space says why in the dialog, the name kept as typed", async () => {
  const api = await at("/access");
  await rows();
  api.on(SpaceAdd, () => {
    throw refusal("name_taken", 409);
  });

  fireEvent.click(screen.getByRole("button", { name: "Add Space" }));
  const dialog = await screen.findByRole("dialog", { name: "Add Space" });
  fireEvent.change(within(dialog).getByRole("textbox", { name: "Name" }), { target: { value: "ops" } });
  fireEvent.click(within(dialog).getByRole("button", { name: "Add Space" }));

  expect(await within(dialog).findByRole("alert")).toHaveProperty("textContent", "This name is already in use.");
  expect(within(dialog).getByRole("textbox", { name: "Name" })).toHaveProperty("value", "ops");
});

test("Access that does not load says why, and Try again loads it", async () => {
  const api = fakeApi();
  actAs(api, alice);
  const w = world();
  serve(api, w);
  api.on(SpaceList, () => {
    throw refusal("internal_error", 500);
  });
  renderAt("/access");

  await waitFor(() => expect(alerts()).toEqual(["The koko server failed unexpectedly. Its log records the cause."]));
  await expectAccessible();
  serve(api, w);
  fireEvent.click(screen.getByRole("button", { name: "Try again" }));

  expect(await rows()).toHaveLength(3);
  expect(alerts()).toEqual([]);
});

// On a phone.

test("on a phone the table is two lists, People then Spaces, and membership changes in either panel", async () => {
  setViewportWidth(390);
  const api = await at("/access");

  const people = await screen.findByRole("region", { name: "3 Users" });
  expect(within(people).getAllByRole("link").map((l) => l.textContent)).toEqual([
    "aliceOwner1 device",
    "bob2 devices · Password set",
    "carolInvited, not yet in",
  ]);
  const spaces = screen.getByRole("region", { name: "Spaces" });
  expect(within(spaces).getAllByRole("link").map((l) => l.textContent)).toEqual([
    "ops3 Hosts · 3 Secrets · bob",
    "web1 Host · 2 Secrets · No Member",
  ]);
  expect(screen.queryByRole("table")).toBeNull();
  await expectAccessible();

  // From the People: bob's panel, its Spaces as switches.
  fireEvent.click(within(people).getByRole("link", { name: /^bob/ }));
  const bobs = await screen.findByRole("dialog", { name: "bob" });
  fireEvent.click(await within(bobs).findByRole("switch", { name: "bob is a Member of web" }));
  expect(await screen.findByText("bob is now a Member of web.")).toBeDefined();
  expect(api.requests(SpaceMemberAdd)).toEqual([{ space: "web", users: ["bob"] }]);
  fireEvent.click(within(bobs).getByRole("button", { name: "Close" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

  // From the Spaces: ops's panel, its Members with Take out.
  fireEvent.click(within(screen.getByRole("region", { name: "Spaces" })).getByRole("link", { name: /^ops/ }));
  const ops = await screen.findByRole("dialog", { name: "ops" });
  fireEvent.click(await within(ops).findByRole("button", { name: "Take bob out of ops" }));
  expect(await screen.findByText("bob is no longer a Member of ops.")).toBeDefined();
  expect(api.requests(SpaceMemberRemove)).toEqual([{ space: "ops", users: ["bob"] }]);
  await waitFor(() => expect(within(ops).getByText("No Member yet. Only the Owner sees what this Space holds.")).toBeDefined());
});

// A Space's panel.

test("a Space's panel shows its three counts, each a link to its list narrowed to the Space, and its Members", async () => {
  await at("/access/spaces/ops");

  const shown = await screen.findByRole("dialog", { name: "ops" });
  expect(await within(shown).findByRole("link", { name: "Hosts in ops: 3" })).toHaveProperty(
    "href",
    `${location.origin}/hosts?filter=space%3Dops`,
  );
  expect(within(shown).getByRole("link", { name: "Retired Hosts in ops: 1" })).toHaveProperty(
    "href",
    `${location.origin}/hosts?filter=space%3Dops&filter=state%3Dretired&all=1`,
  );
  expect(within(shown).getByRole("link", { name: "Secrets in ops: 3" })).toHaveProperty(
    "href",
    `${location.origin}/secrets?space=ops`,
  );
  expect(within(shown).getByText("Shared Space")).toBeDefined();
  expect(within(shown).getAllByRole("listitem").map((i) => i.textContent)).toEqual(["bobTake out"]);
  expect(within(shown).getByRole("button", { name: "Rename" })).toBeDefined();
  expect(within(shown).getByRole("button", { name: "Remove Space" })).toBeDefined();
  // The table stays beneath it.
  expect(screen.getByRole("table", { name: table, hidden: true })).toBeDefined();
  await expectAccessible();
});

test("closing a Space's panel goes back to Access in place of its history entry, keeping its query, the focus back on its link", async () => {
  await at("/access?all=1");
  const link = await screen.findByRole("link", { name: /^ops/ });
  act(() => link.focus());
  fireEvent.click(link);
  const shown = await screen.findByRole("dialog", { name: "ops" });
  expect(window.location.pathname + window.location.search).toBe("/access/spaces/ops?all=1");
  const entries = window.history.length;

  fireEvent.keyDown(shown, { key: "Escape" });

  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(window.location.pathname + window.location.search).toBe("/access?all=1");
  expect(window.history.length).toBe(entries);
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("link", { name: /^ops/ })));
});

test("the Owner adds a Member, chosen among the Users who are not one, and takes one out", async () => {
  const api = await at("/access/spaces/ops");
  const shown = await screen.findByRole("dialog", { name: "ops" });
  await within(shown).findByRole("button", { name: "Take bob out of ops" });

  await searchAndChoose("Add a Member", "", "carol", ["carol"]);
  fireEvent.click(within(shown).getByRole("button", { name: "Add" }));

  expect(await screen.findByText("carol is now a Member of ops.")).toBeDefined();
  expect(api.requests(SpaceMemberAdd)).toEqual([{ space: "ops", users: ["carol"] }]);
  await waitFor(() =>
    expect(within(shown).getAllByRole("listitem").map((i) => i.textContent)).toEqual(["bobTake out", "carolTake out"]),
  );
  expect(await within(shown).findByText("Every User is a Member of this Space already.")).toBeDefined();

  fireEvent.click(within(shown).getByRole("button", { name: "Take bob out of ops" }));
  expect(await screen.findByText("bob is no longer a Member of ops.")).toBeDefined();
  expect(api.requests(SpaceMemberRemove)).toEqual([{ space: "ops", users: ["bob"] }]);
  await waitFor(() => expect(within(shown).getAllByRole("listitem").map((i) => i.textContent)).toEqual(["carolTake out"]));
  // Its Take out button went with it, so the focus moves to the Members' heading.
  await waitFor(() => expect(document.activeElement).toBe(within(shown).getByRole("heading", { name: "Members" })));
});

test("Add a Member with no User chosen says so and sends nothing", async () => {
  const api = await at("/access/spaces/web");
  const shown = await screen.findByRole("dialog", { name: "web" });
  await within(shown).findByRole("combobox", { name: "Add a Member" });

  fireEvent.click(within(shown).getByRole("button", { name: "Add" }));

  expect(await within(shown).findByText("Choose a User.")).toBeDefined();
  expect(api.requests(SpaceMemberAdd)).toEqual([]);
});

test("Rename moves the panel to the new name in place of its history entry", async () => {
  const api = await at("/access");
  fireEvent.click(await screen.findByRole("link", { name: /^web/ }));
  const shown = await screen.findByRole("dialog", { name: "web" });
  const entries = window.history.length;

  fireEvent.click(within(shown).getByRole("button", { name: "Rename" }));
  const dialog = await screen.findByRole("dialog", { name: "Rename web" });
  const field = within(dialog).getByRole("textbox", { name: "New name" });
  expect(field).toHaveProperty("value", "web");
  await expectAccessible();
  fireEvent.change(field, { target: { value: "www" } });
  fireEvent.click(within(dialog).getByRole("button", { name: "Rename" }));

  expect(await screen.findByText("web is renamed to www.")).toBeDefined();
  expect(api.requests(SpaceRename)).toEqual([{ name: "web", new_name: "www" }]);
  expect(window.location.pathname).toBe("/access/spaces/www");
  expect(window.history.length).toBe(entries);
  expect(await screen.findByRole("dialog", { name: "www" })).toBeDefined();
  expect(screen.queryByRole("dialog", { name: "Rename web" })).toBeNull();
  expect(await screen.findByRole("columnheader", { name: /^www/, hidden: true })).toBeDefined();
});

test("Remove Space asks first, shows space_not_empty in place, and closes the panel once removed", async () => {
  const w = world();
  w.spaces.push(space("old"));
  const api = await at("/access/spaces/ops", w);
  const ops = await screen.findByRole("dialog", { name: "ops" });

  fireEvent.click(within(ops).getByRole("button", { name: "Remove Space" }));
  const asking = await screen.findByRole("alertdialog", { name: "Remove ops" });
  expect(asking.textContent).toContain(
    "ops and its memberships are removed permanently. A Space that still holds a Host, a Retired Host or a Secret cannot be removed.",
  );
  await expectAccessible();
  expect(api.requests(SpaceRemove)).toEqual([]);
  fireEvent.click(within(asking).getByRole("button", { name: "Remove" }));
  expect(await within(asking).findByRole("alert")).toHaveProperty(
    "textContent",
    "This Space still holds records. Move or remove them first.",
  );
  fireEvent.click(within(asking).getByRole("button", { name: "Cancel" }));

  navigate("/access/spaces/old");
  const old = await screen.findByRole("dialog", { name: "old" });
  fireEvent.click(await within(old).findByRole("button", { name: "Remove Space" }));
  fireEvent.click(within(await screen.findByRole("alertdialog", { name: "Remove old" })).getByRole("button", { name: "Remove" }));

  expect(await screen.findByText("The Space old is removed.")).toBeDefined();
  expect(api.requests(SpaceRemove)).toEqual([{ name: "ops" }, { name: "old" }]);
  expect(window.location.pathname).toBe("/access");
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  await waitFor(() => expect(screen.queryByRole("columnheader", { name: /^old/ })).toBeNull());
});

test("a Personal Space's panel shows its counts, says it takes no Members, and offers the Owner nothing", async () => {
  await at("/access/spaces/bob");

  const shown = await screen.findByRole("dialog", { name: "bob" });
  expect(await within(shown).findByText("A Personal Space belongs to bob alone and takes no Members.")).toBeDefined();
  expect(within(shown).getByText("Personal Space of bob")).toBeDefined();
  expect(within(shown).getByRole("link", { name: "Hosts in bob: 1" })).toBeDefined();
  for (const name of ["Rename", "Remove Space", "Add"]) {
    expect(within(shown).queryByRole("button", { name })).toBeNull();
  }
  expect(within(shown).queryByRole("combobox")).toBeNull();
  await expectAccessible();
});

// A Member.

test("a Member's Access lists their Spaces, each opening its panel read-only, with no Owner-only control", async () => {
  const api = await at("/access", world(), bob);

  await screen.findByRole("link", { name: /^ops/ });
  const links = within(screen.getByRole("main")).getAllByRole("link", { name: /Secrets/ });
  expect(links.map((l) => l.textContent)).toEqual(["bobPersonal Space1 Host · 0 Secrets", "ops3 Hosts · 3 Secrets · bob"]);
  expect(screen.getByText("The Spaces bob can act in, with their Members.")).toBeDefined();
  expect(
    screen.getByRole("link", { name: "The devices of bob are listed on the Account page." }).getAttribute("href"),
  ).toBe("/account");
  for (const name of ["Invite", "Add Space"]) {
    expect(screen.queryByRole("button", { name })).toBeNull();
  }
  expect(screen.queryByRole("table")).toBeNull();
  expect(screen.queryByRole("switch")).toBeNull();
  expect(api.requests(UserList)).toEqual([]);
  await expectAccessible();

  fireEvent.click(screen.getByRole("link", { name: /^ops/ }));
  const shown = await screen.findByRole("dialog", { name: "ops" });
  expect(await within(shown).findByRole("link", { name: "Hosts in ops: 3" })).toBeDefined();
  expect(within(shown).getAllByRole("listitem").map((i) => i.textContent)).toEqual(["bob"]);
  for (const name of ["Rename", "Remove Space", "Take bob out of ops", "Add"]) {
    expect(within(shown).queryByRole("button", { name })).toBeNull();
  }
  expect(within(shown).queryByRole("combobox")).toBeNull();
  await expectAccessible();
});

test("a person's panel shows Page not found to a Member", async () => {
  const api = fakeApi();
  actAs(api, bob);
  serve(api, world(), bob);
  renderAt("/access/people/carol");

  expect(await screen.findByRole("heading", { level: 1, name: "Page not found" })).toBeDefined();
  expect(api.requests(UserList)).toEqual([]);
});

test("a Space a Member cannot act in says so in its panel", async () => {
  const api = await at("/access/spaces/web", world(), bob);

  const shown = await screen.findByRole("dialog", { name: "web" });
  expect(await within(shown).findByRole("alert")).toHaveProperty(
    "textContent",
    "No Space that you can act in has that name.",
  );
  expect(api.requests(SpaceShow)).toContainEqual({ name: "web" });
});

// Redirects from the pages a released koko served.

test.each([
  ["/spaces", "/access"],
  ["/spaces?all=1", "/access?all=1"],
  ["/people?all=1", "/access?all=1"],
  ["/spaces/ops?all=1", "/access/spaces/ops?all=1"],
])("%s goes to %s in place of its history entry", async (from, to) => {
  const api = fakeApi();
  actAs(api, alice);
  serve(api, world());
  renderAt(from);
  const entries = window.history.length;

  await waitFor(() => expect(window.location.pathname + window.location.search).toBe(to));
  expect(window.history.length).toBe(entries);
  expect(await screen.findByRole("heading", { level: 1, name: "Access" })).toBeDefined();
});
