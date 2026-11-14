# Enroll

`koko host enroll`, or Enroll in [the web UI](#the-web-ui), makes a
Registered Host Managed: the koko server logs in
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

## The web UI

Add Host and a Registered Host's page Enroll through the same
`host.enroll` ([the record's pages](record-pages.md#enroll)), with a
password, a private key or a Secret, and a host key's fingerprint checked
as `--host-key` is. A credential typed into the browser opens no new path:
the web UI takes Secret values already, and the koko server does the login
whichever surface calls. The page keeps to these rules:

- **A secure context only.** The password, the key and the passphrase are
  offered only where `window.isSecureContext` holds: a page served over
  HTTPS, or `koko web`'s on loopback. Elsewhere only a Secret is offered,
  whose value never crosses the API. No page outside one reaches the API
  in practice: the web listener answers only the Web URL's host, takes a
  `POST` only from the Web URL's `https://` origin, and its sign-in cookie
  is `Secure` ([the web listener](web-front.md#the-web-listener)); the
  check holds anyway, should that change.
- **Sent once, kept no longer.** The form clears the credential as the
  call is sent, and keeps the request only as its SHA-256, salted with
  bytes of its own, so the same credential given again, as after a
  dropped answer, reuses its `Idempotency-Key` and the koko server
  answers it once. Closing the form drops what was typed, and a form
  closed while a call runs sends nothing more: Add Host closed before its
  `host.add` answers sends no `host.enroll`. Nothing goes to the
  browser's storage.
- **Asked not to be saved.** A password or passphrase field is marked
  `autocomplete="one-time-code"`, which browsers leave out of what they
  offer to save, where they disregard `autocomplete="off"` on a password
  field; the key's field `autocomplete="off"`; and each carries the
  attributes the common password managers read to leave a field alone
  (`data-1p-ignore`, `data-lpignore`, `data-bwignore`,
  `data-form-type="other"`). Whether each browser keeps to them is
  checked in a browser, by the Owner, not by a test.
- **An encrypted key** goes with its passphrase in `passphrase_base64`,
  which the page asks for once the key it reads is encrypted
  (`encryptedKey`), or once the koko server answers `passphrase_needed`
  for a key it did not read so; the koko server decrypts the key, as
  below.
- **Its own refusal.** `invalid_value` on Enroll says which keys and
  Secret values Enroll takes, not the Host form's message.

## The operation

`host.enroll` takes the Host, exactly one of `password_base64`,
`private_key_base64` (each an `api.EnrollInput`, bytes carried as base64,
which prints and logs as its length) and `secret` (an `api.SecretRef`,
`{"space", "name"}`), and at most one of `host_key` and
`replace_host_key`. It is a [prepared write](api.md#the-operation-framework).

An encrypted `private_key_base64` comes with `passphrase_base64`, an
`api.EnrollInput` too, from revision 2, as [the web UI](#the-web-ui)
sends it;
`koko host enroll --key` decrypts on the device and sends none. The koko
server decrypts the key with it once and keeps neither. An encrypted key
without it is refused with
`passphrase_needed`, one it does not open with `wrong_passphrase`, and a
passphrase with anything but an encrypted `private_key_base64` with
`invalid_request`, each before any login, and no refusal holds it.

Enroll runs in two parts:

1. **Before the write's transaction.** In one short read-only transaction,
   it checks the request, reads the Host through
   [`host.ReachOf`](../../internal/server/host/reach.go) under the access
   check (`unknown_host` for a Host the caller cannot see), reads the Secret
   `secret` names ([reading a Secret](#reading-a-secret)), checks the Host's
   state, and opens the koko key's public key. Then, outside any
   transaction, it logs in through the [Host connector](host-connector.md)
   as the Host Account with the supplied password or key, never the koko
   key, and appends the koko key over one exec request, within
   `timing.EnrollAddKey` (30 s). A Host with a Relay is reached through it
   ([below](#through-a-relay)).
2. **In the write's transaction.** [`host.Enrolled`](../../internal/server/host/reach.go)
   finds the Host again, refuses with `host_changed` when its Host Account,
   state, pin or Relay changed meanwhile, and records the host key it offered as
   its pin, with the state Managed.

A dropped caller cancels neither part, and the caller's retry with the same
`Idempotency-Key` waits for the first attempt, then gets its stored
response, without logging in again.

## Through a Relay

A Host with a [Relay](record.md#a-relay) is Enrolled through its whole
chain of Relays, as the Gateway reaches it, since a Host is given a Relay
because the koko server cannot reach it directly: the koko server logs in to
each Relay in turn as its Host Account with the koko key, checking each
Relay's pin, each through a `direct-tcpip` channel on the one before, then to
the Host through one on the last, with the supplied password or key. The
caller need not see any Relay of the chain. A message names a Relay only to
a caller who can follow the links to it, as the Gateway's do
([the Gateway](gateway.md#relay)).

- A chain that holds a Relay that is not Managed is refused with
  `invalid_relay` before any login, naming the one nearest the koko server,
  since koko logs in to a Relay only with the koko key.
- A chain the record holds broken is refused with `host_unreachable`,
  `koko could not connect to web1; the koko server's log says why`, before
  any login, and logged as `enroll failed` with `reason="relay chain
  broken"` and `read_relay_ids`, the ids read.
- A failure on a Relay's leg is refused with `host_unreachable`: `koko
  could not reach web1 through its Relay, so it did not log in to web1: `
  and why, as the Gateway's banner says it, for the Relay at the hop that
  failed: the Relay down, its host key not the pinned one, which
  `--host-key` and `--replace-host-key` on this Enroll do not change, or
  the koko key refused. It names the Relay, its fingerprints and its Host
  Account only to a caller who can follow the links to it, and never the
  connector's error, which the `enroll failed` line logs as `cause`, with
  `relay`, `relay_id` and `hop`, its place in the chain from 1.
- A chain changed while Enroll logs in through the old one, any Relay of it
  by ids in order, or broken in the record since, is refused with
  `host_changed`
  ([`host.Enrolled`](../../internal/server/host/reach.go)).

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
retried), `host_unreachable`, `key_not_added`, `already_managed`,
`host_changed`, `passphrase_needed` and `wrong_passphrase`
([the codes](api.md#calls)).

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
- [`internal/client/cli/enroll_passphrase_test.go`](../../internal/client/cli/enroll_passphrase_test.go):
  `host.enroll` with an encrypted key and its `passphrase_base64`; the key
  without it refused with `passphrase_needed`, with a wrong one with
  `wrong_passphrase`, and a passphrase with a key that is not encrypted or
  with a password with `invalid_request`, each before any login; and the
  passphrase in no refusal, log line or file of the data directory.
- [`internal/client/cli/enroll_secret_test.go`](../../internal/client/cli/enroll_secret_test.go):
  `--secret` holding a password, with the line ending `echo` adds, and a
  private key in OpenSSH's form and in PKCS #8; no prompt and no stdin read;
  a Secret the caller cannot read refused as a Space that does not exist,
  byte for byte, and a Host the caller cannot see refused whatever Secret
  they read; a membership change holding from the next call; an encrypted
  key, a certificate and a line ending refused before any connection; and
  the value in no output, log line or file under any HOME, nor anywhere in
  the data directory but sealed.
- [`internal/client/cli/relay_test.go`](../../internal/client/cli/relay_test.go):
  Enroll through a Relay, which carries its connection, and refused through
  a Relay whose host key changed, that is down, that refused the koko key,
  or that is Registered, each as the Owner and as a Member who cannot see
  the Relay, and with `host_changed` when the Relay changes meanwhile.
- [`internal/client/cli/chain_test.go`](../../internal/client/cli/chain_test.go):
  Enroll through a chain of two Relays, each carrying its connection to the
  next hop; refused before any login for a Relay of the chain that is not
  Managed, the one nearest the koko server named; each Relay leg's failure
  as `host_unreachable` naming the hop by the rule, with `hop` in the log;
  `host_changed` when a Relay in the middle changes meanwhile; and a chain
  broken in the store refused with `host_unreachable`.
- [`internal/server/access/matrix_enroll_test.go`](../../internal/server/access/matrix_enroll_test.go):
  `host.enroll` as each caller, with a password, with a Secret of the Host's
  Space, and with a Secret of a Space the caller cannot act in.
- [`internal/server/op/prepare_test.go`](../../internal/server/op/prepare_test.go):
  the prepared write.
