import { useSyncExternalStore } from "react";

import type { ConnectionState } from "./connection";
import type { EmulatorHandle, FontSize } from "./emulator/Emulator";
import { keptSessions } from "./kept";

// The terminal workspace's state: its tabs, which live for the document's
// life, so moving about the web UI ends nothing (P1 spec: a terminal in the
// web UI, "The page"). The rail's Terminal section reads it on every page,
// so this module is in the first load, and imports nothing of the
// terminal's own code but types: the workspace that opens the SSH Sessions
// and draws the terminals is a chunk of its own (TerminalPage.tsx), loaded with
// the first terminal.

/** maxTabs is the most tabs a page holds: a Sign-in's limit of SSH Sessions. */
export const maxTabs = 16;

/** terminalPath is the workspace's page. */
export const terminalPath = "/terminal";

/** Tab is one terminal tab: one SSH Session at a time to its Host. */
export interface Tab {
  /** key names the tab for the document's life. */
  key: number;
  host: string;
  /** label names the tab: its Host, numbered from the second tab to the same Host ("web1 (2)"). */
  label: string;
  state: ConnectionState;
  /** opening counts the SSH Sessions the tab has opened: Try again and Open a new one each open the next. */
  opening: number;
  /** cwd is the directory its shell last reported (OSC 7). */
  cwd?: string;
  /** clipboard is the text a program in it asks to copy (OSC 52), not yet answered. */
  clipboard?: string;
  /** ready says its emulator has loaded and laid out its grid, so it can take the focus. */
  ready?: boolean;
  /** failed says its emulator could not load. */
  failed?: boolean;
}

/** Workspace is the workspace's state. */
export interface Workspace {
  tabs: readonly Tab[];
  /** active is the key of the tab shown. */
  active: number | undefined;
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
  /** closing is the key of the tab whose closing asks first, while its shell may still run. */
  closing: number | undefined;
}

/** Runtime is what the workspace's chunk keeps for a tab: its connection and its emulator. */
export interface Runtime {
  /** end ends the tab's SSH Session: the stream's close, or an end while reconnecting. */
  end?: () => void;
  emulator?: EmulatorHandle | null;
}

const initial: Workspace = {
  tabs: [],
  active: undefined,
  loaded: false,
  switcher: false,
  fontSize: 14,
  announce: false,
  tabListId: undefined,
  focusRequest: 0,
  closing: undefined,
};

let state = initial;
let nextKey = 1;
let started = false;
const runtimes = new Map<number, Runtime>();
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

/** shellLives tells whether a tab's shell runs, attached or not, so that closing it or ending it asks first. */
export function shellLives(tab: Tab): boolean {
  return tab.state.status === "connected" || tab.state.status === "reconnecting";
}

/** settled tells whether a tab's last SSH Session is over: ended, refused, or never answered. */
function settled(tab: Tab): boolean {
  const status = tab.state.status;
  return status === "ended" || status === "refused" || status === "unreachable";
}

/** runtime is a tab's runtime, made on first use. */
export function runtime(key: number): Runtime {
  let found = runtimes.get(key);
  if (!found) {
    found = {};
    runtimes.set(key, found);
  }
  return found;
}

/** patchTab changes a tab, if it is still open. */
export function patchTab(key: number, change: Partial<Tab>): void {
  if (state.tabs.some((tab) => tab.key === key)) {
    set({ tabs: state.tabs.map((tab) => (tab.key === key ? { ...tab, ...change } : tab)) });
  }
}

/** labelFor is a new tab's label: the Host's name, else with the lowest number from 2 no open tab has. */
function labelFor(host: string): string {
  const taken = new Set(state.tabs.map((tab) => tab.label));
  let label = host;
  for (let n = 2; taken.has(label); n++) {
    label = `${host} (${n})`;
  }
  return label;
}

/** addTab opens a tab for host and shows it, giving it the focus, and is its key; past the limit it opens none. */
export function addTab(host: string): number | undefined {
  if (state.tabs.length >= maxTabs) {
    return undefined;
  }
  const key = nextKey++;
  const tab: Tab = { key, host, label: labelFor(host), state: { status: "connecting" }, opening: 1 };
  set({ tabs: [...state.tabs, tab], active: key, loaded: true, focusRequest: state.focusRequest + 1 });
  return key;
}

/** closeTab ends a tab's SSH Session, if it has one, and takes the tab away, showing its neighbour. */
export function closeTab(key: number): void {
  const index = state.tabs.findIndex((tab) => tab.key === key);
  if (index < 0) {
    return;
  }
  runtimes.get(key)?.end?.();
  runtimes.delete(key);
  const tabs = state.tabs.filter((tab) => tab.key !== key);
  const active = state.active === key ? tabs[Math.min(index, tabs.length - 1)]?.key : state.active;
  set({ tabs, active, closing: state.closing === key ? undefined : state.closing });
}

/** askClose closes a tab, asking first while its shell may still run. */
export function askClose(key: number): void {
  const tab = state.tabs.find((each) => each.key === key);
  if (tab && shellLives(tab)) {
    set({ closing: key });
  } else {
    closeTab(key);
  }
}

/** cancelClose answers the question askClose asked with no. */
export function cancelClose(): void {
  set({ closing: undefined });
}

/** showTab shows a tab, and with focus gives its terminal the focus. */
export function showTab(key: number, { focus = false } = {}): void {
  if (state.tabs.some((tab) => tab.key === key)) {
    set({ active: key, focusRequest: focus ? state.focusRequest + 1 : state.focusRequest });
  }
}

/** stepTab shows the tab before (-1) or after (1) the active one, wrapping, and gives it the focus. */
export function stepTab(by: 1 | -1): void {
  const { tabs, active } = state;
  const index = tabs.findIndex((tab) => tab.key === active);
  const next = tabs[(index + by + tabs.length) % tabs.length];
  if (next) {
    showTab(next.key, { focus: true });
  }
}

/** reopen opens a new SSH Session in a tab whose last is over: Open a new one, Try again. */
export function reopen(key: number): void {
  const tab = state.tabs.find((each) => each.key === key);
  if (tab && settled(tab)) {
    patchTab(key, { state: { status: "connecting" }, opening: tab.opening + 1, clipboard: undefined });
  }
}

/** setSwitcher opens or closes the switcher; opening it loads the workspace. */
export function setSwitcher(open: boolean): void {
  set({ switcher: open, loaded: state.loaded || open });
}

/** setFontSize sets every terminal's text size. */
export function setFontSize(fontSize: FontSize): void {
  set({ fontSize });
}

/** setAnnounce turns the announcing of output on or off. */
export function setAnnounce(announce: boolean): void {
  set({ announce });
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
  }
}

/**
 * panelId is the id of a tab's panel: the one the rail's tab list names
 * for it (React Aria's Tabs names a tab's panel after its list), or one of
 * its own while no list is shown, as on a phone.
 */
export function panelId(key: number, tabListId: string | undefined): string {
  return tabListId === undefined ? `koko-terminal-panel-${key}` : `${tabListId}-tabpanel-${key}`;
}

/** workspaceQuery is the workspace's URL query: its tabs' Hosts in order, and the active one counted from 1. */
export function workspaceQuery(ws: Workspace): string {
  const query = new URLSearchParams();
  for (const tab of ws.tabs) {
    query.append("host", tab.host);
  }
  const index = ws.tabs.findIndex((tab) => tab.key === ws.active);
  if (index >= 0) {
    query.set("tab", String(index + 1));
  }
  const text = query.toString();
  return text === "" ? "" : `?${text}`;
}

/**
 * startWorkspace runs once per document, as the frame first shows: on the
 * workspace's URL it opens a tab for each Host the URL lists, in order, at
 * most 16, the active one kept; elsewhere, should this browser tab have
 * kept SSH Sessions from the document before a reload, it loads the
 * workspace, which ends them. Opening the tabs again ends them first too.
 */
export function startWorkspace(pathname: string, search: string): void {
  if (started) {
    return;
  }
  started = true;
  if (pathname === terminalPath) {
    const query = new URLSearchParams(search);
    const keys = query
      .getAll("host")
      .filter((host) => host !== "")
      .slice(0, maxTabs)
      .map((host) => addTab(host));
    const shown = keys[Number.parseInt(query.get("tab") ?? "", 10) - 1];
    if (shown !== undefined) {
      showTab(shown);
    } else if (keys[0] !== undefined) {
      showTab(keys[0]);
    }
    loadWorkspace();
  } else if (keptSessions().length > 0) {
    loadWorkspace();
  }
}

/**
 * resetWorkspace ends every tab's SSH Session and starts the workspace
 * afresh, as for a new document: the shell's frame going (signing out),
 * and each test.
 */
export function resetWorkspace(): void {
  for (const each of runtimes.values()) {
    each.end?.();
  }
  runtimes.clear();
  started = false;
  set(initial);
}
