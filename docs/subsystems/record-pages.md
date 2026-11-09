# The record's pages

The web UI's pages for the record: the Hosts list, with its filters,
sorting and columns, a Host's page, the Services list and a Service's page,
with the actions on each. They sit in the [web UI shell](web-shell.md); what
the operations do, and the filter terms and sort keys they take, is in
[the record](record.md). The rules are P0 spec: the record, "The web UI's
Host and Service pages"; the layouts are Astryx's patterns (spec #108: "The
web UI on Astryx"): `table-filter` for the lists, `detail-page` for a Host
and a Service. Each page is a chunk of its own, which loads the first time
the web UI shows it ([`lazyPage`](web-shell.md#pages-and-routing)).

## The Hosts list

[`HostsPage`](../../web/src/pages/host/HostsPage.tsx), at `/hosts`, the
first page the side navigation links, so `/` goes to it. Anyone sees it: it lists
what `host.list` answers the caller, so a Member sees the Hosts of their
Spaces alone, as `koko host ls` shows them. It is `table-filter`'s frame
([`ListLayout`](../../web/src/pages/host/ListParts.tsx)): a header with the
title, the count of Hosts listed and Add Host; a toolbar with the filter,
"Show Retired Hosts" and the column options; the table; and the Host
selected in a resizable pane beside it.

- **Selecting a Host.** A click on a row, or on the Host's name, selects
  it: the detail pane shows what `host.show` answers (its fields,
  Attributes, Note and Services, as on its page) and "Open page", which
  opens `/hosts/<name>`, or `/hosts/<id>` for a Retired Host, since a new
  Host may hold its name ([`hostPath`](../../web/src/pages/host/record.ts)).
  The name is a link to that page, so a modified click opens it in a new
  tab. Below 768 pixels the pane is a bottom sheet.
- **Add Host** opens a dialog, a bottom sheet on a phone, of the fields
  `host add` takes: `name`, `space` (the caller's Personal Space unless
  they choose another of the Spaces `space.list` answers), `address`,
  `port`, `account` and `relay`. It sends only those given and never asks,
  and the page moves to the new Host's page, where its Attributes and Note
  are set. Closing the dialog gives the focus back to Add Host.

## The view in the URL

The page's whole view is its URL's query, so a bookmark is a saved view
and Back undoes a change ([`readView` and `writeView`](../../web/src/pages/host/view.ts)).
The redesign kept its form, so a bookmark from before opens the same view:

| Parameter | Holds | As |
| --- | --- | --- |
| `filter`, once per term | a filter term | the CLI writes it: `expires_at.le=+30d`, `region.lacks` |
| `sort`, once per key, in turn | a sort key | `--sort` takes it: `price`, `price.desc` |
| `columns` | the columns after the name, in order, separated by commas | keys; left out for the default columns |
| `all=1` | the Retired Hosts too | `--all` |

Before any call the page reads each term, sort key and column as the CLI
does ([`readQuery`](../../web/src/pages/host/view.ts)): a field of
`api.HostListFields` (carried as `hostFields`, since the Go map is not
generated) or an Attribute key, of its Preset's type from `attr.list`, or
text. A URL holding one the list does not take, such as a hand-edited
`note=x`, a key sorted by twice, or a column that is the name or shown
twice, says which, in an error `Banner` in place of the table, and lists
nothing, with a link back to the default view.

## The filter

The filter is Astryx's `PowerSearch`, a bar of tokens
([`FilterBar`](../../web/src/pages/host/ListParts.tsx)), each token one of
the view's terms ([`filter.tsx`](../../web/src/pages/host/filter.tsx)):

- **Its config** holds every field, every Attribute key `attr.list`
  answers (a Preset, or any other key in use, the list `koko attr ls`
  prints), and any other key a term in the URL names, grouped so, each
  labelled by its key. Each offers only the comparisons its type takes:
  `=`, `!=`, `has` and `lacks` for every type, and `<`, `<=`, `>`, `>=`
  also for numbers and dates.
- **A token is a term.** Its field is the term's key and its operator the
  comparison (`eq` … `lacks`); its value is the term's in PowerSearch's
  form for the key's type: text a string, a number a float (no less than
  its `min`), an enum's value one of its list in declared order, and a
  date the CLI's own text, `2026-12-01`, `today` or `+30d`, edited in a
  field that says the date it resolves to today, or that it is none. `has`
  and `lacks` take none, and their token is added at once.
- **Into the URL.** A token added or changed is written into the URL as
  the CLI writes it (`termOf`); one handed back unchanged keeps its term as
  written, so `price.gt=10.50` stays so. A value the key does not take,
  such as a date `tomorrow` or a number with seven decimals, is refused,
  saying which term, and the URL kept. Terms are ANDed. A token shows its
  term in words, such as `price: > 10`, and a relative date the date it
  stands for today, `expires_at: <= +30d (2026-12-09)`; only the URL holds
  the CLI's form.
- **On a phone** PowerSearch presents itself as a bottom sheet
  (`presentation="adaptive"`, on a touch screen narrower than 768 pixels):
  a Filters sheet lists the tokens, and Add filter, a field, its
  comparison and its value each take a step of their own.

While a term `state=retired` is there, the page asks for the Retired Hosts
too, since without `all` it would match nothing, and "Show Retired Hosts"
shows on and cannot be turned off, saying why.

## Relative dates

A relative date stays as written in the URL and is resolved, in the
browser's time zone, each time the page builds its request
([`resolveDate`](../../web/src/pages/host/reldate.ts)), so a bookmarked
"expires within 30 days" view stays relative. Its rules are the CLI's
([the record](record.md#relative-dates)), and its test runs the CLI's own
table of cases.

## Sorting and columns

- **Sorting.** The headers are Astryx's sortable headers
  ([`RecordTable`](../../web/src/pages/host/ListParts.tsx)). A click on a
  header sorts by that key alone, or turns it round when the list is sorted
  by it alone; a Shift-click adds it as a further key, or turns it round
  where it already is, like repeated `--sort`
  ([`sortBy`](../../web/src/pages/host/view.ts)). A header shows the
  direction, and its rank when the list sorts by more than one key. Without
  a sort the Hosts come by name, as the koko server orders them.
- **Columns.** The name always comes first, and stays in view as the table
  scrolls sideways in its own box. By default: `space`, `state`,
  `address`, `provider`, `region`, `price` with its `currency`, and
  `expires_at`. The column options, a popover beside their button or a
  bottom sheet on a phone, list the columns shown and those not, each field
  and Attribute key `attr.list` answers, with a button that shows or hides
  each; a column shown goes last, and a column hidden no longer sorts the
  list. Headers are the keys, and cells the values, as stored, never
  translated; `state` shows in a `Badge` of its hue, and a `relay` cell is
  the Relay's name, or "A Relay you cannot see" for a Relay in a Space the
  caller cannot act in (ADR 0035).
- **Shared with the Services list.** The frame, the filter, the table with
  its sortable headers and selection, and the column options are
  [`ListParts`](../../web/src/pages/host/ListParts.tsx), written over a
  list's view; `readQuery` takes the list's
  [`Columns`](../../web/src/pages/host/view.ts): those it always shows
  first, which no `columns=` may name, and those it shows but neither
  filters nor sorts on.

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

## The Services list

[`ServicesPage`](../../web/src/pages/service/ServicesPage.tsx), at
`/services`, lists what `service.list` answers the caller, as `koko service
ls` does: never a Retired Host's Services. Its frame, view, filter,
sorting, column options and detail pane are the Hosts list's, over
`service.list`'s keys (`id`, `name` and `host`, all text, and the Attribute
keys), with no Retired records and so no `all`. The Host and the name always
come first: the Host links to its page, and the name selects the Service,
whose pane shows what `service.show` answers and opens its page. By default
`ports` follows, which the list shows but neither filters nor sorts on, so
its header is no button and a URL sorting by it says so.

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

- [`hosts.test.tsx`](../../web/src/pages/host/hosts.test.tsx): a Member's
  Hosts in the default columns and their links; a term of each type added
  as a token into the URL and back from it, its relative date kept as
  written and resolved against a fixed clock in a fixed time zone, on a
  later day too, and a token removed; a term as written, `price.gt=10.50`,
  kept so beside a token added; the comparisons each key offers and
  an enum's values; a date and a number the key does not take refused; a
  click and a Shift-click on the headers; the column options, a hidden
  column dropping its sort key; the Retired Hosts only when asked for,
  opened by id, and asked for while a `state=retired` term is there; a URL
  the list does not take; a bookmark from before the redesign; the `relay`
  column; the detail pane and Open page; the pane, the column options and
  Add Host as bottom sheets on a phone; a Host added with only the fields
  given, in the caller's Personal Space unless another is chosen; and Add
  Host's Cancel giving the focus back.
- [`filter.test.tsx`](../../web/src/pages/host/filter.test.tsx): every
  comparison of every type round-trips between a token and the URL's term,
  relative dates among them; each type's token value; the config's keys,
  groups and comparisons; a relative date's token showing its resolved date.
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
- [`services.test.tsx`](../../web/src/pages/service/services.test.tsx):
  the Services list's default columns and links; a token, a sort and a
  column shown in the URL, the bookmark reopened, and a column hidden; a URL it does not take; the
  detail pane; a Service's page; its edit dialog sending only what changed
  and clearing its ports, its Note set; rename never asking; delete asking first; a
  Service on a Retired Host read-only; an unknown Service.
- [`reldate.test.ts`](../../web/src/pages/host/reldate.test.ts): every
  case of the CLI's relative-date table.
