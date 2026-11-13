import { render, waitFor } from "@testing-library/react";
import { WTerm } from "@wterm/dom";
import { GhosttyCore } from "@wterm/ghostty";
import { createElement, createRef } from "react";
import { afterEach, expect, test, vi } from "vitest";

import snapshotGolden from "../../../../internal/server/vt/testdata/snapshot.golden?raw";
import { Emulator, type EmulatorHandle } from "./Emulator";

// The emulator against the real wterm and ghostty core under jsdom, which
// lays nothing out: what koko relies on wterm 0.5.10 for, and the patch to
// its renderer (web/patches) that keeps it within kokod's CSP.

/**
 * NodeFS is the part of Node's fs this test reads the core with: Vitest
 * runs under Node, and the app's types leave Node out.
 */
interface NodeFS {
  readFileSync(path: string): Uint8Array<ArrayBuffer>;
}
const { process } = globalThis as unknown as { process: { getBuiltinModule(id: "node:fs"): NodeFS } };

/** wasm is the ghostty core's WebAssembly, which fetch answers with whatever URL it is asked for. */
const wasm = process.getBuiltinModule("node:fs").readFileSync("node_modules/@wterm/ghostty/wasm/ghostty-vt.wasm");

function serveWasm(): void {
  vi.stubGlobal("fetch", async () => new Response(wasm, { headers: { "Content-Type": "application/wasm" } }));
}

/** made is what each test's terminals started, ended with the test. */
const made: (() => void)[] = [];
afterEach(() => {
  for (const end of made.splice(0)) {
    end();
  }
});

/** terminal is a WTerm of cols by rows, 40 by 10 unless given, on a ghostty core, as the Emulator makes one, with what it reports. */
async function terminal(cols = 40, rows = 10) {
  serveWasm();
  const core = await GhosttyCore.load({ wasmPath: "/ghostty-vt.wasm", scrollbackLines: 5000, imageStorageLimit: 0 });
  const element = document.createElement("div");
  document.body.append(element);
  const copied: string[] = [];
  const dirs: string[] = [];
  const data: string[] = [];
  const titles: string[] = [];
  const wt = new WTerm(element, {
    core,
    cols,
    rows,
    onData: (text) => data.push(text),
    onTitle: (title) => titles.push(title),
    onClipboardWrite: (text) => copied.push(text),
    onWorkingDirectory: (uri) => dirs.push(uri),
  });
  made.push(() => {
    wt.destroy();
    core.dispose();
    element.remove();
  });
  await wt.init();
  return { wt, core, copied, dirs, data, titles, element };
}

/** painted waits for wterm to paint text, which it does a frame after a write. */
const painted = (element: HTMLElement, text: string) => waitFor(() => expect(element.textContent).toContain(text));

test("OSC 52 and OSC 7 reach the callbacks, and a wide character takes two cells", async () => {
  const { wt, core, copied, dirs } = await terminal();
  wt.write(`\x1b]52;c;${btoa("hello")}\x07`);
  wt.write("\x1b]7;file://web1/home/deploy\x1b\\");
  wt.write("a中b");
  await waitFor(() => expect(copied).toEqual(["hello"]));
  expect(dirs).toEqual(["file://web1/home/deploy"]);
  expect([0, 1, 2, 3].map((col) => core.getCell(0, col).width ?? 1)).toEqual([1, 2, 0, 1]);
  expect(core.getCursor().col).toBe(4);
});

// kokod's CSP refuses a style attribute the HTML parser meets (style-src
// without 'unsafe-inline'), and wterm 0.5.10 as published writes every run
// of cells as HTML with one, so the terminal drew garbled under it (#169).
// The patch has the rows carry their styles in data-term-style and sets
// them through CSSOM, which the CSP does not govern. Without the patch this
// fails: the rows given to the parser hold style attributes.
test("the patched renderer gives the HTML parser no style attribute, and styles each run through CSSOM", async () => {
  const parsed: string[] = [];
  const inner = Object.getOwnPropertyDescriptor(Element.prototype, "innerHTML");
  Object.defineProperty(Element.prototype, "innerHTML", {
    configurable: true,
    get: inner?.get,
    set(this: Element, html: string) {
      parsed.push(html);
      inner?.set?.call(this, html);
    },
  });
  const fragment = Range.prototype.createContextualFragment;
  Range.prototype.createContextualFragment = function (html: string) {
    parsed.push(html);
    return fragment.call(this, html);
  };
  try {
    const { wt, element } = await terminal();
    wt.write("\x1b[31mred\x1b[0m plain \x1b[1;4;9mbold\x1b[0m 中文\r\n");
    await painted(element, "bold");
    // A change in the middle of a row goes through createContextualFragment.
    wt.write("\x1b[32mgreen\x1b[0m again");
    await painted(element, "green again");
    const rows = parsed.filter((html) => html.includes("<span"));
    expect(rows.length).toBeGreaterThan(0);
    expect(rows.filter((html) => /\sstyle=/.test(html))).toEqual([]);
    expect(element.querySelectorAll("[data-term-style]").length).toBe(0);
    const styled = [...element.querySelectorAll<HTMLElement>(".term-row span")].filter((span) => span.style.cssText !== "");
    expect(styled.some((span) => span.textContent === "red" && span.style.color !== "")).toBe(true);
    expect(styled.some((span) => span.textContent === "green" && span.style.color !== "")).toBe(true);
    expect(styled.some((span) => span.style.textDecoration.includes("line-through"))).toBe(true);
    expect(styled.some((span) => span.style.getPropertyValue("--term-span-cols") !== "")).toBe(true);
  } finally {
    if (inner) {
      Object.defineProperty(Element.prototype, "innerHTML", inner);
    }
    Range.prototype.createContextualFragment = fragment;
  }
});

test("the Emulator sends a pressed key as the terminal's modes ask, and gives what the shell reports", async () => {
  serveWasm();
  const handle = createRef<EmulatorHandle>();
  const input: string[] = [];
  const ready = vi.fn();
  const dirs = vi.fn();
  const copied = vi.fn();
  const title = vi.fn();
  render(
    createElement(Emulator, {
      ref: handle,
      label: "web1",
      fontSize: 14,
      paused: false,
      announce: false,
      onReady: ready,
      onInput: (bytes: Uint8Array) => input.push(new TextDecoder().decode(bytes)),
      onResize: () => {},
      onClipboardWrite: copied,
      onWorkingDirectory: dirs,
      onTitle: title,
      onError: (error: unknown) => {
        throw error;
      },
    }),
  );
  await waitFor(() => expect(ready).toHaveBeenCalled());
  const emulator = handle.current;
  if (!emulator) {
    throw new Error("the Emulator gave no handle");
  }
  emulator.pressKey("ArrowUp", { ctrl: false, alt: false });
  emulator.pressKey("c", { ctrl: true, alt: false });
  emulator.pressKey("b", { ctrl: false, alt: true });
  emulator.pressKey("PageUp", { ctrl: false, alt: false });
  // Application cursor keys, as vim and htop set them.
  emulator.write(new TextEncoder().encode("\x1b[?1h"));
  emulator.pressKey("ArrowUp", { ctrl: false, alt: false });
  emulator.pressKey("Home", { ctrl: false, alt: false });
  expect(input).toEqual(["\x1b[A", "\x03", "\x1bb", "\x1b[5~", "\x1bOA", "\x1bOH"]);

  const encode = (text: string) => new TextEncoder().encode(text);
  emulator.write(encode(`\x1b]52;c;${btoa("ls -la")}\x07`));
  emulator.write(encode("\x1b]7;file://web1/srv/app\x1b\\"));
  emulator.write(encode("\x1b]2;deploy@web1\x07"));
  await waitFor(() => expect(copied).toHaveBeenCalledWith("ls -la"));
  expect(dirs).toHaveBeenCalledWith("file://web1/srv/app");
  expect(title).toHaveBeenCalledWith("deploy@web1");
  expect(document.querySelector(".wterm.koko-term.koko-term-14")?.getAttribute("aria-label")).toBe("web1");
});

/**
 * GoldenCase is one case of the koko server's snapshot golden
 * (internal/server/vt): output a shell wrote, the snapshot of the koko
 * server's emulator after it, and the screen the snapshot draws there.
 */
interface GoldenCase {
  name: string;
  cols: number;
  rows: number;
  output: string;
  snapshot: string;
  then?: string;
  screen: string[];
  cursorRow: number;
  cursorCol: number;
  alternate: boolean;
  title: string;
  pwd: string;
}

const goldenCases = JSON.parse(snapshotGolden) as GoldenCase[];

const encoder = new TextEncoder();

/** screenText is the visible rows of a core as text, without trailing blanks, as the golden names them. */
function screenText(core: GhosttyCore): string[] {
  const rows: string[] = [];
  for (let row = 0; row < core.getRows(); row++) {
    let text = "";
    for (let col = 0; col < core.getCols(); col++) {
      const cell = core.getCell(row, col);
      if (cell.width === 0 || cell.spacerHead) {
        continue;
      }
      text += cell.chars ?? String.fromCodePoint(cell.char);
    }
    rows.push(text.trimEnd());
  }
  return rows;
}

/** state is what a page's core shows: every visible cell, the newest history, the cursor and the modes. */
function state(core: GhosttyCore, history: number) {
  const cells = [];
  for (let row = 0; row < core.getRows(); row++) {
    for (let col = 0; col < core.getCols(); col++) {
      cells.push(core.getCell(row, col));
    }
  }
  // Offset 0 is the newest line of history.
  const lines = [];
  for (let offset = 0; offset < history; offset++) {
    const line = [];
    for (let col = 0; col < core.getScrollbackLineLen(offset); col++) {
      line.push(core.getScrollbackCell(offset, col));
    }
    lines.push(line);
  }
  return {
    cells,
    lines,
    cursor: core.getCursor(),
    alternate: core.usingAltScreen(),
    modes: [
      core.cursorKeysApp(),
      core.bracketedPaste(),
      core.mouseTracking(),
      core.mouseEncoding(),
      core.focusEvents(),
      core.synchronizedOutput(),
      core.modifyOtherKeys(),
      core.kittyKeyboardFlags(),
    ],
  };
}

// The koko server keeps an emulator for each web terminal SSH Session, of
// libghostty-vt 1.0.1, and a page that attaches gets its snapshot (spec
// #213). Each case of its golden, written into wterm's core of Ghostty
// 1.3.1, draws the screen the golden names, and the same terminal as the
// shell's output written into it directly: the cells with their colours,
// the newest history, the cursor and the modes.
test.each(goldenCases)("the koko server's snapshot draws $name", async (c) => {
  const direct = await terminal(c.cols, c.rows);
  const drawn = await terminal(c.cols, c.rows);
  direct.wt.write(encoder.encode(c.output));
  drawn.wt.write(encoder.encode(c.snapshot));
  expect(screenText(drawn.core)).toEqual(c.screen);
  const cursor = drawn.core.getCursor();
  expect([cursor.row, cursor.col]).toEqual([c.cursorRow, c.cursorCol]);
  expect(drawn.core.usingAltScreen()).toBe(c.alternate);
  expect(drawn.titles.at(-1) ?? "").toBe(c.title);
  expect(drawn.dirs.at(-1) ?? "").toBe(c.pwd);
  expect([drawn.titles.at(-1), drawn.dirs.at(-1)]).toEqual([direct.titles.at(-1), direct.dirs.at(-1)]);
  const history = Math.min(direct.core.getScrollbackCount(), drawn.core.getScrollbackCount());
  expect(state(drawn.core, history)).toEqual(state(direct.core, history));
  if (c.then) {
    direct.wt.write(encoder.encode(c.then));
    drawn.wt.write(encoder.encode(c.then));
    const after = Math.min(direct.core.getScrollbackCount(), drawn.core.getScrollbackCount());
    expect(state(drawn.core, after)).toEqual(state(direct.core, after));
  }
});

// The koko server answers what a shell asks of the terminal, once however
// many pages are attached, and the page answers nothing: koko's Emulator
// drops what wterm gives while write runs (spec #213). That holds only
// while wterm 0.5.10 gives each reply within write, the core's through its
// drain and the window's size in pixels after it, so a wterm that moved
// one fails here first. It gives no reply to CSI 18 t, the text area's
// size in cells, which its core leaves to the host.
test.each([
  ["device attributes", "\x1b[c", 1],
  ["the cursor's place", "\x1b[6n", 1],
  ["the foreground colour", "\x1b]10;?\x1b\\", 1],
  ["the background colour", "\x1b]11;?\x1b\\", 1],
  ["the window's size in pixels", "\x1b[14t", 1],
  ["the text area's size in cells", "\x1b[18t", 0],
])("wterm answers a query for %s within write", async (_, query, replies) => {
  const { wt, data } = await terminal();
  // wterm's timers and frames run under the fake clock, so whatever it
  // would give after write has been given once they have all run.
  vi.useFakeTimers();
  try {
    wt.write(encoder.encode(query));
    expect(data).toHaveLength(replies);
    vi.runAllTimers();
    expect(data).toHaveLength(replies);
  } finally {
    vi.useRealTimers();
  }
});
