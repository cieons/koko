# The web UI shell

The frame every page of the web UI sits in, and the parts pages share: the
side navigation and the account menu, routing and who the web UI acts as,
the language, the mode and the catalogues, the API client, the confirmation
dialog, edit forms, reloading, what a page shows once, and the notices and
refusals. The pages themselves are their capabilities'; where an area adds its pages and its
catalogue files is in [code layout](code-layout.md#where-an-area-adds-its-parts).
How the app is built, embedded and typed is [the web UI build](web-ui.md); how
a browser reaches it is [`koko web` and the web front](web-front.md).

## Providers

The web UI is built on Astryx ([ADR
0045](../adr/0045-the-web-ui-is-built-on-astryx.md)). The `Shell` renders
everything inside [`Providers`](../../web/src/shell/providers.tsx), so the
app [`main.tsx`](../../web/src/main.tsx) renders and the app a page test
renders sit in the same ones, outermost first:

| Provider | Gives |
| --- | --- |
| `LanguageProvider` | koko's language and its catalogues ([Language](#language)) |
| [`ModeProvider`](../../web/src/shell/mode.tsx) | the mode chosen, and Astryx's `Theme` with `neutralTheme` (from `@astryxdesign/theme-neutral/built`) in that mode |
| `InternationalizationProvider` | Astryx's own strings, such as a dialog's close label, in koko's language: `en`, or `zh-CN` from Astryx's `locales/zh-CN.generated.js` |
| `LinkProvider` | koko's router `Link` behind every link an Astryx component renders |

`main.tsx` imports Astryx's `reset.css`, `astryx.css` and the theme's
`theme.css`, in that order, then koko's
[`shell/app.css`](../../web/src/shell/app.css). The theme's `fonts.css`,
which loads Figtree from Google Fonts, is never imported: `app.css` declares
the copy in `web/src/assets/fonts/` (Figtree's variable WOFF2, latin, under
the SIL Open Font License beside it) and names the system's Chinese fonts
after it in `--font-family-body`. koko's CSS uses Astryx's tokens and never
a literal colour. Its rules for the pages not yet redone on Astryx reach
only inside an element of class `koko-legacy`, which the Shell wraps each
page in, in Astryx's lowest layer, `reset`, so no Astryx component
outside it takes them, and one inside keeps every property it sets.

## The frame

The [`Shell`](../../web/src/shell/Shell.tsx) is Astryx's `AppShell`, laid
out as its `AppShellTopNavWithSideNav` pattern (spec #108: "Patterns, not a
re-skin"):

- **The top bar** (`TopNav`) holds koko's name, linking `/`; whom the web UI
  acts as ([below](#who-the-web-ui-acts-as)); and the
  [account menu](../../web/src/shell/AccountMenu.tsx), a `DropdownMenu` behind
  the User's `Avatar` that chooses the language and the mode, each a group
  of radio items. The account page and signing out join it with Sign-ins
  (#114, #116).
- **The side navigation** (`SideNav`, named "Pages") links each page the
  caller may see, in the order of `routes`, under its name and its Lucide
  icon (`Route.icon`), and marks the one the URL is in (`aria-current`), a
  page beneath it such as a Host included.
- **On a phone**, below Astryx's `md` width (768 pixels), the top bar
  carries a button that opens the side navigation's links in a `MobileNav`
  drawer, which every visit closes; whom the web UI acts as moves into the
  account menu, where the bar has no room for it.
- **The content** is `AppShell`'s `<main>`, with Astryx's skip link before
  the frame. A page not yet redone on Astryx sits inside an element of class
  `koko-legacy` ([Providers](#providers)).
- **The notices** are the `AppShell`'s banner, above the top bar
  ([Notices](#notices)).
- **A path with no page** shows Astryx's `EmptyState`, "Page not found",
  with a link to the first page; so does a path only the Owner may open, to
  a Member.
- **The icon** of the browser's tab is `web/public/favicon.svg`, which
  `index.html` links, so the browser asks for no `/favicon.ico`.
  `index.html`'s viewport tag carries `viewport-fit=cover`, which lets
  `AppShell` keep its top bar in a phone's safe area.

## Light and dark

- **The mode** is System by default: Astryx follows the system's light or
  dark mode (`prefers-color-scheme`). Light or Dark overrides it, and the
  browser keeps the choice in `localStorage` (`koko.theme`), as it keeps
  the language. `useMode()` is the mode chosen and the function that
  chooses another, which the account menu calls: Light, Dark or System.
- **`<html data-theme>`** holds `light` or `dark` for a choice of either,
  and is absent for System; Astryx's `Theme` writes it, and its tokens
  follow. `index.html` carries `<meta name="color-scheme" content="light
  dark">` for the first frame, and `main.tsx` writes a stored choice to
  `<html>` (`applyStoredMode`) before React's first render, which runs in
  a later task, since `index.html` may run no inline script.

## Pages and routing

- **A page is a path.** A [`Route`](../../web/src/shell/router.tsx) names a
  path (a `:name` segment matches any one segment, decoded, and reaches the
  page as `params.name`), the page component, the message the side
  navigation links it under (`nav`) and its Lucide icon there (`icon`), and
  whether only the Owner sees it (`ownerOnly`). The side navigation links
  pages in the order of `routes` in
  [`web/src/routes.ts`](../../web/src/routes.ts), and `/` goes, keeping its
  query, to the first linked page the caller may see. A path no route
  matches, or one that does not decode, shows that there is no page there.
- **Page state lives in the URL's query** (`useSearchParams`, and
  `useQueryFlag` for a yes-or-no state kept as `name=1`), so a bookmark
  is a saved view and Back returns to the earlier state. The router uses the
  browser's history alone; the web front serves `index.html` for every path
  outside `/v1/`.
- **A visit** is each history entry the browser moves to: a `Link`, Back or
  Forward, even to the URL it is on. A replace (`navigate(to, {replace:
  true})`), such as the redirect from `/`, refines the entry it is on and is
  not a new visit.

## Who the web UI acts as

- **The top bar** shows the User and the device name, and the side
  navigation the links to the pages the caller may see.
- **It learns them from `user.current`**
  ([`api.UserCurrent`](../../internal/shared/api/user.go)), the operation
  `koko join` asks, so the web UI adds no operation of its own (ADR 0006). It
  is a read like any page's, reloaded on each visit and on focus. The shell
  renders a page only once it has answered, and pages read the answer as a
  [`Caller`](../../web/src/shell/caller.tsx) (`useCaller`).
- **Over the Unix socket** `user.current` answers the Owner with no Device
  Session, for the Local caller (ADR 0008), so the top bar shows the Owner's
  name "on the koko server's machine" where a device name would be.
- **Owner-only pages and buttons.** The role in `user.current`'s User decides:
  a Member gets no link to an `ownerOnly` route, and its path shows no page;
  `<OwnerOnly>` shows its children to the Owner alone. The koko server refuses
  a Member's Owner-only call anyway ([the access check](access.md)).

## Language

- **Catalogues.** Every string the web UI shows comes from a catalogue,
  [`web/src/i18n/catalogues.ts`](../../web/src/i18n/catalogues.ts): English
  and Simplified Chinese, each joined from one file per area
  (`en/<area>.ts`, `zh-CN/<area>.ts`). An area's file holds its own keys,
  `<area>.*`, and the messages for its API error codes, `error.<code>`; the
  shell's are in `shell.ts`, and those for the codes every area shares in
  `errors.ts`. A Simplified Chinese file is typed against its English twin,
  so `tsc` fails when a key is missing or extra.
- **The completeness test**,
  [`catalogues.test.ts`](../../web/src/i18n/catalogues.test.ts), fails when
  either language lacks a key the other has, a message is empty, a registered
  API error code (a `Code...` constant of the [generated
  files](web-ui.md#generated-types)) has no message in its area's file in
  either language, an area's file holds another area's keys, or
  `catalogues.ts` leaves an area's file out. So a change that registers an
  error code adds its message in both languages.
- **The register.** Every string is in the formal register of product UI
  (spec #108). Review checks it: a commit that adds or changes strings
  lists them in its body, English and Chinese side by side.
- **Choosing.** The first of the browser's preferred languages
  (`navigator.languages`) that the web UI ships wins: Chinese of any region
  gives Simplified Chinese, English gives English, and none gives English.
  The account menu overrides it, and the browser keeps the choice in
  `localStorage` (`koko.language`). `<html lang>` follows the language, and
  so do Astryx's own strings ([Providers](#providers)).
- **Never translated.** Names, keys, values and Notes reach a message only as
  its `{name}` parameters, shown as stored.
  [`formatDate`](../../web/src/shell/format.ts) shows a date as `YYYY-MM-DD`
  in both languages: a calendar date as stored, and a time the API returns
  (RFC 3339 in UTC) as its UTC date, the first ten characters the CLI prints.
- **API errors** show their code's message (`errorMessage`, `ErrorMessage`);
  the server's own English message is not shown.

## Calls

- **The client.** [`call`](../../web/src/client.ts) posts the request as JSON
  to the operation's path. It throws an `ApiError` with the code when the koko
  server refuses, and fails an answer that is not JSON rather than read it as
  empty.
- **One retry.** A call whose connection drops (fetch's `TypeError`, before
  or during the answer) is sent once more; a second drop throws a
  `DroppedError`. `koko web`'s browser connections reopen after a Device
  Connection drop, since only Tunnels Resume (ADR 0021).
- **`Idempotency-Key`.** Every write carries one, 128 random bits as 32
  lowercase hex characters ([the API](api.md#idempotency-key)), and its retry carries the
  same, so the koko server applies it once.
- **Reads** go through `useRead` (in [`data.ts`](../../web/src/shell/data.ts)),
  which loads again when the request changes, on every visit and when the
  window regains focus. The web UI holds no connection open and never polls.
  An answer to an earlier request never shows as the current one's; a failed
  reload keeps the last answer to the same request beside the error. `reload`
  shows what a write changed. The one read a person asks for by a click,
  revealing a Secret's value (`secret.show`), calls `call` on the click
  instead, and its page shows only the latest answer
  ([the Secrets page](secrets.md#the-web-uis-secrets-page)).
- **Writes** go through `useWrite`, one per form: submitting the same request
  again after it failed reuses its key, and a write that succeeded, or a
  different request, takes a new one. A write that succeeds while a later
  one is pending leaves the later one its key.
- **Edit forms** use [`useEditForm`](../../web/src/shell/form.ts), which
  sends only the fields whose value differs from its baseline: the record as
  the form opened with it, and after `saved()`, once a write succeeded, the
  values written. A field someone else changed meanwhile is never
  overwritten; the last writer wins per field.

## Shown once

What a page shows once and the browser must not keep, a Join Code just
issued or a Secret value revealed, lives in
[`useShownOnce`](../../web/src/shell/shownOnce.ts): the page's state alone,
never the URL or the browser's storage. Leaving the page within the web UI
loses it, the browser leaving the page (`pagehide`) forgets it, so a page
the browser keeps in its back-forward cache holds it no more when Back brings
it back, and the page forgets it at once by setting it to `undefined`.

A line a page shows once to be pasted elsewhere, a Join Code's `koko join`
line today and a Password Link later, shows in
[`ShownOnceLine`](../../web/src/shell/ShownOnceLine.tsx): a region named for
what it holds, in an Astryx `Card`, with the line in Astryx's `Code`, which
breaks anywhere on a phone and one click selects whole; a copy
`IconButton` beside it, which says whether the browser copied it; and the
time left as text, counted each second on the browser's clock from when the
line showed, for the validity the page asked for (`validFor`). The time
left reads in minutes and seconds (`14:59`) for its last hour, and before
that in days and whole hours in the language (`6 days, 23 hours`, `6天23小时`,
`Intl.DurationFormat`), as a validity of up to 7 days (#115) calls for. Once
it ran out, the line goes and the page's own words say what to do instead.

## Confirmations

[`ConfirmDialog`](../../web/src/shell/ConfirmDialog.tsx) asks before an
irreversible action (retire, removing a record, revoke, removing a User) with
a title, the consequence and the action's button, as Astryx's
`AlertDialogDeleteConfirmation` pattern does. It is Astryx's `AlertDialog`
(`role="alertdialog"`, named by its title and described by the
consequence), open while the page renders it, so the page behind takes no
input, a click beside it does nothing, Escape cancels, and the focus returns
where it was when it closes. Its first control takes the focus: Cancel, so
Enter never acts by accident. With `typedName`, it adds the field for the
name, which takes the focus, and the action stays disabled until that name
is typed exactly: the web UI's form of `--last`, for the Owner's own last
Device Session. `AlertDialog` takes no content of its own, so that variant
is its composition (`Dialog`, `Layout`, `Heading`, `Text` and two `Button`s)
with the field added. On a phone, at 640 pixels wide and below, the buttons
fill the width, the action above Cancel; Astryx offers no bottom sheet for
an alert dialog. Reversible writes, revealing a Secret and issuing a Join
Code never ask.

## Notices

Every call's outcome reaches the shell (`observeCalls`), which shows one
notice above the frame, an Astryx `Banner` across the width, announced as
an alert ([`notice.ts`](../../web/src/shell/notice.ts)):

| Notice | When | Ends |
| --- | --- | --- |
| koko web is not running; run it again | a call dropped twice | when a later call reaches the koko server |
| this browser's cookie is not one of this run of `koko web` | `cookie_required` | when a later call gets through, once the browser holds a new run's cookie |
| this device's Device Session was Revoked | `revoked`, or a `device.revoke` that answers with the Device Session the web UI acts through | never |

When the Device Session is Revoked elsewhere, `koko web` exits, and the page
can tell only that it stopped.

**A refusal** shows in place, where the action was, never as a toast:
`ErrorMessage` is an error `Banner` with the message of the API error's
code, announced as an alert (`role="alert"` on its message alone). Astryx
keeps live regions of roles `alert` and `status` of its own, empty until
it announces something, so a test reads the alerts that say something
(`alerts()`, `findAlert()`, `statuses()` in
[`web/src/test/pages.tsx`](../../web/src/test/pages.tsx)).

## The page inventory

Every action each page offered before its redesign on Astryx, with the
call it makes and who may take it. A redesign may move an action, but each
stays possible by the same call with the same request, and each page's
test holds one test per action (spec #108: "Patterns, not a re-skin").
"In the Space" is anyone who can act in the record's Space: the Owner, and
its Members; the koko server answers anyone else as for a record that does
not exist. The page's references hold the rules each action follows:
[the record's pages](record-pages.md), [Secrets](secrets.md#the-web-uis-secrets-page)
and [the Spaces, People and Devices pages](people-pages.md).

**The frame**, on every page:

| Action | Call | Who |
| --- | --- | --- |
| See who the web UI acts as, from which device | `user.current` | anyone |
| Follow a link to each page the caller may see | none | anyone; People the Owner alone |
| Choose the language, which the browser keeps | none | anyone |
| Choose Light, Dark or System, which the browser keeps | none | anyone |

**Hosts** (`/hosts`; the view in the query: `filter`, `sort`, `columns`, `all=1`):

| Action | Call | Who |
| --- | --- | --- |
| List the Hosts, filtered, sorted, in the columns chosen | `host.list` (and `attr.list` for the keys) | anyone: the Hosts of their Spaces |
| Build a filter term: a key, a comparison, a value; take one away | none (the URL) | anyone |
| Sort by a header; Shift-click for a further key | none (the URL) | anyone |
| Show or hide a column | none (the URL) | anyone |
| Show the Retired Hosts; ticked and fixed while a `state=retired` term is there | none (`all=1`) | anyone |
| Go back to the default view from a URL the list does not take, which says which term | none | anyone |
| Open a Host, a Retired one by its id | none | anyone |
| Add a Host: `name`, `space`, `address`, `port`, `account`, `relay` | `host.add` (and `space.list` for the Spaces) | anyone, in a Space they can act in |

**A Host** (`/hosts/<name or id>`); a Retired Host offers only what says so:

| Action | Call | Who |
| --- | --- | --- |
| See its fields, Attributes, Note, Services (read-only when Retired) and fingerprint | `host.show` | in the Space; Retired too |
| Copy `koko host enroll <host>`, for a Registered Host | none | in the Space |
| Edit: set or clear `address`, `port`, `account`, `relay`, Attributes and Note (`unset` for a value emptied) | `host.set` (and `attr.list`) | in the Space; Retired: Attributes and Note alone |
| Rename | `host.rename` | in the Space |
| Move to another Space | `host.move` (and `space.list`) | in both Spaces; Retired too |
| Add a Service: `name`, `ports` | `service.add` | in the Space |
| Retire, asking first | `host.retire` | in the Space |
| Delete, with no pin, asking first | `host.remove` | in the Space; Retired too |
| Open a Service, or the Relay | none | in the Space; Retired too |

**Services** (`/services`; the view in the query: `filter`, `sort`, `columns`):

| Action | Call | Who |
| --- | --- | --- |
| List the Services, filtered, sorted, in the columns chosen | `service.list` (and `attr.list`) | anyone: the Services of their Spaces |
| Build a filter term, sort, show or hide a column | none (the URL) | anyone |
| Go back to the default view from a URL the list does not take, which says which term | none | anyone |
| Open a Service or its Host | none | anyone |

**A Service** (`/services/<host>/<service>`):

| Action | Call | Who |
| --- | --- | --- |
| See its fields, Attributes and Note | `service.show` | in the Space |
| Edit: set or clear its ports, Attributes and Note | `service.set` (and `attr.list`) | in the Space, on a Host not Retired |
| Rename | `service.rename` | in the Space, on a Host not Retired |
| Delete, asking first | `service.remove` | in the Space, on a Host not Retired |

**Secrets** (`/secrets`):

| Action | Call | Who |
| --- | --- | --- |
| List the Secrets by Space | `secret.list` (and `space.list`) | anyone: the Secrets of their Spaces |
| Show a value, then copy it or hide it | `secret.show` | in the Space |
| Add: `space`, `name`, a value of several lines | `secret.add` | in the Space |
| Set a new value | `secret.set` | in the Space |
| Rename | `secret.rename` | in the Space |
| Move to another Space | `secret.move` | in both Spaces |
| Delete, asking first | `secret.remove` | in the Space |

**Spaces** (`/spaces`) and **a Space** (`/spaces/<name>`):

| Action | Call | Who |
| --- | --- | --- |
| List the Spaces, a Personal Space marked so | `space.list` | anyone: the Spaces they can act in |
| Add a Space | `space.add` | the Owner |
| See a Space's Members and its counts of Hosts, Retired Hosts and Secrets | `space.show` | in the Space |
| Rename a Space | `space.rename` | the Owner, on a shared Space |
| Remove a Space, asking first | `space.remove` | the Owner, on a shared Space |
| Add a Member, chosen among the Users who are not one | `space.member.add` (and `user.list`) | the Owner, on a shared Space |
| Take a Member out | `space.member.remove` | the Owner, on a shared Space |

**People** (`/people`, the Owner alone; `all=1` in the query):

| Action | Call | Who |
| --- | --- | --- |
| List the Users, the Removed ones when asked for | `user.list` | the Owner |
| Invite a User, their Join Code shown once, with Copy and its countdown | `user.invite` | the Owner |
| Remove a User, asking first, then say whether they were deleted or Removed | `user.remove` | the Owner, on any User but the Owner who is not Removed |

**Devices** (`/devices`; `all=1` in the query):

| Action | Call | Who |
| --- | --- | --- |
| List the Device Sessions, the Revoked ones when asked for, this one marked | `device.list` | anyone: their own; the Owner everyone's |
| Add a device, with an optional name, its Join Code shown once, with Copy and its countdown | `device.add` | anyone for themselves |
| Add a device for another User, chosen among those not Removed | `device.add` (and `user.list`) | the Owner |
| Revoke a live Device Session, asking first; the Owner's own last only once its name is typed, with `last` | `device.revoke` | anyone for their own; the Owner for anyone's |

## Verifying

- [`Shell.test.tsx`](../../web/src/shell/Shell.test.tsx), Vitest and React
  Testing Library against the [fake API](web-ui.md#testing-pages), with test
  pages of their own: the top bar on a device and over the Unix socket;
  the side navigation marking the page the URL is in, and axe on the
  frame; on a phone, the drawer the top bar opens, linking no Owner-only
  page to a Member and closed by a visit, and the account menu naming whom
  the web UI acts as; the account menu choosing the mode, remembered; "Page
  not found" with its link to the first page; a
  write and a read retried once, with and without the same key; a form
  resubmitted with its key, a new key after success or for another request,
  and overlapping writes; an edit form sending only changed fields, its
  baseline after a save, and Save never asking; the dialog, its focus going
  to Cancel and back to the button that opened it, axe, Escape, and its
  typed-name variant with the focus on its field and back on the button
  that opened it, its action above Cancel on a phone; a Member seeing no Owner-only page, link or button, and
  the Owner seeing them; the language from the browser's preferences, the
  account menu overriding it and remembered; an API error's message, in its
  `Banner`, in either language, and axe on it; values shown as stored; the three notices, revoking the web UI's
  own Device Session, and a refusal ending the stopped notice; reloads on
  focus and on each visit; page state in the URL and Back; `/` going to the
  first page.
- [`providers.test.tsx`](../../web/src/shell/providers.test.tsx): the mode
  System by default and following a faked system mode; Light and Dark
  chosen, kept in `localStorage`, written to `<html data-theme>`, and
  System again; a stored choice applied before the first render, and one
  that is no choice read as System; an Astryx dialog's close label in
  English and in Chinese; no `<style>` element in the page, which the CSP
  refuses, with an Astryx dialog open; an Astryx link followed through
  koko's router.
- [`catalogues.test.ts`](../../web/src/i18n/catalogues.test.ts): the
  completeness test above.
- [`ShownOnceLine.test.tsx`](../../web/src/shell/ShownOnceLine.test.tsx):
  a line valid for 7 days counting down in days and hours, then in minutes
  and seconds, then gone; axe; the copy button copying the line whole.
- [`format.test.ts`](../../web/src/shell/format.test.ts): dates as
  `YYYY-MM-DD`; the time left in minutes and seconds, and in days and hours
  in either language.
- [`App.test.tsx`](../../web/src/App.test.tsx): the web UI opens on its first
  page.
