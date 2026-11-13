import { act, configure, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";

import { type Host, HostList, HostShow } from "../api/host";
import { alice, host } from "../pages/host/records.fixture";
import { navigate } from "../shell/router";
import { expectAccessible } from "../test/axe";
import { clipboard } from "../test/clipboard";
import { type FakeApi, fakeApi } from "../test/fakeApi";
import { setViewportWidth } from "../test/media";
import { actAs, preferLanguages, renderAt } from "../test/pages";
import {
  attachments,
  type FakeWebSocket,
  type Request,
  requestsOn,
  running,
  type WireTab,
  workspaceSockets,
} from "../test/terminal";
import type { EmulatorProps } from "./emulator/Emulator";

// The terminal workspace, its tabs in the rail and a tab's states, run as
// the whole web UI against the fake API, with the emulator replaced by a
// fake that records what it is given, and the WebSockets by fakes the test
// answers as the koko server: the Workspace socket, and each attachment's.

vi.setConfig({ testTimeout: 20_000 });
configure({ asyncUtilTimeout: 5_000 });

/** FakeEmulator is what a test sees of one tab's emulator. */
interface FakeEmulator {
  props: EmulatorProps;
  /** mounts counts the times it was mounted: a remount would lose the screen. */
  mounts: number;
  written: string;
  /** resets are the grids reset gave it. */
  resets: string[];
  selection: string;
}

const emulators = vi.hoisted(() => new Map<string, FakeEmulator>());

/** loading holds back each emulator mounted while hold is set, as a core still loading does, until it is let go. */
const loading = vi.hoisted(() => ({ hold: false, held: [] as (() => void)[] }));

vi.mock("./emulator/Emulator", async () => {
  const { useEffect, useImperativeHandle, useRef, useState } = await import("react");
  const { createElement } = await import("react");
  function Emulator(props: EmulatorProps) {
    const record = emulators.get(props.label) ?? { props, mounts: 0, written: "", resets: [], selection: "" };
    record.props = props;
    emulators.set(props.label, record);
    const input = useRef<HTMLTextAreaElement>(null);
    const [loaded, setLoaded] = useState(!loading.hold);
    useImperativeHandle(props.ref, () => ({
      write: (bytes: Uint8Array) => {
        record.written += new TextDecoder().decode(bytes);
      },
      reset: (cols: number, rows: number) => {
        record.written = "";
        record.resets.push(`${cols}x${rows}`);
      },
      metrics: () => ({ cellWidth: 8, cellHeight: 16, padX: 0, padY: 0 }),
      focus: () => input.current?.focus(),
      blur: () => input.current?.blur(),
      hasFocus: () => document.activeElement === input.current,
      selection: () => record.selection,
      pressKey: () => {},
    }));
    useEffect(() => {
      record.mounts++;
      if (loaded) {
        props.onReady();
      } else {
        loading.held.push(() => {
          setLoaded(true);
          props.onReady();
        });
      }
    }, []);
    if (!loaded) {
      return null;
    }
    // A key typed reaches the page as its name, as wterm's bytes would.
    return createElement("textarea", {
      ref: input,
      "aria-label": props.label,
      "data-font": props.fontSize,
      onKeyDown: (event: KeyboardEvent) => props.onInput(new TextEncoder().encode(event.key)),
    });
  }
  return { fontSizes: [12, 13, 14, 16, 18], Emulator };
});

const hosts: Host[] = [host("web1", { space: "web" }), host("db2", { space: "ops" }), host("cache3", { space: "ops" })];

const web1 = running("a1", "web1");
const db2 = running("b2", "db2");
const cache3 = running("c3", "cache3");

afterEach(() => {
  emulators.clear();
  loading.hold = false;
  loading.held = [];
  sessionStorage.clear();
});

/** serve renders the web UI at path for alice, with the Managed Hosts above. */
function serve(path: string): FakeApi {
  const api = fakeApi();
  actAs(api, alice);
  api.on(HostList, () => ({ hosts }));
  api.on(HostShow, ({ host: name }) => ({ host: hosts.find((h) => h.name === name) ?? hosts[0]!, path: [], services: [] }));
  renderAt(path);
  return api;
}

/** workspace waits for the page's nth Workspace socket, from 1, and is it. */
async function workspace(n = 1): Promise<FakeWebSocket> {
  await waitFor(() => expect(workspaceSockets()).toHaveLength(n));
  return workspaceSockets()[n - 1] as FakeWebSocket;
}

/** says is the koko server sending a Workspace on socket. */
async function says(socket: FakeWebSocket, ...tabs: WireTab[]): Promise<void> {
  await act(async () => socket.answer({ tabs }));
}

/** joined has the page's Workspace socket taken, and the koko server send it the Workspace; it is the socket. */
async function joined(...tabs: WireTab[]): Promise<FakeWebSocket> {
  const socket = await workspace();
  act(() => socket.opened());
  expect(socket.first).toEqual({ protocol: 2 });
  await says(socket, ...tabs);
  return socket;
}

/** asked waits for the page's nth request on socket, from 1, and is it. */
async function asked(socket: FakeWebSocket, n: number): Promise<Request> {
  await waitFor(() => expect(requestsOn(socket).length).toBeGreaterThanOrEqual(n));
  return requestsOn(socket)[n - 1] as Request;
}

/** answers is the koko server answering a request on socket: the Workspace first, then the answer. */
async function answers(socket: FakeWebSocket, request: Request, answer: object, ...tabs: WireTab[]): Promise<void> {
  await says(socket, ...tabs);
  await act(async () => socket.answer({ request: request.request, ...answer }));
}

/** attachment waits for the page's nth attachment, from 1, and is its WebSocket. */
async function attachment(n = 1): Promise<FakeWebSocket> {
  await waitFor(() => expect(attachments().length).toBeGreaterThanOrEqual(n));
  return attachments()[n - 1] as FakeWebSocket;
}

/** attached has the koko server take an attachment, give the pty's size and a snapshot, and say whether this page is active. */
async function attached(socket: FakeWebSocket, snapshot = "", active = true): Promise<void> {
  await act(async () => {
    socket.opened();
    socket.answer({ attachment: "a77ac4" });
    socket.records({ type: "reset", columns: 120, rows: 36 }, { type: "data", data: new TextEncoder().encode(snapshot) }, { type: "active", active });
  });
}

/** tabList is the rail's tab list. */
const tabList = () => screen.getByRole("tablist", { name: "Terminal tabs" });

/** tabs are the rail's tabs, by their names. */
function tabs(): string[] {
  return within(tabList())
    .getAllByRole("tab")
    .map((tab) => tab.querySelector(".truncate")?.textContent ?? "");
}

/** tab is the rail's tab of that name. */
function tab(name: string): HTMLElement {
  const found = within(tabList())
    .getAllByRole("tab")
    .find((each) => each.querySelector(".truncate")?.textContent === name);
  if (!found) {
    throw new Error(`no tab is named ${name}`);
  }
  return found;
}

/** selected is the name of the rail's selected tab. */
const selected = () =>
  within(tabList())
    .getAllByRole("tab")
    .find((each) => each.getAttribute("aria-selected") === "true")
    ?.querySelector(".truncate")?.textContent;

/** press is a pointer's press on el, as a browser sends it. */
function press(el: Element): void {
  fireEvent.pointerDown(el, { pointerType: "mouse" });
  fireEvent.mouseDown(el);
  fireEvent.pointerUp(el, { pointerType: "mouse" });
  fireEvent.mouseUp(el);
  fireEvent.click(el);
}

/** shown is the panel of the tab shown, which is not inert. */
function shown(): HTMLElement {
  const panel = screen.getAllByRole("tabpanel").find((each) => !each.hasAttribute("inert"));
  if (!panel) {
    throw new Error("no tab is shown");
  }
  return panel;
}

/** emulator is the fake emulator of the tab of that name. */
function emulator(name: string): FakeEmulator {
  const found = emulators.get(`Terminal on ${name}`);
  if (!found) {
    throw new Error(`${name} has no emulator`);
  }
  return found;
}

/** chord presses Control, Shift and a key where the focus is. */
function chord(key: string, code = key): void {
  fireEvent.keyDown(document.activeElement ?? document.body, { key, code, ctrlKey: true, shiftKey: true });
}

/** toast waits for a toast saying text. */
async function toast(text: string): Promise<void> {
  await waitFor(() => expect(screen.getByText(text)).toBeDefined());
}

test("the tabs come from the Workspace, in order with the koko server's labels; the URL's tab is shown and attached alone", async () => {
  serve("/terminal?tab=2");
  const socket = await workspace();
  expect(socket.url).toBe(`ws://${window.location.host}/terminal/workspace`);
  act(() => socket.opened());
  await says(socket, web1, db2, running("a3", "web1", "web1 (2)"));
  expect(tabs()).toEqual(["web1", "db2", "web1 (2)"]);
  expect(selected()).toBe("db2");
  expect(document.title).toBe("db2 · Terminal · koko");
  expect(within(shown()).getByRole("status").textContent).toBe("Connecting to db2…");
  // The tab's panel is the one its tab names.
  expect(document.getElementById(tab("db2").getAttribute("aria-controls") ?? "")).toBe(shown());
  expect(window.location.pathname + window.location.search).toBe("/terminal?tab=2");

  // The page attaches to the tab it shows, and only once it shows it.
  const first = await attachment();
  act(() => first.opened());
  expect(first.url).toBe(`ws://${window.location.host}/terminal/socket`);
  expect(first.first).toEqual({ protocol: 2, attach: db2.session, cols: 80, rows: 24 });
  await attached(first, "deploy@db2:~$ ");
  await waitFor(() => expect(emulator("db2").written).toBe("deploy@db2:~$ "));
  expect(emulator("db2").resets).toEqual(["120x36"]);
  expect(within(shown()).queryByRole("status")).toBeNull();
  expect(within(screen.getByRole("region", { name: "Terminal" })).getByText("Connected")).toBeDefined();
  expect(attachments()).toHaveLength(1);
  await expectAccessible();

  // Each Workspace replaces the tabs: one gone, one new, a label as the koko server gives it.
  await says(socket, running("a3", "web1", "web1 (2)"), db2, cache3);
  expect(tabs()).toEqual(["web1 (2)", "db2", "cache3"]);
  expect(selected()).toBe("db2");
  expect(emulator("db2").mounts).toBe(1);
  expect(window.location.search).toBe("?tab=2");
});

test("a reconnected Workspace socket's first message replaces the tabs whole", async () => {
  serve("/terminal");
  const socket = await joined(web1, db2);
  expect(tabs()).toEqual(["web1", "db2"]);
  await act(async () => socket.drop());
  const again = await workspace(2);
  act(() => again.opened());
  expect(again.first).toEqual({ protocol: 2 });
  await says(again, cache3);
  expect(tabs()).toEqual(["cache3"]);
  expect(selected()).toBe("cache3");
});

test("keys typed reach the shell, and each state of a tab shows over its terminal without moving it", async () => {
  serve("/terminal");
  await joined(web1);
  const socket = await attachment();
  await attached(socket);
  fireEvent.keyDown(screen.getByRole("textbox", { name: "Terminal on web1" }), { key: "l" });
  await waitFor(() => expect(socket.typed).toBe("l"));

  // Reconnecting: the terminal stays, dimmed, under a notice with Try now.
  await act(async () => socket.drop());
  await waitFor(() =>
    expect(within(shown()).getByRole("status").textContent).toContain(
      "Reconnecting to the koko server… Keys typed now are sent once it is back.",
    ),
  );
  expect(within(shown()).getByRole("button", { name: "Try now" })).toBeDefined();
  expect(tab("web1").textContent).toContain("Reconnecting");
  await expectAccessible();
  const resume = await attachment(2);
  act(() => resume.opened());
  expect(resume.first).toEqual({ protocol: 2, resume: "a77ac4", offset: expect.any(Number) });
  await act(async () => resume.answer({ offset: 0 }));
  await waitFor(() => expect(within(shown()).queryByRole("status")).toBeNull());
});

test("the clipboard card shows only while this page is the active one, and focus reports go from it alone", async () => {
  serve("/terminal");
  await joined(web1);
  const socket = await attachment();
  await attached(socket, "", false);

  // Another page is active: a copy is not offered here, and no focus report goes.
  act(() => emulator("web1").props.onClipboardWrite("uptime"));
  expect(within(shown()).queryByRole("status")).toBeNull();
  act(() => emulator("web1").props.onInput(new TextEncoder().encode("\x1b[I")));
  expect(socket.typed).toBe("");

  // This page becomes active.
  await act(async () => socket.records({ type: "active", active: true }));
  const copied = clipboard();
  act(() => emulator("web1").props.onClipboardWrite("uptime\nsecond line"));
  const ask = within(shown()).getByRole("status");
  expect(ask.textContent).toContain("web1 asks to copy 18 characters to the clipboard.");
  await expectAccessible();
  fireEvent.click(within(ask).getByRole("button", { name: "Copy" }));
  await waitFor(() => expect(copied).toHaveBeenCalledWith("uptime\nsecond line"));
  act(() => emulator("web1").props.onInput(new TextEncoder().encode("\x1b[O")));
  expect(socket.typed).toBe("\x1b[O");

  // Another page takes it over, and the card asked for goes.
  act(() => emulator("web1").props.onClipboardWrite("x"));
  expect(within(shown()).getByRole("status")).toBeDefined();
  await act(async () => socket.records({ type: "active", active: false }));
  await waitFor(() => expect(within(shown()).queryByRole("status")).toBeNull());
});

test("a pointer on the terminal, a tab chosen and the window resized make this page active; attaching does not", async () => {
  serve("/terminal");
  await joined(web1, db2);
  const first = await attachment();
  await attached(first, "", false);
  const named = () => first.framesSent.flatMap((frame) => (frame.kind === "data" ? [...frame.data] : []));
  expect(named(), "attaching is no person's act").toEqual([]);
  fireEvent.pointerDown(screen.getByRole("textbox", { name: "Terminal on web1" }));
  await waitFor(() => expect(named()).toEqual([0x05, 0x00]));
  act(() => window.dispatchEvent(new Event("resize")));
  await waitFor(() => expect(named()).toEqual([0x05, 0x00, 0x05, 0x00]));

  // A tab chosen by a pointer: its attachment says so once it is made.
  press(tab("db2"));
  const second = await attachment(2);
  await attached(second, "", false);
  await waitFor(() => expect(second.framesSent.some((frame) => frame.kind === "data" && frame.data[0] === 0x05)).toBe(true));
});

test("an SSH Session's end comes from the Workspace, the same words in every page, and Open a new one sends reopen", async () => {
  const endings: [WireTab["ended"], string][] = [
    [{ reason: "shell ended", exit_status: 3 }, "The shell on web1 exited with status 3."],
    [{ reason: "shell ended", exit_signal: "KILL" }, "The shell on web1 ended by signal KILL."],
    [
      { reason: "access", notice: "access to web1 was withdrawn: bob was Removed; this SSH Session is closed." },
      "The SSH Session to web1 has ended.access to web1 was withdrawn: bob was Removed; this SSH Session is closed.",
    ],
    [{ reason: "server stopping", notice: "the koko server is restarting; this SSH Session has ended." }, "The koko server is restarting; this SSH Session has ended."],
  ];
  serve("/terminal");
  const socket = await joined(web1);
  for (const [ended, line] of endings) {
    await says(socket, { id: "a1", host: "web1", label: "web1", state: "ended", ended });
    await waitFor(() => expect(within(shown()).getByRole("status").textContent).toContain(line));
    expect(tab("web1").textContent).toContain("Ended");
  }
  await expectAccessible();
  fireEvent.click(within(shown()).getByRole("button", { name: "Open a new one" }));
  const reopen = await asked(socket, 1);
  expect(reopen).toEqual({ request: 1, reopen: "a1", cols: 80, rows: 24 });
  await answers(socket, reopen, { tab: "a1" }, { id: "a1", host: "web1", label: "web1", state: "starting" });
  expect(within(shown()).getByRole("status").textContent).toBe("Connecting to web1…");
});

test("a refused tab says why, with Try again, which reopens it, and Close tab", async () => {
  const refusals: [string, string, string][] = [
    ["unknown_host", 'no Host is named "web1".', "No Host you can see is named web1."],
    ["host_unreachable", "web1 is down or unreachable: …", "koko could not reach web1. This SSH Session did not start.web1 is down"],
    ["shell_refused", "web1 refused the shell", "web1 refused to start a shell."],
    ["something_new", "a refusal of a later koko", "The koko server refused this SSH Session to web1.a refusal of a later koko"],
  ];
  serve("/terminal");
  const socket = await joined(web1);
  let n = 0;
  for (const [code, message, line] of refusals) {
    await says(socket, { id: "a1", host: "web1", label: "web1", state: "refused", refused: { code, message } });
    const alert = await within(shown()).findByRole("alert");
    expect(alert.textContent, code).toContain(line);
    expect(tab("web1").textContent).toContain("Refused");
    await expectAccessible();
    fireEvent.click(within(shown()).getByRole("button", { name: "Try again" }));
    expect(await asked(socket, ++n)).toMatchObject({ reopen: "a1" });
  }
  // A tab whose shell never started closes at once.
  press(tab("web1").querySelector("[data-close]") as Element);
  expect(screen.queryByRole("alertdialog")).toBeNull();
  expect(await asked(socket, n + 1)).toEqual({ request: n + 1, close: "a1" });
});

test("the koko server's restart: its tabs are reopened in tab order, each once the one before has left starting", async () => {
  serve("/terminal");
  const restarted = (id: string, name: string): WireTab => ({ id, host: name, label: name, state: "restarted" });
  const socket = await joined(restarted("a1", "web1"), restarted("b2", "db2"), { ...cache3, state: "ended", session: undefined, ended: { reason: "shell ended", exit_status: 0 } });
  expect(within(shown()).getByRole("status").textContent).toBe("The koko server restarted.");
  const first = await asked(socket, 1);
  expect(first).toEqual({ request: 1, reopen: "a1", cols: 80, rows: 24 });
  // Another page's change meanwhile asks nothing more.
  await says(socket, restarted("a1", "web1"), restarted("b2", "db2"), { ...cache3, state: "ended", session: undefined, ended: { reason: "shell ended", exit_status: 0 } });
  expect(requestsOn(socket), "one at a time").toHaveLength(1);
  // The answer comes before the koko server dials: the next waits while the tab starts.
  await answers(socket, first, { tab: "a1" }, { ...web1, state: "starting", session: undefined }, restarted("b2", "db2"), cache3);
  expect(requestsOn(socket), "web1 is still starting").toHaveLength(1);
  await says(socket, web1, restarted("b2", "db2"), cache3);
  const second = await asked(socket, 2);
  expect(second).toEqual({ request: 2, reopen: "b2", cols: 80, rows: 24 });
  // One refused counts as done as one running does.
  await answers(socket, second, { tab: "b2" }, web1, { ...db2, state: "starting", session: undefined }, restarted("d4", "cache3"), cache3);
  await says(socket, web1, { id: "b2", host: "db2", label: "db2", state: "refused", refused: { code: "host_unreachable", message: "" } }, restarted("d4", "cache3"), cache3);
  expect(await asked(socket, 3)).toEqual({ request: 3, reopen: "d4", cols: 80, rows: 24 });
  await says(socket, web1, db2, { ...running("d4", "cache3") }, cache3);
  expect(requestsOn(socket), "a tab ended before the restart is left as it is").toHaveLength(3);
  expect(tabs()).toEqual(["web1", "db2", "cache3", "cache3"]);
});

test("the koko server stopping says so, and the page reconnects", async () => {
  serve("/terminal");
  const socket = await joined(web1);
  const first = await attachment();
  await attached(first);
  await act(async () => {
    first.frames({ kind: "close", ack: 0, reason: "the koko server is restarting; this SSH Session has ended." });
    socket.answer({ stopping: "the koko server is restarting" });
    socket.drop();
  });
  expect(within(shown()).getByRole("status").textContent).toBe("The koko server is restarting; this SSH Session has ended.");
  await expectAccessible();
  const again = await workspace(2);
  act(() => again.opened());
  await says(again, { id: "a1", host: "web1", label: "web1", state: "restarted" });
  expect(within(shown()).getByRole("status").textContent).toBe("The koko server restarted.");
  expect(await asked(again, 1)).toMatchObject({ reopen: "a1" });
});

test("open and close are requests: a tab shows once the Workspace names it, and closing asks first in the new words", async () => {
  serve("/terminal");
  const socket = await joined(web1);
  press(screen.getByRole("button", { name: /^New tab/ }));
  const dialog = await screen.findByRole("dialog", { name: "Go to a terminal" });
  press(within(dialog).getByRole("option", { name: /^db2opsOpen/ }));
  const open = await asked(socket, 1);
  expect(open).toEqual({ request: 1, open: "db2", cols: 80, rows: 24 });
  expect(tabs(), "no tab before the Workspace names it").toEqual(["web1"]);
  await answers(socket, open, { tab: "b2" }, web1, { ...db2, state: "starting", session: undefined });
  await waitFor(() => expect(selected()).toBe("db2"));
  expect(window.location.search).toBe("?tab=2");

  // Closing a tab whose shell runs asks first, by its mark, which chooses no tab.
  const mark = tab("web1").querySelector("[data-close]");
  expect(mark?.getAttribute("aria-hidden")).toBe("true");
  press(mark as Element);
  const question = await screen.findByRole("alertdialog", { name: "Close web1?" });
  expect(question.textContent).toContain("This ends its SSH Session in every place it is open.");
  expect(selected()).toBe("db2");
  await expectAccessible();
  fireEvent.click(within(question).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  expect(requestsOn(socket)).toHaveLength(1);

  // Delete on a focused tab, then Close tab: the tab goes once the Workspace no longer names it, and nothing is announced.
  act(() => tab("db2").focus());
  fireEvent.keyDown(tab("db2"), { key: "Delete" });
  fireEvent.click(within(await screen.findByRole("alertdialog", { name: "Close db2?" })).getByRole("button", { name: "Close tab" }));
  const close = await asked(socket, 2);
  expect(close).toEqual({ request: 2, close: "b2" });
  expect(tabs()).toEqual(["web1", "db2"]);
  await answers(socket, close, {}, web1);
  expect(tabs()).toEqual(["web1"]);
  expect(selected()).toBe("web1");
  expect(screen.queryByText(/was closed in another page/)).toBeNull();
});

test("an open refused adds no tab, and says why in place: in the switcher that sent it, else over the terminal", async () => {
  serve("/terminal");
  const socket = await joined(web1);
  press(screen.getByRole("button", { name: /^New tab/ }));
  press(within(await screen.findByRole("dialog")).getByRole("option", { name: /^cache3opsOpen/ }));
  await answers(socket, await asked(socket, 1), { refused: "access_refused", message: "access to cache3 was refused." }, web1);
  const dialog = screen.getByRole("dialog", { name: "Go to a terminal" });
  expect((await within(dialog).findByRole("alert")).textContent).toBe("koko refused this SSH Session to cache3.access to cache3 was refused.");
  expect(tabs()).toEqual(["web1"]);
  expect(screen.queryByRole("alertdialog"), "no toast").toBeNull();
  await expectAccessible();
  // Opened again, the switcher has forgotten it.
  fireEvent.keyDown(within(dialog).getByRole("textbox"), { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  press(screen.getByRole("button", { name: /^New tab/ }));
  expect(within(await screen.findByRole("dialog")).queryByRole("alert")).toBeNull();
  fireEvent.keyDown(within(screen.getByRole("dialog")).getByRole("textbox"), { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

  // An open from a Host's page is refused over the terminal, until dismissed.
  act(() => navigate("/hosts/cache3/terminal"));
  await answers(socket, await asked(socket, 2), { refused: "access_refused", message: "" }, web1);
  const line = await screen.findByRole("alert");
  expect(line.textContent).toBe("koko refused this SSH Session to cache3.");
  await expectAccessible();
  press(screen.getByRole("button", { name: "Dismiss" }));
  await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
});

test("a tab another page closed is announced, and the tab beside it shown", async () => {
  serve("/terminal?tab=2");
  const socket = await joined(web1, db2, cache3);
  expect(selected()).toBe("db2");
  await says(socket, web1, cache3);
  await toast("db2 was closed in another page.");
  expect(selected()).toBe("cache3");
  // One not shown goes without a word.
  await says(socket, cache3);
  expect(screen.queryByText("web1 was closed in another page.")).toBeNull();
});

test("a ninth page detaches this one, which shows it with Show here, and attaches again only then", async () => {
  serve("/terminal");
  await joined(web1);
  const first = await attachment();
  await attached(first);
  await act(async () => {
    first.records({ type: "active", active: false }, { type: "displaced" });
    first.frames({ kind: "close", ack: 0, reason: "" });
  });
  const cover = await within(shown()).findByRole("alert");
  expect(cover.textContent).toBe("This terminal is open in 8 other pages.");
  await expectAccessible();
  expect(attachments()).toHaveLength(1);
  fireEvent.click(within(shown()).getByRole("button", { name: "Show here" }));
  const again = await attachment(2);
  await attached(again, "", false);
  await waitFor(() => expect(again.framesSent.some((frame) => frame.kind === "data" && frame.data[0] === 0x05)).toBe(true));
});

test("an attach refused for an out-of-date page asks for a reload, and the Workspace's for want of a Sign-in goes to Sign in", async () => {
  serve("/terminal?tab=1");
  await joined(web1);
  const first = await attachment();
  await act(async () => {
    first.opened();
    first.answer({ refused: "reload_page", message: "" });
  });
  expect((await within(shown()).findByRole("alert")).textContent).toBe("This page is out of date. Reload it.");
  expect(within(shown()).getByRole("button", { name: "Reload" })).toBeDefined();
  await expectAccessible();
});

test("a Workspace socket refused for want of a Sign-in goes to Sign in, which comes back to the terminal", async () => {
  serve("/terminal?tab=1");
  const socket = await workspace();
  await act(async () => {
    socket.opened();
    socket.answer({ refused: "signin_required", message: "" });
  });
  await waitFor(() => expect(window.location.pathname).toBe("/signin"));
  expect(new URLSearchParams(window.location.search).get("next")).toBe("/terminal?tab=1");
});

test("tabs are switched by a pointer, by the arrow keys in the rail, and by Control, Shift and the arrows anywhere", async () => {
  serve("/terminal?tab=1");
  await joined(web1, db2, cache3);

  press(tab("db2"));
  expect(selected()).toBe("db2");
  expect(window.location.search).toBe("?tab=2");
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("textbox", { name: "Terminal on db2" })));

  // The arrow keys move along the list, showing each tab, and leave the focus there.
  act(() => tab("db2").focus());
  fireEvent.keyDown(tab("db2"), { key: "ArrowDown" });
  expect(selected()).toBe("cache3");
  expect(document.activeElement).toBe(tab("cache3"));

  // The chords, from the terminal: wrapping, and never reaching the shell.
  act(() => screen.getByRole("textbox", { name: "Terminal on cache3" }).focus());
  chord("ArrowRight");
  expect(selected()).toBe("web1");
  chord("ArrowLeft");
  expect(selected()).toBe("cache3");
  chord("ArrowLeft");
  expect(selected()).toBe("db2");
  expect(attachments().map((socket) => socket.typed).join(""), "no key reached a shell").toBe("");
});

test("a tab's terminal takes the focus once its emulator has loaded", async () => {
  loading.hold = true;
  serve("/terminal");
  const socket = await joined(web1);
  await waitFor(() => expect(loading.held).toHaveLength(1));
  press(screen.getByRole("button", { name: /^New tab/ }));
  press(within(await screen.findByRole("dialog")).getByRole("option", { name: /^db2opsOpen/ }));
  await answers(socket, await asked(socket, 1), { tab: "b2" }, web1, db2);
  await waitFor(() => expect(loading.held).toHaveLength(2));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  act(() => loading.held[1]?.());
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("textbox", { name: "Terminal on db2" })));
});

test("the chords never reach the emulator", async () => {
  serve("/terminal?tab=2");
  await joined(web1, db2);
  const typed = vi.fn();
  const textbox = await screen.findByRole("textbox", { name: "Terminal on db2" });
  textbox.addEventListener("keydown", typed);
  act(() => textbox.focus());
  for (const [key, code] of [
    ["ArrowLeft", "ArrowLeft"],
    ["ArrowRight", "ArrowRight"],
    ["K", "KeyK"],
  ] as const) {
    fireEvent.keyDown(textbox, { key, code, ctrlKey: true, shiftKey: true });
  }
  expect(typed).not.toHaveBeenCalled();
  fireEvent.keyDown(textbox, { key: "ArrowLeft", code: "ArrowLeft", ctrlKey: true });
  expect(typed).toHaveBeenCalledTimes(1);
});

test("More changes the text size and copies the selection, and ends nothing", async () => {
  serve("/terminal");
  await joined(web1);
  const more = () => press(within(screen.getByRole("region", { name: "Terminal" })).getByRole("button", { name: "More" }));
  more();
  expect((await screen.findAllByRole("menuitem")).map((item) => item.textContent)).toEqual([
    "Larger text",
    "Smaller text",
    "Copy the selection",
    "Announce output",
  ]);
  fireEvent.click(screen.getByRole("menuitem", { name: "Larger text" }));
  await waitFor(() => expect(screen.getByRole("textbox", { name: "Terminal on web1" }).getAttribute("data-font")).toBe("16"));

  emulator("web1").selection = "uptime";
  const copied = clipboard();
  more();
  fireEvent.click(await screen.findByRole("menuitem", { name: "Copy the selection" }));
  await waitFor(() => expect(copied).toHaveBeenCalledWith("uptime"));
});

test("Files opens a column docked in the island, named for the tab's Host", async () => {
  serve("/terminal");
  await joined(web1);
  const files = screen.getByRole("button", { name: "Files" });
  expect(files.getAttribute("aria-pressed")).toBe("false");
  press(files);
  expect(files.getAttribute("aria-pressed")).toBe("true");
  const column = screen.getByRole("complementary", { name: "Files" });
  expect(within(column).getByRole("heading").textContent).toBe("Files · on web1");
  await expectAccessible();
  press(within(column).getByRole("button", { name: "Close Files" }));
  expect(screen.queryByRole("complementary", { name: "Files" })).toBeNull();
});

test("a Workspace holds at most 16 tabs: New tab is then disabled and says why, and Open terminal asks nothing", async () => {
  serve("/terminal");
  const socket = await joined(...Array.from({ length: 16 }, (_, n) => running(`t${n}`, `h${n + 1}`)));
  expect(tabs()).toHaveLength(16);
  expect(screen.getByRole("button", { name: /^New tab/ }).hasAttribute("disabled")).toBe(true);
  expect(screen.getByText("The terminal holds at most 16 tabs. Close a tab to open another.")).toBeDefined();

  act(() => navigate("/hosts/web1/terminal"));
  await waitFor(() => expect(window.location.pathname).toBe("/terminal"));
  await waitFor(() => expect(screen.getAllByText("The terminal holds at most 16 tabs. Close a tab to open another.")).toHaveLength(2));
  expect(requestsOn(socket)).toEqual([]);
});

test("/hosts/<name>/terminal adds a tab, then shows it, and the tabs stay as they were while the person visits other pages", async () => {
  serve("/hosts/db2/terminal");
  const socket = await joined(web1);
  const open = await asked(socket, 1);
  expect(open).toEqual({ request: 1, open: "db2", cols: 80, rows: 24 });
  await answers(socket, open, { tab: "b2" }, web1, db2);
  await waitFor(() => expect(window.location.pathname + window.location.search).toBe("/terminal?tab=2"));
  expect(selected()).toBe("db2");
  // The page showed web1 until the Workspace named the new tab, and stays attached to it.
  const first = await attachment(2);
  act(() => first.opened());
  expect(first.first).toMatchObject({ attach: db2.session });
  await attached(first, "first");

  // Another page: the tabs stay in the rail, none raised, and the terminal is hidden and kept.
  act(() => navigate("/hosts"));
  await screen.findByRole("heading", { level: 1, name: "Hosts" });
  expect(tabs()).toEqual(["web1", "db2"]);
  expect(tab("db2").className).not.toContain("island");
  act(() => first.output(" second"));
  await expectAccessible();

  // Choosing a tab from another page goes to the terminal showing it.
  act(() => navigate("/secrets"));
  press(tab("db2"));
  await waitFor(() => expect(window.location.pathname + window.location.search).toBe("/terminal?tab=2"));
  expect(emulator("db2").mounts).toBe(1);
  await waitFor(() => expect(emulator("db2").written).toBe("first second"));
  expect(attachments()).toHaveLength(2);
});

test("the rail lists the Workspace on every page, before the terminal is ever opened", async () => {
  serve("/hosts");
  await screen.findByRole("heading", { level: 1, name: "Hosts" });
  await joined(web1, db2);
  expect(tabs()).toEqual(["web1", "db2"]);
  expect(attachments(), "no page shows a tab yet").toHaveLength(0);
});

test("Control, Shift and K on another page goes to the terminal and opens the switcher", async () => {
  serve("/hosts");
  await screen.findByRole("heading", { level: 1, name: "Hosts" });
  const socket = await joined();
  fireEvent.keyDown(document.body, { key: "K", code: "KeyK", ctrlKey: true, shiftKey: true });
  const dialog = await screen.findByRole("dialog", { name: "Go to a terminal" });
  expect(window.location.pathname).toBe("/terminal");
  const field = within(dialog).getByRole("textbox", { name: "Go to a terminal" });
  await waitFor(() => expect(document.activeElement).toBe(field));
  fireEvent.keyDown(field, { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(screen.getByRole("heading", { level: 1, name: "No terminal is open" })).toBeDefined();
  await expectAccessible();
  press(screen.getByRole("button", { name: "db2ops" }));
  expect(await asked(socket, 1)).toMatchObject({ open: "db2" });
});

test("a reload attaches to the tab it shows and ends nothing, and the key the page before kept is removed", async () => {
  sessionStorage.setItem("koko.terminal.sessions", JSON.stringify([web1.session]));
  serve("/terminal?tab=1");
  const socket = await joined(web1, db2);
  expect(sessionStorage.getItem("koko.terminal.sessions")).toBeNull();
  const first = await attachment();
  act(() => first.opened());
  expect(first.first).toEqual({ protocol: 2, attach: web1.session, cols: 80, rows: 24 });
  expect(requestsOn(socket), "no close, no open").toEqual([]);
  expect(attachments()).toHaveLength(1);
});

test("signing out detaches: the sockets close and nothing is closed or ended", async () => {
  serve("/terminal");
  const socket = await joined(web1);
  const first = await attachment();
  await attached(first);
  act(() => navigate("/signin"));
  expect(await screen.findByRole("heading", { level: 1, name: "Sign in" })).toBeDefined();
  expect(socket.closed).toBe(true);
  expect(requestsOn(socket)).toEqual([]);
  expect(first.framesSent.at(-1), "the attachment's close, which detaches it").toMatchObject({ kind: "close", reason: "" });
  expect(first.closed).toBe(true);
});

test("a window crossing 768 pixels keeps every tab's emulator and its one attachment", async () => {
  serve("/terminal");
  await joined(web1, db2);
  await attached(await attachment());
  press(tab("db2"));
  await attached(await attachment(2));
  act(() => setViewportWidth(390));
  act(() => setViewportWidth(1280));
  act(() => setViewportWidth(700));
  expect(emulator("web1").mounts).toBe(1);
  expect(emulator("db2").mounts).toBe(1);
  expect(attachments()).toHaveLength(2);
  expect(attachments().every((socket) => !socket.closed)).toBe(true);
  // On a phone the terminal's own row takes the frame's place, starting with the tab button, named by what it shows.
  act(() => setViewportWidth(390));
  expect(screen.queryByRole("navigation", { name: "Main" })).toBeNull();
  expect(screen.getByRole("button", { name: "db2, 2 open. Show the tabs" })).toBeDefined();
  await expectAccessible();
});

test("the terminal speaks Simplified Chinese", async () => {
  preferLanguages("zh-CN");
  serve("/terminal");
  const socket = await joined({ id: "a1", host: "web1", label: "web1", state: "restarted" }, db2);
  expect(within(shown()).getByRole("status").textContent).toBe("koko 服务器已重启。");
  expect(document.title).toBe("web1 · 终端 · koko");
  expect(screen.getByRole("tablist", { name: "终端标签页" })).toBeDefined();
  await answers(socket, await asked(socket, 1), { tab: "a1" }, { id: "a1", host: "web1", label: "web1", state: "refused", refused: { code: "unknown_host", message: "" } }, db2);
  expect((await within(shown()).findByRole("alert")).textContent).toBe("不存在名为 web1 且当前用户可见的主机。");
  expect(within(shown()).getByRole("button", { name: "重试" })).toBeDefined();
  await expectAccessible();
  const named = (name: string) => screen.getByRole("tab", { name: new RegExp(name) });
  press(named("db2"));
  press(named("web1").querySelector("[data-close]") as Element);
  await answers(socket, await asked(socket, 2), {}, db2);
  press(named("db2").querySelector("[data-close]") as Element);
  const question = await screen.findByRole("alertdialog", { name: "是否关闭 db2？" });
  expect(question.textContent).toContain("其 SSH 会话将在所有打开它的位置结束。");
  await expectAccessible();
});
