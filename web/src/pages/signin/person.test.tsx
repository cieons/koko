import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test } from "vitest";

import { DeviceAdd } from "../../api/device";
import { PasswordLinkIssue, PasswordRemove, SignInList, SignInRevoke } from "../../api/signin";
import { UserCurrent, UserInvite } from "../../api/user";
import { expectAccessible } from "../../test/axe";
import { clipboard } from "../../test/clipboard";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { actAs, renderAt } from "../../test/pages";
import { alice, passwordLink, serve, type World, world } from "../space/access.fixture";
import { serveSignIns, signIns } from "./signins.fixture";

// The Owner's Password Links and Sign-ins of others, against a fake koko
// server: a person's panel with its Password and Browser Sign-ins, Invite's
// way in and Valid for, and Valid for on Add device.

/** asOwner renders path as alice, the Owner, on her laptop, against w, with the Sign-ins of signIns(). */
async function asOwner(path: string, w: World = world()): Promise<FakeApi> {
  const api = fakeApi();
  actAs(api, alice, w.devices[0]);
  serve(api, w);
  serveSignIns(api, signIns(), alice);
  renderAt(path);
  await screen.findByRole("heading", { level: 1, name: "Access" });
  return api;
}

/** openPanel waits for the panel of name to show its Password. */
async function openPanel(name: string): Promise<HTMLElement> {
  const panel = await screen.findByRole("dialog", { name });
  await within(panel).findByRole("heading", { name: "Password" });
  return panel;
}

/** signinsIn are the rows of the Browser Sign-ins in panel, as their text. */
function signinsIn(panel: HTMLElement): string[] {
  const region = within(panel).getByRole("region", { name: "Browser Sign-ins" });
  return within(region)
    .queryAllByRole("listitem")
    .map((i) => i.textContent ?? "");
}

/** chooseValidFor picks option in the Valid for of dialog. */
function chooseValidFor(dialog: HTMLElement, option: string): void {
  fireEvent.click(within(within(dialog).getByRole("radiogroup", { name: "Valid for" })).getByRole("radio", { name: option }));
}

// A person's panel.

test("a person's panel shows when their Password was set, with Issue Password Link, Remove Password and their Sign-ins", async () => {
  const api = await asOwner("/access/people/bob");

  const panel = await openPanel("bob");
  expect(within(panel).getByText("A Password was set on 2026-11-04.")).toBeDefined();
  expect(within(panel).getByRole("button", { name: "Issue Password Link" })).toBeDefined();
  expect(within(panel).getByRole("button", { name: "Remove Password" })).toBeDefined();
  await waitFor(() => expect(signinsIn(panel)).toHaveLength(2));
  expect(signinsIn(panel)[0]).toMatch(/^Firefox on Linux203\.0\.113\.7/);
  expect(api.requests(SignInList)).toContainEqual({ user: "bob" });
  await expectAccessible();
});

test("a User without a Password is told so, and offered no Remove Password", async () => {
  await asOwner("/access/people/carol");

  const panel = await openPanel("carol");
  expect(within(panel).getByText("No Password is set.")).toBeDefined();
  expect(within(panel).queryByRole("button", { name: "Remove Password" })).toBeNull();
  expect(await within(panel).findByText("No browser is signed in as carol.")).toBeDefined();
});

test("Issue Password Link, valid for the time chosen, shows the link once with Copy and its time left", async () => {
  const writeText = clipboard();
  const api = await asOwner("/access/people/bob");
  const panel = await openPanel("bob");

  fireEvent.click(within(panel).getByRole("button", { name: "Issue Password Link" }));
  const dialog = await screen.findByRole("dialog", { name: "Password Link for bob" });
  expect(within(dialog).getByText("An earlier link that was not used stops working.")).toBeDefined();
  expect(within(dialog).getByRole("radio", { name: "15 minutes" }).getAttribute("aria-checked")).toBe("true");
  await expectAccessible();
  chooseValidFor(dialog, "7 days");
  fireEvent.click(within(dialog).getByRole("button", { name: "Issue Password Link" }));

  const done = await screen.findByRole("dialog", { name: "The Password Link is ready" });
  const line = within(done).getByRole("region", { name: "Password Link" });
  expect(line.textContent).toContain(passwordLink("bob"));
  expect(line.textContent).toContain("7 days left");
  expect(api.requests(PasswordLinkIssue)).toEqual([{ user: "bob", valid_for: 604_800 }]);
  await waitFor(() => expect(document.activeElement).toBe(within(done).getByRole("button", { name: "Done" })));
  await expectAccessible();
  fireEvent.click(within(line).getByRole("button", { name: "Copy" }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith(passwordLink("bob")));

  fireEvent.click(within(done).getByRole("button", { name: "Done" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "The Password Link is ready" })).toBeNull());
  expect(document.body.textContent).not.toContain(passwordLink("bob"));
});

test("Issue Password Link for 15 minutes sends the koko server's own validity, left out", async () => {
  const api = await asOwner("/access/people/carol");
  const panel = await openPanel("carol");

  fireEvent.click(within(panel).getByRole("button", { name: "Issue Password Link" }));
  const dialog = await screen.findByRole("dialog", { name: "Password Link for carol" });
  fireEvent.click(within(dialog).getByRole("button", { name: "Issue Password Link" }));

  const done = await screen.findByRole("dialog", { name: "The Password Link is ready" });
  expect(within(done).getByRole("region", { name: "Password Link" }).textContent).toContain("15:00 left");
  expect(api.requests(PasswordLinkIssue)).toEqual([{ user: "carol" }]);
});

test("without a Web URL Issue Password Link is disabled, and the panel says why", async () => {
  const w = world();
  w.webURL = null;
  await asOwner("/access/people/bob", w);
  const panel = await openPanel("bob");

  expect(
    await within(panel).findByText(
      "A Password Link names the Web URL, which is not set. Set it with koko server set web_url= on the koko server's machine.",
    ),
  ).toBeDefined();
  expect(within(panel).getByRole("button", { name: "Issue Password Link" }).hasAttribute("disabled")).toBe(true);
});

test("Remove Password asks first, then the panel shows no Password and Access no longer says Password set", async () => {
  const api = await asOwner("/access/people/bob");
  const panel = await openPanel("bob");

  fireEvent.click(within(panel).getByRole("button", { name: "Remove Password" }));
  const asking = await screen.findByRole("alertdialog", { name: "Remove the Password of bob" });
  expect(
    within(asking).getByText(
      "The Sign-ins of bob end, and an unused Password Link stops working. bob and their devices stay.",
    ),
  ).toBeDefined();
  await expectAccessible();
  fireEvent.click(within(asking).getByRole("button", { name: "Remove Password" }));

  expect(await screen.findByText("The Password of bob is removed.")).toBeDefined();
  expect(api.requests(PasswordRemove)).toEqual([{ user: "bob" }]);
  expect(await within(panel).findByText("No Password is set.")).toBeDefined();
  expect(within(panel).queryByRole("button", { name: "Remove Password" })).toBeNull();
  await waitFor(() => expect(document.activeElement?.textContent).toBe("Password"));
  await waitFor(() => expect(screen.getByRole("link", { name: /^bob/ }).textContent).not.toContain("Password set"));
});

test("Remove Password on the Owner's own panel, from a browser signed in with it, says this browser is signed out", async () => {
  const api = fakeApi();
  const w = world();
  w.users = w.users.map((u) => (u.name === "alice" ? { ...u, password_set_at: "2026-11-04T09:00:00Z" } : u));
  api.on(UserCurrent, () => ({
    user: alice,
    signin: { id: "signin_alice_1", client: "Safari on macOS", browser: "Safari", system: "macOS", started_at: "", expires_at: "" },
  }));
  serve(api, w);
  serveSignIns(api, signIns(), alice);
  renderAt("/access/people/alice");
  const panel = await openPanel("alice");

  fireEvent.click(within(panel).getByRole("button", { name: "Remove Password" }));

  const asking = await screen.findByRole("alertdialog", { name: "Remove the Password of alice" });
  expect(asking.textContent).toContain("this browser's among them, so this browser is signed out");
});

test("Remove Password refused shows why in the confirmation", async () => {
  const api = await asOwner("/access/people/bob");
  api.on(PasswordRemove, () => {
    throw refusal("no_password", 409);
  });
  const panel = await openPanel("bob");

  fireEvent.click(within(panel).getByRole("button", { name: "Remove Password" }));
  const asking = await screen.findByRole("alertdialog", { name: "Remove the Password of bob" });
  fireEvent.click(within(asking).getByRole("button", { name: "Remove Password" }));

  await waitFor(() => expect(within(asking).getByRole("alert").textContent).toBe("That User has no Password."));
});

test("the Owner revokes another User's Sign-in from their panel, which says that User is signed out", async () => {
  const api = await asOwner("/access/people/bob");
  const panel = await openPanel("bob");
  await waitFor(() => expect(signinsIn(panel)).toHaveLength(2));

  fireEvent.click(within(panel).getByRole("button", { name: "Revoke the Sign-in from Safari on iPhone" }));
  const asking = await screen.findByRole("alertdialog", { name: "Revoke the Sign-in from Safari on iPhone" });
  expect(
    within(asking).getByText("bob is signed out of that browser at once and must sign in again with their Password."),
  ).toBeDefined();
  fireEvent.click(within(asking).getByRole("button", { name: "Revoke" }));

  expect(await screen.findByText("The Sign-in from Safari on iPhone is revoked.")).toBeDefined();
  expect(api.requests(SignInRevoke)).toEqual([{ id: "signin_bob_2" }]);
  await waitFor(() => expect(signinsIn(panel)).toHaveLength(1));
});

// Invite.

test("Invite in a browser, valid for the time chosen, shows the Password Link once", async () => {
  const api = await asOwner("/access");
  await screen.findByRole("link", { name: /^bob/ });

  fireEvent.click(screen.getByRole("button", { name: "Invite" }));
  const dialog = await screen.findByRole("dialog", { name: "Invite" });
  expect((within(dialog).getByRole("radio", { name: "In a terminal" }) as HTMLInputElement).checked).toBe(true);
  expect(within(dialog).getByRole("radio", { name: "15 minutes" }).getAttribute("aria-checked")).toBe("true");
  await waitFor(() => expect(within(dialog).getByRole("radio", { name: "In a browser" }).hasAttribute("disabled")).toBe(false));
  await expectAccessible();
  fireEvent.change(within(dialog).getByRole("textbox", { name: "Name" }), { target: { value: "erin" } });
  fireEvent.click(within(dialog).getByRole("radio", { name: "In a browser" }));
  chooseValidFor(dialog, "7 days");
  fireEvent.click(within(dialog).getByRole("button", { name: "Invite" }));

  const done = await screen.findByRole("dialog", { name: "erin is invited" });
  const line = within(done).getByRole("region", { name: "Password Link" });
  expect(line.textContent).toContain(passwordLink("erin"));
  expect(line.textContent).toContain("7 days left");
  expect(within(done).queryByRole("region", { name: "Join Code" })).toBeNull();
  expect(api.requests(UserInvite)).toEqual([{ name: "erin", valid_for: 604_800, web: true }]);
  await expectAccessible();
});

test("Invite in a terminal for 1 day counts down the day it was issued for", async () => {
  const api = await asOwner("/access");
  await screen.findByRole("link", { name: /^bob/ });

  fireEvent.click(screen.getByRole("button", { name: "Invite" }));
  const dialog = await screen.findByRole("dialog", { name: "Invite" });
  fireEvent.change(within(dialog).getByRole("textbox", { name: "Name" }), { target: { value: "erin" } });
  chooseValidFor(dialog, "1 day");
  fireEvent.click(within(dialog).getByRole("button", { name: "Invite" }));

  const done = await screen.findByRole("dialog", { name: "erin is invited" });
  expect(within(done).getByRole("region", { name: "Join Code" }).textContent).toContain("1 day left");
  expect(api.requests(UserInvite)).toEqual([{ name: "erin", valid_for: 86_400 }]);
});

test("without a Web URL Invite's In a browser is disabled, and says why", async () => {
  const w = world();
  w.webURL = null;
  await asOwner("/access", w);
  await screen.findByRole("link", { name: /^bob/ });

  fireEvent.click(screen.getByRole("button", { name: "Invite" }));
  const dialog = await screen.findByRole("dialog", { name: "Invite" });

  const browser = await within(dialog).findByRole("radio", {
    name: "In a browser",
    description: /^A Password Link names the Web URL, which is not set\./,
  });
  expect(browser.hasAttribute("disabled")).toBe(true);
  await expectAccessible();
});

// Add device.

test("Add device issues a Join Code valid for the time chosen", async () => {
  const api = await asOwner("/access/people/bob");
  const panel = await openPanel("bob");

  fireEvent.click(within(panel).getByRole("button", { name: "Add device" }));
  const dialog = await screen.findByRole("dialog", { name: "Add a device for bob" });
  expect(within(dialog).getByRole("radio", { name: "15 minutes" }).getAttribute("aria-checked")).toBe("true");
  chooseValidFor(dialog, "1 hour");
  fireEvent.click(within(dialog).getByRole("button", { name: "Issue Join Code" }));

  await screen.findByRole("dialog", { name: "The Join Code is ready" });
  expect(api.requests(DeviceAdd)).toEqual([{ user: "bob", valid_for: 3600 }]);
});
