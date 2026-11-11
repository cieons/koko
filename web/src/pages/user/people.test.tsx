import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, type MockInstance, onTestFinished, test, vi } from "vitest";

import { DeviceAdd } from "../../api/device";
import { SpaceMemberAdd, SpaceMemberRemove } from "../../api/space";
import { UserInvite, UserRemove } from "../../api/user";
import { navigate } from "../../shell/router";
import { expectAccessible } from "../../test/axe";
import { clipboard } from "../../test/clipboard";
import { searchAndChoose } from "../../test/controls";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { actAs, alerts, renderAt } from "../../test/pages";
import { alice, joinCode, serve, space, type World, world } from "../space/access.fixture";

// A person's panel over Access, the Owner's alone, and Invite, against a
// fake koko server. Access itself is in space/access.test.tsx; the devices
// a panel lists, as Account lists them, in device/devices.test.tsx.

/** asOwner renders path as alice, the Owner, on her laptop, against w. */
async function asOwner(path: string, w: World = world()): Promise<FakeApi> {
  const api = fakeApi();
  actAs(api, alice, w.devices[0]);
  serve(api, w);
  renderAt(path);
  await screen.findByRole("heading", { level: 1, name: "Access" });
  return api;
}

/** openPanel waits for the panel of name to show what it holds. */
async function openPanel(name: string): Promise<HTMLElement> {
  const panel = await screen.findByRole("dialog", { name });
  await within(panel).findByRole("heading", { name: "Devices" });
  return panel;
}

let intervals: MockInstance<typeof window.setInterval> | undefined;

/** stopClock stops the browser's clock for one test; only Date and intervals are faked. */
function stopClock(): void {
  vi.useFakeTimers({ toFake: ["Date", "setInterval", "clearInterval"] });
  intervals = vi.spyOn(window, "setInterval");
  onTestFinished(() => {
    vi.useRealTimers();
  });
}

/** advance moves the stopped clock on by ms once the countdown of a second runs. */
async function advance(ms: number): Promise<void> {
  await vi.waitUntil(() => intervals?.mock.calls.some(([, period]) => period === 1000), { interval: 0 });
  act(() => vi.advanceTimersByTime(ms));
}

// A person's panel.

test("a person's panel shows their role and date, their Spaces under Add to a Space, their Personal Space and devices", async () => {
  await asOwner("/access/people/bob?view=spaces");

  const panel = await openPanel("bob");
  expect(within(panel).getByText("Member · Added 2026-11-02")).toBeDefined();
  expect(within(panel).getByText("A Member of 1 of 2 shared Spaces.")).toBeDefined();
  expect(within(panel).getByRole("combobox", { name: "Add to a Space" })).toBeDefined();
  expect(within(panel).getByRole("link", { name: "ops" }).getAttribute("href")).toBe("/access/spaces/ops?view=spaces");
  expect(within(panel).getByRole("button", { name: "Take bob out of ops" })).toBeDefined();
  expect(await within(panel).findByText(/^The Personal Space bob holds 1 Host and 0 Secrets\./)).toBeDefined();
  expect(within(panel).getByRole("link", { name: "View its Hosts" }).getAttribute("href")).toBe(
    "/hosts?filter=space%3Dbob",
  );
  expect(await within(panel).findByText("laptop")).toBeDefined();
  expect(within(panel).getByText("phone")).toBeDefined();
  expect(within(panel).getByRole("button", { name: "Remove bob" })).toBeDefined();
  await expectAccessible();
});

test("Add to a Space offers only the Spaces the person is not in, and the People list shows the change", async () => {
  const api = await asOwner("/access");
  fireEvent.click(await screen.findByRole("link", { name: /^carol/ }));
  const panel = await openPanel("carol");
  expect(within(panel).getByText("Not a Member of any shared Space yet.")).toBeDefined();

  await searchAndChoose(within(panel).getByRole("combobox", { name: "Add to a Space" }), "", "web", ["ops", "web"]);
  fireEvent.click(within(panel).getByRole("button", { name: "Add" }));

  expect(await screen.findByText("carol is now a Member of web.")).toBeDefined();
  expect(api.requests(SpaceMemberAdd)).toEqual([{ space: "web", users: ["carol"] }]);
  expect(await within(panel).findByRole("button", { name: "Take carol out of web" })).toBeDefined();
  await searchAndChoose(within(panel).getByRole("combobox", { name: "Add to a Space" }), "", "ops", ["ops"]);
  fireEvent.click(within(panel).getByRole("button", { name: "Add" }));
  expect(await within(panel).findByText("carol is a Member of every shared Space already.")).toBeDefined();
  expect(within(panel).getByText("A Member of 2 of 2 shared Spaces.")).toBeDefined();
  await expectAccessible();

  fireEvent.click(within(panel).getByRole("button", { name: "Close" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  await waitFor(() =>
    expect(screen.getByRole("link", { name: /^carol/ }).textContent).toBe("carol2 Spaces · Invited, not yet in"),
  );
});

test("a Space being added is listed only once the koko server answers", async () => {
  const api = await asOwner("/access/people/carol");
  const panel = await openPanel("carol");
  let answer: (() => void) | undefined;
  const w = world();
  api.on(SpaceMemberAdd, ({ space: name }) =>
    new Promise((resolve) => {
      answer = () => resolve({ space: { ...space(name, { members: ["carol"] }), id: `space_${name}` } });
    }),
  );
  void w;

  await searchAndChoose(within(panel).getByRole("combobox", { name: "Add to a Space" }), "", "web");
  fireEvent.click(within(panel).getByRole("button", { name: "Add" }));
  await waitFor(() => expect(answer).toBeDefined());
  expect(within(panel).queryByRole("button", { name: "Take carol out of web" })).toBeNull();
  expect(within(panel).getByText("Not a Member of any shared Space yet.")).toBeDefined();

  act(() => answer?.());
  expect(await within(panel).findByRole("button", { name: "Take carol out of web" })).toBeDefined();
});

test("Add to a Space with no Space chosen says so and sends nothing", async () => {
  const api = await asOwner("/access/people/carol");
  const panel = await openPanel("carol");

  fireEvent.click(within(panel).getByRole("button", { name: "Add" }));

  expect(await within(panel).findByText("Choose a Space.")).toBeDefined();
  expect(api.requests(SpaceMemberAdd)).toEqual([]);
});

test("Take out in a person's panel keeps the focus in their Spaces: the next one, then Add to a Space", async () => {
  const w = world();
  w.spaces = w.spaces.map((s) => (s.name === "web" ? space("web", { members: ["bob"] }) : s));
  const api = await asOwner("/access/people/bob", w);
  const panel = await openPanel("bob");

  const ops = within(panel).getByRole("button", { name: "Take bob out of ops" });
  act(() => ops.focus());
  fireEvent.click(ops);
  expect(await screen.findByText("bob is no longer a Member of ops.")).toBeDefined();
  await waitFor(() =>
    expect(document.activeElement).toBe(within(panel).getByRole("button", { name: "Take bob out of web" })),
  );

  fireEvent.click(within(panel).getByRole("button", { name: "Take bob out of web" }));
  expect(await screen.findByText("bob is no longer a Member of web.")).toBeDefined();
  await waitFor(() =>
    expect(document.activeElement).toBe(within(panel).getByRole("combobox", { name: "Add to a Space" })),
  );
  expect(within(panel).getByText("Not a Member of any shared Space yet.")).toBeDefined();
  expect(api.requests(SpaceMemberRemove)).toEqual([
    { space: "ops", users: ["bob"] },
    { space: "web", users: ["bob"] },
  ]);
});

test("a membership change refused in a person's panel says why there, and not on the page once it closes", async () => {
  const api = await asOwner("/access");
  fireEvent.click(await screen.findByRole("link", { name: /^carol/ }));
  const panel = await openPanel("carol");
  api.on(SpaceMemberAdd, () => {
    throw refusal("user_removed", 409);
  });

  await searchAndChoose(within(panel).getByRole("combobox", { name: "Add to a Space" }), "", "web");
  fireEvent.click(within(panel).getByRole("button", { name: "Add" }));
  expect(await within(panel).findByRole("alert")).toHaveProperty("textContent", "That User was Removed.");
  fireEvent.click(within(panel).getByRole("button", { name: "Close" }));

  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(alerts()).toEqual([]);
});

test("the Owner's own panel says the Owner sees every Space and offers no Remove", async () => {
  await asOwner("/access/people/alice");

  const panel = await openPanel("alice");
  expect(within(panel).getByText("Owner · Added 2026-11-01")).toBeDefined();
  expect(within(panel).getByText("The Owner sees every Space.")).toBeDefined();
  expect(within(panel).queryByRole("combobox", { name: "Add to a Space" })).toBeNull();
  expect(within(panel).queryByRole("button", { name: /^Remove/ })).toBeNull();
});

test("a Removed User's panel says so and offers nothing", async () => {
  await asOwner("/access/people/dave?all=1");

  const panel = await screen.findByRole("dialog", { name: "dave" });
  expect(await within(panel).findByText(/^This User was Removed\./)).toBeDefined();
  expect(within(panel).getByText("Member · Removed 2026-11-06")).toBeDefined();
  expect(within(panel).queryAllByRole("button").map((b) => b.getAttribute("aria-label") ?? b.textContent)).toEqual([
    "Close",
  ]);
  await expectAccessible();
});

test("Remove User asks first, then says the User was Removed, and the panel closes", async () => {
  const api = await asOwner("/access/people/bob");
  const panel = await openPanel("bob");

  fireEvent.click(within(panel).getByRole("button", { name: "Remove bob" }));
  const asking = await screen.findByRole("alertdialog", { name: "Remove bob" });
  expect(asking.textContent).toContain("Otherwise bob is Removed permanently");
  await expectAccessible();
  expect(api.requests(UserRemove)).toEqual([]);
  fireEvent.click(within(asking).getByRole("button", { name: "Remove" }));

  expect(
    await screen.findByText("bob is Removed. Their devices are revoked, and only the Owner sees the Space bob."),
  ).toBeDefined();
  expect(api.requests(UserRemove)).toEqual([{ name: "bob" }]);
  expect(window.location.pathname).toBe("/access");
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  await waitFor(() => expect(screen.queryByRole("link", { name: /^bob/ })).toBeNull());
});

test("removing a User who never used koko says they were deleted", async () => {
  await asOwner("/access/people/carol");
  const panel = await openPanel("carol");

  fireEvent.click(within(panel).getByRole("button", { name: "Remove carol" }));
  fireEvent.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Remove" }));

  expect(await screen.findByText("carol never used koko and is deleted. The name carol is free again.")).toBeDefined();
});

test("a removal refused with space_not_empty names the User's Personal Space", async () => {
  const api = await asOwner("/access/people/carol");
  api.on(UserRemove, () => {
    throw refusal("space_not_empty", 409);
  });
  const panel = await openPanel("carol");

  fireEvent.click(within(panel).getByRole("button", { name: "Remove carol" }));
  const asking = await screen.findByRole("alertdialog");
  fireEvent.click(within(asking).getByRole("button", { name: "Remove" }));

  expect(await within(asking).findByRole("alert")).toHaveProperty(
    "textContent",
    "carol never used koko, but the Personal Space carol still holds records. Move or remove them, then remove carol again.",
  );
  await expectAccessible();
});

test("Add device in a person's panel issues the Join Code for that User", async () => {
  const api = await asOwner("/access/people/bob");
  const panel = await openPanel("bob");

  fireEvent.click(within(panel).getByRole("button", { name: "Add device" }));
  const dialog = await screen.findByRole("dialog", { name: "Add a device for bob" });
  fireEvent.click(within(dialog).getByRole("button", { name: "Issue Join Code" }));

  const line = await screen.findByRole("region", { name: "Join Code" });
  expect(line.textContent).toContain("Run this line on the new device device-1 of bob.");
  expect(api.requests(DeviceAdd)).toEqual([{ user: "bob" }]);
});

// Invite.

test("Invite takes a name and the Spaces, then shows the Join Code once, with Done focused", async () => {
  stopClock();
  const writeText = clipboard();
  const api = await asOwner("/access");
  await screen.findByRole("link", { name: /^bob/ });

  fireEvent.click(screen.getByRole("button", { name: "Invite" }));
  const dialog = await screen.findByRole("dialog", { name: "Invite" });
  fireEvent.click(within(dialog).getByRole("button", { name: "Invite" }));
  expect(await within(dialog).findByText("Enter a name.")).toBeDefined();
  expect(api.requests(UserInvite)).toEqual([]);
  await expectAccessible();

  fireEvent.change(within(dialog).getByRole("textbox", { name: "Name" }), { target: { value: "erin" } });
  fireEvent.click(within(dialog).getByRole("checkbox", { name: "ops" }));
  fireEvent.click(within(dialog).getByRole("checkbox", { name: "web" }));
  fireEvent.click(within(dialog).getByRole("button", { name: "Invite" }));

  const done = await screen.findByRole("dialog", { name: "erin is invited" });
  const line = within(done).getByRole("region", { name: "Join Code" });
  expect(line.textContent).toContain(`koko join ${joinCode}`);
  expect(line.textContent).toContain("15:00 left");
  expect(api.requests(UserInvite)).toEqual([{ name: "erin" }]);
  expect(api.requests(SpaceMemberAdd)).toEqual([
    { space: "ops", users: ["erin"] },
    { space: "web", users: ["erin"] },
  ]);
  await waitFor(() => expect(document.activeElement).toBe(within(done).getByRole("button", { name: "Done" })));
  await expectAccessible();

  await advance(1000);
  expect(line.textContent).toContain("14:59 left");
  fireEvent.click(within(line).getByRole("button", { name: "Copy" }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith(`koko join ${joinCode}`));

  fireEvent.click(within(done).getByRole("button", { name: "Done" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(document.body.textContent).not.toContain(joinCode);
  expect(await screen.findByRole("link", { name: /^erin/ })).toBeDefined();
  expect(window.location.href).not.toContain(joinCode);
  expect(localStorage.length + sessionStorage.length).toBe(0);
});

test("Invite still shows the Join Code when a membership call fails, and names the Space not added", async () => {
  const api = await asOwner("/access");
  await screen.findByRole("link", { name: /^bob/ });
  api.on(SpaceMemberAdd, ({ space }) => {
    if (space === "ops") {
      throw refusal("internal_error", 500);
    }
    return { space: { id: "space_web", name: "web", personal: false, members: [], counts: { hosts: 0, retired_hosts: 0, secrets: 0 } } };
  });

  fireEvent.click(screen.getByRole("button", { name: "Invite" }));
  const dialog = await screen.findByRole("dialog", { name: "Invite" });
  fireEvent.change(within(dialog).getByRole("textbox", { name: "Name" }), { target: { value: "erin" } });
  fireEvent.click(within(dialog).getByRole("checkbox", { name: "ops" }));
  fireEvent.click(within(dialog).getByRole("checkbox", { name: "web" }));
  fireEvent.click(within(dialog).getByRole("button", { name: "Invite" }));

  const done = await screen.findByRole("dialog", { name: "erin is invited" });
  expect(within(done).getByRole("alert").textContent).toBe(
    "erin is not a Member of ops: The koko server failed unexpectedly. Its log records the cause. The memberships can be added in the panel of erin.",
  );
  expect(within(done).getByRole("region", { name: "Join Code" }).textContent).toContain(`koko join ${joinCode}`);
  expect(api.requests(SpaceMemberAdd)).toEqual([
    { space: "ops", users: ["erin"] },
    { space: "web", users: ["erin"] },
  ]);
  await expectAccessible();
});

test("a refused Invite says why on its first step, the name kept as typed", async () => {
  const api = await asOwner("/access");
  await screen.findByRole("link", { name: /^bob/ });
  api.on(UserInvite, () => {
    throw refusal("name_taken", 409);
  });

  fireEvent.click(screen.getByRole("button", { name: "Invite" }));
  const dialog = await screen.findByRole("dialog", { name: "Invite" });
  fireEvent.change(within(dialog).getByRole("textbox", { name: "Name" }), { target: { value: "bob" } });
  fireEvent.click(within(dialog).getByRole("checkbox", { name: "ops" }));
  fireEvent.click(within(dialog).getByRole("button", { name: "Invite" }));

  await waitFor(() => expect(alerts()).toEqual(["This name is already in use."]));
  expect(within(dialog).getByRole("textbox", { name: "Name" })).toHaveProperty("value", "bob");
  expect(api.requests(SpaceMemberAdd)).toEqual([]);
});

/** invite invites erin and waits for her Join Code to show. */
async function invite(): Promise<HTMLElement> {
  await screen.findByRole("link", { name: /^bob/ });
  fireEvent.click(screen.getByRole("button", { name: "Invite" }));
  const dialog = await screen.findByRole("dialog", { name: "Invite" });
  fireEvent.change(within(dialog).getByRole("textbox", { name: "Name" }), { target: { value: "erin" } });
  fireEvent.click(within(dialog).getByRole("button", { name: "Invite" }));
  const done = await screen.findByRole("dialog", { name: "erin is invited" });
  await within(done).findByRole("region", { name: "Join Code" });
  return done;
}

test("the Join Code is gone once the browser leaves the page, and the dialog says so", async () => {
  await asOwner("/access");
  const done = await invite();

  act(() => {
    window.dispatchEvent(new Event("pagehide"));
  });

  expect(document.body.textContent).not.toContain(joinCode);
  expect(
    within(done).getByText(
      "This Join Code is no longer shown. Issue a new one with Add device in the panel of erin.",
    ),
  ).toBeDefined();
  expect(within(done).queryByRole("textbox")).toBeNull();
});

test("the Join Code is gone once the page is left within the web UI", async () => {
  await asOwner("/access");
  await invite();

  act(() => navigate("/account"));
  await screen.findByRole("heading", { level: 2, name: "Devices" });
  act(() => window.history.back());
  await screen.findByRole("heading", { level: 1, name: "Access" });

  expect(document.body.textContent).not.toContain(joinCode);
  expect(screen.queryByRole("dialog")).toBeNull();
});

test("the Join Code is gone once its time runs out", async () => {
  stopClock();
  await asOwner("/access");
  const done = await invite();

  await advance(15 * 60_000);

  expect(document.body.textContent).not.toContain(joinCode);
  expect(
    within(done).getByText("This Join Code has expired. Issue a new one with Add device in the panel of erin."),
  ).toBeDefined();
});
