import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, type MockInstance, onTestFinished, test, vi } from "vitest";

import { CodeNameTaken } from "../../api/errors";
import {
  type Secret,
  SecretAdd,
  SecretList,
  SecretMove,
  SecretRemove,
  SecretRename,
  SecretSet,
  SecretShow,
} from "../../api/secret";
import { type Space, SpaceList } from "../../api/space";
import type { User } from "../../api/user";
import { expectAccessible } from "../../test/axe";
import { clipboard } from "../../test/clipboard";
import { chooseRowAction, expectChosen, searchAndChoose } from "../../test/controls";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { actAs, alerts, renderAt, statuses } from "../../test/pages";

// The Secrets page, against the fake API: one panel per Space, searched and
// narrowed in the URL, a value copied without being shown or shown in its
// row until it is forgotten, and the writes, each in a dialog of its own.

const bob: User = { id: "user_2", name: "bob", role: "member", created_at: "2026-11-02T09:00:00Z" };

function secret(space: string, name: string, updated = "2026-11-05T09:00:00Z"): Secret {
  return { id: `secret_${space}_${name}`, space, name, created_at: "2026-11-04T09:00:00Z", updated_at: updated };
}

const spaces: Space[] = [
  { id: "space_1", name: "bob", personal: true },
  { id: "space_2", name: "ops", personal: false },
  { id: "space_3", name: "web", personal: false },
];

/** marker is a value's text; its base64, with a newline after it, is markerBase64. */
const marker = "s3cret-marker";
const markerBase64 = "czNjcmV0LW1hcmtlcgo=";

/** panel is the region of the Space named space. */
function panel(space: string): HTMLElement {
  return screen.getByRole("region", { name: space });
}

/** names are the Secret names each Space's panel lists, by Space, in order. */
function names(): Record<string, string[]> {
  return Object.fromEntries(
    screen
      .queryAllByRole("region")
      .map((region) => [
        region.getAttribute("aria-label") ?? within(region).getByRole("heading").firstChild?.textContent ?? "",
        within(region)
          .getAllByRole("listitem")
          .map((item) => item.querySelector(".t-name")?.textContent ?? ""),
      ]),
  );
}

/**
 * asBob renders the Secrets page at path as bob, a Member of ops and web,
 * whose Spaces hold secrets, as secret.list answers them.
 */
async function asBob(
  secrets: Secret[] = [secret("bob", "token"), secret("ops", "db-password")],
  path = "/secrets",
): Promise<FakeApi> {
  const api = fakeApi();
  actAs(api, bob);
  api.on(SpaceList, () => ({ spaces }));
  api.on(SecretList, () => ({ secrets }));
  renderAt(path);
  await screen.findByRole("heading", { level: 1, name: "Secrets" });
  await waitFor(() => expect(screen.getAllByRole("listitem").length).toBeGreaterThan(0));
  return api;
}

/** answerShow makes secret.show answer every Secret with the value whose base64 is valueBase64. */
function answerShow(api: FakeApi, valueBase64 = markerBase64): void {
  api.on(SecretShow, ({ space, name }) => ({ secret: secret(space, name), value_base64: valueBase64 }));
}

/** valueOf is the region that shows the value of name, <space>/<secret>, if shown. */
function valueOf(name: string): HTMLElement | null {
  return screen.queryByRole("group", { name: `Value of ${name}` });
}

/** show clicks Show on the Secret named name, <space>/<secret>, and returns its value once it shows. */
async function show(name: string, text = marker): Promise<HTMLElement> {
  fireEvent.click(screen.getByRole("button", { name: `Show the value of ${name}` }));
  const shown = await screen.findByRole("group", { name: `Value of ${name}` });
  await within(shown).findByText(text, { exact: false });
  return shown;
}

/** onPage tells whether the marker value is anywhere on the page. */
function onPage(): boolean {
  return document.body.textContent.includes(marker);
}

// Listing.

test("a Member sees one panel per Space of theirs, each Secret's name and when it changed, and no value", async () => {
  vi.setSystemTime(new Date("2026-11-08T09:00:00Z"));
  onTestFinished(() => {
    vi.useRealTimers();
  });
  const api = await asBob([secret("bob", "token"), secret("ops", "api-key"), secret("ops", "db-password")]);

  expect(names()).toEqual({ bob: ["token"], ops: ["api-key", "db-password"] });
  expect(within(panel("bob")).getByRole("heading").textContent).toBe("bobPersonal Space · 1 Secret");
  expect(within(panel("ops")).getByRole("heading").textContent).toBe("ops2 Secrets");
  expect(within(panel("ops")).getAllByText("Changed 3 days ago")).toHaveLength(2);
  // The koko server answers only the Spaces the caller can act in.
  expect(api.requests(SecretList)).toEqual([{}]);
  expect(api.requests(SecretShow)).toEqual([]);
  expect(
    within(screen.getByRole("navigation", { name: "Main" })).getByRole("link", { name: "Secrets" }),
  ).toHaveProperty("ariaCurrent", "page");
  await waitFor(() => expect(document.title).toBe("Secrets · koko"));
  await expectAccessible();
});

test("a caller whose Spaces hold no Secret is told so, and may add one", async () => {
  const api = fakeApi();
  actAs(api, bob);
  api.on(SpaceList, () => ({ spaces }));
  api.on(SecretList, () => ({ secrets: [] }));

  renderAt("/secrets");

  expect(await screen.findByRole("heading", { name: "No Secret is stored" })).toBeDefined();
  expect(screen.queryByRole("searchbox")).toBeNull();
  expect(screen.getAllByRole("button", { name: "Add Secret" })).toHaveLength(1);
  await expectAccessible();
});

test("a list that does not load says why, and Try again loads it", async () => {
  const api = fakeApi();
  actAs(api, bob);
  api.on(SpaceList, () => ({ spaces }));
  api.on(SecretList, () => {
    throw refusal("internal_error", 500);
  });
  renderAt("/secrets");

  await waitFor(() => expect(alerts()).toEqual(["The koko server failed unexpectedly. Its log records the cause."]));
  await expectAccessible();
  api.on(SecretList, () => ({ secrets: [secret("ops", "db-password")] }));
  fireEvent.click(screen.getByRole("button", { name: "Try again" }));

  await waitFor(() => expect(names()).toEqual({ ops: ["db-password"] }));
  expect(alerts()).toEqual([]);
});

test("the search narrows the list by Space or name, kept in the URL in place, and says when nothing matches", async () => {
  await asBob([secret("bob", "token"), secret("ops", "db-password"), secret("web", "tls-key")]);
  const box = screen.getByRole("searchbox", { name: "Search Secrets" });
  const entries = window.history.length;

  fireEvent.change(box, { target: { value: "OPS/" } });
  expect(names()).toEqual({ ops: ["db-password"] });
  expect(window.location.search).toBe("?q=OPS%2F");

  fireEvent.change(box, { target: { value: "key" } });
  expect(names()).toEqual({ web: ["tls-key"] });

  fireEvent.change(box, { target: { value: "nothing" } });
  expect(screen.getByText("No Secret matches this view.")).toBeDefined();
  expect(window.history.length).toBe(entries);
  await expectAccessible();
  fireEvent.click(screen.getByRole("button", { name: "Clear the search and filters" }));
  expect(names()).toEqual({ bob: ["token"], ops: ["db-password"], web: ["tls-key"] });
  expect(window.location.search).toBe("");
});

test("the Space select narrows the list to one Space, kept in the URL, All Spaces first", async () => {
  await asBob([secret("bob", "token"), secret("ops", "db-password"), secret("web", "tls-key")]);
  const entries = window.history.length;

  await searchAndChoose("Space", "e", "web", ["All Spaces", "web"]);

  expect(names()).toEqual({ web: ["tls-key"] });
  expect(window.location.search).toBe("?space=web");
  fireEvent.change(screen.getByRole("searchbox", { name: "Search Secrets" }), { target: { value: "tls" } });
  expect(new URLSearchParams(window.location.search).get("space")).toBe("web");

  await searchAndChoose("Space", "all", "All Spaces");
  expect(names()).toEqual({ web: ["tls-key"] });
  expect(window.location.search).toBe("?q=tls");
  expect(window.history.length).toBe(entries);
});

test("a bookmark with a search and a Space opens the list narrowed", async () => {
  await asBob(
    [secret("bob", "token"), secret("ops", "db-password"), secret("ops", "token"), secret("web", "token")],
    "/secrets?q=token&space=ops",
  );

  expect(names()).toEqual({ ops: ["token"] });
  expect(screen.getByRole<HTMLInputElement>("searchbox", { name: "Search Secrets" }).value).toBe("token");
  await expectChosen("Space", "ops");
});

// Copy, Show and Hide.

test("Copy calls secret.show and copies the value without putting it on the page", async () => {
  const writeText = clipboard();
  const api = await asBob();
  answerShow(api);

  fireEvent.click(screen.getByRole("button", { name: "Copy the value of ops/db-password" }));

  await waitFor(() => expect(writeText.mock.calls).toEqual([[`${marker}\n`]]));
  expect(api.requests(SecretShow)).toEqual([{ space: "ops", name: "db-password" }]);
  expect(await within(panel("ops")).findByText("Copied")).toBeDefined();
  expect(onPage()).toBe(false);
  expect(valueOf("ops/db-password")).toBeNull();
});

test("Show opens the value in its row, never asking, its own Copy copies it, and Hide forgets it", async () => {
  const writeText = clipboard();
  const api = await asBob();
  answerShow(api);

  const shown = await show("ops/db-password");

  expect(screen.queryByRole("dialog")).toBeNull();
  expect(api.requests(SecretShow)).toEqual([{ space: "ops", name: "db-password" }]);
  expect(within(shown).getByText(marker, { exact: false }).tagName).toBe("PRE");
  const hide = screen.getByRole("button", { name: "Hide the value of ops/db-password" });
  expect(hide.getAttribute("aria-expanded")).toBe("true");
  await expectAccessible();

  fireEvent.click(within(shown).getByRole("button", { name: "Copy" }));
  await waitFor(() => expect(writeText.mock.calls).toEqual([[`${marker}\n`]]));
  fireEvent.click(hide);

  expect(valueOf("ops/db-password")).toBeNull();
  expect(onPage()).toBe(false);
  await show("ops/db-password");
  expect(api.requests(SecretShow)).toHaveLength(2);
});

/**
 * fakeTimeouts fakes setTimeout for one test, so a test moves the time a
 * value shown stays. The faked clock also runs on with the real one, so the
 * testing library's own short waits still end.
 */
function fakeTimeouts(): void {
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"], shouldAdvanceTime: true });
  timeouts = vi.spyOn(window, "setTimeout");
  onTestFinished(() => {
    vi.useRealTimers();
  });
}

/** timeouts records each setTimeout since fakeTimeouts, with its delay. */
let timeouts: MockInstance<typeof window.setTimeout> | undefined;

/**
 * hideTimerStarted waits until the value's own timer of 30 seconds runs:
 * React starts it in an effect that may run only after the value shows. It
 * looks on the real clock, with an interval of 0, which never moves the
 * faked one.
 */
async function hideTimerStarted(): Promise<void> {
  await vi.waitUntil(() => timeouts?.mock.calls.some(([, delay]) => delay === 30_000), {
    interval: 0,
    message: "the value's timer never started",
  });
}

test("a value shown is gone after 30 seconds, and the focus goes back to Show", async () => {
  fakeTimeouts();
  const api = await asBob();
  answerShow(api);
  const shown = await show("ops/db-password");
  act(() => within(shown).getByRole("button", { name: "Copy" }).focus());

  await hideTimerStarted();
  act(() => vi.advanceTimersByTime(25_000));
  expect(valueOf("ops/db-password")).not.toBeNull();
  act(() => vi.advanceTimersByTime(5_000));

  expect(valueOf("ops/db-password")).toBeNull();
  expect(onPage()).toBe(false);
  await waitFor(() =>
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Show the value of ops/db-password" })),
  );
});

test("a refused Show or Copy says why in its row", async () => {
  const api = await asBob();
  api.on(SecretShow, () => {
    throw refusal("unknown_secret", 404);
  });

  fireEvent.click(screen.getByRole("button", { name: "Show the value of ops/db-password" }));
  await waitFor(() => expect(alerts()).toEqual(["The Space holds no Secret with that name."]));
  expect(within(panel("ops")).getByRole("alert")).toBeDefined();
  await expectAccessible();

  const writeText = clipboard();
  fireEvent.click(screen.getByRole("button", { name: "Copy the value of bob/token" }));
  await waitFor(() => expect(within(panel("bob")).queryByRole("alert")).not.toBeNull());
  expect(writeText).not.toHaveBeenCalled();
});

test("a value answered after a write, a navigation or the browser leaving the page never shows", async () => {
  const api = await asBob();
  api.on(SecretSet, ({ space, name }) => ({ secret: secret(space, name, "2026-11-09T09:00:00Z") }));
  const answers: (() => void)[] = [];
  api.on(
    SecretShow,
    ({ space, name }) =>
      new Promise((resolve) => {
        answers.push(() => resolve({ secret: secret(space, name), value_base64: markerBase64 }));
      }),
  );
  const answerAll = async () => {
    await waitFor(() => expect(answers.length).toBeGreaterThan(0));
    await act(async () => {
      for (const answer of answers.splice(0)) {
        answer();
      }
    });
  };

  // The browser leaves the page while secret.show is answering.
  fireEvent.click(screen.getByRole("button", { name: "Show the value of ops/db-password" }));
  fireEvent(window, new Event("pagehide"));
  await answerAll();
  expect(onPage()).toBe(false);

  // The value is set, from the row's menu, while secret.show is answering
  // with the old one.
  fireEvent.click(screen.getByRole("button", { name: "Show the value of ops/db-password" }));
  await chooseRowAction("More actions for ops/db-password", "Set a new value");
  const form = screen.getByRole("dialog", { name: "New value for ops/db-password" });
  fireEvent.change(within(form).getByRole("textbox", { name: "New value" }), { target: { value: "n3w-s3cret" } });
  fireEvent.click(within(form).getByRole("button", { name: "Set a new value" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  await answerAll();
  expect(onPage()).toBe(false);

  // The page is left while secret.show is answering.
  fireEvent.click(screen.getByRole("button", { name: "Show the value of bob/token" }));
  act(() => window.history.pushState(null, "", "/nowhere"));
  act(() => window.dispatchEvent(new PopStateEvent("popstate")));
  await screen.findByRole("heading", { level: 1, name: "Page not found" });
  await answerAll();
  act(() => window.history.back());
  await screen.findByRole("heading", { level: 1, name: "Secrets" });
  expect(onPage()).toBe(false);
});

test("a value shown is gone once its Secret is written to, by this row or as a reload finds it", async () => {
  const api = await asBob();
  answerShow(api);
  api.on(SecretRename, ({ space, new_name }) => ({ secret: secret(space, new_name) }));
  await show("ops/db-password");

  await chooseRowAction("More actions for ops/db-password", "Rename");
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Rename" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(onPage()).toBe(false);

  // Another writer sets the value; the page loads again as the window regains focus.
  await show("bob/token");
  api.on(SecretList, () => ({
    secrets: [secret("bob", "token", "2026-11-09T09:00:00Z"), secret("ops", "db-password")],
  }));
  act(() => window.dispatchEvent(new Event("focus")));
  await waitFor(() => expect(onPage()).toBe(false));
});

test("leaving the page forgets the value, and coming back shows it no more", async () => {
  const api = await asBob();
  answerShow(api);
  await show("ops/db-password");

  act(() => window.history.pushState(null, "", "/nowhere"));
  act(() => window.dispatchEvent(new PopStateEvent("popstate")));
  await screen.findByRole("heading", { level: 1, name: "Page not found" });
  expect(onPage()).toBe(false);
  act(() => window.history.pushState(null, "", "/secrets"));
  act(() => window.dispatchEvent(new PopStateEvent("popstate")));
  await screen.findByRole("button", { name: "Show the value of ops/db-password" });

  expect(valueOf("ops/db-password")).toBeNull();
  expect(onPage()).toBe(false);
  expect(api.requests(SecretShow)).toHaveLength(1);
});

test("the browser leaving the page forgets the value, even when it keeps the page to come back to", async () => {
  const api = await asBob();
  answerShow(api);
  await show("ops/db-password");

  fireEvent(window, new Event("pagehide"));

  expect(valueOf("ops/db-password")).toBeNull();
  expect(onPage()).toBe(false);
});

test("a value that is not UTF-8 shows as its base64, marked so, and both Copy buttons copy the base64", async () => {
  const writeText = clipboard();
  const api = await asBob();
  answerShow(api, "/wAB"); // the bytes ff 00 01

  const shown = await show("ops/db-password", "/wAB");

  expect(shown.textContent).toContain("This value is not UTF-8 text. It is shown in base64");
  fireEvent.click(within(shown).getByRole("button", { name: "Copy" }));
  await waitFor(() => expect(writeText.mock.calls).toEqual([["/wAB"]]));
  fireEvent.click(screen.getByRole("button", { name: "Copy the value of ops/db-password" }));
  await waitFor(() => expect(writeText.mock.calls).toEqual([["/wAB"], ["/wAB"]]));
});

test("a browser that cannot copy says to select the value: shown, or to show it first", async () => {
  clipboard({ refuse: true });
  const api = await asBob();
  answerShow(api);

  fireEvent.click(screen.getByRole("button", { name: "Copy the value of ops/db-password" }));
  await waitFor(() =>
    expect(statuses()).toContain("This browser did not allow copying. Show the value, then select it and copy it."),
  );

  const shown = await show("bob/token");
  fireEvent.click(within(shown).getByRole("button", { name: "Copy" }));
  await waitFor(() =>
    expect(within(shown).getByRole("status").textContent).toBe(
      "This browser did not allow copying. Select the text and copy it.",
    ),
  );
  await expectAccessible();
});

// The writes.

/** idempotencyKeys are the Idempotency-Keys of every call of op, in the order they came. */
function idempotencyKeys(api: FakeApi, op: { path: string }): (string | undefined)[] {
  return api.calls.filter((c) => c.path === op.path).map((c) => c.headers["idempotency-key"]);
}

test("Add Secret never asks, sends the typed value as base64 of its UTF-8, and a retry after a drop resends its Idempotency-Key", async () => {
  const api = await asBob();
  api.on(SecretAdd, ({ space, name }) => ({ secret: secret(space, name) }));

  fireEvent.click(screen.getByRole("button", { name: "Add Secret" }));
  const form = screen.getByRole("dialog", { name: "Add Secret" });
  // bob's Personal Space is chosen first; any Space of theirs may be.
  expect(within(form).getByRole<HTMLInputElement>("combobox", { name: "Space" }).value).toBe("bob");
  await searchAndChoose(within(form).getByRole("combobox", { name: "Space" }), "o", "ops", ["bobPersonal Space", "ops"]);
  fireEvent.change(within(form).getByRole("textbox", { name: "Name" }), { target: { value: "api-key" } });
  fireEvent.change(within(form).getByRole("textbox", { name: "Value" }), { target: { value: "pässwörd\n" } });
  await expectAccessible();

  api.drop(2); // the call and its own retry
  fireEvent.click(within(form).getByRole("button", { name: "Add Secret" }));
  expect(await within(form).findByText("The connection to koko web was lost.")).toBeDefined();
  fireEvent.click(within(form).getByRole("button", { name: "Add Secret" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add Secret" })).toBeNull());

  const sent = { space: "ops", name: "api-key", value_base64: "cMOkc3N3w7ZyZAo=" };
  expect(api.requests(SecretAdd)).toEqual([sent, sent, sent]);
  const keys = idempotencyKeys(api, SecretAdd);
  expect(keys[0]).toMatch(/^[0-9a-f]{32}$/);
  expect(new Set(keys).size).toBe(1);
  expect(await screen.findByText("ops/api-key is stored.")).toBeDefined();
  await waitFor(() => expect(api.requests(SecretList)).toHaveLength(2));
});

test("Add Secret takes the Space the page is narrowed to, and checks its fields before it sends", async () => {
  const api = await asBob([secret("web", "tls-key")], "/secrets?space=web");

  fireEvent.click(screen.getByRole("button", { name: "Add Secret" }));
  const form = screen.getByRole("dialog", { name: "Add Secret" });
  expect(within(form).getByRole<HTMLInputElement>("combobox", { name: "Space" }).value).toBe("web");
  fireEvent.click(within(form).getByRole("button", { name: "Add Secret" }));

  expect(await within(form).findByText("Enter a name.")).toBeDefined();
  expect(within(form).getByText("Enter a value.")).toBeDefined();
  await waitFor(() => expect(document.activeElement).toBe(within(form).getByRole("textbox", { name: "Name" })));
  expect(api.requests(SecretAdd)).toEqual([]);
});

test("Set a new value never asks, and a retry after a drop resends its Idempotency-Key", async () => {
  const api = await asBob();
  api.on(SecretSet, ({ space, name }) => ({ secret: secret(space, name) }));

  await chooseRowAction("More actions for ops/db-password", "Set a new value");
  const form = screen.getByRole("dialog", { name: "New value for ops/db-password" });
  fireEvent.change(within(form).getByRole("textbox", { name: "New value" }), { target: { value: "n3w-s3cret" } });
  await expectAccessible();
  api.drop(2);
  fireEvent.click(within(form).getByRole("button", { name: "Set a new value" }));
  expect(await within(form).findByText("The connection to koko web was lost.")).toBeDefined();
  fireEvent.click(within(form).getByRole("button", { name: "Set a new value" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

  const sent = { space: "ops", name: "db-password", value_base64: "bjN3LXMzY3JldA==" };
  expect(api.requests(SecretSet)).toEqual([sent, sent, sent]);
  expect(new Set(idempotencyKeys(api, SecretSet)).size).toBe(1);
  expect(await screen.findByText("ops/db-password has its new value.")).toBeDefined();
});

test("a value goes into neither the URL nor the browser's storage", async () => {
  const api = await asBob();
  answerShow(api);
  api.on(SecretAdd, ({ space, name }) => ({ secret: secret(space, name) }));
  api.on(SecretSet, ({ space, name }) => ({ secret: secret(space, name) }));

  await show("ops/db-password");
  fireEvent.click(screen.getByRole("button", { name: "Add Secret" }));
  fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "api-key" } });
  fireEvent.change(screen.getByRole("textbox", { name: "Value" }), { target: { value: "added-marker" } });
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Add Secret" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  await chooseRowAction("More actions for bob/token", "Set a new value");
  fireEvent.change(screen.getByRole("textbox", { name: "New value" }), { target: { value: "set-marker" } });
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Set a new value" }));
  await waitFor(() => expect(api.requests(SecretAdd).length + api.requests(SecretSet).length).toBe(2));

  expect(window.location.pathname + window.location.search + window.location.hash).toBe("/secrets");
  expect(localStorage.length + sessionStorage.length).toBe(0);
});

test("Rename never asks, and starts from the Secret's name", async () => {
  const api = await asBob();
  api.on(SecretRename, ({ space, new_name }) => ({ secret: secret(space, new_name) }));

  await chooseRowAction("More actions for ops/db-password", "Rename");
  const form = screen.getByRole("dialog", { name: "Rename ops/db-password" });
  const field = within(form).getByRole<HTMLInputElement>("textbox", { name: "New name" });
  expect(field.value).toBe("db-password");
  fireEvent.change(field, { target: { value: "db-pass" } });
  await expectAccessible();
  fireEvent.click(within(form).getByRole("button", { name: "Rename" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

  expect(api.requests(SecretRename)).toEqual([{ space: "ops", name: "db-password", new_name: "db-pass" }]);
  expect(await screen.findByText("Renamed to db-pass.")).toBeDefined();
  await waitFor(() => expect(api.requests(SecretList)).toHaveLength(2));
});

test("Move never asks, and offers the caller's other Spaces to search", async () => {
  const api = await asBob();
  api.on(SecretMove, ({ new_space, name }) => ({ secret: secret(new_space, name) }));

  await chooseRowAction("More actions for ops/db-password", "Move");
  const form = screen.getByRole("dialog", { name: "Move ops/db-password" });
  expect(form.textContent).toContain("It is in ops.");
  await searchAndChoose("Move to", "", "web", ["bobPersonal Space", "web"]);
  await expectAccessible();
  fireEvent.click(within(form).getByRole("button", { name: "Move" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

  expect(api.requests(SecretMove)).toEqual([{ space: "ops", name: "db-password", new_space: "web" }]);
  expect(await screen.findByText("db-password is now in web.")).toBeDefined();
  await waitFor(() => expect(api.requests(SecretList)).toHaveLength(2));
});

test("a refused write says why, in the dialog it came from, the form kept as typed", async () => {
  const api = await asBob();
  api.on(SecretRename, () => {
    throw refusal(CodeNameTaken, 409);
  });

  await chooseRowAction("More actions for ops/db-password", "Rename");
  const form = screen.getByRole("dialog", { name: "Rename ops/db-password" });
  fireEvent.change(within(form).getByRole("textbox", { name: "New name" }), { target: { value: "token" } });
  fireEvent.click(within(form).getByRole("button", { name: "Rename" }));

  expect(await within(form).findByText("This name is already in use.")).toBeDefined();
  expect(within(form).getByRole<HTMLInputElement>("textbox", { name: "New name" }).value).toBe("token");
  await expectAccessible();
});

test("Delete asks first, saying what it does, and deletes only once confirmed", async () => {
  const api = await asBob();
  api.on(SecretRemove, ({ space, name }) => ({ secret: secret(space, name) }));

  await chooseRowAction("More actions for ops/db-password", "Delete");
  const dialog = screen.getByRole("alertdialog", { name: "Delete ops/db-password" });
  expect(dialog.textContent).toContain(
    "ops/db-password and its value are deleted permanently. koko keeps no earlier value, so the Secret cannot be restored.",
  );
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("alertdialog", { name: "Delete ops/db-password" })).toBeNull());
  expect(api.requests(SecretRemove)).toEqual([]);

  await chooseRowAction("More actions for ops/db-password", "Delete");
  fireEvent.click(within(screen.getByRole("alertdialog", { name: "Delete ops/db-password" })).getByRole("button", { name: "Delete" }));

  await waitFor(() => expect(api.requests(SecretRemove)).toEqual([{ space: "ops", name: "db-password" }]));
  expect(await screen.findByText("ops/db-password is deleted.")).toBeDefined();
  await waitFor(() => expect(api.requests(SecretList)).toHaveLength(2));
});
