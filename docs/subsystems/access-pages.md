# The Access and Account pages

The web UI's pages for people: who works where, who belongs, and which
devices can reach the koko server. Access holds the Users and the Spaces,
with a panel for a Space and, for the Owner, one for a person; Account holds
one's own devices and the choices this browser keeps. They sit in the [web
UI shell](web-shell.md), which routes to them, decides who the web UI acts
as and asks before an irreversible action; each action is an API operation
the CLI also has. How they look is [the web UI's
design](../web-design.md#access-access); what the operations do is in [the
access check and Spaces](access.md), [Users](users.md), [Join
Codes](join-codes.md) and [revoking](device-connection.md#revoking).
Passwords and Sign-ins join both pages with #116.

## The pages

| Page | Path | Who | Source |
| --- | --- | --- | --- |
| Access | `/access` | the Owner: the Users (`user.list`) against the shared Spaces (`space.list`, then `space.show` for each); a Member: the Spaces they can act in | [`AccessPage`](../../web/src/pages/space/AccessPage.tsx) |
| A Space's panel | `/access/spaces/<name>` | anyone who can act in it: its counts and Members (`space.show`) | [`SpacePanel`](../../web/src/pages/space/SpacePanel.tsx) |
| A person's panel | `/access/people/<name>` | the Owner alone (`ownerOnly`): a User's role, Spaces, Personal Space and devices | [`PersonPanel`](../../web/src/pages/user/PersonPanel.tsx) |
| Account | `/account` | anyone: who the web UI acts as, the choices this browser keeps, their own devices (`device.list` naming them) and their Personal Space | [`AccountPage`](../../web/src/pages/user/AccountPage.tsx) |

Access loads as one chunk under its three paths
([`spaceRoutes`](../../web/src/pages/space/routes.ts)), so the page keeps
its state as a panel opens over it and closes. A panel's link keeps the
page's query, and closing a panel, by its close button, Escape or a click
beside it, goes back to `/access` with that query in place of the panel's
history entry, so the focus returns to the link that opened it. A Member
gets no route for a person's panel, so its path shows "Page not found".

**The Owner's Access** is one table on a desktop: a row per User, the
Owner first and the Removed Users last, a link
to their panel that says how they get in ("2 devices · Password set", or
"Invited, not yet in" for one who never had a device or a Password, from
`device.list` with `all`), and a column per shared Space, whose header links
to its panel with its counts. A cell is a checkbox named "<user> is a Member
of <space>": ticking it calls `space.member.add`, unticking
`space.member.remove`, for that one User, at once, with a notice; the cell
shows what it asked while the call runs, then the Space's Members as the
call answered them, until the Spaces are read again
([`useMembership`](../../web/src/pages/space/membership.ts)). The Owner's
cells show a lock, and a Removed User's none. On a phone the table is two
lists, the People and the Spaces, each row a link to its panel, where the
membership is changed: a person's Spaces as switches, a Space's Members
with Take out and Add a Member. "Show Removed Users" is `all=1`. While the
Spaces are read again after a change, the page keeps the rows it showed, so
a panel's close still finds the link that opened it.

**A Member's Access** lists their Personal Space and the Spaces they belong
to, each with its counts and Members, a link to its panel read-only, and
says their devices are on Account.

**A Space's panel** shows its counts of Hosts, Retired Hosts and Secrets,
each a link to its list narrowed to the Space (`/hosts?filter=space%3D<name>`,
with `filter=state%3Dretired&all=1` for the Retired Hosts, and
`/secrets?space=<name>`), and its Members. A Personal Space says it takes
no Members.

**A person's panel** shows the User's role and the date they were added
(or Removed); the shared Spaces as switches of their membership, or for the
Owner that the Owner sees every Space; their Personal Space's counts with a
link to its Hosts; their devices; and Remove User. It finds the User in
`user.list` with `all`, so a Removed User's panel opens too, and says so.

**Account** shows the User's name and role and how this browser got in
(through `koko web` on a device, a Sign-in, or the Local caller); This
browser, Language and Appearance as segments that set what the top bar's
menus set; the caller's devices; and their Personal Space. The account
panel links to it.

**Devices**, on Account and in a person's panel
([`Devices`](../../web/src/pages/device/Devices.tsx)), list one User's
Device Sessions (`device.list` naming the User): the device's name, when it
joined and was last used, the one the web UI runs through marked "This
device" (none for the Local caller or a Sign-in), and Revoke on each live
one. "Show Revoked devices" adds the Revoked ones, struck through; on
Account it is `all=1` in the URL, in a person's panel the panel's own,
since `all=1` there is Access's. Names show bare in a list, and as
`<user>/<device>` for another User's in what revoking says, as the CLI
names them.

`/people` and `/spaces` go to `/access`, `/spaces/<name>` to
`/access/spaces/<name>` and `/devices` to `/account`, each keeping its query
and replacing the history entry ([`movedTo`](../../web/src/shell/router.tsx)).

## What each may do

| Action | Who | Asks first |
| --- | --- | --- |
| Add a Space (`space.add`), a short form | the Owner | no |
| Rename a Space (`space.rename`), and the panel moves to the new name, replacing its history entry | the Owner, on a shared Space | no |
| Remove a Space (`space.remove`), then the panel closes | the Owner, on a shared Space | yes |
| Make a User a Member (`space.member.add`): a cell, a switch, or Add a Member, chosen in a searchable choice among the Users who are neither the Owner, a Removed User nor a Member already (`user.list`) | the Owner, on a shared Space | no |
| Take a Member out (`space.member.remove`): a cell, a switch, or Take out | the Owner, on a shared Space | no |
| Invite a User (`user.invite`, then `space.member.add` once per Space chosen) | the Owner | no |
| Remove a User (`user.remove`), then say from its `outcome` whether they were deleted or Removed, and close the panel | the Owner, on any User but the Owner who is not Removed | yes |
| Add a device (`device.add`), with an optional name: on Account for oneself, with no `user`; in a person's panel for that User, named | anyone for themselves; the Owner for any User who is not Removed | no |
| Revoke a live Device Session (`device.revoke`, naming its User) | anyone for their own; the Owner for anyone's | yes; for the Owner's own last live one, only once its name is typed, and the call carries `last` |

A Member sees none of the Owner's buttons, cells, switches or panels, and a
Personal Space shows the Owner none of the Space actions, since the koko
server refuses them while its User exists. A refusal shows its code's
message where the action was: in its dialog or confirmation, in the panel
for a change of membership made there, or above the table for a cell. Two
say more than their code's message: removing a User refused with
`space_not_empty` names their Personal Space, which the table does not show;
and a refused revoke reloads the list, so a list that was out of date, such
as one still showing a second device of the Owner's, is put right and the
confirmation then asks for the name. The revoke confirmation states the
consequence: the device is cut off and the Join Codes it issued stop
working; or this page stops and the device deletes its key, for the device
the web UI runs through; or only the koko server's own machine adds a
device for the Owner, for the Owner's last; or both of the last two.

When Revoke or Take out takes away the button that had the focus, as its
row goes once the list is read again, the focus moves to the list's heading,
whether it fell to the page's body or the panel caught it
([`useFocusFallback`](../../web/src/ui/focus.ts)).

## A Join Code on screen

Invite and Add device issue in two steps
([`IssueDialog`](../../web/src/pages/device/IssueDialog.tsx)): the first
takes the new User's name and the shared Spaces to make them a Member of,
or the device's optional name; issuing never asks. The second shows the
Join Code once ([`JoinCodeLine`](../../web/src/pages/device/IssueDialog.tsx)),
in the shell's [line shown once](web-shell.md#shown-once): the `koko join
koko_...` line, Copy, and the time left of its 15 minutes, counted on the
browser's clock from when the line showed. Valid for, and Invite's choice
of a Password Link, come with #116. The focus moves to Done, since the
button that issued goes with the first step.

Invite calls `user.invite`, then `space.member.add` once per Space chosen.
A membership call that fails leaves the Invite made, so the second step
still shows the Join Code, which cannot be shown again, and names each Space
the User was not added to, with why, and that the memberships can be added
in the User's panel.

The code lives only in the dialog's `useShownOnce`: never in the URL or the
browser's storage. Closing the dialog or leaving the page within the web UI
loses it; the browser leaving the page (`pagehide`) forgets it, and the
second step then says it is no longer shown; once its time ran out, the
second step says it expired. Either way it says to issue a new one with Add
device.

## Verifying

Each test file below runs axe ([`expectAccessible`](../../web/src/test/axe.ts))
on every page, panel, dialog and confirmation it renders, against a fake
koko server whose writes change what it answers
([`access.fixture.ts`](../../web/src/pages/space/access.fixture.ts)).

- [`access.test.tsx`](../../web/src/pages/space/access.test.tsx): the
  Owner's table with its counts, memberships and locked cells; a cell
  ticked and unticked, with its notice and request, and a refused one;
  "Show Removed Users" as `all=1` and from a bookmark; no shared Space, Add
  Space and its refusal; a failed load with Try again; on a phone the two
  lists, membership changed from a person's panel and a Space's; a Space's
  panel with its counts' links; closing a panel in place of its history
  entry, its query kept and the focus back on its link; Add a Member among
  those who are not one, and Take out with the focus on the Members'
  heading; Add a Member with no User chosen; Rename moving the panel in
  place; Remove Space asking first, `space_not_empty` in place, then
  closing; a Personal Space offering nothing; a Member's Access and panel
  read-only; a person's panel as "Page not found" to a Member; a Space a
  Member cannot act in; each redirect of `/spaces`, `/spaces/<name>` and
  `/people`, keeping its query.
- [`people.test.tsx`](../../web/src/pages/user/people.test.tsx): a person's
  panel with its role, switches, Personal Space and devices; a switch
  changing the membership and the table showing it; the Owner's own panel;
  a Removed User's; Remove User asking first, then Removed or deleted, and
  `space_not_empty` naming the Personal Space; Add device for that User;
  Invite with its Spaces, the Join Code shown once with Done focused, its
  copy and countdown, and gone once the dialog closes; Invite when a
  membership call fails; a refused Invite; the code gone once the browser
  leaves the page, the page is left within the web UI, or its time runs
  out.
- [`devices.test.tsx`](../../web/src/pages/device/devices.test.tsx): a
  Member's Account, its devices and Personal Space, and no Owner-only
  control; the account panel's link; a Sign-in's and the Local caller's
  Account, no device marked; This browser mirroring the top bar; "Show
  Revoked devices" as `all=1`; `/devices` to Account; Add device for
  oneself, on a desktop and a phone, and refused; Revoke asking first,
  stating the consequence, the focus then on the list's heading; the
  device the page runs through; the Owner's own last device needing its
  name and `last`, on that device and over the Unix socket, and no name
  otherwise; a refused revoke reloading the list; another User's device
  revoked from their panel as `<user>/<device>`.
