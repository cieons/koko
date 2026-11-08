import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test } from "vitest";

import { DeviceList } from "../../api/device";
import { CodeOwnerOnly } from "../../api/errors";
import { CodeSpaceNotEmpty, SpaceList } from "../../api/space";
import { type User, UserInvite, UserList, UserRemove } from "../../api/user";
import { navigate } from "../../shell/router";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { actAs, renderAt, tableRows } from "../../test/pages";

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

/** nav is the header's links to the pages. */
const nav = () => within(screen.getByRole("navigation", { name: "Pages" }));

test("a Member sees no People page, nor a link to it", async () => {
  const api = fakeApi();
  actAs(api, bob);
  api.on(SpaceList, () => ({ spaces: [] }));
  api.on(UserList, () => {
    throw refusal(CodeOwnerOnly, 403);
  });
  renderAt("/spaces");
  await screen.findByRole("heading", { name: "Spaces" });

  expect(nav().getAllByRole("link").map((a) => a.textContent)).toEqual(["Hosts", "Secrets", "Spaces", "Devices"]);

  act(() => navigate("/people"));

  expect(await screen.findByText("There is no page here.")).toBeDefined();
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
    ["bob", "Member", "2026-11-02", "Remove"],
  ]);

  fireEvent.click(screen.getByRole("checkbox", { name: "Show Removed Users" }));

  await waitFor(() =>
    expect(rows()).toEqual([
      ["alice", "Owner", "2026-11-01", "", ""],
      ["bob", "Member", "2026-11-02", "", "Remove"],
      ["carol", "Member", "2026-11-03", "2026-11-05", ""],
    ]),
  );
  expect(window.location.search).toBe("?all=1");
  expect(api.requests(UserList)).toEqual([{}, { all: true }]);
});

test("an Invite never asks, and shows its koko join line once, until the page is left", async () => {
  const api = fakeApi();
  actAs(api, alice);
  const all = [alice, bob];
  users(api, all);
  api.on(DeviceList, () => ({ device_sessions: [] }));
  api.on(UserInvite, ({ name }) => {
    const user = { id: "user_4", name, role: "member", created_at: "2026-11-06T09:00:00Z" };
    all.push(user);
    return {
      user,
      join_code: { id: "joincode_1", code: "koko_testinvite", user: name, device: "device-1", expires_at: "2026-11-06T09:15:00Z" },
    };
  });
  renderAt("/people");
  await screen.findByRole("table", { name: "People" });

  fireEvent.change(screen.getByRole("textbox", { name: "New User" }), { target: { value: "dave" } });
  fireEvent.click(screen.getByRole("button", { name: "Invite" }));

  const shown = await screen.findByRole("region", { name: "Join Code" });
  expect(shown.textContent).toContain("On dave's new device device-1, run this line:");
  expect(shown.textContent).toContain("koko join koko_testinvite");
  expect(within(shown).getByRole("button", { name: "Copy" })).toBeDefined();
  expect(shown.textContent).toMatch(/It expires in 1[45]:\d\d\./);
  expect(screen.getByText("Invited dave as a Member.")).toBeDefined();
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(api.requests(UserInvite)).toEqual([{ name: "dave" }]);
  await waitFor(() => expect(rows().map((r) => r[0])).toEqual(["alice", "bob", "dave"]));

  fireEvent.click(nav().getByRole("link", { name: "Devices" }));
  await screen.findByRole("heading", { name: "Devices" });
  fireEvent.click(nav().getByRole("link", { name: "People" }));
  await screen.findByRole("table", { name: "People" });

  expect(screen.queryByRole("region", { name: "Join Code" })).toBeNull();
  expect(document.body.textContent).not.toContain("koko_testinvite");
  expect(screen.queryByText("Invited dave as a Member.")).toBeNull();
});

test.each([
  ["deleted", "bob never joined, so they were deleted, and the name bob is free again."],
  [
    "removed",
    "bob was Removed: their devices are revoked and they are in no Space. Their Personal Space bob is now a Space with no Members that only the Owner sees.",
  ],
])("removing a User asks first, then says they were %s", async (outcome, said) => {
  const api = fakeApi();
  actAs(api, alice);
  const all = [alice, bob];
  users(api, all);
  api.on(UserRemove, ({ name }) => {
    all.splice(all.findIndex((u) => u.name === name), 1);
    return { outcome, user: outcome === "removed" ? { ...bob, removed_at: "2026-11-06T09:00:00Z" } : bob };
  });
  renderAt("/people");
  await screen.findByRole("table", { name: "People" });

  fireEvent.click(screen.getByRole("button", { name: "Remove bob" }));
  const dialog = screen.getByRole("dialog", { name: "Remove bob?" });
  expect(dialog.textContent).toContain(
    "If bob never joined, they are deleted and the name is free again. Otherwise they are Removed for good: their devices are revoked, they leave every Space, their Personal Space passes to the Owner, and the name bob is never used again.",
  );
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  expect(api.requests(UserRemove)).toEqual([]);

  fireEvent.click(screen.getByRole("button", { name: "Remove bob" }));
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Remove" }));

  expect((await screen.findByRole("status")).textContent).toBe(said);
  await waitFor(() => expect(rows().map((r) => r[0])).toEqual(["alice"]));
  expect(api.requests(UserRemove)).toEqual([{ name: "bob" }]);
});

test("a User whose Personal Space still holds records is not removed, and the page says which Space", async () => {
  const api = fakeApi();
  actAs(api, alice);
  users(api, [alice, bob]);
  api.on(UserRemove, () => {
    throw refusal(CodeSpaceNotEmpty, 409);
  });
  renderAt("/people");
  await screen.findByRole("table", { name: "People" });

  fireEvent.click(screen.getByRole("button", { name: "Remove bob" }));
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Remove" }));

  expect((await screen.findByRole("alert")).textContent).toBe(
    "bob never joined, and their Personal Space bob still holds records. Move or remove them first, then remove bob.",
  );
  expect(screen.queryByRole("status")).toBeNull();
  expect(rows().map((r) => r[0])).toEqual(["alice", "bob"]);
});
