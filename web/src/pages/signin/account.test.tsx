import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test } from "vitest";

import { AuthPassword, AuthSignOut, SignInList, SignInRevoke } from "../../api/signin";
import { type User, UserCurrent } from "../../api/user";
import { expectAccessible } from "../../test/axe";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { actAs, alerts, preferLanguages, renderAt, frameButton } from "../../test/pages";
import { alice, bob, serve, world } from "../space/access.fixture";
import { firefox, serveSignIns, signIn, signIns, type SignIns } from "./signins.fixture";

// Account's Password and Browser Sign-ins, and Sign out, against a fake
// koko server: for a browser signed in with a Password, through koko web,
// and for the Local caller.

/** password is a new Password that keeps the rule. */
const password = "correct horse battery staple";

/**
 * atAccount renders Account for bob signed in from Firefox on Linux
 * (signin_bob_1), and waits for his Sign-ins in the panel named region.
 */
async function atAccount(s: SignIns = signIns(), caller: User = bob, region = "Browser Sign-ins"): Promise<FakeApi> {
  const api = fakeApi();
  api.on(UserCurrent, () => ({
    user: caller,
    signin: { id: `signin_${caller.name}_1`, client: "Firefox on Linux", ...firefox, started_at: "", expires_at: "" },
  }));
  serve(api, world(), caller);
  serveSignIns(api, s, caller);
  renderAt("/account");
  await waitFor(() => expect(signins(region).length).toBeGreaterThan(0));
  return api;
}

/** signins are the rows of the Browser Sign-ins panel named region, as their text. */
function signins(region = "Browser Sign-ins"): string[] {
  const panel = screen.queryByRole("region", { name: region });
  return panel ? within(panel).queryAllByRole("listitem").map((i) => i.textContent ?? "") : [];
}

function field(name: string): HTMLInputElement {
  return screen.getByLabelText(name, { selector: "input" });
}

function type(name: string, value: string): void {
  fireEvent.change(field(name), { target: { value } });
}

// The Password.

test("Account for a browser signed in with a Password offers to change it, lists its Sign-ins, and Sign out", async () => {
  const api = await atAccount();

  expect(screen.getByRole("heading", { level: 2, name: "Password" })).toBeDefined();
  expect(field("Current Password").getAttribute("autocomplete")).toBe("current-password");
  expect(field("New Password").getAttribute("autocomplete")).toBe("new-password");
  expect(field("New Password, again").getAttribute("autocomplete")).toBe("new-password");
  const [here, phone, ...rest] = signins();
  expect(here).toMatch(/^Firefox on LinuxThis browser203\.0\.113\.7 · Last used .+ · Expires 2026-11-14Revoke$/);
  expect(phone).toMatch(/^Safari on iPhone203\.0\.113\.7 · Last used .+ · Expires 2026-11-14Revoke$/);
  expect(rest).toEqual([]);
  expect(api.requests(SignInList)).toEqual([{ user: "bob" }]);
  expect(screen.getByRole("button", { name: "Sign out" })).toBeDefined();
  await expectAccessible();
});

test("Change Password sends the current Password and the new one, says so, and the other browsers are signed out", async () => {
  const api = await atAccount();
  fireEvent.click(screen.getByRole("switch", { name: "Show ended Sign-ins" }));
  await waitFor(() => expect(signins()).toHaveLength(3));

  type("Current Password", "the current password");
  type("New Password", password);
  type("New Password, again", password);
  fireEvent.click(screen.getByRole("button", { name: "Change Password" }));

  expect(
    await screen.findByText("The Password is changed. The other browsers signed in with it are signed out."),
  ).toBeDefined();
  expect(api.requests(AuthPassword)).toEqual([{ current: "the current password", password }]);
  expect(field("Current Password").value).toBe("");
  expect(field("New Password").value).toBe("");
  await waitFor(() => expect(signins()[1]).toMatch(/^Safari on iPhoneEnded.+: the Password was changed$/));
  expect(signins()[0]).toMatch(/^Firefox on LinuxThis browser/);
  expect(screen.getByRole("switch", { name: "Show ended Sign-ins" })).toHaveProperty("checked", true);
});

test("a change of Password refused too_many_attempts says in how many minutes to try again", async () => {
  const api = await atAccount();
  api.on(AuthPassword, () => {
    throw refusal("too_many_attempts", 429, 300);
  });

  type("Current Password", "the current password");
  type("New Password", password);
  type("New Password, again", password);
  fireEvent.click(screen.getByRole("button", { name: "Change Password" }));

  await waitFor(() => expect(alerts()).toEqual(["Too many failed attempts. Try again in 5 minutes."]));
  expect(field("New Password").value).toBe(password);
});

test("Change Password checks its fields when submitted and sends nothing", async () => {
  const api = await atAccount();

  type("New Password", "too short");
  fireEvent.click(screen.getByRole("button", { name: "Change Password" }));

  expect(await screen.findByText("Enter the Password.")).toBeDefined();
  expect(screen.getByText("The Password must be at least 15 characters long.")).toBeDefined();
  expect(screen.getByText("The two Passwords differ.")).toBeDefined();
  await waitFor(() => expect(document.activeElement).toBe(field("Current Password")));
  expect(api.requests(AuthPassword)).toEqual([]);
  await expectAccessible();
});

test("a wrong current Password is refused in place, the form kept as typed", async () => {
  await atAccount();

  type("Current Password", "not the password");
  type("New Password", password);
  type("New Password, again", password);
  fireEvent.click(screen.getByRole("button", { name: "Change Password" }));

  await waitFor(() => expect(alerts()).toEqual(["The current Password is incorrect."]));
  expect(field("Current Password").value).toBe("not the password");
  expect(field("New Password").value).toBe(password);
  await expectAccessible();
});

test("through koko web Account says where a Password is changed, and offers no form and no Sign out", async () => {
  const api = fakeApi();
  actAs(api, bob);
  serve(api, world(), bob);
  serveSignIns(api, signIns(), bob);
  renderAt("/account");

  expect(
    await screen.findByText(
      "This browser came through koko web, so a Password is changed in a browser signed in with it.",
    ),
  ).toBeDefined();
  expect(screen.queryByLabelText("Current Password")).toBeNull();
  expect(screen.queryByRole("button", { name: "Sign out" })).toBeNull();
  await waitFor(() => expect(signins()).toHaveLength(2));
  expect(signins().some((row) => row.includes("This browser"))).toBe(false);
});

test("for the Local caller Account offers no Password form and no Sign out", async () => {
  const api = fakeApi();
  api.on(UserCurrent, () => ({ user: alice }));
  serve(api, world(), alice);
  serveSignIns(api, signIns(), alice);
  renderAt("/account");

  expect(
    await screen.findByText(
      "This browser acts on the koko server's own machine, so a Password is changed in a browser signed in with it.",
    ),
  ).toBeDefined();
  expect(screen.queryByRole("button", { name: "Sign out" })).toBeNull();
  await waitFor(() => expect(signins()).toHaveLength(1));
  expect(signins()[0]).toMatch(/^Safari on macOS203\.0\.113\.7/);
});

// Browser Sign-ins.

test("Revoke asks first, says what follows, and revokes the Sign-in", async () => {
  const api = await atAccount();

  fireEvent.click(screen.getByRole("button", { name: "Revoke the Sign-in from Safari on iPhone" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Revoke the Sign-in from Safari on iPhone" });
  expect(
    within(dialog).getByText("That browser is signed out at once and must sign in again with the Password."),
  ).toBeDefined();
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Revoke" }));

  expect(await screen.findByText("The Sign-in from Safari on iPhone is revoked.")).toBeDefined();
  expect(api.requests(SignInRevoke)).toEqual([{ id: "signin_bob_2" }]);
  await waitFor(() => expect(signins()).toHaveLength(1));
  await waitFor(() => expect(document.activeElement?.textContent).toBe("Browser Sign-ins"));
});

test("revoking this browser's own Sign-in says it signs this browser out, then goes to Sign in", async () => {
  const api = await atAccount();

  fireEvent.click(screen.getByRole("button", { name: "Revoke the Sign-in from Firefox on Linux" }));
  const dialog = await screen.findByRole("alertdialog");
  expect(
    within(dialog).getByText("This is the Sign-in of this browser. Once it is revoked, this browser is signed out."),
  ).toBeDefined();
  fireEvent.click(within(dialog).getByRole("button", { name: "Revoke" }));

  expect(await screen.findByRole("heading", { level: 1, name: "Sign in" })).toBeDefined();
  expect(api.requests(SignInRevoke)).toEqual([{ id: "signin_bob_1" }]);
  expect(window.location.pathname + window.location.search).toBe("/signin");
});

test("a refused revoke shows in the dialog and reads the Sign-ins again", async () => {
  const api = await atAccount();
  api.on(SignInRevoke, () => {
    throw refusal("unknown_signin", 404);
  });
  const reads = api.requests(SignInList).length;

  fireEvent.click(screen.getByRole("button", { name: "Revoke the Sign-in from Safari on iPhone" }));
  const dialog = await screen.findByRole("alertdialog");
  fireEvent.click(within(dialog).getByRole("button", { name: "Revoke" }));

  await waitFor(() => expect(within(dialog).getByRole("alert").textContent).toBe("No Sign-in has that ID."));
  await waitFor(() => expect(api.requests(SignInList).length).toBeGreaterThan(reads));
});

test("Show ended Sign-ins adds the ended ones, struck through, with when and why they ended", async () => {
  const api = await atAccount();

  fireEvent.click(screen.getByRole("switch", { name: "Show ended Sign-ins" }));

  await waitFor(() => expect(signins()).toHaveLength(3));
  expect(signins()[2]).toBe("Chrome on WindowsEnded203.0.113.7 · Ended 2026-11-05: signed out");
  expect(screen.queryByRole("button", { name: "Revoke the Sign-in from Chrome on Windows" })).toBeNull();
  expect(api.requests(SignInList).at(-1)).toEqual({ user: "bob", all: true });
  await expectAccessible();
});

test("a User signed in from no browser is told so", async () => {
  const api = fakeApi();
  actAs(api, bob);
  serve(api, world(), bob);
  serveSignIns(api, { signins: [] }, bob);
  renderAt("/account");

  expect(await screen.findByText("No browser is signed in as bob.")).toBeDefined();
});

test("a browser koko does not recognise is named Browser", async () => {
  const s = signIns();
  s.signins.push(signIn("bob", 4, {}));
  await atAccount(s);

  expect(signins()[2]).toMatch(/^Browser203\.0\.113\.7 · /);
  expect(screen.getByRole("button", { name: "Revoke the Sign-in from Browser" })).toBeDefined();
});

test("in Chinese each browser is named in Chinese, on Account, in Browser Sign-ins, Revoke and the account panel", async () => {
  preferLanguages("zh-CN");
  const s = signIns();
  s.signins.push(signIn("bob", 4, {}), signIn("bob", 5, { browser: "Firefox" }));
  await atAccount(s, bob, "浏览器登录会话");

  expect(screen.getByText("当前浏览器已凭密码通过 Linux 上的 Firefox 登录。")).toBeDefined();
  expect(signins("浏览器登录会话").map((row) => row.split("203.0.113.7")[0])).toEqual([
    "Linux 上的 Firefox当前浏览器",
    "iPhone 上的 Safari",
    "浏览器",
    "Firefox",
  ]);
  fireEvent.click(screen.getByRole("button", { name: "吊销来自 iPhone 上的 Safari 的登录会话" }));
  const dialog = await screen.findByRole("alertdialog", { name: "吊销来自 iPhone 上的 Safari 的登录会话" });
  fireEvent.click(within(dialog).getByRole("button", { name: "吊销" }));
  expect(await screen.findByText("来自 iPhone 上的 Safari 的登录会话已吊销。")).toBeDefined();

  fireEvent.click(frameButton("账户：bob"));
  const panel = await screen.findByRole("dialog", { name: "账户：bob" });
  expect(within(panel).getByText("通过 Linux 上的 Firefox 登录")).toBeDefined();
});

// Sign out.

test("Sign out on Account ends this browser's Sign-in and goes to Sign in", async () => {
  const api = await atAccount();

  fireEvent.click(screen.getByRole("button", { name: "Sign out" }));

  expect(await screen.findByRole("heading", { level: 1, name: "Sign in" })).toBeDefined();
  expect(api.requests(AuthSignOut)).toEqual([{}]);
  expect(window.location.pathname + window.location.search).toBe("/signin");
});

test("Sign out in the account panel ends this browser's Sign-in, and a failure shows in the panel", async () => {
  const api = await atAccount();
  let unreachable = true;
  api.on(AuthSignOut, () => {
    if (unreachable) {
      unreachable = false;
      throw new TypeError("Failed to fetch"); // the connection fails, as fetch's own error
    }
    return {};
  });

  fireEvent.click(frameButton("Account: bob"));
  const panel = await screen.findByRole("dialog", { name: "Account: bob" });
  fireEvent.click(within(panel).getByRole("button", { name: "Sign out" }));
  await waitFor(() =>
    expect(within(panel).getByRole("alert").textContent).toBe(
      "The koko server could not be reached. Check the connection and try again.",
    ),
  );
  await expectAccessible();

  fireEvent.keyDown(panel, { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Account: bob" })).toBeNull());
  fireEvent.click(frameButton("Account: bob"));
  const again = await screen.findByRole("dialog", { name: "Account: bob" });
  expect(within(again).queryByRole("alert")).toBeNull();

  fireEvent.click(within(again).getByRole("button", { name: "Sign out" }));
  expect(await screen.findByRole("heading", { level: 1, name: "Sign in" })).toBeDefined();
  expect(api.requests(AuthSignOut)).toEqual([{}, {}]);
});

test("the account panel offers no Sign out through koko web", async () => {
  const api = fakeApi();
  actAs(api, bob);
  serve(api, world(), bob);
  serveSignIns(api, signIns(), bob);
  renderAt("/account");

  fireEvent.click(await screen.findByRole("button", { name: "Account: bob" }));
  const panel = await screen.findByRole("dialog", { name: "Account: bob" });
  expect(within(panel).queryByRole("button", { name: "Sign out" })).toBeNull();
});
