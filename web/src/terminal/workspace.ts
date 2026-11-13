import { useSyncExternalStore } from "react";

import type { Attachment, AttachmentState, Size } from "./connection";
import type { EmulatorHandle, FontSize } from "./emulator/Emulator";
import { type Refusal, type SocketState, type WorkspaceTab, WorkspaceSocket } from "./workspaceSocket";

// The page's view of the User's Workspace (spec #213, "The page"): the
// tabs, their order, labels and states, which the koko server holds and
// sends whole over the Workspace socket, and the page's own state beside
// them: which tab it shows, the switcher, the text size, and per tab its
// attachment and its Files panel. The rail's Terminal section reads it on
// every page, so this module is in the first load, and imports nothing of
// the terminal's own code but types and the Workspace socket: the
// workspace that attaches to the SSH Sessions and draws the terminals is a
// chunk of its own (TerminalPage.tsx), loaded with the first terminal.

/** maxTabs is the most tabs a Workspace holds (the Gateway's maxTabs). */
export const maxTabs = 16;

/** terminalPath is the workspace's page. */
export const terminalPath = "/terminal";

/** Status is a tab's state as the page marks it: the Workspace's, with the page's attachment while its shell runs. */
export type Status = "connecting" | "connected" | "reconnecting" | "refused" | "ended";

/** Tab is one tab of the Workspace, as the koko server gives it, with what this page holds of it. */
export interface Tab extends WorkspaceTab {
  /** attachment is this page's attachment to its SSH Session, once the tab has been shown here. */
  attachment?: AttachmentState;
  /** activePage says this page is its SSH Session's active page, whose box the pty takes. */
  activePage: boolean;
  /** cwd is the directory its shell last reported (OSC 7). */
  cwd?: string;
  /** clipboard is the text a program in it asks to copy (OSC 52), not yet answered. */
  clipboard?: string;
  /** ready says its emulator has loaded, so it can take the focus. */
  ready?: boolean;
  /** failed says its emulator could not load. */
  failed?: boolean;
}

/** OpenRefused is an open this page asked for that the koko server refused: its Host, and why. */
export type OpenRefused = { host: string } & Refusal;

/** Workspace is the workspace's state. */
export interface Workspace {
  tabs: readonly Tab[];
  /** active is the id of the tab shown. */
  active: string | undefined;
  /** synced says a Workspace has come from the koko server since the page started. */
  synced: boolean;
  /** socket is the Workspace socket's state. */
  socket: SocketState;
  /** loaded says the workspace's chunk is wanted: it stays mounted from then on. */
  loaded: boolean;
  /** switcher says the switcher is open. */
  switcher: boolean;
  fontSize: FontSize;
  /** announce has output announced to a screen reader. */
  announce: boolean;
  /** tabListId is the id of the rail's tab list, from which each tab's panel takes its own. */
  tabListId: string | undefined;
  /**
   * focusRequest counts the times the terminal shown was asked to take the
   * focus: a tab chosen by a pointer, the switcher or a chord. A tab chosen
   * with the arrow keys in the rail leaves the focus in the rail.
   */
  focusRequest: number;
  /** closing is the id of the tab whose closing asks first, while its shell may still run. */
  closing: string | undefined;
  /** closedElsewhere is the label of the tab shown that another page closed, until it is announced. */
  closedElsewhere: string | undefined;
  /** atLimit says an open was asked for at 16 tabs, until it is said. */
  atLimit: boolean;
  /**
   * openRefused is the last open this page asked for that the koko server
   * refused, said in place: in the switcher that sent it, else over the
   * terminal, until it is dismissed or the next open is asked for.
   */
  openRefused: OpenRefused | undefined;
  /** ctrl is the key bar's Control: off, applied to the next key, or held. */
  ctrl: Sticky;
  /** alt is the key bar's Alt, as ctrl is Control. */
  alt: Sticky;
}

/** Sticky is a sticky modifier of the key bar: off, applied to the next key only, or held until tapped again. */
export type Sticky = "off" | "once" | "held";

/** Runtime is what the workspace's chunk keeps for a tab: its attachment and its emulator. */
export interface Runtime {
  attachment?: Attachment;
  /** acted says a person acted on the tab before it had an attachment, which its attachment tells once made. */
  acted?: boolean;
  emulator?: EmulatorHandle | null;
  /** pressing says the emulator is encoding a key pressed through pressKey, whose bytes go out as they are. */
  pressing?: boolean;
}

const initial: Workspace = {
  tabs: [],
  active: undefined,
  synced: false,
  socket: { status: "connecting" },
  loaded: false,
  switcher: false,
  fontSize: 14,
  announce: false,
  tabListId: undefined,
  focusRequest: 0,
  closing: undefined,
  closedElsewhere: undefined,
  atLimit: false,
  openRefused: undefined,
  ctrl: "off",
  alt: "off",
};

/** keptKey is where the page before the Workspace kept its SSH Sessions' ids, which the page now removes once. */
const keptKey = "koko.terminal.sessions";

let state = initial;
let started = false;
let socket: WorkspaceSocket | undefined;
/** wantedTab is the tab the URL names, counted from 1, until the first Workspace comes. */
let wantedTab: number | undefined;
/** box is the terminal's box, in cells, at which a tab opens; the workspace's chunk measures it. */
let box: Size = { columns: 80, rows: 24 };
/** closedHere are the tabs this page asked to close, whose going is not announced. */
const closedHere = new Set<string>();
/**
 * reopened are the restarted tabs this page has asked to reopen; reopening
 * is the one asked for last, until its new SSH Session has left starting.
 */
const reopened = new Set<string>();
let reopening: string | undefined;
const runtimes = new Map<string, Runtime>();
const listeners = new Set<() => void>();

function set(next: Partial<Workspace>): void {
  state = { ...state, ...next };
  for (const listener of listeners) {
    listener();
  }
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** useWorkspace is the workspace, followed as it changes. */
export function useWorkspace(): Workspace {
  return useSyncExternalStore(subscribe, () => state);
}

/** workspace is the workspace as it is now. */
export function workspace(): Workspace {
  return state;
}

/** tabStatus is a tab's state as the page marks it. */
export function tabStatus(tab: Tab): Status {
  switch (tab.state) {
    case "ended":
      return "ended";
    case "refused":
      return "refused";
    case "starting":
    case "restarted":
      return "connecting";
    case "running":
      switch (tab.attachment?.status) {
        case "attached":
        case "displaced":
          return "connected";
        case "reconnecting":
          return "reconnecting";
        case "refused":
        case "protocol":
          return "refused";
        default:
          return "connecting";
      }
  }
}

/** shellLives tells whether a tab's shell runs, or is starting, so that closing it asks first. */
export function shellLives(tab: Tab): boolean {
  return tab.state === "running" || tab.state === "starting";
}

/** runtime is a tab's runtime, made on first use. */
export function runtime(id: string): Runtime {
  let found = runtimes.get(id);
  if (!found) {
    found = {};
    runtimes.set(id, found);
  }
  return found;
}

/** patchTab changes the page's own state of a tab, if it is still in the Workspace. */
export function patchTab(id: string, change: Partial<Tab>): void {
  if (state.tabs.some((tab) => tab.id === id)) {
    set({ tabs: state.tabs.map((tab) => (tab.id === id ? { ...tab, ...change } : tab)) });
  }
}

/** boxSize is the terminal's box, in cells, as last measured, at which tabs open and pages attach. */
export function boxSize(): Size {
  return box;
}

/** setBox records the terminal's box, in cells, at which tabs open. */
export function setBox(size: Size): void {
  box = size;
}

/**
 * personActed tells a tab's SSH Session a person acted on it in this page,
 * which makes this page its active one: only for an act in a visible
 * document, never for attaching, reconnecting or resuming.
 */
export function personActed(id: string): void {
  if (document.visibilityState !== "visible") {
    return;
  }
  const rt = runtime(id);
  if (rt.attachment) {
    rt.attachment.active();
  } else {
    rt.acted = true;
  }
}

/** applyWorkspace replaces the tabs with a Workspace from the koko server, keeping the page's own state of each. */
function applyWorkspace(incoming: readonly WorkspaceTab[]): void {
  const before = state.tabs;
  const tabs = incoming.map((each): Tab => {
    const old = before.find((tab) => tab.id === each.id);
    const fields = { id: each.id, host: each.host, label: each.label, state: each.state, session: each.session, ended: each.ended, refused: each.refused };
    if (!old) {
      return { ...fields, activePage: false };
    }
    if (old.session !== undefined && old.session !== each.session) {
      // A new SSH Session, or none: what the page held of the old one goes with it.
      return { ...old, ...fields, attachment: undefined, activePage: false, cwd: undefined, clipboard: undefined };
    }
    return { ...old, ...fields };
  });
  const has = (id: string | undefined) => id !== undefined && tabs.some((tab) => tab.id === id);
  let { active, closedElsewhere } = state;
  if (wantedTab !== undefined) {
    active = (tabs[wantedTab - 1] ?? tabs[0])?.id;
    wantedTab = undefined;
  } else if (active !== undefined && !has(active)) {
    // The tab shown went: the page shows the one beside it, and says so if another page closed it.
    const index = before.findIndex((tab) => tab.id === active);
    if (!closedHere.has(active) && window.location.pathname === terminalPath) {
      closedElsewhere = before[index]?.label;
    }
    active = tabs[Math.min(index, tabs.length - 1)]?.id;
  } else if (active === undefined) {
    active = tabs[0]?.id;
  }
  for (const id of runtimes.keys()) {
    if (!has(id)) {
      runtimes.delete(id);
    }
  }
  set({ tabs, active, synced: true, closedElsewhere, closing: has(state.closing) ? state.closing : undefined });
  reopenRestarted();
}

/**
 * reopenRestarted opens a new SSH Session in each tab the koko server's
 * restart ended, in tab order, at the size of the terminal's box, once
 * this page has the terminal loaded. Each waits until the one before has
 * left starting, running, refused or ended, so the Hosts are dialled one
 * at a time: the koko server answers a reopen before it dials. Another page
 * doing the same at once opens nothing more: the koko server answers a
 * reopen of a tab already starting with the tab as it is.
 */
function reopenRestarted(): void {
  for (const id of reopened) {
    if (!state.tabs.some((tab) => tab.id === id && tab.state === "restarted")) {
      reopened.delete(id);
    }
  }
  const last = state.tabs.find((tab) => tab.id === reopening);
  if (last && (last.state === "restarted" || last.state === "starting")) {
    return;
  }
  reopening = undefined;
  if (!state.loaded || !socket) {
    return;
  }
  const next = state.tabs.find((tab) => tab.state === "restarted" && !reopened.has(tab.id));
  if (!next) {
    return;
  }
  reopened.add(next.id);
  reopening = next.id;
  void socket.request({ reopen: next.id, cols: box.columns, rows: box.rows });
}

/**
 * openTab opens a tab for host and, once the Workspace names it, shows it,
 * giving it the focus; it tells whether it did. Past the limit it asks
 * nothing, and the frame says why; a refusal is kept in openRefused.
 */
export async function openTab(host: string): Promise<boolean> {
  if (state.tabs.length >= maxTabs) {
    set({ atLimit: true });
    return false;
  }
  set({ loaded: true, openRefused: undefined });
  const answer = await (socket?.request({ open: host, cols: box.columns, rows: box.rows }) ?? Promise.resolve({ kind: "lost" as const }));
  if (answer.kind === "tab") {
    showTab(answer.tab, { focus: true, person: true });
    return true;
  }
  if (answer.kind === "refused") {
    if (answer.code === "ssh_session_limit") {
      set({ atLimit: true });
    } else {
      set({ openRefused: { host, code: answer.code, message: answer.message } });
    }
  }
  return false;
}

/** saidAtLimit says the limit has been said. */
export function saidAtLimit(): void {
  set({ atLimit: false });
}

/** dismissOpenRefused takes the refused open's words away. */
export function dismissOpenRefused(): void {
  set({ openRefused: undefined });
}

/** saidClosedElsewhere says the closing of the tab shown by another page has been announced. */
export function saidClosedElsewhere(): void {
  set({ closedElsewhere: undefined });
}

/** closeTab closes a tab, which ends its SSH Session in every page; it goes once the Workspace no longer names it. */
export function closeTab(id: string): void {
  closedHere.add(id);
  set({ closing: state.closing === id ? undefined : state.closing });
  void socket?.request({ close: id }).then(() => closedHere.delete(id));
}

/** askClose closes a tab, asking first while its shell may still run. */
export function askClose(id: string): void {
  const tab = state.tabs.find((each) => each.id === id);
  if (tab && shellLives(tab)) {
    set({ closing: id });
  } else {
    closeTab(id);
  }
}

/** cancelClose answers the question askClose asked with no. */
export function cancelClose(): void {
  set({ closing: undefined });
}

/** reopenTab opens a new SSH Session in a tab whose last is over: Open a new one, Try again. */
export function reopenTab(id: string): void {
  void socket?.request({ reopen: id, cols: box.columns, rows: box.rows });
}

/**
 * showTab shows a tab; with focus it gives its terminal the focus, and with
 * person it is a person's choice, which makes this page the active one.
 */
export function showTab(id: string, { focus = false, person = false } = {}): void {
  if (state.tabs.some((tab) => tab.id === id)) {
    set({ active: id, focusRequest: focus ? state.focusRequest + 1 : state.focusRequest });
    if (person) {
      personActed(id);
    }
  }
}

/** stepTab shows the tab before (-1) or after (1) the active one, wrapping, and gives it the focus. */
export function stepTab(by: 1 | -1): void {
  const { tabs, active } = state;
  const index = tabs.findIndex((tab) => tab.id === active);
  const next = tabs[(index + by + tabs.length) % tabs.length];
  if (next) {
    showTab(next.id, { focus: true, person: true });
  }
}

/** setSwitcher opens or closes the switcher; opening it loads the workspace. */
export function setSwitcher(open: boolean): void {
  set({ switcher: open, loaded: state.loaded || open, openRefused: open ? undefined : state.openRefused });
}

/** setFontSize sets every terminal's text size. */
export function setFontSize(fontSize: FontSize): void {
  set({ fontSize });
}

/** setAnnounce turns the announcing of output on or off. */
export function setAnnounce(announce: boolean): void {
  set({ announce });
}

/**
 * pressKey presses a key in a tab, through its emulator, so it is encoded as
 * the terminal's modes ask, with the key bar's Control and Alt; one applied
 * once is let go after it. name is a KeyboardEvent key: "Escape", "a". A
 * key pressed is a person's act, as a key typed is.
 */
export function pressKey(id: string, name: string): void {
  const rt = runtimes.get(id);
  if (!rt?.emulator) {
    return;
  }
  personActed(id);
  rt.pressing = true;
  try {
    rt.emulator.pressKey(name, { ctrl: state.ctrl !== "off", alt: state.alt !== "off" });
  } finally {
    rt.pressing = false;
  }
  if (state.ctrl === "once" || state.alt === "once") {
    set({ ctrl: state.ctrl === "once" ? "off" : state.ctrl, alt: state.alt === "once" ? "off" : state.alt });
  }
}

/** tapModifier is a tap on the key bar's Control or Alt, a person's act: applied to the next key, then held, then off. */
export function tapModifier(id: string | undefined, which: "ctrl" | "alt"): void {
  if (id !== undefined) {
    personActed(id);
  }
  const next: Record<Sticky, Sticky> = { off: "once", once: "held", held: "off" };
  set({ [which]: next[state[which]] });
}

const decoder = new TextDecoder();

/**
 * takeTyped takes the bytes a tab's emulator gives for what was typed, and
 * tells whether it took them: while the key bar's Control or Alt is on, one
 * printable character, as a phone's soft keyboard types it, is pressed as
 * that key with the modifier. Anything else, and a key pressKey is
 * encoding, is the caller's to send as it is.
 */
export function takeTyped(id: string, bytes: Uint8Array): boolean {
  if ((state.ctrl === "off" && state.alt === "off") || runtimes.get(id)?.pressing === true || bytes.length > 4) {
    return false;
  }
  const text = decoder.decode(bytes);
  if ([...text].length !== 1 || text < " " || text === "\x7f") {
    return false;
  }
  pressKey(id, text);
  return true;
}

/** setTabListId records the id of the rail's tab list while it is shown. */
export function setTabListId(id: string | undefined): void {
  if (state.tabListId !== id) {
    set({ tabListId: id });
  }
}

/** loadWorkspace has the workspace's chunk loaded and mounted, as the terminal's page does. */
export function loadWorkspace(): void {
  if (!state.loaded) {
    set({ loaded: true });
    reopenRestarted();
  }
}

/**
 * panelId is the id of a tab's panel: the one the rail's tab list names
 * for it (React Aria's Tabs names a tab's panel after its list), or one of
 * its own while no list is shown, as on a phone.
 */
export function panelId(id: string, tabListId: string | undefined): string {
  return tabListId === undefined ? `koko-terminal-panel-${id}` : `${tabListId}-tabpanel-${id}`;
}

/** workspaceQuery is the workspace's URL query: the tab shown, counted from 1. */
export function workspaceQuery(ws: Workspace): string {
  const index = ws.tabs.findIndex((tab) => tab.id === ws.active);
  return index >= 0 ? `?tab=${index + 1}` : "";
}

/**
 * startWorkspace runs once per frame, as it first shows: it opens the
 * Workspace socket, and on the workspace's URL loads the workspace and
 * shows the tab the URL names once the Workspace has come. It removes the
 * ids the page before the Workspace kept, once.
 */
export function startWorkspace(pathname: string, search: string): void {
  if (started) {
    return;
  }
  started = true;
  try {
    window.sessionStorage.removeItem(keptKey);
  } catch {
    // A browser that refuses storage kept nothing.
  }
  socket = new WorkspaceSocket({
    onWorkspace: applyWorkspace,
    onState: (next) => set({ socket: next }),
  });
  if (pathname === terminalPath) {
    const tab = Number.parseInt(new URLSearchParams(search).get("tab") ?? "", 10);
    wantedTab = Number.isInteger(tab) && tab >= 1 ? tab : undefined;
    loadWorkspace();
  }
}

/**
 * resetWorkspace leaves the Workspace and starts afresh, as for a new
 * frame: on signing out, and after each test. It ends nothing: the page
 * detaches from every SSH Session, which goes on.
 */
export function resetWorkspace(): void {
  socket?.close();
  socket = undefined;
  for (const each of runtimes.values()) {
    each.attachment?.detach();
  }
  runtimes.clear();
  closedHere.clear();
  reopened.clear();
  reopening = undefined;
  wantedTab = undefined;
  started = false;
  set(initial);
}
