import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { KeyRound, Server, UsersRound } from "lucide-react";
import { useState } from "react";
import { Link } from "react-aria-components";
import { expect, onTestFinished, test, vi } from "vitest";

import { CodeRevoked, DeviceRevoke, type DeviceSession } from "../api/device";
import { CodeNameTaken } from "../api/errors";
import { ServerShow } from "../api/server";
import { type User, UserCurrent } from "../api/user";
import { CodeCookieRequired } from "../api/web";
import { expectAccessible } from "../test/axe";
import { type FakeApi, fakeApi, refusal } from "../test/fakeApi";
import { setSystemScheme, setViewportWidth } from "../test/media";
import { alerts, chooseInFrame, findAlert, frameButton, preferLanguages } from "../test/pages";
import { Page as PageHead } from "../ui/Page";
import { Refusal } from "../ui/states";
import { useCaller } from "./caller";
import { useWrite } from "./data";
import { navigate, type Route } from "./router";
import { Shell } from "./Shell";

// The frame every page sits in, with test pages of its own in the three
// places, against the fake API.

const alice: User = { id: "user_1", name: "alice", role: "owner", created_at: "2026-11-01T09:00:00Z" };
const bob: User = { id: "user_2", name: "bob", role: "member", created_at: "2026-11-02T09:00:00Z" };

const laptop: DeviceSession = { id: "device_1", user: "bob", device: "laptop", created_at: "2026-11-03T09:00:00Z" };

/** asDevice makes user.current answer user on a Device Session of their laptop. */
function asDevice(api: FakeApi, user: User): void {
  api.on(UserCurrent, () => ({ user, device_session: { ...laptop, user: user.name } }));
}

function HostsPage() {
  return (
    <PageHead title="Hosts">
      <p>hosts page</p>
      <Link href="/secrets">secrets from the page</Link>
    </PageHead>
  );
}

function SecretsPage() {
  return (
    <PageHead title="Secrets">
      <p>secrets page</p>
    </PageHead>
  );
}

function AccessPage() {
  return (
    <PageHead title="Access">
      <p>access page</p>
    </PageHead>
  );
}

function AccountPage() {
  return (
    <PageHead title="Account">
      <p>account page</p>
    </PageHead>
  );
}

const routes: Route[] = [
  { path: "/hosts", page: HostsPage, nav: "shell.place.hosts", icon: Server },
  { path: "/secrets", page: SecretsPage, nav: "shell.place.secrets", icon: KeyRound },
  { path: "/access", page: AccessPage, nav: "shell.place.access", icon: UsersRound, ownerOnly: true },
];

/** renderShell renders the shell at path. */
function renderShell(path: string, pages: readonly Route[] = routes) {
  window.history.replaceState(null, "", path);
  return render(<Shell routes={pages} />);
}

/** places are the names of the places the navigation named name links, the current one marked with a *. */
function places(name = "Main"): string[] {
  return within(screen.getByRole("navigation", { name }))
    .getAllByRole("link")
    .map((a) => `${a.textContent}${a.getAttribute("aria-current") === "page" ? "*" : ""}`);
}

/** openAccount opens the account panel and is it. */
async function openAccount(name: string): Promise<HTMLElement> {
  fireEvent.click(frameButton(`Account: ${name}`));
  return screen.findByRole("dialog", { name: `Account: ${name}` });
}

// The account panel: who the web UI acts as, and how this browser reaches koko.

test("the account panel names the User, their role and the device koko web runs on", async () => {
  const api = fakeApi();
  asDevice(api, bob);
  renderShell("/hosts");
  await screen.findByText("hosts page");

  const panel = await openAccount("bob");

  expect(panel.textContent).toBe("bobMemberThrough koko web on laptopAccount");
  await expectAccessible();
  fireEvent.keyDown(panel, { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
});

test("the account panel's Account link goes to Account and closes the panel", async () => {
  const api = fakeApi();
  asDevice(api, bob);
  renderShell("/hosts", [...routes, { path: "/account", page: AccountPage }]);
  await screen.findByText("hosts page");

  const panel = await openAccount("bob");
  fireEvent.click(within(panel).getByRole("link", { name: "Account" }));

  expect(await screen.findByText("account page")).toBeDefined();
  expect(window.location.pathname).toBe("/account");
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
});

test("for a Sign-in the account panel names the browser that signed in", async () => {
  const api = fakeApi();
  api.on(UserCurrent, () => ({
    user: bob,
    signin: { id: "signin_1", client: "Firefox on Linux", browser: "Firefox", system: "Linux", started_at: "", expires_at: "" },
  }));
  renderShell("/hosts");
  await screen.findByText("hosts page");

  expect((await openAccount("bob")).textContent).toBe("bobMemberSigned in from Firefox on LinuxAccountSign out");
});

test("for the Local caller the account panel names the Owner on the koko server's machine", async () => {
  const api = fakeApi();
  api.on(UserCurrent, () => ({ user: alice }));
  renderShell("/hosts");
  await screen.findByText("hosts page");

  expect((await openAccount("alice")).textContent).toBe("aliceOwnerOn the koko server's machineAccount");
});

// The places.

test("the rail links the Owner's three places and marks the one the URL is in", async () => {
  const api = fakeApi();
  asDevice(api, alice);
  renderShell("/hosts");
  await screen.findByText("hosts page");

  expect(places()).toEqual(["Hosts*", "Secrets", "Access"]);
  expect(within(screen.getByRole("banner")).getByRole("navigation", { name: "Main" })).toBeDefined();
  fireEvent.click(within(screen.getByRole("navigation", { name: "Main" })).getByRole("link", { name: "Access" }));

  expect(await screen.findByText("access page")).toBeDefined();
  expect(places()).toEqual(["Hosts", "Secrets", "Access*"]);
});

test("a page beneath a place's path is in that place", async () => {
  const api = fakeApi();
  asDevice(api, alice);
  renderShell("/hosts/web1", [...routes, { path: "/hosts/:host", page: HostsPage }]);
  await screen.findByText("hosts page");

  expect(places()).toEqual(["Hosts*", "Secrets", "Access"]);
});

test("a Member gets no place only the Owner may open, and its path shows Page not found", async () => {
  const api = fakeApi();
  asDevice(api, bob);
  renderShell("/hosts");
  await screen.findByText("hosts page");

  expect(places()).toEqual(["Hosts*", "Secrets"]);
  act(() => navigate("/access"));

  expect(await screen.findByRole("heading", { level: 1, name: "Page not found" })).toBeDefined();
  expect(screen.queryByText("access page")).toBeNull();
});

test("on a phone a top row holds the wordmark, Language, Appearance and the account, and the terminal and the places are in a bar at the foot", async () => {
  setViewportWidth(390);
  const api = fakeApi();
  asDevice(api, alice);
  renderShell("/secrets");
  await screen.findByText("secrets page");

  const top = within(screen.getByRole("banner"));
  expect(top.getAllByRole("link").map((a) => a.textContent)).toEqual(["koko"]);
  expect(top.getAllByRole("button").map((b) => b.getAttribute("aria-label"))).toEqual([
    "Language: English",
    "Appearance: System",
    "Account: alice",
  ]);
  expect(frameButton("Account: alice").textContent).toBe(""); // the initial alone, drawn from data-initial
  expect(top.queryByRole("navigation")).toBeNull();
  expect(screen.queryByRole("button", { name: /side bar$/ })).toBeNull();
  expect(rail()).toBe("none");
  // The terminal's button starts the bar, where the terminal's key bar starts.
  expect(places()).toEqual(["Terminal", "Hosts", "Secrets*", "Access"]);
  const nav = screen.getByRole("navigation", { name: "Main" });
  expect(nav.querySelectorAll("a > svg[aria-hidden='true']")).toHaveLength(4);
  expect((await openAccount("alice")).textContent).toBe("aliceOwnerThrough koko web on laptopAccount");

  act(() => setViewportWidth(1280));
  expect(within(screen.getByRole("banner")).getByRole("navigation", { name: "Main" })).toBeDefined();
  expect(rail()).toBe("open");
});

// The rail: open, folded, and over the page.

/** rail is the room the frame leaves the rail beside the page: open, folded, or none on a phone. */
function rail(): string | null | undefined {
  return document.querySelector("[data-rail]")?.getAttribute("data-rail");
}

/** inert tells whether the page's column takes no input, as while the rail lies over it. */
function inert(): boolean {
  return screen.getByRole("main").closest("[inert]") !== null;
}

test("open, the rail names each place beside its icon and the User beside their initial, with the role", async () => {
  const api = fakeApi();
  asDevice(api, bob);
  renderShell("/hosts");
  await screen.findByText("hosts page");

  expect(rail()).toBe("open");
  const nav = screen.getByRole("navigation", { name: "Main" });
  expect(nav.querySelectorAll("a > svg[aria-hidden='true']")).toHaveLength(2);
  expect(frameButton("Account: bob").textContent).toBe("bobMember");
  const fold = frameButton("Collapse the side bar");
  expect(fold.getAttribute("aria-expanded")).toBe("true");
});

test("from 1280 pixels wide the rail folds to icons, each named as before, and the browser keeps the choice", async () => {
  const api = fakeApi();
  asDevice(api, alice);
  const { unmount } = renderShell("/secrets");
  await screen.findByText("secrets page");

  fireEvent.click(frameButton("Collapse the side bar"));

  expect(rail()).toBe("folded");
  expect(frameButton("Expand the side bar").getAttribute("aria-expanded")).toBe("false");
  const nav = within(screen.getByRole("navigation", { name: "Main" }));
  expect(nav.getAllByRole("link").map((a) => [a.textContent, a.getAttribute("aria-label")])).toEqual([
    ["", "Hosts"],
    ["", "Secrets"],
    ["", "Access"],
  ]);
  expect(nav.getByRole("link", { name: "Secrets", current: "page" })).toBeDefined();
  expect(frameButton("Account: alice").textContent).toBe("");
  expect(frameButton("Language: English")).toBeDefined();
  expect(frameButton("Appearance: System")).toBeDefined();
  expect(inert()).toBe(false);
  expect(localStorage.getItem("koko.rail")).toBe("folded");
  unmount();

  renderShell("/secrets"); // a reload of the page
  await screen.findByText("secrets page");
  expect(rail()).toBe("folded");
  fireEvent.click(frameButton("Expand the side bar"));
  expect(rail()).toBe("open");
  expect(localStorage.getItem("koko.rail")).toBeNull();
});

test("in a browser that refuses storage the rail shows open and still folds, remembering nothing", async () => {
  const refused = () => {
    throw new DOMException("The operation is insecure.", "SecurityError");
  };
  vi.spyOn(Storage.prototype, "getItem").mockImplementation(refused);
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(refused);
  vi.spyOn(Storage.prototype, "removeItem").mockImplementation(refused);
  onTestFinished(() => {
    vi.restoreAllMocks();
  });
  const api = fakeApi();
  asDevice(api, alice);
  renderShell("/hosts");
  await screen.findByText("hosts page");
  expect(rail()).toBe("open");

  fireEvent.click(frameButton("Collapse the side bar"));
  expect(rail()).toBe("folded");
  fireEvent.click(frameButton("Expand the side bar"));
  expect(rail()).toBe("open");
});

/** openOver opens the folded rail over the page, under 1280 pixels wide, and is its fold button. */
function openOver(): HTMLElement {
  const fold = frameButton("Expand the side bar");
  fireEvent.click(fold);
  expect(fold.getAttribute("aria-expanded")).toBe("true");
  expect(fold.getAttribute("aria-label")).toBe("Collapse the side bar");
  return fold;
}

test("under 1280 pixels the rail is folded, and opens over the page, which stays where it was, until Escape", async () => {
  setViewportWidth(1100);
  const api = fakeApi();
  asDevice(api, alice);
  renderShell("/hosts");
  await screen.findByText("hosts page");
  expect(rail()).toBe("folded");

  const fold = openOver();

  expect(rail()).toBe("folded"); // the page keeps its room
  expect(inert()).toBe(true);
  expect(places()).toEqual(["Hosts*", "Secrets", "Access"]);
  expect(frameButton("Account: alice").textContent).toBe("aliceOwner");
  expect(localStorage.getItem("koko.rail")).toBeNull();
  fireEvent.keyDown(document.body, { key: "Escape" });

  expect(fold.getAttribute("aria-expanded")).toBe("false");
  expect(document.activeElement).toBe(fold);
  expect(inert()).toBe(false);

  openOver();
  act(() => setViewportWidth(1280)); // widened past the edge: open beside the page, as the browser keeps it
  expect(rail()).toBe("open");
  expect(inert()).toBe(false);
  act(() => setViewportWidth(1100)); // and narrowed again: folded, no longer over the page
  expect(fold.getAttribute("aria-expanded")).toBe("false");
  expect(inert()).toBe(false);

  openOver();
  act(() => setViewportWidth(390)); // a phone has no rail, and its page takes input
  expect(rail()).toBe("none");
  expect(inert()).toBe(false);
});

test("the skip link moves the focus to the page from under the rail laid over it", async () => {
  setViewportWidth(1100);
  const api = fakeApi();
  asDevice(api, alice);
  renderShell("/hosts");
  await screen.findByText("hosts page");
  const fold = openOver();

  fireEvent.click(screen.getByRole("link", { name: "Skip to main content" }));

  expect(fold.getAttribute("aria-expanded")).toBe("false");
  expect(inert()).toBe(false);
  expect(document.activeElement).toBe(screen.getByRole("main"));
});

test("over the page, the rail closes on a click beside it and on a place chosen, giving the focus back each time", async () => {
  setViewportWidth(1100);
  const api = fakeApi();
  asDevice(api, alice);
  renderShell("/hosts");
  await screen.findByText("hosts page");

  const fold = openOver();
  const backdrop = screen.getByRole("banner").previousElementSibling;
  expect(backdrop?.getAttribute("aria-hidden")).toBe("true");
  fireEvent.click(backdrop!);

  expect(fold.getAttribute("aria-expanded")).toBe("false");
  expect(document.activeElement).toBe(fold);

  openOver();
  const secrets = within(screen.getByRole("navigation", { name: "Main" })).getByRole("link", { name: "Secrets" });
  secrets.focus(); // as a click or a key does in a browser
  fireEvent.click(secrets);

  expect(await screen.findByText("secrets page")).toBeDefined();
  expect(fold.getAttribute("aria-expanded")).toBe("false");
  expect(inert()).toBe(false);
  // The place stays in the folded rail, marked; the focus goes back to the fold button, as on Escape.
  expect(secrets.getAttribute("aria-current")).toBe("page");
  expect(secrets.getAttribute("aria-label")).toBe("Secrets");
  expect(document.activeElement).toBe(fold);
});

test("Escape closes a menu open in the rail over the page and leaves the rail open", async () => {
  setViewportWidth(1100);
  const api = fakeApi();
  asDevice(api, alice);
  renderShell("/hosts");
  await screen.findByText("hosts page");
  const fold = openOver();

  fireEvent.click(frameButton("Language: English"));
  const menu = await screen.findByRole("menu");
  fireEvent.keyDown(menu, { key: "Escape" });

  await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
  expect(fold.getAttribute("aria-expanded")).toBe("true");
  expect(inert()).toBe(true); // what React Aria set and cleared around the menu leaves the page as it was
  await openAccount("alice");
  fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(fold.getAttribute("aria-expanded")).toBe("true");
  expect(inert()).toBe(true);
});

test("/ goes to the first place, keeping the query", async () => {
  const api = fakeApi();
  asDevice(api, alice);

  renderShell("/?view=services");

  expect(await screen.findByText("hosts page")).toBeDefined();
  expect(window.location.pathname + window.location.search).toBe("/hosts?view=services");
});

// Page not found.

test("a path with no page shows Page not found in the frame, with a link to the first place", async () => {
  const api = fakeApi();
  asDevice(api, alice);
  renderShell("/nowhere");

  expect(await screen.findByRole("heading", { level: 1, name: "Page not found" })).toBeDefined();
  expect(screen.getByText("This address leads to no page of this koko server.")).toBeDefined();
  await waitFor(() => expect(document.title).toBe("Page not found · koko"));
  await expectAccessible();
  fireEvent.click(screen.getByRole("link", { name: "Go to Hosts" }));

  expect(await screen.findByText("hosts page")).toBeDefined();
});

test("with no page yet, / and every path show the frame with Page not found", async () => {
  const api = fakeApi();
  asDevice(api, alice);
  renderShell("/", []);

  expect(await screen.findByRole("heading", { level: 1, name: "Page not found" })).toBeDefined();
  expect(screen.queryByRole("navigation")).toBeNull();
  expect(screen.queryByRole("link", { name: /^Go to/ })).toBeNull();
  expect(frameButton("Account: alice")).toBeDefined();
});

// The document's title, the skip link and the focus.

test("the document's title is the page's own name, then koko", async () => {
  const api = fakeApi();
  asDevice(api, alice);
  renderShell("/hosts");
  await screen.findByText("hosts page");
  await waitFor(() => expect(document.title).toBe("Hosts · koko"));

  await chooseInFrame("Language", "简体中文");
  act(() => navigate("/secrets"));

  await screen.findByText("secrets page");
  await waitFor(() => expect(document.title).toBe("Secrets · koko"));
});

test("the skip link is the first stop of Tab and moves the focus to the main region", async () => {
  const api = fakeApi();
  asDevice(api, alice);
  renderShell("/hosts");
  await screen.findByText("hosts page");

  const [first] = focusable();
  expect(first?.textContent).toBe("Skip to main content");
  expect(first?.getAttribute("href")).toBe("#main");
  fireEvent.click(first!);

  expect(document.activeElement).toBe(screen.getByRole("main"));
  expect(window.location.hash).toBe("");
});

/** focusable are the elements Tab stops at, in document order. */
function focusable(): HTMLElement[] {
  return [...document.querySelectorAll<HTMLElement>("a[href], button:not([disabled]), input, [tabindex='0']")];
}

test("after a visit that leaves the focus on nothing, the focus moves to the main region", async () => {
  const api = fakeApi();
  asDevice(api, alice);
  renderShell("/hosts");
  const link = await screen.findByRole("link", { name: "secrets from the page" });
  link.focus(); // as a click or a key does in a browser

  fireEvent.click(link); // the link goes with the page it was on

  await screen.findByText("secrets page");
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("main")));
});

test("after a visit from a place in the rail, the focus stays on the place", async () => {
  const api = fakeApi();
  asDevice(api, alice);
  renderShell("/hosts");
  await screen.findByText("hosts page");
  const secrets = within(screen.getByRole("navigation", { name: "Main" })).getByRole("link", { name: "Secrets" });
  secrets.focus();

  fireEvent.click(secrets);

  await screen.findByText("secrets page");
  expect(document.activeElement).toBe(secrets);
});

// Language and Appearance.

test("the language follows the browser's preferred languages: Chinese of any region gives Simplified Chinese", async () => {
  preferLanguages("zh-TW", "en");
  const api = fakeApi();
  asDevice(api, bob);

  renderShell("/hosts");

  await screen.findByText("hosts page");
  await waitFor(() => expect(document.documentElement.lang).toBe("zh-CN"));
  expect(places("主导航")).toEqual(["主机*", "机密"]);
  fireEvent.click(frameButton("语言：简体中文"));
  expect(await screen.findByRole("menuitemradio", { name: "简体中文", checked: true })).toBeDefined();
});

test.each([
  [["fr-FR", "zh-HK", "en"], "zh-CN"],
  [["fr-FR", "en-US", "zh-CN"], "en"],
  [["fr-FR", "de"], "en"],
])("a preferred language the web UI does not ship is passed over, and none gives English: %j", async (preferred, want) => {
  preferLanguages(...preferred);
  const api = fakeApi();
  asDevice(api, bob);

  renderShell("/hosts");

  await screen.findByText("hosts page");
  await waitFor(() => expect(document.documentElement.lang).toBe(want));
});

test("the Language menu overrides the browser's language, and the browser keeps the choice", async () => {
  preferLanguages("en-GB");
  const api = fakeApi();
  asDevice(api, bob);
  const { unmount } = renderShell("/hosts");
  await screen.findByText("hosts page");
  expect(frameButton("Language: English")).toBeDefined();

  await chooseInFrame("Language", "简体中文");

  expect(frameButton("语言：简体中文")).toBeDefined();
  expect(frameButton("账户：bob")).toBeDefined();
  expect(localStorage.getItem("koko.language")).toBe("zh-CN");
  unmount();

  renderShell("/hosts"); // a reload of the page
  await screen.findByText("hosts page");
  await waitFor(() => expect(document.documentElement.lang).toBe("zh-CN"));
  expect(places("主导航")).toEqual(["主机*", "机密"]);
});

test("the Appearance menu chooses Light, Dark or System, which the browser keeps", async () => {
  const api = fakeApi();
  asDevice(api, bob);
  renderShell("/hosts");
  await screen.findByText("hosts page");
  const html = document.documentElement;
  expect([html.className, html.dataset.theme]).toEqual(["light", "light"]);

  fireEvent.click(frameButton("Appearance: System"));
  expect(await screen.findByRole("menuitemradio", { name: "System", checked: true })).toBeDefined();
  await expectAccessible();
  fireEvent.click(screen.getByRole("menuitemradio", { name: "Dark" }));

  await waitFor(() => expect([html.className, html.dataset.theme]).toEqual(["dark", "dark"]));
  expect(localStorage.getItem("koko.theme")).toBe("dark");
  expect(frameButton("Appearance: Dark")).toBeDefined();
  act(() => setSystemScheme("light")); // Dark holds whatever the system's mode
  expect(html.dataset.theme).toBe("dark");

  await chooseInFrame("Appearance", "System");
  expect(localStorage.getItem("koko.theme")).toBe("system");
  expect([html.className, html.dataset.theme]).toEqual(["light", "light"]);
  act(() => setSystemScheme("dark"));
  expect([html.className, html.dataset.theme]).toEqual(["dark", "dark"]);
});

// Accessibility of the frame, the account panel and both menus.

test.each([
  ["light", 1280, "open"],
  ["dark", 1280, "open"],
  ["light", 1280, "folded"],
  ["dark", 1280, "folded"],
  ["light", 1100, "over"],
  ["dark", 1100, "over"],
  ["light", 390, "none"],
  ["dark", 390, "none"],
] as const)("the frame, the account panel and both menus pass axe, %s, %i pixels wide, the rail %s", async (mode, width, state) => {
  setViewportWidth(width);
  localStorage.setItem("koko.theme", mode);
  if (state === "folded") {
    localStorage.setItem("koko.rail", "folded");
  }
  const api = fakeApi();
  asDevice(api, alice);
  renderShell("/hosts");
  await screen.findByText("hosts page");
  expect(document.documentElement.dataset.theme).toBe(mode);
  if (state === "over") {
    openOver();
  }
  expect(rail()).toBe(state === "over" ? "folded" : state);
  await expectAccessible();

  for (const button of ["Language: English", "Appearance: " + (mode === "light" ? "Light" : "Dark")]) {
    fireEvent.click(frameButton(button));
    const menu = await screen.findByRole("menu");
    await expectAccessible();
    fireEvent.keyDown(menu, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
  }
  await openAccount("alice");
  await expectAccessible();
});

// The shell's notices.

test("when koko web stops the frame says to run it again, until a call gets through", async () => {
  const api = fakeApi();
  asDevice(api, alice);
  renderShell("/hosts");
  await screen.findByText("hosts page");

  api.drop();
  fireEvent.focus(window);

  const stopped = await findAlert();
  expect(stopped).toBe("koko web is not running Run koko web again, then return to this page.");
  expect(screen.getByText("hosts page")).toBeDefined();
  // At the head of the page's column, in the room beside the rail.
  const notice = screen.getByText("koko web is not running").closest("[role='alert']");
  const main = screen.getByRole("main");
  expect(notice?.parentElement).toBe(main.parentElement);
  expect(notice?.nextElementSibling).toBe(main);
  await expectAccessible();

  api.drop(0);
  fireEvent.focus(window);
  await waitFor(() => expect(alerts()).toEqual([]));
});

/** A page that writes, as a capability's form does, and shows its refusal in place. */
function WritePage() {
  const caller = useCaller();
  const revoke = useWrite(DeviceRevoke);
  const [done, setDone] = useState(false);
  const submit = (device: string) => void revoke.submit({ device }).then((r) => setDone(r !== undefined));
  return (
    <>
      <button type="button" onClick={() => submit(caller.deviceSession?.device ?? "")}>
        revoke this device
      </button>
      <button type="button" onClick={() => submit("phone")}>
        revoke phone
      </button>
      <Refusal error={revoke.error} />
      {done && <p>revoked</p>}
    </>
  );
}

const writeRoutes: Route[] = [{ path: "/write", page: WritePage }];

test("a refusal shows koko web runs again, so the notice that it stopped goes", async () => {
  const api = fakeApi();
  asDevice(api, alice);
  api.on(DeviceRevoke, () => {
    throw refusal(CodeNameTaken, 409);
  });
  renderShell("/write", writeRoutes);
  api.drop(2);
  fireEvent.click(await screen.findByRole("button", { name: "revoke phone" }));
  await screen.findByText("koko web is not running");

  api.drop(0);
  fireEvent.click(screen.getByRole("button", { name: "revoke phone" }));

  await waitFor(() => expect(alerts()).toEqual(["This name is already in use."]));
});

test("when the Device Session is Revoked the frame says so, for good", async () => {
  const api = fakeApi();
  asDevice(api, bob);
  api.on(DeviceRevoke, () => {
    throw refusal(CodeRevoked, 401);
  });
  renderShell("/write", writeRoutes);

  fireEvent.click(await screen.findByRole("button", { name: "revoke phone" }));

  const notice = "The Device Session of this device was Revoked";
  await waitFor(() => expect(alerts()[0]).toContain(notice));
  fireEvent.focus(window); // a read that still answers, as one sent before the revocation would
  await waitFor(() => expect(api.calls.filter((c) => c.path === UserCurrent.path)).toHaveLength(2));
  expect(alerts()[0]).toContain(notice);
});

test.each([
  ["this device's Device Session", "revoke this device", true],
  ["another Device Session", "revoke phone", false],
])("revoking %s from the web UI says so only for this device's", async (_, button, notice) => {
  const api = fakeApi();
  asDevice(api, bob);
  const sessions: Record<string, DeviceSession> = {
    laptop,
    phone: { id: "device_2", user: "bob", device: "phone", created_at: "2026-11-04T09:00:00Z" },
  };
  api.on(DeviceRevoke, ({ device }) => ({ device_session: { ...sessions[device]!, revoked_at: "2026-11-06T09:00:00Z" } }));
  renderShell("/write", writeRoutes);

  fireEvent.click(await screen.findByRole("button", { name: button }));

  await screen.findByText("revoked");
  if (notice) {
    expect(alerts()[0]).toContain("The Device Session of this device was Revoked");
  } else {
    expect(alerts()).toEqual([]);
  }
});

test("when the browser's cookie is no longer valid, the frame says to open koko web's address", async () => {
  const api = fakeApi();
  let ended = false;
  api.on(UserCurrent, () => {
    if (ended) {
      throw refusal(CodeCookieRequired, 401);
    }
    return { user: alice };
  });
  // koko web refuses every call without its cookie, the Owner's server.show too.
  api.on(ServerShow, () => {
    throw refusal(ended ? CodeCookieRequired : "internal_error", ended ? 401 : 500);
  });
  renderShell("/hosts");
  await screen.findByText("hosts page");

  ended = true; // koko web ran again over the Unix socket, with a new cookie
  fireEvent.focus(window);

  expect(await findAlert()).toBe(
    "This browser no longer holds a valid cookie for this page. Open the address that koko web printed.",
  );
});

test("until user.current answers the frame shows no page; if it fails, why, with Try again", async () => {
  const api = fakeApi();
  let answer = false;
  api.on(UserCurrent, () => {
    if (!answer) {
      throw refusal("internal_error", 500);
    }
    return { user: alice };
  });
  renderShell("/hosts");

  expect(await findAlert()).toBe("The koko server failed unexpectedly. Its log records the cause.");
  expect(screen.queryByText("hosts page")).toBeNull();
  answer = true;
  fireEvent.click(screen.getByRole("button", { name: "Try again" }));

  expect(await screen.findByText("hosts page")).toBeDefined();
  expect(alerts()).toEqual([]);
});
