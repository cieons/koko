import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { useState } from "react";
import { expect, onTestFinished, test, vi } from "vitest";

import type { Op } from "../api/api";
import { CodeRevoked, DeviceRevoke, type DeviceSession } from "../api/device";
import { CodeNameTaken } from "../api/errors";
import { type User, UserCurrent } from "../api/user";
import { CodeCookieRequired } from "../api/web";
import { expectAccessible } from "../test/axe";
import { type FakeApi, fakeApi, refusal } from "../test/fakeApi";
import { setViewportWidth } from "../test/media";
import { alerts, chooseInAccountMenu, findAlert } from "../test/pages";
import { ConfirmDialog } from "./ConfirmDialog";
import { useRead, useWrite } from "./data";
import { useEditForm } from "./form";
import { ErrorMessage } from "./i18n";
import { navigate, type PageProps, type Route, useSearchParams } from "./router";
import { OwnerOnly, useCaller } from "./caller";
import { Shell } from "./Shell";

// The shell at the page level: pages written as a capability's would be,
// against the fake API, through the shell's own client and hooks.

const alice: User = { id: "user_1", name: "alice", role: "owner", created_at: "2026-11-01T09:00:00Z" };
const bob: User = { id: "user_2", name: "bob", role: "member", created_at: "2026-11-02T09:00:00Z" };

/** asDevice makes user.current answer user on a Device Session of device. */
function asDevice(api: FakeApi, user: User, device: string): void {
  api.on(UserCurrent, () => ({
    user,
    device_session: { id: "device_1", user: user.name, device, created_at: "2026-11-03T09:00:00Z" },
  }));
}

/** asLocalCaller makes user.current answer the Owner, with no Device Session. */
function asLocalCaller(api: FakeApi): void {
  api.on(UserCurrent, () => ({ user: alice }));
}

// A record of the test's own, read, edited and removed as a capability's
// pages would do with theirs.

interface Thing {
  name: string;
  provider: string;
  note: string;
}

const ThingShow: Op<{ name: string }, { thing: Thing }> = {
  name: "thing.show",
  path: "/v1/thing.show",
  write: false,
};

const ThingSet: Op<{ name: string; provider?: string; note?: string }, { thing: Thing }> = {
  name: "thing.set",
  path: "/v1/thing.set",
  write: true,
};

const ThingRemove: Op<{ name: string }, { thing: Thing }> = {
  name: "thing.remove",
  path: "/v1/thing.remove",
  write: true,
};

const web1: Thing = { name: "web1", provider: "hetzner", note: "Line one\n  indented" };

function ThingPage({ params }: PageProps) {
  const show = useRead(ThingShow, { name: params["name"] ?? "" });
  if (!show.data) {
    return show.error ? <ErrorMessage error={show.error} /> : null;
  }
  return <ThingEditor thing={show.data.thing} />;
}

function ThingEditor({ thing }: { thing: Thing }) {
  const form = useEditForm({ provider: thing.provider, note: thing.note });
  const save = useWrite(ThingSet);
  const remove = useWrite(ThingRemove);
  const [removing, setRemoving] = useState(false);
  const [removed, setRemoved] = useState(false);
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void save.submit({ name: thing.name, ...form.changes }).then((r) => r && form.saved());
      }}
    >
      <h1>{thing.name}</h1>
      <input aria-label="provider" value={form.values.provider} onChange={(e) => form.set("provider", e.target.value)} />
      <textarea aria-label="note" value={form.values.note} onChange={(e) => form.set("note", e.target.value)} />
      <button type="submit">Save</button>
      {save.error !== undefined && <ErrorMessage error={save.error} />}
      <OwnerOnly>
        <button type="button" onClick={() => setRemoving(true)}>
          Remove
        </button>
      </OwnerOnly>
      {removing && (
        <ConfirmDialog
          title={`Remove ${thing.name}`}
          consequence={`${thing.name} is deleted for good.`}
          confirm="Remove"
          onCancel={() => setRemoving(false)}
          onConfirm={() => {
            setRemoving(false);
            void remove.submit({ name: thing.name }).then((r) => setRemoved(r !== undefined));
          }}
        />
      )}
      {removed && <p>removed</p>}
    </form>
  );
}

/** A page that keeps its state, a filter, in the URL. */
function FilterPage() {
  const [params, setParams] = useSearchParams();
  const provider = params.get("provider") ?? "";
  return (
    <>
      <p>filter: {provider || "none"}</p>
      <button type="button" onClick={() => setParams(new URLSearchParams({ provider: "hetzner" }))}>
        hetzner only
      </button>
    </>
  );
}

function Home() {
  return <p>home page</p>;
}

function People() {
  return <p>people page</p>;
}

const routes: Route[] = [
  { path: "/home", page: Home },
  { path: "/things/:name", page: ThingPage },
  { path: "/filter", page: FilterPage },
  { path: "/people", page: People, nav: "space.nav", ownerOnly: true },
];

/** renderShell renders the shell at path. */
function renderShell(path: string) {
  window.history.replaceState(null, "", path);
  return render(<Shell routes={routes} />);
}

/** preferLanguages makes the browser prefer these languages for one test. */
function preferLanguages(...languages: string[]): void {
  Object.defineProperty(navigator, "languages", { value: languages, configurable: true });
  onTestFinished(() => {
    Reflect.deleteProperty(navigator, "languages");
  });
}

function keysOf(api: FakeApi, op: Op<unknown, unknown>): (string | undefined)[] {
  return api.calls.filter((c) => c.path === op.path).map((c) => c.headers["idempotency-key"]);
}

async function openThing(api: FakeApi): Promise<void> {
  api.on(ThingShow, () => ({ thing: web1 }));
  renderShell("/things/web1");
  await screen.findByRole("heading", { name: "web1" });
}

// The header and who the web UI acts as.

test("the header shows the User and the device name the web UI acts as", async () => {
  const api = fakeApi();
  asDevice(api, bob, "laptop");

  renderShell("/home");

  await screen.findByText("home page");
  expect(shownOutsideMenus("bob on laptop")).toHaveLength(1);
});

test("over the Unix socket the header shows the Owner on the koko server's machine", async () => {
  const api = fakeApi();
  asLocalCaller(api);

  renderShell("/home");

  await screen.findByText("home page");
  expect(shownOutsideMenus("alice on the koko server's machine")).toHaveLength(1);
});

/**
 * shownOutsideMenus are the elements of the top bar that show text in the
 * bar itself: a closed menu's rows are in the document too, and are left out.
 */
function shownOutsideMenus(text: string): HTMLElement[] {
  return within(screen.getByRole("banner"))
    .queryAllByText(text)
    .filter((e) => !e.closest('[role="menu"]'));
}

// The frame: the side navigation, MobileNav and the account menu.

/** pageNav is the side navigation, or the MobileNav drawer's on a phone. */
function pageNav(): HTMLElement {
  return screen.getByRole("navigation", { name: "Pages" });
}

test("the side navigation links each page the caller may see and marks the one the URL is in", async () => {
  const api = fakeApi();
  asDevice(api, alice, "laptop");
  await openThing(api);

  const spaces = within(pageNav()).getByRole("link", { name: "Spaces" });
  expect(spaces.getAttribute("aria-current")).toBeNull();
  await expectAccessible();

  fireEvent.click(spaces);
  expect(await screen.findByText("people page")).toBeDefined();
  expect(spaces.getAttribute("aria-current")).toBe("page");
  expect(screen.getByRole("main").textContent).toContain("people page");
});

test("on a phone the side navigation is a drawer the top bar opens, which a visit closes", async () => {
  setViewportWidth(390);
  const api = fakeApi();
  asDevice(api, alice, "laptop");
  await openThing(api);
  expect(screen.queryByRole("navigation", { name: "Pages" })).toBeNull();

  fireEvent.click(within(screen.getByRole("banner")).getByRole("button", { name: "Open navigation" }));
  const drawer = await screen.findByRole("dialog");
  await within(drawer).findByRole("link", { name: "koko" }); // once the drawer takes input
  await expectAccessible();
  fireEvent.click(await within(drawer).findByRole("link", { name: "Spaces" }));

  expect(await screen.findByText("people page")).toBeDefined();
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
});

test("on a phone a Member's drawer links no Owner-only page", async () => {
  setViewportWidth(390);
  const api = fakeApi();
  asDevice(api, bob, "laptop");
  await openThing(api);

  fireEvent.click(within(screen.getByRole("banner")).getByRole("button", { name: "Open navigation" }));

  const drawer = await screen.findByRole("dialog");
  await within(drawer).findByRole("link", { name: "koko" }); // once the drawer takes input
  expect(within(drawer).queryByRole("link", { name: "Spaces" })).toBeNull();
});

test("on a phone the account menu, not the top bar, says whom the web UI acts as", async () => {
  setViewportWidth(390);
  const api = fakeApi();
  asDevice(api, bob, "laptop");
  renderShell("/home");
  await screen.findByText("home page");
  expect(shownOutsideMenus("bob on laptop")).toEqual([]);

  fireEvent.click(within(screen.getByRole("banner")).getByRole("button", { name: "Account" }));

  expect((await screen.findByRole("menu")).textContent).toContain("bob on laptop");
  await expectAccessible();
});

test("the account menu chooses Light, Dark or System, which the browser remembers", async () => {
  const api = fakeApi();
  asDevice(api, bob, "laptop");
  renderShell("/home");
  await screen.findByText("home page");

  fireEvent.click(within(screen.getByRole("banner")).getByRole("button", { name: "Account" }));
  expect(await screen.findByRole("menuitemradio", { name: "System", checked: true })).toBeDefined();
  await expectAccessible();
  fireEvent.click(screen.getByRole("menuitemradio", { name: "Dark" }));
  expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
  expect(localStorage.getItem("koko.theme")).toBe("dark");

  await chooseInAccountMenu("System");
  expect(document.documentElement.hasAttribute("data-theme")).toBe(false);
  expect(localStorage.getItem("koko.theme")).toBe("system");
});

test("a path with no page says so, with a way to the first page", async () => {
  const api = fakeApi();
  asDevice(api, alice, "laptop");
  renderShell("/nowhere");

  expect(await screen.findByRole("heading", { level: 1, name: "Page not found" })).toBeDefined();
  await expectAccessible();
  fireEvent.click(screen.getByRole("link", { name: "Go to the first page" }));

  expect(await screen.findByText("people page")).toBeDefined();
});

// The API client: retries and Idempotency-Key.

test("a write whose connection drops is retried once, with the same Idempotency-Key", async () => {
  const api = fakeApi();
  asDevice(api, alice, "laptop");
  api.on(ThingSet, (r) => ({ thing: { ...web1, ...r } }));
  await openThing(api);

  fireEvent.change(screen.getByLabelText("provider"), { target: { value: "ovh" } });
  api.drop(1);
  fireEvent.click(screen.getByRole("button", { name: "Save" }));

  await waitFor(() => expect(keysOf(api, ThingSet)).toHaveLength(2));
  const [first, retry] = keysOf(api, ThingSet);
  expect(first).toMatch(/^[0-9a-f]{32}$/);
  expect(retry).toBe(first);
  expect(alerts()).toEqual([]);
});

test("a read whose connection drops is retried once, without an Idempotency-Key", async () => {
  const api = fakeApi();
  asDevice(api, alice, "laptop");
  api.drop(1);

  renderShell("/home");

  await screen.findByText("home page");
  expect(api.calls.map((c) => [c.method, c.path, c.request, c.headers["idempotency-key"]])).toEqual([
    ["POST", "/v1/user.current", {}, undefined],
    ["POST", "/v1/user.current", {}, undefined],
  ]);
});

test("submitting a form again after its write failed reuses its key; another request takes a new one", async () => {
  const api = fakeApi();
  asDevice(api, alice, "laptop");
  api.on(ThingSet, () => {
    throw refusal(CodeNameTaken, 409);
  });
  await openThing(api);
  const save = screen.getByRole("button", { name: "Save" });
  fireEvent.change(screen.getByLabelText("provider"), { target: { value: "ovh" } });

  api.drop(2); // the write and its retry
  fireEvent.click(save);
  await waitFor(() => expect(alerts()).toContain("The connection to koko web was lost."));
  api.drop(0);
  fireEvent.click(save); // refused this time
  await waitFor(() => expect(keysOf(api, ThingSet)).toHaveLength(3));
  fireEvent.click(save);
  await waitFor(() => expect(keysOf(api, ThingSet)).toHaveLength(4));
  fireEvent.change(screen.getByLabelText("note"), { target: { value: "moved" } });
  fireEvent.click(save);
  await waitFor(() => expect(keysOf(api, ThingSet)).toHaveLength(5));

  const [first, retry, resubmitted, again, changed] = keysOf(api, ThingSet);
  expect([retry, resubmitted, again]).toEqual([first, first, first]);
  expect(changed).not.toBe(first);
});

test("a write that succeeded takes a new key when the same request is sent again", async () => {
  const api = fakeApi();
  asDevice(api, bob, "laptop");
  api.on(DeviceRevoke, () => ({
    device_session: { id: "device_2", user: "bob", device: "phone", created_at: "", revoked_at: "" },
  }));
  window.history.replaceState(null, "", "/revoke");
  render(<Shell routes={[{ path: "/revoke", page: RevokePage }]} />);

  fireEvent.click(await screen.findByRole("button", { name: "revoke phone" }));
  await screen.findByText("revoked");
  fireEvent.click(screen.getByRole("button", { name: "revoke phone" }));

  await waitFor(() => expect(keysOf(api, DeviceRevoke)).toHaveLength(2));
  const [first, second] = keysOf(api, DeviceRevoke);
  expect(second).not.toBe(first);
});

test("a write that succeeds while a later one is pending leaves the later one its key", async () => {
  const api = fakeApi();
  asDevice(api, alice, "laptop");
  let answerFirst = () => {};
  api.on(ThingSet, (r) =>
    r.provider === "ovh"
      ? new Promise((resolve) => {
          answerFirst = () => resolve({ thing: { ...web1, ...r } });
        })
      : { thing: { ...web1, ...r } },
  );
  await openThing(api);
  const save = screen.getByRole("button", { name: "Save" });

  fireEvent.change(screen.getByLabelText("provider"), { target: { value: "ovh" } });
  fireEvent.click(save); // pending until answerFirst
  await waitFor(() => expect(keysOf(api, ThingSet)).toHaveLength(1));
  fireEvent.change(screen.getByLabelText("provider"), { target: { value: "aws" } });
  api.drop(2);
  fireEvent.click(save); // dropped, and dropped again
  await waitFor(() => expect(keysOf(api, ThingSet)).toHaveLength(3));
  await act(async () => answerFirst());
  api.drop(0);
  fireEvent.click(save); // the same request again

  await waitFor(() => expect(keysOf(api, ThingSet)).toHaveLength(4));
  const [first, later, laterRetry, laterAgain] = keysOf(api, ThingSet);
  expect(later).not.toBe(first);
  expect([laterRetry, laterAgain]).toEqual([later, later]);
});

// Edit forms.

test("an edit form sends only the fields that changed", async () => {
  const api = fakeApi();
  asDevice(api, alice, "laptop");
  api.on(ThingSet, (r) => ({ thing: { ...web1, ...r } }));
  await openThing(api);

  fireEvent.change(screen.getByLabelText("note"), { target: { value: "renewed" } });
  fireEvent.change(screen.getByLabelText("provider"), { target: { value: "ovh" } });
  fireEvent.change(screen.getByLabelText("provider"), { target: { value: "hetzner" } }); // back as it was
  fireEvent.click(screen.getByRole("button", { name: "Save" }));

  await waitFor(() => expect(api.calls.filter((c) => c.path === ThingSet.path)).toHaveLength(1));
  expect(api.calls.find((c) => c.path === ThingSet.path)?.request).toEqual({ name: "web1", note: "renewed" });
  expect(screen.queryByRole("alertdialog")).toBeNull(); // a reversible write never asks
});

test("once a write succeeds, its values are the edit form's baseline", async () => {
  const api = fakeApi();
  asDevice(api, alice, "laptop");
  api.on(ThingSet, (r) => ({ thing: { ...web1, ...r } }));
  await openThing(api);
  const sets = () => api.calls.filter((c) => c.path === ThingSet.path).map((c) => c.request);

  fireEvent.change(screen.getByLabelText("provider"), { target: { value: "ovh" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(sets()).toHaveLength(1));
  fireEvent.change(screen.getByLabelText("provider"), { target: { value: "hetzner" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));

  await waitFor(() => expect(sets()).toHaveLength(2));
  expect(sets()).toEqual([
    { name: "web1", provider: "ovh" },
    { name: "web1", provider: "hetzner" },
  ]);
});

// Confirmations.

test("an irreversible action asks first, in a dialog that states the consequence", async () => {
  const api = fakeApi();
  asDevice(api, alice, "laptop");
  api.on(ThingRemove, () => ({ thing: web1 }));
  await openThing(api);

  const remove = screen.getByRole("button", { name: "Remove" });
  remove.focus(); // as a click or a key does in a browser
  fireEvent.click(remove);
  const dialog = screen.getByRole("alertdialog", { name: "Remove web1" });
  expect(dialog.textContent).toContain("web1 is deleted for good.");
  expect(document.activeElement).toBe(within(dialog).getByRole("button", { name: "Cancel" }));
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("alertdialog")).toBeNull();
  await waitFor(() => expect(document.activeElement).toBe(remove));
  expect(api.calls.filter((c) => c.path === ThingRemove.path)).toEqual([]);

  fireEvent.click(screen.getByRole("button", { name: "Remove" }));
  fireEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Remove" }));

  expect(await screen.findByText("removed")).toBeDefined();
  expect(api.calls.filter((c) => c.path === ThingRemove.path)).toHaveLength(1);
});

test("Escape closes the dialog without acting", async () => {
  const onConfirm = vi.fn();
  const onCancel = vi.fn();
  await renderDialog({ onConfirm, onCancel });

  fireEvent(screen.getByRole("alertdialog"), new Event("cancel", { cancelable: true })); // Escape

  expect(onCancel).toHaveBeenCalledOnce();
  expect(onConfirm).not.toHaveBeenCalled();
});

test("the typed-name variant confirms only once the name is typed exactly", async () => {
  const onConfirm = vi.fn();
  await renderDialog({ onConfirm, onCancel: vi.fn(), typedName: "laptop" });
  const dialog = screen.getByRole("alertdialog", { name: "Revoke laptop" });
  const confirm = within(dialog).getByRole<HTMLButtonElement>("button", { name: "Revoke" });
  const input = within(dialog).getByLabelText("Type laptop to confirm.");

  expect(dialog.textContent).toContain("laptop can never connect again.");
  expect(document.activeElement).toBe(input);
  await expectAccessible();
  expect(confirm.disabled).toBe(true);
  fireEvent.click(confirm);
  fireEvent.change(input, { target: { value: "lapto" } });
  expect(confirm.disabled).toBe(true);
  fireEvent.change(input, { target: { value: "Laptop" } });
  expect(confirm.disabled).toBe(true);
  fireEvent.change(input, { target: { value: "laptop" } });
  expect(confirm.disabled).toBe(false);
  fireEvent.click(confirm);

  expect(onConfirm).toHaveBeenCalledOnce();
});

test("the typed-name variant gives the focus back when it closes, and on a phone puts the action above Cancel", async () => {
  setViewportWidth(390);
  const api = fakeApi();
  asDevice(api, alice, "laptop");
  function TypedPage() {
    const [asking, setAsking] = useState(false);
    return (
      <>
        <button type="button" onClick={() => setAsking(true)}>
          revoke laptop
        </button>
        {asking && (
          <ConfirmDialog
            title="Revoke laptop"
            consequence="laptop can never connect again."
            confirm="Revoke"
            typedName="laptop"
            onConfirm={() => setAsking(false)}
            onCancel={() => setAsking(false)}
          />
        )}
      </>
    );
  }
  window.history.replaceState(null, "", "/typed");
  render(<Shell routes={[{ path: "/typed", page: TypedPage }]} />);
  const opener = await screen.findByRole("button", { name: "revoke laptop" });
  opener.focus();
  fireEvent.click(opener);

  const dialog = screen.getByRole("alertdialog", { name: "Revoke laptop" });
  expect(within(dialog).getAllByRole("button").map((b) => b.textContent)).toEqual(["Revoke", "Cancel"]);
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));

  expect(screen.queryByRole("alertdialog")).toBeNull();
  await waitFor(() => expect(document.activeElement).toBe(opener));
});

function renderDialog(props: { onConfirm: () => void; onCancel: () => void; typedName?: string }) {
  const api = fakeApi();
  asDevice(api, alice, "laptop");
  function DialogPage() {
    return (
      <ConfirmDialog
        title="Revoke laptop"
        consequence="laptop can never connect again."
        confirm="Revoke"
        {...props}
      />
    );
  }
  window.history.replaceState(null, "", "/dialog");
  render(<Shell routes={[{ path: "/dialog", page: DialogPage }]} />);
  return screen.findByRole("alertdialog");
}

// Owner-only pages and buttons.

test("a Member sees no Owner-only page, link or button", async () => {
  const api = fakeApi();
  asDevice(api, bob, "laptop");
  await openThing(api);

  expect(within(pageNav()).queryByRole("link", { name: "Spaces" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Remove" })).toBeNull();

  act(() => navigate("/people"));
  expect(await screen.findByRole("heading", { level: 1, name: "Page not found" })).toBeDefined();
  expect(screen.queryByText("people page")).toBeNull();
});

test.each([
  ["on a device", (api: FakeApi) => asDevice(api, alice, "laptop")],
  ["over the Unix socket", asLocalCaller],
])("the Owner %s sees the Owner-only page, link and button", async (_, as) => {
  const api = fakeApi();
  as(api);
  await openThing(api);

  expect(screen.getByRole("button", { name: "Remove" })).toBeDefined();
  fireEvent.click(within(pageNav()).getByRole("link", { name: "Spaces" }));
  expect(await screen.findByText("people page")).toBeDefined();
  expect(window.location.pathname).toBe("/people");
});

// Language.

test("the language follows the browser's preferred languages: Chinese of any region gives Simplified Chinese", async () => {
  preferLanguages("zh-TW", "en");
  const api = fakeApi();
  asDevice(api, bob, "laptop");

  renderShell("/home");

  await screen.findByText("home page");
  expect(screen.getByRole("banner").textContent).toContain("bob · 设备 laptop");
  expect(document.documentElement.lang).toBe("zh-CN");
  fireEvent.click(within(screen.getByRole("banner")).getByRole("button", { name: "账户" }));
  expect(await screen.findByRole("menuitemradio", { name: "简体中文", checked: true })).toBeDefined();
});

test.each([
  [["fr-FR", "zh-HK", "en"], "zh-CN"],
  [["fr-FR", "en-US", "zh-CN"], "en"],
  [["fr-FR", "de"], "en"],
])("a preferred language the web UI does not ship is passed over, and none gives English: %j", async (preferred, want) => {
  preferLanguages(...preferred);
  const api = fakeApi();
  asDevice(api, bob, "laptop");

  renderShell("/home");

  await screen.findByText("home page");
  expect(document.documentElement.lang).toBe(want);
});

test("the account menu's choice overrides the browser's language, and the browser remembers it", async () => {
  preferLanguages("en-GB");
  const api = fakeApi();
  asDevice(api, bob, "laptop");
  const { unmount } = renderShell("/home");
  await screen.findByText("home page");

  await chooseInAccountMenu("简体中文");
  expect(screen.getByRole("banner").textContent).toContain("bob · 设备 laptop");
  unmount();

  renderShell("/home"); // a reload of the page
  await screen.findByText("home page");
  expect(document.documentElement.lang).toBe("zh-CN");
  expect(screen.getByRole("banner").textContent).toContain("bob · 设备 laptop");
});

test("an API error shows its code's message in the current language", async () => {
  const api = fakeApi();
  asDevice(api, alice, "laptop");
  api.on(ThingSet, () => {
    throw refusal(CodeNameTaken, 409);
  });
  await openThing(api);
  fireEvent.change(screen.getByLabelText("provider"), { target: { value: "ovh" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));

  expect(await findAlert()).toBe("This name is already in use.");
  await expectAccessible();
  await chooseInAccountMenu("简体中文");
  expect(alerts()).toEqual(["此名称已被占用。"]);
});

test("names, keys, values and Notes show as stored, in either language", async () => {
  preferLanguages("zh-CN");
  const api = fakeApi();
  asDevice(api, alice, "laptop");
  await openThing(api);

  expect(screen.getByLabelText<HTMLInputElement>("provider").value).toBe("hetzner");
  expect(screen.getByLabelText<HTMLTextAreaElement>("note").value).toBe("Line one\n  indented");
});

// Notices.

test("when koko web stops the page says to run it again, until a call gets through", async () => {
  const api = fakeApi();
  asDevice(api, alice, "laptop");
  renderShell("/home");
  await screen.findByText("home page");

  api.drop();
  fireEvent.focus(window);

  const stopped = await findAlert();
  expect(stopped).toContain("koko web is not running");
  expect(stopped).toContain("Run koko web again, then return to this page.");
  expect(screen.getByText("home page")).toBeDefined();
  await expectAccessible();

  api.drop(0);
  fireEvent.focus(window);
  await waitFor(() => expect(alerts()).toEqual([]));
});

test("a refusal shows koko web runs again, so the notice that it stopped goes", async () => {
  const api = fakeApi();
  asDevice(api, alice, "laptop");
  api.on(ThingSet, () => {
    throw refusal(CodeNameTaken, 409);
  });
  await openThing(api);
  fireEvent.change(screen.getByLabelText("provider"), { target: { value: "ovh" } });
  api.drop(2);
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await screen.findByText("koko web is not running");

  api.drop(0);
  fireEvent.click(screen.getByRole("button", { name: "Save" }));

  await waitFor(() => expect(alerts()).toEqual(["This name is already in use."]));
});

test("when the Device Session is Revoked the page says so, for good", async () => {
  const api = fakeApi();
  asDevice(api, bob, "laptop");
  api.on(ThingSet, () => {
    throw refusal(CodeRevoked, 401);
  });
  await openThing(api);

  fireEvent.change(screen.getByLabelText("provider"), { target: { value: "ovh" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));

  const notice = "The Device Session of this device was Revoked";
  await waitFor(() => expect(alerts()[0]).toContain(notice));
  fireEvent.focus(window); // reads that still answer, as one sent before the revocation would
  await waitFor(() => expect(api.calls.filter((c) => c.path === ThingShow.path)).toHaveLength(2));
  await waitFor(() => expect(api.calls.filter((c) => c.path === UserCurrent.path)).toHaveLength(2));
  expect(alerts()[0]).toContain(notice);
});

/** A page that revokes a Device Session, as the Devices page does. */
function RevokePage() {
  const caller = useCaller();
  const revoke = useWrite(DeviceRevoke);
  const [done, setDone] = useState(false);
  const submit = (device: string) => void revoke.submit({ device }).then(() => setDone(true));
  return (
    <>
      <button type="button" onClick={() => submit(caller.deviceSession?.device ?? "")}>
        revoke this device
      </button>
      <button type="button" onClick={() => submit("phone")}>
        revoke phone
      </button>
      {done && <p>revoked</p>}
    </>
  );
}

test.each([
  ["this device's Device Session", "revoke this device", true],
  ["another Device Session", "revoke phone", false],
])("revoking %s from the web UI says so only for this device's", async (_, button, notice) => {
  const api = fakeApi();
  asDevice(api, bob, "laptop");
  const sessions: Record<string, DeviceSession> = {
    laptop: { id: "device_1", user: "bob", device: "laptop", created_at: "2026-11-03T09:00:00Z" },
    phone: { id: "device_2", user: "bob", device: "phone", created_at: "2026-11-04T09:00:00Z" },
  };
  api.on(DeviceRevoke, ({ device }) => ({ device_session: { ...sessions[device]!, revoked_at: "2026-11-06T09:00:00Z" } }));
  window.history.replaceState(null, "", "/revoke");
  render(<Shell routes={[{ path: "/revoke", page: RevokePage }]} />);

  fireEvent.click(await screen.findByRole("button", { name: button }));

  await screen.findByText("revoked");
  if (notice) {
    expect(alerts()[0]).toContain("The Device Session of this device was Revoked");
  } else {
    expect(alerts()).toEqual([]);
  }
  expect(api.calls.filter((c) => c.path === DeviceRevoke.path)).toHaveLength(1);
});

test("when the browser's cookie is no longer valid, the page says to open koko web's address", async () => {
  const api = fakeApi();
  let ended = false;
  api.on(UserCurrent, () => {
    if (ended) {
      throw refusal(CodeCookieRequired, 401);
    }
    return { user: alice };
  });
  renderShell("/home");
  await screen.findByText("home page");

  ended = true; // koko web ran again over the Unix socket, with a new cookie
  fireEvent.focus(window);

  expect(await findAlert()).toBe(
    "This browser no longer holds a valid cookie for this page. Open the address that koko web printed.",
  );
});

// Reloading.

test("data reloads when the window regains focus and on every navigation", async () => {
  const api = fakeApi();
  asDevice(api, alice, "laptop");
  let provider = "hetzner";
  api.on(ThingShow, () => ({ thing: { ...web1, provider } }));
  renderShell("/things/web1");
  await screen.findByRole("heading", { name: "web1" });

  provider = "ovh";
  fireEvent.focus(window);
  await waitFor(() => expect(api.calls.filter((c) => c.path === ThingShow.path)).toHaveLength(2));
  await waitFor(() => expect(api.calls.filter((c) => c.path === UserCurrent.path)).toHaveLength(2));

  act(() => navigate("/things/web1")); // the page it is on
  await waitFor(() => expect(api.calls.filter((c) => c.path === ThingShow.path)).toHaveLength(3));
});

// Routing.

test("a page keeps its state in the URL, and back returns to the earlier state", async () => {
  const api = fakeApi();
  asDevice(api, alice, "laptop");
  renderShell("/filter");
  expect(await screen.findByText("filter: none")).toBeDefined();

  fireEvent.click(screen.getByRole("button", { name: "hetzner only" }));
  expect(await screen.findByText("filter: hetzner")).toBeDefined();
  expect(window.location.search).toBe("?provider=hetzner");

  act(() => window.history.back());
  expect(await screen.findByText("filter: none")).toBeDefined();
  expect(window.location.search).toBe("");
});

test("/ goes to the first page the side navigation links, keeping the query", async () => {
  const api = fakeApi();
  asDevice(api, alice, "laptop");

  renderShell("/?view=all");

  expect(await screen.findByText("people page")).toBeDefined();
  expect(window.location.pathname + window.location.search).toBe("/people?view=all");
});
