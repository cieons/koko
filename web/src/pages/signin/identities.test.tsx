import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";

import { AuthOIDCBegin, type AuthOIDCBeginResponse, IdentityList, IdentityUnlink } from "../../api/identity";
import { AuthLink, AuthMethods, type CurrentSignIn } from "../../api/signin";
import { type User, UserCurrent, UserPromote } from "../../api/user";
import { catalogues, type MessageKey } from "../../i18n/catalogues";
import { leaveFor } from "../../shell/leave";
import { navigate } from "../../shell/router";
import { expectAccessible } from "../../test/axe";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { actAs, alerts, preferLanguages, renderAt } from "../../test/pages";
import { administered, alice, bob, device, eve, identity, passkey, serve, type World, world } from "../space/access.fixture";
import { fakeAuthenticator, serveMethods, settled, waiting } from "./passkeys.fixture";
import { firefox, serveSignIns, signIn, signIns } from "./signins.fixture";

// The Identity Provider in the web UI, against a fake koko server: Continue
// with it on Sign in and a Sign-in Link's page, the refusals its callback
// comes back with, the Linked Identity on Account and in a person's panel,
// Browser Sign-ins made through it, Make an Administrator's list and
// Access's summary. Leaving for the provider is leaveFor, which jsdom
// cannot do, so the tests stand in for it.

vi.mock("../../shell/leave", () => ({ leaveFor: vi.fn() }));

beforeEach(() => {
  vi.mocked(leaveFor).mockClear();
});

/** provider is the Identity Provider's name, as /auth/methods answers it. */
const provider = "Authentik";

/** location is where /auth/oidc/begin sends the browser: the provider's authorization URL. */
const location = "https://id.example.com/authorize?state=STATE";

/** secret is a Sign-in Link's secret, the part of the link after #. */
const secret = "ABCDEFGHIJKLMNOPQRSTUVWXYZ";

/** serveProvider makes /auth/methods name the provider, beside passkeys, and /auth/oidc/begin answer location. */
function serveProvider(api: FakeApi, name: string | null = provider): void {
  serveMethods(api, true, name);
  api.on(AuthOIDCBegin, () => ({ location }));
}

/** message is the English message of an API error code. */
function message(code: string): string {
  return catalogues.en[`error.${code}` as MessageKey];
}

/** refusedCodes are the codes the Identity Provider's callback comes back with. */
const refusedCodes = [
  "signin_expired",
  "provider_refused",
  "provider_failed",
  "provider_unreachable",
  "identity_not_linked",
  "identity_linked",
  "identity_taken",
  "signin_required",
  "link_used",
  "link_invalid",
  "too_many_attempts",
  "internal_error",
];

// Sign in.

/** atSignIn opens Sign in at path, with WebAuthn when passkeys says so, and waits for its form. */
async function atSignIn(path = "/signin", { passkeys = true } = {}): Promise<void> {
  if (passkeys) {
    fakeAuthenticator();
  }
  renderAt(path);
  await screen.findByLabelText(/^(User name|用户名)$/, { selector: "input" });
}

test("Sign in offers Continue with the provider after the passkey, above the divider and the Password", async () => {
  const api = fakeApi();
  serveProvider(api);
  await atSignIn();

  const withPasskey = screen.getByRole("button", { name: "Sign in with a passkey" });
  const continueWith = screen.getByRole("button", { name: "Continue with Authentik" });
  const signIn = screen.getByRole("button", { name: "Sign in" });
  expect(withPasskey.compareDocumentPosition(continueWith) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  expect(continueWith.compareDocumentPosition(screen.getByRole("separator")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  expect(withPasskey.className).not.toContain("button--secondary");
  expect(continueWith.className).toContain("button--secondary");
  expect(signIn.className).toContain("button--secondary");
  expect(api.requests(AuthMethods)).toEqual([{}]);
  expect(api.requests(AuthOIDCBegin)).toEqual([]);
  await expectAccessible();
});

test("without WebAuthn Continue with the provider is the primary action, and Sign in secondary", async () => {
  const api = fakeApi();
  serveProvider(api);
  await atSignIn("/signin", { passkeys: false });

  expect(screen.queryByRole("button", { name: "Sign in with a passkey" })).toBeNull();
  expect(screen.getByRole("button", { name: "Continue with Authentik" }).className).not.toContain("button--secondary");
  expect(screen.getByRole("button", { name: "Sign in" }).className).toContain("button--secondary");
  expect(screen.getByRole("separator")).toBeDefined();
  await expectAccessible();
});

test("Sign in offers no provider where /auth/methods names none", async () => {
  const api = fakeApi();
  serveProvider(api, null);
  await atSignIn();

  expect(screen.getByRole("button", { name: "Sign in with a passkey" })).toBeDefined();
  expect(screen.queryByRole("button", { name: /^Continue with/ })).toBeNull();
});

test.each([
  { path: "/signin?next=%2Faccount%3Fall%3D1", begun: { purpose: "signin", next: "/account?all=1" } },
  { path: "/signin?next=%2F%2Fexample.com", begun: { purpose: "signin", next: "/" } },
  { path: "/signin", begun: { purpose: "signin" } },
])("Continue with the provider at $path begins a sign-in and sends the browser to the provider", async ({ path, begun }) => {
  const api = fakeApi();
  serveProvider(api);
  await atSignIn(path);

  fireEvent.click(screen.getByRole("button", { name: "Continue with Authentik" }));

  await waitFor(() => expect(leaveFor).toHaveBeenCalledWith(location));
  expect(api.requests(AuthOIDCBegin)).toEqual([begun]);
  expect(screen.getByRole("button", { name: "Continue with Authentik" }).getAttribute("data-pending")).toBe("true");
});

test("a begin refused shows why above Sign in, and the next press begins again", async () => {
  const api = fakeApi();
  serveProvider(api);
  api.on(AuthOIDCBegin, () => {
    throw refusal("provider_unreachable", 502);
  });
  await atSignIn();

  fireEvent.click(screen.getByRole("button", { name: "Continue with Authentik" }));

  await waitFor(() => expect(alerts()).toEqual(["The Identity Provider did not answer in time. Try again later."]));
  expect(leaveFor).not.toHaveBeenCalled();
  await expectAccessible();
  api.on(AuthOIDCBegin, () => ({ location }));
  fireEvent.click(screen.getByRole("button", { name: "Continue with Authentik" }));
  await waitFor(() => expect(leaveFor).toHaveBeenCalledWith(location));
  expect(alerts()).toEqual([]);
});

test("a begin answered after Sign in closed sends the browser nowhere", async () => {
  const api = fakeApi();
  serveProvider(api);
  const begun = waiting<AuthOIDCBeginResponse>();
  api.on(AuthOIDCBegin, () => begun.promise);
  await atSignIn();

  fireEvent.click(screen.getByRole("button", { name: "Continue with Authentik" }));
  await waitFor(() => expect(api.requests(AuthOIDCBegin)).toHaveLength(1));
  act(() => navigate("/link")); // Sign in closes
  expect(await screen.findByRole("heading", { level: 1, name: "This Sign-in Link no longer works" })).toBeDefined();
  await settled(begun, { location });

  expect(leaveFor).not.toHaveBeenCalled();
});

test.each(refusedCodes)("Sign in opened with ?refused=%s shows its message above the form", async (code) => {
  const api = fakeApi();
  serveProvider(api);
  await atSignIn(`/signin?refused=${code}`);

  expect(alerts()).toEqual([message(code)]);
});

test("a refusal back from the provider shows until the person tries again, and an unknown code shows nothing", async () => {
  const api = fakeApi();
  serveProvider(api);
  await atSignIn("/signin?refused=identity_not_linked");

  expect(alerts()).toEqual(["No User has this identity linked. Sign in another way, then link it on Account."]);
  await expectAccessible();
  fireEvent.click(screen.getByRole("button", { name: "Continue with Authentik" }));
  await waitFor(() => expect(leaveFor).toHaveBeenCalledOnce());
  expect(alerts()).toEqual([]);
});

test("Sign in ignores a refused code its callback never sends", async () => {
  const api = fakeApi();
  serveProvider(api);
  await atSignIn("/signin?refused=owner_only");

  expect(alerts()).toEqual([]);
});

test("in Chinese Sign in offers 通过 Authentik 继续, and says a refusal back from the provider in Chinese", async () => {
  preferLanguages("zh-CN");
  const api = fakeApi();
  serveProvider(api);
  await atSignIn("/signin?refused=provider_refused");

  expect(screen.getByRole("button", { name: "通过 Authentik 继续" })).toBeDefined();
  expect(alerts()).toEqual(["身份提供方未完成登录。请重试，或改用其他方式登录。"]);
  await expectAccessible();
});

// A Sign-in Link's page.

/** linkFor is /auth/link's answer for bob, with a Linked Identity when linked says so. */
function linkFor(linked = false) {
  return { user: "bob", expires_at: "2026-11-10T12:15:00Z", password_set: false, passkeys: 0, identity_linked: linked };
}

/**
 * atLink opens bob's Sign-in Link at path, with WebAuthn when passkeys says
 * so, against /auth/methods naming name, and waits for whom it is for.
 */
async function atLink(
  api: FakeApi,
  { path = "/link", linked = false, passkeys = true, name = provider as string | null } = {},
): Promise<void> {
  if (passkeys) {
    fakeAuthenticator();
  }
  serveProvider(api, name);
  api.on(AuthLink, () => linkFor(linked));
  renderAt(`${path}#${secret}`);
  await screen.findByText(/^(For bob\.|用户：bob。)$/);
}

test("a Sign-in Link's page offers Continue with the provider after Create a passkey, then Set a Password instead", async () => {
  const api = fakeApi();
  await atLink(api);

  const create = screen.getByRole("button", { name: "Create a passkey" });
  const continueWith = screen.getByRole("button", { name: "Continue with Authentik" });
  const instead = screen.getByRole("button", { name: "Set a Password instead" });
  expect(create.compareDocumentPosition(continueWith) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  expect(continueWith.compareDocumentPosition(instead) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  expect(create.className).not.toContain("button--secondary");
  expect(continueWith.className).toContain("button--secondary");
  expect(api.requests(AuthOIDCBegin)).toEqual([]);
  await expectAccessible();
});

test.each([
  { who: "a User who has a Linked Identity", linked: true, name: provider },
  { who: "a koko server with no provider", linked: false, name: null },
])("a Sign-in Link's page offers no provider for $who", async ({ linked, name }) => {
  await atLink(fakeApi(), { linked, name });

  expect(screen.getByRole("button", { name: "Create a passkey" })).toBeDefined();
  expect(screen.queryByRole("button", { name: /^Continue with/ })).toBeNull();
  expect(screen.getByRole("button", { name: "Set a Password instead" })).toBeDefined();
});

test("Continue with the provider posts the link's secret and sends the browser to the provider", async () => {
  const api = fakeApi();
  await atLink(api);

  fireEvent.click(screen.getByRole("button", { name: "Continue with Authentik" }));

  await waitFor(() => expect(leaveFor).toHaveBeenCalledWith(location));
  expect(api.requests(AuthOIDCBegin)).toEqual([{ purpose: "link", link: secret }]);
});

test("without WebAuthn the provider is the first choice, and Set a Password instead has Back to it", async () => {
  const api = fakeApi();
  await atLink(api, { passkeys: false });

  expect(screen.queryByRole("button", { name: "Create a passkey" })).toBeNull();
  expect(screen.getByRole("button", { name: "Continue with Authentik" }).className).not.toContain("button--secondary");
  fireEvent.click(screen.getByRole("button", { name: "Set a Password instead" }));
  expect(await screen.findByRole("heading", { level: 1, name: "Set a Password" })).toBeDefined();
  fireEvent.click(screen.getByRole("button", { name: "Back" }));
  expect(await screen.findByRole("button", { name: "Continue with Authentik" })).toBeDefined();
});

test("without WebAuthn or a provider, the link's page is Set a Password at once", async () => {
  await atLink(fakeApi(), { passkeys: false, name: null });

  expect(screen.getByRole("heading", { level: 1, name: "Set a Password" })).toBeDefined();
  expect(screen.queryByRole("button", { name: "Back" })).toBeNull();
});

test("a begin answered after the link's page closed sends the browser nowhere, and the link is not sent again", async () => {
  const api = fakeApi();
  await atLink(api);
  const begun = waiting<AuthOIDCBeginResponse>();
  api.on(AuthOIDCBegin, () => begun.promise);
  api.on(AuthLink, () => ({ ...linkFor(), user: "carol" }));

  fireEvent.click(screen.getByRole("button", { name: "Continue with Authentik" }));
  await waitFor(() => expect(api.requests(AuthOIDCBegin)).toHaveLength(1));
  window.location.hash = "SECONDLINKSECONDLINKSECOND"; // another link: the first one's page closes
  expect(await screen.findByText("For carol.")).toBeDefined();
  await settled(begun, { location });

  expect(leaveFor).not.toHaveBeenCalled();
  expect(api.requests(AuthOIDCBegin)).toEqual([{ purpose: "link", link: secret }]);
});

test("a begin refused shows why in place, and a link used meanwhile replaces the choices", async () => {
  const api = fakeApi();
  await atLink(api);
  api.on(AuthOIDCBegin, () => {
    throw refusal("provider_unreachable", 502);
  });

  fireEvent.click(screen.getByRole("button", { name: "Continue with Authentik" }));
  await waitFor(() => expect(alerts()).toEqual(["The Identity Provider did not answer in time. Try again later."]));
  expect(screen.getByRole("button", { name: "Create a passkey" })).toBeDefined();
  await expectAccessible();

  api.on(AuthOIDCBegin, () => {
    throw refusal("link_used", 410);
  });
  fireEvent.click(screen.getByRole("button", { name: "Continue with Authentik" }));
  expect(await screen.findByRole("heading", { level: 1, name: "This Sign-in Link was used already" })).toBeDefined();
  expect(leaveFor).not.toHaveBeenCalled();
});

test("a refusal back from the provider at /link shows why and asks for the link again, calling nothing", async () => {
  const api = fakeApi();
  renderAt("/link?refused=identity_taken");

  expect(await screen.findByText("Open your Sign-in Link again to choose another way.")).toBeDefined();
  expect(screen.getByRole("heading", { level: 1, name: "Set up sign-in" })).toBeDefined();
  expect(alerts()).toEqual(["Another User has already linked this identity."]);
  expect(screen.queryByRole("button", { name: /^Continue with/ })).toBeNull();
  expect(api.calls).toEqual([]);
  await expectAccessible();
});

test.each([
  { code: "link_used", title: "This Sign-in Link was used already" },
  { code: "link_invalid", title: "This Sign-in Link no longer works" },
  { code: "owner_only", title: "This Sign-in Link no longer works" },
])("a link back from the provider with ?refused=$code shows what happened to it", async ({ code, title }) => {
  fakeApi();
  renderAt(`/link?refused=${code}`);

  expect(await screen.findByRole("heading", { level: 1, name: title })).toBeDefined();
  expect(screen.queryByText("Open your Sign-in Link again to choose another way.")).toBeNull();
});

test("a link issued before the Sign-in Link, at /password, offers the provider too", async () => {
  const api = fakeApi();
  await atLink(api, { path: "/password" });

  fireEvent.click(screen.getByRole("button", { name: "Continue with Authentik" }));

  await waitFor(() => expect(leaveFor).toHaveBeenCalledWith(location));
  expect(window.location.pathname).toBe("/password");
  expect(api.requests(AuthOIDCBegin)).toEqual([{ purpose: "link", link: secret }]);
});

test("in Chinese the link's page offers 通过 Authentik 继续", async () => {
  preferLanguages("zh-CN");
  await atLink(fakeApi());

  expect(screen.getByRole("button", { name: "通过 Authentik 继续" })).toBeDefined();
  await expectAccessible();
});

test("in Chinese a refusal back from the provider at /link asks for the link again", async () => {
  preferLanguages("zh-CN");
  fakeApi();
  renderAt("/link?refused=provider_failed");

  expect(await screen.findByText("请重新打开登录链接，以选择其他方式。")).toBeDefined();
  expect(alerts()).toEqual(["无法验证身份提供方的响应。请重试，或改用其他方式登录。"]);
  await expectAccessible();
});

// Account.

/** withIdentity is world() with bob's identity at the provider, last used. */
function withIdentity(w: World = world()): World {
  return { ...w, identities: [identity("bob", "bob@example.com", { last_used_at: "2026-11-07T09:00:00Z" })] };
}

/** signedInVia is bob's Sign-in from Firefox on Linux, made with the method and credential given. */
function signedInVia(method: string, credential?: string): CurrentSignIn {
  return { id: "signin_bob_1", client: "Firefox on Linux", ...firefox, method, credential, started_at: "", expires_at: "" };
}

/** current is me as user.current answers them from w: their passkeys counted and their identity dated. */
function current(w: World, me: User): User {
  const passkeys = w.passkeys.filter((p) => p.user === me.name).length;
  const linked = w.identities.find((i) => i.user === me.name);
  return {
    ...me,
    passkeys: passkeys === 0 ? undefined : passkeys,
    identity_linked_at: linked?.linked_at,
  };
}

/** sectionOf is the region titled name. */
function sectionOf(name: string): HTMLElement {
  return screen.getByRole("region", { name });
}

/**
 * atAccount renders Account at path for me signed in with signin, against
 * w, with /auth/methods naming name, and waits for the passkeys to be read.
 */
async function atAccount(
  w: World,
  {
    me = bob,
    signin = signedInVia("password"),
    name = provider as string | null,
    path = "/account",
  }: { me?: User; signin?: CurrentSignIn; name?: string | null; path?: string } = {},
): Promise<FakeApi> {
  const api = fakeApi();
  api.on(UserCurrent, () => ({ user: current(w, me), signin }));
  serve(api, w, me);
  serveSignIns(api, signIns(), me);
  serveProvider(api, name);
  renderAt(path);
  await screen.findByRole("region", { name: /^(Passkeys|通行密钥)$/ });
  return api;
}

test("Account's Sign-in panel shows the Linked Identity under the provider's name, with Unlink", async () => {
  const api = await atAccount(withIdentity());

  const section = await screen.findByRole("region", { name: "Authentik" });
  expect(await within(section).findByText("Linked as bob@example.com")).toBeDefined();
  expect(within(section).getByText(/^Linked 2026-11-04 · Last used .+$/)).toBeDefined();
  expect(within(section).getByRole("button", { name: "Unlink" })).toBeDefined();
  expect(within(section).queryByRole("button", { name: /^Link/ })).toBeNull();
  expect(api.requests(IdentityList)).toContainEqual({});
  expect(api.requests(AuthMethods)).toEqual([{}]);
  await expectAccessible();
});

test("unlinking the identity this browser signed in through says so, then goes to Sign in", async () => {
  const api = await atAccount(withIdentity(), { signin: signedInVia("identity", "identity_bob") });
  const section = await screen.findByRole("region", { name: "Authentik" });

  fireEvent.click(await within(section).findByRole("button", { name: "Unlink" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Unlink the identity bob@example.com" });
  expect(
    within(dialog).getByText(
      "This browser signed in through it, so this browser is signed out, as is any other browser signed in through it.",
    ),
  ).toBeDefined();
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Unlink" }));

  expect(await screen.findByRole("heading", { level: 1, name: "Sign in" })).toBeDefined();
  expect(api.requests(IdentityUnlink)).toEqual([{}]);
});

test("unlinking the identity from another Sign-in signs out the browsers through it, then offers Link again", async () => {
  const w = withIdentity();
  const api = await atAccount(w);
  const section = await screen.findByRole("region", { name: "Authentik" });

  fireEvent.click(await within(section).findByRole("button", { name: "Unlink" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Unlink the identity bob@example.com" });
  expect(dialog.textContent).toContain("The browsers signed in through it are signed out.");
  expect(dialog.textContent).not.toContain("This browser");
  expect(dialog.textContent).not.toContain("last way");
  fireEvent.click(within(dialog).getByRole("button", { name: "Unlink" }));

  expect(await screen.findByText("The identity bob@example.com is unlinked.")).toBeDefined();
  expect(api.requests(IdentityUnlink)).toEqual([{}]);
  expect(await within(sectionOf("Authentik")).findByRole("button", { name: "Link Authentik" })).toBeDefined();
  await waitFor(() => expect(document.activeElement?.textContent).toBe("Authentik"));
  expect(window.location.pathname).toBe("/account");
});

test("unlinking one's last way to sign in says the next sign-in needs a Sign-in Link", async () => {
  await atAccount(withIdentity(), {
    me: { ...bob, password_set_at: undefined },
    signin: signedInVia("identity", "identity_bob"),
  });
  const section = await screen.findByRole("region", { name: "Authentik" });

  fireEvent.click(await within(section).findByRole("button", { name: "Unlink" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Unlink the identity bob@example.com" });
  expect(dialog.textContent).toContain("This browser signed in through it, so this browser is signed out");
  expect(dialog.textContent).toContain(
    "It is the last way bob has to sign in, so their next sign-in needs a Sign-in Link.",
  );
  await expectAccessible();
});

test("a User with a passkey unlinking their identity is not told it is their last way", async () => {
  const w = { ...withIdentity(), passkeys: [passkey("bob", "iPhone")] };
  await atAccount(w, { me: { ...bob, password_set_at: undefined } });
  const section = await screen.findByRole("region", { name: "Authentik" });

  fireEvent.click(await within(section).findByRole("button", { name: "Unlink" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Unlink the identity bob@example.com" });
  expect(dialog.textContent).not.toContain("last way");
});

test("a Linked Identity counts as another way when one's one passkey is removed", async () => {
  const w = { ...withIdentity(), passkeys: [passkey("bob", "iPhone")] };
  await atAccount(w, { me: { ...bob, password_set_at: undefined } });
  const passkeys = sectionOf("Passkeys");

  fireEvent.click(await within(passkeys).findByRole("button", { name: "Remove the passkey iPhone" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Remove the passkey iPhone" });
  expect(dialog.textContent).not.toContain("last way");
});

test("Link the provider begins linking to this Sign-in and sends the browser to the provider", async () => {
  const api = await atAccount(world());
  const section = await screen.findByRole("region", { name: "Authentik" });

  expect(within(section).queryByRole("listitem")).toBeNull();
  await expectAccessible();
  fireEvent.click(within(section).getByRole("button", { name: "Link Authentik" }));

  await waitFor(() => expect(leaveFor).toHaveBeenCalledWith(location));
  expect(api.requests(AuthOIDCBegin)).toEqual([{ purpose: "link" }]);
  expect(api.requests(IdentityList)).toEqual([]);
});

test("a link begun as Account closed sends the browser nowhere", async () => {
  const api = await atAccount(world());
  const begun = waiting<AuthOIDCBeginResponse>();
  api.on(AuthOIDCBegin, () => begun.promise);

  fireEvent.click(await screen.findByRole("button", { name: "Link Authentik" }));
  await waitFor(() => expect(api.requests(AuthOIDCBegin)).toHaveLength(1));
  fireEvent.click(within(screen.getByRole("navigation", { name: "Main" })).getByRole("link", { name: "Access" }));
  await screen.findByRole("heading", { level: 1, name: "Access" });
  await settled(begun, { location });

  expect(leaveFor).not.toHaveBeenCalled();
});

test("Link refused shows why in the provider's section", async () => {
  const api = await atAccount(world());
  api.on(AuthOIDCBegin, () => {
    throw refusal("provider_unreachable", 502);
  });

  fireEvent.click(await screen.findByRole("button", { name: "Link Authentik" }));

  await waitFor(() =>
    expect(within(sectionOf("Authentik")).getByRole("alert").textContent).toBe(
      "The Identity Provider did not answer in time. Try again later.",
    ),
  );
  expect(leaveFor).not.toHaveBeenCalled();
  await expectAccessible();
});

test("Account back from linking with ?linked=1 says so in a toast, and leaves the URL", async () => {
  await atAccount(withIdentity(), { path: "/account?all=1&linked=1" });

  expect(await screen.findByText("The identity is linked.")).toBeDefined();
  expect(await within(sectionOf("Authentik")).findByText("Linked as bob@example.com")).toBeDefined();
  await waitFor(() => expect(window.location.search).toBe("?all=1"));
  expect(screen.getAllByText("The identity is linked.")).toHaveLength(1);
});

test("Account back from linking with ?refused= shows the refusal in the provider's section, and leaves the URL", async () => {
  await atAccount(world(), { path: "/account?refused=identity_taken" });

  const section = await screen.findByRole("region", { name: "Authentik" });
  expect(within(section).getByRole("alert").textContent).toBe("Another User has already linked this identity.");
  expect(within(section).getByRole("button", { name: "Link Authentik" })).toBeDefined();
  await waitFor(() => expect(window.location.search).toBe(""));
  await expectAccessible();
});

test("without a provider or an identity Account has no section for one", async () => {
  await atAccount(world(), { name: null });

  await waitFor(() => expect(screen.getByRole("button", { name: "Change Password" })).toBeDefined());
  expect(screen.queryByRole("region", { name: "Linked identity" })).toBeNull();
  expect(screen.queryByRole("button", { name: /^Link / })).toBeNull();
});

test("an identity at a provider since removed is still listed, with Unlink, under Linked identity", async () => {
  await atAccount(withIdentity(), { name: null });

  const section = await screen.findByRole("region", { name: "Linked identity" });
  expect(await within(section).findByText("Linked as bob@example.com")).toBeDefined();
  expect(within(section).getByRole("button", { name: "Unlink" })).toBeDefined();
});

test("through koko web the identity is listed with Unlink, and one sentence says where one is linked", async () => {
  const api = fakeApi();
  const w = withIdentity();
  // user.current reads w, so the page sees the identity go once unlinked.
  api.on(UserCurrent, () => ({ user: current(w, bob), device_session: device("bob", "laptop") }));
  serve(api, w, bob);
  serveSignIns(api, signIns(), bob);
  renderAt("/account");

  const section = await screen.findByRole("region", { name: "Linked identity" });
  expect(await within(section).findByText("Linked as bob@example.com")).toBeDefined();
  expect(within(section).getByRole("button", { name: "Unlink" })).toBeDefined();
  expect(
    screen.getByText("Passkeys are added, and an identity is linked, in a browser signed in at the Web URL."),
  ).toBeDefined();
  expect(screen.queryByRole("button", { name: /^Link/ })).toBeNull();
  expect(api.requests(AuthMethods)).toEqual([]); // koko web has no /auth/ endpoints
  await expectAccessible();

  fireEvent.click(within(section).getByRole("button", { name: "Unlink" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Unlink the identity bob@example.com" });
  fireEvent.click(within(dialog).getByRole("button", { name: "Unlink" }));
  expect(await within(section).findByText("No Linked Identity.")).toBeDefined();
  await waitFor(() => expect(document.activeElement?.textContent).toBe("Linked identity"));
  await expectAccessible();
});

test("in Chinese Account names the provider's section and what unlinking does", async () => {
  preferLanguages("zh-CN");
  await atAccount(withIdentity(), { signin: signedInVia("identity", "identity_bob") });

  const section = await screen.findByRole("region", { name: "Authentik" });
  expect(await within(section).findByText("已关联为 bob@example.com")).toBeDefined();
  expect(within(section).getByText(/^2026-11-04 关联 · /)).toBeDefined();
  fireEvent.click(within(section).getByRole("button", { name: "解除关联" }));
  const dialog = await screen.findByRole("alertdialog", { name: "解除关联身份 bob@example.com" });
  expect(dialog.textContent).toContain("当前浏览器通过该身份登录，将随之退出登录");
  await expectAccessible();
});

// Browser Sign-ins.

test.each([
  { language: "en", region: "Browser Sign-ins", via: "through Authentik" },
  { language: "zh-CN", region: "浏览器登录会话", via: "通过 Authentik" },
])("in $language a Browser Sign-in made through the provider names it", async ({ language, region, via }) => {
  preferLanguages(language);
  const api = fakeApi();
  const s = signIns();
  s.signins = [signIn("bob", 1, firefox, { method: "identity", credential: "identity_bob" })];
  api.on(UserCurrent, () => ({ user: bob, signin: signedInVia("identity", "identity_bob") }));
  serve(api, withIdentity(), bob);
  serveSignIns(api, s, bob);
  serveProvider(api);
  renderAt("/account");

  const panel = await screen.findByRole("region", { name: region });
  await waitFor(() => expect(within(panel).queryAllByRole("listitem")).toHaveLength(1));
  await waitFor(() => expect(within(panel).getByRole("listitem").textContent).toContain(via));
  await expectAccessible();
});

// A person's panel, Make an Administrator and Access.

/** asCaller renders path as caller, the Owner unless given, on their laptop, against w. */
async function asCaller(path: string, w: World, caller: User = alice): Promise<FakeApi> {
  const api = fakeApi();
  actAs(api, caller, w.devices.find((d) => d.user === caller.name && d.device === "laptop"));
  serve(api, w, caller);
  serveSignIns(api, signIns(), caller, undefined, w.users);
  renderAt(path);
  await screen.findByRole("heading", { level: 1, name: /^(Access|访问)/ });
  return api;
}

/** linkedByEve is world() with bob's identity, linked through a Sign-in Link eve issued and never used. */
function linkedByEve(): World {
  return { ...world(), identities: [identity("bob", "bob@example.com", { linked_by: "eve" })] };
}

test("the Owner unlinks a User's identity from their panel, and Access no longer says Linked", async () => {
  const api = await asCaller("/access/people/bob", linkedByEve());
  await waitFor(() =>
    expect(screen.getByRole("link", { name: /^bob/ }).textContent).toBe("bob1 Space · 2 devices · Password set · Linked"),
  );
  const panel = await screen.findByRole("dialog", { name: "bob" });
  const section = await within(panel).findByRole("region", { name: "Linked identity" });
  expect(await within(section).findByText("Linked as bob@example.com")).toBeDefined();
  expect(within(section).getByText("Through a Sign-in Link from eve · Linked 2026-11-04 · Never used")).toBeDefined();
  expect(api.requests(IdentityList)).toContainEqual({ user: "bob" });
  await expectAccessible();

  fireEvent.click(within(section).getByRole("button", { name: "Unlink" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Unlink the identity bob@example.com" });
  expect(dialog.textContent).toContain("The browsers signed in through it are signed out.");
  expect(dialog.textContent).not.toContain("last way");
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Unlink" }));

  expect(await screen.findByText("The identity bob@example.com is unlinked.")).toBeDefined();
  expect(api.requests(IdentityUnlink)).toEqual([{ user: "bob" }]);
  await waitFor(() =>
    expect(screen.getByRole("link", { name: /^bob/ }).textContent).toBe("bob1 Space · 2 devices · Password set"),
  );
  expect(await within(section).findByText("No Linked Identity.")).toBeDefined();
  await expectAccessible();
});

test("in a browser signed in, a person's panel names the provider over their identity and their Sign-ins through it", async () => {
  const api = fakeApi();
  const w = linkedByEve();
  const s = signIns();
  s.signins = [signIn("bob", 2, firefox, { method: "identity", credential: "identity_bob" })];
  api.on(UserCurrent, () => ({ user: alice, signin: { ...signedInVia("password"), id: "signin_alice_1" } }));
  serve(api, w);
  serveSignIns(api, s, alice, undefined, w.users);
  serveProvider(api);
  renderAt("/access/people/bob");
  const panel = await screen.findByRole("dialog", { name: "bob" });

  const section = await within(panel).findByRole("region", { name: "Authentik" });
  expect(await within(section).findByText("Linked as bob@example.com")).toBeDefined();
  expect(within(panel).queryByRole("region", { name: "Linked identity" })).toBeNull();
  const signins = within(panel).getByRole("region", { name: "Browser Sign-ins" });
  await waitFor(() => expect(within(signins).getByRole("listitem").textContent).toContain("through Authentik"));
  expect(within(section).queryByRole("button", { name: /^Link/ })).toBeNull();
  expect(api.requests(AuthMethods)).toEqual([{}]);
  await expectAccessible();
});

test("unlinking a person's last way to sign in says their next sign-in needs a Sign-in Link", async () => {
  await asCaller("/access/people/carol", { ...world(), identities: [identity("carol", "carol@example.com")] });
  const panel = await screen.findByRole("dialog", { name: "carol" });
  const section = await within(panel).findByRole("region", { name: "Linked identity" });

  fireEvent.click(await within(section).findByRole("button", { name: "Unlink" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Unlink the identity carol@example.com" });
  expect(dialog.textContent).toContain(
    "It is the last way carol has to sign in, so their next sign-in needs a Sign-in Link.",
  );
  expect(dialog.textContent).not.toContain("This browser");
});

test("a person without a Linked Identity has no section for one, and nothing is read", async () => {
  const api = await asCaller("/access/people/bob", world());
  const panel = await screen.findByRole("dialog", { name: "bob" });
  await within(panel).findByRole("region", { name: "Passkeys" });

  expect(within(panel).queryByRole("region", { name: "Linked identity" })).toBeNull();
  expect(api.requests(IdentityList)).toEqual([]);
});

test("an Administrator sees the Owner's identity without Unlink", async () => {
  const w = administered({ ...world(), identities: [identity("alice", "alice@example.com")] });
  await asCaller("/access/people/alice", w, eve);
  const panel = await screen.findByRole("dialog", { name: "alice" });

  const section = await within(panel).findByRole("region", { name: "Linked identity" });
  expect(await within(section).findByText("Linked as alice@example.com")).toBeDefined();
  expect(within(section).queryByRole("button", { name: "Unlink" })).toBeNull();
  await expectAccessible();
});

test("Make an Administrator lists the User's identity with who issued the link it came through", async () => {
  const api = await asCaller("/access/people/bob", linkedByEve());
  const panel = await screen.findByRole("dialog", { name: "bob" });

  fireEvent.click(await within(panel).findByRole("button", { name: "Make an Administrator" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Make bob an Administrator" });
  await within(dialog).findByRole("heading", { name: "Linked identity" });
  expect(dialog.textContent).toContain("Their live devices, passkeys, Linked Identity and Sign-ins below stay theirs");
  expect(within(dialog).getByText("bob@example.com, linked through a Sign-in Link from eve")).toBeDefined();
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Make an Administrator" }));
  await waitFor(() => expect(api.requests(UserPromote)).toEqual([{ name: "bob" }]));
});

test("Make an Administrator says who linked an identity from their own Sign-in, or that there is none", async () => {
  await asCaller("/access/people/bob", withIdentity());
  const panel = await screen.findByRole("dialog", { name: "bob" });
  fireEvent.click(await within(panel).findByRole("button", { name: "Make an Administrator" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Make bob an Administrator" });
  expect(await within(dialog).findByText("bob@example.com, linked by bob in a signed-in browser")).toBeDefined();
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());

  act(() => navigate("/access/people/carol"));
  const carols = await screen.findByRole("dialog", { name: "carol" });
  fireEvent.click(await within(carols).findByRole("button", { name: "Make an Administrator" }));
  const asked = await screen.findByRole("alertdialog", { name: "Make carol an Administrator" });
  expect(await within(asked).findByText("No Linked Identity.")).toBeDefined();
});

test("in Chinese Access's summary says 已关联身份", async () => {
  preferLanguages("zh-CN");
  await asCaller("/access", linkedByEve());

  await waitFor(() => expect(screen.getByRole("link", { name: /^bob/ }).textContent).toContain("已关联身份"));
  await expectAccessible();
});
