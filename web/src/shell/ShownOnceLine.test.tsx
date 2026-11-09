import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { expect, type MockInstance, onTestFinished, test, vi } from "vitest";

import { expectAccessible } from "../test/axe";
import { Providers } from "./providers";
import { ShownOnceLine } from "./ShownOnceLine";

// The line a page shows once, as a Join Code's today and a Password
// Link's later; the Devices and People pages test it as they use it.

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

  expect(shown.textContent).toContain("Send this link to bob.");
  expect(within(shown).getByText("https://koko.example.com/password#secret")).toBeDefined();
  expect(shown.textContent).toContain("It expires in 7 days.");
  await expectAccessible();

  await advance(1000);
  expect(shown.textContent).toContain("It expires in 6 days, 23 hours.");
  await advance(7 * 86_400_000 - 3_600_000 - 1000);
  expect(shown.textContent).toContain("It expires in 60:00.");
  await advance(3_600_000);
  expect(shown.textContent).toBe("This link expired.");
});

test("the copy button copies the line whole", async () => {
  const writeText = vi.fn(() => Promise.resolve());
  Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
  onTestFinished(() => {
    Reflect.deleteProperty(navigator, "clipboard");
  });
  const shown = renderLine(900);

  fireEvent.click(within(shown).getByRole("button", { name: "Copy" }));

  expect(await within(shown).findByText("Copied.")).toBeDefined();
  expect(writeText).toHaveBeenCalledWith("https://koko.example.com/password#secret");
});
