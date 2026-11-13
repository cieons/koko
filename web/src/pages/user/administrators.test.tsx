import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test } from "vitest";

import { SignInList } from "../../api/signin";
import { type User, UserDemote, UserPromote } from "../../api/user";
import { expectAccessible } from "../../test/axe";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { actAs, alerts, renderAt } from "../../test/pages";
import { serveSignIns, signIns } from "../signin/signins.fixture";
import { administered, alice, device, eve, serve, type World, world } from "../space/access.fixture";

// Administrators in a person's panel, against a fake koko server that
// answers each caller as the koko server does: the Owner's Make an
// Administrator and Make a Member, and what an Administrator may do on the
// Owner, another Administrator, a Member and themselves.

/** worldWithAdministrators is world() with eve and frank Administrators, and bob's laptop added by eve's. */
function worldWithAdministrators(): World {
  const w = administered(world());
  w.devices = w.devices.map((d) =>
    d.user === "bob" && d.device === "laptop"
      ? { ...d, added_by: { id: "device_eve_laptop", user: "eve", device: "laptop" } }
      : d,
  );
  return w;
}

/** panelOf renders name's panel as caller, on their laptop, against w with the Sign-ins of signIns(), and is it. */
async function panelOf(name: string, caller: User = alice, w: World = worldWithAdministrators()): Promise<{ api: FakeApi; panel: HTMLElement }> {
  const api = fakeApi();
  actAs(api, caller, w.devices.find((d) => d.user === caller.name && d.device === "laptop"));
  serve(api, w, caller);
  serveSignIns(api, signIns(), caller, undefined, w.users);
  renderAt(`/access/people/${name}`);
  const panel = await screen.findByRole("dialog", { name });
  await within(panel).findByRole("heading", { name: "Devices" });
  await within(panel).findByRole("heading", { name: "Browser Sign-ins" });
  await waitFor(() => expect(within(panel).queryAllByRole("status")).toEqual([]));
  return { api, panel };
}

/** buttons are the names of the buttons in panel, its Close and a choice's Show suggestions aside. */
function buttons(panel: HTMLElement): string[] {
  return within(panel)
    .queryAllByRole("button")
    .map((b) => b.getAttribute("aria-label") ?? b.textContent ?? "")
    .filter((name) => name !== "Close" && name !== "Show suggestions");
}

/** subtitle is the role and date under the panel's title. */
function subtitle(panel: HTMLElement): string | undefined {
  return within(panel).getByRole("heading", { level: 2 }).nextElementSibling?.textContent ?? undefined;
}

// The Owner.

test("the Owner makes a Member an Administrator once a confirmation lists their live devices, who added each, and their Sign-ins", async () => {
  const { api, panel } = await panelOf("bob");
  expect(subtitle(panel)).toBe("Member · Added 2026-11-02");

  fireEvent.click(within(panel).getByRole("button", { name: "Make an Administrator" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Make bob an Administrator" });
  await within(dialog).findByText("laptop, added by eve/laptop");
  expect(dialog.textContent).toContain("bob will see and act in every Space, read every Secret and manage every Member.");
  expect(within(dialog).getByRole("heading", { name: "Live devices" })).toBeDefined();
  expect(within(dialog).getByText("phone, added in a browser or on the koko server's machine")).toBeDefined();
  expect(within(dialog).getByRole("heading", { name: "Live Sign-ins" })).toBeDefined();
  expect(within(dialog).getByText("Firefox on Linux, signed in 2026-11-03")).toBeDefined();
  expect(within(dialog).getByText("Safari on iPhone, signed in 2026-11-03")).toBeDefined();
  // A Revoked device and an ended Sign-in are not listed.
  expect(dialog.textContent).not.toContain("old");
  expect(dialog.textContent).not.toContain("Chrome");
  await expectAccessible();

  fireEvent.click(within(dialog).getByRole("button", { name: "Make an Administrator" }));

  expect(await screen.findByText("bob is now an Administrator.")).toBeDefined();
  expect(api.requests(UserPromote)).toEqual([{ name: "bob" }]);
  await waitFor(() => expect(subtitle(panel)).toBe("Administrator · Added 2026-11-02"));
  expect(within(panel).getByText("Every Space")).toBeDefined();
  expect(within(panel).queryByRole("combobox", { name: "Add to a Space" })).toBeNull();
  expect(within(panel).getByRole("button", { name: "Make a Member" })).toBeDefined();
});

test("the confirmation says so when the Member has no live device and no Sign-in, and Cancel promotes nobody", async () => {
  const { api, panel } = await panelOf("carol");

  fireEvent.click(within(panel).getByRole("button", { name: "Make an Administrator" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Make carol an Administrator" });
  expect(await within(dialog).findByText("No live device.")).toBeDefined();
  expect(within(dialog).getByText("No live Sign-in.")).toBeDefined();
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));

  await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  expect(api.requests(UserPromote)).toEqual([]);
});

test("the confirmation's Make an Administrator waits until the devices and Sign-ins are read", async () => {
  const { api, panel } = await panelOf("bob");
  api.on(SignInList, () => new Promise(() => undefined));

  fireEvent.click(within(panel).getByRole("button", { name: "Make an Administrator" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Make bob an Administrator" });

  expect(within(dialog).getByRole("button", { name: "Make an Administrator" }).hasAttribute("disabled")).toBe(true);
  expect(within(dialog).queryByText("Live devices")).toBeNull();
});

test("the Owner makes an Administrator a Member at once, without asking, and the focus stays on the button", async () => {
  const { api, panel } = await panelOf("eve");
  expect(subtitle(panel)).toBe("Administrator · Added 2026-11-02");
  expect(within(panel).getByText("Every Space")).toBeDefined();
  // Make a Member asks nothing, so the panel says what it ends beside it.
  const consequence = /^As a Member, eve acts only in .* their SSH Sessions to other Hosts end\.$/;
  expect(within(panel).getByText(consequence)).toBeDefined();
  const button = within(panel).getByRole("button", { name: "Make a Member" });
  button.focus();

  fireEvent.click(button);

  expect(await screen.findByText("eve is now a Member.")).toBeDefined();
  expect(screen.queryByRole("alertdialog", { name: /^Make eve/ })).toBeNull();
  expect(api.requests(UserDemote)).toEqual([{ name: "eve" }]);
  await waitFor(() => expect(subtitle(panel)).toBe("Member · Added 2026-11-02"));
  expect(await within(panel).findByRole("combobox", { name: "Add to a Space" })).toBeDefined();
  expect(button.textContent).toBe("Make an Administrator");
  // Make an Administrator asks first, and its confirmation says what follows.
  expect(within(panel).queryByText(consequence)).toBeNull();
  expect(document.activeElement).toBe(button);
  await expectAccessible();
});

test("a refused role change shows why in the panel, and in the confirmation", async () => {
  const { api, panel } = await panelOf("eve");
  api.on(UserDemote, () => {
    throw refusal("owner_only", 403);
  });
  fireEvent.click(within(panel).getByRole("button", { name: "Make a Member" }));
  await waitFor(() =>
    expect(alerts()).toContain(
      "Only the Owner or an Administrator can do this, and only the Owner can do it to the Owner or an Administrator.",
    ),
  );
  expect(subtitle(panel)).toBe("Administrator · Added 2026-11-02");

  fireEvent.click(within(panel).getByRole("button", { name: "Close" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  fireEvent.click(await screen.findByRole("link", { name: /^bob/ }));
  const bob = await screen.findByRole("dialog", { name: "bob" });
  api.on(UserPromote, () => {
    throw refusal("user_removed", 409);
  });
  fireEvent.click(await within(bob).findByRole("button", { name: "Make an Administrator" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Make bob an Administrator" });
  await within(dialog).findByText("laptop, added by eve/laptop");
  fireEvent.click(within(dialog).getByRole("button", { name: "Make an Administrator" }));
  expect(await within(dialog).findByText("That User was Removed.")).toBeDefined();
});

test("the Owner's own panel offers no role action and no Remove", async () => {
  const { panel } = await panelOf("alice");

  expect(subtitle(panel)).toBe("Owner · Added 2026-11-01");
  expect(buttons(panel)).not.toContain("Make an Administrator");
  expect(buttons(panel)).not.toContain("Make a Member");
  expect(buttons(panel)).not.toContain("Remove alice");
});

// An Administrator.

test("an Administrator sees the Owner's role, Spaces, devices and Sign-ins, with no action on them", async () => {
  const { panel } = await panelOf("alice", eve);

  expect(subtitle(panel)).toBe("Owner · Added 2026-11-01");
  expect(within(panel).getByText("Every Space")).toBeDefined();
  expect(await within(panel).findByText("laptop")).toBeDefined();
  expect(within(panel).getByText("Safari on macOS")).toBeDefined();
  expect(within(panel).getByText("No Password is set.")).toBeDefined();
  expect(buttons(panel)).toEqual([]);
  await expectAccessible();
});

test("an Administrator sees another Administrator's panel with no action on them", async () => {
  const { panel } = await panelOf("frank", eve);

  expect(subtitle(panel)).toBe("Administrator · Added 2026-11-03");
  expect(await within(panel).findByText("laptop")).toBeDefined();
  expect(within(panel).getByText("A Password was set on 2026-11-04.")).toBeDefined();
  expect(buttons(panel)).toEqual([]);
});

test("an Administrator acts on a Member as the Owner does, but for the role", async () => {
  const { panel } = await panelOf("bob", eve);

  expect(buttons(panel)).toEqual([
    "Add",
    "Take bob out of ops",
    "Add device",
    "Revoke bob/laptop",
    "Revoke bob/phone",
    "Issue Password Link",
    "Remove Password",
    "Revoke the Sign-in from Firefox on Linux",
    "Revoke the Sign-in from Safari on iPhone",
    "Remove bob",
  ]);
});

test("an Administrator's own panel offers what Account does on their devices and Sign-ins, and no role action or Remove", async () => {
  const { panel } = await panelOf("eve", eve);

  expect(subtitle(panel)).toBe("Administrator · Added 2026-11-02");
  expect(buttons(panel)).toEqual(["Add device", "Revoke laptop", "Issue Password Link"]);
});

test("the Owner acts on an Administrator as on a Member, with Make a Member in place of Add to a Space", async () => {
  const w = worldWithAdministrators();
  w.devices = [...w.devices, device("frank", "phone")];
  const { panel } = await panelOf("frank", alice, w);

  expect(buttons(panel)).toEqual([
    "Add device",
    "Revoke frank/laptop",
    "Revoke frank/phone",
    "Issue Password Link",
    "Remove Password",
    "Make a Member",
    "Remove frank",
  ]);
});
