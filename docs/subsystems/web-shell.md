# The web UI shell

The frame every page of the web UI sits in: routing, the header and who the
web UI acts as, the language and its catalogues, the API client, the
confirmation dialog, edit forms, reloading, and the notices. The pages
themselves are their capabilities'; where an area adds its pages and its
catalogue files is in [code layout](code-layout.md#where-an-area-adds-its-parts).
How the app is built, embedded and typed is [the web UI build](web-ui.md); how
a browser reaches it is [`koko web` and the web front](web-front.md).

## Pages and routing

- **A page is a path.** A [`Route`](../../web/src/shell/router.tsx) names a
  path (a `:name` segment matches any one segment, decoded, and reaches the
  page as `params.name`), the page component, the message the header links it
  under (`nav`), and whether only the Owner sees it (`ownerOnly`). The header
  links pages in the order of `routes` in
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

- **The header** shows the User and the device name, the links to the pages
  the caller may see, and the language switch.
- **It learns them from `user.current`**
  ([`api.UserCurrent`](../../internal/shared/api/user.go)), the operation
  `koko join` asks, so the web UI adds no operation of its own (ADR 0006). It
  is a read like any page's, reloaded on each visit and on focus. The shell
  renders a page only once it has answered, and pages read the answer as a
  [`Caller`](../../web/src/shell/caller.tsx) (`useCaller`).
- **Over the Unix socket** `user.current` answers the Owner with no Device
  Session, for the Local caller (ADR 0008), so the header shows the Owner's
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
- **Choosing.** The first of the browser's preferred languages
  (`navigator.languages`) that the web UI ships wins: Chinese of any region
  gives Simplified Chinese, English gives English, and none gives English.
  The header's switch overrides it, and the browser keeps the choice in
  `localStorage` (`koko.language`). `<html lang>` follows the language.
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
  shows what a write changed.
- **Writes** go through `useWrite`, one per form: submitting the same request
  again after it failed reuses its key, and a write that succeeded, or a
  different request, takes a new one. A write that succeeds while a later
  one is pending leaves the later one its key.
- **Edit forms** use [`useEditForm`](../../web/src/shell/form.ts), which
  sends only the fields whose value differs from its baseline: the record as
  the form opened with it, and after `saved()`, once a write succeeded, the
  values written. A field someone else changed meanwhile is never
  overwritten; the last writer wins per field.

## Confirmations

[`ConfirmDialog`](../../web/src/shell/ConfirmDialog.tsx) asks before an
irreversible action (retire, removing a record, revoke, removing a User) with
a title, the consequence and the action's button. It is the browser's modal
`<dialog>` (`showModal`), so the page behind takes no input, Escape cancels,
and the focus returns where it was when it closes. Its first control takes the
focus: Cancel, so Enter never acts by accident, or, with `typedName`, the
field for the name, and the action stays disabled until that name is typed
exactly: the web UI's form of `--last`, for the Owner's own last Device
Session. Reversible writes, revealing a Secret and issuing a Join Code never
ask.

## Notices

Every call's outcome reaches the shell (`observeCalls`), which shows one
notice above the page ([`notice.ts`](../../web/src/shell/notice.ts)):

| Notice | When | Ends |
| --- | --- | --- |
| koko web is not running; run it again | a call dropped twice | when a later call reaches the koko server |
| this browser's cookie is not one of this run of `koko web` | `cookie_required` | when a later call gets through, once the browser holds a new run's cookie |
| this device's Device Session was Revoked | `revoked`, or a `device.revoke` that answers with the Device Session the web UI acts through | never |

When the Device Session is Revoked elsewhere, `koko web` exits, and the page
can tell only that it stopped.

## Verifying

- [`Shell.test.tsx`](../../web/src/shell/Shell.test.tsx), Vitest and React
  Testing Library against the [fake API](web-ui.md#testing-pages), with test
  pages of their own: the header on a device and over the Unix socket; a
  write and a read retried once, with and without the same key; a form
  resubmitted with its key, a new key after success or for another request,
  and overlapping writes; an edit form sending only changed fields, its
  baseline after a save, and Save never asking; the dialog, Escape, and its
  typed-name variant; a Member seeing no Owner-only page, link or button, and
  the Owner seeing them; the language from the browser's preferences, the
  switch overriding it and remembered; an API error's message in either
  language; values shown as stored; the three notices, revoking the web UI's
  own Device Session, and a refusal ending the stopped notice; reloads on
  focus and on each visit; page state in the URL and Back; `/` going to the
  first page.
- [`catalogues.test.ts`](../../web/src/i18n/catalogues.test.ts): the
  completeness test above.
- [`format.test.ts`](../../web/src/shell/format.test.ts): dates as
  `YYYY-MM-DD`.
- [`App.test.tsx`](../../web/src/App.test.tsx): the web UI opens on its first
  page.
