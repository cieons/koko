import { act, render, screen } from "@testing-library/react";
import { expect, onTestFinished, test, vi } from "vitest";

import { UserCurrent } from "../api/user";
import { fakeApi } from "../test/fakeApi";
import { findAlert } from "../test/pages";
import { lazyPage, loadPage } from "./lazyPage";
import type { PageProps } from "./router";
import { Shell } from "./Shell";

// A page lazyPage loads as a chunk of its own: a spinner until it loads,
// and, for a chunk that does not load, a refusal in place of the page; any
// other error goes on past it.

function renderPage(page: ReturnType<typeof lazyPage>) {
  const api = fakeApi();
  api.on(UserCurrent, () => ({
    user: { id: "user_1", name: "alice", role: "owner", created_at: "2026-11-01T09:00:00Z" },
    device_session: { id: "device_1", user: "alice", device: "laptop", created_at: "2026-11-03T09:00:00Z" },
  }));
  window.history.replaceState(null, "", "/lazy");
  render(<Shell routes={[{ path: "/lazy", page, nav: "host.nav" }]} />);
}

test("a lazy page shows a spinner while its chunk loads, then the page", async () => {
  let loaded: (page: (props: PageProps) => React.ReactNode) => void = () => {};
  const load = vi.fn(() => new Promise<(props: PageProps) => React.ReactNode>((resolve) => (loaded = resolve)));
  renderPage(lazyPage(load));

  expect(await screen.findByRole("progressbar", { name: "Loading" })).toBeDefined();
  loaded(() => <h1>Loaded</h1>);

  expect(await screen.findByRole("heading", { name: "Loaded" })).toBeDefined();
  expect(load).toHaveBeenCalledTimes(1);
});

test("a lazy page loadPage loaded renders without waiting on its chunk, which loads once", async () => {
  let loaded: (page: (props: PageProps) => React.ReactNode) => void = () => {};
  const load = vi.fn(() => new Promise<(props: PageProps) => React.ReactNode>((resolve) => (loaded = resolve)));
  const page = lazyPage(load);

  const loading = loadPage(page);
  loaded(() => <h1>Loaded</h1>);
  await loading;
  await act(async () => renderPage(page));

  // No wait: the page is there once the render's own work is done.
  expect(screen.getByRole("heading", { name: "Loaded" })).toBeDefined();
  expect(screen.queryByRole("progressbar", { name: "Loading" })).toBeNull();
  await loadPage(page);
  expect(load).toHaveBeenCalledTimes(1);
});

test("loadPage does nothing for a page that is not lazy", async () => {
  await expect(loadPage(() => <h1>Eager</h1>)).resolves.toBeUndefined();
});

test("a lazy page whose chunk does not load says so in place of the page", async () => {
  renderPage(lazyPage(() => Promise.reject(new TypeError("Failed to fetch dynamically imported module"))));

  expect(await findAlert()).toBe("This page could not be loaded. Reload the browser page to try again.");
  expect(screen.getByRole("navigation", { name: "Pages" })).toBeDefined();
});

test("an error a lazy page throws as it renders is not taken for a chunk that did not load", async () => {
  const thrown = new Error("the page is broken");
  const onError = vi.fn();
  // React reports an error no boundary takes to the window, and to the console.
  const heard = (e: ErrorEvent) => {
    onError(e.error);
    e.preventDefault();
  };
  window.addEventListener("error", heard);
  const quiet = vi.spyOn(console, "error").mockImplementation(() => {});
  onTestFinished(() => {
    window.removeEventListener("error", heard);
    quiet.mockRestore();
  });
  renderPage(
    lazyPage(async () => () => {
      throw thrown;
    }),
  );

  await vi.waitFor(() => expect(onError).toHaveBeenCalledWith(thrown));
  expect(screen.queryByText("This page could not be loaded. Reload the browser page to try again.")).toBeNull();
});
