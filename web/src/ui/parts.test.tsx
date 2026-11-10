import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { useState } from "react";
import { expect, onTestFinished, test, vi } from "vitest";

import { Providers } from "../shell/providers";
import { expectAccessible } from "../test/axe";
import { clipboard } from "../test/clipboard";
import { CommandLine, CopyButton, copiedFor } from "./copy";
import { DateInput } from "./DateInput";
import { notify } from "./notify";
import { Page, Sheet } from "./Page";
import { StateMark } from "./StateMark";

// The small shared parts: a page's head, a panel, Copy, a command line, a
// Host's state mark, a notice of what a call did, and a date field.

function renderIn(ui: React.ReactNode) {
  return render(<Providers>{ui}</Providers>);
}

test("a page's head shows its title, line and actions, and names the browser's tab after it", async () => {
  renderIn(
    <Page title="Hosts" lead="9 Hosts in 3 Spaces." actions={<button type="button">Add Host</button>}>
      <Sheet title="ops">
        <p>a panel</p>
      </Sheet>
    </Page>,
  );

  expect(screen.getByRole("heading", { level: 1, name: "Hosts" })).toBeDefined();
  expect(screen.getByText("9 Hosts in 3 Spaces.")).toBeDefined();
  expect(screen.getByRole("button", { name: "Add Host" })).toBeDefined();
  expect(within(screen.getByRole("region", { name: "ops" })).getByText("a panel")).toBeDefined();
  await waitFor(() => expect(document.title).toBe("Hosts · koko"));
  await expectAccessible();
});

test("a page whose title is more than text names the tab by its name", async () => {
  renderIn(
    <Page title={<span>web1 ●</span>} name="web1">
      <p>the Host</p>
    </Page>,
  );

  await waitFor(() => expect(document.title).toBe("web1 · koko"));
});

test("Copy copies, reads Copied for 1.6 s, announced politely, then Copy again", async () => {
  const writeText = clipboard();
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
  onTestFinished(() => {
    vi.useRealTimers();
  });
  renderIn(<CopyButton value="koko host enroll web1" />);

  fireEvent.click(screen.getByRole("button", { name: "Copy" }));
  await act(async () => {}); // the clipboard's answer

  const copied = screen.getByRole("button", { name: "Copied" });
  expect(within(copied).getByText("Copied").getAttribute("aria-live")).toBe("polite");
  expect(writeText).toHaveBeenCalledWith("koko host enroll web1");
  act(() => vi.advanceTimersByTime(copiedFor - 1));
  expect(screen.getByRole("button", { name: "Copied" })).toBeDefined();
  act(() => vi.advanceTimersByTime(1));
  expect(screen.getByRole("button", { name: "Copy" })).toBeDefined();
});

test("Copy in a browser that does not allow it says to select the text and copy it", async () => {
  clipboard({ refuse: true });
  renderIn(<CopyButton value="secret" />);

  fireEvent.click(screen.getByRole("button", { name: "Copy" }));

  expect(await screen.findByRole("status")).toHaveProperty(
    "textContent",
    "This browser did not allow copying. Select the text and copy it.",
  );
});

test("Copy of a value fetched on the click hands the clipboard a ClipboardItem at once, so Safari keeps the click's permission", async () => {
  const items: ClipboardItem[] = [];
  const write = vi.fn((written: ClipboardItem[]) => {
    items.push(...written);
    return Promise.resolve();
  });
  Object.defineProperty(navigator, "clipboard", { value: { write, writeText: vi.fn() }, configurable: true });
  class FakeClipboardItem {
    readonly types: string[];
    readonly #data: Record<string, Promise<Blob>>;
    constructor(data: Record<string, Promise<Blob>>) {
      this.#data = data;
      this.types = Object.keys(data);
    }
    getType(type: string): Promise<Blob> {
      return this.#data[type] ?? Promise.reject(new Error(type));
    }
  }
  vi.stubGlobal("ClipboardItem", FakeClipboardItem);
  onTestFinished(() => {
    Reflect.deleteProperty(navigator, "clipboard");
    vi.unstubAllGlobals();
  });
  let answer: (value: string) => void = () => undefined;
  renderIn(<CopyButton label="Copy the value of ops/db" value={() => new Promise((resolve) => (answer = resolve))} />);

  fireEvent.click(screen.getByRole("button", { name: "Copy the value of ops/db" }));

  // The clipboard has the item before the value has come.
  await waitFor(() => expect(write).toHaveBeenCalledTimes(1));
  act(() => answer("s3cret"));
  expect(await (await items[0]?.getType("text/plain"))?.text()).toBe("s3cret");
  expect(await screen.findByText("Copied")).toBeDefined();
});

test("a command line shows the command after a $ that Copy leaves out", async () => {
  const writeText = clipboard();
  renderIn(<CommandLine command="ssh web1" label="SSH command" />);
  const line = screen.getByRole("group", { name: "SSH command" });

  expect(line.querySelector("code")?.textContent).toBe("$ssh web1");
  expect(line.querySelector("code [aria-hidden='true']")?.textContent).toBe("$");
  await expectAccessible();
  fireEvent.click(within(line).getByRole("button", { name: "Copy" }));

  await waitFor(() => expect(writeText).toHaveBeenCalledWith("ssh web1"));
});

test("a Host's state shows as a mark and its word, in either language; an unknown one as stored", () => {
  renderIn(
    <>
      <StateMark state="managed" />
      <StateMark state="registered" />
      <StateMark state="retired" />
      <StateMark state="sleeping" />
    </>,
  );

  const marks = [...document.querySelectorAll("body > div span.inline-flex")];
  expect(marks.map((m) => m.textContent)).toEqual(["Managed", "Registered", "Retired", "sleeping"]);
  expect(marks.map((m) => m.querySelectorAll("[aria-hidden='true']").length)).toEqual([1, 1, 1, 0]);
});

test("notify says what a call did in a toast", async () => {
  renderIn(<button type="button" onClick={() => notify("web1 is Retired.")}>retire</button>);

  fireEvent.click(screen.getByRole("button", { name: "retire" }));

  expect(await screen.findByText("web1 is Retired.")).toBeDefined();
  await expectAccessible();
});

test("a date field holds a date as YYYY-MM-DD, typed or picked in its calendar", async () => {
  function Expires() {
    const [value, setValue] = useState("2026-12-09");
    return (
      <>
        <DateInput label="Expires" value={value} onChange={setValue} />
        <p>value: {value || "none"}</p>
      </>
    );
  }
  renderIn(<Expires />);
  const group = screen.getByRole("group", { name: "Expires" });
  expect(within(group).getAllByRole("spinbutton").map((s) => s.getAttribute("aria-valuetext"))).toEqual([
    "12 – December",
    "9",
    "2026",
  ]);

  fireEvent.click(screen.getByRole("button", { name: /Open the calendar/ }));
  const calendar = await screen.findByRole("application", { name: /Expires/ });
  await expectAccessible();
  fireEvent.click(within(calendar).getByRole("button", { name: /December 24, 2026/ }));

  await waitFor(() => expect(screen.getByText("value: 2026-12-24")).toBeDefined());
});
