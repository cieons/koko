# The web terminal

The web UI's page that shows its User's Workspace and opens SSH Sessions
to Managed Hosts in it
([ADR 0053](../adr/0053-a-browser-opens-ssh-sessions-through-a-websocket-to-the-gateway.md),
[ADR 0057](../adr/0057-a-users-web-terminal-tabs-are-one-workspace-held-by-the-koko-server.md)):
the Workspace's tabs, the emulator behind one component, and the page's
attachment to each SSH Session it shows. What it looks like is "The
terminal" in [the web UI's design](../web-design.md#the-terminal-terminal).
The two WebSockets it speaks, `/terminal/workspace` and
`/terminal/socket`, are [the web
front's](web-front.md#the-web-terminals-websocket); the stream of an
attachment is [the resumable stream](resumable-stream.md#the-typescript-end);
the Workspace and its SSH Sessions are [the Gateway's](gateway.md#the-web-terminal);
the Files panel's calls are [the web front's](web-front.md#the-web-terminals-files).

## Modules

Everything is in [`web/src/terminal/`](../../web/src/terminal):

| Module | In | Holds |
| --- | --- | --- |
| `workspace.ts` | the first load | The page's view of the Workspace: its tabs as the koko server gives them, with the page's own state of each (its attachment, whether this page is the active one, directory, clipboard request); the tab shown, the switcher, the text size; the URL's query; the requests; starting and resetting it. It imports the terminal's code as types alone |
| `workspaceSocket.ts`, `timing.ts`, `said.ts` | the first load | The Workspace socket: its messages, requests, liveness and reconnecting; the timings; the page's words for refusals and endings |
| `RailSection.tsx`, `TabMark.tsx`, `keys.ts`, `hosts.ts`, `routes.tsx` | the first load | The rail's Terminal section, the phone bar's button, the close question and the Workspace's notices; the state marks; the chords and the switcher's opener; the Managed Hosts read; the two routes and the workspace's lazy part |
| `TerminalPage.tsx`, `Pane.tsx`, `Switcher.tsx` | the terminal's chunk | The workspace: the island, its head, More, the Files column, the empty workspace, the phone's row; a tab's pane and its states; the switcher |
| `KeyBar.tsx`, `TabSheet.tsx` | the terminal's chunk | The key bar; the phone's tab button and its sheet of tabs |
| `FilesPanel.tsx`, `files.ts` | the terminal's chunk | The Files panel: the column, the phone's side panel, the listing, the replace question, dropping files on the terminal; the files calls and the uploads of every tab |
| `connection.ts`, `stream.ts`, `records.ts`, `themeColors.ts` | the terminal's chunk | A page's attachment to an SSH Session: the first messages, the stream, the records, liveness and resuming; the theme's colours as RGB |
| `emulator/` | the terminal's chunk | The emulator, wterm on its ghostty core: the only directory that imports `@wterm/*` |

## The Workspace

- **It is the koko server's.** The frame opens one Workspace socket,
  `/terminal/workspace`, as it first shows, on every page, and keeps it
  until signing out (`startWorkspace`, `resetWorkspace`). The page sends
  `{"protocol": 2}`, and the koko server answers with the Workspace whole,
  `{"tabs": [...]}`, then again after every change. Each Workspace
  replaces the tabs the page holds, keeping the page's own state of each
  tab by its id; a reconnected socket's first one replaces them whole.
  Until the first comes the workspace shows Connecting, not the empty
  workspace.
- **Requests.** `open`, `reopen` and `close` carry a `request` number,
  wait for the socket's Workspace, and are answered by it. A tab appears
  once the Workspace names it: an open's answer then shows it, giving it
  the focus. A refused open adds no tab, and is said in place
  (`openRefused`): in the switcher that sent it, which stays open, else
  in a card at the terminal's top end with Dismiss. At 16 tabs the page
  asks nothing, and the frame says why in a toast (`WorkspaceNotices`).
- **Ending a shell is closing its tab.** The Workspace has no end short of
  `close`, so More offers no End SSH Session: Close tab, asking first
  while the shell runs, ends it in every page. A close
  or a reopen sent on a socket that dropped is sent again on the next,
  since the koko server answers both the same twice; an open is given up,
  since it may have been taken.
- **Liveness and reconnecting.** The page sends `{}` after
  `timing.probe` (15 s) without sending, and takes the socket for dead
  after `timing.dead` (45 s) without a message; it reconnects at once,
  then after `reconnectFirst` doubling to `reconnectMax`, for as long as it
  takes, and not while the page is hidden. `{"stopping": ...}` is the
  koko server restarting; the socket's close follows, and the page
  reconnects. A refusal of the first message is final:
  `signin_required` goes to Sign in, `reload_page` asks for a reload.
- **It lives with the frame.** The frame mounts the workspace's chunk
  (`WorkspaceLayer`, a `lazyPart`) once it is wanted: a tab opened,
  `/terminal` visited, or the switcher opened. From then on it stays
  mounted for the frame's life, in the main region: shown on `/terminal`,
  hidden and `inert` elsewhere, its terminals' painting paused (wterm's
  `renderingPaused`) while their streams carry on. Signing out takes the
  frame away: the page closes its sockets, which detaches it from every
  SSH Session and ends none, and forgets its uploads.
- **One tree.** Each tab's pane keeps its place in the workspace's tree
  whatever the window's width, so crossing 768 pixels remounts no
  emulator and opens no WebSocket.
- **The routes.** `/terminal` has the workspace loaded; the frame shows it
  there. `/hosts/<name>/terminal` goes to `/terminal` and asks the
  Workspace for a tab for that Host, once per visit, which the page shows
  once it is named. A Managed Host's Reaching it links there as Open
  terminal.
- **The URL** names the tab shown, counted from 1: `/terminal?tab=2`. The
  workspace writes it, replacing the history entry, once the Workspace has
  come, while it is shown; it is read once per frame, and applied to the
  first Workspace.
- **Labels and order** are the koko server's: the Host's name, then "web1
  (2)" with the lowest number no tab has. A Workspace holds at most 16
  tabs (`maxTabs`).
- **A tab closed.** A tab this page closed goes once the Workspace no
  longer names it. One shown here that another page closed is announced,
  "web1 was closed in another page.", and the tab beside it is shown.
- **After a restart** the koko server keeps the tabs as `restarted`. A
  page with the terminal loaded sends their `reopen`s in tab order, at
  its box's size, each once the tab before has left `starting` (it is
  `running`, `refused` or `ended`), since the koko server answers a
  `reopen` before it dials; another page doing it at once opens nothing
  more.
- **The key the Workspace replaced.** The page before it kept its SSH
  Sessions' ids in `sessionStorage` (`koko.terminal.sessions`); the frame
  removes the key once, as it starts, inside a `try`.

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
  is open; one chosen with the keys does not. Either is a person's act,
  which makes this page the tab's active one (below).
- Delete or Backspace on a focused tab, and a click on its close mark,
  close it, asking first while its shell runs or starts (`askClose`,
  answered by `CloseQuestion` in the frame: "Close web1?", "This ends its
  SSH Session in every place it is open."). The mark is
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

## On a phone

The workspace takes its phone form under 768 pixels wide, and on a touch
screen held sideways (`sidewaysQuery` in `ui/phone.ts`: `pointer:
coarse`, landscape, at most 540 pixels high, however wide). The frame
reads the same queries: on `/terminal` in that form it renders neither
the phone's top row and foot bar nor the rail (`data-rail="none"`), and
the workspace, fixed over the whole viewport, follows `visualViewport`'s
offset and height, so its foot sits on the soft keyboard. The form
changes the parts around the panes alone, which keep their place in the
tree, so turning a phone remounts no emulator.

- **The key bar** (`KeyBar`) shows under `pointer: coarse` alone: in the
  phone's row at the foot, after the tab button, and on a desktop at the
  island's foot. Each key calls `pressKey(tab, name)` in `workspace.ts`,
  which presses it through the tab's `Emulator.pressKey` with the sticky
  Control and Alt (`ctrl`, `alt`: `off`, `once`, `held`;
  `tapModifier`), and lets go of one applied once. Every key of the bar,
  Control and Alt included, is a person's act, so a full-screen program
  fits the phone while it is used. The keyboard key
  focuses the emulator, or blurs it while it has the focus. A key is a
  button outside the Tab order whose pointer and mouse presses are
  cancelled, so it never moves the focus; the row is one Tab stop.
- **A character typed with Control or Alt on.** The pane hands the bytes
  the emulator gives to `takeTyped` first: while a modifier is on, one
  printable character (the soft keyboard's) is pressed again as that key
  through `pressKey`, and anything else, or the bytes `pressKey` itself is
  encoding (`Runtime.pressing`), goes to the connection as it is.
- **The focus.** On a touch screen the workspace never gives the
  terminal the focus by itself (`focusRequest` is not answered), since
  focusing it opens the soft keyboard.
- **The tab button** (`TabButton`) opens the sheet of the Workspace's tabs on a tap and,
  on a swipe of more than 36 pixels between its pointer's down and up,
  steps through the tabs (`stepTab`) without opening it. The sheet
  (`TabSheet`, a `SidePanel`) takes the frame's places, which the
  workspace is given, then the tabs (`showTab`, `askClose`), then New tab
  (`setSwitcher`).

## A tab's attachment

Each pane attaches to its tab's SSH Session once the tab is first shown
and its emulator has loaded, and stays attached while other tabs and
pages are shown, so switching back draws at once (`connection.ts`,
`Attachment`). Attaching changes nothing in the SSH Session: it goes on
with no page attached.

- **Attaching.** `{"protocol": 2, "attach": <SSH Session>, "cols", "rows"}`,
  the box's cells, answered `{"attachment": <id>}`; then the stream. The
  page sends the theme's colours (`colors`, from `--term-text` and
  `--term-surface`, `themeColors.ts`) on attach and when the theme
  changes. An attach that gets no answer is tried again with the backoff;
  `unknown_tunnel` and `ssh_session_ended` leave the tab to the
  Workspace, which says how it ended.
- **Resuming.** A dropped WebSocket is resumed, `{"resume": <attachment>,
  "offset"}`, byte-exact, at once and then with the backoff, and not while
  the page is hidden; keys typed meanwhile go once. `resume_refused`, past
  the Gateway's grace, attaches again, and the new snapshot draws the
  screen.
- **The records.** `reset` clears the emulator, its history and modes
  too, and sets its grid to the pty's size (`Emulator.reset`: wterm's
  `resize`, then RIS and ED 3); the snapshot follows as output. `active`
  from the Gateway says whether this page is the SSH Session's active
  page. `displaced` says a ninth page took this one's place: the pane
  shows "This terminal is open in 8 other pages." and attaches again only
  on **Show here**. A close from the Gateway ends the attachment.
- **The active page.** A person's act in a visible document makes this
  page the SSH Session's active page, whose box the pty takes:
  `personActed` sends `active` for a key of the key bar, a pointer
  pressed on the terminal, a tab chosen by a pointer, the keys, the
  switcher or a chord, and the window resized while it shows the tab;
  keys typed make it active as data. Attaching, reconnecting and
  resuming never do. One made before the attachment is told once it is
  attached.
- **The size.** The pane measures its box and sends it, in cells, within
  500 by 200 (`maxGrid`), once it has been still for `timing.still`
  (100 ms). The emulator's grid is set by `reset` alone: the pane draws it
  at the pty's size, scaled down to fit a box too small, from its top
  start, and at its top start with the margin in `--term-surface` in one
  too large.
- **What only the active page does.** The OSC 52 card, and the focus
  reports a program asks for (`CSI I`, `CSI O`), are this page's only
  while the `active` record says it is the active page; the card goes when
  another page becomes it.

The pane's state comes from the Workspace first, then its attachment:

| Tab | The pane shows |
| --- | --- |
| `starting`, or `running` while attaching | A cover: "Connecting to web1…", the spinner past 300 ms |
| `restarted` | A cover: "The koko server restarted.", until its new SSH Session starts |
| `running`, attached | The terminal |
| `running`, reconnecting | The terminal dimmed, and a card: Reconnecting, Try now |
| `running`, displaced | A cover: "This terminal is open in 8 other pages." and Show here |
| `running`, refused | A cover: the page's words for the code, Try again and Close tab; `reload_page`: "This page is out of date. Reload it." and Reload; `signin_required` goes to Sign in with `next` |
| `refused` | A cover: the page's words for the code, the koko server's own under them, Try again (`reopen`) and Close tab |
| `ended` | A card saying why, the same in every page (an exit status, a signal, the restart, another notice with the koko server's words), Open a new one (`reopen`) and Close tab |

Keys typed go to the attachment (`send`); the emulator's OSC 7 directory,
when its host part is empty or the tab's Host, is the tab's directory.
The tab's SSH Session id, from the Workspace, is the one the Files
panel's calls name.

## The Files panel

Files in the island's head opens the panel on the active tab: a column
docked at the island's end edge on a desktop (`FilesColumn`), which the
terminal narrows for, and a side panel from the bottom on a phone
(`FilesSheet`). It lists one directory of the tab's Host at a time over
its SSH Session, through [the web front's files
endpoints](web-front.md#the-web-terminals-files), Detached or not.

- **Where it opens.** On the tab's directory (OSC 7), or else, or when
  that cannot be listed, on the Host Account's home (an empty `path`).
  `sftp_unavailable`, `ssh_session_ended` and `unknown_tunnel` hold for
  every directory, so home is not tried after them.
- **What it shows.** The path in the code face, Up, Refresh and Upload;
  directories first, then everything else, by name in the browser's
  collation, each with its size (a file) or kind, and its date; the count
  past 5,000 (`more`). A tab still connecting shows the loading rows, an
  ended SSH Session "The SSH Session has ended.", one refused or never
  answered that it has none. `sftp_unavailable` takes the whole panel.
  Any other refusal shows in place, the Host's own words beneath for
  `host_refused`, with Try again while nothing is listed.
- **A row.** A directory is listed; a file downloads (`files.download`):
  `download` gives a token, and the page follows a hidden link to
  `FilesDownload.path + "/<token>"` with `download` set, so the browser
  saves it with its own progress and the page stays. A link is
  downloaded, and entered when that is `not_a_file`. Anything else is
  shown, not offered.
- **Uploads** (`files.ts`) are a store beside the workspace, so the
  panel's closing and the tab shown stop none, and every tab's are
  listed, each with its Host, directory, progress and Cancel; done,
  cancelled and failed rows stay until dismissed.
  - Upload takes several files into the directory listed. A name the
    listing holds is asked about first, one file at a time
    (`ConfirmDialog`), and sent with `replace: true`; a name past the
    5,000 listed fails its row with `file_exists`.
  - An upload starts (`upload`), then sends 512 KiB chunks
    (`client.postBytes`, `application/octet-stream`) at the offset the
    koko server last answered, then finishes (`finish`). At most 4 run
    per SSH Session (`maxRunning`); the rest wait in order.
  - A chunk or a finish whose connection failed is sent again with a
    Resume's backoff, `timing.reconnectFirst` doubling to
    `reconnectMax`, for up to `timing.retryFor` (15 minutes): the koko server answers both
    the same when they come twice. A start is sent once. A refusal fails
    the upload.
  - Cancel ends a waiting upload at once; a running one sends `cancel`,
    which removes the partial file, and keeps its place until that is
    answered, so the next start never meets `too_many_uploads`.
  - A finish into the directory shown lists it again.
  - The uploads go with the workspace (`resetUploads`), as on signing
    out. They are the page's own: another page of the User does not see
    them.
- **Dropping files on the terminal** of a tab whose shell runs uploads
  them into its directory: while they are dragged over it, a dashed
  accent edge says where (`DropCover`); dropping opens the panel on that
  directory, or home, and uploads into it as Upload does.

## The emulator

[`Emulator`](../../web/src/terminal/emulator/Emulator.tsx) is wterm's
`<Terminal>` on a ghostty core of its own ([ADR
0054](../adr/0054-the-web-terminal-is-wterm-on-its-ghostty-core-behind-one-component-of-kokos-own.md)).
The page compiles the core's WebAssembly once (`GhosttyCore.load`, by its
hashed URL), and each terminal instantiates it, with 5,000 lines of
scrollback and Kitty graphics off; unmounting a terminal frees its core.

- **What the page sees:** `write`, `reset(cols, rows)`, `metrics` (a
  cell's size and the grid's padding), `focus`, `blur`, `hasFocus`,
  `pressKey(name, modifiers)` (wterm's key encoder, following the
  terminal's modes, such as application cursor keys), `selection`;
  `onInput`, `onClipboardWrite`, `onWorkingDirectory`, `onTitle`,
  `onReady`, `onError`; `fontSize` (12, 13, 14, 16 or 18), `paused`,
  `announce`, `label`.
- **The grid** is wterm's with `autoResize` off: `reset` alone sets it,
  never the element's size.
- **Replies.** The koko server answers the shell's queries once, however
  many pages are attached, so the emulator drops what wterm hands to
  `onData` while `write` runs: in wterm 0.5.10 its replies all come
  within `write`, and it gives none to `CSI 18 t`.
- **Colours** are the theme's `--term-*` tokens, light and dark
  ([`emulator.css`](../../web/src/terminal/emulator/emulator.css) hands
  them to wterm's variables), so they follow Light, Dark or System. No
  colour is passed to the core; the koko server answers OSC 10 and 11
  with the colours the active page sent.
- **The renderer's patch** keeps wterm within kokod's CSP ([the web UI
  build](web-ui.md#heroui-and-tailwind)).

## Verifying

`cd web && node_modules/.bin/vitest run src/terminal`:

- [`Workspace.test.tsx`](../../web/src/terminal/Workspace.test.tsx), the
  whole web UI with the emulator and both WebSockets faked: the tabs from
  the Workspace, each one replacing them, a reconnected socket's first
  whole; attaching to the tab shown alone; keys reaching the shell;
  reconnecting; the clipboard card and focus reports only while active;
  the person's acts that send `active`, and attaching sending none;
  endings and refusals from the Workspace, with Open a new one and Try
  again sending `reopen`; the restart's `reopen`s in order, each once
  the one before has left `starting`; `stopping`;
  open and close as requests, closing asked first in the new words, an
  open refused; a tab closed elsewhere announced; the ninth page's Show
  here; an out-of-date page; `signin_required` going to Sign in; the
  switcher, the chords, More, Files, the 16th tab; `/hosts/<name>/terminal`
  and the tabs kept across pages; the rail on every page; a reload
  attaching, ending nothing and removing the old key; signing out
  detaching; a window crossing 768 pixels; Chinese; axe on each.
- [`KeyBar.test.tsx`](../../web/src/terminal/KeyBar.test.tsx), the whole
  web UI as on a phone: each key's `pressKey` call and what the shell is
  sent, no key taking the focus, the keyboard key; Control and Alt once,
  held and let go; a key of the bar making the page active; the terminal
  scaled in a box smaller than the pty and placed at its start in one
  larger, and the box sent once still; the key bar under `pointer:
  coarse` alone; the tab button and its sheet; the row over the
  terminal, upright and sideways; axe on each.
- [`FilesPanel.test.tsx`](../../web/src/terminal/FilesPanel.test.tsx),
  the whole web UI against the fake API's files endpoints: as before,
  with the tab's SSH Session from the Workspace.
- [`connection.test.ts`](../../web/src/terminal/connection.test.ts): an
  attachment's first message and its answers and refusals, `reset`,
  `active` and `displaced`, liveness, resuming byte-exact,
  `resume_refused` attaching again, `active` only for a person's act in a
  visible page, and detaching.
- [`workspaceSocket.test.ts`](../../web/src/terminal/workspaceSocket.test.ts):
  the Workspace message read, the probe and the cut, reconnecting with
  the backoff, a close and a reopen sent again after a drop and an open
  given up, `stopping`, a final refusal; and the theme's colours as RGB.
- [`wterm.test.ts`](../../web/src/terminal/emulator/wterm.test.ts): the
  real wterm and core under jsdom: OSC 52 and 7, wide characters, the
  patched renderer giving the HTML parser no `style` attribute, and the
  Emulator's key encoding and callbacks; each of the koko server's
  snapshot golden cases drawing the screen it names ([the emulator in the
  koko server](vt.md#verify)); each query's reply given within `write`;
  the Emulator dropping every reply, so none reaches `onInput`; and
  `reset` clearing the screen, its history and modes, and setting the
  grid.
- The static checks (`static.test.ts`): only `emulator/` imports
  `@wterm/*`, the four packages pinned to one version, the patch for it
  applied at install. `go test ./internal/server/webui` checks the build:
  one `ghostty-vt-*.wasm`, no `blob:`, and the chunk that loads the core
  adds no `<style>` element and is not in the first load.

No browser check is run by workers; the Owner checks the page on a
desktop and a phone.
