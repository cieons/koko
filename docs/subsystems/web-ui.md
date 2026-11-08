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
`localStorage`, and stands in for the `showModal` and `close` of a
`<dialog>`, which jsdom lacks; a test sends Escape as the dialog's `cancel`
event. A test of a capability's pages runs the whole app through
[`test/pages.tsx`](../../web/src/test/pages.tsx): `renderAt(path)` renders it
at a path, `actAs(api, user, deviceSession?)` makes `user.current` answer
that User, on a Device Session of their laptop unless another is given, and
`tableRows(name)` reads the cells of the table that name labels.

## Verifying

- [`internal/server/webui/webui_test.go`](../../internal/server/webui/webui_test.go):
  every file of the build output on disk is embedded; a plain `go build`
  (`go run` of `testdata/page` with an overlay that hides the build output)
  serves the placeholder; the placeholder says how to build the web UI.
- [`internal/server/webui_test.go`](../../internal/server/webui_test.go): the
  start log says `web_ui_built`.
- [`internal/shared/api/typescript_test.go`](../../internal/shared/api/typescript_test.go):
  the generated files are up to date, every registered error code is a
  constant of the file that declares it, and the generator's mapping and
  refusals.
- The shell's page tests are listed in [the web UI shell](web-shell.md#verifying),
  those of the Spaces, People and Devices pages in
  [their reference](people-pages.md#verifying), and those of the Hosts list
  in [the record's pages](record-pages.md#verifying).
