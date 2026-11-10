import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { expect, type MockInstance, onTestFinished, test, vi } from "vitest";

import { Providers } from "../shell/providers";
import { expectAccessible } from "../test/axe";
import { clipboard } from "../test/clipboard";
import { ShownOnceLine } from "./ShownOnceLine";

// The line a page shows once, a Join Code's or a Password Link's; the pages
// test it again as they use it.

let intervals: MockInstance<typeof window.setInterval> | undefined;

/** stopClock stops the browser's clock for one test; only Date and intervals are faked. */
function stopClock(): void {
  vi.useFakeTimers({ toFake: ["Date", "setInterval", "clearInterval"] });
  intervals = vi.spyOn(window, "setInterval");
  onTestFinished(() => {
    vi.useRealTimers();
  });
}

/** advance moves the stopped clock on by ms once the line's countdown of a second runs. */
async function advance(ms: number): Promise<void> {
  await vi.waitUntil(() => intervals?.mock.calls.some(([, period]) => period === 1000), {
    interval: 0,
    message: "the countdown never started",
  });
  act(() => vi.advanceTimersByTime(ms));
}

function renderLine(validFor: number) {
  render(
    <Providers>
      <ShownOnceLine
        label="Password Link"
        intro="Send this link to bob."
        line="https://koko.example.com/password#secret"
        validFor={validFor}
        expired="This link expired."
      />
    </Providers>,
  );
  return screen.getByRole("region", { name: "Password Link" });
}

test("a line valid for days counts down in days and hours, then in minutes and seconds in its last hour", async () => {
  stopClock();
  const shown = renderLine(7 * 86_400);

  expect(shown.textContent).toContain("Send this link to bob. It is shown only here, and leaving this page discards it.");
  expect(within(shown).getByText("https://koko.example.com/password#secret")).toBeDefined();
  expect(shown.textContent).toContain("7 days left");
  await expectAccessible();

  await advance(1000);
  expect(shown.textContent).toContain("6 days, 23 hours left");
  await advance(7 * 86_400_000 - 3_600_000 - 1000);
  expect(shown.textContent).toContain("60:00 left");
  await advance(3_600_000);
  expect(shown.textContent).toBe("This link expired.");
});

test("the copy button copies the line whole", async () => {
  const writeText = clipboard();
  const shown = renderLine(900);

  fireEvent.click(within(shown).getByRole("button", { name: "Copy" }));

  expect(await within(shown).findByRole("button", { name: "Copied" })).toBeDefined();
  expect(writeText).toHaveBeenCalledWith("https://koko.example.com/password#secret");
});
