# Enroll

`koko host enroll` makes a Registered Host Managed: the koko server logs in
to it once with a password or private key the User supplies, pins its host
key and appends the koko key to its Host Account's `authorized_keys`
([ADR 0003](../adr/0003-enroll-only-adds-the-koko-key.md)). The operation is
`host.enroll`, in [`internal/server/enroll`](../../internal/server/enroll/enroll.go)
(server-only); the command is in
[`internal/client/cli/enroll.go`](../../internal/client/cli/enroll.go); the
rules are P0 spec: reach, "Enroll", and P0 spec: the record, "Host states".

## The command

`koko host enroll <host> [--password-stdin | --key <file> | --secret
<space>/<secret>] [--host-key SHA256:... | --replace-host-key]`,
irreversible, reads its secret input through the
[secret-input reader](cli.md#the-secret-input-reader), unless a Secret
holds it:

- `--password-stdin` reads the password from stdin. One line ending at its
  end is dropped, since `echo` adds it and no password typed at ssh's prompt
  can hold one.
- `--key <file>` reads a private key file. A key that is not encrypted goes
  to the koko server as the file holds it. An encrypted one is decrypted on
  the device, with its passphrase asked with echo off on a terminal, and
  goes decrypted, in OpenSSH's format: the passphrase never leaves the
  device. Without a terminal, or with a passphrase that does not open it,
  it is refused with exit 2.
- `--secret <space>/<secret>` names a Secret that holds the password or a
  private key, which the koko server reads on its own side
  ([reading a Secret](#reading-a-secret)): only the name crosses the API,
  so the value never reaches the device or the agent. The CLI reads
  neither stdin nor a terminal.
- With none of them, on a terminal, it asks for the password with echo off;
  without one it fails at once with exit 2, naming `--password-stdin`,
  `--key <file>` and `--secret <space>/<secret>`.

Every refusal of its command line (two or three ways to log in, both ways to
pin, a fingerprint that is not `SHA256:` and 43 characters of base64, a key
file it cannot read or that holds no private key, a Secret named without its
Space) comes before any call. It prints
the Host, with `--json` the Host's own shape ([the record](record.md#the-cli));
for people, the fingerprint it pinned.

## The operation

`host.enroll` takes the Host, exactly one of `password_base64`,
`private_key_base64` (each an `api.EnrollInput`, bytes carried as base64,
which prints and logs as its length) and `secret` (an `api.SecretRef`,
`{"space", "name"}`), and at most one of `host_key` and
`replace_host_key`. It is a [prepared write](api.md#the-operation-framework):

1. **Before the write's transaction.** In one short read-only transaction,
   it checks the request, reads the Host through
   [`host.ReachOf`](../../internal/server/host/reach.go) under the access
   check (`unknown_host` for a Host the caller cannot see), reads the Secret
   `secret` names ([reading a Secret](#reading-a-secret)), checks the Host's
   state, and opens the koko key's public key. Then, outside any
   transaction, it logs in through the [Host connector](host-connector.md)
   as the Host Account with the supplied password or key, never the koko
   key, and appends the koko key over one exec request, within
   `timing.EnrollAddKey` (30 s). The Host's Relay is not used: Hosts have
   none yet.
2. **In the write's transaction.** [`host.Enrolled`](../../internal/server/host/reach.go)
   finds the Host again, refuses with `host_changed` when its Host Account,
   state or pin changed meanwhile, and records the host key it offered as
   its pin, with the state Managed.

A dropped caller cancels neither part, and the caller's retry with the same
`Idempotency-Key` waits for the first attempt, then gets its stored
response, without logging in again.

## Reading a Secret

With `secret`, Enroll reads the value through
[Enroll's read](secrets.md#enrolls-read), for the same caller and with the
same refusals as `secret.show`, before koko connects to the Host: being able
to act on the Host grants no Secret.

The value is told apart by its header:

- **A private key** begins with a PEM header, `-----BEGIN `, past any blank
  space, as each form `golang.org/x/crypto/ssh` parses does: OpenSSH,
  PKCS #1, PKCS #8 and SEC 1. It is taken as `private_key_base64` is. One
  that is encrypted is refused with `invalid_value`, since koko keeps no
  passphrase: the way on is to store it decrypted, or to use `--key`, which
  asks for the passphrase on the device. One that does not parse, such as a
  certificate, is refused with `invalid_value` too, and never tried as a
  password.
- **A password** is any other value, less one line ending at its end, as
  `--password-stdin` drops the one `echo` adds
  ([`api.EnrollPassword`](../../internal/shared/api/enroll.go)):
  `echo pw | koko secret add` stores it with one. A value that is only a
  line ending is refused with `invalid_value`.

A refusal names the Secret, as `login_refused`'s "refused the password in
the Secret ops/root-password", and never its value.

## The host key

| Request | The Host must offer | Then pinned |
| --- | --- | --- |
| a Host with no pin | any key | the key it offered |
| a Host with a pin | the pinned key | the same key |
| `host_key` | the key with that fingerprint | that key, in place of any pin |
| `replace_host_key` | any key, the login its only proof | the key it offered |

Any other key ends the attempt before anything is sent to log in with, with
`unexpected_host_key`, whose message holds both fingerprints and the two
ways on, `--host-key` and `--replace-host-key`. A pin that changes is logged
with the old and the new fingerprint.

## Which Hosts Enroll

A Registered Host Enrolls, with a pin when a change of its Host Account
sent it back there ([the record](record.md#a-host)). A Managed one Enrolls
again only with `host_key` or `replace_host_key`, to pin a new host key;
without them it is refused with `already_managed`. A Retired Host is never
Enrolled: it is refused with `host_retired`, whatever the flags, before any
login ([retiring a Host](record.md#retiring-a-host)).

## What changes on the Host

Only the Host Account's `~/.ssh/authorized_keys`, by
[`addKeyScript`](../../internal/server/enroll/enroll.go), which the Host runs
as `sh -s <key type> <key>` with the script on stdin, so it works whatever
the Host Account's login shell, as long as it has one and a POSIX `sh`:

- it creates `~/.ssh` (0700) and `authorized_keys` (0600) when they are
  missing;
- it appends one line, `<key type> <key> koko`, after ending a last line
  that lacks its newline;
- it leaves the file as it is when a line other than a comment holds the
  koko key already, whatever that line's options and comment, so Enrolling
  again adds nothing;
- it runs `restorecon` on what it created where that exists, as
  `ssh-copy-id` does, and leaves the SELinux context of what was there.

A Host that refuses the exec, or where the script fails, is refused with
`key_not_added`, naming its stderr; koko records nothing. A Host refused
with `host_changed` may keep the koko key's line in the Host Account Enroll
logged in as, which the refusal says.

## Failures and the log

Enroll's refusals are `unexpected_host_key`, `login_refused` (never
retried), `host_unreachable`, `key_not_added`, `already_managed` and
`host_changed` ([the codes](api.md#calls)).

The koko server logs `host enrolled` (the Host, its id, the User, and
`enrolled_by`, the Device Session or `unix socket`, with the pinned
fingerprint), `host key re-pinned` (with `old` and `new`), `enroll failed`
with the code, and, at warn level, `enroll refused the host key`. No log
line, output or database file holds the password, the key, its passphrase
or the value of a Secret Enroll read, and no file on the device holds that
value: the request is kept only as the sealed digest of its
`Idempotency-Key` ([Idempotency-Key](api.md#idempotency-key)).

## Verifying

- [`internal/client/cli/enroll_test.go`](../../internal/client/cli/enroll_test.go):
  each input (`--password-stdin`, `--key` plain and encrypted, the prompt,
  the refusals with exit 2 before any call), the pin and printed
  fingerprint, `--host-key`, a changed host key and `--replace-host-key`
  with its log line, on a Registered Host and on a Managed one,
  `authorized_keys` changed only by the koko key's line, the koko key never
  used to log in, the failures leaving the Host Registered, `host_changed`
  when the Host Account changes during the login, the state table, `host
  rm` refused, a dropped caller not cancelling Enroll, and no input in any
  output, log line or database file.
- [`internal/client/cli/enroll_secret_test.go`](../../internal/client/cli/enroll_secret_test.go):
  `--secret` holding a password, with the line ending `echo` adds, and a
  private key in OpenSSH's form and in PKCS #8; no prompt and no stdin read;
  a Secret the caller cannot read refused as a Space that does not exist,
  byte for byte, and a Host the caller cannot see refused whatever Secret
  they read; a membership change holding from the next call; an encrypted
  key, a certificate and a line ending refused before any connection; and
  the value in no output, log line or file under any HOME, nor anywhere in
  the data directory but sealed.
- [`internal/server/access/matrix_enroll_test.go`](../../internal/server/access/matrix_enroll_test.go):
  `host.enroll` as each caller, with a password, with a Secret of the Host's
  Space, and with a Secret of a Space the caller cannot act in.
- [`internal/server/op/prepare_test.go`](../../internal/server/op/prepare_test.go):
  the prepared write.
