import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, onTestFinished, test, vi } from "vitest";

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
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { actAs, renderAt, tableRows } from "../../test/pages";

// The Secrets page, against the fake API: names per Space, a value revealed
// only on a click and hidden again, and the writes.

const bob: User = { id: "user_2", name: "bob", role: "member", created_at: "2026-11-02T09:00:00Z" };

function secret(space: string, name: string): Secret {
  return { id: `secret_${space}_${name}`, space, name, created_at: "2026-11-04T09:00:00Z", updated_at: "2026-11-05T09:00:00Z" };
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
async function asBob(secrets: Secret[] = [secret("bob", "token"), secret("ops", "db-password")]): Promise<FakeApi> {
  const api = fakeApi();
  actAs(api, bob);
  api.on(SpaceList, () => ({ spaces }));
  api.on(SecretList, () => ({ secrets }));
  renderAt("/secrets");
  await screen.findByRole("table", { name: "Secrets" });
  return api;
}

test("a Member sees the names of the Secrets in their Spaces, and no value until one is clicked", async () => {
  const api = await asBob();

  expect(rows().map((cells) => cells.slice(0, 4))).toEqual([
    ["bob", "token", "2026-11-05", "Show"],
    ["ops", "db-password", "2026-11-05", "Show"],
  ]);
  // The koko server answers only the Spaces the caller can act in.
  expect(api.requests(SecretList)).toEqual([{}]);
  expect(api.requests(SecretShow)).toEqual([]);
  expect(within(screen.getByRole("banner")).getByRole("link", { name: "Secrets" })).toBeDefined();
  const choice = screen.getByRole<HTMLSelectElement>("combobox", { name: "Space" });
  expect(Array.from(choice.options, (o) => o.value)).toEqual(["bob", "ops", "web"]);
  expect(choice.value).toBe("bob");
});

test("a caller whose Spaces hold no Secret is told so", async () => {
  const api = fakeApi();
  actAs(api, bob);
  api.on(SpaceList, () => ({ spaces }));
  api.on(SecretList, () => ({ secrets: [] }));

  renderAt("/secrets");

  expect(await screen.findByText("Your Spaces hold no Secret yet.")).toBeDefined();
  expect(screen.queryByRole("table", { name: "Secrets" })).toBeNull();
});

/** answerShow makes secret.show answer every Secret with the value whose base64 is valueBase64. */
function answerShow(api: FakeApi, valueBase64: string): void {
  api.on(SecretShow, ({ space, name }) => ({ secret: secret(space, name), value_base64: valueBase64 }));
}

/** reveal clicks Show on the Secret named name, <space>/<secret>, and returns its value once shown. */
async function reveal(name: string): Promise<HTMLElement> {
  fireEvent.click(screen.getByRole("button", { name: `Show the value of ${name}` }));
  return screen.findByRole("group", { name: `Value of ${name}` });
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
  onTestFinished(() => {
    vi.useRealTimers();
  });
}

/**
 * hideTimerStarted waits until the revealed value's timer runs: React
 * starts it in an effect that may run only after the value shows (#79). It
 * looks with vi.waitUntil, on the real clock, with an interval of 0, which
 * never moves the faked clock.
 */
async function hideTimerStarted(): Promise<void> {
  await vi.waitUntil(() => vi.getTimerCount() > 0, { interval: 0, message: "the revealed value's timer never started" });
}

test("a click reveals the value, never asking, and 30 seconds later it is gone", async () => {
  fakeTimeouts();
  const api = await asBob();
  answerShow(api, "czNjcmV0LW1hcmtlcgo="); // s3cret-marker and a newline

  const shown = await reveal("ops/db-password");

  expect(screen.queryByRole("dialog")).toBeNull();
  expect(api.requests(SecretShow)).toEqual([{ space: "ops", name: "db-password" }]);
  expect(within(shown).getByText("s3cret-marker", { exact: false })).toBeDefined();

  // The faked clock also runs on with the real one, so the value is looked
  // for well before its 30 seconds, leaving a loaded runner time to spare.
  await hideTimerStarted();
  act(() => vi.advanceTimersByTime(25_000));
  expect(screen.getByRole("group", { name: "Value of ops/db-password" })).toBeDefined();
  act(() => vi.advanceTimersByTime(5_000));
  expect(screen.queryByRole("group", { name: "Value of ops/db-password" })).toBeNull();
  expect(document.body.textContent).not.toContain("s3cret-marker");
  expect(screen.getByRole("button", { name: "Show the value of ops/db-password" })).toBeDefined();
});

test("Copy copies the value shown, Hide hides it at once, and revealing another hides the first", async () => {
  const writeText = clipboard();
  const api = await asBob();
  answerShow(api, "czNjcmV0LW1hcmtlcgo=");

  const first = await reveal("ops/db-password");
  fireEvent.click(within(first).getByRole("button", { name: "Copy" }));
  expect(await within(first).findByText("Copied.")).toBeDefined();
  expect(writeText.mock.calls).toEqual([["s3cret-marker\n"]]);
  fireEvent.click(within(first).getByRole("button", { name: "Hide" }));
  expect(screen.queryByRole("group", { name: "Value of ops/db-password" })).toBeNull();

  await reveal("ops/db-password");
  await reveal("bob/token");
  expect(screen.queryByRole("group", { name: "Value of ops/db-password" })).toBeNull();
  expect(screen.getAllByText("s3cret-marker", { exact: false })).toHaveLength(1);
});

test("a value that arrives after a later reveal, a hide, a write or the browser leaving the page never shows", async () => {
  const api = await asBob();
  api.on(SecretSet, ({ space, name }) => ({ secret: secret(space, name) }));
  const answers: (() => void)[] = [];
  api.on(SecretShow, ({ space, name }) =>
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
  expect(screen.getByRole("group", { name: "Value of bob/token" })).toBeDefined();
  expect(screen.queryByRole("group", { name: "Value of ops/db-password" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Hide" }));

  // The browser leaves the page while secret.show is answering.
  fireEvent.click(screen.getByRole("button", { name: "Show the value of ops/db-password" }));
  fireEvent(window, new Event("pagehide"));
  await answerAll();
  expect(document.body.textContent).not.toContain("s3cret-marker");

  // The value is set while secret.show is answering with the old one.
  fireEvent.click(screen.getByRole("button", { name: "Show the value of ops/db-password" }));
  fireEvent.click(screen.getByRole("button", { name: "Set the value of ops/db-password" }));
  fireEvent.change(screen.getByRole("textbox", { name: "New value" }), { target: { value: "n3w-s3cret" } });
  fireEvent.click(screen.getByRole("button", { name: "Set value" }));
  await waitFor(() => expect(screen.queryByRole("region", { name: "Set the value of ops/db-password" })).toBeNull());
  await answerAll();
  expect(document.body.textContent).not.toContain("s3cret-marker");
});

test("leaving the page hides the value, and coming back shows it no more", async () => {
  const api = await asBob();
  answerShow(api, "czNjcmV0LW1hcmtlcgo=");
  await reveal("ops/db-password");

  const nav = within(screen.getByRole("banner"));
  fireEvent.click(nav.getByRole("link", { name: "Spaces" }));
  await screen.findByRole("heading", { name: "Spaces" });
  expect(document.body.textContent).not.toContain("s3cret-marker");
  fireEvent.click(nav.getByRole("link", { name: "Secrets" }));
  await screen.findByRole("table", { name: "Secrets" });

  expect(screen.queryByRole("group", { name: "Value of ops/db-password" })).toBeNull();
  expect(document.body.textContent).not.toContain("s3cret-marker");
  expect(api.requests(SecretShow)).toHaveLength(1);
});

test("the browser leaving the page forgets the value, even when it keeps the page to come back to", async () => {
  const api = await asBob();
  answerShow(api, "czNjcmV0LW1hcmtlcgo=");
  await reveal("ops/db-password");

  fireEvent(window, new Event("pagehide"));

  expect(screen.queryByRole("group", { name: "Value of ops/db-password" })).toBeNull();
  expect(document.body.textContent).not.toContain("s3cret-marker");
});

test("a value that is not UTF-8 shows as its base64, which Copy copies", async () => {
  const writeText = clipboard();
  const api = await asBob();
  answerShow(api, "/wAB"); // the bytes ff 00 01

  const shown = await reveal("ops/db-password");

  expect(shown.textContent).toContain("Not UTF-8 text: shown in base64");
  expect(within(shown).getByText("/wAB")).toBeDefined();
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

  expect((await within(shown).findByRole("status")).textContent).toBe("Could not copy: select the value and copy it.");
});

/** idempotencyKeys are the Idempotency-Keys of every call of op, in the order they came. */
function idempotencyKeys(api: FakeApi, op: { path: string }): (string | undefined)[] {
  return api.calls.filter((c) => c.path === op.path).map((c) => c.headers["idempotency-key"]);
}

test("adding never asks, sends the typed value as base64 of its UTF-8, and a retry after a drop resends its Idempotency-Key", async () => {
  const api = await asBob();
  api.on(SecretAdd, ({ space, name }) => ({ secret: secret(space, name) }));
  fireEvent.change(screen.getByRole("combobox", { name: "Space" }), { target: { value: "ops" } });
  fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "api-key" } });
  fireEvent.change(screen.getByRole("textbox", { name: "Value" }), { target: { value: "pässwörd\n" } });

  api.drop(2); // the call and its own retry
  fireEvent.click(screen.getByRole("button", { name: "Add Secret" }));
  expect(await screen.findByText("The connection to koko web dropped.")).toBeDefined();
  fireEvent.click(screen.getByRole("button", { name: "Add Secret" }));
  await waitFor(() => expect(screen.getByRole<HTMLTextAreaElement>("textbox", { name: "Value" }).value).toBe(""));

  expect(screen.queryByRole("dialog")).toBeNull();
  const sent = { space: "ops", name: "api-key", value_base64: "cMOkc3N3w7ZyZAo=" };
  expect(api.requests(SecretAdd)).toEqual([sent, sent, sent]);
  const keys = idempotencyKeys(api, SecretAdd);
  expect(keys[0]).toMatch(/^[0-9a-f]{32}$/);
  expect(new Set(keys).size).toBe(1);
  expect(screen.getByRole<HTMLInputElement>("textbox", { name: "Name" }).value).toBe("");
  await waitFor(() => expect(api.requests(SecretList)).toHaveLength(2));
});

test("setting a value never asks, hides the value shown, and a retry after a drop resends its Idempotency-Key", async () => {
  const api = await asBob();
  answerShow(api, "czNjcmV0LW1hcmtlcgo=");
  api.on(SecretSet, ({ space, name }) => ({ secret: secret(space, name) }));
  await reveal("ops/db-password");

  fireEvent.click(screen.getByRole("button", { name: "Set the value of ops/db-password" }));
  const form = screen.getByRole("region", { name: "Set the value of ops/db-password" });
  fireEvent.change(within(form).getByRole("textbox", { name: "New value" }), { target: { value: "n3w-s3cret" } });
  api.drop(2);
  fireEvent.click(within(form).getByRole("button", { name: "Set value" }));
  expect(await within(form).findByText("The connection to koko web dropped.")).toBeDefined();
  fireEvent.click(within(form).getByRole("button", { name: "Set value" }));
  await waitFor(() => expect(screen.queryByRole("region", { name: "Set the value of ops/db-password" })).toBeNull());

  expect(screen.queryByRole("dialog")).toBeNull();
  const sent = { space: "ops", name: "db-password", value_base64: "bjN3LXMzY3JldA==" };
  expect(api.requests(SecretSet)).toEqual([sent, sent, sent]);
  expect(new Set(idempotencyKeys(api, SecretSet)).size).toBe(1);
  expect(screen.queryByRole("group", { name: "Value of ops/db-password" })).toBeNull();
  expect(document.body.textContent).not.toContain("s3cret-marker");
});

test("a value goes into neither the URL nor the browser's storage", async () => {
  const api = await asBob();
  answerShow(api, "czNjcmV0LW1hcmtlcgo=");
  api.on(SecretAdd, ({ space, name }) => ({ secret: secret(space, name) }));
  api.on(SecretSet, ({ space, name }) => ({ secret: secret(space, name) }));

  await reveal("ops/db-password");
  fireEvent.change(screen.getByRole("textbox", { name: "Name" }), { target: { value: "api-key" } });
  fireEvent.change(screen.getByRole("textbox", { name: "Value" }), { target: { value: "added-marker" } });
  fireEvent.click(screen.getByRole("button", { name: "Add Secret" }));
  fireEvent.click(screen.getByRole("button", { name: "Set the value of bob/token" }));
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

  fireEvent.click(screen.getByRole("button", { name: "Rename ops/db-password" }));
  fireEvent.change(screen.getByRole("textbox", { name: "New name" }), { target: { value: "db-pass" } });
  fireEvent.click(screen.getByRole("button", { name: "Rename" }));
  await waitFor(() => expect(screen.queryByRole("region", { name: "Rename ops/db-password" })).toBeNull());

  fireEvent.click(screen.getByRole("button", { name: "Move ops/db-password" }));
  const to = screen.getByRole<HTMLSelectElement>("combobox", { name: "To Space" });
  expect(Array.from(to.options, (o) => o.value)).toEqual(["", "bob", "web"]);
  fireEvent.change(to, { target: { value: "web" } });
  fireEvent.click(screen.getByRole("button", { name: "Move" }));
  await waitFor(() => expect(screen.queryByRole("region", { name: "Move ops/db-password" })).toBeNull());

  expect(screen.queryByRole("dialog")).toBeNull();
  expect(api.requests(SecretRename)).toEqual([{ space: "ops", name: "db-password", new_name: "db-pass" }]);
  expect(api.requests(SecretMove)).toEqual([{ space: "ops", name: "db-password", new_space: "web" }]);
  await waitFor(() => expect(api.requests(SecretList)).toHaveLength(3));
});

test("a refused write says why, in the form it came from", async () => {
  const api = await asBob();
  api.on(SecretRename, () => {
    throw refusal(CodeNameTaken, 409);
  });

  fireEvent.click(screen.getByRole("button", { name: "Rename ops/db-password" }));
  const form = screen.getByRole("region", { name: "Rename ops/db-password" });
  fireEvent.change(within(form).getByRole("textbox", { name: "New name" }), { target: { value: "token" } });
  fireEvent.click(within(form).getByRole("button", { name: "Rename" }));

  expect(await within(form).findByText("That name is taken.")).toBeDefined();
});

test("deleting asks first, saying what it does, and deletes only once confirmed", async () => {
  const api = await asBob();
  api.on(SecretRemove, ({ space, name }) => ({ secret: secret(space, name) }));

  fireEvent.click(screen.getByRole("button", { name: "Delete ops/db-password" }));
  const dialog = screen.getByRole("dialog", { name: "Delete ops/db-password?" });
  expect(dialog.textContent).toContain(
    "ops/db-password and its value are deleted for good. koko keeps no earlier value, so nothing brings it back.",
  );
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(api.requests(SecretRemove)).toEqual([]);

  fireEvent.click(screen.getByRole("button", { name: "Delete ops/db-password" }));
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }));

  await waitFor(() => expect(api.requests(SecretRemove)).toEqual([{ space: "ops", name: "db-password" }]));
  await waitFor(() => expect(api.requests(SecretList)).toHaveLength(2));
});
