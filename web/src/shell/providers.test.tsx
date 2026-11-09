import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { Link as AstryxLink } from "@astryxdesign/core/Link";
import { useTheme } from "@astryxdesign/core/theme";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { expect, test } from "vitest";

import { type User, UserCurrent } from "../api/user";
import { fakeApi } from "../test/fakeApi";
import { setSystemScheme } from "../test/media";
import { applyStoredMode, modes, useMode } from "./mode";
import { type Route, useLocation } from "./router";
import { Shell } from "./Shell";

// The providers every page renders inside: Astryx's theme in the mode
// chosen, Astryx's own strings in koko's language, and koko's router behind
// Astryx's links.

const alice: User = { id: "user_1", name: "alice", role: "owner", created_at: "2026-11-01T09:00:00Z" };

/** A page that shows the mode chosen, the mode Astryx shows, and offers each choice. */
function ModePage() {
  const [mode, setMode] = useMode();
  const shown = useTheme().mode;
  return (
    <>
      <p>chosen: {mode}</p>
      <p>shown: {shown}</p>
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
    <Dialog isOpen onOpenChange={() => {}}>
      <DialogHeader title="Details" onOpenChange={() => {}} />
    </Dialog>
  );
}

function LinkPage() {
  const { pathname } = useLocation();
  return (
    <>
      <p>at {pathname}</p>
      <AstryxLink href="/link/next">Next</AstryxLink>
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

test("the mode is System by default, following the system's light or dark mode", async () => {
  renderShell("/mode");

  expect(await screen.findByText("chosen: system")).toBeDefined();
  expect(screen.getByText("shown: light")).toBeDefined();
  expect(document.documentElement.hasAttribute("data-theme")).toBe(false);

  act(() => setSystemScheme("dark"));
  expect(await screen.findByText("shown: dark")).toBeDefined();
  expect(document.documentElement.hasAttribute("data-theme")).toBe(false);
});

test("choosing Light or Dark overrides the system's mode and is remembered; System follows it again", async () => {
  setSystemScheme("dark");
  renderShell("/mode");
  await screen.findByText("chosen: system");

  fireEvent.click(screen.getByRole("button", { name: "light" }));
  expect(await screen.findByText("shown: light")).toBeDefined();
  expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  expect(localStorage.getItem("koko.theme")).toBe("light");

  fireEvent.click(screen.getByRole("button", { name: "dark" }));
  expect(await screen.findByText("shown: dark")).toBeDefined();
  expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
  expect(localStorage.getItem("koko.theme")).toBe("dark");

  fireEvent.click(screen.getByRole("button", { name: "system" }));
  expect(await screen.findByText("chosen: system")).toBeDefined();
  expect(document.documentElement.hasAttribute("data-theme")).toBe(false);
  expect(localStorage.getItem("koko.theme")).toBe("system");
});

test("a stored Light or Dark is applied to the page before the first render, and the first render keeps it", async () => {
  for (const stored of ["dark", "light"] as const) {
    localStorage.setItem("koko.theme", stored);
    setSystemScheme(stored === "dark" ? "light" : "dark");

    applyStoredMode();
    expect(document.documentElement.getAttribute("data-theme")).toBe(stored);

    const { unmount } = renderShell("/mode");
    expect(await screen.findByText(`chosen: ${stored}`)).toBeDefined();
    expect(screen.getByText(`shown: ${stored}`)).toBeDefined();
    expect(document.documentElement.getAttribute("data-theme")).toBe(stored);
    unmount();
  }
});

test("a stored mode that is not one of the choices is System", async () => {
  document.documentElement.setAttribute("data-theme", "dark");
  localStorage.setItem("koko.theme", "sepia");

  applyStoredMode();
  expect(document.documentElement.hasAttribute("data-theme")).toBe(false);

  renderShell("/mode");
  expect(await screen.findByText("chosen: system")).toBeDefined();
});

test("Astryx's own strings follow koko's language: a dialog's close label in English and in Chinese", async () => {
  renderShell("/dialog");
  expect(await screen.findByRole("button", { name: "Close" })).toBeDefined();

  fireEvent.change(screen.getByRole("combobox", { name: "Language" }), { target: { value: "zh-CN" } });
  expect(await screen.findByRole("button", { name: "关闭" })).toBeDefined();
  expect(screen.queryByRole("button", { name: "Close" })).toBeNull();
});

test("the providers put no style element in the page, which the CSP, default-src 'self', would refuse", async () => {
  localStorage.setItem("koko.theme", "dark");
  renderShell("/dialog");
  await screen.findByRole("dialog");

  fireEvent.change(screen.getByRole("combobox", { name: "Language" }), { target: { value: "zh-CN" } });
  await screen.findByRole("button", { name: "关闭" });
  expect(document.querySelectorAll("style")).toHaveLength(0);
});

test("a link an Astryx component renders goes through koko's router, without a page load", async () => {
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
