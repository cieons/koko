# The Seal and the Root Key

The Seal, in [`internal/server/seal`](../../internal/server/seal), owns the
Root Key and every value the koko server stores sealed: Secret values, the
koko key, the koko server's host key and the other keys it makes for itself,
and the responses it keeps for Idempotency-Keys ([the API](api.md#idempotency-key))
([ADR 0018](../adr/0018-secrets-and-private-keys-are-sealed-with-a-root-key.md)).
Every module that stores a secret value calls it, such as the secret area
for each Secret's value ([Secrets](secrets.md#sealing)), and none touches
AES-GCM itself. It is server-only.

## The interface

| Call | What it does |
| --- | --- |
| [`seal.Load(path)`](../../internal/server/seal/seal.go) | Reads the Root Key's file, giving a `*seal.RootKey`, or an error that says the path is not absolute, or the file is missing (it matches `fs.ErrNotExist`), cannot be read, or does not parse. |
| `seal.CheckPath(path)` | Refuses a path that is not absolute, without naming it. `Load` calls it first; so does start-up, before anything else names the path. |
| `seal.NewRootKey()` | Makes a new random Root Key and returns its file's content, for install to write. The koko server never calls it. |
| `RootKey.ID()` | The key's id, such as `rootkey_3f9k2m7q`. |
| `RootKey.Seal(recordID, value)` | Seals a value for the record whose immutable id is `recordID`. |
| `RootKey.Open(recordID, sealed)` | Opens it, or fails closed. |
| `seal.SealedBy(sealed)` | The id of the Root Key a sealed value needs, read without opening it, so that a refusal can name the key it expects. |

## The Root Key's file

One line, `<id>:<key>`, ending in a newline
([ADR 0025](../adr/0025-the-root-key-file-names-its-own-id.md)):

- the key is 32 random bytes, in 43 characters of base64url without padding;
- the id is `rootkey_` and the first 40 bits of SHA-256 over the label
  `koko Root Key id\0` and the key, in lowercase Crockford base32 (the
  alphabet of [record ids](store.md#record-ids)).

`Load` accepts the line with or without its newline, a CRLF and blank space
around it. Everything else does not parse: another line, a space inside, a key
that is not exactly 32 bytes of canonical base64url, an id that is not its
key's, or a file over 1 KiB. No error quotes the file, and a `RootKey` prints
as its id.

The path must be absolute, and a relative one is refused without being named.
Every Root Key file has an absolute path (systemd's credentials directory,
`/etc/koko`), and no Root Key line starts with `/`, so a key, or part of one,
pasted where its file's path belongs never reaches an error or the log.

## The sealed form

Version 1, in order:

| Bytes | What |
| --- | --- |
| 1 | the format version, `1` |
| 5 | the bytes behind the Root Key's id |
| 12 | the nonce, random for every seal |
| the value's length + 16 | the AES-256-GCM ciphertext and its tag |

The cipher is `crypto/cipher.NewGCMWithRandomNonce`. The additional data is
the first 6 bytes (version and key id) followed by the record's id, so a sealed
value opens only for its own record, and moving a value to another row fails.
Renaming or moving the record never reseals, because its id does not change.

`Open` fails closed, with no value, when the version is unknown, the value was
sealed under another Root Key (naming both ids), it was sealed for another
record, any byte has changed, or it is cut short. Its errors never quote a
value; the caller names the record. `Seal` panics on an empty record id, which
would let one sealed value open as another's.

## At start-up

[`server.Start`](../../internal/server/server.go) takes the Root Key's path
from `koko server run --root-key-file` ([the CLI](cli.md)), and:

1. refuses a path that is not absolute, without naming it;
2. refuses a path inside the data directory, or the directory itself,
   comparing both after resolving symlinks
   ([`checkRootKeyOutside`](../../internal/server/rootkey.go)), so a copy of
   the data directory never holds the key;
3. refuses a data directory on a network filesystem, as the installer does
   ([the installer](installer.md#the-steps)), where SQLite's WAL does not
   work: `unsupported_platform`
   ([`checkDataDir`](../../internal/server/startup.go)). It finds the
   filesystem through `Config.Filesystems`, which `cmd/koko` sets to
   `/proc/self/mountinfo` (`filesystem.System`); inside a container, that
   shows a bind mount's own filesystem;
4. refuses a data directory it cannot write, naming it and the uid and gid
   it runs as, which it finds by making a file there and removing it at
   once; before the data directory exists, steps 3 and 4 check the nearest
   directory above it that does, where it would be made
   (`filesystem.Nearest`);
5. given `--owner`, a missing Root Key file and a data directory with no
   `koko.db`, so nothing sealed, refuses a directory it cannot write where
   it would make that file, or the nearest one above it while that is
   missing, in the same words
   ([`checkRootKeyDir`](../../internal/server/firststart.go));
6. claims the socket's path ([`claimSocket`](../../internal/server/startup.go)):
   a socket file there is dialled, for at most a second; one that refuses
   the connection, as a koko server killed without warning leaves behind,
   is removed and logged (`removed a stale socket nobody answered on`); one
   that answers makes start-up refuse, naming the path, with `port_taken`,
   and whatever answers, such as a running koko server, keeps serving. A
   path that holds anything but a socket, or a dial that neither connects
   nor is refused, is refused and left as it is, never taken for stale;
7. opens the store, and reads which Root Key the sealed data needs, if the
   koko server is initialised ([`loadKeys`](../../internal/server/rootkey.go):
   `seal.SealedBy` on the host key);
8. given `--owner`, with nothing sealed yet and no file at the Root Key's
   path, checks its directory as step 5 does, then writes a new Root Key
   there (`seal.NewRootKey`), 0600, as the process's user, through a synced
   temporary file renamed into place
   ([`makeRootKey`](../../internal/server/firststart.go),
   [initialisation at first start](initialisation.md#at-first-start)), and
   logs `wrote a new Root Key` with its path and id once it loads. A file
   that is there is never replaced: step 9 loads or refuses it as it is;
9. loads the Root Key, refusing a missing file, one it cannot read and one
   that does not parse; a missing file on a fresh koko server is refused
   with a hint that one started with `KOKO_OWNER` and `KOKO_ADDRESS` makes
   its own;
10. over sealed data, refuses a Root Key whose id is not the one the data
   needs, then opens the koko server's own keys with it
   ([initialisation](initialisation.md#at-every-start));
11. logs `root_key=<id>` in its `koko server started` line, and serves.

Each Root Key refusal (steps 2, 9 and 10) names the path, and over sealed data the
Root Key id the data needs, so the Owner knows which copy to put back. Steps
1 to 5 write nothing, and step 6 removes only a stale socket file, so a
refusal at steps 1 to 6 leaves the data directory as it was, or missing. Every refusal
stops the start-up before the socket opens. Under systemd, `RuntimeDirectory=`
gives every start a fresh `/run/koko`, so step 6 matters where the socket's
directory outlives the process, as in a container. No refusal writes a key: step 8 alone writes one, never over sealed
data nor over a file that is there, and a file that does not parse is never
taken for a missing one. The store opens
before the key loads, in the order P0 spec: install and run gives, so a
refusal at step 9 may leave a new, empty `koko.db` in a fresh data directory,
and on an upgrade the migration and its snapshot run before a refusal. With
no migration due, a refusal leaves the data directory as it was.

## Verifying

[`seal_test.go`](../../internal/server/seal/seal_test.go) tests the interface:
a new key loads with its id; every way a file fails to load, none quoting it;
values of 0 bytes to 64 KiB round-trip; each kind of failed open; `SealedBy`;
and that printing or logging a key shows only its id.
[`server_startup_test.go`](../../internal/client/cli/server_startup_test.go)
runs `koko server run` against the real start-up (`harness.ServerStart`): it
starts and logs the id its file names, never the key; and it refuses a
relative path without printing it, a file inside the data directory (through
`..` and symlinks too) leaving every file as it was, a missing file writing
nothing in its place, and a file that does not parse leaving it unchanged.
[`server_checks_test.go`](../../internal/client/cli/server_checks_test.go)
covers steps 3 to 6 through the same start-up (`harness.ServerStartWith`): a
data directory on NFS in a faked mount table (`harness.Mountinfo`), existing
or not, and a read-only data directory or parent of a missing one, are
refused writing nothing; a stale socket file is taken over and served on; a
second start beside a running koko server refuses naming the socket while
the first keeps serving; and a file that is not a socket is left as it is.
[`server_firststart_test.go`](../../internal/client/cli/server_firststart_test.go)
covers steps 5 and 8 with `--owner`: a read-only directory where the Root
Key must be made is refused writing nothing; a missing Root Key is written
as one line, 0600, as the process's user, alone in its directory; one that
is there is reused byte for byte, or refused byte for byte when it does not
parse; and none is written over sealed data, whose refusal names the id
it needs even when the key's directory is read-only.
The S1 harness gives every koko server it starts a Root Key file made for
the test (`harness.NewRootKeyFile`). The refusals over sealed data are
verified with [initialisation](initialisation.md#verifying).
