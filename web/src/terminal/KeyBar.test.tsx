import { act, configure, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import { type Host, HostList, HostShow } from "../api/host";
import { alice, host } from "../pages/host/records.fixture";
import { expectAccessible } from "../test/axe";
import { fakeApi } from "../test/fakeApi";
import { setViewport } from "../test/media";
import { actAs, renderAt } from "../test/pages";
import { FakeWebSocket } from "../test/terminal";
import type { EmulatorProps, Modifiers } from "./emulator/Emulator";

// The terminal on a phone: its key bar, the tab button that starts the
// key bar's row, the sheet of tabs, and the phone's layout upright and
// sideways, run as the whole web UI against the fake API, with the
// emulator replaced by a fake that records the keys pressed through it
// and the WebSocket by one the test answers as the koko server.

vi.setConfig({ testTimeout: 20_000 });
configure({ asyncUtilTimeout: 5_000 });

/** FakeEmulator is what a test sees of one tab's emulator. */
interface FakeEmulator {
  props: EmulatorProps;
  mounts: number;
  /** pressed is each key pressed through pressKey, with "^" for Control and "M-" for Alt, as the shell is sent it. */
  pressed: string[];
}

const emulators = vi.hoisted(() => new Map<string, FakeEmulator>());

vi.mock("./emulator/Emulator", async () => {
  const { createElement, useEffect, useImperativeHandle, useRef } = await import("react");
  function Emulator(props: EmulatorProps) {
    const record = emulators.get(props.label) ?? { props, mounts: 0, pressed: [] };
    record.props = props;
    emulators.set(props.label, record);
    const input = useRef<HTMLTextAreaElement>(null);
    useImperativeHandle(props.ref, () => ({
      write: () => {},
      focus: () => input.current?.focus(),
      blur: () => input.current?.blur(),
      hasFocus: () => document.activeElement === input.current,
      fit: () => {},
      selection: () => "",
      // A key is encoded as the fake writes it, and given back through onInput as wterm gives its bytes.
      pressKey: (name: string, modifiers: Modifiers) => {
        const encoded = `${modifiers.ctrl ? "^" : ""}${modifiers.alt ? "M-" : ""}${name}`;
        record.pressed.push(encoded);
        props.onInput(new TextEncoder().encode(encoded));
      },
    }));
    useEffect(() => {
      record.mounts++;
      props.onReady(120, 36);
    }, []);
    // A character typed on the soft keyboard reaches the page as its bytes.
    return createElement("textarea", {
      ref: input,
      "aria-label": props.label,
      onKeyDown: (event: KeyboardEvent) => props.onInput(new TextEncoder().encode(event.key)),
    });
  }
  return { fontSizes: [12, 13, 14, 16, 18], Emulator };
});

const hosts: Host[] = [host("web1", { space: "web" }), host("db2", { space: "ops" }), host("cache3", { space: "ops" })];

const tunnel = (n: number) => n.toString(16).padStart(32, "0");

/** upright and sideways are a phone held upright and turned; narrow is a desktop's window as narrow as a phone. */
const upright = { width: 390, height: 844, coarse: true };
const sideways = { width: 844, height: 390, coarse: true };
const narrow = { width: 390, height: 800, coarse: false };

beforeEach(() => {
  FakeWebSocket.sockets = [];
  vi.stubGlobal("WebSocket", FakeWebSocket);
});

afterEach(() => {
  emulators.clear();
  sessionStorage.clear();
});

/** serve renders the web UI at path for alice, with the Managed Hosts above, on a viewport of that form. */
function serve(path: string, viewport = upright): void {
  setViewport(viewport);
  const api = fakeApi();
  actAs(api, alice);
  api.on(HostList, () => ({ hosts }));
  api.on(HostShow, ({ host: name }) => ({ host: hosts.find((h) => h.name === name) ?? hosts[0]!, services: [] }));
  renderAt(path);
}

/** opened waits for the page to have opened n WebSockets, and has the koko server start each one's shell. */
async function opened(n: number): Promise<FakeWebSocket[]> {
  await waitFor(() => expect(FakeWebSocket.sockets).toHaveLength(n));
  const all = [...FakeWebSocket.sockets];
  for (const [i, socket] of all.entries()) {
    await act(async () => {
      socket.opened();
      socket.answer({ tunnel: tunnel(i + 1) });
    });
  }
  return all;
}

/** emulator is the fake emulator of the tab of that name. */
function emulator(name: string): FakeEmulator {
  const found = emulators.get(`Terminal on ${name}`);
  if (!found) {
    throw new Error(`${name} has no emulator`);
  }
  return found;
}

/** keyBar is the row of keys. */
const keyBar = () => screen.getByRole("toolbar", { name: "Terminal keys" });

/** key is the key bar's key of that name. */
const key = (name: string) => within(keyBar()).getByRole("button", { name });

/**
 * tap is a finger's tap on el, as a phone's browser sends it, and tells
 * whether the page let the press move the focus, which a key never does.
 */
function tap(el: Element, at = 0): boolean {
  const focusing = fireEvent.pointerDown(el, { pointerType: "touch", clientX: at });
  const mouse = fireEvent.mouseDown(el, { clientX: at });
  fireEvent.pointerUp(el, { pointerType: "touch", clientX: at });
  fireEvent.mouseUp(el, { clientX: at });
  fireEvent.click(el);
  return focusing && mouse;
}

/** swipeAcross is a finger moving across el by dx pixels, then lifted. */
function swipeAcross(el: Element, dx: number): void {
  fireEvent.pointerDown(el, { pointerType: "touch", clientX: 200 });
  fireEvent.pointerUp(el, { pointerType: "touch", clientX: 200 + dx });
  fireEvent.click(el);
}

/** type is the soft keyboard typing a character into the terminal with the focus. */
function type(text: string): void {
  fireEvent.keyDown(document.activeElement ?? document.body, { key: text });
}

/** tabButton is the button that starts the key bar's row. */
const tabButton = () => screen.getByRole("button", { name: /open\. Show the tabs$/ });

/** sheet is the open sheet of tabs. */
const sheet = () => screen.getByRole("dialog", { name: "Tabs" });

test("each key of the key bar is pressed through the active tab's emulator, and no key takes the focus", async () => {
  serve("/terminal?host=web1&host=db2&tab=2");
  const [, socket] = await opened(2);
  const terminal = screen.getByRole("textbox", { name: "Terminal on db2" });
  act(() => terminal.focus());

  const names = within(keyBar())
    .getAllByRole("button")
    .map((each) => each.getAttribute("aria-label"));
  expect(names).toEqual([
    "Escape",
    "Tab",
    "Control",
    "Alt",
    "Left arrow",
    "Up arrow",
    "Down arrow",
    "Right arrow",
    "Vertical bar",
    "Slash",
    "Hyphen",
    "Tilde",
    "Home",
    "End",
    "Page Up",
    "Page Down",
    "Show or hide the keyboard",
  ]);
  // The row is one Tab stop, so a keyboard can scroll it; its keys are none.
  expect(keyBar().tabIndex).toBe(0);
  expect(within(keyBar()).getAllByRole("button").every((each) => each.tabIndex === -1)).toBe(true);

  const sent = ["Escape", "Tab", "ArrowLeft", "ArrowUp", "ArrowDown", "ArrowRight", "|", "/", "-", "~", "Home", "End", "PageUp", "PageDown"];
  for (const name of names.filter((each) => !["Control", "Alt", "Show or hide the keyboard"].includes(each ?? ""))) {
    expect(tap(key(name ?? "")), `${name} lets the press move the focus`).toBe(false);
    expect(document.activeElement, `${name} took the focus`).toBe(terminal);
  }
  expect(emulator("db2").pressed).toEqual(sent);
  expect(emulator("web1").pressed).toEqual([]);
  await waitFor(() => expect(socket?.typed).toBe(sent.join("")));

  // The keyboard key closes the soft keyboard by taking the focus off the terminal, and opens it again.
  expect(tap(key("Show or hide the keyboard"))).toBe(false);
  expect(document.activeElement).not.toBe(terminal);
  tap(key("Show or hide the keyboard"));
  expect(document.activeElement).toBe(terminal);
  await expectAccessible();
});

test("Control applies to the next key, typed or pressed, and lets go; a second tap holds it and a third lets it go", async () => {
  serve("/terminal?host=web1");
  const [socket] = await opened(1);
  const terminal = screen.getByRole("textbox", { name: "Terminal on web1" });
  // On a touch screen the terminal never takes the focus by itself, which would open the soft keyboard.
  await act(async () => {});
  expect(document.activeElement).not.toBe(terminal);
  act(() => terminal.focus());

  // Applied once: the next character typed on the soft keyboard goes with Control, the one after as typed.
  expect(tap(key("Control"))).toBe(false);
  expect(key("Control").getAttribute("aria-pressed")).toBe("true");
  expect(document.activeElement).toBe(terminal);
  type("c");
  expect(key("Control").getAttribute("aria-pressed")).toBe("false");
  type("d");
  expect(emulator("web1").pressed).toEqual(["^c"]);
  await waitFor(() => expect(socket?.typed).toBe("^cd"));

  // Held: every key goes with Control, from the soft keyboard and from the bar, until Control is tapped again.
  tap(key("Control"));
  tap(key("Control"));
  expect(key("Control").getAttribute("aria-pressed")).toBe("true");
  expect(key("Control").className).toContain("bg-foreground");
  type("a");
  tap(key("Up arrow"));
  type("e");
  expect(key("Control").getAttribute("aria-pressed")).toBe("true");
  tap(key("Control"));
  expect(key("Control").getAttribute("aria-pressed")).toBe("false");
  type("f");
  expect(emulator("web1").pressed).toEqual(["^c", "^a", "^ArrowUp", "^e"]);
  await waitFor(() => expect(socket?.typed).toBe("^cd^a^ArrowUp^ef"));

  // Alt applies as Control does, and with it; a key typed that is no character goes as typed.
  tap(key("Alt"));
  tap(key("Control"));
  expect(key("Alt").className).toContain("bg-accent");
  type("x");
  expect(key("Alt").getAttribute("aria-pressed")).toBe("false");
  expect(key("Control").getAttribute("aria-pressed")).toBe("false");
  expect(emulator("web1").pressed.at(-1)).toBe("^M-x");
  tap(key("Control"));
  type("Enter");
  expect(emulator("web1").pressed.at(-1)).toBe("^M-x");
  await waitFor(() => expect(socket?.typed).toBe("^cd^a^ArrowUp^ef^M-xEnter"));
});

test("the key bar shows only on a touch screen: at the island's foot on a desktop, beside the tab button on a phone", async () => {
  serve("/terminal?host=web1", { width: 1280, height: 800, coarse: false });
  await opened(1);
  expect(screen.queryByRole("toolbar")).toBeNull();

  act(() => setViewport({ width: 1280, height: 800, coarse: true }));
  expect(keyBar()).toBeDefined();
  expect(screen.queryByRole("button", { name: /Show the tabs$/ })).toBeNull();

  // A desktop's window as narrow as a phone takes the phone's layout, with the tab button and no keys.
  act(() => setViewport(narrow));
  expect(screen.queryByRole("toolbar")).toBeNull();
  expect(tabButton()).toBeDefined();

  act(() => setViewport(upright));
  expect(keyBar()).toBeDefined();
  await expectAccessible();
});

test("the tab button shows the active tab and the count, and a swipe across it shows the next or the one before", async () => {
  serve("/terminal?host=web1&host=db2&host=cache3");
  await opened(3);
  expect(tabButton().getAttribute("aria-label")).toBe("web1, 3 open. Show the tabs");
  expect(tabButton().textContent).toBe("web13");

  // Leftwards shows the next, wrapping; rightwards the one before; neither opens the sheet.
  swipeAcross(tabButton(), -60);
  expect(tabButton().getAttribute("aria-label")).toBe("db2, 3 open. Show the tabs");
  swipeAcross(tabButton(), -60);
  swipeAcross(tabButton(), -60);
  expect(tabButton().getAttribute("aria-label")).toBe("web1, 3 open. Show the tabs");
  swipeAcross(tabButton(), 60);
  expect(tabButton().getAttribute("aria-label")).toBe("cache3, 3 open. Show the tabs");
  await waitFor(() => expect(window.location.search).toBe("?host=web1&host=db2&host=cache3&tab=3"));
  // A move of 36 pixels or less is a tap.
  swipeAcross(tabButton(), -36);
  expect(sheet()).toBeDefined();
  expect(tabButton().getAttribute("aria-label")).toBe("cache3, 3 open. Show the tabs");
  expect(screen.queryByRole("dialog", { name: "Go to a terminal" })).toBeNull();
});

test("the sheet of tabs leads to the places, shows a tab, closes one, asking while its shell runs, and opens the switcher", async () => {
  serve("/terminal?host=web1&host=db2&host=cache3");
  const sockets = await opened(3);
  tap(tabButton());
  const places = within(sheet()).getByRole("navigation", { name: "Main" });
  expect(within(places).getAllByRole("link").map((link) => [link.textContent, link.getAttribute("href")])).toEqual([
    ["Hosts", "/hosts"],
    ["Secrets", "/secrets"],
    ["Access", "/access"],
  ]);
  const list = within(sheet()).getByRole("list", { name: "Terminal tabs" });
  expect(within(list).getAllByRole("listitem")).toHaveLength(3);
  expect(within(list).getByRole("button", { current: true }).textContent).toContain("web1");
  await expectAccessible();

  // Showing a tab closes the sheet.
  tap(within(list).getByRole("button", { name: /^db2/ }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Tabs" })).toBeNull());
  expect(tabButton().getAttribute("aria-label")).toBe("db2, 3 open. Show the tabs");

  // Closing a tab whose shell runs asks first.
  tap(tabButton());
  tap(within(sheet()).getByRole("button", { name: "Close cache3" }));
  const question = await screen.findByRole("alertdialog", { name: "End the SSH Session to cache3?" });
  await expectAccessible();
  tap(within(question).getByRole("button", { name: "End SSH Session" }));
  await waitFor(() => expect(within(sheet()).getAllByRole("listitem")).toHaveLength(2));
  expect(sockets[2]?.framesSent.at(-1)).toMatchObject({ kind: "close" });
  expect(tabButton().getAttribute("aria-label")).toBe("db2, 2 open. Show the tabs");

  // New tab closes the sheet and opens the switcher.
  tap(within(sheet()).getByRole("button", { name: "New tab" }));
  await screen.findByRole("dialog", { name: "Go to a terminal" });
  expect(screen.queryByRole("dialog", { name: "Tabs" })).toBeNull();
  await expectAccessible();
});

test("with no tab open, the tab button opens the sheet, which leads to the places and to New tab", async () => {
  serve("/terminal");
  await screen.findByRole("heading", { level: 1, name: "No terminal is open" });
  expect(tabButton().getAttribute("aria-label")).toBe("Terminal, 0 open. Show the tabs");
  expect(screen.queryByRole("toolbar")).toBeNull();
  tap(tabButton());
  expect(within(sheet()).queryByRole("list")).toBeNull();
  expect(within(sheet()).getByRole("button", { name: "New tab" })).toBeDefined();
  await expectAccessible();
});

test("on a phone the terminal's row takes the frame's place, and held sideways Files and More end the key bar's row", async () => {
  serve("/terminal?host=web1&host=db2");
  const sockets = await opened(2);
  // Upright: no frame, the row over the terminal, the key bar's row under it.
  expect(screen.queryByRole("banner")).toBeNull();
  expect(screen.queryByRole("navigation", { name: "Main" })).toBeNull();
  expect(screen.getByRole("link", { name: "koko, Back to Hosts" }).getAttribute("href")).toBe("/");
  act(() => emulator("web1").props.onWorkingDirectory("file:///srv/app/current"));
  expect(screen.getByText("…/app/current")).toBeDefined();
  const files = screen.getByRole("button", { name: "Files" });
  expect(keyBar().contains(files)).toBe(false);
  expect(files.compareDocumentPosition(keyBar()) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  await expectAccessible();

  // Sideways: no row over the terminal, and no rail though the window is a desktop's width; Files and More follow the keys.
  act(() => setViewport(sideways));
  expect(screen.queryByRole("link", { name: "koko, Back to Hosts" })).toBeNull();
  expect(screen.queryByRole("tablist", { name: "Terminal tabs" })).toBeNull();
  expect(keyBar().compareDocumentPosition(screen.getByRole("button", { name: "Files" })) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  expect(screen.getByRole("button", { name: "More" })).toBeDefined();
  await expectAccessible();

  // Turning the phone, and on to a desktop's form and back, keeps every tab's emulator and SSH Session.
  act(() => setViewport(upright));
  act(() => setViewport({ width: 1280, height: 800, coarse: false }));
  expect(screen.getByRole("tablist", { name: "Terminal tabs" })).toBeDefined();
  act(() => setViewport(sideways));
  expect(emulator("web1").mounts).toBe(1);
  expect(emulator("db2").mounts).toBe(1);
  expect(FakeWebSocket.sockets).toHaveLength(2);
  expect(sockets.every((socket) => !socket.closed)).toBe(true);
});

test("the row over the terminal names a tab's state in words while it is not Connected", async () => {
  serve("/terminal?host=web1");
  await waitFor(() => expect(FakeWebSocket.sockets).toHaveLength(1));
  const head = screen.getByRole("link", { name: "koko, Back to Hosts" }).parentElement as HTMLElement;
  expect(within(head).getByText("Connecting").className).not.toContain("sr-only");
  const [socket] = FakeWebSocket.sockets;
  await act(async () => {
    socket?.opened();
    socket?.answer({ tunnel: tunnel(1) });
  });
  // Connected is said to a screen reader alone.
  expect(within(head).getByText("Connected").className).toContain("sr-only");
});
