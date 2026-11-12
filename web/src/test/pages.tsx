import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeAll, expect, onTestFinished } from "vitest";

import type { DeviceSession } from "../api/device";
import { type User, UserCurrent } from "../api/user";
import { App } from "../App";
import { routes } from "../routes";
import { loadPage } from "../shell/lazyPage";
import { WorkspaceLayer } from "../terminal/routes";
import type { FakeApi } from "./fakeApi";

// Helpers for the tests of the web UI's pages, which run the whole app, its
// shell and every page, against the fake API.

// Each page is a chunk of its own (lazyPage), and the first import of one
// in a test file can take longer on a busy runner than a test's wait for
// what the page shows. Every test file that renders pages through these
// helpers loads all of them first, so a test waits for the page itself,
// never for its module to load. The hook's limit is a ceiling for a slow
// runner, not a wait: it ends as soon as the pages have loaded.
beforeAll(async () => {
  await Promise.all([...routes.map((route) => loadPage(route.page)), loadPage(WorkspaceLayer)]);
}, 60_000);

/** preferLanguages makes the browser prefer these languages for one test. */
export function preferLanguages(...preferred: string[]): void {
  Object.defineProperty(navigator, "languages", { value: preferred, configurable: true });
  onTestFinished(() => {
    Reflect.deleteProperty(navigator, "languages");
  });
}

/** renderAt renders the web UI, with all its pages, at path. */
export function renderAt(path: string) {
  window.history.replaceState(null, "", path);
  return render(<App />);
}

/**
 * actAs makes user.current answer user on deviceSession: by default a
 * Device Session of their laptop.
 */
export function actAs(
  api: FakeApi,
  user: User,
  deviceSession: DeviceSession = { id: "device_1", user: user.name, device: "laptop", created_at: "2026-11-03T09:00:00Z" },
): void {
  api.on(UserCurrent, () => ({ user, device_session: deviceSession }));
}

/**
 * alerts are the text of each alert in the page with something to say, in
 * document order, leaving out a live region React Aria keeps empty until it
 * announces something.
 */
export function alerts(): string[] {
  return saying("alert");
}

/** statuses are the text of each status in the page with something to say, as alerts are of alerts. */
export function statuses(): string[] {
  return saying("status");
}

function saying(role: "alert" | "status"): string[] {
  return screen
    .queryAllByRole(role)
    .map((region) => region.textContent ?? "")
    .filter((text) => text !== "");
}

/** findAlert waits for an alert with something to say, and is the first one's text. */
export async function findAlert(): Promise<string> {
  return waitFor(() => {
    const [first] = alerts();
    if (first === undefined) {
      throw new Error("no alert says anything");
    }
    return first;
  });
}

/**
 * frameButton is the button of the frame's banner whose name matches name,
 * such as /^Language/: in the rail on a desktop, in the top row on a phone.
 */
export function frameButton(name: string | RegExp): HTMLElement {
  return within(screen.getByRole("banner")).getByRole("button", { name });
}

/**
 * chooseInFrame opens the frame's menu behind the button named button,
 * such as Language or Appearance, and chooses its option named option.
 */
export async function chooseInFrame(button: "Language" | "Appearance" | "语言" | "外观", option: string): Promise<void> {
  fireEvent.click(frameButton(new RegExp(`^${button}[:：]`)));
  fireEvent.click(await screen.findByRole("menuitemradio", { name: option }));
  await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
}

/** tableRows are the text of each cell of each row of the table named name, its header row aside. */
export function tableRows(name: string): string[][] {
  return within(screen.getByRole("table", { name }))
    .getAllByRole("row")
    .slice(1)
    .map((row) => within(row).getAllByRole("cell").map((cell) => cell.textContent ?? ""));
}
