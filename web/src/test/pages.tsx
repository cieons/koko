import { render, screen, within } from "@testing-library/react";

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

/** tableRows are the text of each cell of each row of the table named name, its header row aside. */
export function tableRows(name: string): string[][] {
  return within(screen.getByRole("table", { name }))
    .getAllByRole("row")
    .slice(1)
    .map((row) => within(row).getAllByRole("cell").map((cell) => cell.textContent ?? ""));
}
