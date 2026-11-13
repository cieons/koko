import { act, fireEvent, render, screen } from "@testing-library/react";
import { expect, onTestFinished, test, vi } from "vitest";

import { ApiError } from "../client";
import { Providers } from "../shell/providers";
import { expectAccessible } from "../test/axe";
import { alerts, statuses } from "../test/pages";
import { EmptyState, Loaded, LoadFailed, Loading, loadingDelay, NoMatch, Refusal } from "./states";

// The states every screen has: loading, failed to load, empty, no match and
// a refused write.

function renderIn(ui: React.ReactNode) {
  return render(<Providers>{ui}</Providers>);
}

test("Loading shows nothing for its first 300 ms, then three skeleton rows, announced as loading", async () => {
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
  onTestFinished(() => {
    vi.useRealTimers();
  });
  renderIn(<Loading />);

  act(() => vi.advanceTimersByTime(loadingDelay - 1));
  expect(statuses()).toEqual([]);
  act(() => vi.advanceTimersByTime(1));

  expect(statuses()).toEqual(["Loading"]);
  expect(screen.getByRole("status").querySelectorAll("[aria-hidden] > *")).toHaveLength(6); // two bars a row
  vi.useRealTimers();
  await expectAccessible();
});

test("LoadFailed shows the koko server's message, and Try again loads again", async () => {
  const retry = vi.fn();
  renderIn(<LoadFailed error={new ApiError("internal_error", "boom")} retry={retry} />);

  expect(alerts()).toEqual(["The koko server failed unexpectedly. Its log records the cause."]);
  await expectAccessible();
  fireEvent.click(screen.getByRole("button", { name: "Try again" }));

  expect(retry).toHaveBeenCalledOnce();
});

test("Loaded shows the answer once there is one, the failure without one, and both after a failed reload", () => {
  const reload = vi.fn();
  const show = (data: string | undefined, error?: unknown) =>
    renderIn(<Loaded read={{ data, error, reload }}>{(d) => <p>answer: {d}</p>}</Loaded>);
  const failed = new ApiError("internal_error", "boom");

  const { unmount } = show("web1");
  expect(screen.getByText("answer: web1")).toBeDefined();
  expect(alerts()).toEqual([]);
  unmount();

  const second = show(undefined, failed);
  expect(screen.queryByText(/^answer/)).toBeNull();
  expect(screen.getByRole("button", { name: "Try again" })).toBeDefined();
  second.unmount();

  show("web1", failed);
  expect(screen.getByText("answer: web1")).toBeDefined();
  expect(alerts()).toEqual(["The koko server failed unexpectedly. Its log records the cause."]);
});

test("EmptyState says what is missing and offers the action that adds one, with no sentence of its own", async () => {
  const { container } = renderIn(
    <EmptyState title="No Host is recorded yet" action={<button type="button">Add Host</button>} />,
  );

  expect(screen.getByRole("heading", { level: 2, name: "No Host is recorded yet" })).toBeDefined();
  expect(screen.getByRole("button", { name: "Add Host" })).toBeDefined();
  expect(container.querySelector("p")).toBeNull();
  await expectAccessible();
});

test("EmptyState says what its screen cannot, where it has no action", () => {
  renderIn(<EmptyState title="No Service is recorded yet" lead="Services are added on a Host's page." />);

  expect(screen.getByText("Services are added on a Host's page.")).toBeDefined();
});

test("NoMatch says nothing matches the view, and its button clears it", async () => {
  const onClear = vi.fn();
  renderIn(<NoMatch message="No Host matches this view." clear="Clear the search and filters" onClear={onClear} />);

  expect(screen.getByText("No Host matches this view.")).toBeDefined();
  await expectAccessible();
  fireEvent.click(screen.getByRole("button", { name: "Clear the search and filters" }));

  expect(onClear).toHaveBeenCalledOnce();
});

test("Refusal shows a refused write's message as an alert, in either language, and nothing without one", async () => {
  const { rerender } = renderIn(<Refusal error={undefined} />);
  expect(alerts()).toEqual([]);

  rerender(
    <Providers>
      <Refusal error={new ApiError("name_taken", "taken")} />
    </Providers>,
  );

  expect(alerts()).toEqual(["This name is already in use."]);
  await expectAccessible();
});
