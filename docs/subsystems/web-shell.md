# The web UI shell

The frame every page of the web UI sits in, and the parts pages share: the
top bar with its places, its Language and Appearance menus and the account
panel, routing and who the web UI acts as, the language, the mode and the
catalogues, the API client, pages that load when first shown, the states
every screen has, the dialogs and side panels, the confirmation, the
fields, edit forms, reloading, what a page shows once, and the notices,
toasts and refusals. What each looks like is [the web UI's
design](../web-design.md). The pages themselves are their capabilities'; where an area adds its pages and its
catalogue files is in [code layout](code-layout.md#where-an-area-adds-its-parts).
How the app is built, embedded and typed is [the web UI build](web-ui.md); how
a browser reaches it is [`koko web` and the web front](web-front.md).

## Providers

The web UI is built on HeroUI ([ADR
0046](../adr/0046-the-web-ui-is-built-on-heroui.md)). The `Shell` renders
everything inside [`Providers`](../../web/src/shell/providers.tsx), so the
app [`main.tsx`](../../web/src/main.tsx) renders and the app a page test
renders sit in the same ones, outermost first:

| Provider | Gives |
| --- | --- |
| `LanguageProvider` | koko's language and its catalogues ([Language](#language)) |
| [`ModeProvider`](../../web/src/shell/mode.tsx) | the mode chosen, kept on `<html>` ([Light and dark](#light-and-dark)) |
| `I18nProvider` | React Aria's own strings, such as a dialog's dismiss label, and its dates and numbers, in koko's language: `en-US` or `zh-CN`, the two locales the build keeps |
| `RouterProvider` | koko's router behind every link HeroUI and React Aria render, so a link's `href` is followed without a page load; a link takes `routerOptions`, koko's `NavigateOptions`, such as `{ replace: true }` |

`Toast.Provider`, inside them, shows the toasts at the bottom end
([Notices](#notices)). `main.tsx` imports
[`theme/theme.css`](../../web/src/theme/theme.css), the theme on HeroUI's
styles ([the web UI build](web-ui.md#heroui-and-tailwind)).

## The frame

The [`Shell`](../../web/src/shell/Shell.tsx) is the design's frame
([The frame](../web-design.md#the-frame)):

- **A skip link**, "Skip to main content", is the first stop of Tab; it
  moves the focus to the main region, without changing the URL.
- **The top bar**, sticky, holds koko's wordmark, a link to `/`; in the
  centre, from 768 pixels wide, the places the caller may see, in a raised
  track; and at its end ([`TopBar.tsx`](../../web/src/shell/TopBar.tsx))
  the Language and Appearance buttons, each an icon button named for its
  value ("Language: English", "Appearance: System") that opens a menu of
  radio items, and, once `user.current` has answered, the account button,
  named "Account: <name>", which opens the account panel.
- **The places** are the routes with a `nav` message, in the order of
  `routes`; the three of the design, Hosts, Secrets and Access, have their
  messages in the shell's catalogue (`shell.place.*`), and each comes back
  to the top bar with its page. The one the URL is in, a page beneath its
  path such as a Host's included, has `aria-current="page"`. A Member gets
  no place whose route is `ownerOnly`.
- **On a phone**, under 768 pixels wide, the places move to a bar fixed at
  the foot, each with its Lucide icon (`Route.icon`) and its name, above
  the safe area; the main region leaves room for it.
- **The account panel** shows the User's initial, name and role, and how
  this browser reaches koko: "Through koko web on <device>" for a Device
  Session, "Signed in from <browser>" for a Sign-in, and "On the koko
  server's machine" for the Local caller; then a link to Account
  (`accountPage`), which closes the panel as it is followed; and, for a
  browser signed in with a Password, Sign out (`useSignOut` in
  [`signOut.ts`](../../web/src/shell/signOut.ts)), which posts to
  `/auth/signout` and goes to Sign in, replacing the history entry, its
  refusal shown in the panel.
- **The main region** (`<main id="main">`) holds the page. After a visit
  that leaves the focus on nothing, as when the link followed went with the
  page it was on, the focus moves to it; a visit that leaves the focus
  where it was, such as on a place, keeps it there.
- **The document's title** is the page's own name, then koko ("web1 ·
  koko"): the page's head (`Page`, [Parts](#parts)) sets it.
- **The notices** are one notice across the width under the top bar
  ([Notices](#notices)).
- **A path with no page** shows "Page not found" in the frame, with one
  sentence and a link to the first place; so does a path only the Owner may
  open, to a Member.
- **Until `user.current` answers** the main region shows Loading; if it
  fails, why, with Try again ([States](#the-states-every-screen-has)).
- **The icon** of the browser's tab is `web/public/favicon.svg`, which
  `index.html` links, so the browser asks for no `/favicon.ico`.
  `index.html`'s viewport tag carries `viewport-fit=cover`, which lets the
  phone's bar keep clear of the safe area.

## Light and dark

- **The mode** is System by default: the web UI follows the system's light
  or dark mode (`prefers-color-scheme`), and changes with it. Light or Dark
  overrides it, and the browser keeps the choice in `localStorage`
  (`koko.theme`), as it keeps the language. `useMode()` is the mode chosen
  and the function that chooses another, which the Appearance menu calls.
- **`<html>`** holds the mode in effect, light or dark, the system's for
  System, as its class (`light` or `dark`) and its `data-theme`; the
  theme's tokens and HeroUI's follow both. `index.html` carries `<meta
  name="color-scheme" content="light dark">` for the first frame, and
  `main.tsx` writes the stored choice to `<html>` (`applyStoredMode`)
  before React's first render, which runs in a later task, since
  `index.html` may run no inline script.

## Pages and routing

- **A page is a path.** A [`Route`](../../web/src/shell/router.tsx) names a
  path (a `:name` segment matches any one segment, decoded, and reaches the
  page as `params.name`), the page component, the message the top bar
  links it under as a place (`nav`) and its Lucide icon in the phone's bar
  (`icon`), whether only the Owner sees it (`ownerOnly`), and whether it
  sits outside the frame (`outside`). The top bar
  links the places in the order of `routes` in
  [`web/src/routes.ts`](../../web/src/routes.ts), and `/` goes, keeping its
  query, to the first place the caller may see. A path no route
  matches, or one that does not decode, shows that there is no page there.
- **A page loads when first shown.** An area's `routes.ts` gives a page as
  [`lazyPage(load)`](../../web/src/shell/lazyPage.tsx), so its code is a
  chunk of its own, outside [the first load](web-ui.md#size). Loading
  shows while the chunk loads, and a chunk that does not load, as after
  the koko server was upgraded under an open tab, says in place of the
  page, as an alert, that the browser page must be reloaded; any other
  error a page throws goes on past it. `loadPage(page)` loads a lazy
  page's chunk ahead of time, once, so its first render does not wait on
  it; the page tests load every page so before they run.
- **Page state lives in the URL's query** (`useSearchParams`, and
  `useQueryFlag` for a yes-or-no state kept as `name=1`), so a bookmark
  is a saved view and Back returns to the earlier state. The router uses the
  browser's history alone; the web front serves `index.html` for every path
  outside `/v1/`.
- **A list remembers its view.** A list page reads its query through
  [`useRememberedView`](../../web/src/shell/rememberedView.tsx), which
  keeps the last view each list showed in `localStorage`, under the User's
  id and the list's name (`koko.view.<user id>.<list>`): the Hosts view,
  the Services view, Secrets and the Owner's Access, each on its own. A URL with a view shows
  it and is remembered; a URL with none, from the top bar, a new tab, a
  link or Sign in, shows the view remembered and is replaced by it, so the
  URL still shows what is on screen. A history entry the page has shown as
  the default view is marked in its state, so Back to it, or its reload,
  shows the default again and leaves the view remembered as it is. A
  default view the page itself writes forgets the list's view first, and a
  tab that carries nothing to the other list shows that list's own view.
  **Reset view** ([`ResetView`](../../web/src/shell/rememberedView.tsx)),
  unavailable at the default, shows the default view and forgets the one
  remembered. Only the query is kept: no value of a Secret, nothing from a
  Note; signing out leaves it.
- **Outside the frame.** Sign in and Set a Password
  ([Sign-ins](signin.md#the-web-uis-pages)), which a browser opens before it
  is signed in on the web listener, are `outside` routes: the shell renders
  them on their own, with no top bar and without asking `user.current`.
- **A moved page.** A path a released koko served that has moved is a
  route whose page is `movedTo(to)`: it goes to the path `to` gives for its
  params, keeping its query and replacing the history entry.
- **A visit** is each history entry the browser moves to: a link, Back or
  Forward, even to the URL it is on. A replace (`navigate(to, {replace:
  true})`), such as the redirect from `/`, refines the entry it is on and is
  not a new visit.

## Who the web UI acts as

- **The account panel** shows the User and how this browser reaches koko,
  and the top bar the places the caller may see.
- **It learns them from `user.current`**
  ([`api.UserCurrent`](../../internal/shared/api/user.go)), the operation
  `koko join` asks, so the web UI adds no operation of its own (ADR 0006). It
  is a read like any page's, reloaded on each visit and on focus. The shell
  renders a page only once it has answered, and pages read the answer as a
  [`Caller`](../../web/src/shell/caller.tsx) (`useCaller`).
- **Over the Unix socket** `user.current` answers the Owner with no Device
  Session, for the Local caller (ADR 0008), so the account panel says "On
  the koko server's machine" where a device would be. On the web listener
  it answers the User of this browser's Sign-in (`signin`), whose browser
  the panel names.
- **Owner-only pages and buttons.** The role in `user.current`'s User decides:
  a Member gets no place for an `ownerOnly` route, and its path shows no page;
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
  The top bar's Language menu overrides it, and the browser keeps the
  choice in `localStorage` (`koko.language`). `<html lang>` follows the
  language, and so do React Aria's own strings ([Providers](#providers)).
- **Never translated.** Names, keys, values and Notes reach a message only as
  its `{name}` parameters, shown as stored.
  [`formatDate`](../../web/src/shell/format.ts) shows a date as `YYYY-MM-DD`
  in both languages: a calendar date as stored, and a time the API returns
  (RFC 3339 in UTC) as its UTC date, the first ten characters the CLI prints.
  `formatAgo` shows how long ago such a time was, in the language ("3 days
  ago", "3天前"), as a Secret's "Changed" does.
- **Counts.** A message that counts takes the language's plural forms,
  `<key>.one` and `<key>.other` (`useCount`), so English says "1 Host" and
  "9 Hosts"; Simplified Chinese, with one form, says both in `.other`.
- **API errors** show their code's message (`errorMessage`, `Refusal`);
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
- **The `/auth/` endpoints** are not API operations: Sign in, Set a
  Password, Account's Password and Sign out post to them with
  [`post`](../../web/src/client.ts), typed by their generated
  `AuthEndpoint` values (`AuthSignIn`, [the web UI build](web-ui.md#generated-types)),
  once, with no retry and no `Idempotency-Key`, since a Password Link sent
  again after it was spent would be refused as used. A refusal is an
  `ApiError` as a call's is, with the `Retry-After` header in seconds
  (`retryAfter`) when it has one, as `too_many_attempts` does; a
  connection that fails throws an `UnreachableError`, which says the koko
  server could not be reached.
- **`Idempotency-Key`.** Every write carries one, 128 random bits as 32
  lowercase hex characters ([the API](api.md#idempotency-key)), and its retry carries the
  same, so the koko server applies it once.
- **Reads** go through `useRead` (in [`data.ts`](../../web/src/shell/data.ts)),
  which loads again when the request changes, on every visit and when the
  window regains focus. The web UI holds no connection open and never polls.
  An answer to an earlier request never shows as the current one's; a failed
  reload keeps the last answer to the same request beside the error. `reload`
  shows what a write changed. The one read a person asks for by a click,
  showing or copying a Secret's value (`secret.show`), calls `call` on the
  click instead, and its page shows only the latest answer
  ([the Secrets page](secrets.md#the-web-uis-secrets-page)).
- **Writes** go through `useWrite`, one per form: submitting the same request
  again after it failed reuses its key, and a write that succeeded, or a
  different request, takes a new one. A write that succeeds while a later
  one is pending leaves the later one its key.
- **Edit forms** send only the fields whose value differs from their
  baseline: the record as the form opened with it, and, once a write
  succeeded, the values written. A field someone else changed meanwhile is
  never overwritten; the last writer wins per field.
  [`useEditForm`](../../web/src/shell/form.ts) holds a form of plain
  values so; the Host form and a Service's panel, whose Attributes are
  rows, compare each key in turn ([edit
  forms](record-pages.md#edit-forms)).

## Shown once

What a page shows once and the browser must not keep, a Join Code just
issued or a Secret value revealed, lives in
[`useShownOnce`](../../web/src/shell/shownOnce.ts): the page's state alone,
never the URL or the browser's storage. Leaving the page within the web UI
loses it, the browser leaving the page (`pagehide`) forgets it, so a page
the browser keeps in its back-forward cache holds it no more when Back brings
it back, and the page forgets it at once by setting it to `undefined`.

A line a page shows once to be pasted elsewhere, a Join Code's `koko join`
line or a Password Link, shows in
[`ShownOnceLine`](../../web/src/ui/ShownOnceLine.tsx): a region named for
what it holds, an inset block with a dashed accent edge holding its name,
the time left and the line in the code face, which breaks anywhere and one
click selects whole; under it the page's words (`intro`), the line that it
is shown only here, and Copy. The time left is counted each second on the
browser's clock from when the line showed, for the validity the page asked
for (`validFor`). It reads in minutes and seconds (`14:59`) for its last
hour, and before that in days and whole hours in the language (`6 days,
23 hours`, `6天23小时`, `Intl.DurationFormat`), as a validity of up to 7
days (#115) calls for. Once it ran out, the line goes and the page's own
words say what to do instead. The dialog that shows it
([`IssueDialog`](../../web/src/pages/device/IssueDialog.tsx)) is not
closed by a click beside it, since what it shows cannot be shown again.

## Parts

The parts of the design's Components table
([Components](../web-design.md#components)), in
[`web/src/ui/`](../../web/src/ui), each built on HeroUI and the shell's
logic:

| Part | File | What it is |
| --- | --- | --- |
| `Page` | `Page.tsx` | a page's head: its title, an optional line, its actions, a detail page's breadcrumb; it names the browser's tab (`useDocumentTitle`) |
| `Sheet` | `Page.tsx` | a panel, named by its title as a region; `list` lays a list on it edge to edge |
| `Form` | `Form.tsx` | a form that validates when it is submitted ([States](#the-states-every-screen-has)) |
| `FormDialog` | `FormDialog.tsx` | a short form of one to three fields in HeroUI's `Modal`, open while the page renders it: its fields, the refusal in place (`error`), Cancel, and the button that acts (`submit`), pending while the call runs (`pending`); a click beside it closes it unless a field has `changed` or the call runs ([Closing an overlay](../web-design.md#components)); `size="md"` for a field of several lines |
| `SidePanel` | `SidePanel.tsx` | HeroUI's `Drawer`, open while the page renders it: from the right on a desktop (`wide` for the Host form), from the bottom on a phone, with a handle; with `onSubmit` it is a `Form`; a click beside it closes it unless a field has `changed` or the form's call runs (`pending`) |
| `ConfirmDialog` | `ConfirmDialog.tsx` | the confirmation ([Confirmations](#confirmations)) |
| `Field` | `fields.tsx` | a text field or area, its label above, help under it and its error in place; `code` sets a machine value in the code face, with no spelling or capital corrected |
| `Choice` | `fields.tsx` | HeroUI's `Select`, for one item of a short fixed list |
| `SearchChoice` | `fields.tsx` | HeroUI's `ComboBox`, for one item of a list that can grow (the Owner's rule): its whole list opens when its field is pressed, never on the focus alone ([Searchable choices](../web-design.md#components)), and narrows to the items holding what is typed, whatever its case; it takes an item of its list alone, unless `newValues`, as an Attribute key does; `inlineLabel` shows its hidden label in the field before the value, as Group by does |
| `Segments` | `fields.tsx` | HeroUI's `ToggleButtonGroup`, a choice among two to four, all in view |
| `DateInput` | `DateInput.tsx` | HeroUI's `DatePicker`, a calendar date as `YYYY-MM-DD`; a module the forms that use it import alone, so the calendar is not in the first load |
| `RowMenu` | `RowMenu.tsx` | HeroUI's `Dropdown` behind an icon button named for it, its danger actions last after a separator |
| `CopyButton` | `copy.tsx` | Copy: "Copied" for 1.6 s, announced politely; a browser that does not allow it says to select the text. A value fetched on the click, as a Secret's row copies one, keeps the button pending until it comes and goes to the clipboard as a `ClipboardItem` at once (`writeClipboard`), since Safari refuses a copy made after a wait; a failure to fetch it goes to `onError` |
| `CommandLine` | `copy.tsx` | a command to copy: a `$`, which Copy leaves out, the command in the code face, and Copy, in an inset block named for it |
| `ShownOnceLine` | `ShownOnceLine.tsx` | what a page shows once ([Shown once](#shown-once)) |
| `StateMark` | `StateMark.tsx` | a Host's state as a mark of its own shape and its word |
| `notify` | `notify.ts` | a toast saying what a call that changed something did ([Notices](#notices)) |
| `Loading`, `LoadFailed`, `Loaded`, `EmptyState`, `NoMatch`, `Refusal` | `states.tsx` | the states every screen has |
| `useFocusFallback` | `focus.ts` | the focus kept in place when an action takes away the control that had it, as Revoke does with its row: the list's heading takes it |

A dialog or panel takes the focus as it opens, closes on Escape, and on a
click beside it as [Closing an overlay](../web-design.md#components) says,
and gives the focus back to what opened it; every icon button has a name.

## The states every screen has

[`states.tsx`](../../web/src/ui/states.tsx) gives each state of the design
([States every screen has](../web-design.md#states-every-screen-has)):

- **Loading**: a page keeps its title and frame; while its first call runs,
  each panel shows nothing for 300 ms, then three skeleton rows in place of
  its list, announced as loading (`Loading`). A button whose call runs is
  pending and takes no second press.
- **Failed to load**: the panel shows the koko server's message and Try
  again (`LoadFailed`). Any call within the frame answered
  `signin_required`, a read or a write, as when the Sign-in ended in
  another tab, takes the browser to `/signin?next=<this page>` instead as
  the answer comes, replacing the history entry; a second such answer finds
  it on Sign in and leaves it there (`useSignInWhenRequired` in
  [`Shell.tsx`](../../web/src/shell/Shell.tsx)). `Loaded` shows a read's answer once it has
  one, Loading until then, and the failure, above the last answer when a
  reload failed.
- **Empty**: one panel with a line saying what is missing, a sentence
  saying what the thing is for, and the action that adds one (`EmptyState`).
- **No match**: one panel saying nothing matches the view, and the button
  that clears the search and filters (`NoMatch`).
- **A refused write**: the koko server's message in place, an alert, the
  form kept as typed (`Refusal`); a page that says more than the code's
  message, such as the Personal Space that keeps a User from being deleted,
  gives `Refusal` its own message as a string.
- **Forms** validate when submitted (`Form`): the submit button stays
  enabled; each field at fault shows koko's message under it (a field's
  `validate`), the first takes the focus, and nothing is sent. The form
  carries `noValidate`, so the browser's own bubbles never show; it calls
  `checkValidity`, whose invalid events React Aria's fields hear.

## Confirmations

[`ConfirmDialog`](../../web/src/ui/ConfirmDialog.tsx) asks before an
irreversible action (retire, removing a record, revoke, removing a User)
with a title, the consequence and the action's button, a danger button. It
is HeroUI's `AlertDialog` (`role="alertdialog"`, named by its title and
described by the consequence), open while the page renders it, so the page
behind takes no input, a click beside it and Escape cancel unless its call
runs (`pending`), and the focus returns where it was when
it closes. Its first control takes the focus: Cancel, so Enter never acts
by accident. With `typedName`, it adds the field for the name, which takes
the focus, and the action stays disabled until that name is typed exactly:
the web UI's form of `--last`, for the Owner's own last Device Session. A
refusal shows in it (`error`). Reversible writes, revealing a Secret and
issuing a Join Code never ask.

## Notices

Every call's outcome reaches the shell (`observeCalls`), which shows one
notice across the width under the top bar, announced as an alert, until
what ends it ([`notice.ts`](../../web/src/shell/notice.ts)); it is not a
toast:

| Notice | When | Ends |
| --- | --- | --- |
| koko web is not running; run it again | a call dropped twice | when a later call reaches the koko server |
| this browser's cookie is not one of this run of `koko web` | `cookie_required` | when a later call gets through, once the browser holds a new run's cookie |
| this device's Device Session was Revoked | `revoked`, or a `device.revoke` that answers with the Device Session the web UI acts through | never |

When the Device Session is Revoked elsewhere, `koko web` exits, and the page
can tell only that it stopped.

**What a call changed** shows in a toast at the bottom end (`notify`),
such as "web1 is Retired.".

**A refusal** shows in place, where the action was, never as a toast:
`Refusal` is a block with the message of the API error's code, announced
as an alert (`role="alert"`). React Aria keeps live regions of its own,
empty until it announces something, so a test reads the alerts that say
something (`alerts()`, `findAlert()`, `statuses()` in
[`web/src/test/pages.tsx`](../../web/src/test/pages.tsx)).

## The page inventory

Every action each page offered before its redesign, and each its
redesign adds, with the call it makes and who may take it. A redesign may move an action, but each
stays possible by the same call with the same request, and each page's
test holds one test per action (spec #108: "Patterns, not a re-skin").
"In the Space" is anyone who can act in the record's Space: the Owner, and
its Members; the koko server answers anyone else as for a record that does
not exist. The page's references hold the rules each action follows:
[the record's pages](record-pages.md), [Secrets](secrets.md#the-web-uis-secrets-page)
and [the Access and Account pages](access-pages.md). The move to
HeroUI (#121) took the pages out; each came back with its ticket (#122 to
#125), holding these actions, and Sign-ins came back with #116.

**The frame**, on every page:

| Action | Call | Who |
| --- | --- | --- |
| See who the web UI acts as, from which device | `user.current` | anyone |
| Follow a link to each page the caller may see | none | anyone; a person's panel the Owner alone |
| Open Account from the account panel | none | anyone |
| Sign out, from the account panel | `/auth/signout` | a browser signed in with a Password |
| Choose the language, which the browser keeps | none | anyone |
| Choose Light, Dark or System, which the browser keeps | none | anyone |

**Hosts** (`/hosts`; the view in the query: `filter`, `sort`, `columns`, `all=1`, and `q`, `group`, `view=services` and `space`):

| Action | Call | Who |
| --- | --- | --- |
| List the Hosts, filtered, sorted, in the columns chosen | `host.list` (and `attr.list` for the keys) | anyone: the Hosts of their Spaces |
| Build a filter term: a key, a comparison, a value; take one away | none (the URL) | anyone |
| Sort by a header; Shift-click for a further key | none (the URL) | anyone |
| Show or hide a column | none (the URL) | anyone |
| Group each Space's Hosts by an Attribute key | none (the URL) | anyone |
| Show the Retired Hosts, through the state select, and while a `state=retired` term is there | none (`all=1`) | anyone |
| Go back to the default view from a URL the list does not take, which says which term | none | anyone |
| Reset view: the default view, the view the browser remembers forgotten | none | anyone |
| Open a Host, a Retired one by its id | none | anyone |
| Add a Host: `name`, `space`, `address`, `port`, `account`, `relay`, its Attributes and Note | `host.add` (and `space.list` for the Spaces) | anyone, in a Space they can act in |

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

**Services** (`/hosts?view=services`, which `/services` goes to; the view in the query: `filter`, `sort`, `columns`, and `q` and `space`):

| Action | Call | Who |
| --- | --- | --- |
| List the Services, filtered, sorted, in the columns chosen | `service.list` (and `attr.list`) | anyone: the Services of their Spaces |
| Build a filter term, sort, show or hide a column | none (the URL) | anyone |
| Go back to the default view from a URL the list does not take, which says which term | none | anyone |
| Reset view: the default view, the view the browser remembers forgotten | none | anyone |
| Open a Service or its Host: a Service opens on its Host's page | none | anyone |

**A Service** (`/hosts/<host>/services/<service>`, a panel over its Host's page, which `/services/<host>/<service>` goes to):

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
| Search and narrow to a Space; Reset view, which the browser then forgets | none (the URL) | anyone |
| Show a value, then copy it or hide it | `secret.show` | in the Space |
| Copy a value without showing it | `secret.show` | in the Space |
| Add: `space`, `name`, a value of several lines | `secret.add` | in the Space |
| Set a new value | `secret.set` | in the Space |
| Rename | `secret.rename` | in the Space |
| Move to another Space | `secret.move` | in both Spaces |
| Delete, asking first | `secret.remove` | in the Space |

**Access** (`/access`, which `/spaces` and `/people` go to; `view=spaces`, `q=` and `all=1` in the
query) and **a Space's panel** (`/access/spaces/<name>`, which
`/spaces/<name>` goes to):

| Action | Call | Who |
| --- | --- | --- |
| List the Spaces, a Personal Space marked so, each with its Members and its counts of Hosts and Secrets | `space.list`, one call | anyone: the Spaces they can act in |
| See a Space's Members and its counts of Hosts, Retired Hosts and Secrets, in its panel | `space.show` | in the Space |
| List the People and the shared Spaces under two tabs, searched by name, the Removed Users when asked for | `user.list` (and `space.list`, `device.list`) | the Owner |
| Reset view: the People with no search and without the Removed Users, the view the browser remembers forgotten | none | the Owner |
| Add a Space | `space.add` | the Owner |
| Rename a Space | `space.rename` | the Owner, on a shared Space |
| Remove a Space, asking first | `space.remove` | the Owner, on a shared Space |
| Make a User a Member: a switch in a person's panel, or Add a Member chosen among the Users who are not one | `space.member.add` (and `user.list`) | the Owner, on a shared Space |
| Take a Member out: a switch in a person's panel, or Take out | `space.member.remove` | the Owner, on a shared Space |
| Invite a User, to the Spaces chosen, in a terminal or in a browser, valid for the time chosen, their Join Code or Password Link shown once, with Copy and its countdown | `user.invite` (and `server.show` for the Web URL), then `space.member.add` | the Owner |

**A person's panel** (`/access/people/<name>`, the Owner alone):

| Action | Call | Who |
| --- | --- | --- |
| See a User's role, Spaces, Personal Space and devices, the Revoked ones when asked for | `user.list`, `space.show`, `device.list` | the Owner |
| Make them a Member of a shared Space, or take them out, by its switch | `space.member.add`, `space.member.remove` | the Owner |
| Add a device for them, with an optional name, valid for the time chosen, its Join Code shown once | `device.add` | the Owner, for a User not Removed |
| Revoke a live Device Session of theirs, asking first; the Owner's own last only once its name is typed, with `last` | `device.revoke` | the Owner |
| See when their Password was set, or that they have none | `user.list` | the Owner |
| Issue a Password Link for them, valid for the time chosen, shown once with Copy and its countdown; disabled, saying why, without a Web URL | `password.link` (and `server.show`) | the Owner, for a User not Removed |
| Remove their Password, asking first | `password.remove` | the Owner, for a User with one |
| List their Browser Sign-ins, the ended ones when asked for, and revoke a live one, asking first | `signin.list`, `signin.revoke` | the Owner |
| Remove the User, asking first, then say whether they were deleted or Removed | `user.remove` | the Owner, on any User but the Owner who is not Removed |

**Account** (`/account`, which `/devices` goes to; `all=1` in the query):

| Action | Call | Who |
| --- | --- | --- |
| See who the web UI acts as and how this browser got in | `user.current` | anyone |
| Choose the language and Light, Dark or System, as the top bar does | none | anyone |
| List one's own Device Sessions, the Revoked ones when asked for, this one marked | `device.list` | anyone |
| Add a device, with an optional name, valid for the time chosen, its Join Code shown once, with Copy and its countdown | `device.add` | anyone for themselves |
| Revoke a live Device Session, asking first; the Owner's own last only once its name is typed, with `last` | `device.revoke` | anyone for their own |
| See what one's Personal Space holds | `space.show` | anyone |
| Change one's Password: the current one and the new one twice, which keeps this Sign-in and ends the others | `/auth/password` | a browser signed in with a Password |
| List one's own Browser Sign-ins, the ended ones when asked for, this browser's marked | `signin.list` | anyone |
| Revoke a live Sign-in, asking first; this browser's own signs it out | `signin.revoke` | anyone for their own |
| Sign out | `/auth/signout` | a browser signed in with a Password |

For the Owner, `/devices` listed everyone's Device Sessions; that view is
now each person's panel under Access.

**Sign in** (`/signin`, outside the frame; `next` in the query) and **Set a
Password** (`/password#<secret>`, outside the frame), the web listener's
alone ([Sign-ins](signin.md#the-web-uis-pages)):

| Action | Call | Who |
| --- | --- | --- |
| Sign in with a User name and Password, then go to `next` on this origin | `/auth/signin` | anyone with a Password |
| See whom a Password Link is for | `/auth/link` | anyone holding the link |
| Set the Password through the link, which signs this browser in | `/auth/password` | anyone holding the link |

## Verifying

- [`Shell.test.tsx`](../../web/src/shell/Shell.test.tsx), Vitest and React
  Testing Library against the [fake API](web-ui.md#testing-pages), with test
  pages of their own in the three places: the account panel for a Device
  Session, a Sign-in and the Local caller, and its Account link closing
  it; the Owner's three places and the one the URL is in marked, a page beneath a place's path in it, a Member
  given no Owner-only place and its path showing Page not found; on a
  phone the places in the bar at the foot with their icons; `/` going to
  the first place, keeping the query; Page not found with its link to the
  first place, and with no page yet, on every path; the document's title;
  the skip link as the first stop of Tab, moving the focus to the main
  region; the focus moved to the main region after a visit that left it on
  nothing, and kept on a place; the language from the browser's
  preferences, the Language menu overriding it and kept; Light, Dark and
  System from the Appearance menu, kept, System following the system; axe
  on the frame, the account panel and both menus, light and dark, at a
  desktop's and a phone's width; the three notices, revoking the web UI's
  own Device Session, a refusal ending the stopped notice; and
  `user.current` failing, with Try again.
- [`calls.test.tsx`](../../web/src/shell/calls.test.tsx): a write and a
  read retried once, with and without the same key; a form resubmitted
  with its key, a new key after success or for another request, and
  overlapping writes; an edit form sending only changed fields, and its
  baseline after a save; Owner-only buttons for a Member and the Owner; an
  API error's message in place in either language, and axe on it; values
  shown as stored; reloads on focus and on each visit; page state in the
  URL and Back.
- [`providers.test.tsx`](../../web/src/shell/providers.test.tsx): the mode
  System by default and following a faked system mode; Light and Dark
  chosen, kept in `localStorage`, written to `<html>`'s class and
  `data-theme`, and System again; a stored choice applied before the first
  render, and one that is no choice read as System; a React Aria string, a
  dialog's dismiss label, in English and in Chinese; a HeroUI link followed
  through koko's router.
- [`lazyPage.test.tsx`](../../web/src/shell/lazyPage.test.tsx): a lazy
  page's Loading, then the page; a page `loadPage` loaded rendering without
  waiting on its chunk, which loads once; a chunk that does not load saying
  so in the frame, and axe on it; another error not taken for one.
- [`states.test.tsx`](../../web/src/ui/states.test.tsx): Loading after 300
  ms and axe; LoadFailed with Try again; `Loaded`'s three cases; the empty
  state, no match and a refusal, each with axe.
- [`dialogs.test.tsx`](../../web/src/ui/dialogs.test.tsx): a short form
  validating on submit (the error under its field, the focus on it,
  nothing sent, `noValidate`), its refusal in place with the form kept as
  typed, a pending call taking no second press, Cancel, Escape and the
  close button closing it with the focus back; a side panel from the right
  and from the bottom, named, axe, Escape and the focus back, and as a form
  validating on submit; a confirmation described by its consequence, the
  focus on Cancel and back on its opener, acting, Escape not while its
  call runs, its refusal, and its typed-name variant.
- [`fields.test.tsx`](../../web/src/ui/fields.test.tsx): a field in the
  code face with its help; a `Choice`; a `SearchChoice` narrowing whatever
  the case, taking no value outside its list, and with `newValues` taking
  one; `Segments`; a row menu with its danger action last.
- [`parts.test.tsx`](../../web/src/ui/parts.test.tsx): a page's head and
  the tab's title; Copy, "Copied" for 1.6 s and its failure; Copy of a value
  fetched on the click, handing the clipboard a `ClipboardItem` before the
  value comes; a command
  line; the state marks; a toast; the date field typed and picked.
- [`ShownOnceLine.test.tsx`](../../web/src/ui/ShownOnceLine.test.tsx): a
  line valid for 7 days counting down in days and hours, then in minutes
  and seconds, then gone; axe; Copy copying the line whole.
- [`catalogues.test.ts`](../../web/src/i18n/catalogues.test.ts): the
  completeness test above.
- [`format.test.ts`](../../web/src/shell/format.test.ts): dates as
  `YYYY-MM-DD`; how long ago a time was, in either language; the time left in minutes and seconds, and in days and hours
  in either language.
- [`App.test.tsx`](../../web/src/App.test.tsx): the web UI opens on the
  frame; `/` opens at Hosts.
