# The record's pages

The web UI's pages for the record: the Hosts page, with its Services view,
its search, Filter, sorting and columns, and Add Host; a Host's page, with
a Service's panel over it, and the actions on each. They sit in the [web UI
shell](web-shell.md); what the operations do, and the filter terms and sort
keys they take, is in [the record](record.md). The rules are P0 spec: the
record, "The web UI's Host and Service pages"; the layout is [the web UI's
design](../web-design.md) ("Hosts", "The Services view", "Add Host and
Edit", "A Host", "A Service"). Each page is a chunk of its own, which loads the first time the web
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
  "All, with Retired"); **Filter**; in the Hosts view **Group by**
  ([below](#group-by)); from 768 pixels wide, the columns button; in the
  Hosts view **Select** ([the Select mode](#the-select-mode)); and
  **Reset view** ([the view remembered](#the-view-in-the-url)).
  The view's terms that no select shows are chips under them, each with a
  button, "Remove the filter <term>", that takes it away.
- **The list** ([`HostsList`](../../web/src/pages/host/HostsList.tsx)) is
  what `host.list` answers for the view, with `with_note`, the search
  applied: a heading per Space (its name, its count, "Personal Space" for
  one), in the order of the Spaces' names, over one panel each. Within a
  Space the Hosts keep `host.list`'s order: the view's sort, or else by
  name. With [Group by](#group-by), a Space's Hosts are under a
  subheading per value instead, a panel each. A panel is a table
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

## Group by

**Group by**, in the Hosts view alone, is a searchable choice of "None",
the default, then every Attribute key `attr.list` answers, each by its
key, as Filter offers them; the field shows "Group by" before the key
chosen. With a key, each Space's section holds one subheading per value of
it, as stored, with its count, over a panel of its Hosts, and a last one,
"No <key>", for the Hosts without it
([`groupBy`](../../web/src/pages/host/group.ts)). The subheadings come in
the order `host.list` sorts the values in, by the key's type
([`compareValues`](../../web/src/pages/host/group.ts), the koko server's
`record.Type.Compare`): numbers by value, dates by the calendar, an enum's
values in their declared order and text in natural order. Only the Hosts
the search and the terms leave are grouped, so a value none of them holds
has no subheading, and within a subheading the Hosts keep the list's sort.
A panel's table, or its list on a phone, is named by its Space and value
("ops, node").

The Services view lists its Services on one panel, not by Space, so it
has no Group by, and choosing it drops the Hosts view's.

## The Select mode

**Select**, in the Hosts view's controls, starts the Select mode (#134,
[the design](../web-design.md#the-select-mode)), in which several Hosts
are ticked and one action is taken on them all. The page's part is
[`useHostSelectMode`](../../web/src/pages/host/HostBatch.tsx); what any
list page shares is in [`web/src/pages/batch/`](../../web/src/pages/batch/):
the mode and its selection
([`useSelectMode`](../../web/src/pages/batch/useSelectMode.tsx),
[`selection.ts`](../../web/src/pages/batch/selection.ts)), the bar
([`BatchBar`](../../web/src/pages/batch/BatchBar.tsx)) and the dialog
([`BatchDialog`](../../web/src/pages/batch/BatchDialog.tsx)).

- **In the mode** a row is no link: it has a checkbox, and a click or tap
  anywhere on it toggles it. A Shift-click ticks, or unticks, every row
  from the one toggled last to it, in the order the page shows them. Each
  table's header has a checkbox for its rows, named "Select the 4 Hosts of
  ops"; with Group by the Space's heading has one for the whole Space, and
  on a phone, which has no table, each heading and subheading has one.
- **The bar** at the foot is there from the start of the mode: "3
  selected", how many of them the view does not show ("1 not in this
  view"), **Select all N** ("Select all N matching" while the search or a
  term narrows the view) until every row shown is ticked, the actions,
  unavailable until a Host is ticked, and **Done**, which leaves the mode
  and clears the selection, as pressing Select again does.
- **The selection is kept** while the search, Filter, Group by, the sort
  and the columns change and while the list reloads, by the Hosts' ids,
  so it can be gathered across several searches. It holds any Host
  `host.list` answers with `all`, a Retired one too; a Host that is gone
  has left it. The Services view shows neither the button nor the bar.
- **An action** opens one dialog for the whole batch, whose title and
  button name the action and the count ("Retire 12 Hosts") and which lists
  the Hosts. Applied, it is one call of the operation's batch form, all or
  none ([the API](api.md#batches)): a notice says what was done, every
  read of the page loads again, the mode ends and the selection clears.

  | Action | Asks | Leaves out, saying so | Call |
  | --- | --- | --- | --- |
  | Move | the Space, of those that do not hold every Host selected | the Hosts in that Space already | `host.move` with `hosts`, `space` |
  | Attribute: set | the key, which may be a new one, and its value, in the field of its type | none; it says how many have the key now | `host.set` with `hosts`, `attributes` |
  | Attribute: remove | the key, of those the Hosts selected have | the Hosts without the key | `host.set` with `hosts`, `unset` |
  | Retire | to confirm, stating what follows | the Hosts Retired already | `host.retire` with `hosts` |
  | Delete | to confirm, and for more than one their number typed | the Hosts that have been Managed (a pinned host key) | `host.remove` with `hosts` |

  With every Host selected left out, the dialog says so and offers only
  Close.
- **A refused batch** changed nothing. The dialog stays, headed "No Host
  was moved", lists each Host at fault (the refusal's `items`, by index)
  with the message of its code, and offers to send the batch again
  without them ("Move the other 17 Hosts"), or only Close when every Host
  is at fault. Cancel leaves the selection as it was. A failure that
  names no Host, as a dropped connection or a batch of one, shows in
  place, the form kept, and sending again reuses the Idempotency-Key.

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
| `group` | the Hosts view's Group by | its Attribute key; left out for none |
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
  the other view's terms, sort keys, columns and Group by. With neither to
  keep, the other view opens as the browser remembers it.

**The view remembered.** The browser keeps the last view of the Hosts
view and of the Services view, each on its own, for each User
([a list remembers its view](web-shell.md#pages-and-routing)): `/hosts`
with no query, from the top bar, a new tab, a link or Sign in, is replaced
by the Hosts view remembered, and `/hosts?view=services` alone, which
`/services` goes to, by the Services view remembered. A URL with a view
wins and is remembered in its place. **Reset view**, at the end of the row
of controls and unavailable at the default view, goes to the view's
default and forgets the one remembered, as "Clear the search and filters"
and "Go to the default view" do.

Before any call the page reads each term, sort key and column as the CLI
does ([`readQuery`](../../web/src/pages/host/view.ts)): a field of
`api.HostListFields` (carried as `hostFields`, since the Go map is not
generated) or an Attribute key, of its Preset's type from `attr.list`, or
text. A URL holding one the list does not take, such as a hand-edited
`note=x`, a key sorted by twice, a column that is the name or shown
twice, or a Group by that is no Attribute key, such as `group=state`,
shows one panel naming each, in place of the list, and calls nothing for
it, with "Go to the default view".

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
  translated: Provider shows its region after it, or the region alone for
  a Host with no provider, a separator only between the two, Price its
  currency and billing cycle, and Address `:port` for a port other than 22, unless a
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
four groups, is what [Edit](#edit-forms) reuses without Name and Space.

## A Host's page

[`HostPage`](../../web/src/pages/host/HostPage.tsx), at `/hosts/<host>`,
the Host's name or its id, shows what `host.show` answers the caller, as
`koko host show` does, in the design's "A Host":

- **The head**: the breadcrumb, Hosts › its Space, the Space a link to
  the list narrowed to it (`/hosts?filter=space%3D<space>`); the name,
  which names the browser's tab, with its [state mark](web-shell.md#parts);
  **Edit**, and "More actions for <name>": Rename, Move to another Space,
  then Retire and Delete as danger items.
- **Reaching it**, by state: Managed, `ssh <name>` in a [command
  line](web-shell.md#parts) with Copy, and that koko logs in as the Host
  Account and keeps the Host's key; Registered, that koko cannot log in
  yet, and `koko host enroll <name>` with Copy, since Enroll stays in the
  CLI; Retired, one sentence. Under a hairline: Address, Port, Host
  Account, the Relay when it has one (its name, a link to the Relay's page
  by its id, or "A Relay you cannot see", ADR 0035) and Pinned host key
  (the pin's SHA-256 fingerprint, or "Not pinned").
- **Attributes**, each a label over its value as stored, four across:
  Provider, Region, Price (with its currency), Billing cycle, Expires,
  Renews automatically and Provider account, then every other key by
  name; with none, one sentence. An expiry is a plain Attribute.
- **Services**, a row each, its name and ports, a link that opens its
  [panel](#a-services-panel), and **Add Service**.
- **Secrets in <space>**: up to four of the Secrets `secret.list` answers
  for the Host's Space, those whose name holds the Host's name first, each
  a compact [`SecretRow`](../../web/src/pages/secret/SecretRow.tsx), with
  Copy, Show and its menu as on [the Secrets
  page](secrets.md#the-web-uis-secrets-page); "All N", when there are
  more, links to `/secrets?space=<space>`.
- **Note**, as written, then "Recorded <date>, changed <ago>" and the id.

On a phone the panels stack, Reaching it first. A Host no one can see
answers `unknown_host`, and the page says no Host the caller can see has
that name or id, with a link to Hosts.

The actions name the Host by its id, so a rename made meanwhile never sends
one to another Host. Rename and Move are [short
forms](web-shell.md#dialogs); Retire and Delete ask first, in the
[confirmation dialog](web-shell.md#confirmations), saying what follows; a
refusal, such as `relay_in_use` or `host_was_managed`, shows in place. A
write that succeeded says so in a toast.

| Action | Asks | After |
| --- | --- | --- |
| Edit (`host.set`), [below](#edit-forms) | no | the page loads again |
| Rename (`host.rename`): the new name, checked by the name rule | no | the page moves to the new name, replacing its history entry |
| Move to another Space (`host.move`): the Space, a searchable choice of the others `space.list` answers | no | the page loads again |
| Add Service (`service.add`), [below](#a-services-panel) | no | the new Service's panel opens |
| Retire (`host.retire`): it leaves the list and frees its name, its record, Services and pin kept under its id | yes | the page moves to `/hosts/<id>`, replacing its history entry |
| Delete (`host.remove`), offered only with no pin: it goes with its Attributes, Note and Services, the count given | yes | the page goes back to `/hosts`, replacing its history entry |

A Retired Host's page offers Edit, of its Attributes and Note alone, Move,
and Delete when it has no pin; Rename and Retire are disabled, there is no
Add Service, and its Services' panels are read-only, as the koko server
refuses any other change with `host_retired`.

## Edit forms

[`EditHost`](../../web/src/pages/host/HostForm.tsx), which loads with its
first opening, is the Host form in its wide side panel, but Name and Space,
which Rename and Move change. It opens with the Host as `host.show`
answered it ([`formValuesOf`](../../web/src/pages/host/HostForm.tsx)) and
sends one `host.set`, naming the Host by its id, with only what differs
from that ([`hostChanges`](../../web/src/pages/host/HostForm.tsx)), so a
field someone else changed meanwhile is never overwritten. A value emptied
is cleared through `unset`: the port (back to 22), the Relay (None), an
Attribute, an enum's value (None, its first option), and the Note; Address
and Host Account cannot be emptied. Renews automatically is a switch: on
sends `yes`, and off, once it was on, `no`. What the Host form and a
Service's panel share of the Attribute rows and of what an edit sends of
them is [`AttributeRows`](../../web/src/pages/host/AttributeRows.tsx). The
Relay is a searchable choice of the Managed Hosts reached directly but this
one, with the Host's own Relay, a Relay it cannot see among them. Each
value is checked against its key's type when the form is submitted, a
relative date such as `+1y` resolved in the browser's time zone as the CLI
resolves one in a write; nothing is sent while one is at fault. With
nothing changed, Save closes the panel and sends nothing. A refusal shows
in the panel, the form kept as typed. A Retired Host's form shows how koko
reaches it disabled, takes its Attributes and Note alone, and says so.

## A Service's panel

[`OpenService`](../../web/src/pages/host/ServicePanel.tsx), at
`/hosts/<host>/services/<service>`, a side panel over the Host's page, the
Host by the name or id the Service names it by, shows what `service.show`
answers, naming the Host by its id: Name, Ports (separated by commas; a
bare port is TCP), the Attributes as key and value rows, as the Host
form's, and the Note. **Save** checks each field (the name rule, each port
as `record.ParseServicePort` takes one, each value against its key's type)
and sends one `service.set` for the ports, Attributes and Note that
changed, with `unset` for those emptied, then `service.rename` when the
name changed. If the rename is refused, the panel says that the other
changes are saved under the old name and shows the refusal in place; Save
then sends the rename alone. With nothing changed, Save closes the panel
and sends nothing. **Delete**, at the foot's start, asks first,
and the panel closes. Closing the panel goes back to the Host's page,
replacing the history entry; the page loads again after a write.

[`AddService`](../../web/src/pages/host/ServicePanel.tsx), from the Host's
**Add Service**, is the same panel, empty, its Name focused. It sends one
`service.add` by the Host's id with only what is given, then the new
Service's panel opens, giving the focus back to Add Service when it closes.

A Service whose Host is Retired is reached through the Host's id; its
panel shows its ports, Attributes and Note, says they cannot be changed,
and offers no action. A Service the Host has not shows `unknown_service`
in the panel, with Try again.

`/services/<host>/<service>`, where a released koko showed a Service, goes
to `/hosts/<host>/services/<service>`, keeping its query and replacing the
history entry ([`routes.ts`](../../web/src/pages/host/routes.ts)).

## The Services view

[`ServicesList`](../../web/src/pages/host/ServicesList.tsx), the Services
tab of the Hosts page (`/hosts?view=services`), lists what `service.list`
answers the caller for the view, as `koko service ls` does: never a Retired
Host's Services. One panel, a table named "Services": the name, a link that
covers its row and opens the Service on its Host's page,
`/hosts/<host>/services/<service>`; the Host, a link of its own
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

## Verifying

Each test file holds a test for each action of
[the page inventory](web-shell.md#the-page-inventory) on its pages, and runs
axe on each page, dialog and pane state it renders.

- [`hosts.test.tsx`](../../web/src/pages/host/hosts.test.tsx): the Hosts
  grouped by Space in the default columns, their links and the head's
  counts; Provider and region with only one of the two; a Member's, with
  no Space select; 0, 3 and 300 Hosts, and every Host Retired; the search
  over each value it matches, replacing the history entry, and no match
  with its way back; each option of the state select in the URL, a Retired
  Host opened by its id, a state term it cannot show as a chip, and a
  bookmarked `state=retired` asking for all; the Space select's term, one
  it cannot show; a click and a Shift-click on the headers; the columns
  button, a column hidden dropping its sort key, and the `relay` column; a
  URL the list does not take; a bookmark from before the redesign; the
  `/services` redirect; a phone's rows.
- [`groupby.test.tsx`](../../web/src/pages/host/groupby.test.tsx): Group by
  chosen into the URL, each Host under its value within its Space and
  those without it last, keeping the sort; a subheading the search or a
  term empties hidden; a number's and an enum's subheadings by their type
  and a date's by the calendar; the choice remembered and cleared by Reset
  view; the Services tab dropping it; a field refused; a phone's lists.
  [`group.test.ts`](../../web/src/pages/host/group.test.ts) holds the order
  of each type, natural text order as the koko server's.
- [`select.test.tsx`](../../web/src/pages/host/select.test.tsx): the
  Select mode entered and left, by Done and by the button; a row, a
  Shift-click's range, a table's header and, with Group by, a Space's
  heading ticked; Select all and Select all matching; the selection kept
  across a search and a state term, with the count of those not shown, and
  sent whole; each action's dialog with its count, its list, what it
  leaves out and its one call, the Attribute's value refused when its key
  does not take it, and Delete's typed count; a refused batch naming the
  Hosts at fault, Cancel keeping the selection, and the rest sent; a batch
  with every Host at fault, and a refusal that names none; a phone's rows
  and headings.
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
- [`host.test.tsx`](../../web/src/pages/host/host.test.tsx): a Managed
  Host's page with its breadcrumb, ssh line copied, facts, pin, Attributes
  in order, Services, the Secrets of its Space (four, the Host's first,
  and All N), Note and when it was recorded; a Secret copied and shown
  there; a Registered Host's `koko host enroll` copied, a Relay unseen, no
  pin and Delete offered; a Relay linked by its id, and no Delete with a
  pin; an unknown Host; Edit sending only what changed, a relative date
  resolved, and the port, the Relay and an enum cleared through `unset`;
  Edit refusing wrong values before the call, sending nothing unchanged,
  and a refusal in place; Rename, Move, Retire with `relay_in_use` and
  Delete with `host_was_managed`, each by the Host's id, the confirmations'
  Cancel sending nothing and giving the focus back; a Retired Host's page
  with Rename and Retire disabled, no Add Service, its Edit, Move and
  Delete; Renews automatically switched off and on; the page and the
  Space's Secrets failing to load, with Try again; a phone's page and Edit
  from the bottom; Escape closing Edit.
- [`servicepanel.test.tsx`](../../web/src/pages/host/servicepanel.test.tsx):
  a Service's panel opened from its row, and Escape giving the focus back
  to it; Save as one `service.set` then `service.rename`; the rename
  refused, saying what was saved, and sent alone after; the ports and an
  Attribute cleared, and nothing sent with nothing changed; a wrong port or
  name refused before any call; Delete asking first; Add Service as one
  `service.add` opening the new Service's panel; a Retired Host's Service
  read-only; an unknown Service; the `/services/<host>/<service>`
  redirect.
- [`reldate.test.ts`](../../web/src/pages/host/reldate.test.ts): every
  case of the CLI's relative-date table.
