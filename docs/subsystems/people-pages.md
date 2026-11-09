# The Spaces, People and Devices pages

The web UI's pages for people: who works where, who belongs, and which
devices can reach the koko server. They sit in the [web UI
shell](web-shell.md), which routes to them, decides who the web UI acts as and
asks before an irreversible action; each action is an API operation the CLI
also has. The rules they follow are P0 spec: people and Spaces; what the
operations do is in [the access check and Spaces](access.md),
[Users](users.md), [Join Codes](join-codes.md) and
[revoking](device-connection.md#revoking).

## The pages

| Page | Path | Who | Source |
| --- | --- | --- | --- |
| Spaces | `/spaces` | anyone: the Spaces they can act in (`space.list`), a Personal Space marked so | [`SpaceListPage`](../../web/src/pages/space/SpaceListPage.tsx) |
| A Space | `/spaces/<name>` | anyone who can act in it: its Members and its counts of Hosts, Retired Hosts and Secrets (`space.show`) | [`SpaceDetailPage`](../../web/src/pages/space/SpaceDetailPage.tsx) |
| People | `/people` | the Owner alone (`ownerOnly`): the Users (`user.list`) | [`PeoplePage`](../../web/src/pages/user/PeoplePage.tsx) |
| Devices | `/devices` | anyone: their own Device Sessions, everyone's for the Owner (`device.list`), the one the web UI runs through marked so | [`DevicesPage`](../../web/src/pages/device/DevicesPage.tsx) |

Each is built on an Astryx template (spec #108: "Patterns, not a re-skin"),
and each loads as a chunk of its own the first time it is visited
([`lazyPage`](../../web/src/shell/lazyPage.tsx) in its area's `routes.ts`):

- **Spaces, People and Devices** follow `table-page`: a header with the
  page's title, the number of rows and its one action (Add Space, the
  Owner's alone; Invite User; Add device), then Astryx's `Table`, which on a phone scrolls sideways in its own box
  with its name column pinned (`useTableStickyColumns`). A row's
  actions on People and Devices sit in its overflow menu (a `DropdownMenu`
  behind an icon button named "Actions for" the row, a bottom sheet on a
  phone); a row with no action left, such as the Owner's own or a Revoked
  one, has none.
- **A Space** follows `detail-page`: a header with the way back to the
  Spaces, the name, whether it is a Personal Space or a shared one, its
  counts, and the Owner's Rename and Remove Space; then its Members as line
  items (`List`), with Add Member and each Member's Take out.
- **The writes** that take input open one of the shell's
  [dialogs](web-shell.md#dialogs), a bottom sheet on a phone: Add Space,
  Rename and Add Member a `FormDialog` with their field; Invite and Add
  device an `AdaptiveDialog` laid out as `form-wizard-dialog`, of two steps (the person or the device, then the
  Join Code, [below](#a-join-code-on-screen)). A refusal shows in the
  dialog that made the call; a dialog's title takes the focus as it opens,
  and the focus goes back to what opened it as it closes.

`?all=1` in the URL shows the Removed Users on People and the Revoked Device
Sessions on Devices, as `--all` does for `user ls` and `device ls`; the page's
switch sets it. Names show bare for the caller's own devices and as
`<user>/<device>` for another User's, as the CLI names them. The Devices page
has no counterpart of `device ls --user`: the Owner's list names each Device
Session's User.

## What each may do

| Action | Who | Asks first |
| --- | --- | --- |
| Add a Space (`space.add`) | the Owner | no |
| Rename a Space (`space.rename`), and the page moves to the new name, replacing the old one in the history | the Owner, on a shared Space | no |
| Remove a Space (`space.remove`), then back to the Spaces | the Owner, on a shared Space | yes |
| Add a Member, chosen among the Users who are not one (`space.member.add`), and take one out (`space.member.remove`) | the Owner, on a shared Space | no |
| Invite a User (`user.invite`) | the Owner | no |
| Remove a User (`user.remove`), then say from its `outcome` whether they were deleted or Removed | the Owner, on any User but the Owner who is not Removed | yes |
| Add a device (`device.add`), with an optional name | anyone for themselves; the Owner for any User who is not Removed, chosen from `user.list` | no |
| Revoke a live Device Session (`device.revoke`) | anyone for their own; the Owner for anyone's | yes; for the Owner's own last live one, only once its name is typed, and the call carries `last` |

A Member sees none of the Owner's buttons, fields or columns, and a Personal
Space shows the Owner none of the Space actions, since the koko server
refuses them while its User exists. A refusal shows its code's message where
the action was: in its dialog, or above the list for a removal or a revoke.
Two say more than their code's message: removing a User
refused with `space_not_empty` names their Personal Space, which the People
page does not show; and a refused revoke reloads the list, so a list that was
out of date, such as one still showing a second device of the Owner's, is put
right. The revoke dialog states the consequence: the device is cut off; or
this page stops and the device deletes its key, for the device the web UI
runs through; or only the koko server's own machine adds a device for the
Owner, for the Owner's last; or both of the last two.

## A Join Code on screen

Invite and Add device are wizards ([`IssueWizard`](../../web/src/pages/device/IssueWizard.tsx)):
the first step takes the new User's name, or whose device and its optional
name; issuing never asks. The second shows the Join Code once
([`JoinCodeLine`](../../web/src/pages/device/JoinCodeLine.tsx)), in the
shell's [line shown once](web-shell.md#shown-once): the `koko join
koko_...` line, which one click selects whole, a button that copies it, and
the time left of its 15 minutes, counted each second on the browser's clock
from when the line showed. It does not count to the code's `expires_at`,
since the browser's clock may differ from the koko server's; a code has at
most its 15 minutes left when it arrives, so the line never goes before the
code does. Once the time ran out the wizard shows the code no more and says to
issue a new one. A browser that cannot copy says to select the line instead.
The focus moves to the step's Done, since the button that issued goes with
the first step.

The code lives only in the state of the page that issued it
([shown once](web-shell.md#shown-once)): never in the URL or the browser's
storage, so closing the wizard or leaving the page within the web UI loses
it, and the browser leaving the page (`pagehide`) forgets it, so a page the
browser keeps in its back-forward cache holds it no more when Back brings
it back.

## Verifying

Each test file below runs axe ([`expectAccessible`](../../web/src/test/axe.ts))
on every page, dialog and wizard step it renders.

- [`space.test.tsx`](../../web/src/pages/space/space.test.tsx): a Member sees
  the Spaces, a Space's Members and counts, and no Owner-only action; the
  Owner adds, renames and removes a Space, removing asking first and a
  refusal saying why, in its dialog or on the page; Members added, chosen
  among the Users who are not one, and taken out without asking; a Personal
  Space offering the Owner nothing; a Space the caller cannot act in saying
  so.
- [`people.test.tsx`](../../web/src/pages/user/people.test.tsx): a Member gets
  no People page or link; the Removed Users only when asked for, and from a
  bookmark; Invite never asking, showing its line on its last step with the
  focus on Done, gone once the wizard closes, the page is left or the
  browser leaves it, a refusal on its first step, and on a phone; removing a
  User asking first, then saying deleted or Removed; a refusal naming the
  Personal Space that still holds records.
- [`devices.test.tsx`](../../web/src/pages/device/devices.test.tsx): a
  Member's own Device Sessions and no Owner-only control; the Owner's view of
  everyone's and adding for another User; adding never asking, the line's
  copy and countdown, and the line gone once the wizard closes, the page is
  left, the browser leaves it, or its time ran out; the countdown whatever
  the browser's clock; a browser that cannot copy; a refused Add device;
  revoking asking first, stating the consequence; the Owner's own last
  Device Session needing its name typed and `last`, on its device and over
  the Unix socket, and no typed name otherwise; a refused revoke putting the
  list right; the Revoked ones only when asked for, with nothing to revoke.
- [`test/controls.ts`](../../web/src/test/controls.ts) holds what these
  tests share to drive Astryx's own controls: `chooseRowAction` opens a
  row's overflow menu and chooses an item, and `chooseOption` checks and
  chooses a `Selector`'s options.
