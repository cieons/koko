import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test } from "vitest";

import { CodeNameTaken } from "../../api/errors";
import {
  CodeSpaceNotEmpty,
  SpaceAdd,
  type SpaceDetail,
  SpaceList,
  SpaceMemberAdd,
  SpaceMemberRemove,
  SpaceRemove,
  SpaceRename,
  SpaceShow,
} from "../../api/space";
import { type User, UserList } from "../../api/user";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { actAs, renderAt } from "../../test/pages";
import { spacePath } from "./SpaceListPage";

// The Spaces page and a Space's page, against the fake API, as the Owner and
// as a Member.

const alice: User = { id: "user_1", name: "alice", role: "owner", created_at: "2026-11-01T09:00:00Z" };
const bob: User = { id: "user_2", name: "bob", role: "member", created_at: "2026-11-02T09:00:00Z" };

/** opsSpace is a shared Space with two Members, made afresh for each test that changes it. */
function opsSpace(): SpaceDetail {
  return {
    id: "space_2",
    name: "ops",
    personal: false,
    members: [
      { id: "user_2", name: "bob" },
      { id: "user_3", name: "carol" },
    ],
    counts: { hosts: 3, retired_hosts: 1, secrets: 2 },
  };
}

/** spaces answers space.list and space.show from the Spaces given, as they are at each call. */
function spaces(api: FakeApi, details: SpaceDetail[]): void {
  api.on(SpaceList, () => ({
    spaces: details.map(({ id, name, personal }) => ({ id, name, personal })),
  }));
  api.on(SpaceShow, ({ name }) => {
    const space = details.find((d) => d.name === name);
    if (!space) {
      throw new Error(`no Space ${name} in this test`);
    }
    return { space };
  });
}

test("a Member sees the Spaces they can act in, a Space's Members and counts, and no Owner-only button", async () => {
  const api = fakeApi();
  actAs(api, bob);
  spaces(api, [{ ...opsSpace(), id: "space_1", name: "bob", personal: true, members: [] }, opsSpace()]);

  renderAt("/spaces");

  const list = await screen.findByRole("list", { name: "Spaces" });
  expect(within(list).getAllByRole("listitem").map((li) => li.textContent)).toEqual(["bob Personal", "ops"]);
  expect(screen.queryAllByRole("button")).toEqual([]);
  expect(screen.queryAllByRole("textbox")).toEqual([]);

  fireEvent.click(within(list).getByRole("link", { name: "ops" }));

  expect(await screen.findByRole("heading", { name: "ops" })).toBeDefined();
  expect(window.location.pathname).toBe("/spaces/ops");
  const members = screen.getByRole("list", { name: "Members" });
  expect(within(members).getAllByRole("listitem").map((li) => li.textContent)).toEqual(["bob", "carol"]);
  expect(screen.getAllByRole("term").map((dt) => dt.textContent)).toEqual(["Hosts", "Retired Hosts", "Secrets"]);
  expect(screen.getAllByRole("definition").map((dd) => dd.textContent)).toEqual(["3", "1", "2"]);
  expect(screen.queryAllByRole("button")).toEqual([]);
  expect(screen.queryAllByRole("textbox")).toEqual([]);
  expect(screen.queryAllByRole("combobox", { name: "User" })).toEqual([]);
});

test("the Owner adds a Space, which is never asked about", async () => {
  const api = fakeApi();
  actAs(api, alice);
  const details = [opsSpace()];
  spaces(api, details);
  api.on(SpaceAdd, ({ name }) => {
    const space = { id: "space_9", name, personal: false };
    details.push({ ...space, members: [], counts: { hosts: 0, retired_hosts: 0, secrets: 0 } });
    return { space };
  });
  renderAt("/spaces");
  await screen.findByRole("link", { name: "ops" });

  fireEvent.change(screen.getByRole("textbox", { name: "New Space" }), { target: { value: "web" } });
  fireEvent.click(screen.getByRole("button", { name: "Add Space" }));

  expect(await screen.findByRole("link", { name: "web" })).toBeDefined();
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(api.requests(SpaceAdd)).toEqual([{ name: "web" }]);
  expect(screen.getByRole<HTMLInputElement>("textbox", { name: "New Space" }).value).toBe("");
});

test("a Space the Owner cannot add says why", async () => {
  const api = fakeApi();
  actAs(api, alice);
  spaces(api, [opsSpace()]);
  api.on(SpaceAdd, () => {
    throw refusal(CodeNameTaken, 409);
  });
  renderAt("/spaces");
  await screen.findByRole("link", { name: "ops" });

  fireEvent.change(screen.getByRole("textbox", { name: "New Space" }), { target: { value: "ops" } });
  fireEvent.click(screen.getByRole("button", { name: "Add Space" }));

  expect((await screen.findByRole("alert")).textContent).toBe("That name is taken.");
  expect(screen.getByRole<HTMLInputElement>("textbox", { name: "New Space" }).value).toBe("ops");
});

/**
 * ownerOn renders a Space's page as the Owner, with the Spaces given and the
 * Users alice, bob, carol and dave.
 */
async function ownerOn(name: string, details: SpaceDetail[]): Promise<FakeApi> {
  const api = fakeApi();
  actAs(api, alice);
  spaces(api, details);
  api.on(UserList, () => ({
    users: [alice, bob, { ...bob, id: "user_3", name: "carol" }, { ...bob, id: "user_4", name: "dave" }],
  }));
  renderAt(spacePath(name));
  await screen.findByRole("heading", { name });
  return api;
}

test("the Owner renames a Space, which is never asked about, and its page moves to the new name", async () => {
  const space = opsSpace();
  const api = await ownerOn("ops", [space]);
  api.on(SpaceRename, ({ new_name }) => {
    space.name = new_name;
    return { space: { id: space.id, name: new_name, personal: false } };
  });

  fireEvent.change(screen.getByRole("textbox", { name: "New name" }), { target: { value: "infra" } });
  fireEvent.click(screen.getByRole("button", { name: "Rename" }));

  expect(await screen.findByRole("heading", { name: "infra" })).toBeDefined();
  expect(window.location.pathname).toBe("/spaces/infra");
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(api.requests(SpaceRename)).toEqual([{ name: "ops", new_name: "infra" }]);
});

test("removing a Space asks first, stating the consequence, then goes back to the Spaces", async () => {
  const api = await ownerOn("ops", [opsSpace()]);
  api.on(SpaceRemove, ({ name }) => ({ space: { id: "space_2", name, personal: false } }));

  fireEvent.click(screen.getByRole("button", { name: "Remove Space" }));
  const dialog = screen.getByRole("dialog", { name: "Remove ops?" });
  expect(dialog.textContent).toContain(
    "ops is removed for good, with its memberships. A Space that still holds a Host, a Retired Host or a Secret is refused.",
  );
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(api.requests(SpaceRemove)).toEqual([]);

  fireEvent.click(screen.getByRole("button", { name: "Remove Space" }));
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Remove" }));

  expect(await screen.findByRole("heading", { name: "Spaces" })).toBeDefined();
  expect(window.location.pathname).toBe("/spaces");
  expect(api.requests(SpaceRemove)).toEqual([{ name: "ops" }]);
});

test("a Space that still holds records is not removed, and its page says why", async () => {
  const api = await ownerOn("ops", [opsSpace()]);
  api.on(SpaceRemove, () => {
    throw refusal(CodeSpaceNotEmpty, 409);
  });

  fireEvent.click(screen.getByRole("button", { name: "Remove Space" }));
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Remove" }));

  expect((await screen.findByRole("alert")).textContent).toBe(
    "This Space still holds records. Move or remove them first.",
  );
  expect(window.location.pathname).toBe("/spaces/ops");
});

test("the Owner adds a Member and takes one out, which are never asked about", async () => {
  const space = opsSpace();
  const api = await ownerOn("ops", [space]);
  api.on(SpaceMemberAdd, ({ users }) => {
    space.members.push(...users.map((name) => ({ id: `user_${name}`, name })));
    return { space };
  });
  api.on(SpaceMemberRemove, ({ users }) => {
    space.members = space.members.filter((m) => !users.includes(m.name));
    return { space };
  });
  const users = await screen.findByRole("combobox", { name: "User" });
  await waitFor(() => expect(within(users).getAllByRole("option").map((o) => o.textContent)).toContain("dave"));
  expect(within(users).getAllByRole("option").map((o) => o.textContent)).toEqual(["Choose a User", "alice", "dave"]);

  fireEvent.change(users, { target: { value: "dave" } });
  fireEvent.click(screen.getByRole("button", { name: "Add Member" }));
  const members = () => within(screen.getByRole("list", { name: "Members" })).getAllByRole("listitem");
  await waitFor(() => expect(members().map((li) => li.firstChild?.textContent)).toEqual(["bob", "carol", "dave"]));

  fireEvent.click(screen.getByRole("button", { name: "Take carol out of ops" }));

  await waitFor(() => expect(members().map((li) => li.firstChild?.textContent)).toEqual(["bob", "dave"]));
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(api.requests(SpaceMemberAdd)).toEqual([{ space: "ops", users: ["dave"] }]);
  expect(api.requests(SpaceMemberRemove)).toEqual([{ space: "ops", users: ["carol"] }]);
});

test("a Personal Space shows the Owner nothing to rename, remove or give Members", async () => {
  const api = await ownerOn("alice", [{ ...opsSpace(), id: "space_1", name: "alice", personal: true, members: [] }]);

  expect(screen.getByText("Personal Space")).toBeDefined();
  expect(screen.getByText("No Members.")).toBeDefined();
  expect(screen.queryAllByRole("button")).toEqual([]);
  expect(screen.queryAllByRole("textbox")).toEqual([]);
  expect(api.requests(UserList)).toEqual([]);
});
