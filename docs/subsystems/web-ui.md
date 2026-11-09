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

## Astryx

The web UI is built on Astryx, in its Neutral theme ([ADR
0045](../adr/0045-the-web-ui-is-built-on-astryx.md)); how the app sits in
its providers is [the web UI shell](web-shell.md#providers).

- **The packages**, each at an exact version:

  | Package | In | Why |
  | --- | --- | --- |
  | `@astryxdesign/core` | `dependencies` | the components, `reset.css` and `astryx.css` |
  | `@astryxdesign/theme-neutral` | `dependencies` | the Neutral theme: `theme.css` and the built theme object; it brings `lucide-react` for icons |
  | `@stylexjs/stylex` | `dependencies` | the peer dependency of `@astryxdesign/core`, on its `0.19` line; no StyleX build step, since the CSS is prebuilt |
  | `@astryxdesign/cli` | `devDependencies` | component docs and the agent docs for agents working in `web/`; never in the build |
  | `axe-core` | `devDependencies` | the accessibility check of page tests |

  Not adopted: `@astryxdesign/build`, Tailwind, canary releases, `vega`
  and `charts`, and `astryx swizzle`.
- **Install scripts.** `allowScripts` sets `@astryxdesign/core` and
  `@astryxdesign/cli` to `false`: each ships a `postinstall` that only prints
  a hint. No dependency's install script runs.
- **Provenance.** CI's check job runs `npm audit signatures` after `npm ci`,
  and fails on a registry signature or provenance attestation that does not
  verify. It needs the registry, so `make check` does not run it.
- **Components** are imported from their subpaths
  (`@astryxdesign/core/Button`), never the package's root, so the build
  leaves out the rest; `astryx.css` is taken whole.
- **The CSP**, `default-src 'self'` ([the web front](web-front.md)), refuses
  an inline script, a `<style>` element and a `data:` URL, so the web UI
  needs none: `index.html` runs no inline script, Vite inlines no asset
  (`assetsInlineLimit: 0`), and the theme is the built one, whose CSS is a
  file. Astryx's layers (Dialog, Popover, Menu, Tooltip) would inject their
  slide rules (`@position-try`) in a `<style>` element unless the document
  holds an element of its id already, so
  [`web/public/astryx-layer-rules.css`](../../web/public/astryx-layer-rules.css)
  holds those rules and `index.html` links it under that id,
  `astryx-layer-slide-rules`; Vite copies `public/` as it is, keeping the
  link. `npm run astryx-layer-rules` writes the file from the pinned core.
  Astryx's code blocks and date inputs inject styles too, and are not used
  yet; a page that adopts one checks it under the CSP first.
- **An upgrade** is a commit of its own: it bumps the three `@astryxdesign`
  packages together (the theme pins core exactly, the CLI is at core's
  version), reviews what `npx astryx upgrade --from <old version>`
  proposes, runs `npm run astryx-layer-rules`, refreshes the agent docs
  (below) in the same commit, and passes the page tests, the size budget
  and the real-browser check. koko stays on its pinned version until such a
  commit; nothing moves it by itself.
- **Agents working in `web/`** run the CLI at the pinned version there:
  `npx astryx component <Name> --json` for a component's props,
  `npx astryx build "<the page>"` for the template to start from,
  `npx astryx template --list --type page` and `npx astryx template <name>`
  for the templates, and `npx astryx search "<words>"`. `npx astryx init
  --features agents --agent-docs-path AGENTS.md CLAUDE.md` wrote Astryx's
  managed block into [`web/AGENTS.md`](../../web/AGENTS.md) and
  [`web/CLAUDE.md`](../../web/CLAUDE.md), both committed; an edit goes
  outside its markers, and an upgrade refreshes it with the same command.
  `init --user` and its other features are never run, and the repository's
  own `AGENTS.md` and `CLAUDE.md` stay koko's.

## Size

`kokod` embeds the build uncompressed, so it has a budget: at most 3 MiB
on disk, and at most 450 KiB gzipped for the first load, the files a
browser loads to show the first page (`index.html`, the scripts and
stylesheets it names, and the fonts their stylesheets name). The reverse
proxy compresses them for browsers. The size test logs each size, and a
commit that changes the build much records them in its body.

## Building and embedding

- `make webui` type-checks the app (`tsc`) and builds it with Vite into
  `internal/server/webui/site/app/`, which git ignores. `make build` and
  `make check` run it first (`check` runs its steps in order, even under
  `-j`), so the binaries they build carry the app.
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
  `api.ts`. A type from another file is imported. An operation or a
  registered error code that no `var` of package api declares as an `Op` or
  `Code` literal is refused.
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
the test ends. `api.on(SpaceList, handler)` answers an operation, typed by
the generated types, and `api.calls` records each request (method, path,
parsed body and headers); `api.requests(SpaceAdd)` is the bodies of one
operation's calls. A handler may answer later, with a promise, and
refuses a call by throwing `refusal(code, status)`; an operation without a
handler answers
`unknown_operation`, as the koko server does. `api.drop(n)` fails the next `n`
requests as a dropped connection does, and `api.drop()` every one, as when
`koko web` stopped, until `api.drop(0)`. So a page test runs the web UI's own
client ([the shell](web-shell.md#calls)), with its retry and
`Idempotency-Key`. The setup puts each test back at `/` with an empty
`localStorage` and the system in light mode, gives the document
`index.html`'s title and its link to Astryx's layer rules, and stands in
for what jsdom lacks: the `showModal` and `close` of a `<dialog>` (a test sends Escape as
the dialog's `cancel` event); `matchMedia`, whose colour scheme
[`setSystemScheme`](../../web/src/test/media.ts) changes and every other
query of which matches nothing; and a `ResizeObserver` and an
`IntersectionObserver` that observe nothing, since jsdom lays nothing out.
A test of a capability's pages runs the whole app, inside the same
[providers](web-shell.md#providers) as `main.tsx`'s, through
[`test/pages.tsx`](../../web/src/test/pages.tsx): `renderAt(path)` renders it
at a path, `actAs(api, user, deviceSession?)` makes `user.current` answer
that User, on a Device Session of their laptop unless another is given, and
`tableRows(name)` reads the cells of the table that name labels.
`await expectAccessible()` ([`test/axe.ts`](../../web/src/test/axe.ts)) runs
axe-core on what is rendered and fails naming each violation of WCAG 2.2 A
and AA, all rules but `color-contrast`, which jsdom cannot compute and the
real-browser check covers.

## Static checks

[`static.test.ts`](../../web/src/test/static.test.ts) reads the app's own
files and fails on an import of `@astryxdesign/core`'s root, a colour
literal in koko's CSS (hex, a colour function or a named colour), an import
of the theme's `fonts.css`, a URL of another site in any CSS file or in
`index.html`, or an inline script or event handler in `index.html`, and
when `index.html` lacks the viewport or `color-scheme` tag, or the link to
Astryx's layer rules under the id the pinned core looks for, or the file
differs from what that core would inject. It reads every CSS file as
written, which Vitest gives a test only for a `?raw` import
(`vite.config.ts`), and fails when one reads empty.

## Verifying

- [`internal/server/webui/webui_test.go`](../../internal/server/webui/webui_test.go):
  every file of the build output on disk is embedded; a plain `go build`
  (`go run` of `testdata/page` with an overlay that hides the build output)
  serves the placeholder; the placeholder says how to build the web UI;
  and, when the build is embedded, `TestTheBuiltWebUIStaysWithinItsSizeBudget`
  fails above [the budget](#size), logging each size.
- [`static.test.ts`](../../web/src/test/static.test.ts): the static checks
  above; [`axe.test.tsx`](../../web/src/test/axe.test.tsx): the
  accessibility check passing on the first page and on an Astryx dialog,
  and naming a violation.
- The real-browser check, an acceptance check rather than a test (spec
  #108: "Phones, dark mode and the real browser"): every page a change
  touches, at 360×740, 390×844 and 1280×800, light and dark, English and
  Chinese, with axe-core's `color-contrast` too, no CSP violation and no
  request to another origin.
- [`internal/server/webui_test.go`](../../internal/server/webui_test.go): the
  start log says `web_ui_built`.
- [`internal/shared/api/typescript_test.go`](../../internal/shared/api/typescript_test.go):
  the generated files are up to date, every registered error code is a
  constant of the file that declares it, and the generator's mapping and
  refusals.
- The shell's page tests are listed in [the web UI shell](web-shell.md#verifying),
  those of the Spaces, People and Devices pages in
  [their reference](people-pages.md#verifying), those of the Host and
  Service pages in [the record's pages](record-pages.md#verifying), and
  those of the Secrets page in [Secrets](secrets.md#verifying).
