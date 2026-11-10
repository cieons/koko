import { Button, Link, Modal } from "@heroui/react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { expect, test } from "vitest";

import { type User, UserCurrent } from "../api/user";
import { fakeApi } from "../test/fakeApi";
import { setSystemScheme } from "../test/media";
import { chooseInTopBar } from "../test/pages";
import { applyStoredMode, modes, useMode } from "./mode";
import { type Route, useLocation } from "./router";
import { Shell } from "./Shell";

// The providers every page renders inside: the mode kept on <html>, React
// Aria's own strings in koko's language, and koko's router behind HeroUI's
// links.

const alice: User = { id: "user_1", name: "alice", role: "owner", created_at: "2026-11-01T09:00:00Z" };

/** A page that shows the mode chosen and offers each choice. */
function ModePage() {
  const [mode, setMode] = useMode();
  return (
    <>
      <p>chosen: {mode}</p>
      {modes.map((m) => (
        <button key={m} type="button" onClick={() => setMode(m)}>
          {m}
        </button>
      ))}
    </>
  );
}

function DialogPage() {
  return (
    <Modal.Backdrop isOpen>
      <Modal.Container>
        <Modal.Dialog aria-label="Details">
          <Modal.CloseTrigger />
          <Modal.Header>
            <Modal.Heading>Details</Modal.Heading>
          </Modal.Header>
          <Modal.Footer>
            <Button>Done</Button>
          </Modal.Footer>
        </Modal.Dialog>
      </Modal.Container>
    </Modal.Backdrop>
  );
}

function LinkPage() {
  const { pathname } = useLocation();
  return (
    <>
      <p>at {pathname}</p>
      <Link href="/link/next">Next</Link>
    </>
  );
}

const routes: Route[] = [
  { path: "/mode", page: ModePage },
  { path: "/dialog", page: DialogPage },
  { path: "/link/:step", page: LinkPage },
];

function renderShell(path: string) {
  fakeApi().on(UserCurrent, () => ({ user: alice }));
  window.history.replaceState(null, "", path);
  return render(<Shell routes={routes} />);
}

/** shown is the mode <html> shows: its class and its data-theme. */
function shown(): [string, string | undefined] {
  const html = document.documentElement;
  return [html.className, html.dataset.theme];
}

test("the mode is System by default, following the system's light or dark mode", async () => {
  renderShell("/mode");

  expect(await screen.findByText("chosen: system")).toBeDefined();
  expect(shown()).toEqual(["light", "light"]);

  act(() => setSystemScheme("dark"));
  expect(shown()).toEqual(["dark", "dark"]);
});

test("choosing Light or Dark overrides the system's mode and is remembered; System follows it again", async () => {
  setSystemScheme("dark");
  renderShell("/mode");
  await screen.findByText("chosen: system");

  fireEvent.click(screen.getByRole("button", { name: "light" }));
  expect(await screen.findByText("chosen: light")).toBeDefined();
  expect(shown()).toEqual(["light", "light"]);
  expect(localStorage.getItem("koko.theme")).toBe("light");

  fireEvent.click(screen.getByRole("button", { name: "dark" }));
  expect(await screen.findByText("chosen: dark")).toBeDefined();
  expect(shown()).toEqual(["dark", "dark"]);
  expect(localStorage.getItem("koko.theme")).toBe("dark");

  fireEvent.click(screen.getByRole("button", { name: "system" }));
  expect(await screen.findByText("chosen: system")).toBeDefined();
  expect(shown()).toEqual(["dark", "dark"]);
  expect(localStorage.getItem("koko.theme")).toBe("system");
});

test("a stored Light or Dark is applied to <html> before the first render, and the first render keeps it", async () => {
  for (const stored of ["dark", "light"] as const) {
    localStorage.setItem("koko.theme", stored);
    setSystemScheme(stored === "dark" ? "light" : "dark");

    applyStoredMode();
    expect(shown()).toEqual([stored, stored]);

    const { unmount } = renderShell("/mode");
    expect(await screen.findByText(`chosen: ${stored}`)).toBeDefined();
    expect(shown()).toEqual([stored, stored]);
    unmount();
  }
});

test("a stored mode that is not one of the choices is System", async () => {
  setSystemScheme("dark");
  localStorage.setItem("koko.theme", "sepia");

  applyStoredMode();
  expect(shown()).toEqual(["dark", "dark"]);

  renderShell("/mode");
  expect(await screen.findByText("chosen: system")).toBeDefined();
});

test("React Aria's own strings follow koko's language: a dialog's close label in English and in Chinese", async () => {
  renderShell("/dialog");
  expect(await screen.findByRole("button", { name: "Dismiss" })).toBeDefined();

  await chooseInTopBar("Language", "简体中文");
  expect(await screen.findByRole("button", { name: "取消" })).toBeDefined();
  expect(screen.queryByRole("button", { name: "Dismiss" })).toBeNull();
});

test("a link HeroUI renders goes through koko's router, without a page load", async () => {
  renderShell("/link/first");
  await screen.findByText("at /link/first");

  const click = new MouseEvent("click", { bubbles: true, cancelable: true, button: 0 });
  act(() => {
    screen.getByRole("link", { name: "Next" }).dispatchEvent(click);
  });

  expect(click.defaultPrevented).toBe(true);
  expect(await screen.findByText("at /link/next")).toBeDefined();
  expect(window.location.pathname).toBe("/link/next");
});
