import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test } from "vitest";

import { CodeNameTaken } from "../../api/errors";
import {
  CodeSpaceNotEmpty,
  CodeUnknownSpace,
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
import { expectAccessible } from "../../test/axe";
import { chooseOption } from "../../test/controls";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { actAs, findAlert, renderAt, tableRows } from "../../test/pages";
import { spacePath } from "./path";

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
      throw refusal(CodeUnknownSpace, 404);
    }
    return { space };
  });
}

/** members are the names of a Space's Members, as its page lists them. */
function members(): string[] {
  return within(screen.getByRole("list", { name: "Members" }))
    .getAllByRole("listitem")
    .map((li) => li.textContent?.replace(/Take out$/, "") ?? "");
}

/** counts are the labels and values of the counts in a Space's header. */
function counts(): string[][] {
  const terms = screen.getAllByRole("term").map((dt) => dt.textContent);
  const values = screen.getAllByRole("definition").map((dd) => dd.textContent);
  return terms.map((term, i) => [term ?? "", values[i] ?? ""]);
}

test("a Member sees the Spaces they can act in, a Space's Members and counts, and no Owner-only action", async () => {
  const api = fakeApi();
  actAs(api, bob);
  spaces(api, [{ ...opsSpace(), id: "space_1", name: "bob", personal: true, members: [] }, opsSpace()]);
  renderAt("/spaces");

  await screen.findByRole("table", { name: "Spaces" });
  expect(tableRows("Spaces")).toEqual([
    ["bob", "Personal Space"],
    ["ops", "Shared Space"],
  ]);
  expect(within(screen.getByRole("main")).queryAllByRole("button")).toEqual([]);
  await expectAccessible();

  fireEvent.click(within(screen.getByRole("table", { name: "Spaces" })).getByRole("link", { name: "ops" }));
  expect(await screen.findByRole("heading", { level: 1, name: "ops" })).toBeDefined();
  expect(window.location.pathname).toBe("/spaces/ops");
  expect(screen.getByText("Shared Space")).toBeDefined();
  expect(members()).toEqual(["bob", "carol"]);
  expect(counts()).toEqual([
    ["Hosts", "3"],
    ["Retired Hosts", "1"],
    ["Secrets", "2"],
  ]);
  expect(within(screen.getByRole("main")).queryAllByRole("button")).toEqual([]);
  expect(api.requests(UserList)).toEqual([]);
  expect(screen.getByRole("link", { name: "All Spaces" }).getAttribute("href")).toBe("/spaces");
  await expectAccessible();
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

  fireEvent.click(screen.getByRole("button", { name: "Add Space" }));
  const dialog = screen.getByRole("dialog", { name: "Add Space" });
  fireEvent.change(within(dialog).getByRole("textbox", { name: "Name" }), { target: { value: "web" } });
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Add" }));

  expect(await screen.findByRole("link", { name: "web" })).toBeDefined();
  expect(screen.queryByRole("dialog", { name: "Add Space" })).toBeNull();
  expect(screen.queryByRole("alertdialog")).toBeNull();
  expect(api.requests(SpaceAdd)).toEqual([{ name: "web" }]);
});

test("a Space the Owner cannot add says why, in its dialog", async () => {
  const api = fakeApi();
  actAs(api, alice);
  spaces(api, [opsSpace()]);
  api.on(SpaceAdd, () => {
    throw refusal(CodeNameTaken, 409);
  });
  renderAt("/spaces");
  await screen.findByRole("link", { name: "ops" });

  fireEvent.click(screen.getByRole("button", { name: "Add Space" }));
  const dialog = screen.getByRole("dialog", { name: "Add Space" });
  fireEvent.change(within(dialog).getByRole("textbox", { name: "Name" }), { target: { value: "ops" } });
  fireEvent.click(within(dialog).getByRole("button", { name: "Add" }));

  expect(await within(dialog).findByText("This name is already in use.")).toBeDefined();
  expect(within(dialog).getByRole<HTMLInputElement>("textbox", { name: "Name" }).value).toBe("ops");
  await expectAccessible();
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
  await screen.findByRole("heading", { level: 1, name });
  return api;
}

test("the Owner renames a Space, which is never asked about, and its page moves to the new name", async () => {
  const space = opsSpace();
  const api = await ownerOn("ops", [space]);
  api.on(SpaceRename, ({ new_name }) => {
    space.name = new_name;
    return { space: { id: space.id, name: new_name, personal: false } };
  });

  fireEvent.click(screen.getByRole("button", { name: "Rename" }));
  const dialog = screen.getByRole("dialog", { name: "Rename ops" });
  fireEvent.change(within(dialog).getByRole("textbox", { name: "New name" }), { target: { value: "infra" } });
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Rename" }));

  expect(await screen.findByRole("heading", { level: 1, name: "infra" })).toBeDefined();
  expect(window.location.pathname).toBe("/spaces/infra");
  // The page stays mounted as it moves: no dialog opens again for the new name.
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(screen.queryByRole("alertdialog")).toBeNull();
  expect(api.requests(SpaceRename)).toEqual([{ name: "ops", new_name: "infra" }]);
});

test("removing a Space asks first, stating the consequence, then goes back to the Spaces", async () => {
  const api = await ownerOn("ops", [opsSpace()]);
  api.on(SpaceRemove, ({ name }) => ({ space: { id: "space_2", name, personal: false } }));

  fireEvent.click(screen.getByRole("button", { name: "Remove Space" }));
  const dialog = screen.getByRole("alertdialog", { name: "Remove ops" });
  expect(dialog.textContent).toContain(
    "ops and its memberships are removed permanently. A Space that still holds a Host, a Retired Host or a Secret cannot be removed.",
  );
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("alertdialog")).toBeNull();
  expect(api.requests(SpaceRemove)).toEqual([]);

  fireEvent.click(screen.getByRole("button", { name: "Remove Space" }));
  fireEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Remove" }));
  expect(await screen.findByRole("heading", { level: 1, name: "Spaces" })).toBeDefined();
  expect(window.location.pathname).toBe("/spaces");
  expect(api.requests(SpaceRemove)).toEqual([{ name: "ops" }]);
});

test("a Space that still holds records is not removed, and its page says why", async () => {
  const api = await ownerOn("ops", [opsSpace()]);
  api.on(SpaceRemove, () => {
    throw refusal(CodeSpaceNotEmpty, 409);
  });

  fireEvent.click(screen.getByRole("button", { name: "Remove Space" }));
  fireEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Remove" }));

  expect(await findAlert()).toBe("This Space still holds records. Move or remove them first.");
  expect(window.location.pathname).toBe("/spaces/ops");
  await expectAccessible();
});

test("the Owner adds a Member, chosen among the Users who are not one, and takes one out, never asked", async () => {
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

  fireEvent.click(screen.getByRole("button", { name: "Add Member" }));
  const dialog = screen.getByRole("dialog", { name: "Add a Member to ops" });
  await chooseOption(within(dialog).getByRole("combobox", { name: "User" }), "dave", ["alice", "dave"]);
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Add" }));
  await waitFor(() => expect(members()).toEqual(["bob", "carol", "dave"]));
  expect(screen.queryByRole("dialog", { name: "Add a Member to ops" })).toBeNull();

  fireEvent.click(screen.getByRole("button", { name: "Take carol out of ops" }));
  await waitFor(() => expect(members()).toEqual(["bob", "dave"]));

  expect(screen.queryByRole("alertdialog")).toBeNull();
  expect(api.requests(SpaceMemberAdd)).toEqual([{ space: "ops", users: ["dave"] }]);
  expect(api.requests(SpaceMemberRemove)).toEqual([{ space: "ops", users: ["carol"] }]);
  await expectAccessible();
});

test("a Personal Space shows the Owner nothing to rename, remove or give Members", async () => {
  const api = await ownerOn("alice", [{ ...opsSpace(), id: "space_1", name: "alice", personal: true, members: [] }]);

  expect(screen.getByText("Personal Space")).toBeDefined();
  expect(screen.getByText("This Space has no Members.")).toBeDefined();
  expect(within(screen.getByRole("main")).queryAllByRole("button")).toEqual([]);
  expect(api.requests(UserList)).toEqual([]);
  await expectAccessible();
});

test("a Space the caller cannot act in says so, with the way back to the Spaces", async () => {
  const api = fakeApi();
  actAs(api, bob);
  spaces(api, [opsSpace()]);

  renderAt(spacePath("infra"));

  expect(await findAlert()).toBe("No Space that you can act in has that name.");
  expect(screen.getByRole("link", { name: "All Spaces" })).toBeDefined();
  await expectAccessible();
});
