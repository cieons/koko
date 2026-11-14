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
[the Access and Account pages](subsystems/access-pages.md) and
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
| `src/app/Shell.tsx` | The account panel and the page header; its top bar and phone's bar are replaced by the rail (below) |
| `src/ui/parts.tsx`, `src/ui/DateInput.tsx` | The shared parts listed under Components |
| `src/pages/*.tsx` | One file per screen |
| `src/i18n.ts` | Every string, English and Simplified Chinese |
| `index.html`, `vite.config.ts` | The CSP workaround's `<meta>`, the locale plugin |

The frame, the rail of [The frame](#the-frame), was approved on a later
preview, `../koko-terminal-preview` (#170), whose
`web/src/frame/RailFrame.tsx` and `frame.css` are its reference; that
preview's record pages are static stand-ins for koko's own.

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
7. Avoid unnecessary secondary labels and descriptions; let the layout do
   the explaining (#212). A lead, hint, description, empty-state sentence
   or secondary label stays only when the screen cannot say it through its
   structure, headings and controls: the consequence of an irreversible
   action (a rename that breaks old SSH names, a value replaced with no
   history, a Removal that revokes devices), why a link no longer works,
   why a control is disabled, or a format a field takes that cannot be
   seen. Data in a row's second line (when it changed, its counts) is not
   such text.

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
| `--default` | 0.948 0.005 265 | 0.235 0.008 265 | Quiet fills: avatars |
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
| `--term-surface` | 1 0 0 | 0.145 0.005 265 | The terminal's background, and the covers over it |
| `--wait` | 0.6 0.14 70 | 0.84 0.14 80 | The Reconnecting mark |

- The terminal's text, cursor, selection and sixteen ANSI colours are
  `--term-text`, `--term-caret`, `--term-selection` and `--term-ansi-0`
  to `--term-ansi-15`, a set for each mode, in `theme.css`, written in
  hex and `rgb()` as the approved preview gave them;
  `terminal/emulator/emulator.css` hands them to wterm.
- Filled buttons darken on hover (`color-mix` with 8% black). Lightening
  them drops the white label under 4.5:1 in dark.
- Dark needs two reds: no single red holds 4.5:1 both as text and under
  white text.
- `--field-shadow` in dark is `0 0 0 0 transparent inset`, never `none`:
  HeroUI composes a field's focus ring with it, and `none` voids the list.
- **The field edge is the Owner's choice** (2026-11-10): about 1.5:1 on a
  panel, under the 3:1 of WCAG 1.4.11. Do not raise it in a review.
- Radius: `--radius` 0.75rem; panels 1rem; inset blocks 0.75rem; a place
  in the rail and buttons are full pills (HeroUI's default); a place in
  the phone's foot bar is 0.75rem.
- The panel's shadow (`--sheet-shadow`) is, in light, a 1px ring at 7%, a
  1px drop and a wide soft drop; in dark, a 1px highlight inside its top
  edge at 7% white, a 1px ring at 5% white and a deep drop. **The island**
  (`--island-shadow`), what is raised beside the page, such as
  the current place, the active tab on the terminal, a Host's card in the
  empty workspace and the terminal's island (`.island`), is the panel's
  colour and shadow, and in dark also a
  1px edge at 9% white, so it holds against the page. Overlays use
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
| `t-label` | 13px, muted | A fact's label, help, counts, a Group by subheading (in the secondary text colour) |
| `t-figure` | 24px, 600, -0.02em, tabular numerals | A count that stands alone, such as a Space's in its panel |
| `t-code` | Geist Mono 13px, no contextual ligatures | Machine values |

Fields set in the code face are 16px under 640px wide, so iOS Safari does
not zoom on focus. Figures in columns use tabular numerals.

## The frame

One rail down the start edge of every page holds the navigation: the
places, and the User with the browser's two choices. The page fills the
rest. There is no top bar. The Owner chose it on a live preview (#170,
2026-11-12) over a top bar for the record pages beside a rail for the
terminal, for one navigation across the web UI. The terminal's open tabs
are a section of it.

**The rail**, on a desktop (768px and wider), fixed to the start edge, the
full height, on the page colour. It is 15.5rem wide when open, with 12px of
padding. From top to bottom:

- **The wordmark** `koko` (20px, 600), a link to `/`, which opens the
  first place, Hosts, and at the end of
  its row the fold button (a panel icon, "Collapse the side bar" or "Expand
  the side bar", `aria-expanded`).
- **The places**: Hosts, Secrets and Access, each a 40px pill with its
  Lucide icon (18px) and its name. The current one is raised (the island:
  panel colour and the panel shadow, in dark also a 1px edge at 9% white)
  and has `aria-current="page"`. The others are in `--ink-2`, and hover
  gives them a faint panel fill.
- **The Terminal section**, after a hairline and 16px, a labelled group
  outside the places' navigation:
  - its head: **Terminal**, a link to `/terminal` with the count "3 of 16"
    in a `--default` badge while tabs are open, and **New tab**, a 36px
    icon button (`+`) named with its chord, which opens the switcher on the
    terminal from any page (see [The terminal](#the-terminal-terminal));
  - the open tabs, a vertical React Aria `Tabs` list, one per row, 56px.
    Each row shows the state mark, the name (15px; 600 for the active tab,
    else 500 in `--ink-2`), and under the name either the state in words
    when the tab is not Connected (Refused in the danger colour), or the
    directory its shell last reported in the code face at 12px, cut to its
    last two parts ("…/srv/app"), or, with none reported, its Space. A
    close mark, for a pointer and no button inside the tab, ends the row on
    hover and on the active tab; Delete or Backspace closes the focused
    tab;
  - on the terminal the active tab is raised as a place is, and the arrow
    keys show each tab in turn; on other pages no tab is raised, the arrow
    keys only move along the list, and choosing a tab goes to the terminal
    showing it;
  - with no tab open, one line, "Open terminal", with the chord;
  - at 16 tabs, New tab is disabled and a line says why (folded, to a
    screen reader alone), and Open terminal on a Host's page says so in a
    notice;
  - the list scrolls past what fits, and the section keeps at least 10rem.
- **On the terminal only**, above the foot, "To leave the terminal with
  the keyboard, press Escape, then Tab." in `t-label` at 12px. A window
  under 640px high, and the folded rail, keep the line for screen readers
  alone.
- **The foot**, after a hairline:
  - **the User**: an initial in a circle (32px), the name (600) and the
    role ("Owner", "Administrator" or "Member") under it. It opens the account panel, 19rem wide,
    upwards from the rail's start: the initial, the name and role, how
    this browser reaches koko ("Signed in from Chrome on macOS", "Through
    koko web on laptop", or, for the Local caller, "On the koko server's
    machine"), the koko server line for the Owner and an Administrator,
    then Account, and Sign out
    when the browser has a Sign-in;
  - **Language**: a languages icon; opens a menu of two radio items,
    English and 简体中文. Its accessible name says the current value
    ("Language: English");
  - **Appearance**: a sun, a moon or a monitor, whichever is chosen; opens
    a menu of three radio items, Light, Dark and System.
  Both choices are kept by the browser.

**Folded**, the rail is 5rem wide with 8px of padding, and shows the same
items as icons, each keeping its accessible name:

- the wordmark over the fold button;
- the places as 44px icon buttons named by the place;
- Terminal as an icon with its count in a badge on its corner, then `+`;
- the tabs as 56px tiles: the state mark over the name's first characters
  in the code face at 11px, the full name as the tile's tooltip;
- the User's initial, Language and Appearance, stacked.

**Its three states:**

| Window | The rail | Room the page leaves it (`--rail-w`) |
| --- | --- | --- |
| 1280px wide or more | Open, unless the person folds it; the browser keeps the choice (`koko.rail`) | 15.5rem open, 5rem folded |
| 768px to 1279px | Folded. The fold button opens it over the page, on the backdrop, in the overlay colour with a 1.5rem radius at its end edge and the overlay shadow, until a place is chosen, Escape, or a click beside it, each giving the focus back to the fold button. The page stays where it was and takes no input meanwhile | 5rem |
| Under 768px | None: the phone's frame below | 0 |

**A page** sits beside the rail, with 32px of padding (16px on a phone). A
page whose body is a table or a list (Hosts, Secrets and Access) fills the
room left beside the rail. Every other page is a column, 76rem at most,
that starts at the rail, so the room left over is at its end; text that
reads as prose keeps its own limit in `ch` on both. A page is
a title (`t-display`), a line under it only where the page says what its
layout cannot (Account's, how this browser got in), and its one or two
actions to the right of the title. A detail page has a breadcrumb above
the title. With the rail open a page at 1280px wide gets about 1030px, and
at 1100px wide with the rail folded about 1020px. A page's own layout keys
on the room it has, not on the window's width: where it changes from 768px
wide up, beside the rail, it is a container query on the part that
changes (the Host page's and Account's two columns, Access's rows, the
Hosts table's columns that give way).

- **The shell's notices** (koko web is not running; the cookie is not one
  of this run of `koko web`; this device's Device Session was Revoked) run
  across the room beside the rail at the head of the page's column,
  announced as an alert, until what ends them. They are not toasts.
- **A page whose chunk does not load**, as after an upgrade under an open
  tab, shows in place of the page that the browser page must be reloaded.
- **The release notice**, the Owner's and the Administrators' alone, while a newer koko release is
  known: a panel at the head of the main region, above the page's title,
  in the page's column, so it reads as part of the page rather
  than as a warning. An upgrade icon in an accent-soft circle; "koko v0.5.0
  is available." in `t-name`; under it, muted, the version that runs and
  the release notes' link; at its end, Check now and Later as ghost
  buttons and the way to upgrade as a secondary button, since a page's one
  primary button stays the page's. The actions wrap under the text on a
  phone. How to upgrade opens its two lines inside the panel. Once the
  Owner or an Administrator confirms an upgrade, the same panel shows its progress, a spinner
  in the circle, then a check mark or, on a failure, a cross in the danger
  colour with the outcome's message in the code face. The account
  panel shows the Owner and an Administrator a koko server line above Account: the version,
  when it last checked, and Check now.

**On a phone** (under 768px), every page has:

- **A top row**, sticky, 56px under the safe area, the page colour at 85%
  with a backdrop blur and a hairline under it: the wordmark, as in the
  rail; then Language, Appearance and the User's initial, which opens the
  account panel downwards.
- **A bar at the foot**, fixed, the page colour at 90% with a blur and a
  hairline over it, above the safe area, items 48px high:
  - first, where the terminal's key bar starts, **the terminal's button**:
    while tabs are open it is filled in the text colour and shows a stack
    icon, the active tab's name and the count of tabs; with none open it is
    a raised **Terminal** with the terminal icon. It goes to the terminal,
    and takes at most 40% of the bar;
  - then Hosts, Secrets and Access, each an icon over its name at 12px;
    the current one is raised.
- Pages leave room for both. [The Select mode](#the-select-mode)'s bar
  covers the foot bar while the mode is on.
- On the terminal both give way to the terminal's own phone layout, as
  the rail does on a phone held sideways (see [The
  terminal](#the-terminal-terminal), "On a phone").

**A skip link** is the first stop of Tab. **The document's title** is the
page's own name, then "koko" ("web1 · koko"). After a navigation that
leaves focus on nothing, focus moves to the main region.

## Components

HeroUI parts, as the preview composes them:

| Part | Built from | Notes |
| --- | --- | --- |
| Rail | a `<header>` with a `<nav>`, `Link`s, `Button`s, `Tabs` | The frame's navigation down the start edge: open, folded, or over the page, with the terminal's tabs ([The frame](#the-frame)) |
| Switcher | a `ModalOverlay` with an `Autocomplete`: a field over one `ListBox` of two sections | The terminal's: an open tab to show or a Managed Host to open ([The terminal](#the-terminal-terminal)) |
| Panel (`.sheet`) | a `<section>` | Surface colour, 1rem radius, the panel shadow, 24px padding; a list on a panel has no padding and hairlines between rows |
| Button | `Button` | Primary: one per page at most. Secondary: panel colour and shadow. Tertiary and ghost inside panels. Danger for the confirming action only |
| Side panel | `Drawer` | From the right on a desktop (32rem, 44rem for the Host form), inset 12px with a 1.75rem radius; from the bottom on a phone, with a handle, at most 92% of the height |
| Short form | `Modal` | One to three fields; the refusal shows in place |
| Confirmation | `AlertDialog` | States what is lost; closes on Escape or a click beside it unless the call runs; can ask for a name to be typed |
| Field, select, date | `TextField`, `Select`, `DatePicker` | Label above; help under; the error in place |
| Searchable choice | `ComboBox` | Picking one item from a list that can grow; see below |
| Segments | `ToggleButtonGroup` | A choice among two to four, all in view; the chosen one inverts (text colour as fill) |
| Row menu | `Dropdown` | The actions a row uses less |
| Selection bar | an island at the foot, `Button`s | The count, Select all, the actions and Done of [the Select mode](#the-select-mode); centred in the room beside the rail on a desktop, over the foot bar on a phone |
| Copy | `Button` | Becomes "Copied" for 1.6 s, announced politely |
| Command line | a `<code>` in an inset block | A `$`, the command, Copy; a line for a file has no `$` |
| Shown once | an inset block with a dashed accent edge | A Join Code or Password Link, its time left counting down, Copy, and the line that it is not shown again |
| State mark | a `<span>` | Managed: a filled green dot with a halo. Registered: a hollow dot. Retired: a short bar, muted. A terminal's tab: Connecting, a hollow ring in `--muted`; Connected, the Managed mark; Reconnecting, a dot half filled in `--wait`; Ended, a short bar in `--muted`; Refused, or not reached, a ✕ in the danger colour. Always with its word, shown or for a screen reader |
| Notice | `toast` | The outcome of a call that changed something, bottom end; a danger toast only for a refusal whose form closed before it came |
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
| Enroll, and Add Host's Enroll | the Secret to log in with |
| Move a Host, Move a Secret | the Space to move it to |
| A person's panel: Add to a Space | a shared Space the person is not in |
| Add device, where it asks whose device it is | a User who is not Removed |
| Filter (Hosts and Services) | the field or Attribute key; the value of an enum with many values, such as `currency` |
| The Attribute rows of the Host and Service forms | the key, which may also be a new one |
| The Hosts list and the Secrets page | the Space the view is narrowed to ("All Spaces" first) |
| The Hosts list: Group by | the Attribute key each Space's Hosts are grouped by ("None" first) |
| The Host and Service forms | the value of an enum with many values, such as `currency` |

Only the Attribute key of a form takes a value the list does not hold; the
others take an item of their list alone.

Where a person picks several items from a list that can grow, the control
is HeroUI's `Autocomplete` with several chosen: a field showing each item
picked as a tag with its remove button, whose list has a search field over
it and ticks each item picked. The places: a Space's panel, Add Members
(the Users who are not Members), and Invite's shared Spaces.

The list opens when the field is pressed or typed in, or on its button or
the up or down arrow. It does not open when the field only takes the focus,
which HeroUI's `ComboBox` does by default: a modal overlay gives the focus
back to the field it was in when a click beside the overlay takes it away,
and a list that opened then would take that click, so the overlay would
not close (#133).

**Closing an overlay**, the Owner's rule (#133): a click beside an overlay
closes it, unless closing would lose something.

| Overlay | A click beside it |
| --- | --- |
| A popover or menu: Filter, Columns, a select's or searchable choice's list, a row menu, the frame's menus, the account panel | Closes it, always |
| The rail laid over the page, under 1280px wide | Closes it, always, as Escape and a place chosen do |
| A short form or a side panel with a form: Add Host, Edit, Enroll, Add Service, a Service's panel, Rename, Move, Add Secret, Set value, Add Space, the first step of Invite, Add device and a Password Link; a Space's panel, for its Add Members field; a person's panel, for its Add to a Space field | Closes it while nothing in it has changed; once something has been typed or chosen, does nothing |
| A confirmation | Cancels it, as Cancel does, unless its call runs; a name typed in it to confirm is not kept |
| A batch's dialog ([the Select mode](#the-select-mode)) | A form (Move, Attribute) as a short form; a confirmation (Retire, Delete) as one, but not once the count has been typed; a refused batch's list of records at fault closes, as Cancel does |
| A value shown once: a Join Code or a Password Link | Does nothing, since the value cannot be shown again; Done, the close button and Escape close it |
| A side panel without a form: a Service on a Retired Host | Closes it, always |

A form that has changed is not closed by a click beside it, rather than
asking whether to discard what was typed: a stray click loses nothing and
asks nothing, and Cancel and Escape still close it at once. A form whose
call runs is not closed by one either. Wherever a click beside an overlay
closes it, the focus goes back to what opened it, as after Escape. With a
list open in an overlay, the first click beside it closes the list and the
next the overlay, as Escape does.

## States every screen has

The preview's mock answers at once, so these are specified here:

- **Loading**: a page keeps its title and frame. If its first call takes
  over 300 ms, each panel shows three skeleton rows in place of its list.
  A button whose call runs shows HeroUI's pending state and takes no
  second press.
- **Failed to load**: the panel shows the koko server's message and a
  "Try again" button. `signin_required` goes to `/signin?next=<this page>`.
- **Empty**: one panel with a line saying what is missing and the action
  that adds one; a sentence under it only where the screen offers no such
  action, to say where one is added.
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
| `/access` | `/people`, `/spaces` | People and Spaces as two lists, `view=spaces` for the Spaces |
| `/access/people/<name>` | none | A side panel over Access: the Owner and the Administrators alone |
| `/access/spaces/<name>` | `/spaces/<name>` | A side panel over Access |
| `/account` | `/devices` | One's own devices and Sign-ins live here |
| `/signin`, `/password` | none | Outside the frame |
| `/terminal` | none | The User's Workspace: `?tab=2` names the tab shown, counted from 1 |
| `/hosts/<name>/terminal` | none | Adds a tab for the Host to the Workspace, then shows it on `/terminal` |
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

- Title "Hosts", with no line under it, since the tabs carry the counts;
  action **Add Host** (primary).
- One row of controls: the Hosts / Services tabs with their counts; a
  search field that grows; a Space select ("All Spaces" first), shown when
  there are two Spaces or more; then, in the Hosts view, a state select
  (In use, Managed, Registered, Retired, "All, with Retired"), **Filter**,
  **Group by**, the columns button (desktop only), **Select**
  ([the Select mode](#the-select-mode)) and **Reset view**. Each
  control keeps its label on one line, a tab as wide as its label and
  count; when the room beside the rail runs out the row wraps onto a
  second line.
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
- **Group by**, the Owner's choice (#137), adds a second level under each
  Space: a searchable choice, "Group by" shown in the field before the key,
  of None (the default) or any Attribute key in use or Preset, such as
  `role` or `region`. With a key, a Space's section holds a subheading per
  value (the value as stored in 13px `t-label`, the secondary text colour,
  its count muted) over a panel of those Hosts, in the order the list
  sorts the values in by the key's type, and a last one, "No role", for
  the Hosts without the key; a value the search and Filter leave no Host
  of has none. Within a subheading the Hosts keep the sort. A phone shows
  the same subheadings over its lists. The Owner chose an Attribute over a
  Preset `group` key or Spaces as groups: a Space is an access boundary,
  and the useful groupings differ (`role`, `region`, `provider`).
- A panel is a table with a header row. Columns by default: Name with the
  Host's Service names after it in muted text; Provider and region;
  Address in the code face, with `:port` when it is not 22; Price with its
  billing cycle; Expires as a plain date; State. Rows are 60px. The whole
  row is the link to the Host: the name is the link and covers the row.
- A header sorts by its column; Shift adds a further key, which shows its
  rank. The columns button ticks which columns show, any Attribute key
  among them. The view lives in the URL. The table and its columns
  button are a desktop's (768px and wider), as the rows of three lines
  are a phone's.
- **The table in the room beside the rail**, the Owner's choice (#182):
  as the table itself narrows (a container query on its own width), two
  of the default columns give way, Provider and region under 64rem, then
  Price under 52rem; their header and cells hide, and the view in the
  URL keeps them. Every other column shows always, any column the person
  adds with the columns button among them, and so does a column the list
  sorts by. Past what they free, the table scrolls sideways inside its
  panel, never the page.
- **A list remembers its view** in the browser, for each User: the Hosts
  view, the Services view, Secrets and Access, each on its own. Opened with no
  view in its URL (from the rail, a new tab, a link or after signing
  in), a list shows the view it was left in, and its URL is replaced by
  it, so the URL and a bookmark keep exactly the view on screen; a URL
  with a view wins and is remembered. **Reset view** (secondary, with a
  turning-back arrow, unavailable at the default view, so the row keeps
  its layout) shows the default view and forgets the one remembered. Only
  what the URL holds is kept.
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
  | Group by | `group=<key>`, left out for None |
  | The Services tab | `view=services`; the other view's `filter`, `sort`, `columns` and `group` are dropped |

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
- Empty: "No Host is recorded yet" and **Add Host**.

### The Services view (`/hosts?view=services`)

One panel: a row per Service with its name, "on <host>", and its ports in
the code face. A row opens the Service on its Host's page. The search
matches Service, Host and port; the Space select applies.

The preview stops there. The build also gives this view what the Hosts
view has, over a Service's own fields (name, host) and Attributes:
**Filter**, sorting by a header with Shift for a further key, the columns
button, Reset view, a view of its own remembered, and the panel for a URL
the list does not take. With columns, the
panel becomes a table as a Space's panel of Hosts is.

### Add Host and Edit (side panel, wide)

Four groups, in the order of a provider's order confirmation, then
Enroll in Add Host:

1. **The Host**: Name (the SSH Name), Space, Address.
2. **How koko reaches it**: Host Account (prefilled `root`), Port (22),
   Relay (shown when a Host can be one).
3. **The purchase**: Provider, Region, Price, Currency, Billing cycle,
   Expires (date field with a calendar), Renews automatically (a switch),
   Provider account.
4. **Anything else**: Attributes of any name as key and value rows, then
   the Note.
5. **Enroll** (Add Host only): [the Enroll fields](#enroll-short-form),
   "Not now" chosen.

Add sends one `host.add`, with only the fields given, and the page moves
to the new Host. With a way to Enroll given, it then sends one
`host.enroll` for the new Host, and the toast names the host key pinned.
An Enroll refused leaves the Host recorded: the panel's form gives way to
"web3 is recorded, but not Enrolled:" and the refusal, as an alert, with
**Open web3** (primary), whose page offers Enroll again, and Close. Edit shows no Name or Space (Rename and Move are their own
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
- Two columns once the page's own room is 56rem wide (container query),
  one under the other below that.
- Two thirds, left:
  - **Reaching it**. Managed: **Open terminal**, a secondary button
    linking to `/hosts/<name>/terminal`, over `$ ssh <name>` with Copy.
    Registered: **Enroll**, a secondary button that opens
    [Enroll](#enroll-short-form). Retired: "It can no longer be
    reached.", muted.
    Under a hairline: Address, Port, Host Account, Relay (the path koko
    dials, `web1 → db1`, each Relay a link by its id, the part you cannot
    see as "A Relay you cannot see"), Pinned host key (its fingerprint, or
    "Not pinned"), each a label over a value in the code face.
  - **Attributes**: a grid of label over value, four across: Provider,
    Region, Price, Billing cycle, Expires, Renews automatically, Provider
    account, then every other key. No Attribute: "No Attribute is
    recorded."
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

### Enroll (short form)

A Registered Host's **Enroll** opens it, "Enroll <name>",
with **Enroll** as its button; Add Host's fifth group holds the same
fields ([Enroll](subsystems/enroll.md#the-web-ui)):

- **Enroll with**: segments, Password, Private key, Secret; Add Host puts
  "Not now" first, chosen.
- **Password of <Host Account>**: a password field.
- **Private key**: several lines in the code face, pasted, or filled by
  **Read from a file** (tertiary, under it), read in the browser. A key
  that is encrypted adds **Passphrase of the private key**, a password
  field, under it.
- **Secret**: a searchable choice of the Secrets the person can use, each
  as `<space>/<name>`.
- **Host key fingerprint**, for any way: optional, in the code face,
  `SHA256:…` as its placeholder, and a line under it saying that Enroll
  then accepts only that key; checked on submit as the CLI checks it.
  The passphrase also shows once the koko server says the key needs one.

Outside a secure context the form offers a Secret alone, with one line
saying why: the connection is not secure. The credential fields ask the
browser not to fill or save them; the form clears them as its call is
sent, and closing it drops them. Add Host closed while its calls run
sends nothing more and stays on the list, which loads again; an answer
that comes after says in a toast what was done. A refusal shows in place, the credential
to be given again. Once Enrolled, a toast says "<name> is Managed, with its
host key SHA256:… pinned.", as the CLI says it, and the page shows the
pin.

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

- Title and **Add Secret**. A search field and the Space select.
- One panel per Space. A row: the name, "Changed <ago>", then **Copy**,
  **Show** and a menu (Set a new value, Rename, Move, Delete).
- The search and the Space select live in the URL (`q=<text>`, as today,
  and `space=<name>`), replacing the history entry as they change, so a
  bookmark opens the page narrowed and a Host's page links to it narrowed
  to its Space. The browser remembers them as it remembers a list's view
  (Hosts above), with **Reset view** after the Space select.
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
- **Select**, after the Space select, starts [the Select
  mode](#the-select-mode): Move and Delete on several Secrets at once.

### The Select mode

Acting on several records at once, the Owner's choice of three designs on
a live preview (#134): a mode, the same on a phone and a desktop, over a
checkbox column with a bar and over actions on the whole view. It is on
the Hosts list and the Secrets page.

- **Select** (secondary, a list with ticks) sits in the controls row.
  Pressed, it becomes **Done** (primary) and the mode is on: rows are no
  longer links; each shows a checkbox before its name, and a click or tap
  anywhere on a row toggles it. A ticked row takes the soft accent fill.
  Shift-click ticks a range on a desktop. On Secrets, Copy, Show and the
  row's menu hide in the mode, so a tap only toggles.
- **Groups.** Each table's header row has a checkbox for its rows. With
  Group by, the Space's heading has one for the whole Space; on a phone,
  where a panel is a list, each heading and subheading has one. On
  Secrets each Space's heading has one. A group with some rows ticked
  shows the mixed state.
- **The bar** floats at the foot from the start of the mode, an island,
  centred in the room beside the rail on a desktop and covering the foot
  bar on a phone: the count
  ("12 selected"), how many of those the view does not show ("3 not in
  this view"), **Select all N** ("Select all N matching" under a search
  or Filter) until every row shown is ticked, the actions, each an icon
  and a word, the ones that take something away set apart in the danger
  colour, and **Done**. The actions are unavailable until something is
  ticked. On a phone the bar is two lines: the count, Select all and Done
  over the actions. Done leaves the mode and clears the selection.
- **The selection is kept when the view changes.** Search, Filter, Group
  by and sorting keep working in the mode, so a selection can be gathered
  across several searches.
- **Actions on Hosts:** Move (to a Space), Attribute (set or remove one),
  Retire, Delete. **On Secrets:** Move, Delete.
- **One dialog per batch.** Its title and its button name the action and
  the count ("Retire 12 Hosts"), and it lists the records, two columns in
  an inset block that scrolls in place. Move and Attribute are a short
  form (`Modal`, medium); Retire and Delete are a confirmation
  (`AlertDialog`) that states the consequence, and Delete of more than
  one asks for the count to be typed.
- **Left out before sending, and said so** under the list: Hosts already
  in the chosen Space, already Retired, without the key being removed,
  and, for Delete, Hosts that have been Managed. The count in the title
  and the button is of the records the batch acts on.
- **A refused batch.** A batch is applied in full or not at all. Refused,
  the dialog stays and says nothing was applied ("No Host was moved"),
  lists the records at fault, each with the koko server's reason, and
  offers to send the batch again without them ("Move the other 17
  Hosts"). Cancel leaves the selection as it was.
- **Success.** A notice says what was done ("17 Hosts are now in ops."),
  the mode ends and the selection clears.

### The terminal (`/terminal`)

The User's Workspace: tabs, each its own SSH Session to a Managed Host,
held by the koko server and the same in every page of the User, beside the
frame's rail. Its tabs live in the rail's Terminal section on every page,
and the workspace stays mounted while the person visits other pages (spec
#169, spec #213; [the web terminal](subsystems/web-terminal.md)). The
Owner approved it on a live preview (#170, 2026-11-12), the rail's
direction. Details may be polished in implementation.

**On a desktop:**

- **The island.** The workspace fills the room beside the rail with 12px
  around it (none at the rail's side, whose own padding is the gap). It is
  one island, with a 1rem radius, the panel shadow, and in dark a 1px edge
  at 9% white. The frame's notice, when there is one, heads it. In it:
  - **The head**, 56px, panel colour, a hairline under it:
    - the breadcrumb: the Space (from 1024px wide), "›", and the tab's
      name (17px, 600), a link to the Host's page;
    - the state mark with its word;
    - the directory the shell reported, from 1280px wide: a `--default`
      chip with a folder icon and the full path in the code face at 12px,
      named "Directory:" for a screen reader;
    - at the end, **Files** (secondary, a folder icon and the word;
      `aria-pressed`, filled in the text colour while open) and **More**
      (ghost, an ellipsis);
  - **the terminal**, under the head, filling the island.
- **The terminal itself:**
  - background `--term-surface`, a step above the page;
  - Geist Mono at 14px on 18px rows, Chinese falling through to the
    system's fonts, padded 14px by 18px (8px by 10px on a phone);
  - the sixteen ANSI colours of each mode as `--term-*` tokens;
  - text sizes 12, 13, 14, 16 and 18px from More;
  - drawn at the pty's size, which follows the page a person last acted
    in: in a box too small, scaled down to fit it, from its top start; in
    a box too large, at its top start with the margin in `--term-surface`.
- **The key bar**, when the primary pointer is a touch screen (`pointer:
  coarse`), as on a tablet, at the island's foot under a hairline (see "On
  a phone").
- **Files** docks inside the island at its end edge, as a column
  `clamp(20rem, 28vw, 26rem)` wide with a hairline at its start. The
  terminal's box narrows to make room, and the koko server is told it. The
  column has its own 56px head ("Files · on web1", and Close) over the
  Files panel, padded 16px by 20px, which acts on the active tab's SSH
  Session (below).

**The Files panel** lists one directory of the tab's Host at a time, and
takes uploads into it (spec #169, "Files over SFTP"):

- **Where it opens:** the directory the shell last reported (OSC 7, its
  host part empty or the Host's name), or else, or when that cannot be
  listed, the Host Account's home.
- **The path** in the code face at 13px on a `--default` field, the full
  path wrapping, named "Directory:" for a screen reader; under it Up and
  Refresh (ghost, icons alone) and, at the end, **Upload**, the panel's
  primary button, which takes several files.
- **The listing:** directories first, then the rest, by name in the
  browser's collation; each row a folder, link or file icon (a directory
  or link's in the link colour), the name, and under it its size in
  binary units (a file) or its kind, then its date. A directory row opens
  it, a file row downloads it with the browser's own progress, a link
  downloads or is entered; anything else is shown dimmed and not offered.
  An empty directory says "This directory is empty."; past 5,000 entries,
  "7120 more entries are not listed."
- **Uploads**, under a hairline, from every tab: each row the file's name,
  "To web1: /home/deploy", HeroUI's `ProgressBar` (small) while it runs
  with "512 KiB of 600 KiB" under it, and Cancel (ghost, ✕); its state in
  words otherwise (Waiting; "The connection dropped. Sending again…";
  Uploaded; Cancelled; the refusal in the danger colour), with Dismiss.
- **A replace** is asked first, one file at a time: "Replace notes.txt in
  /home/deploy?" (`AlertDialog`), "The file on web1 is replaced by the
  one uploaded, and its present content is lost.", Replace.
- **Dropping files on the terminal** uploads them into the directory its
  shell is in: while they are dragged over it, a dashed accent edge 12px
  in, over the terminal at 85%, says "Drop to upload into /home/deploy on
  web1"; dropping opens Files there and asks about a name it holds.
- **States:** loading rows while the tab connects or a directory loads;
  a refusal in place, the Host's own words beneath, with Try again; a Host
  without SFTP as one line in place of the panel ("web1 offers no SFTP,
  which koko moves files through.");
  "The SSH Session has ended." in place of a listing once it has.
- **On a phone** the panel is a side panel from the bottom, titled "Files
  on web1".
- **More**: Larger text, Smaller text, Copy the selection and Announce
  output.
- **The title** is the active tab's Host: "web1 · Terminal · koko".

**The keys that reach the workspace.** The workspace takes these chords on
the window, before the terminal sees them, so the shell never receives
them; the browsers bind none of them:

| Chord (`⌃⇧` on a Mac) | Does |
| --- | --- |
| Ctrl+Shift+K | Opens the switcher, from any page; from another page it goes to the terminal first |
| Ctrl+Shift+← and → | Show the tab before and after the active one, wrapping, on the terminal, unless the switcher is open |

In the rail's tab list the arrow keys move between tabs, and Delete or
Backspace closes the focused tab, asking first while its shell runs.

**The switcher.** New tab and Ctrl+Shift+K open it: one field over the open
tabs and the Managed Hosts the caller can see.

- A dialog 36rem wide, 12vh from the top (12px from it on a phone), radius
  1.25rem, the overlay colour and shadow, over the backdrop.
- The field is 56px high, at 17px (16px on a phone), with a search icon and
  the placeholder "Search tabs and Hosts". Under it, one list box of two
  sections:
  - **Open tabs**: the mark, the name, and the directory or state; the
    shown tab is marked "Shown";
  - **Open in a new tab**: `+`, each Managed Host's name and Space, and
    "Open".
- Typing narrows both by name, directory and Space, the first match taking
  the virtual focus. ↑ and ↓ move, Enter shows the tab or opens the Host
  in a new tab, Escape closes. None matching: "No tab or Host matches."
- At 16 tabs the second section gives way to the limit's line. A footer
  line gives the chords.

**Tabs and their states.**

- **Labels** are the koko server's: the Host's name, and for a second tab
  to the same Host "web1 (2)", the lowest number no open tab has. A
  Workspace holds at most 16 tabs.
- **A state never moves the layout.** Each shows inside its tab, over the
  terminal:
  - **Covers**, in `--term-surface`, for a tab with nothing to show, the
    text and actions at the start of a column 32rem wide, centred:
    - Connecting: "Connecting to web1…" (17px, 500), with the spinner past
      300 ms, while its SSH Session starts or the page attaches;
    - after a koko server restart: "The koko server restarted.", as
      Connecting is, until its new SSH Session starts;
    - Refused: the refusal block, the page's words for the code ("koko
      could not reach web1. This SSH Session did not start.") over the
      koko server's own words in the code face, as it wrote them, then
      **Try again** (primary), which opens a new SSH Session in the tab,
      and **Close tab** (tertiary);
    - another page took its place: "This terminal is open in 8 other
      pages." and **Show here** (primary);
    - an out-of-date page: "This page is out of date. Reload it." and
      **Reload** (primary).
    A refusal for want of a Sign-in goes to Sign in, which comes back here.
  - **Notices**, for a tab whose terminal stays: cards at the terminal's
    top end, 12px in, at most 34rem wide (8px in and full width on a
    phone), the overlay colour and shadow, a 1rem radius, the state's mark
    first, announced politely. Several stack in this order:
    - Reconnecting dims the terminal to 55%, still selectable, under
      "Reconnecting to the koko server… Keys typed now are sent once it is
      back." and **Try now** (secondary);
    - Ended: why, the same words in every page ("The shell on web1 exited
      with status 0.", "The shell on web1 ended by signal KILL.", "The koko
      server is restarting; this SSH Session has ended.", or "The SSH
      Session to web1 has ended." with the koko server's notice under it),
      and **Open a new one** (primary), in the same tab, and **Close tab**
      (tertiary);
    - the clipboard request (OSC 52), in the page a person last acted in
      alone: a clipboard icon, "web1 asks to copy 42 characters to the
      clipboard.", the first line in the code face, then **Copy**
      (secondary) and **Dismiss** (tertiary). A newer request replaces an
      unanswered one.
- **Opening and closing** change the Workspace: a tab appears, and goes,
  once the koko server's Workspace says so. Closing a tab whose shell runs
  or starts asks first: "Close web1?", "This ends its SSH Session in every
  place it is open.", **Close tab**; one refused or ended closes at once. A
  tab shown here that another page closed is announced, "web1 was closed
  in another page.", and the tab beside it shown. An open refused adds no
  tab, and says why in place: in the switcher that sent it, which stays
  open; for an open from a Host's card or Open terminal, in a card at the
  terminal's top end with **Dismiss** (tertiary).
- **An empty workspace:** "No terminal is open" (`t-display`), up to
  eight Managed Hosts as raised cards
  (name over Space; one opens a tab), and "Search every Managed Host" with
  the chord, which opens the switcher.
- **One tree for every form.** The workspace keeps its terminals in one
  place in its tree on a desktop and a phone alike. A window crossing
  768px, or a phone turning, keeps every tab's emulator and SSH Session.

**On a phone** (under 768px wide, and on a touch screen held sideways:
`pointer: coarse`, landscape, at most 540px high), the frame's top row
and foot bar give way on this page, and the rail too, held sideways. The
workspace fills the visual viewport (it follows `visualViewport`'s
height), so the key bar's row sits on the soft keyboard, and the
terminal runs edge to edge in `--term-surface`. On a touch screen the
terminal never takes the focus by itself, which would open the soft
keyboard: a tap on it, or the keyboard key, does.

- **The row over the terminal**, 48px under the safe area, the page
  colour, a hairline under it:
  - the wordmark, a link back to Hosts ("koko, Back to Hosts" for a screen
    reader);
  - the state mark, with its word when the tab is not Connected;
  - the directory's last two parts ("…/srv/app") in the code face at
    12px;
  - Files and More as ghost icon buttons.

  Held sideways the row is hidden, and Files and More end the key bar's
  row.
- **The key bar's row**, at the foot, the page colour, a hairline over
  it, above the safe area:
  - **the tab button**: a 44px key filled in the text colour, a stack
    icon, the active tab's name and the number of tabs, at most 42% of
    the width ("web1, 3 open. Show the tabs"). With no tab open it is a
    raised **Terminal** with the terminal icon. A tap opens the sheet of
    tabs. A swipe across it, more than 36px, shows the next tab
    (leftwards) or the one before (rightwards), wrapping, without opening
    the sheet. A press on it moves no focus, so the soft keyboard stays
    until the sheet opens;
  - **the keys**, on a touch screen with a tab open: 44px, raised, in the
    code face at 15px: Esc, Tab, Ctrl, Alt, ←, ↑, ↓, →, `|`, `/`, `-`,
    `~`, Home, End, PgUp, PgDn, and the keyboard key, which opens or
    closes the soft keyboard. Each is named for a screen reader
    ("Escape", "Left arrow", "Vertical bar"). They scroll sideways,
    fading at their end. Each key goes to the active tab as the
    terminal's modes encode it, and makes the phone the page the
    terminal fits, so a full-screen program fits the phone while it is
    used. Ctrl and Alt are sticky: one tap applies
    the modifier to the next key, from the bar or the soft keyboard, and
    lets it go, with the accent fill; a second tap holds it, with the text
    colour as fill; a third lets it go (`aria-pressed`). A key never takes
    the focus. The row of keys is one Tab stop, so a keyboard can scroll
    it;
  - without a touch screen, as in a narrow desktop window, the row holds
    the tab button alone.
- **The sheet of tabs**, a side panel from the bottom titled "Tabs":
  - Hosts, Secrets and Access first, as raised tiles of icon over name,
    so one thumb gets back to them;
  - then the Workspace's tabs, 56px rows: the mark, the name, and the state or
    directory under it, as in the rail; a tap shows the tab and closes the
    sheet, and each row's close button closes it, asking first while its
    shell runs;
  - then **New tab** (primary, full width), which opens the switcher; at
    16 tabs the limit's line in its place.
- The frame's notice, when there is one, sits under the row, over the
  terminal.

**Strings.** The page is 终端, Files is 文件, a tab is 标签页, an SSH
Session is SSH 会话.

### Access (`/access`)

For the Owner and an Administrator:

- Title, and the actions **Add Space** and **Invite** (primary).
- One row of controls: the tabs **People** and **Spaces**, each with its
  count; one search field that grows and narrows the list shown by name;
  **Reset view**. The tab is `view=spaces` in the URL for the Spaces, the
  search `q=<text>`, replacing the history entry as it is typed, and the
  browser remembers them, with "Show Removed Users", as it remembers a
  list's view.
- Under it, one panel holding the list of the tab chosen, a row per person
  or Space, each a link to its panel. A person's row: initial, name,
  "Owner" or "Administrator" where so, and a one-line summary, "16 Spaces
  · 2 devices · Password set", "No Space · Invited, not yet in", "Every
  Space · 1 device" for the Owner and an Administrator. A Space's row: name and "30 Members · 40 Hosts ·
  0 Secrets". The summary is beside the name once the list itself is
  42rem wide (container query) and under it below that, as on a phone.
  The Owner is first, then the Administrators, then the Members, and the
  Removed Users last.
- Nothing matching the search: one panel with "No User matches this
  search." or "No Space matches this search." and **Clear the search**.
  No shared Space: the Spaces tab says so.
- "Show Removed Users" under the People adds them, struck through; it is
  `all=1` in the URL.
- Membership is changed in the panels, never in the lists.
- **A person's panel**: role and date; their Spaces, "A Member of 3 of 25
  shared Spaces.", under **Add to a Space**, a searchable choice of the
  shared Spaces they are not in, each Space a link to its panel with
  **Take out**; and the Personal Space's counts; Devices with Add device and Revoke, and "Show
  Revoked devices"; Password ("set on <date>" or none) with Issue Password
  Link and Remove Password; Browser Sign-ins with Revoke; for the Owner,
  **Make an Administrator**, which asks first and lists the User's live
  devices, each with who added it, and live Sign-ins, or **Make a
  Member**, which does not; Remove User at the foot, which asks first and
  then says whether the User was deleted or Removed. An Administrator or
  the Owner is "Every Space" in place of the Spaces. Viewing the Owner or
  another Administrator, an Administrator sees the panel without any of
  its actions. A refusal with `space_not_empty` names the User's Personal
  Space, which still holds records.
- **Revoking a device** asks first and says what follows: the device is
  cut off; for the device the web UI runs through, this page stops and the
  device deletes its key; for the Owner's own last live device, only the
  koko server's own machine, or a browser signed in with the Owner's
  Password, can add a device for the Owner again, and the
  device's name must be typed (the call carries `last`). A refused revoke
  reloads the list.
- **A Space's panel**: three counts (Hosts, Retired Hosts, Secrets) that
  link to the lists filtered; **Add Members**, a searchable choice of
  several among those who are not Members, which adds them in one call
  ("Add 3 Members"), above the Members, each with **Take out**; Rename, and Remove Space, which asks
  first and shows `space_not_empty` in place. After Rename the panel moves
  to the new name, replacing the history entry; after Remove Space it
  closes. A Personal Space shows its counts and says it takes no Members.
- **After Take out** the focus stays in the list: on the next row's Take
  out, the one before for the last row, or the add field once the list is
  empty.
- **Add Space**: a short form.

For a Member: their Personal Space and the Spaces they belong to as one
list, each with its summary ("Personal Space" and its counts, or "3
Members · 4 Hosts · 1 Secret"), each opening its panel read-only, which
names the Members. A Member gets
no person's panel: `/access/people/<name>` shows "Page not found" to them.

### Invite (dialog, two steps)

1. Name; "How they will use koko" as two radio cards, **In a terminal** (a
   Join Code) and **In a browser** (a Password Link, disabled with the
   reason while the Web URL is unset); **Valid for** as four segments (15
   minutes, 1 hour, 1 day, 7 days); **Spaces**, the shared Spaces to make
   them a Member of, a searchable choice of several, as Add Members is.
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
  and Appearance as segments, which mirror the rail's); **Devices** (this
  one marked, Add device, Revoke as above, and "Show Revoked devices",
  `all=1` in the URL); **Browser Sign-ins** (this browser marked; revoking
  it signs out); **Personal Space** (counts and a link). Two columns once
  the page's own room is 56rem wide (container query).
- For the Local caller there is no device to mark and no Sign-in; the
  Owner's own last device asks for its name there too.

### Sign in and Set a Password (`/signin`, `/password`)

Outside the frame: the wordmark, then one panel 26rem wide, then the two
languages as buttons.

- **Sign in**: name, Password, **Sign in**; a refusal above the fields
  (`wrong_password`, `too_many_attempts` with the minutes); one line that
  the Owner or an Administrator resets a lost Password.
- **Set a Password**: "For <name>."; New Password with a count toward 15;
  again; **Set Password and sign in**. `link_used` and `link_invalid` each
  replace the form with what happened and "Go to Sign in".

### Page not found

The frame, a title and a link to Hosts.

## Accessibility and checks

- axe with colour contrast on every page and every open dialog, panel,
  menu and popover, light and dark, in both languages, in the real-browser
  check ([the web UI build](subsystems/web-ui.md#verifying)) at 360×740,
  390×844, 1100×800 (the rail folded, and laid over the page), 1280×800
  (the rail open and folded), and at 1440 wide. The preview is at 0
  violations.
  Page tests run axe on the same states, without colour contrast, which
  jsdom cannot compute.
- Every overlay takes focus when it opens, closes on Escape, and on a
  click beside it as [Closing an overlay](#components) says, and returns
  focus to what opened it. Every icon button has a name. A row that is a
  link is one Tab stop.
- No horizontal scroll at 320px.
- A focused control is never left under the phone's top row or its foot
  bar; the root carries scroll padding for it. A desktop's rail lies
  beside the page, and over it only while the page takes no input.
- In forced colours (such as Windows' contrast themes) the focus ring is
  an outline, and the chosen place, tab (the terminal's active tab too),
  segment, checked box, switch and radio, the key bar's Ctrl and Alt
  while on, the rail's end edge and the edges of panels, overlays,
  islands and the phone's tab button are
  drawn in the system's colours ([the web UI build](subsystems/web-ui.md#heroui-and-tailwind)).
- Checked on the build (#126): 200% text size (no loss of content; on a
  phone the search fields and Account's panels kept their width then, and
  give way to the screen's since #130, which no browser has checked), forced colours, every modal
  overlay on iOS Safari 26.3 in the iOS Simulator, and axe over the whole
  Hosts list with 300 Hosts. A screen reader pass has not been run, by the
  Owner's choice.
