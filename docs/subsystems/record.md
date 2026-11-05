# The record: Hosts

The Hosts koko records, each in one Space, with their Attributes and Notes.
The rules are [P0 spec: the record](https://github.com/cieons/koko/issues/22)
and [ADR 0014](../adr/0014-host-names-are-unique-across-the-koko-server.md);
the operations are [`internal/server/host`](../../internal/server/host/host.go),
the value rules [`internal/shared/record`](../../internal/shared/record/record.go),
and the commands [`internal/client/cli/host.go`](../../internal/client/cli/host.go).

## A Host

Stored in `hosts`, with its Attributes in `host_attributes`
([`hosts.sql`](../../internal/server/store/migrations/hosts.sql)).

| Field | Rule |
| --- | --- |
| `id` | `host_` and 8 characters, set by koko. A Host name never holds `_`, so an id stands wherever a name does. |
| `name` | The SSH Name: a lowercase DNS label ([`name.Check`](../../internal/shared/name/name.go)), unique among the Hosts that are not Retired, whatever Space each is in. The partial unique index `hosts_live_name` backs the check, which runs inside the write's transaction. |
| `space` | The name of its one Space. `host add` puts it in the caller's Personal Space ([`space.Personal`](../../internal/server/space/space.go)) unless `space=` names another; only `host move` changes it. |
| `address` | An IP address without a zone, kept in its canonical form (`2001:db8::1`), or a DNS name, kept in lowercase: labels of letters, digits and `-`, 253 characters at most, the last not all digits. Not unique. |
| `port` | 1 to 65535; 22 when not given. Not unique. |
| `account` | The Host Account: 1 to 32 of `a-z`, `A-Z`, `0-9`, `.`, `_` and `-`, not starting with `-`. No default. |
| `state` | `registered`, `managed` or `retired`, set only by koko. Every Host is Registered until Enroll and retiring set the others. |
| `host_key` | The pinned host key's SHA-256 fingerprint, `SHA256:...`, or `null` for a Host with no pin. Stored as the key in SSH wire format, which Enroll writes. |
| `attributes` | Free text by key: 1 byte to 1 KiB of UTF-8 on one line, with no control character. A key is snake_case (`a-z`, `0-9`, `_`, starting with a letter, 1 to 63 characters) and not a field's name: `id`, `name`, `space`, `address`, `port`, `account`, `relay`, `state`, `host_key`, `host`, `ports`, `attributes`, `note`, `created_at` or `updated_at`. |
| `note` | 1 byte to 64 KiB of UTF-8 on any number of lines, with no control character but tab, line feed and carriage return; `null` for none. |
| `created_at`, `updated_at` | RFC 3339 in UTC to the second; every write moves `updated_at`. |

The CLI checks each value rule before any call and refuses a bad one with
exit 2 and `invalid_usage`; the koko server checks them again on every
write, whichever client sent it, and refuses with `invalid_value`, or
`invalid_name` for a name ([the codes](api.md#calls)). An empty value and an
empty Note are refused, so clearing is always `--unset`.

## Operations

| Operation | Does |
| --- | --- |
| `host.add` | Records the Hosts in `hosts`, all or none, each Registered. |
| `host.list` | Every Host the caller can see that is not Retired, by name and then id; the Notes only with `with_note`. |
| `host.show` | One Host. |
| `host.set` | Sets the address, port, Host Account, Attributes and Note it names, and clears the Attribute keys in `unset`, the Note for `note`, and the port back to 22 for `port`; nothing else changes. It refuses a request that changes nothing, a key both set and cleared, and clearing the address or the Host Account ([`record.Required`](../../internal/shared/record/record.go)). It takes no `name`, `space` or `state`. |
| `host.rename` | A new name, checked as on `host.add`; the id and everything else stay. |
| `host.move` | Another Space, with the Attributes and Note; the name stays. |
| `host.remove` | Deletes the Host with its Attributes and Note; answers it as it was. |

- **Naming a Host.** A request names a Host by its name, which names only a
  Host that is not Retired, or by its id (`host_...`), which names any.
- **Who.** Space membership decides every call ([the access check](access.md)):
  a Host in a Space the caller cannot act in answers exactly as one that
  does not exist, `unknown_host`, by name and by id; `host.move` needs the
  target Space too, looked up through `space.ByName`. The Owner and the
  Local caller act in every Space.
- **The one leak.** A name a Host holds is refused with `name_taken`, "a Host
  named "web1" already exists", the same whichever Space holds it, one the
  caller can see or not. It names nothing else.
- **The Note** is in every answer about one Host; `host.list` leaves it out
  unless `with_note` asks for it.
- **What a Space holds** is counted by [`host.Holding`](../../internal/server/host/host.go)
  ([what a Space holds](access.md#what-a-space-holds)).

## The CLI

`koko host add`, `ls`, `show`, `set`, `rename`, `move` and `rm` ([the commands
so far](cli.md#the-commands-so-far)). Fields and Attributes are both
`key=value`; `koko host set` refuses `name=`, `space=` and `state=`, naming
`koko host rename` and `koko host move`. `--note-file <file>` reads the Note from a file
and `--note-file -` from stdin, which it refuses when stdin is a terminal.
`--json` prints the CLI's own Host, `hostJSON`, with its Attributes in their
own object and `note` a string or `null`, which `ls` leaves out without
`--with-note`. For people, `ls` prints a table (`NAME SPACE STATE ADDRESS
PORT ACCOUNT ID`) followed, with `--with-note`, by each Host's Note.

## Verifying

- [`internal/client/cli/host_test.go`](../../internal/client/cli/host_test.go):
  every `host` command with its goldens, each value rule refused once before
  any call, the one leak answered alike whichever Space holds the name, a Host
  the caller cannot see answering as unknown, the Spaces a move needs,
  concurrent adds of one name, the Note from a file and from stdin, and
  `space show` and `space rm` counting Hosts.
- [`internal/server/host/host_test.go`](../../internal/server/host/host_test.go):
  the koko server's own checks on raw `host.add` and `host.set` calls, a batch
  added all or none, and which answers carry the Note.
- [`internal/server/access/matrix_host_test.go`](../../internal/server/access/matrix_host_test.go):
  every host operation as each caller.
