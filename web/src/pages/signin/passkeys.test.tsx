import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test } from "vitest";

import {
  AuthPasskeyAdd,
  AuthPasskeyAddBegin,
  type AuthPasskeyAddBeginResponse,
  AuthPasskeySignIn,
  AuthPasskeySignInBegin,
  PasskeyList,
  PasskeyRemove,
} from "../../api/passkey";
import { AuthLink, AuthMethods, type AuthMethodsResponse, AuthPassword, type CurrentSignIn } from "../../api/signin";
import { type User, UserCurrent, UserPromote } from "../../api/user";
import { navigate } from "../../shell/router";
import { expectAccessible } from "../../test/axe";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { actAs, alerts, preferLanguages, renderAt } from "../../test/pages";
import { administered, alice, bob, eve, passkey, serve, type World, world } from "../space/access.fixture";
import {
  assertion,
  type Authenticator,
  base64url,
  bytes,
  cancel,
  challenge,
  creationOptions,
  fakeAuthenticator,
  registration,
  serveCeremonies,
  serveMethods,
  settled,
  signsInWithoutUserHandle,
  type Waiting,
  waiting,
} from "./passkeys.fixture";
import { firefox, serveSignIns, signIn, signIns } from "./signins.fixture";

// Passkeys in the web UI, against a fake koko server and a fake
// authenticator in place of navigator.credentials: Sign in with a passkey,
// a Sign-in Link's Create a passkey, Account's Sign-in panel, a person's
// passkeys, Make an Administrator's list and Access's summary.

/** secret is a Sign-in Link's secret, the part of the link after #. */
const secret = "ABCDEFGHIJKLMNOPQRSTUVWXYZ";

/** password is a Password that keeps the rule. */
const password = "correct horse battery staple";

/** thisBrowsers is the id of the passkey bob signed this browser in with. */
const thisBrowsers = "passkey_bob_firefox_on_linux";

/**
 * withPasskeys is world() with bob's three passkeys: this browser's, made
 * from his own Sign-in and last used; his phone's, synced, made through a
 * link eve issued and never used; and one made for an earlier host.
 */
function withPasskeys(w: World = world()): World {
  return {
    ...w,
    passkeys: [
      passkey("bob", "Firefox on Linux", { last_used_at: "2026-11-07T09:00:00Z" }),
      passkey("bob", "iPhone", { synced: true, added_by: "eve" }),
      passkey("bob", "Old laptop", { host: "old.example.com" }),
    ],
  };
}

/** viaPasskey is bob's Sign-in from Firefox on Linux, made with the passkey thisBrowsers. */
const viaPasskey: CurrentSignIn = {
  id: "signin_bob_1",
  client: "Firefox on Linux",
  ...firefox,
  method: "passkey",
  credential: thisBrowsers,
  started_at: "",
  expires_at: "",
};

/** signedIn makes user.current answer me with signin, and serves w, the Sign-ins and the passkey ceremonies for me. */
function signedIn(api: FakeApi, w: World, me: () => User = () => bob, signin: CurrentSignIn = viaPasskey): void {
  api.on(UserCurrent, () => ({ user: me(), signin }));
  serve(api, w, me());
  serveSignIns(api, signIns(), me());
  serveCeremonies(api, w, me());
}

/** passkeysIn are the rows of the Passkeys in scope, as their text, once read. */
function passkeysIn(scope: HTMLElement = document.body): string[] {
  const region = within(scope).queryByRole("region", { name: "Passkeys" });
  return region ? within(region).queryAllByRole("listitem").map((i) => i.textContent ?? "") : [];
}

function field(name: string): HTMLInputElement {
  return screen.getByLabelText(name, { selector: "input" });
}

function type(name: string, value: string): void {
  fireEvent.change(field(name), { target: { value } });
}

/** list is the bytes of a buffer or view, to compare. */
function list(b: BufferSource | undefined): number[] {
  if (b === undefined) {
    return [];
  }
  return Array.from(ArrayBuffer.isView(b) ? new Uint8Array(b.buffer, b.byteOffset, b.byteLength) : new Uint8Array(b));
}

/** pending makes the next ceremony of fn's kind wait for the test to settle it. */
function pending(fn: Authenticator["create"] | Authenticator["get"]): Waiting<unknown> {
  const w = waiting<unknown>();
  fn.mockImplementationOnce(() => w.promise);
  return w;
}

/** madeNow is a new passkey's credential as the fake authenticator answers one. */
const madeNow = {
  id: "bmV3",
  rawId: bytes("new").buffer,
  type: "public-key",
  response: { clientDataJSON: bytes("c").buffer, attestationObject: bytes("a").buffer },
};

/** usedNow is a sign-in's credential as the fake authenticator answers one. */
const usedNow = {
  id: "bmV3",
  rawId: bytes("new").buffer,
  type: "public-key",
  response: {
    clientDataJSON: bytes("c").buffer,
    authenticatorData: bytes("a").buffer,
    signature: bytes("s").buffer,
    userHandle: null,
  },
};

// Sign in.

test("Sign in offers a passkey first, then the name and Password with Sign in secondary", async () => {
  fakeAuthenticator();
  const api = fakeApi();
  serveMethods(api);
  renderAt("/signin");

  const withPasskey = await screen.findByRole("button", { name: "Sign in with a passkey" });
  const signIn = screen.getByRole("button", { name: "Sign in" });
  expect(withPasskey.compareDocumentPosition(field("User name")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  expect(signIn.className).toContain("button--secondary");
  expect(withPasskey.className).not.toContain("button--secondary");
  expect(screen.getByRole("separator")).toBeDefined();
  expect(screen.getByText("The Owner or an Administrator can send you a Sign-in Link.")).toBeDefined();
  expect(api.calls.map((c) => [c.path, c.request])).toEqual([[AuthMethods.path, {}]]);
  await expectAccessible();
});

test("Sign in shows Loading until /auth/methods answers, never a passkey action it then takes back", async () => {
  fakeAuthenticator();
  const api = fakeApi();
  const methods = waiting<AuthMethodsResponse>();
  api.on(AuthMethods, () => methods.promise);
  renderAt("/signin");

  expect(await screen.findByRole("heading", { level: 1, name: "Sign in" })).toBeDefined();
  await waitFor(() => expect(api.requests(AuthMethods)).toEqual([{}]));
  expect(screen.queryByRole("button", { name: "Sign in with a passkey" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Sign in" })).toBeNull();
  methods.settle({ passkeys: true, provider: null });

  expect(await screen.findByRole("button", { name: "Sign in with a passkey" })).toBeDefined();
});

test.each([
  { at: "Sign in", path: "/signin" },
  { at: "a Sign-in Link's page", path: `/link#${secret}` },
])("$at offers no passkey where the Web URL's host is an IP address", async ({ path }) => {
  const authenticator = fakeAuthenticator();
  const api = fakeApi();
  serveMethods(api, false);
  api.on(AuthLink, () => linkFor());
  renderAt(path);

  const submit = await screen.findByRole("button", { name: /^(Sign in|Set Password and sign in)$/ });
  expect(submit.className).not.toContain("button--secondary");
  expect(screen.queryByRole("button", { name: "Sign in with a passkey" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Create a passkey" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Back" })).toBeNull();
  expect(screen.queryByRole("separator")).toBeNull();
  expect(api.requests(AuthMethods)).toEqual([{}]);
  expect(authenticator.create).not.toHaveBeenCalled();
  await expectAccessible();
});

test("a failed /auth/methods still offers a passkey, the browser's own refusal showing if it comes", async () => {
  fakeAuthenticator();
  const api = fakeApi();
  api.on(AuthMethods, () => {
    throw refusal("internal_error", 500);
  });
  renderAt("/signin");

  expect(await screen.findByRole("button", { name: "Sign in with a passkey" })).toBeDefined();
});

test("without WebAuthn Sign in offers no passkey, and Sign in stays the primary action", async () => {
  fakeApi();
  renderAt("/signin");

  const signIn = await screen.findByRole("button", { name: "Sign in" });
  expect(screen.queryByRole("button", { name: "Sign in with a passkey" })).toBeNull();
  expect(screen.queryByRole("separator")).toBeNull();
  expect(signIn.className).not.toContain("button--secondary");
});

test("Sign in with a passkey passes the koko server's options as bytes, posts the answer in base64url and follows next", async () => {
  const authenticator = fakeAuthenticator();
  const api = fakeApi();
  signedIn(api, withPasskeys());
  renderAt("/signin?next=%2Faccount");

  fireEvent.click(await screen.findByRole("button", { name: "Sign in with a passkey" }));

  expect(await screen.findByRole("heading", { level: 2, name: "Devices" })).toBeDefined();
  expect(window.location.pathname).toBe("/account");
  expect(api.requests(AuthPasskeySignInBegin)).toEqual([{}]);
  const [options] = authenticator.get.mock.calls[0] ?? [];
  expect(list(options?.publicKey?.challenge)).toEqual(challenge);
  expect(options?.publicKey?.rpId).toBe("koko.example.com");
  expect(options?.publicKey?.userVerification).toBe("required");
  expect(options?.signal).toBeInstanceOf(AbortSignal);
  expect(api.requests(AuthPasskeySignIn)).toEqual([{ flow: "flow_signin", credential: assertion() }]);
});

test("nothing is posted once Sign in has closed while the browser asked for the passkey", async () => {
  const authenticator = fakeAuthenticator();
  const api = fakeApi();
  signedIn(api, withPasskeys());
  const asking = pending(authenticator.get);
  renderAt("/signin");

  fireEvent.click(await screen.findByRole("button", { name: "Sign in with a passkey" }));
  await waitFor(() => expect(authenticator.get).toHaveBeenCalledTimes(1));
  act(() => navigate("/link")); // Sign in closes
  expect(await screen.findByRole("heading", { level: 1, name: "This Sign-in Link no longer works" })).toBeDefined();
  expect(authenticator.get.mock.calls[0]?.[0].signal?.aborted).toBe(true);
  await settled(asking, usedNow);

  expect(api.requests(AuthPasskeySignIn)).toEqual([]);
});

test("a passkey's sign-in answered after Sign in closed goes nowhere, and a toast says this browser is signed in", async () => {
  fakeAuthenticator();
  const api = fakeApi();
  signedIn(api, withPasskeys());
  const answer = waiting<{ user: User }>();
  api.on(AuthPasskeySignIn, () => answer.promise);
  renderAt("/signin?next=%2Faccount");

  fireEvent.click(await screen.findByRole("button", { name: "Sign in with a passkey" }));
  await waitFor(() => expect(api.requests(AuthPasskeySignIn)).toHaveLength(1));
  act(() => navigate("/link"));
  expect(await screen.findByRole("heading", { level: 1, name: "This Sign-in Link no longer works" })).toBeDefined();
  answer.settle({ user: bob });

  expect(await screen.findByText("This browser is signed in as bob.")).toBeDefined();
  expect(window.location.pathname).toBe("/link");
});

test("an authenticator that leaves out the userHandle has it posted empty", async () => {
  signsInWithoutUserHandle(fakeAuthenticator());
  const api = fakeApi();
  signedIn(api, withPasskeys());
  renderAt("/signin");

  fireEvent.click(await screen.findByRole("button", { name: "Sign in with a passkey" }));

  await waitFor(() => expect(api.requests(AuthPasskeySignIn)).toEqual([{ flow: "flow_signin", credential: assertion("") }]));
});

test("a passkey's ceremony cancelled shows nothing, and the next press begins a new one", async () => {
  const authenticator = fakeAuthenticator();
  authenticator.get.mockRejectedValueOnce(cancel());
  const api = fakeApi();
  signedIn(api, withPasskeys());
  renderAt("/signin");
  const withPasskey = await screen.findByRole("button", { name: "Sign in with a passkey" });

  fireEvent.click(withPasskey);
  await waitFor(() => expect(authenticator.get).toHaveBeenCalledTimes(1));
  await waitFor(() => expect(withPasskey.getAttribute("data-pending")).not.toBe("true"));
  expect(alerts()).toEqual([]);
  expect(api.requests(AuthPasskeySignIn)).toEqual([]);
  expect(window.location.pathname).toBe("/signin");

  fireEvent.click(withPasskey);
  expect(await screen.findByRole("button", { name: "Account: bob" })).toBeDefined();
  expect(api.requests(AuthPasskeySignInBegin)).toEqual([{}, {}]);
});

test("a passkey refused, or too many attempts, shows why above Sign in", async () => {
  fakeAuthenticator();
  const api = fakeApi();
  serveCeremonies(api, withPasskeys(), bob);
  api.on(AuthPasskeySignIn, () => {
    throw refusal("passkey_refused", 401);
  });
  renderAt("/signin");

  fireEvent.click(await screen.findByRole("button", { name: "Sign in with a passkey" }));
  await waitFor(() => expect(alerts()).toEqual(["The passkey was not accepted. Try again, or sign in another way."]));
  expect(window.location.pathname).toBe("/signin");
  await expectAccessible();

  api.on(AuthPasskeySignIn, () => {
    throw refusal("too_many_attempts", 429, 120);
  });
  fireEvent.click(screen.getByRole("button", { name: "Sign in with a passkey" }));
  await waitFor(() => expect(alerts()).toEqual(["Too many failed attempts. Try again in 2 minutes."]));
});

test("a ceremony the browser fails shows that the passkey could not be used", async () => {
  fakeAuthenticator().get.mockRejectedValueOnce(new DOMException("The RP ID is not valid.", "SecurityError"));
  const api = fakeApi();
  signedIn(api, withPasskeys());
  renderAt("/signin");

  fireEvent.click(await screen.findByRole("button", { name: "Sign in with a passkey" }));

  await waitFor(() =>
    expect(alerts()).toEqual(["The browser could not complete the passkey. Try again, or use another way."]),
  );
  expect(api.requests(AuthPasskeySignIn)).toEqual([]);
});

test("in Chinese Sign in offers 使用通行密钥登录, and says a refusal in Chinese", async () => {
  preferLanguages("zh-CN");
  fakeAuthenticator();
  const api = fakeApi();
  signedIn(api, withPasskeys());
  api.on(AuthPasskeySignIn, () => {
    throw refusal("passkey_refused", 401);
  });
  renderAt("/signin");

  fireEvent.click(await screen.findByRole("button", { name: "使用通行密钥登录" }));

  await waitFor(() => expect(alerts()).toEqual(["通行密钥未被接受。请重试，或改用其他方式登录。"]));
  expect(screen.getByRole("button", { name: "登录" })).toBeDefined();
  await expectAccessible();
});

// A Sign-in Link's page.

/** linkFor is /auth/link's answer for bob, who holds a Password when passwordSet says so. */
function linkFor(passwordSet = false) {
  return { user: "bob", expires_at: "2026-11-10T12:15:00Z", password_set: passwordSet, passkeys: 0, identity_linked: false };
}

/** atLink opens bob's Sign-in Link at path with WebAuthn, and waits for its choices, Create a passkey named create. */
async function atLink(
  api: FakeApi,
  { path = "/link", passwordSet = false, create = "Create a passkey" } = {},
): Promise<Authenticator> {
  const authenticator = fakeAuthenticator();
  serveMethods(api);
  api.on(AuthLink, () => linkFor(passwordSet));
  renderAt(`${path}#${secret}`);
  await screen.findByRole("button", { name: create });
  return authenticator;
}

test("a Sign-in Link's page offers Create a passkey first, and Set a Password instead", async () => {
  const api = fakeApi();
  await atLink(api);

  expect(screen.getByRole("heading", { level: 1, name: "Set up sign-in" })).toBeDefined();
  expect(screen.getByText("For bob.")).toBeDefined();
  const create = screen.getByRole("button", { name: "Create a passkey" });
  const instead = screen.getByRole("button", { name: "Set a Password instead" });
  expect(create.className).not.toContain("button--secondary");
  expect(instead.className).toContain("button--secondary");
  expect(screen.queryByLabelText("New Password")).toBeNull();
  expect(api.requests(AuthLink)).toEqual([{ link: secret }]);
  expect(api.requests(AuthPasskeyAddBegin)).toEqual([]);
  await expectAccessible();
});

test("Create a passkey makes one through the link, posting the link then the answer, and goes to the first place", async () => {
  const api = fakeApi();
  const w = withPasskeys();
  signedIn(api, w);
  const authenticator = await atLink(api);

  fireEvent.click(screen.getByRole("button", { name: "Create a passkey" }));

  expect(await screen.findByRole("button", { name: "Account: bob" })).toBeDefined();
  await waitFor(() => expect(window.location.pathname).toBe("/hosts"));
  expect(api.requests(AuthPasskeyAddBegin)).toEqual([{ link: secret }]);
  const [options] = authenticator.create.mock.calls[0] ?? [];
  expect(list(options?.publicKey?.challenge)).toEqual(challenge);
  expect(list(options?.publicKey?.user.id)).toEqual(Array.from(bytes("user_2")));
  expect(options?.publicKey?.rp.id).toBe("koko.example.com");
  expect(options?.publicKey?.excludeCredentials?.map((d) => list(d.id))).toEqual([Array.from(bytes("an earlier passkey"))]);
  expect(api.requests(AuthPasskeyAdd)).toEqual([{ flow: "flow_add", credential: registration }]);
  expect(registration.rawId).toBe(base64url("new passkey"));
});

test("the link's secret is not sent once its page has closed while the browser made the passkey", async () => {
  const api = fakeApi();
  const authenticator = await atLink(api);
  serveCeremonies(api, withPasskeys(), bob);
  const making = pending(authenticator.create);
  api.on(AuthLink, () => ({ ...linkFor(), user: "carol" }));

  fireEvent.click(screen.getByRole("button", { name: "Create a passkey" }));
  await waitFor(() => expect(authenticator.create).toHaveBeenCalledTimes(1));
  const signal = authenticator.create.mock.calls[0]?.[0].signal;
  window.location.hash = "SECONDLINKSECONDLINKSECOND"; // another link: the first one's page closes
  expect(await screen.findByText("For carol.")).toBeDefined();
  expect(signal?.aborted).toBe(true);
  await settled(making, madeNow);

  expect(api.requests(AuthPasskeyAdd)).toEqual([]);
  expect(api.requests(AuthPasskeyAddBegin)).toEqual([{ link: secret }]);
});

test("nothing is begun once the page closed while the koko server answered the begin", async () => {
  const api = fakeApi();
  const authenticator = await atLink(api);
  const begun = waiting<AuthPasskeyAddBeginResponse>();
  api.on(AuthPasskeyAddBegin, () => begun.promise);
  api.on(AuthLink, () => ({ ...linkFor(), user: "carol" }));

  fireEvent.click(screen.getByRole("button", { name: "Create a passkey" }));
  await waitFor(() => expect(api.requests(AuthPasskeyAddBegin)).toHaveLength(1));
  window.location.hash = "SECONDLINKSECONDLINKSECOND";
  expect(await screen.findByText("For carol.")).toBeDefined();
  await settled(begun, { flow: "flow_add", options: creationOptions });

  expect(authenticator.create).not.toHaveBeenCalled();
  expect(api.requests(AuthPasskeyAdd)).toEqual([]);
});

test("a passkey made through the link answered after its page closed goes nowhere, and a toast says this browser is signed in", async () => {
  const api = fakeApi();
  await atLink(api);
  serveCeremonies(api, withPasskeys(), bob);
  const answer = waiting<{ user: User; passkey: ReturnType<typeof passkey> }>();
  api.on(AuthPasskeyAdd, () => answer.promise);
  api.on(AuthLink, () => ({ ...linkFor(), user: "carol" }));

  fireEvent.click(screen.getByRole("button", { name: "Create a passkey" }));
  await waitFor(() => expect(api.requests(AuthPasskeyAdd)).toHaveLength(1));
  window.location.hash = "SECONDLINKSECONDLINKSECOND"; // another link: the first one's page closes
  expect(await screen.findByText("For carol.")).toBeDefined();
  answer.settle({ user: bob, passkey: passkey("bob", "Firefox on Linux") });

  expect(await screen.findByText("This browser is signed in as bob.")).toBeDefined();
  expect(window.location.pathname).toBe("/link");
  expect(screen.getByText("For carol.")).toBeDefined();
});

test("a ceremony cancelled on the link's page shows nothing, and a refused one says why in place", async () => {
  const api = fakeApi();
  const authenticator = await atLink(api);
  authenticator.create.mockRejectedValueOnce(cancel());
  serveCeremonies(api, withPasskeys(), bob);

  fireEvent.click(screen.getByRole("button", { name: "Create a passkey" }));
  await waitFor(() => expect(authenticator.create).toHaveBeenCalledTimes(1));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Create a passkey" }).getAttribute("data-pending")).not.toBe("true"),
  );
  expect(alerts()).toEqual([]);
  expect(api.requests(AuthPasskeyAdd)).toEqual([]);

  api.on(AuthPasskeyAdd, () => {
    throw refusal("passkey_refused", 401);
  });
  fireEvent.click(screen.getByRole("button", { name: "Create a passkey" }));
  await waitFor(() => expect(alerts()).toEqual(["The passkey was not accepted. Try again, or sign in another way."]));
  expect(screen.getByRole("button", { name: "Set a Password instead" })).toBeDefined();
  await expectAccessible();

  authenticator.create.mockRejectedValueOnce(new DOMException("excluded", "InvalidStateError"));
  fireEvent.click(screen.getByRole("button", { name: "Create a passkey" }));
  await waitFor(() =>
    expect(alerts()).toEqual(["This device or password manager already holds a passkey of this User."]),
  );
});

test("a Sign-in Link used meanwhile, as the passkey's answer finds it, says so in place of the choices", async () => {
  const api = fakeApi();
  await atLink(api);
  serveCeremonies(api, withPasskeys(), bob);
  api.on(AuthPasskeyAdd, () => {
    throw refusal("link_used", 410);
  });

  fireEvent.click(screen.getByRole("button", { name: "Create a passkey" }));

  expect(await screen.findByRole("heading", { level: 1, name: "This Sign-in Link was used already" })).toBeDefined();
  expect(screen.queryByRole("button", { name: "Create a passkey" })).toBeNull();
});

test("a Sign-in Link that expired before the begin says so in place of the choices", async () => {
  const api = fakeApi();
  const authenticator = await atLink(api);
  api.on(AuthPasskeyAddBegin, () => {
    throw refusal("link_invalid", 404);
  });

  fireEvent.click(screen.getByRole("button", { name: "Create a passkey" }));

  expect(await screen.findByRole("heading", { level: 1, name: "This Sign-in Link no longer works" })).toBeDefined();
  expect(authenticator.create).not.toHaveBeenCalled();
});

test("Set a Password instead opens the Password's fields, saying it replaces theirs, and Back returns to the choices", async () => {
  const api = fakeApi();
  signedIn(api, withPasskeys());
  api.on(AuthPassword, () => ({ user: bob }));
  await atLink(api, { passwordSet: true });

  fireEvent.click(screen.getByRole("button", { name: "Set a Password instead" }));
  expect(await screen.findByRole("heading", { level: 1, name: "Set a Password" })).toBeDefined();
  expect(screen.getByText("A Password set here replaces the current Password of bob.")).toBeDefined();
  expect(screen.queryByRole("button", { name: "Create a passkey" })).toBeNull();
  await expectAccessible();

  fireEvent.click(screen.getByRole("button", { name: "Back" }));
  expect(await screen.findByRole("button", { name: "Create a passkey" })).toBeDefined();
  fireEvent.click(screen.getByRole("button", { name: "Set a Password instead" }));

  type("New Password", password);
  type("New Password, again", password);
  fireEvent.click(screen.getByRole("button", { name: "Set Password and sign in" }));

  expect(await screen.findByRole("button", { name: "Account: bob" })).toBeDefined();
  expect(api.requests(AuthPassword)).toEqual([{ link: secret, password }]);
  expect(api.requests(AuthPasskeyAddBegin)).toEqual([]);
});

test("for a User without a Password, Set a Password instead says nothing is replaced", async () => {
  await atLink(fakeApi());

  fireEvent.click(screen.getByRole("button", { name: "Set a Password instead" }));

  expect(await screen.findByLabelText("New Password", { selector: "input" })).toBeDefined();
  expect(screen.queryByText(/replaces the current Password/)).toBeNull();
});

test("a link issued before the Sign-in Link, at /password, offers the same choices", async () => {
  const api = fakeApi();
  await atLink(api, { path: "/password" });

  expect(window.location.pathname).toBe("/password");
  expect(window.location.hash).toBe("");
  expect(screen.getByRole("button", { name: "Set a Password instead" })).toBeDefined();
});

test("in Chinese the link's page offers 创建通行密钥 and 改为设置密码", async () => {
  preferLanguages("zh-CN");
  await atLink(fakeApi(), { create: "创建通行密钥" });

  expect(screen.getByRole("heading", { level: 1, name: "设置登录方式" })).toBeDefined();
  expect(screen.getByText("用户：bob。")).toBeDefined();
  expect(screen.getByRole("button", { name: "改为设置密码" })).toBeDefined();
  await expectAccessible();
});

// Account.

/**
 * atAccount renders Account for me, signed in with signin, against w, and
 * waits for the passkeys to be listed, under the heading passkeys.
 */
async function atAccount(
  w: World = withPasskeys(),
  {
    me = () => bob,
    signin = viaPasskey,
    passkeys = "Passkeys",
    methods = true,
  }: { me?: () => User; signin?: CurrentSignIn; passkeys?: string; methods?: boolean } = {},
): Promise<FakeApi> {
  const api = fakeApi();
  signedIn(api, w, me, signin);
  serveMethods(api, methods);
  renderAt("/account");
  const region = await screen.findByRole("region", { name: passkeys });
  const own = w.passkeys.filter((p) => p.user === me().name).length;
  await waitFor(() => expect(within(region).queryAllByRole("listitem")).toHaveLength(own));
  if (own === 0) {
    await within(region).findByText(/^(No passkey has been added\.|尚未添加通行密钥。)$/);
  }
  return api;
}

test("Account's Sign-in panel lists the passkeys, one made for another host, Synced, last used or never", async () => {
  fakeAuthenticator();
  const api = await atAccount();

  expect(screen.getByText("This browser is signed in with a passkey from Firefox on Linux.")).toBeDefined();
  expect(screen.getByRole("heading", { level: 2, name: "Sign-in" })).toBeDefined();
  const [here, phone, old] = passkeysIn();
  expect(here).toMatch(/^Firefox on LinuxAdded 2026-11-04 · Last used .+Remove$/);
  expect(phone).toBe("iPhoneSyncedAdded 2026-11-04 · Never usedRemove");
  expect(old).toBe("Old laptopMade for old.example.com · Added 2026-11-04 · Never usedRemove");
  expect(api.requests(PasskeyList)).toContainEqual({ user: "bob" });
  expect(field("Name").value).toBe("Firefox on Linux");
  expect(screen.getByRole("button", { name: "Add a passkey" })).toBeDefined();
  expect(field("Current Password")).toBeDefined();
  await expectAccessible();
});

test("Account names a Sign-in made through the identity provider", async () => {
  await atAccount(withPasskeys(), { signin: { ...viaPasskey, method: "identity", credential: "identity_1" } });

  expect(screen.getByText("This browser is signed in through the identity provider from Firefox on Linux.")).toBeDefined();
});

test("Add a passkey makes one from this browser's Sign-in with the name typed, and lists it", async () => {
  const authenticator = fakeAuthenticator();
  const api = await atAccount();

  type("Name", "  Work laptop ");
  fireEvent.click(screen.getByRole("button", { name: "Add a passkey" }));

  expect(await screen.findByText("The passkey Work laptop is added.")).toBeDefined();
  expect(api.requests(AuthPasskeyAddBegin)).toEqual([{}]);
  expect(list(authenticator.create.mock.calls[0]?.[0].publicKey?.challenge)).toEqual(challenge);
  expect(api.requests(AuthPasskeyAdd)).toEqual([{ flow: "flow_add", credential: registration, name: "Work laptop" }]);
  await waitFor(() => expect(passkeysIn()).toHaveLength(4));
  expect(passkeysIn()[3]).toBe("Work laptopAdded 2026-11-08 · Never usedRemove");
  expect(field("Name").value).toBe("Firefox on Linux");
});

test("Add a passkey checks its name when submitted, and a cancelled ceremony shows nothing", async () => {
  const authenticator = fakeAuthenticator();
  authenticator.create.mockRejectedValueOnce(cancel());
  const api = await atAccount();

  type("Name", " ");
  fireEvent.click(screen.getByRole("button", { name: "Add a passkey" }));
  expect(await screen.findByText("Enter a name.")).toBeDefined();
  type("Name", "x".repeat(65));
  fireEvent.click(screen.getByRole("button", { name: "Add a passkey" }));
  expect(await screen.findByText("The name must be at most 64 characters long.")).toBeDefined();
  expect(api.requests(AuthPasskeyAddBegin)).toEqual([]);

  type("Name", "Work laptop");
  fireEvent.click(screen.getByRole("button", { name: "Add a passkey" }));
  await waitFor(() => expect(authenticator.create).toHaveBeenCalledTimes(1));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Add a passkey" }).getAttribute("data-pending")).not.toBe("true"),
  );
  expect(alerts()).toEqual([]);
  expect(api.requests(AuthPasskeyAdd)).toEqual([]);
  expect(field("Name").value).toBe("Work laptop");
});

test("Add a passkey refused shows why in place, a name the koko server refuses as the passkey's own rule", async () => {
  fakeAuthenticator();
  const api = await atAccount();
  api.on(AuthPasskeyAdd, () => {
    throw refusal("invalid_name");
  });

  type("Name", "");
  fireEvent.click(screen.getByRole("button", { name: "Add a passkey" }));
  expect(await screen.findByText("Enter a name.")).toBeDefined();
  await expectAccessible();

  type("Name", "Work laptop");
  fireEvent.click(screen.getByRole("button", { name: "Add a passkey" }));
  await waitFor(() =>
    expect(alerts()).toEqual(["The name must be 1 to 64 characters long, with no control characters."]),
  );
  await expectAccessible();

  api.on(AuthPasskeyAdd, () => {
    throw refusal("passkey_refused", 401);
  });
  fireEvent.click(screen.getByRole("button", { name: "Add a passkey" }));
  await waitFor(() => expect(alerts()).toEqual(["The passkey was not accepted. Try again, or sign in another way."]));
  expect(field("Name").value).toBe("Work laptop");
  expect(api.requests(AuthPasskeyAddBegin)).toEqual([{}, {}]);
});

test("a passkey whose answer was sent as Account closed is said added in a toast", async () => {
  fakeAuthenticator();
  const api = await atAccount();
  let answer: () => void = () => undefined;
  api.on(
    AuthPasskeyAdd,
    ({ name }) =>
      new Promise((resolve) => {
        answer = () => resolve({ user: bob, passkey: passkey("bob", name ?? "") });
      }),
  );

  fireEvent.click(screen.getByRole("button", { name: "Add a passkey" }));
  await waitFor(() => expect(api.requests(AuthPasskeyAdd)).toHaveLength(1));
  fireEvent.click(within(screen.getByRole("navigation", { name: "Main" })).getByRole("link", { name: "Access" }));
  await screen.findByRole("heading", { level: 1, name: "Access" });
  answer();

  expect(await screen.findByText("The passkey Firefox on Linux is added.")).toBeDefined();
});

test("nothing is posted once Account has closed while the browser made the passkey", async () => {
  const authenticator = fakeAuthenticator();
  const api = await atAccount();
  const making = pending(authenticator.create);

  fireEvent.click(screen.getByRole("button", { name: "Add a passkey" }));
  await waitFor(() => expect(authenticator.create).toHaveBeenCalledTimes(1));
  fireEvent.click(within(screen.getByRole("navigation", { name: "Main" })).getByRole("link", { name: "Access" }));
  await screen.findByRole("heading", { level: 1, name: "Access" });
  expect(authenticator.create.mock.calls[0]?.[0].signal?.aborted).toBe(true);
  await settled(making, madeNow);

  expect(api.requests(AuthPasskeyAdd)).toEqual([]);
});

test.each([
  { language: "en", heading: "Passkeys", line: "Passkeys need a Web URL whose host is a domain name, not an IP address." },
  { language: "zh-CN", heading: "通行密钥", line: "通行密钥要求 Web 地址的主机为域名，而非 IP 地址。" },
])("in $language Account says why no passkey is added where the Web URL's host is an IP address", async ({ language, heading, line }) => {
  preferLanguages(language);
  fakeAuthenticator();
  const api = await atAccount(withPasskeys(), { methods: false, passkeys: heading });

  expect(await screen.findByText(line)).toBeDefined();
  expect(screen.queryByRole("button", { name: /^(Add a passkey|添加通行密钥)$/ })).toBeNull();
  expect(within(screen.getByRole("region", { name: heading })).getAllByRole("listitem")).toHaveLength(3);
  expect(api.requests(AuthMethods)).toEqual([{}]);
  await expectAccessible();
});

test("removing the passkey this browser signed in with says this browser is signed out, then goes to Sign in", async () => {
  fakeAuthenticator();
  const api = await atAccount();

  fireEvent.click(screen.getByRole("button", { name: "Remove the passkey Firefox on Linux" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Remove the passkey Firefox on Linux" });
  expect(
    within(dialog).getByText(
      "This browser signed in with it, so this browser is signed out, as is any other browser signed in with it. The device or password manager that holds it keeps it, so remove it in its own settings too.",
    ),
  ).toBeDefined();
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Remove" }));

  expect(await screen.findByRole("heading", { level: 1, name: "Sign in" })).toBeDefined();
  expect(api.requests(PasskeyRemove)).toEqual([{ id: thisBrowsers }]);
});

test("removing another passkey says the browsers signed in with it are signed out, and lists the rest", async () => {
  const api = await atAccount();

  fireEvent.click(screen.getByRole("button", { name: "Remove the passkey iPhone" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Remove the passkey iPhone" });
  expect(dialog.textContent).toContain(
    "The browsers signed in with it are signed out. The device or password manager that holds it keeps it, so remove it in its own settings too.",
  );
  fireEvent.click(within(dialog).getByRole("button", { name: "Remove" }));

  expect(await screen.findByText("The passkey iPhone is removed.")).toBeDefined();
  expect(api.requests(PasskeyRemove)).toEqual([{ id: "passkey_bob_iphone" }]);
  await waitFor(() => expect(passkeysIn()).toHaveLength(2));
  await waitFor(() => expect(document.activeElement?.textContent).toBe("Passkeys"));
  expect(window.location.pathname).toBe("/account");
});

test("removing one's last way to sign in says the next sign-in needs a Sign-in Link", async () => {
  const w = { ...world(), passkeys: [passkey("bob", "Firefox on Linux")] };
  const me = { ...bob, password_set_at: undefined, passkeys: 1 };
  await atAccount(w, { me: () => me });

  fireEvent.click(screen.getByRole("button", { name: "Remove the passkey Firefox on Linux" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Remove the passkey Firefox on Linux" });
  expect(dialog.textContent).toContain(
    "It is the last way bob has to sign in, so their next sign-in needs a Sign-in Link.",
  );
  expect(dialog.textContent).toContain("This browser signed in with it, so this browser is signed out");
  await expectAccessible();
});

test("a User with a Password removing their one passkey is not told it is their last way", async () => {
  await atAccount({ ...world(), passkeys: [passkey("bob", "iPhone")] });

  fireEvent.click(screen.getByRole("button", { name: "Remove the passkey iPhone" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Remove the passkey iPhone" });
  expect(dialog.textContent).not.toContain("last way");
});

test("through koko web the passkeys are listed with Remove, and one sentence says where one is added", async () => {
  fakeAuthenticator();
  const api = fakeApi();
  const w = withPasskeys();
  actAs(api, bob);
  serve(api, w, bob);
  serveSignIns(api, signIns(), bob);
  renderAt("/account");

  await waitFor(() => expect(passkeysIn()).toHaveLength(3));
  expect(screen.getByText("Passkeys are added, and an identity is linked, in a browser signed in at the Web URL.")).toBeDefined();
  expect(screen.queryByRole("button", { name: "Add a passkey" })).toBeNull();
  expect(screen.queryByLabelText("Name")).toBeNull();
  expect(api.requests(AuthMethods)).toEqual([]); // koko web has no /auth/ endpoints
  expect(screen.getByRole("button", { name: "Remove the passkey iPhone" })).toBeDefined();
  expect(
    screen.getByText("This browser came through koko web, so a Password is changed in a browser signed in with it."),
  ).toBeDefined();
  await expectAccessible();
});

test("for the Local caller the Owner's passkeys are listed, with the same sentence", async () => {
  const api = fakeApi();
  const w = { ...world(), passkeys: [passkey("alice", "MacBook")] };
  api.on(UserCurrent, () => ({ user: alice }));
  serve(api, w, alice);
  serveSignIns(api, signIns(), alice);
  renderAt("/account");

  await waitFor(() => expect(passkeysIn()).toHaveLength(1));
  expect(screen.getByText("Passkeys are added, and an identity is linked, in a browser signed in at the Web URL.")).toBeDefined();
  expect(screen.queryByRole("button", { name: "Add a passkey" })).toBeNull();
});

test("a browser without WebAuthn lists the passkeys without Add a passkey", async () => {
  await atAccount();

  expect(passkeysIn()).toHaveLength(3);
  expect(screen.queryByRole("button", { name: "Add a passkey" })).toBeNull();
  expect(screen.queryByText("Passkeys are added, and an identity is linked, in a browser signed in at the Web URL.")).toBeNull();
});

test("a User without a Password, signed in with a passkey, sets a first one, and is then offered to change it", async () => {
  let me: User = { ...bob, password_set_at: undefined, passkeys: 3 };
  const api = await atAccount(withPasskeys(), { me: () => me });
  api.on(AuthPassword, () => {
    me = { ...me, password_set_at: "2026-11-08T09:00:00Z" };
    return { user: me };
  });

  expect(screen.queryByLabelText("Current Password")).toBeNull();
  await expectAccessible();
  type("New Password", password);
  type("New Password, again", password);
  fireEvent.click(screen.getByRole("button", { name: "Set Password" }));

  expect(await screen.findByText("The Password is set.")).toBeDefined();
  expect(api.requests(AuthPassword)).toEqual([{ password }]);
  expect(await screen.findByLabelText("Current Password", { selector: "input" })).toBeDefined();
  expect(screen.getByRole("button", { name: "Change Password" })).toBeDefined();
});

test.each([
  {
    language: "en",
    region: "Browser Sign-ins",
    rows: ["Firefox on Linuxwith a passkeyThis browser", "Safari on iPhonewith a Password", "Chrome on Windowsthrough the identity provider"],
  },
  {
    language: "zh-CN",
    region: "浏览器登录会话",
    rows: ["Linux 上的 Firefox凭通行密钥当前浏览器", "iPhone 上的 Safari凭密码", "Windows 上的 Chrome通过身份提供方"],
  },
])("in $language each Browser Sign-in names how it signed in", async ({ language, region, rows }) => {
  preferLanguages(language);
  const api = fakeApi();
  const s = signIns();
  s.signins = [
    signIn("bob", 1, firefox, { method: "passkey", credential: thisBrowsers }),
    signIn("bob", 2, { browser: "Safari", system: "iPhone" }),
    signIn("bob", 3, { browser: "Chrome", system: "Windows" }, { method: "identity", credential: "identity_1" }),
  ];
  const w = withPasskeys();
  api.on(UserCurrent, () => ({ user: bob, signin: viaPasskey }));
  serve(api, w, bob);
  serveSignIns(api, s, bob);
  renderAt("/account");

  const panel = await screen.findByRole("region", { name: region });
  await waitFor(() => expect(within(panel).queryAllByRole("listitem")).toHaveLength(3));
  expect(within(panel).getAllByRole("listitem").map((i) => (i.textContent ?? "").split("203.0.113.7")[0])).toEqual(rows);
  await expectAccessible();
});

test("in Chinese Account's Sign-in panel names the passkeys and what removing one does", async () => {
  preferLanguages("zh-CN");
  fakeAuthenticator();
  await atAccount(withPasskeys(), { passkeys: "通行密钥" });

  expect(screen.getByText("当前浏览器（Linux 上的 Firefox）已凭通行密钥登录。")).toBeDefined();
  expect(screen.getByRole("heading", { level: 2, name: "登录方式" })).toBeDefined();
  expect(screen.getByRole("heading", { level: 3, name: "通行密钥" })).toBeDefined();
  expect(screen.getByText("为 old.example.com 创建 · 2026-11-04 添加 · 从未使用")).toBeDefined();
  expect(screen.getByText("已同步")).toBeDefined();
  expect(screen.getByRole("button", { name: "添加通行密钥" })).toBeDefined();
  fireEvent.click(screen.getByRole("button", { name: "移除通行密钥 Firefox on Linux" }));
  const dialog = await screen.findByRole("alertdialog", { name: "移除通行密钥 Firefox on Linux" });
  expect(dialog.textContent).toContain("当前浏览器凭该通行密钥登录，将随之退出登录");
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
  await screen.findByRole("heading", { level: 1, name: "Access" });
  return api;
}

test("Access's summary counts each User's passkeys after the Password", async () => {
  await asCaller("/access", withPasskeys());

  const bobs = await screen.findByRole("link", { name: /^bob/ });
  await waitFor(() => expect(bobs.textContent).toBe("bob1 Space · 2 devices · Password set · 3 passkeys"));
  await expectAccessible();
});

test("a User with one passkey and nothing else is in, with 1 passkey", async () => {
  await asCaller("/access", { ...world(), passkeys: [passkey("carol", "Phone")] });

  await waitFor(() =>
    expect(screen.getByRole("link", { name: /^carol/ }).textContent).toBe("carolNo Space · 1 passkey"),
  );
});

test("the Owner removes a User's passkey from their panel, and Access counts one fewer", async () => {
  const api = await asCaller("/access/people/bob", withPasskeys());
  const panel = await screen.findByRole("dialog", { name: "bob" });
  await waitFor(() => expect(passkeysIn(panel)).toHaveLength(3));
  expect(within(panel).getByRole("heading", { name: "Sign-in" })).toBeDefined();
  expect(passkeysIn(panel)[2]).toBe("Old laptopMade for old.example.com · Added 2026-11-04 · Never usedRemove");
  await expectAccessible();

  fireEvent.click(within(panel).getByRole("button", { name: "Remove the passkey Old laptop" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Remove the passkey Old laptop" });
  expect(dialog.textContent).toContain("The browsers signed in with it are signed out.");
  expect(dialog.textContent).toContain("so remove it in its own settings too.");
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Remove" }));

  expect(await screen.findByText("The passkey Old laptop is removed.")).toBeDefined();
  expect(api.requests(PasskeyRemove)).toEqual([{ id: "passkey_bob_old_laptop" }]);
  await waitFor(() => expect(passkeysIn(panel)).toHaveLength(2));
  await waitFor(() =>
    expect(screen.getByRole("link", { name: /^bob/ }).textContent).toBe("bob1 Space · 2 devices · Password set · 2 passkeys"),
  );
});

test("removing a person's last way to sign in says their next sign-in needs a Sign-in Link", async () => {
  await asCaller("/access/people/carol", { ...world(), passkeys: [passkey("carol", "Phone")] });
  const panel = await screen.findByRole("dialog", { name: "carol" });
  await waitFor(() => expect(passkeysIn(panel)).toHaveLength(1));

  fireEvent.click(within(panel).getByRole("button", { name: "Remove the passkey Phone" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Remove the passkey Phone" });
  expect(dialog.textContent).toContain(
    "It is the last way carol has to sign in, so their next sign-in needs a Sign-in Link.",
  );
  expect(dialog.textContent).not.toContain("This browser");
});

test("an Administrator sees the Owner's passkeys without Remove", async () => {
  const w = administered({ ...world(), passkeys: [passkey("alice", "MacBook")] });
  await asCaller("/access/people/alice", w, eve);
  const panel = await screen.findByRole("dialog", { name: "alice" });

  await waitFor(() => expect(passkeysIn(panel)).toEqual(["MacBookAdded 2026-11-04 · Never used"]));
  expect(within(panel).queryByRole("button", { name: /^Remove/ })).toBeNull();
  await expectAccessible();
});

test("Make an Administrator lists the User's passkeys, each with who made the link it came through", async () => {
  const api = await asCaller("/access/people/bob", withPasskeys());
  const panel = await screen.findByRole("dialog", { name: "bob" });

  fireEvent.click(await within(panel).findByRole("button", { name: "Make an Administrator" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Make bob an Administrator" });
  await within(dialog).findByRole("heading", { name: "Passkeys" });
  expect(dialog.textContent).toContain("Their live devices, passkeys, Linked Identity and Sign-ins below stay theirs");
  expect(within(dialog).getByText("Firefox on Linux, added by bob in a signed-in browser")).toBeDefined();
  expect(within(dialog).getByText("iPhone, made through a Sign-in Link from eve")).toBeDefined();
  expect(api.requests(PasskeyList)).toContainEqual({ user: "bob" });
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Make an Administrator" }));
  await waitFor(() => expect(api.requests(UserPromote)).toEqual([{ name: "bob" }]));
});

test("Make an Administrator says so when the User has no passkey", async () => {
  await asCaller("/access/people/carol", world());
  const panel = await screen.findByRole("dialog", { name: "carol" });

  fireEvent.click(await within(panel).findByRole("button", { name: "Make an Administrator" }));
  const dialog = await screen.findByRole("alertdialog", { name: "Make carol an Administrator" });
  expect(await within(dialog).findByText("No passkey.")).toBeDefined();
});
