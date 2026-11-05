# The store

The koko server's SQLite database, `koko.db` in the data directory, in
[`internal/server/store`](../../internal/server/store).

## Opening it

[`store.Open`](../../internal/server/store/store.go) creates the data directory
(0700) when it is missing, opens `koko.db` with `modernc.org/sqlite` through
`database/sql`, and migrates it. The DSN sets every default koko relies on, on
every connection:

```
file:<escaped path>?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(FULL)&_txlock=immediate
```

The path is URL-escaped, so a data directory may hold any character.
`go.mod` selects exactly the `modernc.org/libc` that `modernc.org/sqlite`'s own
`go.mod` requires; `TestLibcIsTheVersionSQLitePins` fails otherwise, so the two
are upgraded together.

## Transactions

`Store.Read` runs a function in a read-only transaction on a pool of
connections opened with `query_only(1)`, so a statement that would write fails
(modernc's read-only transaction alone only begins deferred). `Store.Write`
runs it in one `IMMEDIATE` transaction on the write pool, which commits only
when the function returns no error. The function gets a `store.Tx`, which can
query and execute but not commit. Writes begin `IMMEDIATE` because a deferred transaction that reads and
then writes fails at once with `SQLITE_BUSY`, whatever the busy timeout.

## Migrations

[`steps`](../../internal/server/store/migrations.go) lists the forward
migration steps, files in `migrations/`, in landing order; a step's position is
its schema version. `Open` applies each step past `PRAGMA user_version` in its
own `IMMEDIATE` transaction, which reads `user_version` and records the new
one, so two processes never apply a step twice. The rules for adding a step
are rule 2 in [code layout](code-layout.md#rules-for-working-in-parallel).
The snapshot before migrating, and refusing a schema newer than the binary,
come with #30.

## Record ids

[`store.NewID`](../../internal/server/store/id.go) makes a record's id: its
prefix, `_` and 8 random characters of lowercase Crockford base32 (40 bits),
as `space_3f9k2m7q`. A table's primary key refuses the rare repeat.

## Verifying

[`store_test.go`](../../internal/server/store/store_test.go) asserts each
pragma, that 160 concurrent read-then-write transactions all commit, that a
failed write leaves nothing, that a read cannot write, a data directory with
URI characters, and that a second open migrates nothing.
[`migrations_test.go`](../../internal/server/store/migrations_test.go) fails
when a step file and the list disagree.
