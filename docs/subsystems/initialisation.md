# Initialisation

How a fresh koko server gets its Owner, its keys and its Server Address,
once, over its Unix socket. The order is P0 spec: install and run; the
sealing is [the Seal](seal.md) and
[ADR 0018](../adr/0018-secrets-and-private-keys-are-sealed-with-a-root-key.md).

## The operation

`server.init` ([`api.ServerInit`](../../internal/shared/api/server.go)),
registered by the koko server process itself
([`init.go`](../../internal/server/init.go)), since it acts on the Root Key
the process loaded. It answers only the Local caller (`op.LocalOnly`; any
other caller gets `local_only`), and is the one operation that answers before
initialisation (`op.BeforeInit`).

- **In:** `{"owner": "<user name>", "address": "<host[:port]>"}`.
- **Out:** `{"address": "<as given>", "host_key_fingerprint": "SHA256:...",
  "join_code": {...}}`, the fingerprint as OpenSSH prints it, and the Owner's
  first Join Code as `device.add` returns one.

In one transaction, so that a refusal leaves the koko server as it was, it:

1. refuses an initialised koko server with `already_initialised` (409), so
   nothing replaces the Owner or the keys;
2. checks the Server Address
   ([`serveraddr.Check`](../../internal/shared/serveraddr/serveraddr.go):
   a DNS name, an IPv4 address or a bracketed IPv6 address, then an optional
   `:port` from 1 to 65535), refusing with `invalid_address` (400), and
   stores it as given: without a port, its port is the listen port;
3. makes the koko key and the host key (ed25519), and stores each as PKCS #8
   sealed under the Root Key for its own row's id
   ([`deployment.Create`](../../internal/server/deployment/deployment.go));
4. creates the Owner and the Owner's Personal Space, which takes the Owner's
   name ([`user.AddOwner`](../../internal/server/user/user.go)); the name is
   checked as a User name, with no default, and refused with `invalid_name`;
5. issues the Owner's first Join Code, for `device-1`, through the same path
   as `koko device add` over the socket
   ([Join Codes](join-codes.md#issuing)), naming the Server Address and the
   host key made in step 3; a Server Address too long for a Join Code is
   refused here with `invalid_address`.

No command calls it: `koko server install` (#38) does, and the S1 harness.

## Before initialisation

The operation framework asks
[`deployment.Initialised`](../../internal/server/deployment/deployment.go)
inside each call's transaction. Until the `deployment` row exists, every
operation but `server.init` is refused with `not_initialised` (409), and
records nothing: no Space or other record exists without an Owner, so the
Owner's name can never be taken before it is given.

## The record

| Table | What it holds |
| --- | --- |
| `deployment` | One row: the Server Address. The koko server is initialised once it exists. |
| `own_keys` | One row per key the koko server makes for itself, sealed for the row's id: `koko` and `host`, each PKCS #8, made at initialisation; and `cookie`, the web front's 32-byte cookie key, made on first use ([`koko web` and the web front](web-front.md#runs-tokens-and-the-cookie)). |
| `users` | The Owner, with the role `owner`; a partial unique index allows one. Invited Users have the role `member`. |
| `spaces.user_id` | The User a Personal Space belongs to, which is not a membership; empty for a shared Space. |
| `join_codes` | The Owner's first Join Code, kept as the hash of its secret ([Join Codes](join-codes.md#the-record)). |

## At every start

An initialised koko server opens both keys with the Root Key
([`loadKeys`](../../internal/server/rootkey.go)), after checking that the
Root Key is the one they were sealed under; its refusals are in
[the Seal](seal.md#at-start-up). Its `koko server started` log line says
`initialised=true host_key=SHA256:...` once initialised, and
`initialised=false` before.

## Verifying

- [`init_test.go`](../../internal/server/init_test.go), over the socket: the
  Owner and their Personal Space, the refusals before initialisation and of a
  second one, the Owner's name and the Server Address checked, the first Join
  Code and what it names, and no private key or Root Key in the raw bytes of
  `koko.db` and its `-wal`, while running and once stopped.
- [`server_rootkey_test.go`](../../internal/client/cli/server_rootkey_test.go),
  through `koko server run`: a restart with the same Root Key logs the host key
  initialisation made; another Root Key, a missing file and one that does not
  parse are refused, naming the id needed, and leave every file as it was.
- [`init_caller_test.go`](../../internal/server/init_caller_test.go):
  `server.init` refused to a caller who is not the Local caller.
- [`op_test.go`](../../internal/server/op/op_test.go): `BeforeInit` and
  `LocalOnly`.
- [`serveraddr_test.go`](../../internal/shared/serveraddr/serveraddr_test.go):
  the Server Address's forms.
