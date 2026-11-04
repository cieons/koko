# Which pure-Go SQLite driver keeps `make cross` at `CGO_ENABLED=0`?

Research note for issue #2. Checked on 2026-11-05 against the repositories,
their source, release metadata, sqlite.org and measurements of my own. Each
claim links to what it rests on.

## Question

koko is one Go binary that keeps its record in an embedded SQLite file
([ADR 0001](../adr/0001-go-single-binary-with-sqlite.md)). `make cross`
builds `linux/amd64`, `linux/arm64` and `darwin/arm64` with
`CGO_ENABLED=0` (`Makefile`, `CROSS_TARGETS`). Go 1.27; the standard library
and `golang.org/x` come first, and any other dependency needs a reason
([code standards](../code-standards.md#go)). Which driver should koko build
on? Candidates: `modernc.org/sqlite` and `github.com/ncruces/go-sqlite3`,
with `github.com/mattn/go-sqlite3` (cgo) as the baseline. Also checked and
set aside: `zombiezen.com/go/sqlite` ([[51]](#o1)),
`github.com/glebarez/go-sqlite` ([[52]](#o2)),
`github.com/bnema/purego-sqlite` ([[53]](#o3)),
`github.com/cvilsmeier/sqinn-go` ([[54]](#o4)).

## Summary and recommendation

Use **`modernc.org/sqlite`**, through `database/sql` only, with a DSN that
sets every default koko relies on, for example
`file:<path>?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate`.

- Both pure-Go drivers meet the hard requirement. Each builds with
  `CGO_ENABLED=0` for all three `CROSS_TARGETS`, is a full `database/sql`
  driver, bundles SQLite 3.54.1 (upstream's current release, 2026-10-16),
  and supports WAL, foreign keys, JSON and JSONB, a busy timeout and
  `_txlock=immediate` from the DSN. `mattn/go-sqlite3` does not: built with
  `CGO_ENABLED=0` it compiles to a stub that fails at runtime.
- `modernc.org/sqlite` is the conservative choice. It is v1 since 2018, has
  10,072 known importing modules against 345 for ncruces's driver, two
  named maintainers, sponsors (Tailscale among them), a security
  policy, and a release about every ten days. Its locking and WAL code is
  SQLite's own Unix OS layer, transpiled.
- `ncruces/go-sqlite3` is faster and lighter. It wins modernc's own
  benchmark game (95 points against 27), links 4 modules instead of 10, and
  turns foreign keys and a busy timeout on by default. But it is pre-1.0,
  and three of its last four minor releases broke its API. It has run
  on a translator (`wasm2go`) only since March 2026, which its author then
  called "a recent project". It replaces SQLite's OS layer with a Go VFS its
  own README calls "not as battle tested as the original". One person writes
  almost all of it.
- koko holds the record of a personal or small-team fleet. Its write rate
  is low and its queries are small, so speed is not the constraint; the
  file must not break. The maturity of the storage path decides, and that
  favours modernc. The choice is cheap to reverse: both drivers read the
  same `_pragma=...` and `_txlock=...` DSN keys.
- The concurrency rule is SQLite's, not the driver's. A deferred
  transaction that reads and then writes fails with `SQLITE_BUSY` at once,
  whatever the busy timeout. In my measurement 948 to 1,573 of 1,600 such
  transactions failed on every driver; with `_txlock=immediate` none did.
  koko must begin write transactions as `IMMEDIATE`.

## Comparison table

| | modernc.org/sqlite | ncruces/go-sqlite3 | mattn/go-sqlite3 (baseline) |
|---|---|---|---|
| How SQLite becomes Go | C transpiled to Go (`ccgo`), on `modernc.org/libc` [[3]](#m3) | C to Wasm to Go source by `wasm2go`, since v0.33 (March 2026); wazero before [[22]](#n2) [[23]](#n3) | cgo |
| Latest release | v1.61.1, 2026-10-31 [[1]](#m1) | v0.36.0, 2026-10-29 [[21]](#n1) | v1.14.52, 2026-09-05 [[41]](#t1) |
| Releases, last 12 months | 35 tags [[1]](#m1) | 27 tags [[21]](#n1) | 20 tags [[41]](#t1) |
| API stability | v1 since 2018 [[1]](#m1) | v0; breaking changes in v0.33, v0.35, v0.36 [[24]](#n4) | v1 |
| Bundled SQLite (upstream: 3.54.1, 2026-10-16 [[61]](#s1)) | 3.54.1 [[3]](#m3) | 3.54.1 [[21]](#n1) | 3.53.4 [[41]](#t1) |
| Maintainers | two named; sponsors [[2]](#m2) | one author, 132 of 149 commits in a year [[33]](#n13) | one author, 156 of 176 commits in a year [[41]](#t1) |
| `CGO_ENABLED=0` on koko's three targets | yes; 20 targets listed [[3]](#m3); built [[81]](#e1) | yes; full locking and shared-memory WAL in CI [[25]](#n5); built [[81]](#e1) | no; runtime stub [[41]](#t1) |
| `database/sql` name | `sqlite` [[8]](#m8) | `sqlite3` [[27]](#n7) | `sqlite3` |
| Pragmas in the DSN | `_pragma=name(value)`, repeatable; mattn-style keys too [[5]](#m5) | `_pragma=name(value)`, repeatable [[27]](#n7) | own keys (`_busy_timeout`, `_foreign_keys`, ...) [[41]](#t1) |
| `_txlock` | `deferred` (default), `immediate`, `exclusive` [[5]](#m5) | same; `immediate` becomes default on `main`, unreleased [[27]](#n7) | same |
| Default busy timeout | 0 [[82]](#e2) | 60 s if no `_pragma` is given [[27]](#n7) | 5 s [[41]](#t1) |
| Busy wait | SQLite's back-off, steps up to 100 ms; ends on context [[6]](#m6) | random sleeps of at most ~2 ms; ends on context [[28]](#n8) | SQLite's default |
| Foreign keys by default ([[65]](#s5)) | off [[82]](#e2) | on (compile option) [[29]](#n9) | off [[82]](#e2) |
| JSON, `->>`, JSONB ([[66]](#s6)) | yes [[82]](#e2) | yes [[82]](#e2) | yes [[82]](#e2) |
| WAL | yes; SQLite's Unix VFS; OFD locks opt-in [[9]](#m9) | yes; Go VFS, OFD locks and `mmap` [[26]](#n6) | yes |
| Encryption at rest | none [[12]](#m12) | `adiantum` and `xts` VFS, deterministic, no integrity [[31]](#n11) | none |
| Stripped binary, over an empty `database/sql` program | +4.4 MiB (linux/amd64) [[81]](#e1) | +5.8 MiB (linux/amd64) [[81]](#e1) | +1.6 MiB (darwin/arm64, cgo) [[81]](#e1) |
| Modules linked into the binary | 10 [[86]](#e6) | 4 [[86]](#e6) | 1 [[86]](#e6) |
| Memory per idle connection | ~137 KiB [[84]](#e4) | ~387 KiB [[84]](#e4) [[30]](#n10) | ~186 KiB [[84]](#e4) |
| Compile of the generated package, per target | ~2.0 s, 0.85 GB peak [[85]](#e5) | ~3.1 s, 1.4 GB peak [[85]](#e5) | C compiler |
| Benchmarks game 26.10 score | 27 [[71]](#b1) | 95 [[71]](#b1) | 86 [[71]](#b1) |
| Known importing modules | 10,072 [[11]](#m11) | 345 (driver) [[33]](#n13) | 22,593 [[41]](#t1) |
| Licence | BSD-3-Clause; SQLite public domain [[10]](#m10) | MIT; translation MIT-0 [[32]](#n12) | MIT [[41]](#t1) |

Every licence here is permissive and fits koko's Apache-2.0.

## Detail per candidate

### modernc.org/sqlite

- <a id="m1"></a>[1] Latest version v1.61.1, 2026-10-31; v1.61.0
  (2026-10-21) upgraded to SQLite 3.54.1. 35 tags between 2025-11-05 and
  2026-11-05. `v1.0.0` was tagged 2018-11-09; the first commit is from
  2017-04-21 (module proxy
  <https://proxy.golang.org/modernc.org/sqlite/@latest>, git tags of
  <https://gitlab.com/cznic/sqlite>,
  <https://gitlab.com/cznic/sqlite/-/blob/master/CHANGELOG.md>).
- <a id="m2"></a>[2] `GOVERNANCE.md`: Jan Mercl is primary maintainer, Ian
  Chechin maintainer; GitLab is canonical, GitHub a mirror. Commits in the
  last year: Jan Mercl 212 (171 under his name, 41 as `cznic`), Ian
  Chechin 53, Josh Bleecher Snyder 22, others. The README lists Tailscale
  as "Enterprise Infrastructure Tier Sponsor" and exe.dev and October
  Swimmer below it.
  `SECURITY.md`: private reporting, GitHub advisories and the Go
  vulnerability database; "The latest release only" is supported. Last
  commit 2026-11-02
  (<https://gitlab.com/cznic/sqlite/-/blob/master/GOVERNANCE.md>,
  <https://gitlab.com/cznic/sqlite/-/blob/master/SECURITY.md>,
  <https://gitlab.com/cznic/sqlite/-/blob/master/README.md>).
- <a id="m3"></a>[3] `doc.go`: "a sql/database driver using a CGo-free port
  of the C SQLite3 library". Its platform table lists 20 GOOS/GOARCH pairs,
  each with SQLite 3.54.1, including `darwin/arm64`, `linux/amd64` and
  `linux/arm64`. The transpiled C lives in `lib/`, generated by
  `modernc.org/libsqlite3`
  (<https://gitlab.com/cznic/sqlite/-/blob/v1.61.1/doc.go>).
- <a id="m4"></a>[4] `go.mod`: `go 1.26.0`; requires `github.com/google/pprof`,
  `golang.org/x/sys`, `modernc.org/fileutil`, `modernc.org/libc v1.79.0`,
  `modernc.org/mathutil`, and indirectly `go-humanize`, `google/uuid`,
  `go-isatty`, `ncruces/go-strftime`, `remyoudompheng/bigfft`,
  `modernc.org/memory`. `doc.go`, "Fragile modernc.org/libc dependency":
  "you should use in your go.mod file the exact same version of
  modernc.org/libc as seen in the go.mod file of this repository". GitLab
  issue #177 is the case behind it: v1.29.3 failed to compile against a
  different libc ("cannot use bufLen (variable of type int) as uint64"),
  closed 2024-03-08. Every changelog entry since v1.53.0 (2026-06-21) that
  moves libc repeats the warning
  (<https://gitlab.com/cznic/sqlite/-/blob/v1.61.1/go.mod>,
  <https://gitlab.com/cznic/sqlite/-/issues/177>).
- <a id="m5"></a>[5] `Driver.Open` doc in `driver.go`: `_pragma` values run
  "with the PRAGMA keyword prepended, as SQL text" and may repeat;
  `StrictPragmas(true)` rejects a value holding more than one statement.
  Since v1.55.0 (2026-07-20) the mattn-style keys `_busy_timeout`,
  `_foreign_keys`, `_journal_mode`, `_synchronous`, `_auto_vacuum`,
  `_query_only` are validated and applied. `_txlock`: "May be "deferred"
  (the default), "immediate", or "exclusive"". `_time_format`: "If this
  parameter is not specified, then the default String() format will be
  used, including any monotonic clock reading". `_dqs` and `_defensive` are
  opt-in; "this build compiles with neither SQLITE_TRUSTED_SCHEMA=0 nor
  SQLITE_DQS=0" (<https://gitlab.com/cznic/sqlite/-/blob/v1.61.1/driver.go>).
- <a id="m6"></a>[6] `busy.go`: the driver replaces SQLite's busy callback
  with one that keeps SQLite's retry schedule (1, 2, 5, 10, 15, 20, 25, 25,
  25, 50, 50, 100 ms, then 100 ms steps) "but gives up once the context of
  the call in progress is done" (v1.61.0). `tx.go`: a read-only
  transaction begins with plain `begin`; others with `begin <_txlock>`.
  v1.61.1: `Tx.Commit` stops waiting for a lock once the `BeginTx` context
  is done (<https://gitlab.com/cznic/sqlite/-/blob/v1.61.1/busy.go>,
  <https://gitlab.com/cznic/sqlite/-/blob/v1.61.1/tx.go>, CHANGELOG).
- <a id="m7"></a>[7] `doc.go`, "Performance": "The transpiled SQLite core runs
  slower than the same C compiled natively." CPU time per query against C
  with the same options (September 2026, linux/amd64, Go 1.27): 2.0x for an
  unindexed `ORDER BY`, 1.9x for a `GROUP BY`, 1.3x for an index walk. It
  advises bounding the pool with `SetMaxOpenConns`. v1.59.0 (2026-09-15)
  cut CPU time "by up to a third" on Linux by replacing transpiled `memcpy`
  and friends in libc with native Go (CHANGELOG).
- <a id="m8"></a>[8] `database/sql` surface: registers as `sqlite`; implements
  `driver.Connector` and the `RowsColumnType*` interfaces; `NewConnector`
  (v1.56.0) and `RegisterConnectionHook` give per-connection setup. Since
  v1.61.0 a cancelled context stops a statement "within 1000 VM steps"
  (<https://gitlab.com/cznic/sqlite/-/blob/v1.61.1/sqlite.go>, CHANGELOG).
- <a id="m9"></a>[9] Locking and WAL use SQLite's own Unix VFS, transpiled:
  `lib/` holds `os_unix.c`'s `unixLock` and `unixShmMap` as Go functions
  (`_unixLock`, `_unixShmMap`). v1.58.0 added opt-in Linux OFD locks
  (`MODERNC_SQLITE_OFD_LOCK=1`), "off by default". v1.60.0: a fault
  reading the `-shm` mapping "no longer crashes the process". v1.56.0
  patched an upstream SQLite 3.53.3 journal-rollback corruption bug before
  upstream shipped its own fix (CHANGELOG).
- <a id="m10"></a>[10] README, "Licensing": "This package is BSD-3-Clause";
  SQLite is public domain (`LICENSE-SQLITE`); the bundled `sqlite-vec` is
  MIT. `LICENSE-3RD-PARTY.md` and CycloneDX/SPDX SBOMs account for the
  transpiled C and libc's upstreams
  (<https://gitlab.com/cznic/sqlite/-/blob/master/README.md>).
- <a id="m11"></a>[11] pkg.go.dev, "Known importing modules: 10,072"
  (<https://pkg.go.dev/modernc.org/sqlite?tab=importedby>, read 2026-11-05).
- <a id="m12"></a>[12] The README, `doc.go` and CHANGELOG do not mention
  encryption. The package's `vfs` exposes a Go `fs.FS` as a read-only VFS
  (`driver.go`, `vfs` parameter).

### github.com/ncruces/go-sqlite3

- <a id="n1"></a>[21] Latest release v0.36.0, 2026-10-29: "SQLite 3.54.1".
  27 tags between 2025-11-05 and 2026-11-05, not counting `gormlite/`
  (GitHub releases API,
  <https://github.com/ncruces/go-sqlite3/releases/tag/v0.36.0>).
- <a id="n2"></a>[22] README: "It wraps a Wasm build of SQLite, and uses
  wasm2go to translate it to Go. Go and x/sys are the only required
  dependencies." Commit `1165211` "Switch from wazero to wasm2go (#362)",
  2026-03-12. v0.32.0 (2026-03-13): "likely the last version of this
  package to depend on wazero". v0.33.2 (2026-03-29): "one of the first
  versions of this package to use wasm2go. wasm2go is a recent project, so
  you may prefer to stick to previous versions, if you value stability."
  `ncruces/wasm2go` was created 2026-02-13 (MIT, latest tag v0.4.20). The
  translated SQLite is a single 160,392-line `sqlite3.go` in
  `github.com/ncruces/go-sqlite3-wasm/v6`
  (<https://github.com/ncruces/go-sqlite3/blob/main/README.md>,
  <https://github.com/ncruces/go-sqlite3/releases/tag/v0.33.2>,
  <https://github.com/ncruces/wasm2go>,
  <https://github.com/ncruces/go-sqlite3-wasm>).
- <a id="n3"></a>[23] Discussion #361 "Switching away from wazero"
  (2026-03-05): wasm2go "resolves long standing issues, such as the slow
  performance on interpreter targets, and slow startup on compiler
  targets", and seems "easier to maintain going forward than wazero (even
  considering the bus factor of 1)". A user reported "~40 seconds and 4GiB
  of RAM" to compile the generated source (2026-03-09)
  (<https://github.com/ncruces/go-sqlite3/discussions/361>).
- <a id="n4"></a>[24] Breaking changes: v0.33.2 ("This is a breaking
  change": drop the `embed` import; memory limits move from wazero config
  to a context); v0.35.0 ("This is a **breaking change**": FTS5 and R*Tree
  need `ext/fts5` and `ext/rtree`); v0.36.0 ("Breaking changes: Removed
  FCNTL_NULL_IO"). v0.34.0 moved native extensions such as Spellfix1 to
  their own import paths (GitHub release notes).
- <a id="n5"></a>[25] Wiki support matrix (updated 2026-11-03): `linux/amd64`,
  `linux/arm64` and `darwin/arm64` are tested in CI with full file locking
  and full shared-memory WAL. FreeBSD, NetBSD, OpenBSD, illumos and
  DragonFly have "reduced concurrency"; other OS/arch pairs have no locking
  (<https://github.com/ncruces/go-sqlite3/wiki/Support-matrix>).
- <a id="n6"></a>[26] `vfs/README.md`: the module "replaces the default SQLite
  VFS with a pure Go implementation ... It's also not as battle tested as
  the original." On Linux and macOS it uses OFD locks (Linux 3.15+); WAL
  shared memory uses `mmap`; the default build is "compatible with the
  standard Unix and Windows SQLite VFSes". Two VFS bugs in 2026: #404
  "Windows: concurrent WAL writes across pooled connections corrupt the
  database" (2026-07-02, closed 2026-08-01) and #411, a `VACUUM` that made
  no progress on XFS because of `fallocate` (closed 2026-07-21)
  (<https://github.com/ncruces/go-sqlite3/blob/main/vfs/README.md>,
  <https://github.com/ncruces/go-sqlite3/issues/404>,
  <https://github.com/ncruces/go-sqlite3/issues/411>).
- <a id="n7"></a>[27] `driver/driver.go` at v0.36.0: registers `sqlite3`;
  `_txlock` "deferred" (the default), "immediate", "exclusive"; a
  serializable transaction is always immediate, a read-only one always
  deferred (and runs with `PRAGMA query_only=on`). `_pragma` as in
  `?_pragma=busy_timeout(10000)`; "If no PRAGMAs are specified, a busy
  timeout of 1 minute is set." `_timefmt` defaults to RFC 3339 on write.
  On `main`, commit `e0eb6cf` "Default immediate." (2026-11-03) makes
  `immediate` the default for writable connections; no release has it yet
  (<https://github.com/ncruces/go-sqlite3/blob/v0.36.0/driver/driver.go>,
  <https://github.com/ncruces/go-sqlite3/commit/e0eb6cf>).
- <a id="n8"></a>[28] `conn.go`, `Xgo_busy_timeout`: retries until the busy
  timeout or the connection's context ends, sleeping a random 0-2 ms only
  when the last retry was under 1 ms ago. `build/busy_timeout.patch`
  replaces `sqliteDefaultBusyCallback` "so Go can handle, and interrupt,
  sqlite3_busy_timeout"
  (<https://github.com/ncruces/go-sqlite3/blob/v0.36.0/conn.go>,
  <https://github.com/ncruces/go-sqlite3-wasm/blob/main/build/busy_timeout.patch>).
- <a id="n9"></a>[29] `build/sqlite_opt.h`: `SQLITE_DEFAULT_FOREIGN_KEYS 1`,
  `SQLITE_DQS 0`, `SQLITE_THREADSAFE 0`, `SQLITE_DEFAULT_WAL_SYNCHRONOUS 1`,
  `SQLITE_OMIT_LOAD_EXTENSION`, `SQLITE_OMIT_SHARED_CACHE`,
  `SQLITE_ENABLE_SETLK_TIMEOUT 2`; FTS5 and R*Tree only in the CLI build
  (<https://github.com/ncruces/go-sqlite3-wasm/blob/main/build/sqlite_opt.h>).
- <a id="n10"></a>[30] README, "Caveats": "Because each database connection
  executes within a Wasm sandboxed environment, memory usage will be higher
  than alternatives." "Concurrency": goroutine-safe as long as one
  connection is not used by several goroutines at once.
- <a id="n11"></a>[31] `vfs/adiantum` and `vfs/xts` wrap the VFS "to offer
  encryption at rest". Adiantum's README: "The encryption offered by this
  package is fully deterministic" and "This package does not claim protect
  databases against tampering or forgery." `xts` uses "only NIST and FIPS
  140-3 approved cryptographic primitives"
  (<https://github.com/ncruces/go-sqlite3/blob/main/vfs/adiantum/README.md>,
  <https://github.com/ncruces/go-sqlite3/blob/main/vfs/xts/README.md>).
- <a id="n12"></a>[32] `LICENSE`: MIT. `go-sqlite3-wasm`: machine-translated
  code keeps its original licences; "Everything else is licensed under
  MIT-0". `go.mod`: `go 1.26.0`; requires `go-sqlite3-wasm/v6`,
  `ncruces/julianday`, `ncruces/sort`, `ncruces/wbt`, `golang.org/x/sys`;
  the second `require` block serves only `ext/`, `vfs/` and tests
  (<https://github.com/ncruces/go-sqlite3/blob/main/go.mod>).
- <a id="n13"></a>[33] Of 149 commits on `main` in the last year, 132 are by
  Nuno Cruces and 15 by Dependabot; last commit 2026-11-03. The repository
  has no `SECURITY.md` (GitHub community profile). pkg.go.dev: 345 known
  importing modules for `.../driver`, 145 for the root package
  (<https://pkg.go.dev/github.com/ncruces/go-sqlite3/driver?tab=importedby>,
  read 2026-11-05).

### github.com/mattn/go-sqlite3 (baseline)

- <a id="t1"></a>[41] v1.14.52, 2026-09-05; 20 tags in the last year;
  `sqlite3-binding.h` defines `SQLITE_VERSION "3.53.4"`; MIT; `go.mod`
  has no requirements. 156 of the 176 commits on `master` in the last
  year are by Yasuhiro Matsumoto (as `mattn` and under his name); last
  commit 2026-11-04. Default busy timeout `busyTimeout := 5000`; DSN keys
  `_busy_timeout`, `_foreign_keys`, `_journal_mode`, `_txlock` (README,
  "Connection String"). Built with `CGO_ENABLED=0` it compiles, and running
  it prints "Binary was compiled with 'CGO_ENABLED=0', go-sqlite3 requires
  cgo to work. This is a stub" (my run). pkg.go.dev: 22,593 known
  importing modules
  (<https://github.com/mattn/go-sqlite3/blob/v1.14.52/sqlite3.go>,
  <https://github.com/mattn/go-sqlite3/blob/v1.14.52/README.md>).

### Others checked and set aside

- <a id="o1"></a>[51] `zombiezen.com/go/sqlite` v1.4.2 (2025-05-23): "This
  package deliberately does not provide a `database/sql` driver"; built on
  `modernc.org/sqlite v1.37.1` and `libc v1.65.7`; ISC licence; last
  commit 2026-06-20 (Dependabot)
  (<https://github.com/zombiezen/go-sqlite/blob/main/README.md>).
- <a id="o2"></a>[52] `github.com/glebarez/go-sqlite` v1.23.0 (2026-08-06),
  the first release since v1.22.0 (2023-12-27): "a fork of cznic/sqlite
  with changes to make it compatible with GORM", now on modernc v1.55.0.
  It trails modernc and adds nothing koko needs
  (<https://github.com/glebarez/go-sqlite>).
- <a id="o3"></a>[53] `github.com/bnema/purego-sqlite`: "Linux only", needs
  `libsqlite3.so` at runtime, "This is early stage". Not self-contained,
  no `darwin/arm64` (<https://pkg.go.dev/github.com/bnema/purego-sqlite>).
- <a id="o4"></a>[54] `github.com/cvilsmeier/sqinn-go`: "starts Sqinn as a
  child process" and talks to it over stdin/stdout; its own API, not
  `database/sql` (<https://github.com/cvilsmeier/sqinn-go>).

### SQLite behaviour every driver inherits

- <a id="s1"></a>[61] SQLite 3.54.1 was released 2026-10-16, 3.53.4 on
  2026-07-24 (<https://sqlite.org/chronology.html>).
- <a id="s2"></a>[62] "If the first statement after BEGIN DEFERRED is a
  SELECT, then a read transaction is started. Subsequent write statements
  will upgrade the transaction to a write transaction if possible, or
  return SQLITE_BUSY." "IMMEDIATE causes the database connection to start a
  new write immediately"
  (<https://sqlite.org/lang_transaction.html>).
- <a id="s3"></a>[63] `SQLITE_BUSY_SNAPSHOT` (517) "occurs on WAL mode
  databases when a database connection tries to promote a read transaction
  into a write transaction but finds that another database connection has
  already written" (<https://sqlite.org/rescode.html#busy_snapshot>).
- <a id="s4"></a>[64] WAL: "readers do not block writers and a writer does not
  block readers"; "there can only be one writer at a time"; "WAL does not
  work over a network filesystem"; with `synchronous=NORMAL` writers omit
  the sync on commit and trade durability after power loss
  (<https://sqlite.org/wal.html>).
- <a id="s5"></a>[65] Foreign key enforcement "is disabled by default" and is
  turned on per connection (<https://sqlite.org/foreignkeys.html>).
- <a id="s6"></a>[66] "The JSON functions and operators are built into SQLite
  by default, as of SQLite version 3.38.0"; JSONB since 3.45.0
  (<https://sqlite.org/json1.html>).

So for koko: begin every write transaction `IMMEDIATE` ([[62]](#s2)), keep
reads in read-only transactions (both pure-Go drivers then begin them
deferred, per [[6]](#m6) and [[27]](#n7)), and turn foreign keys on in the
DSN ([[65]](#s5)).

### Published benchmarks

- <a id="b1"></a>[71] "The SQLite Drivers 26.10 Benchmarks Game",
  `modernc.org/sqlite-bench` v1.1.17, results committed 2026-10-30 by Jan
  Mercl, modernc's maintainer. A fork of cvilsmeier's benchmark; drivers
  `mattn@v1.14.52`, `modernc@v1.61.0`, `ncruces@v0.35.6`; 16 builders.
  Scorecard (one point per fastest result): ncruces 95, mattn 86, modernc
  27; "the scores fluctuate as much as about +/- 10 points". On
  `linux/amd64-0` (Ryzen 9 3900X), inserting 1 million rows took mattn
  2398 ms, ncruces 3018 ms, modernc 3501 ms; querying them 1200, 945,
  1124 ms. On `linux/arm64` (Raspberry Pi 5) the same insert took 3128,
  4593, 6427 ms (<https://pkg.go.dev/modernc.org/sqlite-bench#section-readme>,
  <https://gitlab.com/cznic/sqlite-bench>).
- <a id="b2"></a>[72] `cvilsmeier/go-sqlite-bench`, run 2026-03-23 by
  C. Vilsmeier, author of `sqinn-go`, one i7-1165G7 on Debian 12, with
  `modernc v1.47.0`, `ncruces v0.33.0` (an early wasm2go build) and
  `mattn v1.14.37`. "We cannot declare a clear winner, it all depends on
  the use case." Its "Real" test (users, articles, comments, one
  transaction per user) inserted in 1586 ms (mattn), 1759 (modernc), 1469
  (ncruces). It predates modernc's libc speed-up in v1.59.0
  (<https://github.com/cvilsmeier/go-sqlite-bench>).

### My own measurements

Apple M4 Pro, 12 cores, 48 GiB, macOS, go1.27.1, on 2026-11-05. The
programs are scratch code, not committed. Drivers: `modernc.org/sqlite
v1.61.1` (with `modernc.org/libc v1.79.0`), `github.com/ncruces/go-sqlite3
v0.36.0`, `github.com/mattn/go-sqlite3 v1.14.52`.

- <a id="e1"></a>[81] Binary size. A 20-line program opens `:memory:` and
  prints `sqlite_version()`; the baseline is the same program with no
  driver import. Command:
  `GOOS=<os> GOARCH=<arch> CGO_ENABLED=0 go build -trimpath -ldflags='-s -w'`;
  mattn with `CGO_ENABLED=1`, `darwin/arm64` only. All six pure-Go builds
  succeeded.

  | | linux/amd64 | linux/arm64 | darwin/arm64 |
  |---|---|---|---|
  | no driver | 2.0 MiB | 1.9 MiB | 2.0 MiB |
  | modernc | 6.4 MiB (+4.4) | 6.3 MiB (+4.4) | 6.2 MiB (+4.3) |
  | ncruces | 7.8 MiB (+5.8) | 7.4 MiB (+5.5) | 7.5 MiB (+5.5) |
  | mattn (cgo) | - | - | 3.6 MiB (+1.6) |

  modernc's build includes FTS5, R*Tree and the session extension; ncruces
  leaves FTS5 and R*Tree to separate imports [[24]](#n4).
- <a id="e2"></a>[82] Defaults and features, on a file database with a plain
  `file:<path>` DSN, then with the tuned DSN from the summary. Run natively
  on `darwin/arm64`, in Docker (`debian:trixie-slim`) on `linux/arm64`,
  and for the two pure-Go drivers on `linux/amd64` under Docker emulation.
  Same results on every platform run:

  | | modernc | ncruces | mattn |
  |---|---|---|---|
  | `PRAGMA busy_timeout` | 0 | 60000 | 5000 |
  | `PRAGMA foreign_keys` | 0 | 1 | 0 |
  | tuned DSN gives WAL, FK on, 5000 ms | yes | yes | yes (own keys) |
  | FK violation rejected | yes | yes | yes |
  | `json_extract`, `->>`, `json(jsonb(...))`, `json_each` | yes | yes | yes |

  `PRAGMA compile_options`: modernc has `THREADSAFE=1`,
  `DEFAULT_WAL_SYNCHRONOUS=2`, `ENABLE_FTS5`, `ENABLE_RTREE`,
  `ENABLE_SESSION`; ncruces `THREADSAFE=0`, `DEFAULT_FOREIGN_KEYS`, `DQS=0`,
  `DEFAULT_WAL_SYNCHRONOUS=1`, `OMIT_LOAD_EXTENSION`.
- <a id="e3"></a>[83] Concurrent writers. 8 goroutines on one `*sql.DB`
  (`SetMaxOpenConns(8)`), WAL, busy timeout 5000 ms; each runs 200
  transactions of `SELECT count(*)`, then `INSERT`, then `COMMIT`.

  | committed of 1,600 | modernc | ncruces | mattn |
  |---|---|---|---|
  | deferred, darwin/arm64 | 133-142 | 270-324 | 217-239 |
  | deferred, linux/arm64 | 68-131 | 446-652 | - |
  | deferred, linux/amd64 (emulated) | 27-95 | 465-591 | - |
  | immediate, all runs | 1,600 | 1,600 | 1,600 |

  Every failure was "database is locked" at the `INSERT`, as SQLite
  documents ([[62]](#s2)); modernc shows the codes, `SQLITE_BUSY` (5) and
  `SQLITE_BUSY_SNAPSHOT` (517) ([[63]](#s3)). Time for the
  1,600 immediate transactions: on `darwin/arm64` modernc 198-203 ms,
  ncruces 79-85 ms, mattn 63-113 ms; on `linux/arm64` modernc 1.15-1.45 s
  over four runs, ncruces 46-51 ms. The likely cause is the busy wait
  ([[6]](#m6) against [[28]](#n8)); I did not profile it.
- <a id="e4"></a>[84] Throughput, startup and memory. Inserting 200,000 rows
  in one transaction with a prepared statement, then reading them all, on
  `darwin/arm64`: modernc 149-150 ms and 57-59 ms, ncruces 119-123 ms and
  39-41 ms, mattn 114-123 ms and 56-58 ms. First `sql.Open` plus first
  query on a file: modernc 3.4 ms, ncruces 4.9 ms, mattn 3.1 ms on
  `darwin/arm64`; 1.1 ms and 1.6 ms for the pure-Go two on `linux/arm64`.
  Resident memory added by 64 idle open connections, each in a fresh
  process: modernc +8.6 MiB (about 137 KiB each), ncruces +24.2 MiB (about
  387 KiB each), mattn +11.6 MiB (about 186 KiB each).
- <a id="e5"></a>[85] Compile cost. Fresh `GOCACHE`, standard library warmed
  by building the baseline program, then `go build -toolexec` with a
  wrapper around `/usr/bin/time -l`. The generated SQLite package compiled
  in about 2.0 s at 0.82-0.85 GB peak RSS for modernc (`modernc.org/sqlite/lib`)
  and 3.1-3.2 s at 1.33-1.43 GB for ncruces (`go-sqlite3-wasm/v6`), per
  target. A cold build of all three `CROSS_TARGETS` took 12.2 s (modernc)
  and 13.5 s (ncruces) of wall time.
- <a id="e6"></a>[86] Modules linked, from `go version -m`. modernc:
  `modernc.org/sqlite`, `modernc.org/libc`, `modernc.org/mathutil`,
  `modernc.org/memory`, `golang.org/x/sys`, `dustin/go-humanize`,
  `google/uuid`, `mattn/go-isatty`, `ncruces/go-strftime`,
  `remyoudompheng/bigfft` (10). ncruces: `ncruces/go-sqlite3`,
  `ncruces/go-sqlite3-wasm/v6`, `ncruces/julianday`, `golang.org/x/sys`
  (4). mattn: itself (1).

## What the choice costs

- One direct dependency, `modernc.org/sqlite`, and nine modules under it,
  one of them `golang.org/x/sys` ([[86]](#e6)). The standard library has
  no SQLite and `golang.org/x` has no driver, so the reason for the commit
  body is ADR 0001 itself: an embedded SQLite with `CGO_ENABLED=0`.
- koko's `go.mod` must pin exactly the `modernc.org/libc` that the chosen
  `modernc.org/sqlite` pins (v1.79.0 for v1.61.1), and the two move
  together ([[4]](#m4)). A dependency bot must update them as one group;
  bumping libc alone has broken builds before.
- Every default koko depends on goes in the DSN: busy timeout (0 by
  default), `foreign_keys` (off), `_txlock=immediate` (deferred by
  default). Times need `_time_format`, or explicit encoding in koko,
  because the default writes Go's `String()` form with the monotonic clock
  reading ([[5]](#m5)). A test should open a database through koko's own
  open function and assert these pragmas.
- About 4.4 MiB more binary ([[81]](#e1)), and about 2 s and 0.85 GB per
  target for the generated package in a cold build ([[85]](#e5)).
- It is the slowest of the three in its maintainer's own benchmark
  ([[71]](#b1)); an older independent run found no clear winner
  ([[72]](#b2)). CPU-bound queries run 1.3-2.0x slower than C ([[7]](#m7)),
  and contended `IMMEDIATE` writes were slow on Linux in my test
  ([[83]](#e3)). At koko's size none of this shows. If it ever does,
  sending all writes through one connection removes the contention inside
  the process; that is a design option, not something measured here.
- No encryption at rest in the driver ([[12]](#m12)); Secrets are encrypted
  by koko itself (see Open questions).
- What is given up: ncruces's safer defaults, smaller module graph and
  speed. Moving to it later is an import and a driver name (`sqlite3`),
  since the `_pragma` and `_txlock` keys are the same ([[5]](#m5),
  [[27]](#n7)), provided koko stores times in a format it chooses rather
  than the driver's default.

## Open questions

- [Secret encryption at rest and where its key lives](https://github.com/cieons/koko/issues/10)
  (#10): modernc offers no encrypted database file. Encrypting Secret
  values in koko with an AEAD works with any driver. Whole-file encryption
  exists only in ncruces (`adiantum`, `xts`), which is deterministic and
  does not detect tampering ([[31]](#n11)). If #10 wants the whole file
  encrypted, it reopens this choice.
- [Install and run the koko server](https://github.com/cieons/koko/issues/14)
  (#14): the data directory must be on a local filesystem, because WAL
  "does not work over a network filesystem" ([[64]](#s4)); the `-wal` and
  `-shm` files live beside the database. Whether to keep modernc's
  `synchronous=FULL` in WAL mode or choose `NORMAL` ([[82]](#e2),
  [[64]](#s4)) belongs there too.
- Schema migration across koko server upgrades (the map's "Not yet
  specified" list, and #14's "what an upgrade does to the schema"): neither
  driver ships migrations; `zombiezen.com/go/sqlite` has a migration
  package but no `database/sql` ([[51]](#o1)). koko writes its own.
- [Write the P0 specs](https://github.com/cieons/koko/issues/17) (#17): the
  storage conventions every capability shares belong in the overview spec:
  the DSN, `IMMEDIATE` writes and read-only reads, pool size, and how
  times are stored.
- Re-check ncruces when it reaches v1, or after wasm2go has a year in use;
  its unreleased change to an `immediate` default ([[27]](#n7)) shows the
  direction.

## Not verified

- mattn was built and run on `darwin/arm64` only (cgo); no Linux cgo
  build.
- The cause of modernc's slow contended writes on Linux is read from the
  two busy handlers ([[6]](#m6), [[28]](#n8)), not profiled. The
  `linux/amd64` runs used Docker's emulation on Apple Silicon; they show
  behaviour, not speed.
- The published benchmarks were run by Jan Mercl (modernc) and
  C. Vilsmeier (sqinn-go), not by me; their numbers are quoted from their
  READMEs. The 40 s / 4 GiB compile report for ncruces is one user's
  comment from March 2026; I measured 3.1 s and 1.4 GB.
- Memory per connection was measured on idle connections to a tiny
  database; page caches under load were not measured.
- Crash recovery, power loss and long-running use were not tested for
  any driver.
- Licences of transitive modules were not audited beyond each project's
  own statement ([[10]](#m10), [[32]](#n12)).
