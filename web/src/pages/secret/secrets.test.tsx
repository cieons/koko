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
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { chooseOption, chooseRowAction } from "../../test/controls";
import { actAs, renderAt, tableRows } from "../../test/pages";

// The Secrets page, against the fake API: names per Space, searched in
// place, a value revealed only on a click, in a dialog that forgets it, and
// the writes, each in a dialog of its own.

const bob: User = { id: "user_2", name: "bob", role: "member", created_at: "2026-11-02T09:00:00Z" };

function secret(space: string, name: string): Secret {
  return {
    id: `secret_${space}_${name}`,
    space,
    name,
    created_at: "2026-11-04T09:00:00Z",
    updated_at: "2026-11-05T09:00:00Z",
  };
}

const spaces: Space[] = [
  { id: "space_1", name: "bob", personal: true },
  { id: "space_2", name: "ops", personal: false },
  { id: "space_3", name: "web", personal: false },
];

const rows = () => tableRows("Secrets");

/**
 * asBob renders the Secrets page as bob, a Member of ops and web, whose
 * Spaces hold the Secrets secrets, as secret.list answers them.
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
  await screen.findByRole("table", { name: "Secrets" });
  return api;
}

test("a Member sees the names of the Secrets in their Spaces, and no value until one is asked for", async () => {
  const api = await asBob();

  expect(rows().map((cells) => cells.slice(0, 4))).toEqual([
    ["bob", "token", "2026-11-05", "Show"],
    ["ops", "db-password", "2026-11-05", "Show"],
  ]);
  expect(screen.getByRole("heading", { level: 1, name: "Secrets" })).toBeDefined();
  // The koko server answers only the Spaces the caller can act in.
  expect(api.requests(SecretList)).toEqual([{}]);
  expect(api.requests(SecretShow)).toEqual([]);
  expect(
    within(screen.getByRole("navigation", { name: "Pages" })).getByRole("link", { name: "Secrets" }),
  ).toBeDefined();
  await expectAccessible();
});

test("a caller whose Spaces hold no Secret is told so, and may add one", async () => {
  const api = fakeApi();
  actAs(api, bob);
  api.on(SpaceList, () => ({ spaces }));
  api.on(SecretList, () => ({ secrets: [] }));

  renderAt("/secrets");

  expect(await screen.findByRole("heading", { name: "No Secrets" })).toBeDefined();
  expect(screen.queryByRole("table", { name: "Secrets" })).toBeNull();
  expect(screen.getByRole("button", { name: "Add Secret" })).toBeDefined();
  await expectAccessible();
});

test("the search narrows the list by Space or name, kept in the URL, and says when nothing matches", async () => {
  await asBob([secret("bob", "token"), secret("ops", "db-password"), secret("web", "tls-key")]);
  const box = screen.getByRole("searchbox", { name: "Search Secrets" });

  fireEvent.change(box, { target: { value: "OPS/" } });
  expect(rows().map((cells) => cells[1])).toEqual(["db-password"]);
  expect(window.location.search).toBe("?q=OPS%2F");

  fireEvent.change(box, { target: { value: "key" } });
  expect(rows().map((cells) => cells[1])).toEqual(["tls-key"]);

  fireEvent.change(box, { target: { value: "nothing" } });
  expect(screen.getByRole("heading", { name: "No matching Secrets" })).toBeDefined();
  expect(screen.getByText("No Secret matches nothing.")).toBeDefined();
  await expectAccessible();
  fireEvent.click(screen.getByRole("button", { name: "Clear search" }));
  expect(rows()).toHaveLength(3);
  expect(window.location.search).toBe("");
});

test("a bookmark with a search opens the list narrowed", async () => {
  await asBob([secret("bob", "token"), secret("ops", "db-password")], "/secrets?q=token");

  expect(rows().map((cells) => cells[1])).toEqual(["token"]);
  expect(screen.getByRole<HTMLInputElement>("searchbox", { name: "Search Secrets" }).value).toBe("token");
});

/** answerShow makes secret.show answer every Secret with the value whose base64 is valueBase64. */
function answerShow(api: FakeApi, valueBase64: string): void {
  api.on(SecretShow, ({ space, name }) => ({ secret: secret(space, name), value_base64: valueBase64 }));
}

/** valueDialog is the dialog of the value of name, <space>/<secret>, if open. */
function valueDialog(name: string): HTMLElement | null {
  return screen.queryByRole("dialog", { name: `Value of ${name}` });
}

/** reveal clicks Show on the Secret named name, <space>/<secret>, and returns its dialog once the value shows. */
async function reveal(name: string, text = "s3cret-marker"): Promise<HTMLElement> {
  fireEvent.click(screen.getByRole("button", { name: `Show the value of ${name}` }));
  const dialog = await screen.findByRole("dialog", { name: `Value of ${name}` });
  await within(dialog).findByText(text, { exact: false });
  return dialog;
}

/**
 * clipboard stands in for the browser's clipboard for one test, or, when
 * not available, takes it away, as a browser without the API does.
 */
function clipboard(available = true) {
  const writeText = vi.fn((_: string) => Promise.resolve());
  Object.defineProperty(navigator, "clipboard", { value: available ? { writeText } : undefined, configurable: true });
  onTestFinished(() => {
    Reflect.deleteProperty(navigator, "clipboard");
  });
  return writeText;
}

/**
 * fakeTimeouts fakes setTimeout for one test, so a test moves the time a
 * revealed value stays on screen. The faked clock also runs on with the real
 * one, so the testing library's own short waits still end.
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
 * hideTimerStarted waits until the revealed value's own timer of 30 seconds
 * runs, not any other, such as one of Astryx's components: React starts it
 * in an effect that may run only after the value shows (#79). It looks with
 * vi.waitUntil, on the real clock, with an interval of 0, which never moves
 * the faked clock.
 */
async function hideTimerStarted(): Promise<void> {
  await vi.waitUntil(() => timeouts?.mock.calls.some(([, delay]) => delay === 30_000), {
    interval: 0,
    message: "the revealed value's timer never started",
  });
}

test("Show reveals the value in a dialog, never asking, and 30 seconds later it is gone", async () => {
  fakeTimeouts();
  const api = await asBob();
  answerShow(api, "czNjcmV0LW1hcmtlcgo="); // s3cret-marker and a newline

  const shown = await reveal("ops/db-password");

  expect(screen.queryByRole("alertdialog")).toBeNull();
  expect(api.requests(SecretShow)).toEqual([{ space: "ops", name: "db-password" }]);
  expect(within(shown).getByText("s3cret-marker", { exact: false }).tagName).toBe("CODE");
  await expectAccessible();

  // The faked clock also runs on with the real one, so the value is looked
  // for well before its 30 seconds, leaving a loaded runner time to spare.
  await hideTimerStarted();
  act(() => vi.advanceTimersByTime(25_000));
  expect(valueDialog("ops/db-password")).not.toBeNull();
  act(() => vi.advanceTimersByTime(5_000));
  expect(valueDialog("ops/db-password")).toBeNull();
  expect(document.body.textContent).not.toContain("s3cret-marker");
  expect(screen.getByRole("button", { name: "Show the value of ops/db-password" })).toBeDefined();
});

test("Copy copies the value shown, Hide forgets it at once, and the next reveal asks again", async () => {
  const writeText = clipboard();
  const api = await asBob();
  answerShow(api, "czNjcmV0LW1hcmtlcgo=");

  const first = await reveal("ops/db-password");
  fireEvent.click(within(first).getByRole("button", { name: "Copy" }));
  expect(await within(first).findByText("Copied.")).toBeDefined();
  expect(writeText.mock.calls).toEqual([["s3cret-marker\n"]]);
  fireEvent.click(within(first).getByRole("button", { name: "Hide" }));
  expect(valueDialog("ops/db-password")).toBeNull();
  expect(document.body.textContent).not.toContain("s3cret-marker");

  const second = await reveal("bob/token");
  fireEvent.click(within(second).getByRole("button", { name: "Close" }));
  expect(valueDialog("bob/token")).toBeNull();
  expect(document.body.textContent).not.toContain("s3cret-marker");
  expect(api.requests(SecretShow)).toHaveLength(2);
});

test("a refused reveal says why in its dialog", async () => {
  const api = await asBob();
  api.on(SecretShow, () => {
    throw refusal("unknown_secret", 404);
  });

  fireEvent.click(screen.getByRole("button", { name: "Show the value of ops/db-password" }));

  const dialog = await screen.findByRole("dialog", { name: "Value of ops/db-password" });
  expect(await within(dialog).findByText("The Space holds no Secret with that name.")).toBeDefined();
  await expectAccessible();
});

test("a value that arrives after a later reveal, a hide, a write or the browser leaving the page never shows", async () => {
  const api = await asBob();
  api.on(SecretSet, ({ space, name }) => ({ secret: secret(space, name) }));
  const answers: (() => void)[] = [];
  api.on(
    SecretShow,
    ({ space, name }) =>
      new Promise((resolve) => {
        answers.push(() => resolve({ secret: secret(space, name), value_base64: "czNjcmV0LW1hcmtlcgo=" }));
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

  // A Show clicked after another wins, whichever answer comes last.
  fireEvent.click(screen.getByRole("button", { name: "Show the value of ops/db-password" }));
  fireEvent.click(screen.getByRole("button", { name: "Show the value of bob/token" }));
  await waitFor(() => expect(answers).toHaveLength(2));
  await act(async () => answers.splice(1)[0]?.());
  await act(async () => answers.splice(0)[0]?.());
  expect(valueDialog("bob/token")).not.toBeNull();
  expect(valueDialog("ops/db-password")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Hide" }));

  // The dialog is closed while secret.show is answering.
  fireEvent.click(screen.getByRole("button", { name: "Show the value of ops/db-password" }));
  fireEvent.click(
    within(screen.getByRole("dialog", { name: "Value of ops/db-password" })).getByRole("button", { name: "Hide" }),
  );
  await answerAll();
  expect(document.body.textContent).not.toContain("s3cret-marker");

  // The browser leaves the page while secret.show is answering.
  fireEvent.click(screen.getByRole("button", { name: "Show the value of ops/db-password" }));
  fireEvent(window, new Event("pagehide"));
  await answerAll();
  expect(document.body.textContent).not.toContain("s3cret-marker");

  // The value is set, from the row's menu, while secret.show is answering
  // with the old one.
  fireEvent.click(screen.getByRole("button", { name: "Show the value of ops/db-password" }));
  await chooseRowAction("Actions for ops/db-password", "Set value");
  const form = screen.getByRole("dialog", { name: "Set the value of ops/db-password" });
  fireEvent.change(within(form).getByRole("textbox", { name: "New value" }), { target: { value: "n3w-s3cret" } });
  fireEvent.click(within(form).getByRole("button", { name: "Set value" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Set the value of ops/db-password" })).toBeNull());
  await answerAll();
  expect(document.body.textContent).not.toContain("s3cret-marker");
});

test("leaving the page forgets the value, and coming back shows it no more", async () => {
  const api = await asBob();
  answerShow(api, "czNjcmV0LW1hcmtlcgo=");
  await reveal("ops/db-password");

  // A visit elsewhere in the web UI, then back.
  act(() => window.history.pushState(null, "", "/spaces"));
  act(() => window.dispatchEvent(new PopStateEvent("popstate")));
  await screen.findByRole("heading", { name: "Spaces" });
  expect(document.body.textContent).not.toContain("s3cret-marker");
  act(() => window.history.pushState(null, "", "/secrets"));
  act(() => window.dispatchEvent(new PopStateEvent("popstate")));
  await screen.findByRole("table", { name: "Secrets" });

  expect(valueDialog("ops/db-password")).toBeNull();
  expect(document.body.textContent).not.toContain("s3cret-marker");
  expect(api.requests(SecretShow)).toHaveLength(1);
});

test("the browser leaving the page forgets the value, even when it keeps the page to come back to", async () => {
  const api = await asBob();
  answerShow(api, "czNjcmV0LW1hcmtlcgo=");
  await reveal("ops/db-password");

  fireEvent(window, new Event("pagehide"));

  expect(valueDialog("ops/db-password")).toBeNull();
  expect(document.body.textContent).not.toContain("s3cret-marker");
});

test("a value that is not UTF-8 shows as its base64, which Copy copies", async () => {
  const writeText = clipboard();
  const api = await asBob();
  answerShow(api, "/wAB"); // the bytes ff 00 01

  const shown = await reveal("ops/db-password", "/wAB");

  expect(shown.textContent).toContain("This value is not UTF-8 text. It is shown in base64");
  fireEvent.click(within(shown).getByRole("button", { name: "Copy" }));
  expect(await within(shown).findByText("Copied.")).toBeDefined();
  expect(writeText.mock.calls).toEqual([["/wAB"]]);
});

test("a browser that cannot copy says to select the value instead", async () => {
  clipboard(false);
  const api = await asBob();
  answerShow(api, "czNjcmV0LW1hcmtlcgo=");
  const shown = await reveal("ops/db-password");

  fireEvent.click(within(shown).getByRole("button", { name: "Copy" }));

  await waitFor(() =>
    expect(
      within(shown)
        .getAllByRole("status")
        .map((s) => s.textContent),
    ).toContain("This browser did not allow copying. Select the text and copy it."),
  );
});

/** idempotencyKeys are the Idempotency-Keys of every call of op, in the order they came. */
function idempotencyKeys(api: FakeApi, op: { path: string }): (string | undefined)[] {
  return api.calls.filter((c) => c.path === op.path).map((c) => c.headers["idempotency-key"]);
}

test("adding never asks, sends the typed value as base64 of its UTF-8, and a retry after a drop resends its Idempotency-Key", async () => {
  const api = await asBob();
  api.on(SecretAdd, ({ space, name }) => ({ secret: secret(space, name) }));

  fireEvent.click(screen.getByRole("button", { name: "Add Secret" }));
  const form = screen.getByRole("dialog", { name: "Add Secret" });
  // bob's Personal Space comes first, chosen; any Space of theirs may be.
  expect(within(form).getByRole("combobox", { name: "Space" }).textContent).toContain("bob");
  await chooseOption(within(form).getByRole("combobox", { name: "Space" }), "ops", ["bob", "ops", "web"]);
  fireEvent.change(within(form).getByRole("textbox", { name: "Name" }), { target: { value: "api-key" } });
  fireEvent.change(within(form).getByRole("textbox", { name: "Value" }), { target: { value: "pässwörd\n" } });
  await expectAccessible();

  api.drop(2); // the call and its own retry
  fireEvent.click(within(form).getByRole("button", { name: "Add" }));
  expect(await within(form).findByText("The connection to koko web was lost.")).toBeDefined();
  fireEvent.click(within(form).getByRole("button", { name: "Add" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add Secret" })).toBeNull());

  expect(screen.queryByRole("alertdialog")).toBeNull();
  const sent = { space: "ops", name: "api-key", value_base64: "cMOkc3N3w7ZyZAo=" };
  expect(api.requests(SecretAdd)).toEqual([sent, sent, sent]);
  const keys = idempotencyKeys(api, SecretAdd);
  expect(keys[0]).toMatch(/^[0-9a-f]{32}$/);
  expect(new Set(keys).size).toBe(1);
  await waitFor(() => expect(api.requests(SecretList)).toHaveLength(2));
});

test("setting a value never asks, forgets the value shown, and a retry after a drop resends its Idempotency-Key", async () => {
  const api = await asBob();
  answerShow(api, "czNjcmV0LW1hcmtlcgo=");
  api.on(SecretSet, ({ space, name }) => ({ secret: secret(space, name) }));
  const shown = await reveal("ops/db-password");
  fireEvent.click(within(shown).getByRole("button", { name: "Hide" }));

  await chooseRowAction("Actions for ops/db-password", "Set value");
  const form = screen.getByRole("dialog", { name: "Set the value of ops/db-password" });
  fireEvent.change(within(form).getByRole("textbox", { name: "New value" }), { target: { value: "n3w-s3cret" } });
  await expectAccessible();
  api.drop(2);
  fireEvent.click(within(form).getByRole("button", { name: "Set value" }));
  expect(await within(form).findByText("The connection to koko web was lost.")).toBeDefined();
  fireEvent.click(within(form).getByRole("button", { name: "Set value" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Set the value of ops/db-password" })).toBeNull());

  expect(screen.queryByRole("alertdialog")).toBeNull();
  const sent = { space: "ops", name: "db-password", value_base64: "bjN3LXMzY3JldA==" };
  expect(api.requests(SecretSet)).toEqual([sent, sent, sent]);
  expect(new Set(idempotencyKeys(api, SecretSet)).size).toBe(1);
  expect(document.body.textContent).not.toContain("s3cret-marker");
});

test("a value goes into neither the URL nor the browser's storage", async () => {
  const api = await asBob();
  answerShow(api, "czNjcmV0LW1hcmtlcgo=");
  api.on(SecretAdd, ({ space, name }) => ({ secret: secret(space, name) }));
  api.on(SecretSet, ({ space, name }) => ({ secret: secret(space, name) }));

  fireEvent.click(within(await reveal("ops/db-password")).getByRole("button", { name: "Hide" }));
  fireEvent.click(screen.getByRole("button", { name: "Add Secret" }));
  fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "api-key" } });
  fireEvent.change(screen.getByRole("textbox", { name: "Value" }), { target: { value: "added-marker" } });
  fireEvent.click(screen.getByRole("button", { name: "Add" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add Secret" })).toBeNull());
  await chooseRowAction("Actions for bob/token", "Set value");
  fireEvent.change(screen.getByRole("textbox", { name: "New value" }), { target: { value: "set-marker" } });
  fireEvent.click(screen.getByRole("button", { name: "Set value" }));
  await waitFor(() => expect(api.requests(SecretAdd).length + api.requests(SecretSet).length).toBe(2));

  expect(window.location.pathname + window.location.search + window.location.hash).toBe("/secrets");
  expect(localStorage.length + sessionStorage.length).toBe(0);
});

test("renaming and moving never ask", async () => {
  const api = await asBob();
  api.on(SecretRename, ({ space, new_name }) => ({ secret: secret(space, new_name) }));
  api.on(SecretMove, ({ new_space, name }) => ({ secret: secret(new_space, name) }));

  await chooseRowAction("Actions for ops/db-password", "Rename");
  const rename = screen.getByRole("dialog", { name: "Rename ops/db-password" });
  fireEvent.change(within(rename).getByRole("textbox", { name: "New name" }), { target: { value: "db-pass" } });
  await expectAccessible();
  fireEvent.click(within(rename).getByRole("button", { name: "Rename" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Rename ops/db-password" })).toBeNull());

  await chooseRowAction("Actions for ops/db-password", "Move");
  const move = screen.getByRole("dialog", { name: "Move ops/db-password" });
  // The Spaces bob can act in, but the one it is in.
  await chooseOption(within(move).getByRole("combobox", { name: "Destination Space" }), "web", ["bob", "web"]);
  await expectAccessible();
  fireEvent.click(within(move).getByRole("button", { name: "Move" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Move ops/db-password" })).toBeNull());

  expect(screen.queryByRole("alertdialog")).toBeNull();
  expect(api.requests(SecretRename)).toEqual([{ space: "ops", name: "db-password", new_name: "db-pass" }]);
  expect(api.requests(SecretMove)).toEqual([{ space: "ops", name: "db-password", new_space: "web" }]);
  await waitFor(() => expect(api.requests(SecretList)).toHaveLength(3));
});

test("a refused write says why, in the dialog it came from", async () => {
  const api = await asBob();
  api.on(SecretRename, () => {
    throw refusal(CodeNameTaken, 409);
  });

  await chooseRowAction("Actions for ops/db-password", "Rename");
  const form = screen.getByRole("dialog", { name: "Rename ops/db-password" });
  fireEvent.change(within(form).getByRole("textbox", { name: "New name" }), { target: { value: "token" } });
  fireEvent.click(within(form).getByRole("button", { name: "Rename" }));

  expect(await within(form).findByText("This name is already in use.")).toBeDefined();
  await expectAccessible();
});

test("deleting asks first, saying what it does, and deletes only once confirmed", async () => {
  const api = await asBob();
  api.on(SecretRemove, ({ space, name }) => ({ secret: secret(space, name) }));

  await chooseRowAction("Actions for ops/db-password", "Delete");
  const dialog = screen.getByRole("alertdialog", { name: "Delete ops/db-password" });
  expect(dialog.textContent).toContain(
    "ops/db-password and its value are deleted permanently. koko keeps no earlier value, so the Secret cannot be restored.",
  );
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("alertdialog")).toBeNull();
  expect(api.requests(SecretRemove)).toEqual([]);

  await chooseRowAction("Actions for ops/db-password", "Delete");
  fireEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Delete" }));

  await waitFor(() => expect(api.requests(SecretRemove)).toEqual([{ space: "ops", name: "db-password" }]));
  await waitFor(() => expect(api.requests(SecretList)).toHaveLength(2));
});
