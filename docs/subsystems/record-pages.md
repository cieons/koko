# The record's pages

The web UI's pages for the record: the Hosts list, with its filters,
sorting and columns. They sit in the [web UI shell](web-shell.md); what
the operations do, and the filter terms and sort keys they take, is in
[the record](record.md). The rules are P0 spec: the record, "The web UI's
Host and Service pages".

## The Hosts list

[`HostsPage`](../../web/src/pages/host/HostsPage.tsx), at `/hosts`, the
first page the header links, so `/` goes to it. Anyone sees it: it lists
what `host.list` answers the caller, so a Member sees the Hosts of their
Spaces alone, as `koko host ls` shows them. Each Host's name links to
`/hosts/<name>`, or `/hosts/<id>` for a Retired Host, since a new Host may
hold its name ([`hostPath`](../../web/src/pages/host/HostsPage.tsx)): the
path of the Host's own page, which #74 adds; until then the link finds no
page there.

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
  values, as stored, never translated.

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
- [`reldate.test.ts`](../../web/src/pages/host/reldate.test.ts): every case
  of the CLI's relative-date table.
