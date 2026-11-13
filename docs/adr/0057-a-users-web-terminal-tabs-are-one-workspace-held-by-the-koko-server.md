# A User's web terminal tabs are one Workspace, held by the koko server

Each User has one Workspace: the web terminal's tabs, the same in every page
of the web UI the User has open, in any browser, on any device, signed in
with a Password or opened through `koko web`. This is the Owner's choice of
2026-11-13 (#208, spec #213), a terminal that behaves like one tmux server
per User, with nothing installed on the Hosts.

- **The koko server holds the shells.** A tab's SSH Session goes on with no
  page attached, until a page closes the tab, its shell ends, access to it
  ends, or 7 days pass with no page attached. The tabs' Hosts, names and
  order are kept in the koko server's database; shells, screens and
  scrollback are kept in its memory alone.
- **Any page attaches.** Any live caller of the User's may attach a page to
  any of the Workspace's SSH Sessions: a Sign-in, a Device Session through
  `koko web`, or the Local caller, as the Owner. Several pages may be
  attached to one SSH Session at once, and keys typed in any of them reach
  the shell. An attaching page gets an exact picture of the terminal, then
  the live output, from an emulator the koko server keeps for each SSH
  Session (spec #213, "The emulator in the koko server").
- **One size.** The pty takes the size of the page most recently active in
  it, a person's act in a visible page, as tmux's `window-size latest`.
  Other pages draw the terminal at that size.
- **Leaving only detaches; a credential taken back ends what it opened.**
  Signing out, a Sign-in's expiry, closing or reloading a page and the end
  of a `koko web` run detach that page alone. Revoking a Sign-in or a
  Device Session ends the SSH Sessions it opened and detaches its pages; a
  Password set, changed or removed ends those the Sign-ins it ends opened.
  The User Removed, the Host out of the User's reach and the Web URL
  changing end SSH Sessions as before.
- **A restart.** Every shell ends, since the koko server holds the Host
  connections. The tabs come back, and the first page that holds them opens
  a new shell in each, saying that the earlier one ended in the restart.
- **The limit** is 16 tabs per Workspace, so a User holds at most 16 web
  terminal SSH Sessions, however many pages they open.

This refines ADR 0053: its Resume of a page's SSH Session within 15
minutes, the ids a page kept in `sessionStorage`, the Web Locks rule for a
duplicated browser tab, the close sent as a page leaves, and its limit of
16 per Sign-in give way to the Workspace. A reload or a duplicated tab is
one more attach. ADR 0053's WebSocket, Origin check, liveness, files over
SFTP and grants of access stand. The CLI is unchanged: `ssh` and `koko exec`
open their own SSH Sessions through Tunnels, outside the Workspace.

## Considered Options

- **A Workspace per Sign-in or per browser,** the existing model widened:
  less to build, but a phone would still not show the desktop's tabs.
- **tmux, or a holder process, on each Host:** shells would survive a koko
  server restart, but koko would install and run software on every Host,
  which the Owner ruled out.
- **The smallest page's size:** nothing would need scaling, but a phone
  left attached would pin every terminal to phone size on the desktop.
- **Revoking a Sign-in, or a Password change, ending the whole Workspace:**
  it would also end what an intruder only typed into, but a lost phone's
  revocation would end the desktop's work.
- **Signing out ending what the Sign-in opened,** as before: signing out on
  a shared computer would cost the User's shells everywhere, so signing out
  would no longer be always safe to do.
- **Nothing kept across a restart,** as a tmux server after a reboot: no
  table, but every upgrade would cost the layout as well as the shells.
- **Restored tabs coming back ended, reopened one by one:** no Host dialled
  unasked, but a restart would not look as a reload does.

## Consequences

- A shell no longer ends when the person leaves it. The Gateway holds up to
  16 per User for up to 7 days without a page, each with its Host
  connection and an emulator in memory.
- An SSH Session's access is checked as its User's, not as the credential
  that opened it, so it outlives a Sign-in that signed out or expired. What
  such a Sign-in opened is reachable only from the User's live pages, and a
  later revocation does not reach it.
- A page needs a channel of its own for the tab list, beside each terminal's
  stream: the Workspace socket, which carries the whole tab list after
  every change, applied in the order the koko server receives requests.
- The terminal protocol rises to 2. The page and the koko server are one
  build, so an older page is told to reload; `koko web` forwards the page's
  bytes as they are, so no channel or API revision rises.
- Upgrading ends every shell, as before, but no longer loses the tabs.
