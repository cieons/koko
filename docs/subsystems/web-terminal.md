# The web terminal

The web UI's page that opens SSH Sessions to Managed Hosts in a browser
([ADR 0053](../adr/0053-a-browser-opens-ssh-sessions-through-a-websocket-to-the-gateway.md)):
the workspace and its tabs, the emulator behind one component, and the
page's end of each SSH Session. What it looks like is "The terminal" in
[the web UI's design](../web-design.md#the-terminal-terminal). The
WebSocket it speaks, `/terminal/socket`, is [the web
front's](web-front.md#the-web-terminals-websocket); the stream on it is
[the resumable stream](resumable-stream.md#the-typescript-end); the SSH
Session behind it is [the Gateway's](gateway.md#the-web-terminal). The
phone's layout and key bar (#178) and the Files panel's listing and
uploads (#179) are still to come.

## Modules

Everything is in [`web/src/terminal/`](../../web/src/terminal):

| Module | In | Holds |
| --- | --- | --- |
| `workspace.ts` | the first load | The workspace's state: its tabs (Host, label, state, directory, clipboard request), the active one, the switcher, the text size; the URL's query; starting and resetting it. It imports the terminal's code as types alone |
| `kept.ts` | the first load | The ids of a browser tab's SSH Sessions in `sessionStorage` (`koko.terminal.sessions`) |
| `RailSection.tsx`, `TabMark.tsx`, `keys.ts`, `hosts.ts`, `routes.tsx` | the first load | The rail's Terminal section, the phone bar's button and the close question; the state marks; the chords and the switcher's opener; the Managed Hosts read; the two routes and the workspace's lazy part |
| `TerminalPage.tsx`, `Pane.tsx`, `Switcher.tsx` | the terminal's chunk | The workspace: the island, its head, More, the Files column, the empty workspace; a tab's pane and its states; the switcher |
| `connection.ts`, `stream.ts`, `records.ts` | the terminal's chunk | The page's end of an SSH Session: the first messages, the stream, the records, liveness and the Resume |
| `emulator/` | the terminal's chunk | The emulator, wterm on its ghostty core: the only directory that imports `@wterm/*` |

## The workspace

- **It lives with the frame.** The frame mounts the workspace
  (`WorkspaceLayer`, a `lazyPart`) once it is wanted: a tab opened,
  `/terminal` visited, the switcher opened, or SSH Sessions kept from the
  document before a reload. From then on it stays mounted for the frame's
  life, in the main region: shown on `/terminal`, hidden and `inert`
  elsewhere, its terminals' painting paused (wterm's `renderingPaused`)
  while their streams carry on. Signing out takes the frame away, which
  ends every tab's SSH Session (`resetWorkspace`).
- **One tree.** Each tab's pane keeps its place in the workspace's tree
  whatever the window's width, so crossing 768 pixels remounts no
  emulator and opens no WebSocket.
- **The routes.** `/terminal` has the workspace loaded; the frame shows it
  there. `/hosts/<name>/terminal` adds a tab for that Host, once per
  visit, and replaces itself with the workspace's URL; at 16 tabs it adds
  none. A Managed Host's Reaching it links there as Open terminal.
- **The URL** lists the tabs' Hosts in order and the active one, counted
  from 1: `/terminal?host=web1&host=db2&host=web1&tab=2`. The workspace
  writes it, replacing the history entry, as tabs open, close and switch,
  while it is shown. It is read once per document, as the frame first
  shows (`startWorkspace`): each listed Host opens in a tab, at most 16,
  the active one kept.
- **Labels** are the Host's name, then "web1 (2)" with the lowest number
  no open tab has. A page holds at most 16 tabs (`maxTabs`), a Sign-in's
  limit.

## The tabs in the rail

The rail's Terminal section reads the workspace's state on every page.
Its tab list is React Aria's `Tabs`, vertical. Each tab's panel is a plain
`role="tabpanel"` in the workspace, since `Tabs` keeps a list and its
panels in one subtree, while this list is in the frame's banner, the
panels load with the workspace, and a phone has no list. React Aria names
a selected tab's panel `<list id>-tabpanel-<key>` (`aria-controls`), so
the rail publishes the list's id (`setTabListId`) and each panel takes
that id (`panelId`); with no list shown a panel has an id of its own.
`Workspace.test.tsx` pins it.

- On the terminal the arrow keys show each tab in turn (automatic
  activation) and leave the focus in the list; elsewhere they only move
  along it (manual activation), and choosing a tab goes to the terminal.
- A tab chosen by a pointer, the switcher or a chord gives its terminal
  the focus (`focusRequest`), once its emulator has loaded and no overlay
  is open; one chosen with the keys does not.
- Delete or Backspace on a focused tab, and a click on its close mark,
  close it, asking first while its shell runs (connected or reconnecting;
  `askClose`, answered by `CloseQuestion` in the frame). The mark is
  `aria-hidden`, since a tab holds no button.

## The chords and the switcher

`useTerminalKeys` listens on the window, in the capture phase, on every
page of the frame, so the terminal and its shell never see the chords:
Ctrl+Shift+K opens the switcher (from another page it goes to the terminal
first, and opens the switcher once the visit has settled, so the frame's
moving the focus to the main region goes first); on the terminal,
Ctrl+Shift+← and → step through the tabs, wrapping, unless the switcher
is open, whose field takes them to select words.

The switcher is React Aria's `Autocomplete`: a field over one `ListBox`
of two sections, the open tabs and the Managed Hosts (`host.list` with
`state=managed`), narrowed by `useFilter`'s `contains` on each item's text
(a tab's name and directory, a Host's name and Space). Typing gives the
first match the virtual focus, and Enter acts on it.

## A tab's SSH Session

Each pane opens its tab's SSH Session once its emulator has laid out its
grid, at that size, through the workspace's one `Terminals`, and again on
Try again or Open a new one (`opening`). The pane's state is the
connection's (`connection.ts`):

| Connection | The pane shows |
| --- | --- |
| `connecting` | A cover: "Connecting to web1…", the spinner past 300 ms |
| `connected` | The terminal |
| `reconnecting` | The terminal dimmed, and a card: Reconnecting, Try now |
| `refused` | A cover: the page's words for the code, the koko server's own under them as written, Try again and Close tab. `reload_page`: "This page is out of date. Reload it." and Reload. `signin_required`: the page goes to Sign in with `next`, at an open or a resume. `cookie_required` and `revoked`: the shell's words for them |
| `unreachable` | A cover: the koko server could not be reached, Try again and Close tab |
| `ended` | A card saying why (an exit status, a signal, the restart, another notice with the koko server's words, a lost connection with the Gateway's reason, this page's own End SSH Session, a protocol mismatch), Open a new one and Close tab |

Keys typed go to the connection (`send`); a resize of the grid goes as a
resize record; the emulator's OSC 7 directory, when its host part is
empty or the tab's Host, is the tab's directory; an OSC 52 request is a
card asking before the clipboard is written.

## A reload, a duplicated browser tab, and closing

- **Kept ids.** Each SSH Session's id is kept in `sessionStorage` from the
  open's answer until it ends. A reload, or a browser tab the system
  discarded and restored, keeps them; the new document ends them before
  its first open (`Terminals.endEarlier`), then opens each listed Host
  again. A document with kept ids loads the workspace on any page, to end
  them.
- **A duplicated browser tab** starts with a copy of the original's
  `sessionStorage`. Each document holds a Web Lock,
  `koko.terminal.<id>`, for each SSH Session it holds, which the browser
  lets go with the document. The new document ends the kept ids no live
  document holds, and waits for each other one to be let go, at most
  `timing.heldWait` (1 s), ending it too, before it opens anything: a
  reload's old document lets its locks go within that. One still held
  after it belongs to a live document, the original of a duplicated tab,
  which the copy leaves alone and forgets; it ends it only once that
  document lets it go, as when the original was closed and its close
  never arrived (one the original ended is answered as ended). Without Web
  Locks it cannot tell, ends none and forgets them; the Gateway ends them
  at the grace.
- **Closing the browser tab** sends each SSH Session's close on `pagehide`
  not kept for the back-forward cache, at best effort.

## The emulator

[`Emulator`](../../web/src/terminal/emulator/Emulator.tsx) is wterm's
`<Terminal>` on a ghostty core of its own ([ADR
0054](../adr/0054-the-web-terminal-is-wterm-on-its-ghostty-core-behind-one-component-of-kokos-own.md)).
The page compiles the core's WebAssembly once (`GhosttyCore.load`, by its
hashed URL), and each terminal instantiates it, with 5,000 lines of
scrollback and Kitty graphics off; unmounting a terminal frees its core.

- **What the page sees:** `write`, `focus`, `fit`, `pressKey(name,
  modifiers)` (wterm's key encoder, following the terminal's modes, such
  as application cursor keys), `selection`; `onInput`, `onResize`,
  `onClipboardWrite`, `onWorkingDirectory`, `onTitle`, `onReady`,
  `onError`; `fontSize` (12, 13, 14, 16 or 18), `paused`, `announce`,
  `label`.
- **Colours** are the theme's `--term-*` tokens, light and dark
  ([`emulator.css`](../../web/src/terminal/emulator/emulator.css) hands
  them to wterm's variables), so they follow Light, Dark or System; wterm
  reads them back when a program asks for the terminal's colours (OSC 10
  and 11). No colour is passed to the core.
- **The renderer's patch** keeps wterm within kokod's CSP ([the web UI
  build](web-ui.md#heroui-and-tailwind)).

## Verifying

`cd web && node_modules/.bin/vitest run src/terminal`:

- [`Workspace.test.tsx`](../../web/src/terminal/Workspace.test.tsx), the
  whole web UI with the emulator and the WebSocket faked: the URL's Hosts
  as tabs and the URL written; each state, ending and refusal, with axe;
  keys reaching the shell; signin_required going to Sign in; the switcher
  showing a tab and opening a Host, "web1 (2)"; switching by a pointer,
  the arrow keys and the chords; the chords never reaching the emulator;
  closing asked first by the mark and by Delete; More; Files' column; the
  16th tab; `/hosts/<name>/terminal`, Open terminal and the tabs kept
  across pages with one emulator and one WebSocket; Ctrl+Shift+K from
  another page and the empty workspace; a reload ending the kept ids
  first; a window crossing 768 pixels; Chinese.
- [`connection.test.ts`](../../web/src/terminal/connection.test.ts): the
  connection, and the kept ids across a reload, a duplicated browser tab
  and a browser without Web Locks.
- [`wterm.test.ts`](../../web/src/terminal/emulator/wterm.test.ts): the
  real wterm and core under jsdom: OSC 52 and 7, wide characters, the
  patched renderer giving the HTML parser no `style` attribute, and the
  Emulator's key encoding and callbacks.
- The static checks (`static.test.ts`): only `emulator/` imports
  `@wterm/*`, the four packages pinned to one version, the patch for it
  applied at install. `go test ./internal/server/webui` checks the build:
  one `ghostty-vt-*.wasm`, no `blob:`, and the chunk that loads the core
  adds no `<style>` element and is not in the first load.

No browser check is run by workers; the Owner checks the page on a
desktop and a phone.
