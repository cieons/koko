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

[`upgrade`](../../internal/server/store/migrations.go) runs at every `Open`,
so an upgrade needs no separate step. By `user_version` against the binary's
schema version:

- **Newer:** `Open` refuses, naming both schema versions, and changes nothing.
  There are no down-migrations; `koko server run` exits 1 with the message.
  When the data directory holds a snapshot this binary can open, the message
  names it as the one to put in place of `koko.db` to go back.
- **Equal:** nothing to do.
- **Older:** [`snapshot`](../../internal/server/store/snapshot.go) makes sure
  of `koko.db.before-<n>` in the data directory, `n` being the schema version
  the migration goes to, then the steps run. The snapshot holds the database
  as the older koko left it, at its own `user_version`, so that koko opens it
  unchanged.
- **0, a new database:** migrated with no snapshot, since it holds nothing to
  go back to.

To go back, the Owner stops the koko server, reinstalls the older koko, moves
`koko.db` aside with any `koko.db-wal` and `koko.db-shm` (only a crash leaves
them; SQLite would replay a leftover `-wal` onto the snapshot), and puts the
snapshot in its place.

Each step's transaction makes sure of the snapshot under its write lock
before it applies the step, so two processes starting together never
snapshot at once. A new snapshot is written with `VACUUM INTO` on another
connection (WAL lets it read beside the write lock) to `<name>.tmp`, synced,
renamed into place and its directory synced before the step commits, because
`VACUUM INTO` syncs nothing. Then every other `koko.db.before-<n>` and
`koko.db.before-<n>.tmp` is removed, so only the latest stays; other files are
left alone.

A failing step leaves the steps before it committed and the database between
two schemas, and systemd restarts the koko server. The next start, of the same
koko or a fixed one, finds a snapshot whose name is beyond the database's
schema version and whose own `user_version` is below it: a migration stopped
part way, and that snapshot still holds the record as the older koko left it.
It is kept, renamed `koko.db.before-<n>` for the migration it now stands
before, instead of being replaced by a copy of the half-migrated database. A
snapshot whose header cannot be read in that case is left alone, and `Open`
refuses, naming it.

## Reading it as it is

[`store.Peek`](../../internal/server/store/peek.go) runs a function in a
read-only transaction on `koko.db` as it is, for `koko server install`,
which reads the record before the koko server runs and beside one that runs
([the installer](installer.md#what-is-there-already)). It opens the file
`mode=ro` and `query_only(1)`, with the busy timeout and none of the settings
that would write, and `immutable=1` when there is no `koko.db-wal`: then
nothing has the file open and it holds every commit, and SQLite takes no lock
and makes no `-shm` or `-wal` file. It never creates the file or the data
directory, migrates or snapshots, and calls nothing when there is no
`koko.db` or one at `user_version` 0.
[`store.CheckVersion`](../../internal/server/store/migrations.go) gives
install, which reads `user_version` that way, the refusal `Open` gives a
database at a schema this koko predates, naming the snapshot to put back.

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
[`upgrade_test.go`](../../internal/server/store/upgrade_test.go) plays an
older and a newer koko with step lists of its own: the snapshot before a
migration and that it opens under the koko it came from, only the latest
kept and the Owner's own files left, two starts at once snapshotting once, a
newer schema refused and left as it was, a failed step keeping the steps
before it and the snapshot, upgrading again after going back, and an
unreadable snapshot left alone. It also migrates every older schema of
koko's own steps, which covers each step as it lands.
[`peek_test.go`](../../internal/server/store/peek_test.go) shows `Peek`
finding nothing without a database or in an empty one, reading an older
schema beside the koko that holds it without migrating or snapshotting it,
leaving no file in a stopped koko's data directory, and unable to write.
[`schema_test.go`](../../internal/server/schema_test.go) shows the koko server
refusing to start on a newer schema, with no socket opened.
