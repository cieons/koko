# The record's pages

The web UI's pages for the record: the Hosts list, with its filters,
sorting and columns, a Host's page, the Services list and a Service's page,
with the actions on each. They sit in the [web UI shell](web-shell.md); what
the operations do, and the filter terms and sort keys they take, is in
[the record](record.md). The rules are P0 spec: the record, "The web UI's
Host and Service pages".

## The Hosts list

[`HostsPage`](../../web/src/pages/host/HostsPage.tsx), at `/hosts`, the
first page the header links, so `/` goes to it. Anyone sees it: it lists
what `host.list` answers the caller, so a Member sees the Hosts of their
Spaces alone, as `koko host ls` shows them. Each Host's name links to
`/hosts/<name>`, or `/hosts/<id>` for a Retired Host, since a new Host may
hold its name ([`hostPath`](../../web/src/pages/host/record.ts)).
"Add a Host" opens a form of the fields `host add` takes: `name`, `space`
(the caller's Personal Space unless they choose another of the Spaces
`space.list` answers), `address`, `port`, `account` and `relay`. It sends
only those given and never asks, and the page moves to the new Host's page,
where its Attributes and Note are set.

## The view in the URL

The page's whole view is its URL's query, so a bookmark is a saved view
and Back undoes a change ([`readView` and `writeView`](../../web/src/pages/host/view.ts)):

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
twice, says which and lists nothing, with a link back to the default view.

## Building a filter

[`FilterBuilder`](../../web/src/pages/host/FilterBuilder.tsx) builds a term
in three steps:

1. **A key**: a field, a Preset, or any other key in use, as `attr.list`
   answers them, the list `koko attr ls` prints.
2. **A comparison** its type offers: `=`, `!=`, `has` and `lacks` for every
   type, and `<`, `<=`, `>`, `>=` also for numbers and dates.
3. **A value** of its type, checked as the koko server checks it before the
   term can be added: an enum's from its list in declared order, a date from
   a picker or written (`2026-12-01`, `today`, `+30d`), a number as an exact
   decimal (ADR 0029) no less than its `min`, and text on one line of at
   most 1 KiB. `has` and `lacks` take none.

Terms are ANDed. Each shows in words, such as `price > 10` or `region
lacks`, with a button that takes it away, and a relative date shows the
date it stands for today; only the URL holds the CLI's form. While a term
`state=retired` is there, the page asks for the Retired Hosts too, since
without `all` it would match nothing, and the "Show Retired Hosts" box
shows ticked and cannot be cleared.

## Relative dates

A relative date stays as written in the URL and is resolved, in the
browser's time zone, each time the page builds its request
([`resolveDate`](../../web/src/pages/host/reldate.ts)), so a bookmarked
"expires within 30 days" view stays relative. Its rules are the CLI's
([the record](record.md#relative-dates)), and its test runs the CLI's own
table of cases.

## Sorting and columns

- **Sorting.** A click on a column header sorts by that key alone, or turns
  it round when the list is sorted by it alone; a Shift-click adds it as a
  further key, or turns it round where it already is, like repeated
  `--sort`. Without a sort the Hosts come by name, as the koko server
  orders them.
- **Columns.** The name always comes first. By default: `space`, `state`,
  `address`, `provider`, `region`, `price` with its `currency`, and
  `expires_at`. The column picker shows or hides each field and each
  Attribute key `attr.list` answers; a column added goes last, and a column
  hidden no longer sorts the list. Headers are the keys, and cells the
  values, as stored, never translated; a `relay` cell is the Relay's name,
  or "(one you cannot see)" for a Relay in a Space the caller cannot act in
  (ADR 0035).
- **Shared with the Services list.** The terms in words, the column picker
  and the sortable headers are
  [`ListControls`](../../web/src/pages/host/ListControls.tsx), written over a
  list's view; `readQuery` takes the list's
  [`Columns`](../../web/src/pages/host/view.ts): those it always shows
  first, which no `columns=` may name, and those it shows but neither
  filters nor sorts on.

## A Host's page

[`HostPage`](../../web/src/pages/host/HostPage.tsx), at `/hosts/<host>`,
the Host's name or its id, shows what `host.show` answers the caller, as
`koko host show` does: the fields by key (`relay` linking to the Relay's
page by its id, or "(one you cannot see)"; `host_key` the pin's SHA-256
fingerprint, or none), the Attributes by key, the Note as written, and the
Host's Services, each linking to its page. A Registered Host shows
`koko host enroll <host>` with a button that copies it, since Enroll stays
in the CLI. A Host no one can see answers `unknown_host`, as one that does
not exist.

The actions name the Host by its id, so a rename made meanwhile never sends
one to another Host. Only retire and delete ask first, in the
[confirmation dialog](web-shell.md#confirmations), saying what follows:

| Action | Asks | After |
| --- | --- | --- |
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
Service's, sends only what differs from the record as the form opened with
it ([`useEditForm`](web-shell.md#calls)), so a field someone else changed
meanwhile is never overwritten, and cannot be saved with nothing changed.
A value emptied is cleared through `unset`: `port` (back to 22), `relay`,
`ports`, an Attribute, the Note; `address` and `account` cannot be emptied.
One more Attribute can be added by key. A value is checked against its
key's type first, as the filter builder checks one, a relative date such
as `+1y` resolved in the browser's time zone, as the CLI resolves one in a
write; an enum's comes from its list. Saving never asks.

## The Services list

[`ServicesPage`](../../web/src/pages/service/ServicesPage.tsx), at
`/services`, lists what `service.list` answers the caller, as `koko service
ls` does: never a Retired Host's Services. Its view, filter builder,
sorting and column picker are the Hosts list's, over `service.list`'s keys
(`id`, `name` and `host`, all text, and the Attribute keys), with no
Retired records and so no `all`. The Host and the name always come first,
linking to their pages; by default `ports` follows, which the list shows
but neither filters nor sorts on, so its header is no button and a URL
sorting by it says so.

## A Service's page

[`ServicePage`](../../web/src/pages/service/ServicePage.tsx), at
`/services/<host>/<service>`, the Host by the name or id the Service names
it by, shows what `service.show` answers: its fields, Attributes and Note.
It edits the ports, Attributes and Note and renames the Service, which never
ask; the page then reloads, or moves to the new name. Delete asks first and
goes back to the Host's page, replacing its history entry. A Service whose Host is Retired names it by its
id, which holds a `_` no Host name holds, and its page offers no action.

## Verifying

- [`hosts.test.tsx`](../../web/src/pages/host/hosts.test.tsx): a Member's
  Hosts in the default columns and their links; a filter built in three
  steps into the URL and back from it, its relative date kept as written and
  resolved against a fixed clock in a fixed time zone, on a later day too;
  the comparisons each type offers, an enum's values, a date from the picker
  and a value of the wrong type refused; a click and a Shift-click on the
  headers; the column picker, a hidden column dropping its sort key; the
  Retired Hosts only when asked for, linked by id, and asked for while a
  `state=retired` term is there; a URL the list does not take.
  The `relay` column, and a Host added with only the fields given, in the
  caller's Personal Space unless another is chosen.
- [`host.test.tsx`](../../web/src/pages/host/host.test.tsx): a Host's
  page with its fields, Attributes, Note, Services and fingerprint; a
  Registered Host's `koko host enroll` line copied, an unseen Relay and no
  pin; an unknown Host; the edit form sending only what changed, a relative
  date resolved, a wrong value and an unchanged form refused, the port,
  Relay and an enum cleared; rename and move never asking; retire and delete
  asking with their consequence, Cancel sending nothing; delete offered only
  with no pin; a Retired Host's page; a Service added.
- [`services.test.tsx`](../../web/src/pages/service/services.test.tsx):
  the Services list's default columns, links, keys, a filter, a sort and a
  column in the URL, and a URL it does not take; a Service's page; its edit
  form sending only what changed and clearing its ports; rename never
  asking; delete asking first; a Service on a Retired Host read-only.
- [`reldate.test.ts`](../../web/src/pages/host/reldate.test.ts): every case
  of the CLI's relative-date table.
