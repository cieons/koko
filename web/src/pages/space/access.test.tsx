import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test } from "vitest";

import { SpaceAdd, SpaceList, SpaceMemberAdd, SpaceMemberRemove, SpaceRemove, SpaceRename, SpaceShow } from "../../api/space";
import { type User, UserList } from "../../api/user";
import { navigate } from "../../shell/router";
import { expectAccessible } from "../../test/axe";
import { clickOutside, searchAndChoose, searchAndChooseSeveral } from "../../test/controls";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { setViewportWidth } from "../../test/media";
import { actAs, alerts, renderAt } from "../../test/pages";
import { alice, bob, serve, space, type World, world } from "./access.fixture";

// Access, against a fake koko server: for the Owner, the People and the
// shared Spaces as two lists under tabs, searched by name, Add Space, and a
// Space's panel; for a Member, their Spaces, each panel read-only. The
// person's panel and Invite are in user/people.test.tsx.

/** at renders path as caller, on their laptop, against w, and waits for Access. */
async function at(path: string, w: World = world(), caller: User = alice): Promise<FakeApi> {
  const api = fakeApi();
  actAs(api, caller, w.devices.find((d) => d.user === caller.name && d.device === "laptop"));
  serve(api, w, caller);
  renderAt(path);
  await screen.findByRole("heading", { level: 1, name: "Access" });
  return api;
}

/** rows are the text of each row of the list the tab named shows, once it shows one. */
async function rows(tab: "People" | "Spaces" = "People"): Promise<string[]> {
  const panel = await screen.findByRole("tabpanel", { name: new RegExp(`^${tab}`), hidden: true });
  return (await within(panel).findAllByRole("link", { hidden: true })).map((l) => l.textContent ?? "");
}

/** tab is the tab of the list named. */
function tab(name: "People" | "Spaces"): HTMLElement {
  return screen.getByRole("tab", { name: new RegExp(`^${name}`) });
}

/** where is the page's path and query. */
function where(): string {
  return window.location.pathname + window.location.search;
}

/** search types text in the search field of the list shown. */
function search(label: string, text: string): void {
  fireEvent.change(screen.getByRole("searchbox", { name: label }), { target: { value: text } });
}

// The Owner's lists.

test("Access reads the Spaces, their Members and counts in one space.list, with no space.show per Space", async () => {
  const api = await at("/access");
  await rows();
  expect(api.requests(SpaceList)).toHaveLength(1);
  expect(api.requests(SpaceShow)).toEqual([]);

  fireEvent.click(screen.getByRole("link", { name: /^carol/ }));
  const panel = await screen.findByRole("dialog", { name: "carol" });
  await searchAndChoose(await within(panel).findByRole("combobox", { name: "Add to a Space" }), "", "web");
  // Opening the panel is a visit, which reads the Spaces again too.
  const reads = api.requests(SpaceList).length;
  fireEvent.click(within(panel).getByRole("button", { name: "Add" }));
  expect(await screen.findByText("carol is now a Member of web.")).toBeDefined();
  await waitFor(() => expect(api.requests(SpaceList)).toHaveLength(reads + 1));
  // Her panel reads her own Personal Space; no shared Space is read on its own.
  expect(api.requests(SpaceShow)).toEqual([{ name: "carol" }]);
});

test("the Owner sees the People, each a link to their panel with how many Spaces they are in and how they get in", async () => {
  await at("/access");

  expect(await rows()).toEqual([
    "aliceOwnerEvery Space · 1 device",
    "bob1 Space · 2 devices · Password set",
    "carolNo Space · Invited, not yet in",
  ]);
  expect(tab("People").textContent).toBe("People3");
  expect(tab("Spaces").textContent).toBe("Spaces2");
  expect(tab("People").getAttribute("aria-selected")).toBe("true");
  expect(screen.getByRole("link", { name: /^bob/ }).getAttribute("href")).toBe("/access/people/bob");
  expect(screen.queryByRole("table")).toBeNull();
  expect(screen.getByRole("button", { name: "Invite" })).toBeDefined();
  expect(screen.getByRole("button", { name: "Add Space" })).toBeDefined();
  expect(
    within(screen.getByRole("navigation", { name: "Main" })).getByRole("link", { name: "Access" }),
  ).toHaveProperty("ariaCurrent", "page");
  await waitFor(() => expect(document.title).toBe("Access · koko"));
  await expectAccessible();
});

test("the Spaces tab lists the shared Spaces, each with its Members, Hosts and Secrets, as view=spaces in the URL", async () => {
  await at("/access");
  await rows();

  fireEvent.click(tab("Spaces"));

  await waitFor(() => expect(where()).toBe("/access?view=spaces"));
  expect(await rows("Spaces")).toEqual(["ops1 Member · 3 Hosts · 3 Secrets", "webNo Member · 1 Host · 2 Secrets"]);
  expect(tab("Spaces").getAttribute("aria-selected")).toBe("true");
  expect(screen.getByRole("link", { name: /^ops/ }).getAttribute("href")).toBe("/access/spaces/ops?view=spaces");
  // Show Removed Users is for the People.
  expect(screen.queryByRole("switch", { name: "Show Removed Users" })).toBeNull();
  await expectAccessible();

  fireEvent.click(tab("People"));
  await waitFor(() => expect(where()).toBe("/access"));
  expect(await rows()).toHaveLength(3);
});

test("the search narrows the list shown by name, as q in the URL, and says so when nothing matches", async () => {
  await at("/access");
  await rows();

  search("Search people", "BO");
  await waitFor(() => expect(where()).toBe("/access?q=BO"));
  expect(await rows()).toEqual(["bob1 Space · 2 devices · Password set"]);

  // One search field: it narrows the Spaces too.
  fireEvent.click(tab("Spaces"));
  await waitFor(() => expect(where()).toBe("/access?q=BO&view=spaces"));
  expect(await screen.findByText("No Space matches this search.")).toBeDefined();
  await expectAccessible();
  search("Search Spaces", "we");
  expect(await rows("Spaces")).toEqual(["webNo Member · 1 Host · 2 Secrets"]);

  search("Search Spaces", "zz");
  const empty = await screen.findByRole("tabpanel", { name: /^Spaces/ });
  fireEvent.click(await within(empty).findByRole("button", { name: "Clear the search" }));
  await waitFor(() => expect(where()).toBe("/access?view=spaces"));
  expect(await rows("Spaces")).toHaveLength(2);
});

test("Access opens as it was left, from a link with no view, and Reset view goes back to the People with no search", async () => {
  await at("/access?view=spaces&q=op");
  expect(await rows("Spaces")).toEqual(["ops1 Member · 3 Hosts · 3 Secrets"]);
  cleanup();

  await at("/access");
  await waitFor(() => expect(where()).toBe("/access?view=spaces&q=op"));
  expect(await rows("Spaces")).toEqual(["ops1 Member · 3 Hosts · 3 Secrets"]);

  fireEvent.click(screen.getByRole("button", { name: "Reset view" }));
  await waitFor(() => expect(where()).toBe("/access"));
  expect(await rows()).toHaveLength(3);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "Reset view" }).disabled).toBe(true);
  cleanup();

  await at("/access");
  expect(await rows()).toHaveLength(3);
  expect(where()).toBe("/access");
});

test("a panel's link with no query opens over the view Access was left in", async () => {
  await at("/access?view=spaces&q=op");
  expect(await rows("Spaces")).toEqual(["ops1 Member · 3 Hosts · 3 Secrets"]);
  cleanup();

  await at("/access/spaces/web");

  await waitFor(() => expect(where()).toBe("/access/spaces/web?view=spaces&q=op"));
  expect(await screen.findByRole("dialog", { name: "web" })).toBeDefined();
});

test("Show Removed Users adds them, struck through, as all=1 in the URL, and a bookmark opens with them", async () => {
  const api = await at("/access");
  await rows();
  expect(screen.queryByText("dave")).toBeNull();

  fireEvent.click(screen.getByRole("switch", { name: "Show Removed Users" }));

  await waitFor(() => expect(screen.getByText("dave").className).toContain("line-through"));
  expect(where()).toBe("/access?all=1");
  expect(api.requests(UserList)).toEqual([{}, { all: true }]);
  expect((await rows())[3]).toBe("daveRemoved");
  // The count is of the Users who are not Removed.
  expect(tab("People").textContent).toBe("People3");
});

test("the People list the Owner first and the Removed Users last", async () => {
  const w = world();
  w.users = [...w.users].reverse();
  await at("/access?all=1", w);

  expect((await rows()).map((r) => /^[a-z]+/.exec(r)?.[0])).toEqual([
    "alice",
    "carol",
    "bob",
    "dave",
  ]);
});

test("a bookmark of Access with all=1 shows the Removed Users", async () => {
  const api = await at("/access?all=1");

  expect(await rows()).toContain("daveRemoved");
  expect(api.requests(UserList)).toEqual([{ all: true }]);
  expect((screen.getByRole("switch", { name: "Show Removed Users" }) as HTMLInputElement).checked).toBe(true);
});

test("with no shared Space the Spaces tab says so, and Add Space adds one", async () => {
  const w = world();
  w.spaces = w.spaces.filter((s) => s.personal);
  const api = await at("/access?view=spaces", w);
  expect(await screen.findByText(/^There is no shared Space yet\./)).toBeDefined();
  expect(tab("Spaces").textContent).toBe("Spaces0");

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
  expect(await rows("Spaces")).toEqual(["dbNo Member · 0 Hosts · 0 Secrets"]);
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

test("on a phone the lists are the same, and membership changes in either panel", async () => {
  setViewportWidth(390);
  const api = await at("/access");

  expect(await rows()).toEqual([
    "aliceOwnerEvery Space · 1 device",
    "bob1 Space · 2 devices · Password set",
    "carolNo Space · Invited, not yet in",
  ]);
  expect(screen.queryByRole("table")).toBeNull();
  await expectAccessible();

  // From the People: bob's panel, his Spaces under Add to a Space.
  fireEvent.click(screen.getByRole("link", { name: /^bob/ }));
  const bobs = await screen.findByRole("dialog", { name: "bob" });
  await searchAndChoose(await within(bobs).findByRole("combobox", { name: "Add to a Space" }), "", "web");
  fireEvent.click(within(bobs).getByRole("button", { name: "Add" }));
  expect(await screen.findByText("bob is now a Member of web.")).toBeDefined();
  expect(api.requests(SpaceMemberAdd)).toEqual([{ space: "web", users: ["bob"] }]);
  fireEvent.click(within(bobs).getByRole("button", { name: "Close" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

  // From the Spaces: ops's panel, its Members with Take out.
  fireEvent.click(tab("Spaces"));
  fireEvent.click(await screen.findByRole("link", { name: /^ops/ }));
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
  // The lists stay beneath it.
  expect(screen.getByRole("tab", { name: /^People/, hidden: true })).toBeDefined();
  await expectAccessible();
});

test("closing a Space's panel goes back to Access in place of its history entry, keeping its query, the focus back on its link", async () => {
  await at("/access?view=spaces");
  const link = await screen.findByRole("link", { name: /^ops/ });
  act(() => link.focus());
  fireEvent.click(link);
  const shown = await screen.findByRole("dialog", { name: "ops" });
  expect(where()).toBe("/access/spaces/ops?view=spaces");
  const entries = window.history.length;

  fireEvent.keyDown(shown, { key: "Escape" });

  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(where()).toBe("/access?view=spaces");
  expect(window.history.length).toBe(entries);
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("link", { name: /^ops/ })));
});

test("a click beside a Space's panel closes it, but not while a User is picked in Add Members and not yet added", async () => {
  const api = await at("/access?view=spaces");
  fireEvent.click(await screen.findByRole("link", { name: /^ops/ }));
  await screen.findByRole("dialog", { name: "ops" });
  await clickOutside();
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(window.location.pathname).toBe("/access");

  fireEvent.click(screen.getByRole("link", { name: /^ops/ }));
  const shown = await screen.findByRole("dialog", { name: "ops" });
  await within(shown).findByRole("button", { name: "Take bob out of ops" });
  await searchAndChooseSeveral("Add Members", "", ["carol"], { holder: shown });
  await clickOutside();
  expect(screen.getByRole("dialog", { name: "ops" })).toBe(shown);
  expect(api.requests(SpaceMemberAdd)).toEqual([]);
});

/** withMore is world() with erin and frank, Members of no Space. */
function withMore(): World {
  const w = world();
  w.users.push(
    { id: "user_5", name: "erin", role: "member", created_at: "2026-11-06T09:00:00Z" },
    { id: "user_6", name: "frank", role: "member", created_at: "2026-11-06T10:00:00Z" },
  );
  w.spaces.push(space("erin", { personal: true }), space("frank", { personal: true }));
  return w;
}

test("Add Members makes the Users picked, among those who are not one, Members in one call", async () => {
  const api = await at("/access/spaces/ops", withMore());
  const shown = await screen.findByRole("dialog", { name: "ops" });
  await within(shown).findByRole("button", { name: "Take bob out of ops" });

  await searchAndChooseSeveral("Add Members", "", ["carol", "erin", "frank"], {
    holder: shown,
    offered: ["carol", "erin", "frank"],
  });
  expect(within(shown).getByRole("button", { name: "Add 3 Members" })).toBeDefined();
  await expectAccessible();
  fireEvent.click(within(shown).getByRole("button", { name: "Add 3 Members" }));

  expect(await screen.findByText("3 Users are now Members of ops.")).toBeDefined();
  expect(api.requests(SpaceMemberAdd)).toEqual([{ space: "ops", users: ["carol", "erin", "frank"] }]);
  await waitFor(() =>
    expect(within(shown).getAllByRole("listitem").map((i) => i.textContent)).toEqual([
      "bobTake out",
      "carolTake out",
      "erinTake out",
      "frankTake out",
    ]),
  );
  expect(await within(shown).findByText("Every User is a Member of this Space already.")).toBeDefined();
});

test("a refused Add Members says why under the field, keeps the Users picked and adds nobody", async () => {
  const api = await at("/access/spaces/web", withMore());
  const shown = await screen.findByRole("dialog", { name: "web" });
  await within(shown).findByRole("button", { name: /Add Members$/ });
  api.on(SpaceMemberAdd, () => {
    throw refusal("user_removed", 409);
  });

  await searchAndChooseSeveral("Add Members", "", ["erin", "frank"], { holder: shown });
  fireEvent.click(within(shown).getByRole("button", { name: "Add 2 Members" }));

  const alert = await within(shown).findByRole("alert");
  expect(alert.textContent).toBe("That User was Removed.");
  // In place: right under the field, above the Members.
  expect(alert.compareDocumentPosition(within(shown).getByText("No Member yet. Only the Owner sees what this Space holds.")))
    .toBe(Node.DOCUMENT_POSITION_FOLLOWING);
  expect(within(shown).getAllByRole("row").map((r) => r.textContent)).toEqual(["erin", "frank"]);
  expect(api.requests(SpaceMemberAdd)).toEqual([{ space: "web", users: ["erin", "frank"] }]);
});

test("adding the last Users who are not Members moves the focus to the line saying every User is one", async () => {
  await at("/access/spaces/web");
  const shown = await screen.findByRole("dialog", { name: "web" });
  await within(shown).findByRole("button", { name: /Add Members$/ });

  await searchAndChooseSeveral("Add Members", "", ["bob", "carol"], { holder: shown });
  const add = within(shown).getByRole("button", { name: "Add 2 Members" });
  act(() => add.focus());
  fireEvent.click(add);

  const line = await within(shown).findByText("Every User is a Member of this Space already.");
  await waitFor(() => expect(document.activeElement).toBe(line));
});

test("a refused Take out says why and keeps the focus on its button", async () => {
  const api = await at("/access/spaces/ops");
  const shown = await screen.findByRole("dialog", { name: "ops" });
  const takeOut = await within(shown).findByRole("button", { name: "Take bob out of ops" });
  api.on(SpaceMemberRemove, () => {
    throw refusal("internal_error", 500);
  });

  act(() => takeOut.focus());
  fireEvent.click(takeOut);

  expect(await within(shown).findByRole("alert")).toHaveProperty(
    "textContent",
    "The koko server failed unexpectedly. Its log records the cause.",
  );
  expect(within(shown).getByRole("button", { name: "Take bob out of ops" })).toBe(takeOut);
  await waitFor(() => expect(document.activeElement).toBe(takeOut));
});

test("Add Members searches the Users by name and narrows the list", async () => {
  await at("/access/spaces/web", withMore());
  const shown = await screen.findByRole("dialog", { name: "web" });
  await within(shown).findByRole("button", { name: /Add Members$/ });

  await searchAndChooseSeveral("Add Members", "R", ["erin"], { holder: shown, offered: ["carol", "erin", "frank"] });
  await searchAndChooseSeveral("Add Members", "fr", ["frank"], { holder: shown, offered: ["frank"] });

  expect(within(shown).getByRole("button", { name: "Add 2 Members" })).toBeDefined();
  expect(within(shown).getAllByRole("row").map((r) => r.textContent)).toEqual(["erin", "frank"]);
});

test("Take out keeps the focus in the Members: on the next one, the one before for the last, then Add Members", async () => {
  const w = withMore();
  w.spaces = w.spaces.map((s) => (s.name === "ops" ? space("ops", { members: ["bob", "carol", "erin"] }) : s));
  const api = await at("/access/spaces/ops", w);
  const shown = await screen.findByRole("dialog", { name: "ops" });
  const takeOut = (user: string) => within(shown).getByRole("button", { name: `Take ${user} out of ops` });

  const first = await within(shown).findByRole("button", { name: "Take bob out of ops" });
  act(() => first.focus());
  fireEvent.click(first);
  expect(await screen.findByText("bob is no longer a Member of ops.")).toBeDefined();
  await waitFor(() => expect(document.activeElement).toBe(takeOut("carol")));

  act(() => takeOut("erin").focus());
  fireEvent.click(takeOut("erin"));
  expect(await screen.findByText("erin is no longer a Member of ops.")).toBeDefined();
  await waitFor(() => expect(document.activeElement).toBe(takeOut("carol")));

  fireEvent.click(takeOut("carol"));
  expect(await screen.findByText("carol is no longer a Member of ops.")).toBeDefined();
  await waitFor(() =>
    expect(document.activeElement).toBe(within(shown).getByRole("button", { name: /Add Members$/ })),
  );
  expect(within(shown).getByText("No Member yet. Only the Owner sees what this Space holds.")).toBeDefined();
  expect(api.requests(SpaceMemberRemove)).toEqual([
    { space: "ops", users: ["bob"] },
    { space: "ops", users: ["erin"] },
    { space: "ops", users: ["carol"] },
  ]);
});

test("Add Members with no User picked says so and sends nothing", async () => {
  const api = await at("/access/spaces/web");
  const shown = await screen.findByRole("dialog", { name: "web" });
  await within(shown).findByRole("button", { name: /Add Members$/ });

  fireEvent.click(within(shown).getByRole("button", { name: "Add" }));

  expect(await within(shown).findByText("Choose at least one User.")).toBeDefined();
  await waitFor(() => expect(document.activeElement).toBe(within(shown).getByRole("button", { name: /Add Members$/ })));
  expect(api.requests(SpaceMemberAdd)).toEqual([]);
});

test("Rename moves the panel to the new name in place of its history entry", async () => {
  const api = await at("/access?view=spaces");
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
  await waitFor(async () => expect(await rows("Spaces")).toContain("wwwNo Member · 1 Host · 2 Secrets"));
});

test("Remove Space asks first, shows space_not_empty in place, and closes the panel once removed", async () => {
  const w = world();
  w.spaces.push(space("old"));
  const api = await at("/access/spaces/ops?view=spaces", w);
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

  navigate("/access/spaces/old?view=spaces");
  const old = await screen.findByRole("dialog", { name: "old" });
  fireEvent.click(await within(old).findByRole("button", { name: "Remove Space" }));
  fireEvent.click(within(await screen.findByRole("alertdialog", { name: "Remove old" })).getByRole("button", { name: "Remove" }));

  expect(await screen.findByText("The Space old is removed.")).toBeDefined();
  expect(api.requests(SpaceRemove)).toEqual([{ name: "ops" }, { name: "old" }]);
  expect(window.location.pathname).toBe("/access");
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  await waitFor(async () => expect(await rows("Spaces")).not.toContain("oldNo Member · 0 Hosts · 0 Secrets"));
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
  expect(links.map((l) => l.textContent)).toEqual(["bobPersonal Space1 Host · 0 Secrets", "ops1 Member · 3 Hosts · 3 Secrets"]);
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
