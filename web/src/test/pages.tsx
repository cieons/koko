import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeAll } from "vitest";

import type { DeviceSession } from "../api/device";
import { type User, UserCurrent } from "../api/user";
import { App } from "../App";
import { routes } from "../routes";
import { loadPage } from "../shell/lazyPage";
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
  await Promise.all(routes.map((route) => loadPage(route.page)));
}, 60_000);

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
 * document order: Astryx keeps a live region of role alert of its own,
 * empty until it announces something, which they leave out.
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
 * chooseInAccountMenu opens the account menu in the top bar and chooses the
 * option named name, such as a language or a mode.
 */
export async function chooseInAccountMenu(name: string): Promise<void> {
  const button = within(screen.getByRole("banner")).getByRole("button", { name: /^(Account|账户)$/ });
  fireEvent.click(button);
  fireEvent.click(await screen.findByRole("menuitemradio", { name }));
}

/** tableRows are the text of each cell of each row of the table named name, its header row aside. */
export function tableRows(name: string): string[][] {
  return within(screen.getByRole("table", { name }))
    .getAllByRole("row")
    .slice(1)
    .map((row) => within(row).getAllByRole("cell").map((cell) => cell.textContent ?? ""));
}
