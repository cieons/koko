# The record's pages

> **A Host and a Service come back in #124.** The move to HeroUI (ADR
> 0046) took these pages out. The Hosts page, with its Services view and
> Add Host, is back (#122); the sections from "A Host's page" to "A
> Service's page" still describe the Astryx pages 6841eb2 held, whose
> behaviour rules stand for #124, on [the web UI's
> design](../web-design.md)'s layout. Their links to files and tests name
> what 6841eb2 held.

The web UI's pages for the record: the Hosts page, with its Services view,
its search, Filter, sorting and columns, and Add Host; a Host's page and a
Service's, with the actions on each. They sit in the [web UI
shell](web-shell.md); what the operations do, and the filter terms and sort
keys they take, is in [the record](record.md). The rules are P0 spec: the
record, "The web UI's Host and Service pages"; the layout is [the web UI's
design](../web-design.md) ("Hosts", "The Services view", "Add Host and
Edit"). Each page is a chunk of its own, which loads the first time the web
UI shows it ([`lazyPage`](web-shell.md#pages-and-routing)).

## The Hosts list

[`HostsPage`](../../web/src/pages/host/HostsPage.tsx), at `/hosts`, the
first place of the top bar, so `/` goes to it. Anyone sees it: it lists
what `host.list` answers the caller, so a Member sees the Hosts of their
Spaces alone, as `koko host ls` shows them.

- **The head**: "Hosts", the line "9 Hosts in 3 Spaces, running 12
  Services." counted from `host.list` with `all` and `service.list` with
  no term (the Hosts in use, the Spaces they are in, every Service), and
  **Add Host**. With no Host recorded, Retired ones included, and nothing
  in the URL, the page is one panel saying so, with Add Host.
- **The controls**, in one row: the Hosts and Services tabs with their
  counts; the search; the Space select, a searchable choice with "All
  Spaces" first, shown when `space.list` answers two Spaces or more; in
  the Hosts view the state select (In use, Managed, Registered, Retired,
  "All, with Retired"); **Filter**; and, from 768 pixels wide, the columns
  button. The view's terms that no select shows are chips under them, each
  with a button, "Remove the filter <term>", that takes it away.
- **The list** ([`HostsList`](../../web/src/pages/host/HostsList.tsx)) is
  what `host.list` answers for the view, with `with_note`, the search
  applied: a heading per Space (its name, its count, "Personal Space" for
  one), in the order of the Spaces' names, over one panel each. Within a
  Space the Hosts keep `host.list`'s order: the view's sort, or else by
  name. A panel is a table
  ([`RecordTable`](../../web/src/pages/host/RecordTable.tsx)), named by its
  Space, whose first cell is the Host's name, a link that covers the row,
  so the row is one Tab stop, with its Services' names after it. A Host
  opens at `/hosts/<name>`, or `/hosts/<id>` when it is Retired, since a
  new Host may hold its name ([`hostPath`](../../web/src/pages/host/record.ts)).
  On a phone a Host is one link of three lines: its name and state, its
  address, and its provider, region and expiry.
- **The search**, `q` in the URL, is applied in the browser to what the
  list answered ([`search.ts`](../../web/src/pages/host/search.ts)): a Host
  matches when its name, address, Space, Host Account, Note, an Attribute's
  value, or one of its Services' names or ports holds what is searched for,
  whatever its case. Typing replaces the history entry. While the search or
  a term narrows the list, a line announced politely says how many it left
  ("3 of 9 Hosts"); when it leaves none, one panel says "No Host matches
  this view." with "Clear the search and filters", which goes to `/hosts`.
- **Add Host** opens the Host form ([below](#add-host)).

## The view in the URL

The page's whole view is its URL's query, so a bookmark is a saved view and
Back undoes a change. It keeps koko's grammar
([`readView` and `writeView`](../../web/src/pages/host/view.ts)), so a
bookmark from before the redesign opens the same view, and adds the page's
own parameters ([`controls.ts`](../../web/src/pages/host/controls.ts)):

| Parameter | Holds | As |
| --- | --- | --- |
| `filter`, once per term | a filter term | the CLI writes it: `expires_at.le=+30d`, `region.lacks` |
| `sort`, once per key, in turn | a sort key | `--sort` takes it: `price`, `price.desc` |
| `columns` | the columns after the fixed ones, in order, separated by commas | keys; left out for the default columns |
| `all=1` | the Retired Hosts too | `--all` |
| `q` | the search | as typed |
| `view=services` | the Services view | left out for the Hosts view |
| `space` | the Services view's Space | its name; left out for every Space |

Each control writes it as the design's table under "The URL keeps koko's
grammar" says:

- **The Space select** sets the `space` key alone: in the Hosts view a
  term `space=<name>`, every other `space` term going; in the Services
  view `space=<name>`, since `service.list` takes no `space` key, applied in
  the browser by each Service's Host's Space; "All Spaces" writes neither.
- **The state select** sets the `state` key alone: In use, no `state` term
  and no `all`; Managed or Registered, a term of that value; Retired, the
  term `state=retired` and `all=1`; "All, with Retired", `all=1` and no
  term.
- A select shows the one term of its key built in Filter, `key=<value>`
  of a value it offers; a term it cannot show, another comparison or a
  second term of the key, leaves it on its first option and is a chip.
- While a term `state=retired` is there, the page asks for the Retired
  Hosts too, since without `all` it would match nothing.
- **The tabs**: choosing the other view keeps the search and the Space (a
  `space` term in the Hosts view, `space=` in the Services view) and drops
  the other view's terms, sort keys and columns.

Before any call the page reads each term, sort key and column as the CLI
does ([`readQuery`](../../web/src/pages/host/view.ts)): a field of
`api.HostListFields` (carried as `hostFields`, since the Go map is not
generated) or an Attribute key, of its Preset's type from `attr.list`, or
text. A URL holding one the list does not take, such as a hand-edited
`note=x`, a key sorted by twice, or a column that is the name or shown
twice, shows one panel naming each, in place of the list, and calls
nothing for it, with "Go to the default view".

## The filter

**Filter** ([`Filter.tsx`](../../web/src/pages/host/Filter.tsx)) opens a
popover that builds one term:

- **Field or Attribute**, a searchable choice of the list's fields, then
  every Attribute key `attr.list` answers (a Preset, or any other key in
  use, the list `koko attr ls` prints), each by its key, as the URL's
  term and its chip write it.
- **Comparison**, a select of those the key's type takes: is, is not, has
  a value and has no value for every type, and is less than, is at most,
  is more than and is at least also for numbers and dates.
- **Value**, as the key's type takes it: an enum's value from its list,
  in a select, or in a searchable choice past eight values, such as
  `currency`; a date in the date field
  ([`DateValue`](../../web/src/pages/host/DateValue.tsx)), typed as the CLI
  writes one, `2026-12-01`, `today` or `+30d`, or picked in the calendar
  beside it, the field saying the date a relative one resolves to today;
  and text otherwise. Has a value and has no value take none.
- **Add filter** checks the term when the popover is submitted: a value
  the key does not take, such as a date `tomorrow` or a number with seven
  decimals, is refused under the field, naming the term
  (`expires_at=tomorrow is not a filter this list takes`), and the URL is
  kept. A term taken is added to the URL as the CLI writes it
  ([`writeTerm`](../../web/src/pages/host/view.ts)); the terms already
  there stay as written, so `price.gt=10.50` stays so. Terms are ANDed.
- **A chip** shows its term as its key, a sign and its value as written
  (`price > 10.50`), has and lacks in words (`region has no value`), and a
  relative date with the date it stands for today
  (`expires_at ≤ +30d (2026-12-09)`); only the URL holds the CLI's form.

## Relative dates

A relative date stays as written in the URL and is resolved, in the
browser's time zone, each time the page builds its request
([`resolveDate`](../../web/src/pages/host/reldate.ts)), so a bookmarked
"expires within 30 days" view stays relative. Its rules are the CLI's
([the record](record.md#relative-dates)), and its test runs the CLI's own
table of cases. The Host form resolves one the same way before it sends it,
as the CLI resolves one in a write.

## Sorting and columns

- **Sorting.** A header of a column the list sorts on is a button. A click
  sorts by that key alone, or turns it round when the list is sorted by it
  alone; a Shift-click adds it as a further key, or turns it round where it
  already is, like repeated `--sort`
  ([`sortBy`](../../web/src/pages/host/view.ts)). The first key's header
  carries `aria-sort`; a header shows its direction, and its rank when the
  list sorts by more than one key, which its button's description then
  says ("Sort key 2, ascending"). Without a sort the Hosts come by name, as the koko
  server orders them.
- **Columns.** The name always comes first. By default: `provider`,
  `address`, `price`, `expires_at` and `state`. The columns button, a
  popover, ticks which show among the list's fields and every Attribute key
  `attr.list` answers; a column ticked goes last, and one unticked no
  longer sorts the list. Headers are the names of fields and Presets in the
  language, and any other key as stored. Cells are values as stored, never
  translated: Provider shows its region after it, Price its currency and
  billing cycle, and Address `:port` for a port other than 22, unless a
  column of their own shows them; `state` shows its state mark; a `relay`
  cell is the Relay's name, or "A Relay you cannot see" for a Relay in a
  Space the caller cannot act in (ADR 0035).
- **Shared with the Services view**: the table, its headers, the columns
  button, Filter and the panel for a URL the list does not take,
  written over a list's view; `readQuery` takes the list's
  [`Columns`](../../web/src/pages/host/view.ts): those it always shows
  first, which no `columns=` may name, and those it shows but neither
  filters nor sorts on.

## Add Host

[`AddHost`](../../web/src/pages/host/HostForm.tsx), a module that loads
with its first opening, with the calendar, is the Host form in a wide side
panel, from the bottom on a phone, in the design's four groups:

1. **The Host**: Name (the SSH Name), Space, a searchable choice of the
   Spaces `space.list` answers, the caller's Personal Space chosen, and
   Address.
2. **How koko reaches it**: Host Account, `root` given; Port, `22` given;
   and the Relay, a searchable choice of the Managed Hosts reached directly,
   shown when there is one.
3. **The purchase**: Provider, Region, Price, Currency (searchable),
   Billing cycle, Expires (the date field, which takes a relative date
   too), Renews automatically (a switch) and Provider account.
4. **Anything else**: rows of an Attribute key, picked among those
   `attr.list` answers or typed anew, and its value, as its key's type
   takes it; then the Note.

It checks each field when submitted (the name rule, a port, each value
against its key's type, an Attribute key's rule and that no row gives a
key given above), and sends one `host.add` with only the fields given:
`port` only when it is not 22, the Relay only when one is chosen, each
Attribute given (a relative date resolved, Renews automatically as `yes`
when on) and the Note when it holds text. Then a toast says the Host is
recorded and the page moves to it. A refusal shows in the panel, the form
kept as typed. Cancel and Escape close it, sending nothing, the focus back
on Add Host. [`HostFields`](../../web/src/pages/host/HostForm.tsx), the
four groups, is what #124's Edit reuses without Name and Space.

## A Host's page

[`HostPage`](../../web/src/pages/host/HostPage.tsx), at `/hosts/<host>`,
the Host's name or its id, shows what `host.show` answers the caller, as
`koko host show` does, on `detail-page`'s pattern:

- **The summary header**
  ([`RecordHeader`](../../web/src/pages/host/RecordView.tsx)): a link back
  to the Hosts, the name, its state in a `Badge` and its Space, and its
  actions, with a refusal under them.
- **The content:** the Services as its line items, each linking to its
  page, with Add Service; the fields by key in a `MetadataList` (`relay`
  linking to the Relay's page by its id, or "A Relay you cannot see";
  `host_key` the pin's SHA-256 fingerprint, or "Not pinned"); and the
  Attributes by key in another.
- **The rail** holds the Note as written. At 1024 pixels and below the
  rail's Note moves under the Attributes.

A Host no one can see answers `unknown_host`, as one that does not exist.

The actions name the Host by its id, so a rename made meanwhile never sends
one to another Host. Each opens one of [the shell's dialogs](web-shell.md#dialogs),
a bottom sheet on a phone, which gives the focus back to its button when it
closes. Only retire and delete
ask first, in the [confirmation dialog](web-shell.md#confirmations), saying
what follows:

| Action | Asks | After |
| --- | --- | --- |
| Enroll, for a Registered Host: shows `koko host enroll <host>` with a button that copies it, since Enroll stays in the CLI | no | nothing changes |
| Edit: `address`, `port`, `account`, `relay`, the Attributes and the Note (`host.set`) | no | the page reloads |
| Rename (`host.rename`) | no | the page moves to the new name, replacing its history entry |
| Move to another Space (`host.move`) | no | the page reloads |
| Add a Service: `name` and `ports` (`service.add`) | no | the page moves to the Service |
| Retire (`host.retire`): it leaves the list and frees its name, its record, Services and pin kept under its id | yes | the page moves to `/hosts/<id>`, replacing its history entry |
| Delete (`host.remove`), offered only with no pin: it goes with its Attributes, Note and Services, the count given | yes | the page goes back to `/hosts`, replacing its history entry |

A Retired Host's page offers Edit, of its Attributes and Note alone, Move,
and Delete when it has no pin; its Services show, read-only, as the koko
server refuses any other change with `host_retired`.

## Edit forms

[`EditRecord`](../../web/src/pages/host/RecordForm.tsx), a Host's and a
Service's, in a dialog, sends only what differs from the record as the form
opened with it ([`useEditForm`](web-shell.md#calls)), so a field someone
else changed meanwhile is never overwritten, and cannot be saved with
nothing changed. A value emptied is cleared through `unset`: `port` (back
to 22), `relay`, `ports`, an Attribute, the Note; `address` and `account`
cannot be emptied. One more Attribute can be added by key. A value is
checked against its key's type first, as the filter checks one, a relative
date such as `+1y` resolved in the browser's time zone, as the CLI resolves
one in a write; an enum's comes from its list, and its clear button clears
it. Saving never asks.

## The Services view

[`ServicesList`](../../web/src/pages/host/ServicesList.tsx), the Services
tab of the Hosts page (`/hosts?view=services`), lists what `service.list`
answers the caller for the view, as `koko service ls` does: never a Retired
Host's Services. One panel, a table named "Services": the name, a link that
covers its row and opens the Service on its Host's page,
`/hosts/<host>/services/<service>` (#124); the Host, a link of its own
above the row's that opens the Host; and the view's
columns, by default `ports`, which the list shows but neither filters nor
sorts on, so its header is no button and a URL sorting by it says so. Its
view, Filter, sorting and columns are the Hosts view's, over
`service.list`'s keys (`id`, `name` and `host`, all text, and the Attribute
keys), with no Retired records and so no `all` and no state select. The
name and the Host always come first. The Space select narrows it in the
browser, by each Service's Host's Space; the search matches a Service's
name, its Host and its ports. With no Service recorded, one panel says so
and where one is added. On a phone a Service is one link: its name, "on
<host>" and its ports.

`/services`, where a released koko listed the Services, goes to
`/hosts?view=services`, keeping its query and replacing the history entry
([`routes.ts`](../../web/src/pages/host/routes.ts)).

## A Service's page

[`ServicePage`](../../web/src/pages/service/ServicePage.tsx), at
`/services/<host>/<service>`, the Host by the name or id the Service names
it by, shows what `service.show` answers, on the Host page's pattern: a
summary header with a link back to its Host, its name and its actions; its
fields and Attributes; and its Note in the rail. It edits the ports,
Attributes and Note and renames the Service, which never ask; the page then
reloads, or moves to the new name. Delete asks first and goes back to the
Host's page, replacing its history entry. A Service whose Host is Retired
names it by its id, which holds a `_` no Host name holds, and its page says
it is read-only and offers no action.

## Verifying

Each test file holds a test for each action of
[the page inventory](web-shell.md#the-page-inventory) on its pages, and runs
axe on each page, dialog and pane state it renders.

- [`hosts.test.tsx`](../../web/src/pages/host/hosts.test.tsx): the Hosts
  grouped by Space in the default columns, their links and the head's
  counts; a Member's, with no Space select; 0, 3 and 300 Hosts, and every
  Host Retired; the search
  over each value it matches, replacing the history entry, and no match
  with its way back; each option of the state select in the URL, a Retired
  Host opened by its id, a state term it cannot show as a chip, and a
  bookmarked `state=retired` asking for all; the Space select's term, one
  it cannot show; a click and a Shift-click on the headers; the columns
  button, a column hidden dropping its sort key, and the `relay` column; a
  URL the list does not take; a bookmark from before the redesign; the
  `/services` redirect; a phone's rows.
- [`filter.test.tsx`](../../web/src/pages/host/filter.test.tsx): every
  comparison of every type built in Filter into the URL's term and back
  from it as the same chip and filter, relative dates kept as written and
  resolved against a fixed clock in a fixed time zone, on a later day too,
  and a chip's term taken away; a term as written, `price.gt=10.50`, kept
  so beside one added; the comparisons each type offers, an enum's values,
  a relative date's resolved date and a date picked in the calendar; a date
  and a number the key does not take refused, naming the term.
- [`services.test.tsx`](../../web/src/pages/host/services.test.tsx): the
  Services view's rows and their links to the Service and its Host; the tabs keeping the search and the
  Space and dropping the rest; the Space select as `space=` applied in the
  browser; the search; a term, a sort and a column into the URL and the
  bookmark reopened, the ports neither filtering nor sorting; a URL it does
  not take; no Service; a phone's rows.
- [`addhost.test.tsx`](../../web/src/pages/host/addhost.test.tsx): a Host
  added with only the fields given, in the caller's Personal Space, then
  its page; one added in another Space with its port, Relay, purchase,
  other Attributes and Note in one `host.add`, a relative date resolved;
  each field checked on submit and nothing sent, and a refusal in place
  with the form kept; Cancel giving the focus back; the panel from the
  bottom on a phone.
- [`host.test.tsx`](../../web/src/pages/host/host.test.tsx): a Host's
  page with its fields, Attributes, Services, fingerprint, actions and Note
  in the rail, and the Note under the content on a narrow screen; a
  Registered Host's Enroll copying `koko host enroll`, an unseen Relay and
  no pin; an unknown Host; the edit dialog sending only what changed, a
  relative date resolved, a wrong value and an unchanged form refused, the
  port, Relay and an enum cleared; rename and move never asking; retire and
  delete asking with their consequence, Cancel sending nothing; delete
  offered only with no pin; a Retired Host's page, its Attributes and Note
  set, moved, and deleted with no pin; a Service added; a
  dialog's Cancel giving the focus back.
- The Service's page's tests, under 6841eb2's `pages/service/services.test.tsx`:
  a Service's page; its edit dialog sending only what changed and clearing
  its ports, its Note set; rename never asking; delete asking first; a
  Service on a Retired Host read-only; an unknown Service (#124).
- [`reldate.test.ts`](../../web/src/pages/host/reldate.test.ts): every
  case of the CLI's relative-date table.
