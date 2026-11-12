import { act, configure, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import { type Host, HostList, HostShow } from "../api/host";
import { alice, host } from "../pages/host/records.fixture";
import { navigate } from "../shell/router";
import { expectAccessible } from "../test/axe";
import { clipboard } from "../test/clipboard";
import { type FakeApi, fakeApi } from "../test/fakeApi";
import { setViewportWidth } from "../test/media";
import { actAs, preferLanguages, renderAt } from "../test/pages";
import { FakeWebSocket, useFakeLocks } from "../test/terminal";
import type { EmulatorProps } from "./emulator/Emulator";

// The terminal workspace, its tabs in the rail and a tab's states, run as
// the whole web UI against the fake API, with the emulator replaced by a
// fake that records what it is given and the WebSocket by one the test
// answers as the koko server.

vi.setConfig({ testTimeout: 20_000 });
configure({ asyncUtilTimeout: 5_000 });

/** FakeEmulator is what a test sees of one tab's emulator. */
interface FakeEmulator {
  props: EmulatorProps;
  /** mounts counts the times it was mounted: a remount would lose the screen. */
  mounts: number;
  written: string;
  selection: string;
}

const emulators = vi.hoisted(() => new Map<string, FakeEmulator>());

/** loading holds back each emulator mounted while hold is set, as a core still loading does, until it is let go. */
const loading = vi.hoisted(() => ({ hold: false, held: [] as (() => void)[] }));

vi.mock("./emulator/Emulator", async () => {
  const { useEffect, useImperativeHandle, useRef, useState } = await import("react");
  const { createElement } = await import("react");
  function Emulator(props: EmulatorProps) {
    const record = emulators.get(props.label) ?? { props, mounts: 0, written: "", selection: "" };
    record.props = props;
    emulators.set(props.label, record);
    const input = useRef<HTMLTextAreaElement>(null);
    const [loaded, setLoaded] = useState(!loading.hold);
    useImperativeHandle(props.ref, () => ({
      write: (bytes: Uint8Array) => {
        record.written += new TextDecoder().decode(bytes);
      },
      focus: () => input.current?.focus(),
      fit: () => {},
      selection: () => record.selection,
      pressKey: () => {},
    }));
    useEffect(() => {
      record.mounts++;
      if (loaded) {
        props.onReady(120, 36);
      } else {
        loading.held.push(() => {
          setLoaded(true);
          props.onReady(120, 36);
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

const tunnel = (n: number) => n.toString(16).padStart(32, "0");

beforeEach(() => {
  FakeWebSocket.sockets = [];
  vi.stubGlobal("WebSocket", FakeWebSocket);
});

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
  api.on(HostShow, ({ host: name }) => ({ host: hosts.find((h) => h.name === name) ?? hosts[0]!, services: [] }));
  renderAt(path);
  return api;
}

/** sockets waits for the page to have opened n WebSockets, and is them. */
async function sockets(n: number): Promise<FakeWebSocket[]> {
  await waitFor(() => expect(FakeWebSocket.sockets).toHaveLength(n));
  return [...FakeWebSocket.sockets];
}

/** connect has the koko server take the open on socket and start the shell. */
async function connect(socket: FakeWebSocket | undefined, id: string): Promise<void> {
  await act(async () => {
    socket?.opened();
    socket?.answer({ tunnel: id });
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

test("the workspace opens the URL's Hosts as tabs in order, the active one kept, and names the page after it", async () => {
  serve("/terminal?host=web1&host=db2&host=web1&tab=2");
  const opened = await sockets(3);
  for (const socket of opened) {
    act(() => socket.opened());
  }
  expect(opened.map((socket) => socket.first)).toEqual([
    { protocol: 1, open: "web1", cols: 120, rows: 36 },
    { protocol: 1, open: "db2", cols: 120, rows: 36 },
    { protocol: 1, open: "web1", cols: 120, rows: 36 },
  ]);
  expect(opened[0]?.url).toBe(`ws://${window.location.host}/terminal/socket`);
  expect(tabs()).toEqual(["web1", "db2", "web1 (2)"]);
  expect(selected()).toBe("db2");
  expect(document.title).toBe("db2 · Terminal · koko");
  expect(within(shown()).getByRole("status").textContent).toBe("Connecting to db2…");
  // The tab's panel is the one its tab names.
  expect(document.getElementById(tab("db2").getAttribute("aria-controls") ?? "")).toBe(shown());
  expect(window.location.pathname + window.location.search).toBe("/terminal?host=web1&host=db2&host=web1&tab=2");
  await expectAccessible();

  await connect(opened[1], tunnel(2));
  act(() => opened[1]?.output("deploy@db2:~$ "));
  await waitFor(() => expect(emulator("db2").written).toBe("deploy@db2:~$ "));
  expect(within(shown()).queryByRole("status")).toBeNull();
  expect(within(screen.getByRole("region", { name: "Terminal" })).getByText("Connected")).toBeDefined();
  await expectAccessible();
});

test("keys typed reach the shell, and each state of a tab shows over its terminal without moving it", async () => {
  serve("/terminal?host=web1");
  const [socket] = await sockets(1);
  await connect(socket, tunnel(1));
  fireEvent.keyDown(screen.getByRole("textbox", { name: "Terminal on web1" }), { key: "l" });
  await waitFor(() => expect(socket?.framesSent.some((frame) => frame.kind === "data")).toBe(true));

  // Reconnecting: the terminal stays, dimmed, under a notice with Try now.
  await act(async () => socket?.drop());
  await waitFor(() =>
    expect(within(shown()).getByRole("status").textContent).toContain(
      "Reconnecting to the koko server… Keys typed now are sent once it is back.",
    ),
  );
  expect(within(shown()).getByRole("button", { name: "Try now" })).toBeDefined();
  expect(tab("web1").textContent).toContain("Reconnecting");
  await expectAccessible();
  const resume = (await sockets(2))[1];
  await act(async () => {
    resume?.opened();
    resume?.answer({ offset: 0 });
  });
  await waitFor(() => expect(within(shown()).queryByRole("status")).toBeNull());

  // A program asks to copy: the page asks first.
  const copied = clipboard();
  act(() => emulator("web1").props.onClipboardWrite("uptime\nsecond line"));
  const ask = within(shown()).getByRole("status");
  expect(ask.textContent).toContain("web1 asks to copy 18 characters to the clipboard.");
  expect(ask.textContent).toContain("uptime");
  await expectAccessible();
  fireEvent.click(within(ask).getByRole("button", { name: "Copy" }));
  await waitFor(() => expect(copied).toHaveBeenCalledWith("uptime\nsecond line"));
  await waitFor(() => expect(within(shown()).queryByRole("status")).toBeNull());
  act(() => emulator("web1").props.onClipboardWrite("x"));
  fireEvent.click(within(shown()).getByRole("button", { name: "Dismiss" }));
  expect(within(shown()).queryByRole("status")).toBeNull();

  // The shell exits: why, and a new one in the same tab.
  await act(async () => {
    resume?.records({ type: "exitStatus", status: 3 });
    resume?.frames({ kind: "close", ack: 0, reason: "" });
  });
  await waitFor(() =>
    expect(within(shown()).getByRole("status").textContent).toContain("The shell on web1 exited with status 3."),
  );
  expect(tab("web1").textContent).toContain("Ended");
  await expectAccessible();
  fireEvent.click(within(shown()).getByRole("button", { name: "Open a new one" }));
  const again = (await sockets(3))[2];
  act(() => again?.opened());
  expect(again?.first).toEqual({ protocol: 1, open: "web1", cols: 120, rows: 36 });
  expect(within(shown()).getByRole("status").textContent).toBe("Connecting to web1…");
});

test("an SSH Session's end says why: a signal, the koko server's notice, a restart, a lost connection, or this page", async () => {
  const endings: [string, (socket: FakeWebSocket) => void, string][] = [
    [
      "a signal",
      (socket) => {
        socket.records({ type: "exitSignal", signal: "KILL" });
        socket.frames({ kind: "close", ack: 0, reason: "" });
      },
      "The shell on web1 ended by signal KILL.",
    ],
    [
      "a notice",
      (socket) => socket.frames({ kind: "close", ack: 0, reason: "access to web1 was withdrawn: bob was Removed; this SSH Session is closed." }),
      "The SSH Session to web1 has ended.access to web1 was withdrawn: bob was Removed; this SSH Session is closed.",
    ],
    [
      "a restart",
      (socket) => socket.frames({ kind: "close", ack: 0, reason: "the koko server is restarting; this SSH Session has ended." }),
      "The koko server is restarting; this SSH Session has ended.",
    ],
  ];
  serve("/terminal?host=web1");
  for (const [what, end, says] of endings) {
    const before = FakeWebSocket.sockets.length;
    const socket = (await sockets(before + 1))[before];
    await connect(socket, tunnel(before + 1));
    await act(async () => socket && end(socket));
    await waitFor(() => expect(within(shown()).getByRole("status").textContent, what).toContain(says));
    fireEvent.click(within(shown()).getByRole("button", { name: "Open a new one" }));
  }

  // A lost connection the Gateway will not resume.
  const socket = (await sockets(4))[3];
  await connect(socket, tunnel(9));
  await act(async () => socket?.drop());
  const resume = (await sockets(5))[4];
  await act(async () => {
    resume?.opened();
    resume?.answer({ refused: "resume_refused", message: "web1 closed the connection" });
  });
  await waitFor(() =>
    expect(within(shown()).getByRole("status").textContent).toMatch(
      /Lost the connection to the koko server at \d\d:\d\d( [AP]M)?; the SSH Session to web1 could not be resumed\.web1 closed the connectionOpen a new one/,
    ),
  );
  await expectAccessible();
});

test("each refusal is said, with Try again and Close tab, and one of an out-of-date page asks for a reload", async () => {
  const refusals: [string, string, string][] = [
    ["unknown_host", 'no Host is named "web1".', "No Host you can see is named web1."],
    ["host_registered", "web1 is Registered: …", "web1 is Registered, so koko does not log in to it yet.web1 is Registered: …"],
    ["ssh_session_limit", "this Sign-in already has 16 SSH Sessions open, …", "The most SSH Sessions koko allows are already open."],
    ["host_unreachable", "web1 is down or unreachable: …", "koko could not reach web1. This SSH Session did not start.web1 is down"],
    ["shell_refused", "web1 refused the shell", "web1 refused to start a shell."],
    ["revoked", "", "This device's Device Session was Revoked."],
    ["something_new", "a refusal of a later koko", "The koko server refused this SSH Session to web1.a refusal of a later koko"],
  ];
  serve("/terminal?host=web1");
  await sockets(1);
  for (const [code, message, says] of refusals) {
    const socket = FakeWebSocket.sockets.at(-1);
    await act(async () => {
      socket?.opened();
      socket?.answer({ refused: code, message });
    });
    const alert = await within(shown()).findByRole("alert");
    expect(alert.textContent, code).toContain(says);
    expect(tab("web1").textContent).toContain("Refused");
    await expectAccessible();
    const count = FakeWebSocket.sockets.length;
    fireEvent.click(within(shown()).getByRole("button", { name: "Try again" }));
    await sockets(count + 1);
  }

  // An open that got no answer.
  await act(async () => FakeWebSocket.sockets.at(-1)?.drop());
  expect((await within(shown()).findByRole("alert")).textContent).toBe(
    "The koko server could not be reached. Check the network connection and try again.",
  );
  expect(tab("web1").textContent).toContain("Not connected");

  // An out-of-date page.
  fireEvent.click(within(shown()).getByRole("button", { name: "Try again" }));
  const socket = (await sockets(FakeWebSocket.sockets.length))[FakeWebSocket.sockets.length - 1];
  await act(async () => {
    socket?.opened();
    socket?.answer({ refused: "reload_page", message: "" });
  });
  expect((await within(shown()).findByRole("alert")).textContent).toBe("This page is out of date. Reload it.");
  expect(within(shown()).getByRole("button", { name: "Reload" })).toBeDefined();
  expect(within(shown()).queryByRole("button", { name: "Try again" })).toBeNull();
  await expectAccessible();

  // A tab whose shell never started closes at once.
  press(tab("web1").querySelector("[data-close]") as Element);
  expect(screen.queryByRole("alertdialog")).toBeNull();
  expect(screen.queryByRole("tablist", { name: "Terminal tabs" })).toBeNull();
  expect(screen.getByRole("heading", { level: 1, name: "No terminal is open" })).toBeDefined();
});

test("a refusal for want of a Sign-in goes to Sign in, which comes back to the terminal", async () => {
  serve("/terminal?host=web1");
  const [socket] = await sockets(1);
  await act(async () => {
    socket?.opened();
    socket?.answer({ refused: "signin_required", message: "" });
  });
  await waitFor(() => expect(window.location.pathname).toBe("/signin"));
  expect(new URLSearchParams(window.location.search).get("next")).toBe("/terminal?host=web1&tab=1");
});

test("the switcher shows an open tab or opens a Host, and two tabs to one Host are told apart", async () => {
  serve("/terminal?host=web1");
  await connect((await sockets(1))[0], tunnel(1));
  press(screen.getByRole("button", { name: /^New tab/ }));
  const dialog = await screen.findByRole("dialog", { name: "Go to a terminal" });
  const field = within(dialog).getByRole("textbox", { name: "Go to a terminal" });
  await waitFor(() => expect(document.activeElement).toBe(field));
  expect(within(dialog).getAllByRole("option").map((option) => option.textContent)).toEqual([
    "Connectedweb1Shown",
    "web1webOpen",
    "db2opsOpen",
    "cache3opsOpen",
  ]);
  await expectAccessible();

  // A browser tells of typing before the change, which has the first match take the virtual focus.
  fireEvent(field, new InputEvent("beforeinput", { inputType: "insertText", data: "ops", bubbles: true }));
  fireEvent.change(field, { target: { value: "ops" } });
  await waitFor(() =>
    expect(within(dialog).getAllByRole("option").map((option) => option.textContent)).toEqual(["db2opsOpen", "cache3opsOpen"]),
  );
  fireEvent.keyDown(field, { key: "Enter" });
  fireEvent.keyUp(field, { key: "Enter" });
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(tabs()).toEqual(["web1", "db2"]);
  expect(selected()).toBe("db2");
  expect(window.location.search).toBe("?host=web1&host=db2&tab=2");
  await sockets(2);

  // The same Host again, labelled apart, then an open tab shown.
  fireEvent.keyDown(document.activeElement ?? document.body, { key: "K", code: "KeyK", ctrlKey: true, shiftKey: true });
  press(within(await screen.findByRole("dialog")).getByRole("option", { name: /^web1webOpen/ }));
  await waitFor(() => expect(tabs()).toEqual(["web1", "db2", "web1 (2)"]));
  expect(window.location.search).toBe("?host=web1&host=db2&host=web1&tab=3");
  chord("K", "KeyK");
  press(within(await screen.findByRole("dialog")).getByRole("option", { name: /^Connectedweb1$/ }));
  await waitFor(() => expect(selected()).toBe("web1"));
  expect(window.location.search).toBe("?host=web1&host=db2&host=web1&tab=1");
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("textbox", { name: "Terminal on web1" })));
});

test("tabs are switched by a pointer, by the arrow keys in the rail, and by Control, Shift and the arrows anywhere", async () => {
  serve("/terminal?host=web1&host=db2&host=cache3&tab=1");
  await sockets(3);

  press(tab("db2"));
  expect(selected()).toBe("db2");
  expect(window.location.search).toBe("?host=web1&host=db2&host=cache3&tab=2");
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
  expect(
    FakeWebSocket.sockets.flatMap((socket) => socket.framesSent),
    "no key reached a shell",
  ).toEqual([]);
});

test("a new tab's terminal takes the focus once its emulator has loaded", async () => {
  loading.hold = true;
  serve("/terminal?host=web1");
  await waitFor(() => expect(loading.held).toHaveLength(1));
  press(screen.getByRole("button", { name: /^New tab/ }));
  press(within(await screen.findByRole("dialog")).getByRole("option", { name: /^db2opsOpen/ }));
  await waitFor(() => expect(loading.held).toHaveLength(2));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  act(() => loading.held[1]?.());
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("textbox", { name: "Terminal on db2" })));
});

test("the chords never reach the emulator", async () => {
  serve("/terminal?host=web1&host=db2");
  await sockets(2);
  const typed = vi.fn();
  const textbox = screen.getByRole("textbox", { name: "Terminal on db2" });
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

test("closing a tab whose shell runs asks first, by its mark or by Delete; one that has ended closes at once", async () => {
  serve("/terminal?host=web1&host=db2&host=cache3&tab=1");
  const opened = await sockets(3);
  await connect(opened[0], tunnel(1));
  await connect(opened[1], tunnel(2));

  // The close mark, which is no button inside the tab, and asks first.
  const mark = tab("web1").querySelector("[data-close]");
  expect(mark?.getAttribute("aria-hidden")).toBe("true");
  press(mark as Element);
  const dialog = await screen.findByRole("alertdialog", { name: "End the SSH Session to web1?" });
  expect(selected(), "the mark chooses no tab").toBe("web1");
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  expect(tabs()).toEqual(["web1", "db2", "cache3"]);

  press(mark as Element);
  fireEvent.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "End SSH Session" }));
  await waitFor(() => expect(tabs()).toEqual(["db2", "cache3"]));
  expect(opened[0]?.framesSent.at(-1)).toEqual({ kind: "close", ack: 0, reason: "" });
  expect(selected()).toBe("db2");

  // Delete on a focused tab.
  act(() => tab("db2").focus());
  fireEvent.keyDown(tab("db2"), { key: "Delete" });
  fireEvent.click(within(await screen.findByRole("alertdialog", { name: "End the SSH Session to db2?" })).getByRole("button", { name: "End SSH Session" }));
  await waitFor(() => expect(tabs()).toEqual(["cache3"]));

  // A tab still connecting has no shell yet, and Backspace closes it at once.
  act(() => tab("cache3").focus());
  fireEvent.keyDown(tab("cache3"), { key: "Backspace" });
  expect(screen.queryByRole("alertdialog")).toBeNull();
  expect(screen.queryByRole("tablist", { name: "Terminal tabs" })).toBeNull();
  expect(window.location.search).toBe("");
});

test("More changes the text size, copies the selection, and ends the SSH Session after asking", async () => {
  serve("/terminal?host=web1");
  await connect((await sockets(1))[0], tunnel(1));
  const more = () => press(within(screen.getByRole("region", { name: "Terminal" })).getByRole("button", { name: "More" }));
  more();
  fireEvent.click(await screen.findByRole("menuitem", { name: "Larger text" }));
  await waitFor(() => expect(screen.getByRole("textbox", { name: "Terminal on web1" }).getAttribute("data-font")).toBe("16"));

  emulator("web1").selection = "uptime";
  const copied = clipboard();
  more();
  fireEvent.click(await screen.findByRole("menuitem", { name: "Copy the selection" }));
  await waitFor(() => expect(copied).toHaveBeenCalledWith("uptime"));

  more();
  fireEvent.click(await screen.findByRole("menuitem", { name: "End SSH Session" }));
  fireEvent.click(within(await screen.findByRole("alertdialog", { name: "End the SSH Session to web1?" })).getByRole("button", { name: "End SSH Session" }));
  await waitFor(() =>
    expect(within(shown()).getByRole("status").textContent).toContain("The SSH Session to web1 was ended from this page."),
  );
  expect(tabs()).toEqual(["web1"]);
});

test("Files opens a column docked in the island, named for the tab's Host", async () => {
  serve("/terminal?host=web1");
  await sockets(1);
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

test("a page holds at most 16 tabs: New tab is then disabled and says why", async () => {
  const sixteen = Array.from({ length: 17 }, (_, n) => `host=h${n + 1}`).join("&");
  serve(`/terminal?${sixteen}`);
  await sockets(16);
  expect(tabs()).toHaveLength(16);
  expect(screen.getByRole("button", { name: /^New tab/ }).hasAttribute("disabled")).toBe(true);
  expect(screen.getByText("A browser holds at most 16 SSH Sessions at once. Close a tab to open another.")).toBeDefined();

  // Open terminal at 16 adds none, and says why in a notice beside the rail's line.
  act(() => navigate("/hosts/web1/terminal"));
  await waitFor(() => expect(window.location.pathname).toBe("/terminal"));
  expect(tabs()).toHaveLength(16);
  await waitFor(() =>
    expect(screen.getAllByText("A browser holds at most 16 SSH Sessions at once. Close a tab to open another.")).toHaveLength(2),
  );
});

test("/hosts/<name>/terminal adds a tab, and the tabs stay as they were while the person visits other pages", async () => {
  serve("/hosts/web1/terminal");
  await waitFor(() => expect(window.location.pathname + window.location.search).toBe("/terminal?host=web1&tab=1"));
  const [socket] = await sockets(1);
  await connect(socket, tunnel(1));
  act(() => socket?.output("first"));

  // Another page: the tabs stay in the rail, none raised, and the terminal is hidden and kept.
  act(() => navigate("/hosts"));
  await screen.findByRole("heading", { level: 1, name: "Hosts" });
  expect(tabs()).toEqual(["web1"]);
  expect(tab("web1").className).not.toContain("island");
  act(() => socket?.output(" second"));
  await expectAccessible();

  // A Managed Host's page offers Open terminal, which adds a second tab.
  act(() => navigate("/hosts/db2"));
  const open = await screen.findByRole("link", { name: "Open terminal" });
  expect(open.getAttribute("href")).toBe("/hosts/db2/terminal");
  press(open);
  await waitFor(() => expect(tabs()).toEqual(["web1", "db2"]));
  expect(window.location.search).toBe("?host=web1&host=db2&tab=2");

  // Choosing a tab from another page goes to the terminal showing it.
  act(() => navigate("/secrets"));
  press(tab("web1"));
  await waitFor(() => expect(window.location.pathname + window.location.search).toBe("/terminal?host=web1&host=db2&tab=1"));
  expect(emulator("web1").mounts).toBe(1);
  await waitFor(() => expect(emulator("web1").written).toBe("first second"));
  expect(FakeWebSocket.sockets.filter((each) => (each.first as { open?: string } | undefined)?.open === "web1")).toHaveLength(1);
});

test("Control, Shift and K on another page goes to the terminal and opens the switcher", async () => {
  serve("/hosts");
  await screen.findByRole("heading", { level: 1, name: "Hosts" });
  expect(screen.getByRole("button", { name: "Open terminal" })).toBeDefined();
  fireEvent.keyDown(document.body, { key: "K", code: "KeyK", ctrlKey: true, shiftKey: true });
  const dialog = await screen.findByRole("dialog", { name: "Go to a terminal" });
  expect(window.location.pathname).toBe("/terminal");
  const field = within(dialog).getByRole("textbox", { name: "Go to a terminal" });
  await waitFor(() => expect(document.activeElement).toBe(field));
  fireEvent.keyDown(field, { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(screen.getByRole("heading", { level: 1, name: "No terminal is open" })).toBeDefined();
  expect(screen.getAllByRole("button", { name: /^(web1|db2|cache3)/ }).map((card) => card.textContent)).toEqual([
    "web1web",
    "db2ops",
    "cache3ops",
  ]);
  await expectAccessible();
  press(screen.getByRole("button", { name: "db2ops" }));
  await waitFor(() => expect(tabs()).toEqual(["db2"]));
});

test("a reload ends the SSH Sessions the page before it held, then opens each Host again, the active tab kept", async () => {
  useFakeLocks();
  sessionStorage.setItem("koko.terminal.sessions", JSON.stringify([tunnel(1), tunnel(2)]));
  serve("/terminal?host=web1&host=db2&tab=2");
  const ends = await sockets(2);
  for (const socket of ends) {
    act(() => socket.opened());
  }
  expect(ends.map((socket) => socket.first)).toEqual([
    { protocol: 1, end: tunnel(1) },
    { protocol: 1, end: tunnel(2) },
  ]);
  for (const socket of ends) {
    await act(async () => socket.answer({ ended: true }));
  }
  const opens = (await sockets(4)).slice(2);
  for (const socket of opens) {
    act(() => socket.opened());
  }
  expect(opens.map((socket) => socket.first)).toEqual([
    { protocol: 1, open: "web1", cols: 120, rows: 36 },
    { protocol: 1, open: "db2", cols: 120, rows: 36 },
  ]);
  expect(selected()).toBe("db2");
});

test("a window crossing 768 pixels keeps every tab's emulator and its one WebSocket", async () => {
  serve("/terminal?host=web1&host=db2");
  const opened = await sockets(2);
  await connect(opened[0], tunnel(1));
  await connect(opened[1], tunnel(2));
  act(() => setViewportWidth(390));
  act(() => setViewportWidth(1280));
  act(() => setViewportWidth(700));
  expect(emulator("web1").mounts).toBe(1);
  expect(emulator("db2").mounts).toBe(1);
  expect(FakeWebSocket.sockets).toHaveLength(2);
  expect(opened.every((socket) => !socket.closed)).toBe(true);
  // On a phone the bar at the foot starts with the terminal's button, named by what it shows.
  act(() => setViewportWidth(390));
  expect(screen.getByRole("link", { name: "web1, Terminal, 2 open" })).toBeDefined();
  await expectAccessible();
});

test("the terminal speaks Simplified Chinese", async () => {
  preferLanguages("zh-CN");
  serve("/terminal?host=web1");
  const [socket] = await sockets(1);
  act(() => socket?.opened());
  expect(within(shown()).getByRole("status").textContent).toBe("正在连接 web1…");
  expect(document.title).toBe("web1 · 终端 · koko");
  expect(screen.getByRole("tablist", { name: "终端标签页" })).toBeDefined();
  await act(async () => socket?.answer({ refused: "unknown_host", message: 'no Host is named "web1".' }));
  expect((await within(shown()).findByRole("alert")).textContent).toBe("不存在名为 web1 且当前用户可见的主机。");
  expect(within(shown()).getByRole("button", { name: "重试" })).toBeDefined();
  await expectAccessible();
});
