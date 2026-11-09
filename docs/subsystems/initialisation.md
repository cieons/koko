# Initialisation

How a fresh koko server gets its Owner, its keys and its Server Address,
once, over its Unix socket or at its first start, and how the Owner shows and changes its
deployment afterwards. The order is P0 spec: install and run; the
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

In one transaction, so that a refusal leaves the koko server as it was, it
runs steps 1 to 5, initialisation's steps, which a first start shares
([`initialiseSteps`](../../internal/server/init.go)), then step 6:

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
5. starts listening on TCP at `--listen`: it binds the address now, so the
   first Join Code can name the port, and serves Device Connections once the
   transaction commits, closing the listener if it rolls back
   ([the Device Connection](device-connection.md#the-tcp-listener));
6. issues the Owner's first Join Code, for `device-1`, through the same path
   as `koko device add` over the socket
   ([Join Codes](join-codes.md#issuing)), naming the Server Address, with the
   port it listens on when the address names none, and the host key made in
   step 3; a Server Address too long for a Join Code is refused here with
   `invalid_address`.

No command of its own calls it: [`koko server install`](installer.md)
does, once `kokod install` has started the koko service and it has opened
its socket, so that `kokod` needs no API client; so does the S1 harness.

## At first start

A koko server can also initialise itself when it starts, given the Owner's
name and the Server Address: `kokod run --owner <user> --address
<host[:port]>`, or `KOKO_OWNER` and `KOKO_ADDRESS`
([ADR 0038](../adr/0038-a-container-initialises-at-its-first-start-and-gives-its-first-join-code-to-docker-exec.md)).
The container image's docs use them; the systemd unit sets neither, and
install still initialises over the socket. `kokod run`
([the CLI](cli.md#the-commands-so-far)) checks the two before the koko
server runs.

[`server.Start`](../../internal/server/server.go), in the order of
[the Seal's start-up](seal.md#at-start-up):

1. opens the store, then makes the Root Key when its file is missing and
   nothing is sealed yet ([`makeRootKey`](../../internal/server/firststart.go));
   a file that is there is reused when it loads and refused, left as it is,
   when it does not;
2. loads the Root Key, as every start does;
3. on a koko server not initialised yet, runs initialisation's steps 1 to 5
   in one write of the koko server's own, as `server.init` does
   ([`initialiseAtStart`](../../internal/server/firststart.go), through
   `op.API.Do`, which gives the steps the call's time and `op.AfterTx` as an
   operation has them), and issues no Join Code. `server.init` refuses a
   Server Address too long for a Join Code when it issues the first one;
   this start issues none, so it refuses that address as `server.set`
   does, with `invalid_address`;
4. only then listens on the Unix socket, so no caller is ever told
   `not_initialised` in between; the TCP listener, bound in the write,
   serves only once it commits.

A refused initialisation, such as an address too long for a Join Code, stops
the start with that refusal and leaves the koko server uninitialised. The
Root Key step 1 wrote stays, and the next start reuses it, since nothing is
sealed under it yet: a fix costs one more start.

The log of that start holds `koko server initialised` with the host key and
the listen address, as `server.init`'s does, and the Root Key's file and id
(`root_key_file=... root_key=...`), then `koko server started`, then a line
telling the Owner to keep a copy of the Root Key file off the machine,
naming it, and one saying that the Owner's first Join Code comes from `koko device add` on this
machine, or `docker exec <container> koko device add` in a container. When
start-up made the Root Key, it logs `wrote a new Root Key` with its file and
id first. No line holds the Root Key or a Join Code.

An initialised koko server does not use `--owner` and `--address`. When
either differs from the record, the Owner's name or the stored Server
Address, start-up logs one warning
([`warnUnused`](../../internal/server/firststart.go)), at `WARN`, saying
that only a fresh koko server reads them and naming `koko server set
address=<host[:port]>`, with `owner=`, `recorded_owner=`, `address=` and
`recorded_address=` for those that differ; equal values log nothing. It
never refuses over them, and never makes a Root Key over sealed data: a
missing or another Root Key is refused as at every start, naming the id the
sealed data needs ([the Seal](seal.md#at-start-up)).

Without `--owner`, a fresh koko server starts uninitialised and waits for
`server.init`, as before; when its Root Key file is missing, the refusal
adds that a fresh koko server makes its own when started with `KOKO_OWNER`
and `KOKO_ADDRESS`.

## Before initialisation

The operation framework asks
[`deployment.Initialised`](../../internal/server/deployment/deployment.go)
inside each call's transaction. Until the `deployment` row exists, every
operation but `server.init` is refused with `not_initialised` (409), and
records nothing: no Space or other record exists without an Owner, so the
Owner's name can never be taken before it is given. Nothing listens on TCP
for devices; the web listener, when it is on, answers every request with a
`503` page saying the koko server is not initialised
([the web listener](web-front.md#the-web-listener)).

## The record

| Table | What it holds |
| --- | --- |
| `deployment` | One row: the Server Address, and the Web URL (`web_url`), `NULL` until the Owner sets it. The koko server is initialised once it exists. |
| `own_keys` | One row per key the koko server makes for itself, sealed for the row's id: `koko` and `host`, each PKCS #8, made at initialisation; and `cookie`, the web front's 32-byte cookie key, made on first use ([`koko web` and the web front](web-front.md#runs-tokens-and-the-cookie)). |
| `users` | The Owner, with the role `owner`; a partial unique index allows one. Invited Users have the role `member` ([Users](users.md#the-record)). |
| `spaces.user_id` | The User a Personal Space belongs to, which is not a membership; empty for a shared Space. |
| `join_codes` | The Owner's first Join Code, kept as the hash of its secret ([Join Codes](join-codes.md#the-record)). |

## At every start

An initialised koko server opens both keys with the Root Key
([`loadKeys`](../../internal/server/rootkey.go)), after checking that the
Root Key is the one they were sealed under; its refusals are in
[the Seal](seal.md#at-start-up). It then listens on TCP at once, presenting
the sealed host key, and refuses to start when it cannot. Its `koko server
started` log line says `initialised=true host_key=SHA256:... listen=<addr>`
once initialised, and `initialised=false` before; initialisation itself logs
`koko server initialised` with the host key and the listen address. A
`web listener` line comes just before `koko server started`, with
`listen=off`, or the web listener's address and the Web URL
(`web_url=none: …` while it is not set).

## The deployment afterwards

Two operations of the server area read and change the deployment once the
koko server is initialised; like every operation but `server.init`, both
are refused with `not_initialised` before. The process registers them
([`settings.go`](../../internal/server/settings.go)), since they show what it
loaded and runs with. Each returns `{"server": {...}}`
([`api.Server`](../../internal/shared/api/server.go)):

| Field | What |
| --- | --- |
| `address` | the Server Address as stored: without a port, its port is `listen_port` |
| `listen_port` | the TCP port the koko server listens on |
| `host_key_fingerprint` | the host key devices pin, as OpenSSH prints its SHA-256 fingerprint |
| `host_key` | that host key itself, as in `authorized_keys` without a comment, which [`koko ssh setup`](device-connection.md#on-the-koko-servers-own-machine) writes into koko's known_hosts; `koko server show` prints only its fingerprint |
| `version` | the koko server's version, as `koko version` names its own |
| `root_key_file`, `root_key_id` | the Root Key's file as the Owner keeps it, and the id it names; never the key |
| `data_dir` | the data directory, as `kokod run` was given it |
| `web_url` | the Web URL, as stored: lowercased, without the default port 443; `null` while it is not set |
| `web_listen` | the web listener's bound address, such as `127.0.0.1:7823`; `null` while it is off |

- **`server.show`** ([`koko server show`](cli.md#the-commands-so-far), read
  tier) answers every caller, a Member's Device Session included: the
  overview spec marks it for no one, and nothing in it is secret. Its
  `root_key_file` is the file the Root Key was loaded from, except under
  systemd, where `kokod run` reads the copy `LoadCredential=` hands it
  in `$CREDENTIALS_DIRECTORY`: then it is `/etc/koko/root.key`, the file the
  koko unit loads that credential from, which install wrote and printed
  (`server.Config.RootKeySource`). A drop-in that loads the credential from
  another file leaves it naming `/etc/koko/root.key`, since the service is
  never told where its credential came from.
- **`server.set`** (`koko server set address=<host[:port]>`,
  `web_url=<https://host[:port]>` and `--unset web_url`, reversible tier)
  answers only the Local caller (`op.LocalOnly`): over a Device Connection,
  the Owner's own Device Session included, it is refused with `local_only`,
  so no device can move where devices connect. A browser tab of `koko web`
  on the koko server's machine, whose calls come over the Unix socket as the
  Local caller, is refused with `local_only` too (a `web@koko` channel,
  `transport.WebPortFrom`), so no browser tab can either. It changes only
  what the request names, `{"address": "<host[:port]>", "web_url": "<url>",
  "unset": ["web_url"]}`; naming nothing, it changes nothing. The address is checked as at initialisation, and refused
  with `invalid_address` when it is not a Server Address or, with the listen
  port written, is longer than a Join Code carries
  ([`joincode.CheckAddress`](../../internal/shared/joincode/joincode.go),
  255 bytes); a refusal changes nothing. The change takes effect
  for the Join Codes issued afterwards: a code issued before keeps the
  address it carries, and still joins there while the koko server answers
  at it. A joined device takes the new address with `koko connect
  <host:port>`; nothing moves a device by itself.
- **The Web URL** ([`weburl.Parse`](../../internal/shared/weburl/weburl.go),
  ADR 0042) is an origin, `https://<host>[:<port>]`, whose host is a DNS
  name or an IP address as a Server Address's is, with no path, query,
  fragment or user information; a lone trailing `/` is dropped. It is
  stored lowercased and without the port 443. Anything else, an empty value
  included, is refused with `invalid_web_url` (400). `"unset": ["web_url"]`
  clears it; the Server Address is never cleared, and an unset of anything
  else, or a Web URL both set and cleared, is `invalid_request`. A change
  takes effect on the web listener's next request
  ([the web listener](web-front.md#the-web-listener)).

The CLI refuses a `key=value` other than `address=` and `web_url=`, an
address that is not a Server Address, a Web URL that is not one, an
`--unset` of anything but `web_url`, and a `server set` naming nothing, with
exit 2, before any call. `koko server show` prints the web listener's
address, or `off`, and the Web URL, or `not set`; `koko server set
web_url=` says what changed, and how to turn the web listener on while it
is off.

## Verifying

- [`run_firststart_test.go`](../../internal/server/kokod/run_firststart_test.go),
  through `kokod run` serving in the background
  (`harness.KokodServing`): a first start writes the Root Key and comes
  up initialised with no Join Code, `koko server show` naming the address,
  the Root Key's file and id and the data directory; `koko device add` over
  the socket issues the first Join Code, and a device joins with it,
  pinning the host key the log named; the whole log holds the host key, the
  listen address, the Root Key's file and id and the `koko device add`
  line, never the Root Key or the Join Code; no caller dialling during a
  first start gets `not_initialised`; a Root Key file that is there is
  reused or refused byte for byte; none is made over sealed data; an
  address too long for a Join Code stops the start and the next start, with
  a good one, initialises on the Root Key it left; an initialised koko
  server given another Owner or address warns and keeps its record, and
  given the same logs nothing; a missing Root Key without `--owner` names
  `KOKO_OWNER` and `KOKO_ADDRESS`; and the command's tier.
- [`kokod_test.go`](../../internal/server/kokod/kokod_test.go): `--owner`
  and `--address` from flags and the environment, and the refusals of one
  without the other and of a bad name or address, before the koko server
  runs.
- [`server_settings_test.go`](../../internal/client/cli/server_settings_test.go),
  through the CLI: `server show` and `server set` with their `--json`
  golden files and never the Root Key, the Root Key's file named under
  systemd's credential on the installer's fake machine, Join Codes issued
  before and after a change and where each joins, the refusals before any
  call and of an address no Join Code carries, `server set` refused and
  `server show` answered from the Owner's and a Member's devices and from a
  browser tab of `koko web`, and the two commands' tiers; the Web URL set
  as stored and cleared, `server show` naming it and the web listener, and
  its refusals in the CLI (exit 2) and in `server.set` (`invalid_web_url`).
- [`weburl_test.go`](../../internal/shared/weburl/weburl_test.go): the Web
  URL's forms and refusals.
- [`listen_test.go`](../../internal/server/listen_test.go): nothing on TCP
  before initialisation, the sealed host key once initialised and after a
  restart, and nothing listening after a refused initialisation.
- [`init_test.go`](../../internal/server/init_test.go), over the socket: the
  Owner and their Personal Space, the refusals before initialisation and of a
  second one, the Owner's name and the Server Address checked, the first Join
  Code and what it names, and no private key or Root Key in the raw bytes of
  `koko.db` and its `-wal`, while running and once stopped.
- [`run_rootkey_test.go`](../../internal/server/kokod/run_rootkey_test.go),
  through `kokod run`: a restart with the same Root Key logs the host key
  initialisation made; another Root Key, a missing file and one that does not
  parse are refused, naming the id needed, and leave every file as it was.
- [`init_caller_test.go`](../../internal/server/init_caller_test.go):
  `server.init` refused to a caller who is not the Local caller.
- [`op_test.go`](../../internal/server/op/op_test.go): `BeforeInit` and
  `LocalOnly`, and `Do`, a write of the koko server's own, with the call's
  time and `AfterTx`.
- [`serveraddr_test.go`](../../internal/shared/serveraddr/serveraddr_test.go):
  the Server Address's forms.
