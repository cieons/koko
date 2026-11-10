# The web UI's design: Obsidian on HeroUI

This is the design the Owner approved on 2026-11-10, written for the
engineers who build it. It says what every screen shows, how it behaves and
which values it uses. The decision to build on HeroUI is
[ADR 0046](adr/0046-the-web-ui-is-built-on-heroui.md).

This design governs the look, the layout and the interaction. What each
action calls and the rules it follows stay in the subsystem references:
[the web UI shell](subsystems/web-shell.md) and its page inventory,
[the record's pages](subsystems/record-pages.md),
[Secrets](subsystems/secrets.md#the-web-uis-secrets-page),
[the Spaces, People and Devices pages](subsystems/people-pages.md) and
[Sign-ins](subsystems/signin.md). Where this design and a reference
disagree on behaviour, the reference holds, and the difference is a bug in
this design.

## The preview and how to read it

The approved preview is a throwaway app, not koko's code. It is kept beside
the repository as `../koko-preview`, with its source in
`../koko-preview/koko-preview/`, while the move to HeroUI is built; the
paths below are within that source. Run `npm ci`, `npx vite build` and
`node serve.mjs` there, and it serves on `127.0.0.1:5194` under
`Content-Security-Policy: default-src 'self'`.

| In the preview | Holds |
| --- | --- |
| `src/styles.css` | Every token, the type roles and the panel: the source of truth for values |
| `src/heroui.css` | The HeroUI component styles imported, in HeroUI's order |
| `src/app/Shell.tsx` | The top bar, the account panel, the phone's bar, the page header |
| `src/ui/parts.tsx`, `src/ui/DateInput.tsx` | The shared parts listed under Components |
| `src/pages/*.tsx` | One file per screen |
| `src/i18n.ts` | Every string, English and Simplified Chinese |
| `index.html`, `vite.config.ts` | The CSP workaround's `<meta>`, the locale plugin |

Build from the preview's layout, values and strings. Do not copy these
parts of it, which are stand-ins:

- **The mock API** (`src/mock/`). Reads answer at once there, so the
  preview shows no loading or failed-load state; "States every screen has"
  below specifies them.
- **The query names of the Hosts list** (`f=key:op:value`, `cols`, `state`,
  `sort`). koko keeps its own: `filter` with the CLI's terms, `sort`,
  `columns` and `all=1`, as the list takes them today, and the Hosts
  list's "The URL keeps koko's grammar" below.
- **The preview's router** (in `src/app/core.ts`), **the flask button**
  (`src/preview/`), `src/directions/`, an earlier comparison, and every
  font in `public/fonts/` but Geist's and Geist Mono's, with their
  licences, which `package.json`'s `@fontsource` packages brought.
- **Catalogue keys that nothing uses** (the renewal and due-date strings of
  an earlier round). Carry over only the strings a screen shows.
- **Strings that disagree with koko.** The Simplified Chinese name of each
  product term is the one listed at the top of
  `web/src/i18n/zh-CN/shell.ts` (Pinned host key is 固定的主机密钥, not
  the preview's 已固定的主机密钥). A string that states what an action
  does is checked against the subsystem reference before it is carried
  over. Strings follow GLOSSARY.md and its _Avoid_ lists: where the
  preview says "this account" for the User, the build names the User or
  leaves the phrase out ("Devices", "Sign-ins").

## Principles

1. koko is a professional tool. The look is calm: one neutral page, raised
   panels, one accent. Nothing is decorated.
2. A screen leads with what its visitor came to do, and offers that action
   once.
3. Machine values (addresses, ports, keys, ids, commands, a Secret's value,
   a Join Code) are set in the code face. Names and prose are not.
4. Colour carries state only, and never alone: a mark's shape and its word
   say the same thing.
5. An expiry date is an Attribute like any other. Nothing counts down,
   warns or sums costs.
6. The server's address appears only where a person copies it, such as in
   a Password Link.

## Tokens

Values are OKLCH, on HeroUI's variable names, set once for light on `:root`
and once for dark on `.dark`. Light, Dark or System sets `class` and
`data-theme` on `<html>`.

| Token | Light | Dark | Used for |
| --- | --- | --- | --- |
| `--background` | 0.972 0.003 265 | 0.115 0.004 265 | The page |
| `--surface` | 1 0 0 | 0.165 0.006 265 | Panels |
| `--surface-secondary` | 0.972 0.003 265 | 0.2 0.007 265 | A row under the pointer |
| `--surface-tertiary` | 0.95 0.005 265 | 0.235 0.008 265 | |
| `--overlay` | 1 0 0 | 0.2 0.007 265 | Menus, popovers, dialogs, side panels |
| `--default` | 0.948 0.005 265 | 0.235 0.008 265 | Quiet fills: the current place, avatars |
| `--foreground` | 0.2 0.015 265 | 0.97 0.003 265 | Text |
| `--ink-2` | 0.36 0.016 265 | 0.87 0.006 265 | Values of second rank in a row |
| `--muted` | 0.47 0.018 265 | 0.72 0.012 265 | Labels, help, counts |
| `--accent` | 0.52 0.18 262 | 0.565 0.19 262 | The primary button, a ticked checkbox |
| `--focus` | 0.52 0.18 262 | 0.74 0.13 262 | The focus ring |
| `--link` | 0.46 0.18 262 | 0.8 0.11 262 | Links in text |
| `--line` | 0.93 0.005 265 | 0.225 0.007 265 | Hairlines between rows and sections |
| `--border` | 0.895 0.007 265 | 0.29 0.009 265 | The edge of an inset block |
| `--field-border` | 0.86 0.009 265 | 0.33 0.01 265 | The edge of a field, a select, a checkbox |
| `--ok` | 0.5 0.13 155 | 0.8 0.15 155 | The Managed mark |
| `--danger` | 0.555 0.21 26 | 0.64 0.2 26 | Danger as text |
| `--danger-fill` | as `--danger` | 0.58 0.2 26 | A danger button's fill |

- Filled buttons darken on hover (`color-mix` with 8% black). Lightening
  them drops the white label under 4.5:1 in dark.
- Dark needs two reds: no single red holds 4.5:1 both as text and under
  white text.
- `--field-shadow` in dark is `0 0 0 0 transparent inset`, never `none`:
  HeroUI composes a field's focus ring with it, and `none` voids the list.
- **The field edge is the Owner's choice** (2026-11-10): about 1.5:1 on a
  panel, under the 3:1 of WCAG 1.4.11. Do not raise it in a review.
- Radius: `--radius` 0.75rem; panels 1rem; inset blocks 0.75rem; the nav
  track, buttons and the phone's bar are full pills (HeroUI's default).
- The panel's shadow (`--sheet-shadow`) is, in light, a 1px ring at 7%, a
  1px drop and a wide soft drop; in dark, a 1px highlight inside its top
  edge at 7% white, a 1px ring at 5% white and a deep drop. Overlays use
  the stronger `--overlay-shadow`.

Measured contrast, light and dark: text on a panel 18.1 and 17.7;
`--ink-2` 10.9 and 13.0; `--muted` 6.8 and 7.8 (6.3 and 8.2 on the page);
links 7.4 and 10.2; white on the primary button 5.5 and 4.6; danger text
5.3 and 4.9; the focus ring 5.7 and 8.3.

## Type

Geist for text, Geist Mono for machine values, both variable, Latin subset,
bundled. Chinese falls through to the system's fonts (PingFang SC, Noto
Sans CJK SC, Microsoft YaHei). The root is 16px; body text is 15px at 1.5.

| Role | Size, weight, tracking | Used for |
| --- | --- | --- |
| `t-display` | 28px, 600, -0.03em | A page's title |
| `t-title` | 20px, 600, -0.02em | A dialog's or side panel's title, an empty state's line |
| `t-heading` | 14px, 600 | A panel's title, a group's name |
| `t-name` | 15px, 600, -0.01em | A record's name in a row |
| body | 15px, 400 | |
| `t-label` | 13px, muted | A fact's label, help, counts |
| `t-code` | Geist Mono 13px, no contextual ligatures | Machine values |

Fields set in the code face are 16px under 640px wide, so iOS Safari does
not zoom on focus. Figures in columns use tabular numerals.

## The frame

**Top bar**, sticky, 64px, the page colour at 80% with a backdrop blur and
a hairline under it. Three zones on a `1fr auto 1fr` grid, in a column
76rem wide:

- Left: the wordmark `koko` (20px, 600), a link to Hosts.
- Centre, from 768px: the three places, Hosts, Secrets and Access, in a
  raised pill track (panel colour and shadow, 4px padding). Each is a 32px
  pill; the current one is filled with `--default` and has
  `aria-current="page"`.
- Right: two icon buttons and the account button.
  - **Language**: a languages icon; opens a menu of two radio
    items, English and 简体中文. Its accessible name says the current
    value ("Language: English").
  - **Appearance**: a sun, a moon or a monitor, whichever is chosen; opens
    a menu of three radio items, Light, Dark and System.
  - **Account**: an initial in a circle, the name (hidden under 640px) and
    a chevron; opens a panel 19rem wide: the initial, the name and role,
    "Signed in from Chrome on macOS", "Through koko web on laptop", or, for
    the Local caller, "On the koko server's machine"; then Account, and
    Sign out when the browser has a Sign-in.
- Both choices are kept by the browser, as today.
- **The shell's notices** (koko web is not running; the cookie is not one
  of this run of `koko web`; this device's Device Session was Revoked) stay
  as the shell gives them: one notice across the width under the top bar,
  announced as an alert, until what ends it. They are not toasts.
- **A page whose chunk does not load**, as after an upgrade under an open
  tab, shows in place of the page that the browser page must be reloaded.

**On a phone** (under 768px) the places move to a pill bar fixed at the
foot, with icon and label, 44px targets, above the safe area. Pages leave
room for it.

**A skip link** is the first stop of Tab. **The document's title** is the
page's own name, then "koko" ("web1 · koko"). After a navigation that
leaves focus on nothing, focus moves to the main region.

**A page** is a title (`t-display`), an optional line under it, and its one
or two actions to the right of the title. A detail page has a breadcrumb
above the title.

## Components

HeroUI parts, as the preview composes them:

| Part | Built from | Notes |
| --- | --- | --- |
| Panel (`.sheet`) | a `<section>` | Surface colour, 1rem radius, the panel shadow, 24px padding; a list on a panel has no padding and hairlines between rows |
| Button | `Button` | Primary: one per page at most. Secondary: panel colour and shadow. Tertiary and ghost inside panels. Danger for the confirming action only |
| Side panel | `Drawer` | From the right on a desktop (32rem, 44rem for the Host form), inset 12px with a 1.75rem radius; from the bottom on a phone, with a handle, at most 92% of the height |
| Short form | `Modal` | One to three fields; the refusal shows in place |
| Confirmation | `AlertDialog` | States what is lost; closes on Escape unless the call runs; can ask for a name to be typed |
| Field, select, date | `TextField`, `Select`, `DatePicker` | Label above; help under; the error in place |
| Searchable choice | `ComboBox` | Picking one item from a list that can grow; see below |
| Segments | `ToggleButtonGroup` | A choice among two to four, all in view; the chosen one inverts (text colour as fill) |
| Row menu | `Dropdown` | The actions a row uses less |
| Copy | `Button` | Becomes "Copied" for 1.6 s, announced politely |
| Command line | a `<code>` in an inset block | A `$`, the command, Copy |
| Shown once | an inset block with a dashed accent edge | A Join Code or Password Link, its time left counting down, Copy, and the line that it is not shown again |
| State mark | a `<span>` | Managed: a filled green dot with a halo. Registered: a hollow dot. Retired: a short bar, muted. Always with its word |
| Notice | `toast` | The outcome of a call that changed something, bottom end |
| Refusal | a `role="alert"` block | The koko server's message, in the form or dialog that made the call |

**Searchable choices**, the Owner's rule (2026-11-10): wherever a person
picks one item from a list that can grow, the control is a `ComboBox`, a
field that narrows the list as they type, never a plain `Select`. A plain
`Select` or segments stay for a short fixed list (a comparison, a billing
cycle, Valid for). The places:

| Where | Picks |
| --- | --- |
| Add Host, Edit | the Relay; the Space (Add Host) |
| Add Secret | the Space |
| Move a Host, Move a Secret | the Space to move it to |
| A Space's panel: Add a Member | a User who is not a Member |
| Add device, where it asks whose device it is | a User who is not Removed |
| Filter (Hosts and Services) | the field or Attribute key; the value of an enum with many values, such as `currency` |
| The Attribute rows of the Host and Service forms | the key, which may also be a new one |
| The Hosts list and the Secrets page | the Space the view is narrowed to ("All Spaces" first) |
| The Host and Service forms | the value of an enum with many values, such as `currency` |

Only the Attribute key of a form takes a value the list does not hold; the
others take an item of their list alone.

## States every screen has

The preview's mock answers at once, so these are specified here:

- **Loading**: a page keeps its title and frame. If its first call takes
  over 300 ms, each panel shows three skeleton rows in place of its list.
  A button whose call runs shows HeroUI's pending state and takes no
  second press.
- **Failed to load**: the panel shows the koko server's message and a
  "Try again" button. `signin_required` goes to `/signin?next=<this page>`.
- **Empty**: one panel with a line saying what is missing, a sentence
  saying what the thing is for, and the action that adds one.
- **No match**: one panel with "No Host matches this view." and "Clear the
  search and filters".
- **A refused write**: the message in place, the form kept as typed.
- **Forms**: the submit button stays enabled. A form validates when it is
  submitted, shows each error under its field, focuses the first field at
  fault, and carries `noValidate` so the browser's own bubbles never show.
  The Host form does this in the preview; the short forms there still
  disable their button, which is to be changed.
- **Motion**: only in answer to an action, from HeroUI; off under
  `prefers-reduced-motion`.

## The page map

| Now | Was | Notes |
| --- | --- | --- |
| `/hosts` | `/hosts` | The Hosts view |
| `/hosts?view=services` | `/services` | The Services view of the same page |
| `/hosts/<name or id>` | `/hosts/<name or id>` | |
| `/hosts/<host>/services/<service>` | `/services/<host>/<service>` | A side panel over the Host's page |
| `/secrets` | `/secrets` | |
| `/access` | `/people`, `/spaces` | People and Spaces in one table |
| `/access/people/<name>` | none | A side panel over Access: the Owner alone |
| `/access/spaces/<name>` | `/spaces/<name>` | A side panel over Access |
| `/account` | `/devices` | One's own devices and Sign-ins live here |
| `/signin`, `/password` | none | Outside the frame |
| `/` | `/` | Goes to `/hosts`, keeping its query |

Every URL a released koko served (`/services`, `/services/<host>/<service>`,
`/spaces`, `/spaces/<name>`, `/people`, `/devices`) redirects to its new
one, replacing the history entry and keeping its query, so `all=1` still
shows the Removed Users on Access and the Revoked devices on Account. For
the Owner, `/devices` listed everyone's devices; that view is now each
person's panel under Access. `/signins` was planned (#108) but never
released, so it needs no redirect.

## Screens

### Hosts (`/hosts`)

- Title "Hosts"; under it "9 Hosts in 3 Spaces, running 12 Services.";
  action **Add Host** (primary).
- One row of controls: the Hosts / Services tabs with their counts; a
  search field that grows; a Space select ("All Spaces" first), shown when
  there are two Spaces or more; then, in the Hosts view, a state select
  (In use, Managed, Registered, Retired, "All, with Retired"), **Filter**
  and the columns button (desktop only).
- **Filter** opens a popover: field or Attribute, comparison, value (a
  select for an enum, a date field for a date). Each term becomes a chip
  under the controls with a remove button named for it.
- **A date's value** is a date picked in the calendar or the CLI's own
  text for a relative date (`today`, `+30d`), which stays relative in the
  URL; the field says the date it resolves to today, and the chip shows it
  beside the term (`expires_at ≤ +30d (2026-12-09)`), as
  [relative dates](subsystems/record-pages.md#relative-dates) require. A
  value the key does not take is refused in the popover, naming the term.
- **Hosts are grouped by Space**, the Owner's choice: a heading (the
  Space's name, its count, "Personal Space" where it is one) over one
  panel per Space. Sorting orders the Hosts within each Space.
- A panel is a table with a header row. Columns by default: Name with the
  Host's Service names after it in muted text; Provider and region;
  Address in the code face, with `:port` when it is not 22; Price with its
  billing cycle; Expires as a plain date; State. Rows are 60px. The whole
  row is the link to the Host: the name is the link and covers the row.
- A header sorts by its column; Shift adds a further key, which shows its
  rank. The columns button ticks which columns show, any Attribute key
  among them. The view lives in the URL.
- **The URL keeps koko's grammar** ([the view in the
  URL](subsystems/record-pages.md#the-view-in-the-url)), so a bookmark
  from before opens the same view, and each control writes it:

  | Control | In the URL |
  | --- | --- |
  | Search | `q=<text>`, replacing the history entry as it is typed |
  | Space select | in the Hosts view a `space` term in `filter`; in the Services view `space=<name>`, applied in the browser by each Service's Host, since `service.list` takes no `space` key; "All Spaces" neither |
  | State select: In use | no `state` term and no `all` |
  | State select: Managed, Registered | a `state` term of that value |
  | State select: Retired | a `state` term `retired`, and `all=1` |
  | State select: All, with Retired | `all=1` and no `state` term |
  | Filter | one `filter` per term, as the CLI writes it |
  | A header | `sort`, once per key, in turn |
  | The columns button | `columns`, left out for the default columns |
  | The Services tab | `view=services`; the other view's `filter`, `sort` and `columns` are dropped |

  A `space` or `state` term built in Filter shows in its select, and a
  term the select cannot show (another comparison than `=`, or a second
  such term) leaves it on its first option and shows as a chip.
- **A Host** opens at `/hosts/<name>`, or `/hosts/<id>` when it is
  Retired, since a new Host may hold its name.
- On a phone a row is three lines: name and state; address; provider and
  region with the expiry date.
- With 300 Hosts the page is the same, longer. The search matches name,
  address, Space, Host Account, Note, every Attribute's value and the
  Host's Services and ports.
- A URL whose view the list does not take (an unknown key, a comparison
  the key does not allow) shows one panel that names the term at fault and
  offers "Go to the default view". The preview does not show this state.
- Empty: "No Host is recorded yet", one sentence, **Add Host**.

### The Services view (`/hosts?view=services`)

One panel: a row per Service with its name, "on <host>", and its ports in
the code face. A row opens the Service on its Host's page. The search
matches Service, Host and port; the Space select applies.

The preview stops there. The build also gives this view what the Hosts
view has, over a Service's own fields (name, host) and Attributes:
**Filter**, sorting by a header with Shift for a further key, the columns
button, and the panel for a URL the list does not take. With columns, the
panel becomes a table as a Space's panel of Hosts is.

### Add Host and Edit (side panel, wide)

Four groups, in the order of a provider's order confirmation:

1. **The Host**: Name (the SSH Name), Space, Address.
2. **How koko reaches it**: Host Account (prefilled `root`), Port (22),
   Relay (shown when a Host can be one).
3. **The purchase**: Provider, Region, Price, Currency, Billing cycle,
   Expires (date field with a calendar), Renews automatically (a switch),
   Provider account.
4. **Anything else**: Attributes of any name as key and value rows, then
   the Note.

Add sends one `host.add`, with only the fields given, and the page moves
to the new Host. Edit shows no Name or Space (Rename and Move are their own
actions), sends one `host.set` with the keys that changed and `unset` for
those emptied, and never overwrites a field someone else changed meanwhile
([edit forms](subsystems/record-pages.md#edit-forms)). A Retired Host's
form enables Attributes and Note alone and says so. A value is checked
against its key's type before it is sent; a date field also takes a
relative date such as `+1y`, resolved in the browser's time zone as the
CLI resolves one in a write.

### A Host (`/hosts/<name or id>`)

- Breadcrumb "Hosts › <space>" (the Space links to the list filtered by
  it). Title: the name, with its state mark. Actions: **Edit** and a
  "More actions" menu: Rename, Move to another Space, then Retire and
  Delete as danger items. Delete is offered only to a Host with no pinned
  host key. A Retired Host disables Rename and Retire, offers no Add
  Service, and shows its Services read-only.
- Two thirds, left:
  - **Reaching it**. Managed: `$ ssh <name>` with Copy, then "From a device
    that has joined this koko server. koko logs in as <account>; no key of
    the Host leaves koko." Registered: "koko cannot log in to this Host
    yet. Enroll it from a device that has the CLI:" and
    `$ koko host enroll <name>` with Copy. Retired: one muted sentence.
    Under a hairline: Address, Port, Host Account, Relay (a link by its
    id, or "A Relay you cannot see"), Pinned host key (its fingerprint, or
    "Not pinned"), each a label over a value in the code face.
  - **Attributes**: a grid of label over value, four across: Provider,
    Region, Price, Billing cycle, Expires, Renews automatically, Provider
    account, then every other key. No Attribute: one sentence.
- One third, right:
  - **Services**: name and ports per row; a row opens the Service's panel;
    **Add Service**.
  - **Secrets in <space>**: up to four of the Space's Secrets, those whose
    name holds the Host's name first, each with Copy, Show and its menu as
    on the Secrets page; "All N" links to the Secrets page filtered.
  - **Note**, as written.
  - Under them: "Recorded <date>, changed <ago>" and the id.
- Rename, Move: short forms. Retire, Delete: confirmations that say what
  happens; `relay_in_use` and `host_was_managed` show in place. After
  Rename the page moves to the new name, after Retire to `/hosts/<id>`,
  and after Delete back to `/hosts`, each replacing the history entry.
- The actions name the Host by its id, so a rename made meanwhile never
  sends one to another Host. A Host no one can see answers `unknown_host`,
  shown as for one that does not exist.

### A Service (side panel)

Name, Ports (comma separated; a bare port is TCP), Attributes as key and
value rows as in the Host form, Note; **Save**, and **Delete** at the
foot's start, which asks first. Read-only on a Retired Host. Add Service is
the same panel, empty, and sends one `service.add`. Save sends one
`service.set` for what changed, with `unset` for what was emptied, then
`service.rename` when the name changed; if the second is refused, the
panel says what was saved and shows the refusal in place. The preview's
panel has no Attributes rows; the build adds them. A Service whose Host is
Retired is reached through the Host's id, and its panel offers no action.

### Secrets (`/secrets`)

- Title, one line, **Add Secret**. A search field and the Space select.
- One panel per Space. A row: the name, "Changed <ago>", then **Copy**,
  **Show** and a menu (Set a new value, Rename, Move, Delete).
- The search and the Space select live in the URL (`q=<text>`, as today,
  and `space=<name>`), replacing the history entry as they change, so a
  bookmark opens the page narrowed and a Host's page links to it narrowed
  to its Space.
- Copy calls `secret.show` and copies without showing. Show opens the
  value in the row, in an inset block in the code face with its own Copy,
  and becomes Hide. The value leaves the page after Hide, after 30
  seconds, when the row's Secret is written to, when the page is left and
  when the browser leaves it (`pagehide`); a value answered after any of
  those never shows. A value that is not UTF-8 shows as the base64 it came
  in, marked so, and Copy copies the base64 (the [Secrets
  page](subsystems/secrets.md#the-web-uis-secrets-page)).
- Add Secret: Name, Space, Value (several lines). Set a new value: one
  field. Delete asks first.

### Access (`/access`)

For the Owner:

- Title, one line, actions **Add Space** and **Invite** (primary).
- One panel holding a table: a row per User (initial, name, "Owner" where
  so, and under it how they get in: "3 devices · Password set", or
  "Invited, not yet in"), a column per shared Space (its name and
  "3 Hosts · 3 Secrets", a link to the Space's panel).
- A cell is a checkbox named "<user> is a Member of <space>". Ticking it
  calls `space.member.add`, unticking `space.member.remove`, at once, with
  a notice. The Owner's cells show a lock; a line under the table says the
  Owner sees every Space.
- "Show Removed Users" under the panel adds them, struck through; it is
  `all=1` in the URL.
- On a phone the table becomes two lists, People then Spaces; membership
  is changed in either panel.
- **A person's panel**: role and date; Spaces as switches, and the
  Personal Space's counts; Devices with Add device and Revoke, and "Show
  Revoked devices"; Password ("set on <date>" or none) with Issue Password
  Link and Remove Password; Browser Sign-ins with Revoke; Remove User at
  the foot, which asks first and then says whether the User was deleted
  or Removed. A refusal with `space_not_empty` names the User's Personal
  Space, which still holds records.
- **Revoking a device** asks first and says what follows: the device is
  cut off; for the device the web UI runs through, this page stops and the
  device deletes its key; for the Owner's own last live device, only the
  koko server's own machine can add a device for the Owner again, and the
  device's name must be typed (the call carries `last`). A refused revoke
  reloads the list.
- **A Space's panel**: three counts (Hosts, Retired Hosts, Secrets) that
  link to the lists filtered; Members with "Take out"; Add a Member (a
  select of those who are not one); Rename, and Remove Space, which asks
  first and shows `space_not_empty` in place. After Rename the panel moves
  to the new name, replacing the history entry; after Remove Space it
  closes. A Personal Space shows its counts and says it takes no Members.
- **Add Space**: a short form.

For a Member: their Personal Space and the Spaces they belong to as one
list (counts and Members), each opening its panel read-only, and a line
that their own devices and Sign-ins are on the Account page. A Member gets
no person's panel: `/access/people/<name>` shows "Page not found" to them.

### Invite (dialog, two steps)

1. Name; "How they will use koko" as two radio cards, **In a terminal** (a
   Join Code) and **In a browser** (a Password Link, disabled with the
   reason while the Web URL is unset); **Valid for** as four segments (15
   minutes, 1 hour, 1 day, 7 days); the shared Spaces as checkboxes.
2. "<name> is invited": the code or link shown once with its time left and
   Copy. For a Join Code, the `koko join <code>` line too.

It calls `user.invite`, then `space.member.add` once per Space chosen. If
a membership call fails, step 2 still shows the Invite, which cannot be
shown again, and names the Space that was not added. The preview does not
handle that failure; the build does.

### Account (`/account`)

- Title: the name and role; a line saying how this browser got in; **Sign
  out** when it has a Sign-in.
- Panels: **Password** (current, new, again; a count toward 15 characters;
  through `koko web`, one sentence instead); **This browser** (Language
  and Appearance as segments, which mirror the top bar); **Devices** (this
  one marked, Add device, Revoke as above, and "Show Revoked devices",
  `all=1` in the URL); **Browser Sign-ins** (this browser marked; revoking
  it signs out); **Personal Space** (counts and a link).
- For the Local caller there is no device to mark and no Sign-in; the
  Owner's own last device asks for its name there too.

### Sign in and Set a Password (`/signin`, `/password`)

Outside the frame: the wordmark, then one panel 26rem wide, then the two
languages as buttons.

- **Sign in**: name, Password, **Sign in**; a refusal above the fields
  (`wrong_password`, `too_many_attempts` with the minutes); one line that
  the Owner resets a lost Password.
- **Set a Password**: "For <name>."; New Password with a count toward 15;
  again; **Set Password and sign in**. `link_used` and `link_invalid` each
  replace the form with what happened and "Go to Sign in".

### Page not found

The frame, a title, one sentence and a link to Hosts.

## Accessibility and checks

- axe with colour contrast on every page and every open dialog, panel,
  menu and popover, light and dark, in both languages, in the real-browser
  check ([the web UI build](subsystems/web-ui.md#verifying)) at 360×740,
  390×844 and 1280×800, and at 1440 wide. The preview is at 0 violations.
  Page tests run axe on the same states, without colour contrast, which
  jsdom cannot compute.
- Every overlay takes focus when it opens, closes on Escape and returns
  focus to what opened it. Every icon button has a name. A row that is a
  link is one Tab stop.
- No horizontal scroll at 320px.
- Not yet verified on the preview, and owed by the build: a screen reader
  pass, 200% text size, forced colours, an iOS Safari device, and axe over
  the whole page with 300 Hosts.
