import { fireEvent, screen, waitFor } from "@testing-library/react";
import { expect, test } from "vitest";

import { DeviceAdd } from "../../api/device";
import { AuthLink, AuthPassword, AuthSignIn } from "../../api/signin";
import { UserCurrent } from "../../api/user";
import { expectAccessible } from "../../test/axe";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { alerts, renderAt } from "../../test/pages";
import { bob, serve, world } from "../space/access.fixture";
import { nextPage } from "./SignInPage";

// Sign in and Set a Password, outside the frame, against a fake koko
// server's /auth/ endpoints; and the shell sending a browser whose Sign-in
// ended to Sign in.

/** secret is a Password Link's secret, the part of the link after #. */
const secret = "ABCDEFGHIJKLMNOPQRSTUVWXYZ";

/** password is a Password that keeps the rule. */
const password = "correct horse battery staple";

/** signedIn makes user.current answer bob signed in from a browser, and serves his world. */
function signedIn(api: FakeApi): void {
  api.on(UserCurrent, () => ({
    user: bob,
    signin: { id: "signin_1", client: "Firefox on Linux", browser: "Firefox", system: "Linux", started_at: "", expires_at: "" },
  }));
  serve(api, world(), bob);
}

function field(name: string): HTMLInputElement {
  return screen.getByLabelText(name, { selector: "input" });
}

function type(name: string, value: string): void {
  fireEvent.change(field(name), { target: { value } });
}

// Sign in.

test("Sign in is outside the frame, asks for the User name and Password the browser can fill, and calls nothing first", async () => {
  const api = fakeApi();
  renderAt("/signin");

  expect(await screen.findByRole("heading", { level: 1, name: "Sign in" })).toBeDefined();
  expect(screen.queryByRole("button", { name: /^Language:/ })).toBeNull(); // no frame
  expect(screen.queryByRole("navigation", { name: "Main" })).toBeNull();
  expect(field("User name").getAttribute("autocomplete")).toBe("username");
  expect(field("Password").getAttribute("autocomplete")).toBe("current-password");
  expect(field("Password").type).toBe("password");
  expect(screen.getByText("A lost Password is reset by the Owner or an Administrator, who issues a new Password Link.")).toBeDefined();
  await waitFor(() => expect(document.title).toBe("Sign in · koko"));
  expect(api.calls).toEqual([]);
  await expectAccessible();
});

test("Sign in checks its fields when submitted and sends nothing while one is empty", async () => {
  const api = fakeApi();
  renderAt("/signin");
  await screen.findByRole("heading", { level: 1, name: "Sign in" });

  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

  expect(await screen.findByText("Enter the User name.")).toBeDefined();
  expect(screen.getByText("Enter the Password.")).toBeDefined();
  await expectAccessible();

  type("User name", "bob");
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
  await waitFor(() => expect(document.activeElement).toBe(field("Password")));
  expect(screen.queryByText("Enter the User name.")).toBeNull();
  expect(api.calls).toEqual([]);
});

test("Sign in signs in and goes on to the page next names", async () => {
  const api = fakeApi();
  api.on(AuthSignIn, () => ({ user: bob }));
  signedIn(api);
  renderAt("/signin?next=%2Faccount%3Fall%3D1");
  await screen.findByRole("heading", { level: 1, name: "Sign in" });

  type("User name", "bob");
  type("Password", password);
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

  expect(await screen.findByRole("heading", { level: 2, name: "Devices" })).toBeDefined();
  expect(window.location.pathname + window.location.search).toBe("/account?all=1");
  expect(api.requests(AuthSignIn)).toEqual([{ name: "bob", password }]);
  const signin = api.calls.find((c) => c.path === AuthSignIn.path);
  expect(signin?.headers["idempotency-key"]).toBeUndefined();
});

test("Sign in follows next on this origin only, and goes to the first place otherwise", () => {
  expect(nextPage(null)).toBe("/");
  expect(nextPage("/hosts/web1?view=services#top")).toBe("/hosts/web1?view=services#top");
  for (const elsewhere of [
    "//example.com/hosts",
    "https://example.com/hosts",
    "/\\example.com",
    "hosts",
    "javascript:alert(1)",
    "/signin?next=%2Fhosts",
    "/password",
  ]) {
    expect(nextPage(elsewhere)).toBe("/");
  }
});

test("Sign in refused shows why above the fields and keeps what was typed", async () => {
  const api = fakeApi();
  api.on(AuthSignIn, () => {
    throw refusal("wrong_password", 401);
  });
  renderAt("/signin");
  await screen.findByRole("heading", { level: 1, name: "Sign in" });

  type("User name", "bob");
  type("Password", "not the password");
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

  await waitFor(() => expect(alerts()).toEqual(["The User name or the Password is incorrect."]));
  expect(window.location.pathname).toBe("/signin");
  expect(field("User name").value).toBe("bob");
  expect(field("Password").value).toBe("not the password");
  await expectAccessible();
});

test("Sign in refused too_many_attempts says in how many minutes to try again", async () => {
  const api = fakeApi();
  api.on(AuthSignIn, () => {
    throw refusal("too_many_attempts", 429, 541);
  });
  renderAt("/signin");
  await screen.findByRole("heading", { level: 1, name: "Sign in" });

  type("User name", "bob");
  type("Password", password);
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));

  await waitFor(() =>
    expect(alerts()).toEqual(["Too many failed attempts. Try again in 10 minutes."]),
  );
});

test("Sign in's language buttons choose the language, which the browser keeps", async () => {
  fakeApi();
  renderAt("/signin");
  await screen.findByRole("heading", { level: 1, name: "Sign in" });

  fireEvent.click(screen.getByRole("button", { name: "简体中文" }));

  expect(await screen.findByRole("heading", { level: 1, name: "登录" })).toBeDefined();
  expect(screen.getByRole("button", { name: "简体中文" }).getAttribute("aria-pressed")).toBe("true");
  expect(localStorage.getItem("koko.language")).toBe("zh-CN");
  await expectAccessible();
});

// The shell.

test("a read answered signin_required takes the browser to Sign in, which comes back to the page", async () => {
  const api = fakeApi();
  api.on(UserCurrent, () => {
    throw refusal("signin_required", 401);
  });
  renderAt("/hosts?q=web");

  expect(await screen.findByRole("heading", { level: 1, name: "Sign in" })).toBeDefined();
  expect(window.location.pathname + window.location.search).toBe("/signin?next=%2Fhosts%3Fq%3Dweb");
  expect(alerts()).toEqual([]);
});

test("a write answered signin_required takes the browser to Sign in, which comes back to the page", async () => {
  const api = fakeApi();
  signedIn(api);
  api.on(DeviceAdd, () => {
    throw refusal("signin_required", 401);
  });
  renderAt("/account?all=1");
  fireEvent.click(await screen.findByRole("button", { name: "Add device" }));
  fireEvent.click(await screen.findByRole("button", { name: "Issue Join Code" }));

  expect(await screen.findByRole("heading", { level: 1, name: "Sign in" })).toBeDefined();
  expect(window.location.pathname + window.location.search).toBe("/signin?next=%2Faccount%3Fall%3D1");
});

// Set a Password.

/** atLink opens the Password Link of secret for bob, and waits for its form. */
async function atLink(api: FakeApi): Promise<void> {
  api.on(AuthLink, () => ({ user: "bob", expires_at: "2026-11-10T12:15:00Z" }));
  renderAt(`/password#${secret}`);
  await screen.findByRole("heading", { level: 1, name: "Set a Password" });
  await screen.findByLabelText("New Password", { selector: "input" });
}

test("a Password Link's page takes the secret out of the URL at once, and says whom the link is for", async () => {
  const api = fakeApi();
  await atLink(api);

  expect(window.location.hash).toBe("");
  expect(window.location.pathname).toBe("/password");
  expect(api.requests(AuthLink)).toEqual([{ link: secret }]);
  expect(screen.getByText("For bob.")).toBeDefined();
  expect(field("New Password").getAttribute("autocomplete")).toBe("new-password");
  expect(field("New Password, again").getAttribute("autocomplete")).toBe("new-password");
  expect(screen.queryByRole("button", { name: /^Language:/ })).toBeNull(); // no frame
  await expectAccessible();
});

test("a second Password Link opened on the page is read in turn and taken out of the URL", async () => {
  const api = fakeApi();
  await atLink(api);
  api.on(AuthLink, ({ link }) => ({ user: link === "SECONDLINKSECONDLINKSECOND" ? "carol" : "bob", expires_at: "" }));

  type("New Password", "typed for the first link");
  window.location.hash = "SECONDLINKSECONDLINKSECOND";

  expect(await screen.findByText("For carol.")).toBeDefined();
  expect(field("New Password").value).toBe("");
  expect(window.location.hash).toBe("");
  expect(api.requests(AuthLink)).toEqual([{ link: secret }, { link: "SECONDLINKSECONDLINKSECOND" }]);
});

test("the new Password's count runs toward 15 characters, as the koko server counts them", async () => {
  await atLink(fakeApi());

  expect(screen.getByText(/^15 to 128 characters/)).toBeDefined();
  type("New Password", "café au lait");
  expect(screen.getByText("12 of at least 15 characters.")).toBeDefined();
  type("New Password", password);
  expect(screen.getByText("Long enough.")).toBeDefined();
  type("New Password", "x".repeat(129));
  expect(screen.getByText("129 characters, more than the 128 allowed.")).toBeDefined();
});

test("Set a Password checks the rule and the second field when submitted, and sends nothing", async () => {
  const api = fakeApi();
  await atLink(api);

  type("New Password", "too short");
  type("New Password, again", "too short!");
  fireEvent.click(screen.getByRole("button", { name: "Set Password and sign in" }));
  expect(await screen.findByText("The Password must be at least 15 characters long.")).toBeDefined();
  expect(screen.getByText("The two Passwords differ.")).toBeDefined();

  type("New Password", "x".repeat(129));
  type("New Password, again", "x".repeat(129));
  fireEvent.click(screen.getByRole("button", { name: "Set Password and sign in" }));
  expect(await screen.findByText("The Password must be at most 128 characters long.")).toBeDefined();

  expect(api.requests(AuthPassword)).toEqual([]);
  await expectAccessible();
});

test("Set a Password sets it through the link and signs this browser in", async () => {
  const api = fakeApi();
  api.on(AuthPassword, () => ({ user: bob }));
  signedIn(api);
  await atLink(api);

  type("New Password", password);
  type("New Password, again", password);
  fireEvent.click(screen.getByRole("button", { name: "Set Password and sign in" }));

  expect(await screen.findByRole("button", { name: "Account: bob" })).toBeDefined();
  expect(api.requests(AuthPassword)).toEqual([{ link: secret, password }]);
  await waitFor(() => expect(window.location.pathname).toBe("/hosts"));
});

test("a Password refused by the koko server shows why in place, the form kept", async () => {
  const api = fakeApi();
  api.on(AuthPassword, () => {
    throw refusal("invalid_password");
  });
  await atLink(api);

  type("New Password", password);
  type("New Password, again", password);
  fireEvent.click(screen.getByRole("button", { name: "Set Password and sign in" }));

  await waitFor(() =>
    expect(alerts()).toEqual([
      "The Password must be 15 to 128 characters long and must not be the User name. Any characters are allowed.",
    ]),
  );
  expect(field("New Password").value).toBe(password);
});

test("a Password Link used already says so in place of the form, with Go to Sign in", async () => {
  const api = fakeApi();
  api.on(AuthLink, () => {
    throw refusal("link_used", 410);
  });
  renderAt(`/password#${secret}`);

  expect(await screen.findByRole("heading", { level: 1, name: "This Password Link was used already" })).toBeDefined();
  expect(screen.queryByLabelText("New Password")).toBeNull();
  await expectAccessible();
  fireEvent.click(screen.getByRole("link", { name: "Go to Sign in" }));
  expect(await screen.findByRole("heading", { level: 1, name: "Sign in" })).toBeDefined();
});

test("a Password Link that expired as the form was filled says so in place of the form", async () => {
  const api = fakeApi();
  api.on(AuthPassword, () => {
    throw refusal("link_invalid", 404);
  });
  await atLink(api);

  type("New Password", password);
  type("New Password, again", password);
  fireEvent.click(screen.getByRole("button", { name: "Set Password and sign in" }));

  expect(await screen.findByRole("heading", { level: 1, name: "This Password Link no longer works" })).toBeDefined();
  expect(screen.getByRole("link", { name: "Go to Sign in" })).toBeDefined();
});

test("the Password page without a link says the link no longer works, and calls nothing", async () => {
  const api = fakeApi();
  renderAt("/password");

  expect(await screen.findByRole("heading", { level: 1, name: "This Password Link no longer works" })).toBeDefined();
  expect(api.calls).toEqual([]);
});

test("a Password Link that could not be read says why, with Try again", async () => {
  const api = fakeApi();
  let refuse = true;
  api.on(AuthLink, () => {
    if (refuse) {
      throw refusal("too_many_attempts", 429, 60);
    }
    return { user: "bob", expires_at: "" };
  });
  renderAt(`/password#${secret}`);

  await waitFor(() =>
    expect(alerts()).toEqual(["Too many failed attempts. Try again in 1 minute."]),
  );
  await expectAccessible();
  refuse = false;
  fireEvent.click(screen.getByRole("button", { name: "Try again" }));
  expect(await screen.findByLabelText("New Password", { selector: "input" })).toBeDefined();
  expect(api.requests(AuthLink)).toEqual([{ link: secret }, { link: secret }]);
});
