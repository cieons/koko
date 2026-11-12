# The web UI build

How the web UI is built, embedded and typed. It is a React and TypeScript
single-page app in [`web/`](../../web), built by Vite and never committed
([ADR 0023](../adr/0023-the-web-ui-is-react-built-by-make-and-never-committed.md)).
Serving it on `web@koko` is the [web front](web-front.md)'s job.

## Toolchain

- **Node** is the version `devEngines.runtime` names in
  [`web/package.json`](../../web/package.json), and the package manager is
  npm, which comes with Node. Under another Node or package manager, npm
  refuses to install or run a script (`EBADDEVENGINES`). CI installs Node with
  `actions/setup-node`, which reads the same field.
- **Dependencies** are exact versions, locked in `web/package-lock.json`. Make
  runs `npm ci` only when `package.json` or the lockfile is newer than
  `web/node_modules/.package-lock.json`. npm runs no dependency's install
  script unless `allowScripts` in `package.json` allows it.
- [`web/go.mod`](../../web/go.mod) makes `web/` a Go module of its own, so
  `./...` skips it, and with it the `.go` files some npm packages ship.

## HeroUI and Tailwind

The web UI is built on HeroUI v3, React Aria Components styled with
Tailwind CSS 4, in koko's own theme, Obsidian ([ADR
0046](../adr/0046-the-web-ui-is-built-on-heroui.md)); what every screen
shows, and its values, is [the web UI's design](../web-design.md). How the
app sits in its providers is [the web UI shell](web-shell.md#providers).

- **The packages**, each at an exact version:

  | Package | In | Why |
  | --- | --- | --- |
  | `@heroui/react` 3.2.7 | `dependencies` | the components, React Aria Components composed and styled |
  | `@heroui/styles` 3.2.7 | `dependencies` | HeroUI's stylesheets: its base, one file per component, its default theme's variables, utilities and variants |
  | `react-aria-components` 1.22.1 | `dependencies` | the primitives HeroUI composes, and those koko uses directly, such as `Link` and `RouterProvider`; the version HeroUI is built against, so there is one copy |
  | `react-aria` 3.53.1 | `dependencies` | React Aria's hooks, at the version `react-aria-components` brings, so there is one copy; its `usePress` and `usePreventScroll` are what the CSP meets (below) |
  | `@internationalized/date` 3.12.4 | `dependencies` | the calendar dates of the date field's value (`ui/DateInput.tsx`) |
  | `lucide-react` 1.70.0 | `dependencies` | the icons koko names itself, such as each place's in the phone's bar |
  | `@wterm/react`, `@wterm/dom`, `@wterm/core`, `@wterm/ghostty` 0.5.10 | `dependencies` | the web terminal's emulator, wterm on its ghostty core ([ADR 0054](../adr/0054-the-web-terminal-is-wterm-on-its-ghostty-core-behind-one-component-of-kokos-own.md)), imported by `src/terminal/emulator/` alone; `@wterm/dom`'s renderer is patched (below) |
  | `patch-package` 8.0.1 | `devDependencies` | applies `web/patches/` at install |
  | `tailwindcss` 4.3.5, `@tailwindcss/vite` 4.3.5 | `devDependencies` | Tailwind, the web UI's one styling vocabulary, and its Vite plugin, which builds the stylesheet; never in the build itself |
  | `@react-aria/optimize-locales-plugin` 2.1.0 | `devDependencies` | keeps React Aria's own strings for English and Simplified Chinese alone (`vite.config.ts`) |
  | `axe-core` | `devDependencies` | the accessibility check of page tests |

  HeroUI v3 needs no animation library: its motion is CSS.
- **Install scripts.** `allowScripts` in `package.json` lists `fsevents`,
  the one dependency that ships one, as `false`, so no dependency's install
  script runs. The web UI's own `postinstall` runs `patch-package
  --error-on-fail`, which applies each file in `web/patches/` to the package
  it names, so `make webui` and CI build the patched package.
- **wterm's renderer patch.** `@wterm/dom` 0.5.10 writes each row of the
  terminal as HTML whose runs carry `style` attributes, which kokod's CSP
  refuses, so the terminal drew garbled.
  `patches/@wterm+dom+0.5.10.patch` has the rows carry their styles in
  `data-term-style` and sets them through CSSOM after the parser (ADR
  0054). The install fails when the patch no longer applies, so an upgrade
  of wterm makes it again, or drops it once wterm ships the change.
  `src/terminal/emulator/wterm.test.ts` runs the real renderer and core
  under jsdom and fails on any `style` attribute handed to the HTML parser;
  the page tests' CSP watch cannot see one, since jsdom enforces no CSP.
- **Provenance.** CI's check job runs `npm audit signatures` after `npm ci`,
  and fails on a registry signature or provenance attestation that does not
  verify. It needs the registry, so `make check` does not run it.
- **The theme.** [`src/theme/heroui.css`](../../web/src/theme/heroui.css)
  imports Tailwind, HeroUI's base, the style file of each HeroUI component
  in use, in the order HeroUI's own `components/index.css` gives them, then
  HeroUI's default theme, utilities and variants; a page that takes up
  another component adds its file there.
  [`src/theme/theme.css`](../../web/src/theme/theme.css) imports it, then
  writes Obsidian's tokens on HeroUI's variable names, light on `:root`
  and dark on `.dark` and `[data-theme="dark"]`, the type roles (`t-display`
  to `t-code`), the panel (`.sheet`), the island's shadow (`--island-shadow`)
  that raises the current place, the rail's widths (`--rail-open`,
  `--rail-folded`) and the room it takes (`--rail-w`, by the frame's
  `data-rail`) and koko's
  adjustments to HeroUI's parts. Two of its rules are for keyboard and
  contrast-theme users. Under `ui/phone.ts`'s width, the root's scroll
  padding keeps a focused control clear of the phone's sticky top row and
  its bar at the foot. Under `forced-colors: active`, where
  the browser drops shadows and paints fills in the page's colour, the
  focus ring is a `Highlight` outline; panels, overlays, the rail's end
  edge and a switch's track
  get a `CanvasText` edge; the current place, the chosen tab and the chosen
  segment are filled with `SelectedItem`, the tab's indicator hidden; and a
  checked box, a chosen radio and a switch turned on are filled with
  `Highlight`, their mark in `HighlightText`. It is the one file that
  writes a colour; every other rule and
  class uses the tokens. Geist and Geist Mono, their Latin subsets as
  variable WOFF2 files, are bundled in `src/assets/fonts/` with their SIL
  Open Font License files; Chinese text uses the system's fonts.
- **The CSP**, `default-src 'self'` with one hash in `style-src` and
  `'wasm-unsafe-eval'` in `script-src`, which lets the web terminal's
  emulator compile WebAssembly and nothing else (ADR 0054; [the web
  front](web-front.md#the-headers-every-answer-carries)), refuses an inline
  script, a `<style>` element but that one, and a `data:` URL, so the web UI
  needs none: `index.html` runs no inline script, and Vite inlines no asset
  (`assetsInlineLimit: 0`). React Aria adds two `<style>` elements of its
  own: `usePress` adds its rule for pressable elements unless the document
  holds an element whose id is `react-aria-pressable-style`, so
  `index.html` holds a `<meta>` of that id and `theme.css` holds the rule
  (`touch-action: pan-x pan-y pinch-zoom`); and on iOS Safari,
  `usePreventScroll` adds `@layer { * { overscroll-behavior: contain } }`
  while an overlay is open, which kokod's CSP allows by its hash, the path
  ADR 0046 records after a run in the iOS Simulator. A page test fails on
  any CSP violation (below), so an upgrade that adds a third is caught.
- **An upgrade** is a commit of its own: it moves `@heroui/react` and
  `@heroui/styles` together, `react-aria-components`, `react-aria` and
  `@internationalized/date` to the versions the new HeroUI is built
  against, and `tailwindcss` with `@tailwindcss/vite`; it checks the
  component files `heroui.css` imports against HeroUI's new
  `components/index.css`, and the static checks against the new React
  Aria (a changed `<style>` text needs a new hash in kokod's CSP, and a
  hash in this reference and ADR 0046); and it passes the page tests, the
  size budget and the real-browser check, on iOS Safari too. koko stays on
  its pinned versions until such a commit; nothing moves them by itself.
- **Agents working in `web/`** read [the web UI's design](../web-design.md)
  and HeroUI's own documentation (`https://heroui.com/react/llms.txt`),
  checked against the pinned package's types, as
  [`web/AGENTS.md`](../../web/AGENTS.md) says; there is no CLI.

## Size

`kokod` embeds the build uncompressed, so it has a budget: at most 3 MiB
on disk, and at most 450 KiB gzipped for the first load, the files a
browser loads to show the first page (`index.html`, the scripts and
stylesheets it names, and the fonts their stylesheets name). The reverse
proxy compresses them for browsers. Every page keeps its own code out of
the first load: its area's `routes.ts` gives it through
[`lazyPage`](web-shell.md#pages-and-routing), a chunk of its own, and a
part only forms use, such as the date field with its calendar
(`ui/DateInput.tsx`), is imported by those forms alone, so it loads with
them. The size test logs each size, and a commit that changes the build
much records them in its body. With the frame alone (#121) the first load
was 237 KB gzipped and the build 0.78 MB on disk.

## Building and embedding

- `make webui` type-checks the app (`tsc`) and builds it with Vite into
  `internal/server/webui/site/app/`, which git ignores. `make build` and
  `make check` run it first (`check` runs its steps in order, even under
  `-j`), so the binaries they build carry the app. Vite writes every
  script, stylesheet, font and other imported file under `assets/`, with a
  hash of its content in its name, and the web front lets a browser keep
  those for a year; `index.html` and the files beside it are not cached
  that way ([the web
  front](web-front.md#the-web-uis-files-and-the-browsers-cache)).
- [`internal/server/webui`](../../internal/server/webui/webui.go), in the
  server-only group, embeds `site/`. `webui.FS()` is the files to serve, with
  `index.html` at the root: the built app when `site/app/index.html` was there
  at build time, else the committed placeholder page
  (`site/placeholder/index.html`), which says how to build the web UI.
  `webui.Built()` says which; the koko server logs it at start as
  `web_ui_built`.
- A plain `go build` or `go install` needs no Node and embeds the placeholder.
  Only Linux builds link the koko server, and so the web UI
  ([ADR 0024](../adr/0024-the-server-and-the-cli-are-separable.md)).

## Generated types

- Each Go file of [`internal/shared/api`](../../internal/shared/api) becomes
  one TypeScript file in [`web/src/api/`](../../web/src/api): `space.go` gives
  `space.ts`. A file holds what the API carries of that Go file's
  declarations, with their doc comments: the request, response and record
  types as `encoding/json/v2` shapes them, each operation as an `Op` value
  (`SpaceAdd`, with its name, path and whether it writes), each registered
  error code as a string constant under its Go name
  (`export const CodeUnknownSpace = "unknown_space";`), and `Op` itself in
  `api.ts`. The web front's own `/auth/` endpoints, which are no operations,
  come as `AuthEndpoint` values in `signin.ts` (`AuthSignIn`, with its path
  and its request and response types), from the `api.AuthEndpoint`
  literals `api.AuthEndpoints` lists. A type from another file is imported.
  An operation, a registered error code or an `/auth/` endpoint that no
  `var` of package api declares as an `Op`, `Code` or `AuthEndpoint`
  literal is refused.
- The generator is test code,
  [`tsgen_test.go`](../../internal/shared/api/tsgen_test.go), so none of it
  ships. The files are committed, and
  [`TestTypeScriptTypes`](../../internal/shared/api/typescript_test.go) fails
  in `make test` when one is missing, stale or not generated.
  `make generate` rewrites them. The directory is the generator's alone; a
  rebase conflict in it is resolved by rule 4 of the
  [rules for working in parallel](code-layout.md#rules-for-working-in-parallel).
- A field's JSON name is its tag's, else its Go name. `omitzero` and
  `omitempty` make it optional, and an optional pointer is never `null`.
  Slices and maps are never `null` (json/v2), `time.Time` is a string, an
  Attribute value (`record.Value`) is `string | number`, a number being an
  exact decimal a JavaScript number holds exactly ([ADR
  0029](../adr/0029-a-number-attribute-is-a-decimal-a-javascript-number-holds.md)),
  and an empty struct is `Record<string, never>`. The generator refuses any other
  shape, naming it, rather than write it wrong; an API type that needs one
  extends the generator (`tsType` and `tsFields`).

## Testing pages

Vitest and React Testing Library run the pages in jsdom; `make test` runs
them after the Go tests, under the same temporary HOME. A page test gets
[`fakeApi()`](../../web/src/test/fakeApi.ts), which stands in for `fetch` until
the test ends. `api.on(SpaceList, handler)` answers an operation, and
`api.on(AuthSignIn, handler)` an `/auth/` endpoint, typed by the generated
types, and `api.calls` records each request (method, path, parsed body and
headers); `api.requests(SpaceAdd)` is the bodies of one operation's calls.
A handler may answer later, with a promise, and refuses a call by
throwing `refusal(code, status)`, or `refusal(code, status, retryAfter)`
for one whose answer carries `Retry-After`, or `batchRefusal({1:
"relay_in_use"})` for a batch refused for the records at those indexes,
as [ADR 0050](../adr/0050-a-write-of-several-records-is-applied-all-or-none.md)
has it; an operation without a
handler answers
`unknown_operation`, as the koko server does. `api.drop(n)` fails the next `n`
requests as a dropped connection does, and `api.drop()` every one, as when
`koko web` stopped, until `api.drop(0)`. So a page test runs the web UI's own
client ([the shell](web-shell.md#calls)), with its retry and
`Idempotency-Key`. The setup ([`test/setup.ts`](../../web/src/test/setup.ts))
puts each test back at `/` with no toast and nothing React Aria's live
announcer announced left from the one before, an empty `localStorage`, the system in light mode, a desktop's viewport and
`<html>` without a mode; gives the
document `index.html`'s title and its pressable `<meta>`; and stands in for
what jsdom lacks: `matchMedia`, whose colour scheme
[`setSystemScheme`](../../web/src/test/media.ts) changes and whose width,
1280 pixels unless `setViewportWidth` makes it a phone's, answers the width
queries Tailwind's breakpoints and the web UI ask, in pixels or rems
(`(width < 768px)`, `(width >= 48rem)`, `(max-width: 640px)`,
`(min-width: 40rem)`), and reduced motion only within `withoutMotion`,
which the setup clears the toasts in so HeroUI removes them at once,
every other query matching nothing; a
`ResizeObserver` and an `IntersectionObserver` that observe nothing,
`scrollIntoView` and `document.elementsFromPoint` that do nothing, since
jsdom lays nothing out; and `inert`, which every supported browser has and
React Aria sets on the page behind an open overlay. It also fails every
test that leaves a CSP violation ([`test/csp.ts`](../../web/src/test/csp.ts)):
each `securitypolicyviolation` event the document hears, and each
`<style>` element, inline script or inline event handler added to the
page, which jsdom, enforcing no CSP, would let through. React Aria's
`usePress` adds no `<style>` under `NODE_ENV` `test`, so the CSP watch's
own test stubs it to check `index.html`'s `<meta>`.
A test of a capability's pages runs the whole app, inside the same
[providers](web-shell.md#providers) as `main.tsx`'s, through
[`test/pages.tsx`](../../web/src/test/pages.tsx). Before a test file's
tests, it loads every page's chunk (`loadPage`), since a cold import on a
busy runner can take longer than a test's wait for what the page shows
(#119). `renderAt(path)` renders the app at a path, `actAs(api, user,
deviceSession?)` makes `user.current` answer that User, on a Device Session
of their laptop unless another is given, `tableRows(name)` reads the cells
of the table that name labels, `alerts()`, `findAlert()` and `statuses()`
read the alerts and statuses that say something (React Aria keeps empty
live regions), `frameButton(name)` finds a button of the frame's banner
(the rail on a desktop, the top row on a phone), and
`chooseInFrame(menu, option)` chooses a language or a mode.
[`test/controls.ts`](../../web/src/test/controls.ts) drives React Aria's
controls as a person does, each checking with axe while its list is open:
`chooseRowAction(menu, action)` a row menu and `chooseOption(select,
option, offered?)` a select, each the whole page, and
`searchAndChoose(choice, typed, option, offered?)` a searchable choice,
named or given, which it focuses and types in, as the combobox opens only
on real focus, and `searchAndChooseSeveral(choice, typed, options,
{holder, offered})` a searchable choice of several, which it opens with a
press on its field, types in the search over its list and closes with
Escape. Those two check their list alone: while the list is open,
React Aria hides the rest of the page from assistive technology, and axe
would fault every control there as focusable yet hidden, though the focus
stays in the field and Tab closes the list before it moves;
[`test/clipboard.ts`](../../web/src/test/clipboard.ts)
stands in for the clipboard. React Aria's controls answer
`fireEvent.click` as a press.
`await expectAccessible()` ([`test/axe.ts`](../../web/src/test/axe.ts)) runs
axe-core on what is rendered and fails naming each violation of WCAG 2.2 A
and AA, all rules but `color-contrast`, which jsdom cannot compute and the
real-browser check covers.

## Static checks

[`static.test.ts`](../../web/src/test/static.test.ts) reads the app's own
files and fails on:

- a colour written anywhere but `src/theme/theme.css`: in another CSS file
  (hex, a colour function or a named colour) or in a Tailwind arbitrary
  value of the app's code, such as `shadow-[0_0_0_3px_#fff]`;
- a HeroUI component style imported out of the order HeroUI's own
  `components/index.css` gives, or one that file does not hold;
- a URL of another site in any CSS file or in `index.html`;
- `index.html` without the `<meta>` whose id the pinned `react-aria`'s
  `usePress` looks for, or the theme without the rule it would add;
- a `<style>` text the pinned `react-aria`'s `usePreventScroll` adds whose
  hash is not the one, and the only one, in kokod's CSP
  ([`web.go`](../../internal/server/web/web.go), which `vite.config.ts`
  lets the test read);
- an inline script or event handler in `index.html`, or `index.html`
  without the viewport, `color-scheme` and icon tags.

It reads every file as written, which Vitest gives a test only for a `?raw`
import of a CSS file (`vite.config.ts`), and fails when a CSS file reads
empty.

## Verifying

- [`internal/server/webui/webui_test.go`](../../internal/server/webui/webui_test.go):
  every file of the build output on disk is embedded; a plain `go build`
  (`go run` of `testdata/page` with an overlay that hides the build output)
  serves the placeholder; the placeholder says how to build the web UI;
  and, when the build is embedded, `TestTheBuiltWebUIStaysWithinItsSizeBudget`
  fails above [the budget](#size), logging each size.
- [`static.test.ts`](../../web/src/test/static.test.ts): the static checks
  above; [`axe.test.tsx`](../../web/src/test/axe.test.tsx): the
  accessibility check passing on the frame with a HeroUI dialog open, and
  naming a violation; [`csp.test.tsx`](../../web/src/test/csp.test.tsx): the
  CSP watch recording a `securitypolicyviolation`, a `<style>` element, an
  inline script and an inline handler, and a HeroUI button adding no
  `<style>` with `index.html`'s `<meta>` and one without it.
- The real-browser check, an acceptance check rather than a test (spec
  #108: "Phones, dark mode and the real browser"; [the design's
  checks](../web-design.md#accessibility-and-checks)): every page and
  overlay a change touches, at 360×740, 390×844, 1100×800 (the rail
  folded, and laid over the page), 1280×800 (the rail open and folded) and
  1440 wide, light and dark, English and Chinese, with axe-core's `color-contrast`
  too, no CSP violation, no request to another origin, no horizontal
  scroll at 320 pixels, and one task per page done with the keyboard
  alone; agent-browser drives CloakBrowser in Docker. An overlay's CSP
  case on iOS Safari runs in the iOS Simulator, as ADR 0046's did.
- [`internal/server/webui_test.go`](../../internal/server/webui_test.go): the
  start log says `web_ui_built`.
- [`internal/shared/api/typescript_test.go`](../../internal/shared/api/typescript_test.go):
  the generated files are up to date, every registered error code is a
  constant of the file that declares it, and the generator's mapping and
  refusals.
- The shell's page tests are listed in [the web UI shell](web-shell.md#verifying).
  The pages' tests, back with the pages (#122 to #125), are each
  listed in its page's reference: [the record's
  pages](record-pages.md#verifying), [Secrets](secrets.md#verifying) and
  [the Access and Account pages](access-pages.md#verifying).
