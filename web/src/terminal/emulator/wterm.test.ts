import { render, waitFor } from "@testing-library/react";
import { WTerm } from "@wterm/dom";
import { GhosttyCore } from "@wterm/ghostty";
import { createElement, createRef } from "react";
import { afterEach, expect, test, vi } from "vitest";

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

const elements: HTMLElement[] = [];
afterEach(() => {
  for (const element of elements.splice(0)) {
    element.remove();
  }
});

/** terminal is a WTerm of 40 by 10 on a ghostty core, as the Emulator makes one, with what it reports. */
async function terminal() {
  serveWasm();
  const core = await GhosttyCore.load({ wasmPath: "/ghostty-vt.wasm", scrollbackLines: 5000, imageStorageLimit: 0 });
  const element = document.createElement("div");
  document.body.append(element);
  elements.push(element);
  const copied: string[] = [];
  const dirs: string[] = [];
  const wt = new WTerm(element, {
    core,
    cols: 40,
    rows: 10,
    onClipboardWrite: (text) => copied.push(text),
    onWorkingDirectory: (uri) => dirs.push(uri),
  });
  await wt.init();
  return { wt, core, copied, dirs, element };
}

/** painted waits for wterm's next paint, which it schedules after a write. */
const painted = () => new Promise((resolve) => setTimeout(resolve, 50));

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
    await painted();
    // A change in the middle of a row goes through createContextualFragment.
    wt.write("\x1b[32mgreen\x1b[0m again");
    await painted();
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
