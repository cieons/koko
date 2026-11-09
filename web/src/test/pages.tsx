import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";

import type { DeviceSession } from "../api/device";
import { type User, UserCurrent } from "../api/user";
import { App } from "../App";
import type { FakeApi } from "./fakeApi";

// Helpers for the tests of the web UI's pages, which run the whole app, its
// shell and every page, against the fake API.

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
