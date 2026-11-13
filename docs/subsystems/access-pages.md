# The Access and Account pages

The web UI's pages for people: who works where, who belongs, and which
devices can reach the koko server. Access holds the Users and the Spaces,
with a panel for a Space and, for the Owner and the Administrators, one
for a person; Account holds
one's own devices and the choices this browser keeps. They sit in the [web
UI shell](web-shell.md), which routes to them, decides who the web UI acts
as and asks before an irreversible action; each action is an API operation
the CLI also has. How they look is [the web UI's
design](../web-design.md#access-access); what the operations do is in [the
access check and Spaces](access.md), [Users](users.md), [Join
Codes](join-codes.md) and [revoking](device-connection.md#revoking).
One's own Password and Browser Sign-ins are on Account, and, for the
Owner and the Administrators, anyone's in their panel ([Sign-ins](signin.md#the-web-uis-pages)).

## The pages

| Page | Path | Who | Source |
| --- | --- | --- | --- |
| Access | `/access` | the Owner and the Administrators: the Users (`user.list`) against the shared Spaces, each with its Members and counts (`space.list`, one call); a Member: the Spaces they can act in | [`AccessPage`](../../web/src/pages/space/AccessPage.tsx) |
| A Space's panel | `/access/spaces/<name>` | anyone who can act in it: its counts and Members (`space.show`) | [`SpacePanel`](../../web/src/pages/space/SpacePanel.tsx) |
| A person's panel | `/access/people/<name>` | the Owner and the Administrators alone (`administers`): a User's role, Spaces, Personal Space, devices and Sign-ins | [`PersonPanel`](../../web/src/pages/user/PersonPanel.tsx) |
| Account | `/account` | anyone: who the web UI acts as, the choices this browser keeps, their own devices (`device.list` naming them) and their Personal Space | [`AccountPage`](../../web/src/pages/user/AccountPage.tsx) |

Access loads as one chunk under its three paths
([`spaceRoutes`](../../web/src/pages/space/routes.ts)), so the page keeps
its state as a panel opens over it and closes. A panel's link keeps the
page's query, and closing a panel, by its close button, Escape or a click
beside it, goes back to `/access` with that query in place of the panel's
history entry, so the focus returns to the link that opened it. A Member
gets no route for a person's panel, so its path shows "Page not found".

**The Owner's Access**, which an Administrator gets too, is two lists, one at a time, on a phone as on a
desktop: the People and the shared Spaces, under the tabs **People** and
**Spaces**, each with its count (the Users who are not Removed, and the
shared Spaces), beside one search field that narrows the list shown by
name, whatever its case, and Reset view. A person's row is a link to their
panel: the initial, the name, "Owner" or "Administrator" where so, and a one-line summary of
the shared Spaces they are a Member of and how they get in ("16 Spaces ·
2 devices · Password set", "No Space · Invited, not yet in" for one who
never had a device or a Password, from `device.list` with `all`; "Every
Space · …" for the Owner and an Administrator; "Removed" for a Removed
User). The Owner comes first, then the Administrators, then the Members,
and the Removed Users last. A Space's row is a link to its panel:
its name and "30 Members · 40 Hosts · 0 Secrets". The summary sits beside
the name on a desktop and under it on a phone. Nothing that matches the
search shows "No User matches this search." or "No Space matches this
search." with Clear the search; with no shared Space, the Spaces tab says
so. The Spaces tab is `view=spaces` in the URL,
the search `q=`, replaced as it is typed, and "Show Removed Users", shown
under the People, is `all=1`; the browser remembers them together as
Access's view, as it remembers a list's
([`useRememberedView`](../../web/src/shell/rememberedView.tsx)), so Access
opened with no query, a panel's path among them, shows the view it was
left in, and Reset view goes back to the People with no search and without
the Removed Users. `all=1` stays in the view while the Spaces show, for
the return to the People. Membership is changed in the panels:
a person's Spaces with Take out under Add to a Space, a Space's Members
with Take out under Add Members; a change shows what it asked while the call runs, then the Space's
Members as the call answered them, until the Spaces are read again
([`useMembership`](../../web/src/pages/space/membership.ts)). While the
Spaces are read again after a change, the page keeps the rows it showed,
so a panel's close still finds the link that opened it.

**A Member's Access** lists their Personal Space and the Spaces they belong
to, each with its summary ("Personal Space" and its counts, or "3 Members
· 4 Hosts · 1 Secret"), a link to its panel read-only, which names the
Members.

**A Space's panel** shows its counts of Hosts, Retired Hosts and Secrets,
each a link to its list narrowed to the Space (`/hosts?filter=space%3D<name>`,
with `filter=state%3Dretired&all=1` for the Retired Hosts, and
`/secrets?space=<name>`), and its Members. A Personal Space says it takes
no Members.

**A person's panel** shows the User's role and the date they were added
(or Removed); how many of the shared Spaces they are a Member of, **Add to
a Space**, a searchable choice of the shared Spaces they are not in (or,
in every one, a line saying so), and those they are in, each a link to its
panel with **Take out**; for the Owner and an Administrator, "Every
Space" in place of those; their Personal Space's
counts with a link to its Hosts; their devices; their Password, with Issue
Password Link and Remove Password; their Browser Sign-ins; for the Owner,
**Make an Administrator** for a Member or **Make a Member** for an
Administrator; and Remove User. It finds the User in `user.list` with
`all`, so a Removed User's panel opens too, its subtitle saying when they
were Removed, with nothing under it.

Who may act on whom is the koko server's rule (ADR 0056), which
[`mayActOn`](../../web/src/shell/caller.tsx) mirrors: anyone on
themselves, the Owner on anyone, and an Administrator on a Member. Where
the caller may not act on the person, as an Administrator viewing the
Owner or another Administrator, the panel shows their role, Spaces,
devices and Sign-ins and none of the actions on them: no Add device,
Revoke, Issue Password Link, Remove Password, Remove User, Make an
Administrator or Make a Member. One's own panel offers what the caller may
do on themselves, and neither role action nor Remove User.

**Make an Administrator** asks first (`user.promote`, the irreversible
tier): it names what it gives, "bob will see and act in every Space, read
every Secret and manage every Member", and lists the User's live devices
(`device.list` naming them), each with who added it (`added_by`, as
`<user>/<device>`, or "in a browser or on the koko server's machine"
without one), and their live Sign-ins (`signin.list`), since a promotion
keeps them; its button waits until both are read. **Make a Member**
(`user.demote`) does not ask, as Take out does not. Either is one button
that keeps the focus as the role changes, and the panel then shows the
new role.

**Account** shows the User's name and role and how this browser got in
(through `koko web` on a device, a Sign-in, or the Local caller), with
Sign out by the title for a Sign-in; the Password, changed there for a
Sign-in and otherwise one sentence saying where; This browser, Language
and Appearance as segments that set what the frame's menus set; the
caller's devices; their Browser Sign-ins; and their Personal Space. The
account panel links to it.

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
| Add a Space (`space.add`), a short form | the Owner or an Administrator | no |
| Rename a Space (`space.rename`), and the panel moves to the new name, replacing its history entry | the Owner or an Administrator, on a shared Space | no |
| Remove a Space (`space.remove`), then the panel closes | the Owner or an Administrator, on a shared Space | yes |
| Make a User a Member (`space.member.add`): Add to a Space in a person's panel, one Space chosen in a searchable choice of those they are not in; or Add Members in a Space's, several Users chosen in a searchable choice among those who are neither the Owner, a Removed User nor a Member already (`user.list`), in one call | the Owner or an Administrator, on a shared Space | no |
| Take a Member out (`space.member.remove`): Take out, in a person's panel or a Space's | the Owner or an Administrator, on a shared Space | no |
| Invite a User (`user.invite`, with `web` for In a browser and `valid_for` unless 15 minutes, then `space.member.add` once per Space chosen) | the Owner or an Administrator | no |
| Issue a Password Link (`password.link`, with `valid_for` unless 15 minutes), in a person's panel; disabled, saying why, while the Web URL is unset (`server.show`) | anyone for themselves; the Owner for anyone; an Administrator for a Member; each for a User not Removed | no |
| Remove a Password (`password.remove`), in a person's panel | anyone for their own; the Owner for anyone's; an Administrator for a Member's; each for a User with one | yes |
| Revoke a live Sign-in (`signin.revoke`), on Account and in a person's panel | anyone for their own; the Owner for anyone's; an Administrator for a Member's | yes |
| Make a Member an Administrator (`user.promote`), in a person's panel, the question listing their live devices with who added each and their live Sign-ins | the Owner | yes |
| Make an Administrator a Member (`user.demote`), in a person's panel | the Owner | no |
| Remove a User (`user.remove`), then say from its `outcome` whether they were deleted or Removed, and close the panel | the Owner, on any User but themselves who is not Removed; an Administrator, on a Member who is not Removed | yes |
| Add a device (`device.add`), with an optional name: on Account for oneself, with no `user`; in a person's panel for that User, named | anyone for themselves; the Owner for any User, and an Administrator for a Member, who is not Removed | no |
| Revoke a live Device Session (`device.revoke`, naming its User) | anyone for their own; the Owner for anyone's; an Administrator for a Member's | yes; for the Owner's own last live one, only once its name is typed, and the call carries `last` |

A Member sees none of the buttons, fields or panels of the Owner and the
Administrators, and a Personal Space shows them none of the Space actions, since the koko
server refuses them while its User exists. A refusal shows its code's
message where the action was: in its dialog or confirmation, in the panel,
under its add field, for a change of membership made there. Two
say more than their code's message: removing a User refused with
`space_not_empty` names their Personal Space, which the People list does
not show;
and a refused revoke reloads the list, so a list that was out of date, such
as one still showing a second device of the Owner's, is put right and the
confirmation then asks for the name. The revoke confirmation states the
consequence: the device is cut off and the Join Codes it issued stop
working; or this page stops and the device deletes its key, for the device
the web UI runs through; or, for the Owner's last, that only the koko
server's own machine or a browser signed in with the Owner's Password adds
a device for the Owner; or both of the last two.

When Revoke takes away the button that had the focus, as its row goes once
the list is read again, the focus moves to the list's heading, whether it
fell to the page's body or the panel caught it
([`useFocusFallback`](../../web/src/ui/focus.ts)). Take out keeps it in the
list instead ([`useRowFocus`](../../web/src/ui/focus.ts)): once its row
goes, the next row's Take out takes it, the one before for the last row,
or the add field, Add to a Space or Add Members, once the list is empty.

## A Join Code or Password Link on screen

Invite, Add device and Issue Password Link issue in two steps
([`IssueDialog`](../../web/src/pages/device/IssueDialog.tsx)): the first
takes the new User's name, how they will use koko and the shared Spaces to
make them a Member of, chosen in a searchable choice of several, or the
device's optional name, and each takes
**Valid for** ([`ValidForChoice`](../../web/src/pages/device/IssueDialog.tsx)):
15 minutes, 1 hour, 1 day or 7 days, as segments, starting at the koko
server's own 15 minutes (#108, ADR 0044), which a request leaves out;
another carries `valid_for` in seconds. Issuing never asks. The second shows the
Join Code ([`JoinCodeLine`](../../web/src/pages/device/IssueDialog.tsx)) or
the Password Link ([`PasswordLinkLine`](../../web/src/pages/device/IssueDialog.tsx))
once, in the shell's [line shown once](web-shell.md#shown-once): the
`koko join koko_...` line or the link, Copy, and the time left of the
validity chosen, counted on the browser's clock from when the line
showed, counting the validity the line was issued for. The focus moves to
Done, since the button that issued goes with the first step.

Invite's **How they will use koko** is two radio cards: In a terminal, a
Join Code, and In a browser, a Password Link (`web`), disabled with the
reason while `server.show` answers no Web URL
([`useWebURL`](../../web/src/pages/signin/webURL.ts)).
It calls `user.invite`, then `space.member.add` once per Space chosen. A
membership call that fails leaves the Invite made, so the second step
still shows the code or link, which cannot be shown again, and names each
Space the User was not added to, with why, and that the memberships can be
added in the User's panel.

The code or link lives only in the dialog's `useShownOnce`: never in the
URL or the browser's storage. Closing the dialog or leaving the page
within the web UI loses it; the browser leaving the page (`pagehide`)
forgets it, and the second step then says it is no longer shown; once its
time ran out, the second step says it expired. Either way it says how to
issue a new one: Add device, or Issue Password Link.

## Verifying

Each test file below runs axe ([`expectAccessible`](../../web/src/test/axe.ts))
on every page, panel, dialog and confirmation it renders, against a fake
koko server whose writes change what it answers
([`access.fixture.ts`](../../web/src/pages/space/access.fixture.ts)).

- [`access.test.tsx`](../../web/src/pages/space/access.test.tsx): the
  Spaces read in one `space.list`, with no `space.show` per Space, on load
  and after a change; the People with their summaries and the Spaces with
  theirs, each tab as `view=` in the URL; the search narrowing the list
  shown as `q=`, across both tabs, with no match and Clear the search;
  Access, and a panel's path, opened with no query showing the view it was
  left in, and Reset view; "Show Removed Users" as `all=1` and from a bookmark, the Owner
  first and the Removed Users last; no shared Space, Add Space and its
  refusal; a failed load with Try again; on a phone the same lists,
  membership changed from a person's panel and a Space's; a Space's
  panel with its counts' links; closing a panel in place of its history
  entry, its query kept and the focus back on its link; Add Members with
  three Users picked among those who are not one, sent in one call, and
  its search; a refused Add Members said under its field, the Users picked
  kept; the focus on the line saying every User is a Member once the last
  are added; Take out with the focus on the next Member, the one before,
  then Add Members, and a refused one keeping it on its button; Add
  Members with no User picked; Rename moving the panel in
  place; Remove Space asking first, `space_not_empty` in place, then
  closing; a Personal Space offering nothing; a Member's Access and panel
  read-only; the People ranked the Owner, the Administrators, the Members,
  then the Removed Users, each Administrator labelled with "Every Space" in
  their summary, in both languages; an Administrator's Access as the
  Owner's; a person's panel as "Page not found" to a Member; a Space a
  Member cannot act in; each redirect of `/spaces`, `/spaces/<name>` and
  `/people`, keeping its query.
- [`people.test.tsx`](../../web/src/pages/user/people.test.tsx): a person's
  panel with its role, Spaces, Personal Space and devices; Add to a Space
  offering only the Spaces the person is not in, and the People list
  showing the change; a Space being added listed only once the koko
  server answers; Add to a Space with none chosen; Take out with the
  focus on the next Space, then Add to a Space; a refused change shown in
  the panel alone; the Owner's own panel;
  a Removed User's; Remove User asking first, then Removed or deleted, and
  `space_not_empty` naming the Personal Space; Add device for that User;
  Invite with several Spaces chosen from the search, the Join Code shown
  once with Done focused, its copy and countdown, and gone once the dialog closes; Invite when a
  membership call fails; a refused Invite; the code gone once the browser
  leaves the page, the page is left within the web UI, or its time runs
  out.
- [`administrators.test.tsx`](../../web/src/pages/user/administrators.test.tsx):
  the Owner's Make an Administrator, its question listing the live devices
  with who added each and the live Sign-ins, or saying there are none, its
  button waiting until they are read, Cancel, then the panel showing
  Administrator; Make a Member at once, the focus kept on its button; a
  refused role change shown in the panel and in the question; the Owner's
  own panel; an Administrator viewing the Owner, another Administrator, a
  Member and themselves, each with the actions shown or hidden; the Owner
  acting on an Administrator.
- [`devices.test.tsx`](../../web/src/pages/device/devices.test.tsx): a
  Member's Account, its devices and Personal Space, and no control for
  the Owner and the Administrators; an Administrator's Account naming the
  role, and the three roles in Chinese; the account panel's link; a Sign-in's and the Local caller's
  Account, no device marked; This browser mirroring the frame; "Show
  Revoked devices" as `all=1`; `/devices` to Account; Add device for
  oneself, on a desktop and a phone, and refused; Revoke asking first,
  stating the consequence, the focus then on the list's heading; the
  device the page runs through; the Owner's own last device needing its
  name and `last`, on that device and over the Unix socket, and no name
  otherwise; a refused revoke reloading the list; another User's device
  revoked from their panel as `<user>/<device>`.
