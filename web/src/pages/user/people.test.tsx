import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test } from "vitest";

import { DeviceList } from "../../api/device";
import { CodeNameTaken, CodeOwnerOnly } from "../../api/errors";
import { CodeSpaceNotEmpty, SpaceList } from "../../api/space";
import { type User, UserInvite, UserList, UserRemove } from "../../api/user";
import { navigate } from "../../shell/router";
import { expectAccessible } from "../../test/axe";
import { chooseRowAction } from "../../test/controls";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { setViewportWidth } from "../../test/media";
import { actAs, findAlert, renderAt, statuses, tableRows } from "../../test/pages";

// The People page, against the fake API: the Owner's alone.

const alice: User = { id: "user_1", name: "alice", role: "owner", created_at: "2026-11-01T09:00:00Z" };
const bob: User = { id: "user_2", name: "bob", role: "member", created_at: "2026-11-02T09:00:00Z" };
const carol: User = {
  id: "user_3",
  name: "carol",
  role: "member",
  created_at: "2026-11-03T09:00:00Z",
  removed_at: "2026-11-05T09:00:00Z",
};

/** users answers user.list from the Users given, the Removed ones only for all. */
function users(api: FakeApi, all: User[]): void {
  api.on(UserList, (r) => ({ users: all.filter((u) => r.all || !u.removed_at) }));
}

const rows = () => tableRows("People");

/** nav is the side navigation's links to the pages. */
const nav = () => within(screen.getByRole("navigation", { name: "Pages" }));

/** asAlice renders the People page as alice, the Owner, with the Users given. */
async function asAlice(all: User[] = [alice, bob]): Promise<FakeApi> {
  const api = fakeApi();
  actAs(api, alice);
  users(api, all);
  renderAt("/people");
  await screen.findByRole("table", { name: "People" });
  return api;
}

test("a Member sees no People page, nor a link to it", async () => {
  const api = fakeApi();
  actAs(api, bob);
  api.on(SpaceList, () => ({ spaces: [] }));
  api.on(UserList, () => {
    throw refusal(CodeOwnerOnly, 403);
  });
  renderAt("/spaces");
  await screen.findByRole("heading", { name: "Spaces" });

  expect(
    nav()
      .getAllByRole("link")
      .map((a) => a.textContent),
  ).toEqual(["Hosts", "Services", "Secrets", "Spaces", "Devices"]);

  act(() => navigate("/people"));

  expect(await screen.findByRole("heading", { level: 1, name: "Page not found" })).toBeDefined();
  expect(api.requests(UserList)).toEqual([]);
});

test("the Owner sees the Users, and the Removed ones when asked for", async () => {
  const api = fakeApi();
  actAs(api, alice);
  api.on(SpaceList, () => ({ spaces: [] }));
  users(api, [alice, bob, carol]);
  renderAt("/spaces");
  await screen.findByRole("heading", { name: "Spaces" });

  fireEvent.click(nav().getByRole("link", { name: "People" }));

  await screen.findByRole("table", { name: "People" });
  expect(rows()).toEqual([
    ["alice", "Owner", "2026-11-01", ""],
    ["bob", "Member", "2026-11-02", ""],
  ]);
  // The Owner is never removed, so only bob's row has a menu.
  expect(screen.queryByRole("button", { name: "Actions for alice" })).toBeNull();
  expect(screen.getByRole("button", { name: "Actions for bob" })).toBeDefined();
  await expectAccessible();

  fireEvent.click(screen.getByRole("switch", { name: "Show Removed Users" }));

  await waitFor(() =>
    expect(rows()).toEqual([
      ["alice", "Owner", "2026-11-01", "", ""],
      ["bob", "Member", "2026-11-02", "", ""],
      ["carol", "Member", "2026-11-03", "2026-11-05", ""],
    ]),
  );
  expect(screen.queryByRole("button", { name: "Actions for carol" })).toBeNull();
  expect(window.location.search).toBe("?all=1");
  expect(api.requests(UserList)).toEqual([{}, { all: true }]);
});

test("a bookmark with ?all=1 opens the list with the Removed Users", async () => {
  const api = fakeApi();
  actAs(api, alice);
  users(api, [alice, bob, carol]);

  renderAt("/people?all=1");

  await waitFor(() => expect(rows().map((r) => r[0])).toEqual(["alice", "bob", "carol"]));
  expect(screen.getByRole<HTMLInputElement>("switch", { name: "Show Removed Users" }).checked).toBe(true);
});

/** inviteDave makes user.invite answer with an Invite for the name asked, whom user.list then lists. */
function inviteDave(api: FakeApi, all: User[]): void {
  api.on(UserInvite, ({ name }) => {
    const user = { id: "user_4", name, role: "member", created_at: "2026-11-06T09:00:00Z" };
    all.push(user);
    return {
      user,
      join_code: {
        id: "joincode_1",
        code: "koko_testinvite",
        user: name,
        device: "device-1",
        expires_at: "2026-11-06T09:15:00Z",
      },
    };
  });
}

test("Invite is a wizard that never asks, and shows its koko join line once, until it closes", async () => {
  const all = [alice, bob];
  const api = await asAlice(all);
  inviteDave(api, all);

  fireEvent.click(screen.getByRole("button", { name: "Invite User" }));
  const wizard = screen.getByRole("dialog", { name: "Invite User" });
  expect(within(wizard).getByText("The new User joins as a Member.")).toBeDefined();
  await expectAccessible();
  fireEvent.change(within(wizard).getByRole("textbox", { name: "Name" }), { target: { value: "dave" } });
  fireEvent.click(within(wizard).getByRole("button", { name: "Invite" }));

  const shown = await within(wizard).findByRole("region", { name: "Join Code" });
  expect(wizard.textContent).toContain("dave was invited as a Member.");
  expect(shown.textContent).toContain("Run this line on dave's new device device-1:");
  expect(shown.textContent).toContain("koko join koko_testinvite");
  expect(within(shown).getByRole("button", { name: "Copy" })).toBeDefined();
  expect(shown.textContent).toMatch(/It expires in 1[45]:\d\d\./);
  // The button that invited went with the first step; the focus moves to the last step's own
  // once that step has rendered, so the test waits for the focus itself.
  const done = within(wizard).getByRole("button", { name: "Done" });
  await waitFor(() => expect(document.activeElement).toBe(done));
  expect(screen.queryByRole("alertdialog")).toBeNull();
  expect(api.requests(UserInvite)).toEqual([{ name: "dave" }]);
  await waitFor(() => expect(rows().map((r) => r[0])).toEqual(["alice", "bob", "dave"]));
  await expectAccessible();

  fireEvent.click(done);

  expect(screen.queryByRole("dialog", { name: "Invite User" })).toBeNull();
  expect(document.body.textContent).not.toContain("koko_testinvite");
  fireEvent.click(screen.getByRole("button", { name: "Invite User" }));
  expect(
    within(screen.getByRole("dialog", { name: "Invite User" })).getByRole<HTMLInputElement>("textbox", { name: "Name" })
      .value,
  ).toBe("");
});

test("an Invite's line is gone once the page is left, or the browser leaves it", async () => {
  const all = [alice, bob];
  const api = await asAlice(all);
  inviteDave(api, all);
  api.on(DeviceList, () => ({ device_sessions: [] }));
  const invite = async () => {
    fireEvent.click(screen.getByRole("button", { name: "Invite User" }));
    const wizard = screen.getByRole("dialog", { name: "Invite User" });
    fireEvent.change(within(wizard).getByRole("textbox", { name: "Name" }), { target: { value: "dave" } });
    fireEvent.click(within(wizard).getByRole("button", { name: "Invite" }));
    await within(wizard).findByRole("region", { name: "Join Code" });
  };

  await invite();
  fireEvent(window, new Event("pagehide"));
  expect(document.body.textContent).not.toContain("koko_testinvite");
  fireEvent.click(screen.getByRole("button", { name: "Close" }));

  await invite();
  act(() => navigate("/devices"));
  await screen.findByRole("heading", { level: 1, name: "Devices" });
  act(() => navigate("/people"));
  await screen.findByRole("table", { name: "People" });
  expect(screen.queryByRole("dialog", { name: "Invite User" })).toBeNull();
  expect(document.body.textContent).not.toContain("koko_testinvite");
});

test("a refused Invite says why on its first step", async () => {
  const api = await asAlice();
  api.on(UserInvite, () => {
    throw refusal(CodeNameTaken, 409);
  });

  fireEvent.click(screen.getByRole("button", { name: "Invite User" }));
  const wizard = screen.getByRole("dialog", { name: "Invite User" });
  fireEvent.change(within(wizard).getByRole("textbox", { name: "Name" }), { target: { value: "bob" } });
  fireEvent.click(within(wizard).getByRole("button", { name: "Invite" }));

  expect(await within(wizard).findByText("This name is already in use.")).toBeDefined();
  expect(within(wizard).queryByRole("region", { name: "Join Code" })).toBeNull();
  await expectAccessible();
});

test("Invite works on a phone", async () => {
  setViewportWidth(390);
  const all = [alice, bob];
  const api = await asAlice(all);
  inviteDave(api, all);

  fireEvent.click(screen.getByRole("button", { name: "Invite User" }));
  const wizard = screen.getByRole("dialog", { name: "Invite User" });
  fireEvent.change(within(wizard).getByRole("textbox", { name: "Name" }), { target: { value: "dave" } });
  fireEvent.click(within(wizard).getByRole("button", { name: "Invite" }));

  expect((await within(wizard).findByRole("region", { name: "Join Code" })).textContent).toContain(
    "koko join koko_testinvite",
  );
  await expectAccessible();
});

test.each([
  ["deleted", "bob never joined and was deleted. The name bob is available again."],
  [
    "removed",
    "bob was Removed. Their devices are revoked and they belong to no Space. The Personal Space bob now has no Members, and only the Owner can see it.",
  ],
])("removing a User asks first, then says they were %s", async (outcome, said) => {
  const all = [alice, bob];
  const api = await asAlice(all);
  api.on(UserRemove, ({ name }) => {
    all.splice(
      all.findIndex((u) => u.name === name),
      1,
    );
    return { outcome, user: outcome === "removed" ? { ...bob, removed_at: "2026-11-06T09:00:00Z" } : bob };
  });

  await chooseRowAction("Actions for bob", "Remove");
  const dialog = screen.getByRole("alertdialog", { name: "Remove bob" });
  expect(dialog.textContent).toContain(
    "If bob never joined, the User is deleted and the name becomes available again. Otherwise bob is Removed permanently: their devices are revoked, they leave every Space, their Personal Space passes to the Owner, and the name bob cannot be used again.",
  );
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  expect(api.requests(UserRemove)).toEqual([]);

  await chooseRowAction("Actions for bob", "Remove");
  fireEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Remove" }));

  await waitFor(() => expect(statuses()).toContain(said));
  await waitFor(() => expect(rows().map((r) => r[0])).toEqual(["alice"]));
  expect(api.requests(UserRemove)).toEqual([{ name: "bob" }]);
  await expectAccessible();
});

test("a User whose Personal Space still holds records is not removed, and the page says which Space", async () => {
  const api = await asAlice();
  api.on(UserRemove, () => {
    throw refusal(CodeSpaceNotEmpty, 409);
  });

  await chooseRowAction("Actions for bob", "Remove");
  fireEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Remove" }));

  expect(await findAlert()).toBe(
    "bob never joined, and the Personal Space bob still holds records. Move or remove those records, then remove bob again.",
  );
  expect(statuses().filter((s) => s.includes("bob"))).toEqual([]); // no word of a removal
  expect(rows().map((r) => r[0])).toEqual(["alice", "bob"]);
});
