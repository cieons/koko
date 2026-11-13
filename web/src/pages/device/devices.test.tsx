import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test } from "vitest";

import { DeviceAdd, DeviceList, DeviceRevoke } from "../../api/device";
import { type User, UserCurrent } from "../../api/user";
import { expectAccessible } from "../../test/axe";
import { clipboard } from "../../test/clipboard";
import { clickOutside } from "../../test/controls";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { setViewportWidth } from "../../test/media";
import { actAs, alerts, chooseInFrame, frameButton, preferLanguages, renderAt } from "../../test/pages";
import { administered, alice, bob, device, eve, joinCode, serve, type World, world } from "../space/access.fixture";

// Account, against a fake koko server: who the web UI acts as, the choices
// this browser keeps, one's own devices with Add device and Revoke, and the
// Personal Space. A person's panel lists the devices the same way
// (user/people.test.tsx).

/** atAccount renders path as caller, on their laptop unless local, against w, and waits for the devices. */
async function atAccount(
  { caller = bob, w = world(), local = false, path = "/account" }: { caller?: User; w?: World; local?: boolean; path?: string } = {},
): Promise<FakeApi> {
  const api = fakeApi();
  if (local) {
    api.on(UserCurrent, () => ({ user: caller }));
  } else {
    actAs(api, caller, w.devices.find((d) => d.user === caller.name && d.device === "laptop"));
  }
  serve(api, w, caller);
  renderAt(path);
  await screen.findByRole("heading", { level: 2, name: "Devices" });
  await waitFor(() => expect(devices().length).toBeGreaterThan(0));
  return api;
}

/** devices are the rows of the Devices panel, as their text. */
function devices(): string[] {
  return within(screen.getByRole("region", { name: "Devices" }))
    .queryAllByRole("listitem")
    .map((i) => i.textContent ?? "");
}

/** revoke opens the confirmation of revoking name and is it. */
async function revoke(name: string): Promise<HTMLElement> {
  fireEvent.click(screen.getByRole("button", { name: `Revoke ${name}` }));
  return screen.findByRole("alertdialog", { name: `Revoke ${name}` });
}

// Who and how.

test("a Member's Account names them, their role and the device this browser runs through, with their own devices", async () => {
  const api = await atAccount();

  expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("bobMember");
  expect(
    screen.getByText("This browser was opened by koko web on the device laptop and acts as that device."),
  ).toBeDefined();
  expect(devices()).toEqual([
    "laptopThis deviceJoined 2026-11-03 · Not used yetRevoke",
    "phoneJoined 2026-11-03 · Not used yetRevoke",
  ]);
  expect(api.requests(DeviceList)).toEqual([{ user: "bob" }]);
  expect(await screen.findByText(/^The Personal Space bob holds 1 Host and 0 Secrets\./)).toBeDefined();
  for (const name of ["Invite", "Add Space"]) {
    expect(screen.queryByRole("button", { name })).toBeNull();
  }
  await waitFor(() => expect(document.title).toBe("Account · koko"));
  await expectAccessible();
});

test("an Administrator's Account names their role", async () => {
  await atAccount({ caller: eve, w: administered(world()) });

  expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("eveAdministrator");
  expect(devices()).toEqual(["laptopThis deviceJoined 2026-11-03 · Not used yetRevoke"]);
  await expectAccessible();
});

test("in Chinese Account names the role 管理员, 所有者 or 成员", async () => {
  preferLanguages("zh-CN");
  const w = administered(world());
  for (const [caller, role] of [
    [eve, "管理员"],
    [alice, "所有者"],
    [bob, "成员"],
  ] as const) {
    const api = fakeApi();
    actAs(api, caller, w.devices.find((d) => d.user === caller.name && d.device === "laptop"));
    serve(api, w, caller);
    const { unmount } = renderAt("/account");
    await screen.findByRole("heading", { level: 2, name: "设备" });
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(`${caller.name}${role}`);
    unmount();
  }
});

test("the account panel links to Account", async () => {
  const api = fakeApi();
  actAs(api, bob);
  serve(api, world(), bob);
  renderAt("/access");
  await screen.findByRole("heading", { level: 1, name: "Access" });

  fireEvent.click(frameButton("Account: bob"));
  const panel = await screen.findByRole("dialog", { name: "Account: bob" });
  fireEvent.click(within(panel).getByRole("link", { name: "Account" }));

  expect(await screen.findByRole("heading", { level: 2, name: "Devices" })).toBeDefined();
  expect(window.location.pathname).toBe("/account");
});

test("for a Sign-in Account names the browser that signed in, and marks no device", async () => {
  const api = fakeApi();
  api.on(UserCurrent, () => ({
    user: bob,
    signin: { id: "signin_1", client: "Firefox on Linux", browser: "Firefox", system: "Linux", started_at: "", expires_at: "" },
  }));
  serve(api, world(), bob);
  renderAt("/account");
  expect(await screen.findByText("This browser is signed in with a Password from Firefox on Linux.")).toBeDefined();
  await waitFor(() => expect(devices()).toHaveLength(2));
  expect(screen.queryByText("This device")).toBeNull();
});

test("for the Local caller Account says so and marks no device", async () => {
  await atAccount({ caller: alice, local: true });

  expect(
    screen.getByText("This browser acts on the koko server's own machine, as the Local caller."),
  ).toBeDefined();
  expect(devices()).toHaveLength(1);
  expect(devices()[0]).toMatch(/^laptopJoined 2026-11-03 · Last used .+Revoke$/);
  expect(screen.queryByText("This device")).toBeNull();
});

test("This browser's Language and Appearance mirror the frame's", async () => {
  await atAccount();

  fireEvent.click(screen.getByRole("radio", { name: "Dark" }));
  await waitFor(() => expect(document.documentElement.classList.contains("dark")).toBe(true));
  expect(frameButton(/^Appearance/).getAttribute("aria-label")).toBe("Appearance: Dark");

  await chooseInFrame("Language", "简体中文");
  expect((await screen.findByRole("radio", { name: "简体中文" })).getAttribute("aria-checked")).toBe("true");
  expect(screen.getByRole("heading", { level: 2, name: "设备" })).toBeDefined();
  fireEvent.click(screen.getByRole("radio", { name: "English" }));
  expect(await screen.findByRole("heading", { level: 2, name: "Devices" })).toBeDefined();
});

// The Revoked devices.

test("Show Revoked devices shows them as all=1, with nothing to revoke, and a bookmark opens with them", async () => {
  const api = await atAccount();

  fireEvent.click(screen.getByRole("switch", { name: "Show Revoked devices" }));

  await waitFor(() => expect(devices()).toHaveLength(3));
  expect(devices()[2]).toBe("oldRevokedRevoked 2026-11-04");
  expect(window.location.search).toBe("?all=1");
  expect(api.requests(DeviceList)).toEqual([{ user: "bob" }, { user: "bob", all: true }]);
  expect(screen.queryByRole("button", { name: "Revoke old" })).toBeNull();
  await expectAccessible();
});

test("/devices goes to Account, keeping its query, in place of its history entry", async () => {
  const entries = window.history.length;
  await atAccount({ path: "/devices?all=1" });

  expect(window.location.pathname + window.location.search).toBe("/account?all=1");
  expect(window.history.length).toBe(entries);
  await waitFor(() => expect(devices()).toHaveLength(3));
});

// Add device.

test("Add device issues a Join Code for the caller's own device, never asking first, shown once with Copy", async () => {
  const writeText = clipboard();
  const api = await atAccount();

  fireEvent.click(screen.getByRole("button", { name: "Add device" }));
  const dialog = await screen.findByRole("dialog", { name: "Add device" });
  await expectAccessible();
  fireEvent.change(within(dialog).getByRole("textbox", { name: "Device name" }), { target: { value: "desk" } });
  fireEvent.click(within(dialog).getByRole("button", { name: "Issue Join Code" }));

  const done = await screen.findByRole("dialog", { name: "The Join Code is ready" });
  const line = within(done).getByRole("region", { name: "Join Code" });
  expect(line.textContent).toContain("Run this line on the new device desk of bob.");
  expect(api.requests(DeviceAdd)).toEqual([{ device: "desk" }]);
  await waitFor(() => expect(document.activeElement).toBe(within(done).getByRole("button", { name: "Done" })));
  await expectAccessible();
  fireEvent.click(within(line).getByRole("button", { name: "Copy" }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith(`koko join ${joinCode}`));

  fireEvent.keyDown(done, { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(document.body.textContent).not.toContain(joinCode);
});

test("a click beside Add device closes it while nothing in it has changed, but never once its Join Code shows, which Done closes", async () => {
  const api = await atAccount();
  const add = screen.getByRole("button", { name: "Add device" });

  act(() => add.focus());
  fireEvent.click(add);
  await screen.findByRole("dialog", { name: "Add device" });
  await clickOutside();
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  await waitFor(() => expect(document.activeElement).toBe(add));
  expect(api.requests(DeviceAdd)).toEqual([]);

  fireEvent.click(add);
  const dialog = await screen.findByRole("dialog", { name: "Add device" });
  fireEvent.change(within(dialog).getByRole("textbox", { name: "Device name" }), { target: { value: "desk" } });
  await clickOutside();
  expect(screen.getByRole("dialog", { name: "Add device" })).toBe(dialog);
  fireEvent.click(within(dialog).getByRole("button", { name: "Issue Join Code" }));

  const done = await screen.findByRole("dialog", { name: "The Join Code is ready" });
  await waitFor(() => expect(document.activeElement).toBe(within(done).getByRole("button", { name: "Done" })));
  await clickOutside();
  expect(within(done).getByRole("region", { name: "Join Code" }).textContent).toContain(joinCode);
  fireEvent.click(within(done).getByRole("button", { name: "Done" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  await waitFor(() => expect(document.activeElement).toBe(add));
  expect(document.body.textContent).not.toContain(joinCode);
});

test("Add device on a phone opens its dialog and issues the code", async () => {
  setViewportWidth(390);
  const api = await atAccount();

  fireEvent.click(screen.getByRole("button", { name: "Add device" }));
  const dialog = await screen.findByRole("dialog", { name: "Add device" });
  fireEvent.click(within(dialog).getByRole("button", { name: "Issue Join Code" }));

  expect(await screen.findByRole("region", { name: "Join Code" })).toBeDefined();
  expect(api.requests(DeviceAdd)).toEqual([{}]);
  await expectAccessible();
});

test("a refused Add device says why on its first step", async () => {
  const api = await atAccount();
  api.on(DeviceAdd, () => {
    throw refusal("name_taken", 409);
  });

  fireEvent.click(screen.getByRole("button", { name: "Add device" }));
  const dialog = await screen.findByRole("dialog", { name: "Add device" });
  fireEvent.change(within(dialog).getByRole("textbox", { name: "Device name" }), { target: { value: "phone" } });
  fireEvent.click(within(dialog).getByRole("button", { name: "Issue Join Code" }));

  expect(await within(dialog).findByRole("alert")).toHaveProperty("textContent", "This name is already in use.");
  expect(within(dialog).getByRole("textbox", { name: "Device name" })).toHaveProperty("value", "phone");
});

// Revoke.

test("Revoke asks first, states the consequence, and revokes the device", async () => {
  const api = await atAccount();

  const asking = await revoke("phone");
  expect(asking.textContent).toContain(
    "phone is disconnected at once and can never connect again, and the Join Codes it issued stop working. The devices it added keep working.",
  );
  await waitFor(() => expect(document.activeElement).toBe(within(asking).getByRole("button", { name: "Cancel" })));
  await expectAccessible();
  expect(api.requests(DeviceRevoke)).toEqual([]);
  fireEvent.click(within(asking).getByRole("button", { name: "Revoke" }));

  expect(await screen.findByText("phone is Revoked.")).toBeDefined();
  expect(api.requests(DeviceRevoke)).toEqual([{ user: "bob", device: "phone" }]);
  await waitFor(() => expect(devices()).toHaveLength(1));
  await waitFor(() => expect(screen.queryByRole("alertdialog", { name: "Revoke phone" })).toBeNull());
  // Its Revoke button went with it, so the focus moves to the list's heading.
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("heading", { level: 2, name: "Devices" })));
});

test("a click beside Revoke cancels it, the focus going back to its button, but not while the revoke runs", async () => {
  const w = world();
  const api = await atAccount({ w });
  const button = screen.getByRole("button", { name: "Revoke phone" });

  act(() => button.focus());
  const idle = await revoke("phone");
  await waitFor(() => expect(document.activeElement).toBe(within(idle).getByRole("button", { name: "Cancel" })));
  await clickOutside();
  await waitFor(() => expect(screen.queryByRole("alertdialog", { name: "Revoke phone" })).toBeNull());
  await waitFor(() => expect(document.activeElement).toBe(button));
  expect(api.requests(DeviceRevoke)).toEqual([]);

  let answer = () => {};
  const phone = w.devices.find((d) => d.user === "bob" && d.device === "phone");
  api.on(
    DeviceRevoke,
    () => new Promise((resolve) => (answer = () => resolve({ device_session: { ...phone!, revoked_at: "2026-11-08T09:00:00Z" } }))),
  );
  const asking = await revoke("phone");
  fireEvent.click(within(asking).getByRole("button", { name: "Revoke" }));
  await waitFor(() => expect(api.requests(DeviceRevoke)).toHaveLength(1));
  await clickOutside();
  expect(screen.getByRole("alertdialog", { name: "Revoke phone" })).toBe(asking);
  act(() => answer());
  expect(await screen.findByText("phone is Revoked.")).toBeDefined();
  await waitFor(() => expect(screen.queryByRole("alertdialog", { name: "Revoke phone" })).toBeNull());
});

test("revoking the device this page runs through says this page stops", async () => {
  await atAccount();

  const asking = await revoke("laptop");

  expect(asking.textContent).toContain(
    "laptop is the device this page runs through. Once it is revoked, this page can take no further action, this device deletes its key",
  );
  expect(within(asking).queryByRole("textbox")).toBeNull();
});

test("the Owner's own last device needs its name typed and sends last, on that device", async () => {
  const api = await atAccount({ caller: alice });

  const asking = await revoke("laptop");
  expect(asking.textContent).toContain(
    "laptop is the Owner's last device and the one this page runs through. Once it is revoked, this page can take no further action, this device deletes its key, and a device can be added for the Owner only by koko device add on the koko server's own machine, or from a browser signed in with the Owner's Password.",
  );
  const confirm = within(asking).getByRole("button", { name: "Revoke" });
  expect(confirm).toHaveProperty("disabled", true);
  const typed = within(asking).getByRole("textbox", { name: "Type laptop to confirm" });
  await waitFor(() => expect(document.activeElement).toBe(typed));
  await expectAccessible();
  fireEvent.change(typed, { target: { value: "laptop" } });
  fireEvent.click(confirm);

  await waitFor(() => expect(api.requests(DeviceRevoke)).toEqual([{ user: "alice", device: "laptop", last: true }]));
});

test("the Owner's own last device needs its name typed and sends last over the Unix socket", async () => {
  const api = await atAccount({ caller: alice, local: true });

  const asking = await revoke("laptop");
  expect(asking.textContent).toContain(
    "laptop is the Owner's last device. Once it is revoked, a device can be added for the Owner only by koko device add on the koko server's own machine, or from a browser signed in with the Owner's Password.",
  );
  fireEvent.change(within(asking).getByRole("textbox", { name: "Type laptop to confirm" }), { target: { value: "laptop" } });
  fireEvent.click(within(asking).getByRole("button", { name: "Revoke" }));

  await waitFor(() => expect(api.requests(DeviceRevoke)).toEqual([{ user: "alice", device: "laptop", last: true }]));
});

test("a device that is not the Owner's last asks for no name and sends no last", async () => {
  const w = world();
  w.devices.push(device("alice", "desk"));
  const api = await atAccount({ caller: alice, w });

  const asking = await revoke("desk");
  expect(within(asking).queryByRole("textbox")).toBeNull();
  fireEvent.click(within(asking).getByRole("button", { name: "Revoke" }));

  await waitFor(() => expect(api.requests(DeviceRevoke)).toEqual([{ user: "alice", device: "desk" }]));
});

test("a refused revoke says why and reloads the list, which then asks for the name", async () => {
  const w = world();
  w.devices.push(device("alice", "desk"));
  const api = await atAccount({ caller: alice, w });
  // Elsewhere, desk is revoked: laptop is now the Owner's last live device.
  w.devices = w.devices.map((d) => (d.device === "desk" ? { ...d, revoked_at: "2026-11-08T08:00:00Z" } : d));
  const lists = api.requests(DeviceList).length;

  const asking = await revoke("laptop");
  fireEvent.click(within(asking).getByRole("button", { name: "Revoke" }));

  await waitFor(() => expect(alerts()).toEqual(["This is the Owner's last device. Type its name to revoke it."]));
  expect(api.requests(DeviceRevoke)).toEqual([{ user: "alice", device: "laptop" }]);
  await waitFor(() => expect(api.requests(DeviceList).length).toBe(lists + 1));
  expect(await within(asking).findByRole("textbox", { name: "Type laptop to confirm" })).toBeDefined();
  await waitFor(() => expect(devices()).toHaveLength(1));
});

test("in a person's panel the Owner revokes another User's device, named user/device", async () => {
  const api = fakeApi();
  const w = world();
  actAs(api, alice, w.devices[0]);
  serve(api, w);
  renderAt("/access/people/bob");
  const panel = await screen.findByRole("dialog", { name: "bob" });
  await within(panel).findByText("phone");

  fireEvent.click(within(panel).getByRole("button", { name: "Revoke bob/phone" }));
  const asking = await screen.findByRole("alertdialog", { name: "Revoke bob/phone" });
  expect(asking.textContent).toContain("bob/phone is disconnected at once");
  fireEvent.click(within(asking).getByRole("button", { name: "Revoke" }));

  expect(await screen.findByText("bob/phone is Revoked.")).toBeDefined();
  expect(api.requests(DeviceRevoke)).toEqual([{ user: "bob", device: "phone" }]);
});
