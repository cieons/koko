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

`?all=1` in the URL shows the Removed Users on People and the Revoked Device
Sessions on Devices, as `--all` does for `user ls` and `device ls`; the page's
checkbox sets it. Names show bare for the caller's own devices and as
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
the action was. Two say more than their code's message: removing a User
refused with `space_not_empty` names their Personal Space, which the People
page does not show; and a refused revoke reloads the list, so a list that was
out of date, such as one still showing a second device of the Owner's, is put
right. The revoke dialog states the consequence: the device is cut off; or
this page stops and the device deletes its key, for the device the web UI
runs through; or only the koko server's own machine adds a device for the
Owner, for the Owner's last; or both of the last two.

## A Join Code on screen

An Invite or an added device shows its Join Code once
([`JoinCodeLine`](../../web/src/pages/device/JoinCodeLine.tsx)), in the
shell's [line shown once](web-shell.md#shown-once): the `koko join
koko_...` line, which one click selects whole, a button that copies it, and
the time left of its 15 minutes, counted each second on the browser's clock
from when the line showed. It does not count to the code's `expires_at`,
since the browser's clock may differ from the koko server's; a code has at
most its 15 minutes left when it arrives, so the line never goes before the
code does. Once the time ran out the page shows the code no more and says to
issue a new one. A browser that cannot copy says to select the line instead.

The code lives only in the state of the page that issued it
([shown once](web-shell.md#shown-once)): never in the URL or the browser's
storage, so leaving the page within the web UI loses it, and the browser
leaving the page (`pagehide`) forgets it, so a page the browser keeps in its
back-forward cache holds it no more when Back brings it back.

## Verifying

- [`space.test.tsx`](../../web/src/pages/space/space.test.tsx): a Member sees
  the Spaces, a Space's Members and counts, and no Owner-only button; the
  Owner adds, renames and removes a Space, removing asking first and a
  refusal saying why; Members added and taken out without asking; a Personal
  Space offering the Owner nothing.
- [`people.test.tsx`](../../web/src/pages/user/people.test.tsx): a Member gets
  no People page or link; the Removed Users only when asked for; an Invite
  never asking and showing its line until the page is left; removing a User
  asking first, then saying deleted or Removed; a refusal naming the Personal
  Space that still holds records.
- [`devices.test.tsx`](../../web/src/pages/device/devices.test.tsx): a
  Member's own Device Sessions and no Owner-only control; the Owner's view of
  everyone's and adding for another User; adding never asking, the line's
  copy and countdown, and the line gone once the page is left, once the
  browser leaves it, or once its time ran out; the countdown whatever the
  browser's clock; a browser that cannot copy; revoking asking first,
  stating the consequence; the Owner's own last Device Session needing its
  name typed and `last`, on its device and over the Unix socket, and no
  typed name otherwise; a refused revoke putting the list right; the Revoked
  ones only when asked for.
