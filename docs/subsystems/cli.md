# The CLI

The command tree, the parser, the runner and the output layer, in
[`internal/client/cli`](../../internal/client/cli). The grammar is
[ADR 0019](../adr/0019-the-command-path-carries-the-risk.md); the `--json`
contract is [ADR 0016](../adr/0016-the-cli-json-is-its-own-contract.md).

## The command tree

One list of every command, [`cli.Tree`](../../internal/client/cli/tree.go),
joined from each area's `<area>Commands`. An entry
([`cli.Command`](../../internal/client/cli/tree.go)) holds the noun and verb,
the positional arguments (each plain, qualified, or qualified and allowed
bare; each required, or optional after every required one, shown as
`[<device>]`; the last, if plain, may take many words, one or more, shown as
`<user>...`; each may list the only values it takes, its choices), the flags
(each with its `KOKO_*` variable, default and choices), whether it takes
`key=value` pairs and `--unset`, the fields a list filters and sorts on
(`Filters`, with their types), whether it reads a secret input
(`ReadsSecret`), the risk tier (required),
Owner-only, the API operation it calls and its `Run`, made with `action(f)`
from a function returning the command's `--json` type, which is also its
shape. Every command takes `--json`, which is not listed, except one with
`RemoteCommand`, whose last argument is a command it runs on a Host as ssh
does, `koko exec`; its `Run` is a `remoteAction`, returning the remote
command's exit status; and one with `Stdio`, which carries a byte stream on
its stdin and stdout for the program that runs it, `koko tunnel`, ssh's
ProxyCommand; its `Run` is a `quietAction`. The parser, `koko help`, `koko completion` and `koko
skill` are all generated from the tree
([below](#koko-skill-koko-help-and-koko-completion)), so a command added to
it reaches all four; a command's `Run` gets the tree through its `Input`.
[`tree_test.go`](../../internal/client/cli/tree_test.go) checks every entry: a
tier, a summary, a unique path, sound arguments and flags (neither `--json`
nor `--unset` listed, `--unset` only beside pairs, and many words only for
the last argument, unqualified), an operation that `api.Operations` lists,
and for `RemoteCommand` many words last, no flags, pairs, filters, secret
input or shape, and a `remoteAction`; for `Stdio`, no many words, flags,
pairs, filters, secret input, remote command or shape.

## The parser

[`parse`](../../internal/client/cli/parse.go) turns argv into an entry with its
arguments and flags, or refuses the command line with exit 2 and the code
`invalid_usage`, before any call:

- a flag before or inside the command path, naming the command as it should be
  written (`koko --json space ls` → `koko space ls --json`), including a flag
  after a shorter command that a later word extends (`koko skill --json
  install`);
- an unknown command, naming what the typed words could continue with;
- an unknown flag, a flag given twice, a value flag without a value or with an
  empty one, a value outside the flag's choices (from the flag or its
  variable), `--json` or a boolean flag given a value, and `--json` to a
  command with `Stdio` (`koko tunnel takes no flags, not even --json`),
  which `koko help` and `koko skill` leave out of the commands that take it;
- `--help`, before the command path or as an unknown flag after it,
  pointing to `koko help` and the command path typed;
- a missing, an empty or an extra argument, so `koko space add "$NAME"` with
  `NAME` unset never reaches the koko server; an optional argument may be
  left out, but given empty it is refused too; an argument that takes many
  words needs one, refuses an empty one, and leaves no word extra; and an
  argument outside its choices;
- a qualified name (`<space>/<secret>`, `<host>/<service>`, `<user>/<device>`)
  with a part missing or empty, or a part that holds `/`; one that may be bare
  (`[<user>/]<device>`) is also taken without its `/`;
- for a command that takes `key=value` pairs: a pair with an empty key or an
  empty value (naming the key, so `region=$REGION` with `REGION` unset never
  clears it), a key given twice, `--unset` of one key twice, and a key both set
  and unset.

A list with `Filters`, such as `host ls` with `api.HostListFields`, reads
every word holding `=` or `.` as a filter term (`price.gt=10`, `region!=eu`,
`price.has`) and takes `--sort <key>[.desc]`, repeatable; `Input.Terms` and
`Input.Sorts` return them in order. Reading a term (its operator, key and
value, by the key's type) is the command's, through `readFilters` and
`readSort`, which check it against the command's `Filters`
([the record](record.md#filtering-and-sorting)) and refuse a bad one with
exit 2 before any call. `filterTerms` lists the forms of a term, for `koko
skill` and the refusals.

A command with `RemoteCommand` ([`parseRemote`](../../internal/client/cli/parse.go))
takes its arguments before the last as plain words, then every word left, as
typed, as its last, flags, `--json` and `--help` included: `koko exec web1
ls -l --json` runs `ls -l --json`. It refuses a flag anywhere before its
last argument (`--help` pointing to `koko help exec`), and a missing or
empty argument, with exit 255 rather than 2, so that a refusal never passes
for the remote command's own status. `koko help` and `koko skill` say so in
place of its flags, and leave it out of the commands that take `--json`.

A command that reads a secret input (`ReadsSecret`, such as `secret add`)
refuses an extra argument, and an unknown flag that does not start with
`--`, without repeating the word: it may be the secret written where an
argument goes, which koko never takes, and a refusal that repeated it would
put it in an agent's transcript once more.

Flags come after the path, mixed with arguments in any order, as `--name value`
or `--name=value`; a value that starts with `--` needs the `=` form. A flag not
given takes its `KOKO_*` variable when that is set and not empty, else its
default.

A command with `Pairs` reads every word holding `=` as a pair, cut at its
first `=` (`note=a=b` sets `note` to `a=b`); no name holds `=`, so no argument
is mistaken for one. One with `Unset` also takes `--unset <key>`, repeatable.
`Input.Pairs` and `Input.Unset` return them in the order given; checking a key
against the field names and Attribute key rules is the command's.
`Input.Args` returns the words of an argument that takes many, in order. A qualified
argument keeps its text in `Input.Arg` and its parts in `Input.Qualified`.

## The secret-input reader

[`Input.ReadSecret`](../../internal/client/cli/secretinput.go) is the one way
a command reads a secret input: a Secret value, an Enroll password or a key's
passphrase, never an argument. The command declares its forms in a
`SecretInput` (a file flag, a stdin flag, stdin itself, and the forms it reads
itself, such as `--secret`, for the refusal to name), and the reader reads the
first one given, byte for byte. With none given it prompts on stderr
(`Enter the value of ops/db-password: `) and reads one line with echo off when
stdin is a terminal; without one it fails at once with exit 2, naming the
forms. It refuses:

- a stdin flag when stdin is a terminal, which would echo what is typed;
- an empty value from any form, so a pipe from a command that failed sends
  nothing; empty stdin, such as an agent's `/dev/null`, gives nothing;
- a value longer than the input's `Max`, when it has one (a Secret value's
  is 64 KiB), never cut short; it reads no file or stdin past `Max` and one
  byte, so an endless one is never read to its end, and checks a typed line
  once it is read;
- a file it cannot read, with exit 2.

It reads stdin only for a stdin form. Stdin itself as the form (`secret add`)
is read to its end, or to `Max` and one byte, whenever stdin is not a
terminal, so a pipe that never closes and stays under `Max` waits; every
other secret input fails at once without a terminal.

**Stdin decides "on a terminal".** The prompt reads its answer there, and a
terminal anywhere else (stderr, or the controlling terminal of an agent's
harness) has nobody typing into it. Nothing else in the CLI prompts, and
nothing asks "are you sure?". The terminal sits behind
[`cli.Terminal`](../../internal/client/cli/terminal.go), whose per-OS code is
under [rule 13](code-layout.md#rules-for-working-in-parallel). Ctrl-C at the
prompt turns echo back on and ends the command.

## The runner and the output layer

[`cli.Runner.Run`](../../internal/client/cli/cli.go) takes argv, stdin,
stdout, stderr and the environment (HOME included) and returns the exit
status. A command reaches the koko server only on its first `in.Call`, so
`koko version` needs none.

- **Exit statuses:** 0 success; 1 the operation failed; 2 the command line was
  refused before any call. They never change meaning. A command with
  `RemoteCommand`, `koko exec`, exits instead with its remote command's
  status, and 255 when there is none or for every failure of its own, a
  refused command line included, as ssh does ([`koko exec`](gateway.md#koko-exec)); its failures are always
  `koko: <message>` lines, never the error object.
- **stdout** gets the result only on success, rendered in full first, so it
  stays empty on failure. A command that runs until it is stopped, such as
  `koko web`, prints its result once, as soon as it is ready, and nothing
  when it ends; a failure after that still goes to stderr with its status.
  **stderr** gets every error: `koko: <message>`, or with `--json` (anywhere
  on the line, even where refused) the error object
  `{"error": {"code": ..., "message": ...}}`. A refused batch, so far `koko
  host add` of several Hosts on stdin, adds `items`, each Host at fault, by
  the koko server or before any call alike
  ([a refused batch](record.md#a-refused-batch)).
- **Codes:** the API's pass through; the CLI adds `invalid_usage` (exit 2),
  `not_joined` (no koko server joined and no socket on this machine, or the
  joined koko server holds no Device Session with this device's key),
  `server_unreachable` (the socket or the joined koko server does not answer,
  or the connection could not carry the call, or ended under `koko web`; for
  the joined koko server the hint names `koko connect <host:port>`),
  `host_key_mismatch` (the koko server presented a host key other than the
  one pinned), the API's `revoked` for a device whose Device Session the
  koko server refused as Revoked or that keeps the record of it
  ([the device](device-connection.md#on-the-device)), the API's
  `cli_too_old` for a CLI below the oldest koko the koko server's report
  names, before it sends anything, naming both versions and `koko update`,
  which `koko tunnel` prints as its one line, exit 1, before ssh exits 255
  ([the window](api.md#the-way-in)), `already_joined`
  (`koko join` on a joined device, naming `koko device revoke` and `koko
  leave`; `koko ssh setup` on one, naming `koko join`),
  `invalid_join_code` (the koko server refused the Join Code, used, expired
  or never issued alike), `unsupported_platform` (`koko server install` or
  `uninstall` outside Linux, on an architecture with no `kokod`, not as
  root, or where systemd does not run the machine, before any `kokod` is
  read; or a machine `kokod install` cannot use, such as a data directory
  on a network filesystem), `port_taken` (another process holds the port
  `koko web` or `koko server install` listens on), `server_too_old` (the koko server's report does not
  list the command's operation; the message names the koko server's version
  and the command, and nothing was sent; or, so far for `koko ssh setup`
  only, the koko server answered without a field the command needs, which
  names it; or the koko server is below `compat.Oldest`, and every command
  fails with it, naming both versions, before sending anything), `download_failed` (`koko server install` or `uninstall` could
  not download the `kokod` of its release, or got a file it does not run;
  the message names the URL and `--kokod`, [the download](installer.md#the-download);
  or `koko update` could not find or download the release it installs, or
  got a file it does not install, [`koko update`](#koko-update)),
  `update_unsupported` (`koko update` of a koko it did not install, naming
  how to upgrade that one), `upgrade_failed` (`koko server upgrade` whose
  upgrade failed, with the outcome's message, or did not end within 15
  minutes; this koko left as it was, [`koko server
  upgrade`](#koko-server-upgrade)), `kokod_newer` (`koko server install`
  where the `kokod` installed is a newer release than this koko, before any
  download, naming both, `sudo koko update` and `KOKO_VERSION`,
  [the installer](installer.md#a-newer-kokod)),
  and `internal_error` for a failure it did not expect. `kokod` reports `invalid_usage`, `unsupported_platform`
  and `port_taken` too, so those three live in the shared
  [`install`](../../internal/shared/install/install.go) package, beside
  `install.Refusal`, and the CLI reads them from there. A refusal `kokod
  install` or `uninstall` prints becomes `koko server install`'s or
  `uninstall`'s, code, message and exit status unchanged.
- **A drop:** a call the connection drops is sent once more on a new
  connection, a write with the same `Idempotency-Key`, so it applies once
  ([the API client](api.md#the-api-client)); only when that one drops too
  does the command fail, with `server_unreachable`.
- **Which koko server:** the Unix socket when it is at its path, and
  otherwise the koko server this device joined, through this build's Device
  Mux, which `cli.Runner.StartDeviceMux` starts when none answers, or over
  a direct Device Connection of the command's own, which `KOKO_DIRECT=1`
  forces ([the Device Connection](device-connection.md#which-koko-server-the-cli-reaches),
  [the Device Mux](device-mux.md#which-way-a-command-goes)).
- **The command's own log** goes to stderr in slog's text format at the
  level `KOKO_LOG_LEVEL` names (`debug`, `info`, `warn` or `error`, as
  `kokod run --log-level` takes; info when unset, and any other value
  refuses the command line with `invalid_usage`); only debug lines exist so
  far, such as the direct path's one. `koko device-mux`, the
  Device Mux's entry point, is no command of the tree
  ([the Device Mux](device-mux.md#starting-the-lock-and-the-socket)).
- **Signals:** SIGINT and SIGTERM end a command's context (`cmd/koko`'s
  `signal.NotifyContext`), as Ctrl-C ends `koko web`. A command that
  handles a signal itself asks `cli.Runner.Signals` for it, which
  `cmd/koko` sets to `signal.Notify` and the S1 harness to the signals a
  test sends (`p.Signal`): so far `koko tunnel`, which ends on SIGHUP and
  SIGTERM and ignores its context ([`koko tunnel`](gateway.md#koko-tunnel)).

## `--json`

- The shapes are the CLI's own Go types (`spaceJSON`), mapped by hand from the
  API's. A list prints a bare array of every item (`[]` when empty), and a
  write prints the record it made or changed. An `rm` prints the record it
  removed, as it was, in the shape its `ls` prints, so a script keeps the
  id of what is gone. `user remove`, which deletes a User or Removes them,
  prints `{"outcome", "user"}`: which it did, and the User in `user ls`'s
  shape as it left them.
- Indented by two spaces, one value, ending in a newline. The members of a
  map, such as a Host's `attributes`, are sorted by key, so one record always
  prints the same.
- Every record carries an `id`: its kind, `_` and 8 lowercase Crockford base32
  characters, such as `space_3f9k2m7q`. No name holds `_` but a Secret's,
  which a command always takes with its Space, so an id never passes for a
  name.
- Timestamps, when a record has them, are RFC 3339 in UTC to the second, as
  `2026-11-05T14:03:07Z`.
- Every field of a record is always there: one with no value is `null`, as
  a Device Session's `revoked_at` while it is live. The one exception is the
  `note` of a Host in `host ls` and of a Service in `service ls`, left out
  unless `--with-note` asks for it, so that lists stay small.
- **The mapping test**
  ([`mapping_test.go`](../../internal/client/cli/mapping_test.go)) fails when a
  field of an API response a command reads is neither in the CLI's type under
  the same JSON name nor listed in the area's `mapping.omitted` with a reason.
- Golden files in [`testdata`](../../internal/client/cli/testdata) pin each
  command's output, one file per command and case.

## Output for people

Not a contract: a write says what it did (`Added Space ops (space_3f9k2m7q).`,
`Removed Space ops (space_3f9k2m7q).`), and a list is a table with a header
(`NAME  ID`), or a sentence when empty. A
command that issues a Join Code says for whom and until when, then prints the
one `koko join koko_...` line to paste on the new device. `koko join` says
which koko server it joined, as whose device, then any Include warning.
`koko status` lists its
fields one per line (`Server Address:`, `User:`, `Device:`, `Unix socket:`,
`koko server:`, `Device Mux:`, `This CLI:`, `Revoked:`), then any Include warning, then any warning of bare names ssh reads as IP addresses, or that koko cannot tell. `koko device ls` names who
added each device as `<user>/<device>` or `unix socket`, and adds a REVOKED
column when it lists a Revoked one; `koko device revoke` says which device it
revoked, and when it was this device, that koko deleted its files. `koko
user ls` adds a REMOVED column when it lists a Removed User, and `koko user
remove` says whether it deleted the User, freeing the name, or Removed them.

## `koko skill`, `koko help` and `koko completion`

Three commands describe the command tree, from the binary alone: they call
no koko server ([`skill.go`](../../internal/client/cli/skill.go)).

- **`koko skill`** prints the reference for agents of this build, in
  Markdown: the rules of the command line, every command under its risk tier
  (by tier, then path) with its usage, its flags, its arguments' choices,
  the fields a list filters on and the shape of its `--json`, then the
  filter syntax (`filterTerms` and the relative dates), the value types, the
  Preset Attributes (`record.Presets`), every error code (`api.Codes`, then
  the CLI's `cliCodes`) and the exit statuses (`exitStatuses`). A read
  command whose path is a prefix of a riskier one is named, as `koko skill`
  of `koko skill install`. A shape is written from the command's `--json`
  type by [`shape`](../../internal/client/cli/shape.go) as encoding/json/v2
  marshals it (`string`, `number`, `boolean`, `T | null`, `[T]`,
  `{"key": T}`, `"key"?: T` for a member that may be left out, `{<key>: T}`
  for a map, and `string | number` for an Attribute value); a type that
  marshals itself, such as `host add`'s one Host or array of Hosts, says its
  shape through a `jsonShape` method (`shaped`), and a test refuses one that
  does not; the text spells
  each shape out once and names the first command that printed it after
  that. With `--json` it prints the same data:
  `{"version", "commands": [...], "filters": {"terms": [{"term",
  "matches"}], "relative_dates"}, "presets": [{"key", "type", "values",
  "min"}], "error_codes": [{"code", "from"}], "exit_statuses": [{"status",
  "means"}]}`, `from` `"server"` or `"cli"`. A command is `{"command",
  "usage", "summary", "tier", "owner_only", "args": [{"arg", "choices"}],
  "flags": [{"flag", "summary", "env", "default", "choices",
  "repeatable"}], "filters", "json"}`, `filters` the list's fields as
  `presets` writes a key, or `null`, and `json` the shape, or `null` for a
  command that prints nothing. Its tests check properties (every command of
  the tree under its tier with its flags and shape, every error code, every
  Preset, the exit statuses), never the whole output (rule 11 in [code
  layout](code-layout.md#rules-for-working-in-parallel)).
- **`koko skill install [--dir <dir>]`** writes the stub `SKILL.md`, whose
  bytes are fixed, into `<dir>/koko/`, a directory koko owns, so it never
  overwrites another skill's `SKILL.md`; `<dir>` is the agent's skills
  directory, `~/.claude/skills` when `--dir` is not given (refused with exit
  2 when `HOME` is unset) ([ADR
  0032](../adr/0032-skill-install-writes-a-koko-directory-in-the-skills-directory.md)).
  It writes through a temporary file, synced and renamed over the stub, mode
  `0644`, and leaves every other file in the directory as it was. The stub holds the name, a description and "run `koko skill`", so it
  never goes stale across upgrades.
- **`koko help`** lists every command with its arguments and summary. `koko
  help <words>` shows the command the words name in full (usage, summary,
  tier, flags with their variables, defaults and choices, the fields a list
  filters on, and the longer commands its path starts, as `koko skill
  install` of `koko skill`), or else lists every command whose path starts
  with them. Words that start no command are refused with exit 2, as the
  parser refuses them.
  With `--json`, an array of the commands, each as `koko skill --json`
  describes one.
- **`koko completion <shell>`** prints a script for `bash` or `zsh`
  ([`completion.go`](../../internal/client/cli/completion.go)), the login
  shells of the Linux and macOS the CLI runs on, loaded with `eval
  "$(koko completion bash)"`, or for zsh after `compinit` or saved as
  `_koko` in `$fpath`. The script holds the tree as case tables
  (`_koko_next`, `_koko_flags`, `_koko_value`, `_koko_arg`) and one walk over
  the words typed (`_koko_complete`), all in what bash 3.2 and zsh share,
  and completes command words, flags (`--json` among them, for each command
  that takes it), a flag's
  choices, a file or directory for a flag whose value is a `file` or `dir`,
  and an argument's choices; it completes no record name, which only the
  koko server knows, and a flag's value only after a space, not after
  `--name=`. A shell is one entry of `shellScripts`, its head and driver.
  Its tests run the script in bash, and in zsh where it is installed.

## `koko update`

[`koko update`](../../internal/client/cli/update.go) upgrades the CLI where
install.sh puts it, with no root and no restart ([ADR
0047](../adr/0047-koko-updates-itself-in-its-users-own-directory.md)). It
needs no join, never contacts the koko server and never prompts. In this
order, each refusal with nothing changed:

1. `--version` must be a release's tag (`build.Info.Release`), or the
   command line is refused with exit 2.
2. The running koko must be one it may replace: its file
   (`cli.Runner.Executable`, which `cmd/koko` sets to `os.Executable`,
   symlinks resolved) is `$HOME/.local/bin/koko` run by a user other than
   root, or `/usr/local/bin/koko` run by root. A development build,
   `KOKO_IMAGE` set (the image sets it), `/usr/local/bin/koko` run by
   another user and any other path are refused with `update_unsupported`,
   each naming how that koko is upgraded.
3. The target is `--version`'s, or the tag the `releases/latest` redirect
   names (`release.Source.Latest`). When it is this build, it says so and
   exits 0, having asked only for the redirect. `--check` stops here and
   says what it would do.
4. It downloads `SHA256SUMS` and `koko-<os>-<arch>` into a new 0700
   `.koko-update-*` directory beside the file, so the rename stays on one
   filesystem, and checks the SHA-256 and the build: `cmd/koko`, the
   target's version, this platform (`release.Source.Fetch`). A failure is
   `download_failed`, naming the URL, and removes the directory.
5. It sets mode 0755 and renames the file over the old one. A running koko
   keeps its file: open SSH Sessions, `koko web` and the old build's Device
   Mux carry on, and the Device Mux idles out.
6. It points koko's ssh_config's ProxyCommand at the file (below), and
   prints what it did. Going back to a release from before `koko update`
   names the install line as the way back up.

**ssh's ProxyCommand.** koko's ssh_config names its ProxyCommand's koko by
full path, which on a v0.3.x device is `/usr/local/bin/koko`. Every command
run from the user's shell, every one but `koko tunnel` and `koko
device-mux`, by a release build that `koko update` would replace, points
the ProxyCommand at its own file when it runs another
([`devicefiles.PointProxyCommand`](../../internal/client/devicefiles/ssh.go)):
it rewrites that path alone, keeping the bare names, and leaves a file
another user owns, as a user's is to root run with that user's HOME.
That costs one file read per command. `koko update` itself does it only
once it has replaced its file, so a refusal or a failure changes nothing.
The Device Mux's refresh still keeps the path there
([the bare-name refresh](device-mux.md#the-bare-name-refresh)).

## `koko server upgrade`

[`koko server upgrade`](../../internal/client/cli/server_upgrade.go), the
Owner's, upgrades the koko server in place through its upgrade helper
([ADR 0048](../adr/0048-the-koko-server-upgrades-itself-through-a-root-helper-that-takes-only-a-newer-releases-version.md),
[the helper](installer.md#the-upgrade-helper)), then this koko. It is in the
irreversible tier by its path, since the koko server restarts and every open
SSH Session through it ends, and never prompts (ADR 0019).

- **`--check [--json]`** calls `server.check_update`, which checks at once,
  then `server.show` for `upgrade_way`, and changes nothing. It prints the
  koko server's version, the latest release with when it was checked, and
  how the koko server upgrades, as `koko server show`'s `Upgrade:` line
  does; its JSON is `{"version", "latest", "checked_at",
  "update_available", "upgrade_way"}`. It exits 0, or 1 with
  `update_check_failed`, and 2 with `--version`. `--check` only lowers the
  command's risk, so the command keeps its tier, and a harness asks before
  it as before the upgrade; `koko server show` is the read-tier way to see
  the last check's result.
- **The upgrade, `[--version <vX.Y.Z>] [--json]`**, in this order:
  1. `--version` must be a release's tag, or it exits 2.
  2. `server.show`, for the outcome there before, then `server.upgrade`
     with the version, whose refusals (`upgrade_unsupported`,
     `invalid_request`, `upgrade_running`, `owner_only`) are the command's.
     For people it says on stderr: `Asked the koko server to upgrade from
     v0.4.0 to v0.5.0. It restarts, and every open SSH Session through it
     ends.`
  3. It reads `server.show`'s `last_upgrade` every second until an outcome
     other than the one before, to that version, is final, trying each call
     that fails while the koko server restarts again, on a new connection,
     for at most 15 minutes, which covers the helper's 10-minute download.
     Then it gives up with `upgrade_failed`, naming `journalctl -u koko -u
     koko-upgrade`.
  4. A failed outcome exits 1 with `upgrade_failed` and its message, and
     leaves this koko as it was.
  5. After a success, when this koko is older than the release the koko
     server now runs and `koko update` may replace it, it is updated to
     that release by [`koko update`](#koko-update)'s steps, which it prints
     as `koko update` does. Otherwise it is left, and stderr says why and
     the line that brings it there (`koko update --version vX.Y.Z`, with
     `sudo` for root's).

  Its JSON is `{"server": {"from", "to", "state", "message"}, "cli": <koko
  update's JSON> | null}`. With `--json` stderr says nothing on success, so
  that a failure's holds its error object alone.

## The commands so far

| Command | Tier | Calls | `--json` |
| --- | --- | --- | --- |
| `koko space add <space>` (Owner) | reversible | `space.add` | the Space: `{"id", "name", "personal"}` |
| `koko space ls` | read | `space.list` | an array of the Spaces the caller can act in, `{"id", "name", "personal"}`: not the Members and counts `space.list` also answers, since an older koko server answers without them and the CLI could not tell that from none; `space show` prints them |
| `koko space show <space>` | read | `space.show` | the Space with its Members and counts: `{"id", "name", "personal", "members": [{"id", "name"}], "counts": {"hosts", "retired_hosts", "secrets"}}`; a member's `id` is the User's |
| `koko space rename <space> <new-name>` (Owner) | reversible | `space.rename` | the Space under its new name |
| `koko space rm <space>` (Owner) | irreversible | `space.remove` | the Space it removed, as it was |
| `koko space member add <space> <user>...` (Owner) | reversible | `space.member.add` | the Space with its Members after the change, as `space show` prints it |
| `koko space member rm <space> <user>...` (Owner) | reversible | `space.member.remove` | the same |
| `koko user invite <user> [--web] [--valid <duration>]` (Owner) | irreversible | `user.invite` | `{"user": {"id", "name", "role", "created_at", "removed_at", "password_set_at"}, "join_code": ...}`, the User as `user ls` prints one and the Join Code as `device add` prints it; with `--web`, `{"user", "password_link", "expires_at"}`, a Password Link in place of the Join Code ([Sign-ins](signin.md#password-links)); `--valid` as `device add` takes it |
| `koko user ls [--all]` (Owner) | read | `user.list` | an array of the Users by name, `--all` adding the Removed ones: `{"id", "name", "role", "created_at", "removed_at", "password_set_at"}`, `removed_at` `null` while the User is not Removed and `password_set_at` while they have no Password ([Users](users.md#listing)) |
| `koko user remove <user>` (Owner) | irreversible | `user.remove` | `{"outcome", "user"}`: `"deleted"` for a User who never used koko, `"removed"` for anyone else, and the User as the call left them ([Users](users.md#removing)) |
| `koko password link <user> [--valid <duration>]` (Owner) | irreversible | `password.link` | `{"user", "password_link", "expires_at"}`: the link for the User's browser, valid as `--valid` says, 15 minutes without it ([Sign-ins](signin.md#password-links)) |
| `koko password rm <user>` (Owner) | irreversible | `password.remove` | `{"user"}` |
| `koko signin ls [--user <user>] [--all]` | read | `signin.list` | an array of Sign-ins: `{"id", "user", "client", "address", "started_at", "last_used_at", "expires_at", "ended_at", "end_reason"}`, the last two `null` while it is live ([Sign-ins](signin.md#sign-ins)) |
| `koko signin revoke <id>` | irreversible | `signin.revoke` | the Sign-in as it left it, as `signin ls` prints one |
| `koko device add [<device>] [--user <user>] [--valid <duration>]` | irreversible | `device.add` | the Join Code: `{"id", "code", "user", "device", "expires_at"}`; `--valid`, `1m` to `7d` in whole minutes, hours or days, sets `expires_at` (15 minutes without it), and anything else is refused with exit 2 before any call ([Join Codes](join-codes.md#issuing)) |
| `koko device ls [--user <user>] [--all]` | read | `device.list` | an array of Device Sessions, by User and then in the order they joined: `{"id", "user", "device", "added_by", "created_at", "last_used_at", "revoked_at"}`, where `added_by` is `{"id", "user", "device"}` of the Device Session that issued its Join Code, or `null` for the Unix socket; a Device Session's id is `device_...` |
| `koko device revoke <device>` or `<user>/<device>` `[--last]` | irreversible | `device.revoke` | the Device Session it Revoked, as `device ls` prints one ([revoking](device-connection.md#revoking)) |
| `koko join <join-code>` or `koko join -` `[--no-ssh-config]` | irreversible | `user.current`, after joining in SSH authentication | `{"server_address", "device_session", "include_warning"}`, the Device Session as `device ls` prints one, and `include_warning` as `koko status` prints it right after the join, or `null` with `--no-ssh-config` |
| `koko secret ls [<space>]` | read | `secret.list` | an array of Secrets, never a value: `{"id", "space", "name", "created_at", "updated_at"}` ([Secrets](secrets.md)) |
| `koko secret show <space>/<secret>` | irreversible | `secret.show` | the Secret with its value, `"value_base64"`, the one `--json` that holds a value; without `--json`, the value's bytes alone |
| `koko secret add <space>/<secret> [--from-file <file>]` | reversible | `secret.add` | the Secret it stored, without its value |
| `koko secret set <space>/<secret> [--from-file <file>]` | reversible | `secret.set` | the Secret it changed, without its value |
| `koko secret rename <space>/<secret> <new-name>` | reversible | `secret.rename` | the Secret under its new name |
| `koko secret move <space>/<secret> <space>` | reversible | `secret.move` | the Secret in its new Space |
| `koko secret rm <space>/<secret>` | irreversible | `secret.remove` | the Secret it deleted, as it was, without its value |
| `koko connect [<host:port>]` | reversible | none: the koko server's version comes from its report | `{"server_address", "previous_address", "host_keys", "server_version", "device_mux"}`: the Server Address the client config now holds, the one `<host:port>` replaced or `null`, the fingerprints of the pinned host keys, the koko server's version, and the Device Mux as `koko status` shows it, or `null` on the direct path ([`koko connect`](device-connection.md#koko-connect)) |
| `koko disconnect` | reversible | none | `{"device_muxes": [{"socket", "commands_ended"}], "commands_ended"}`: each Device Mux it stopped, by socket, with how many commands were connected to it, which ended, and the sum ([`koko disconnect`](device-mux.md#state-and-exit)) |
| `koko ssh setup` | reversible | `server.show`, then `host.list` | `{"ssh_config", "include_warning"}`: the path of koko's ssh_config it wrote for this OS user on the koko server's own machine, and `include_warning` as `koko status` prints it ([on the koko server's own machine](device-connection.md#on-the-koko-servers-own-machine)) |
| `koko leave` | irreversible | none: it never contacts the koko server | `{"server_address", "device_session"}`: the koko server the device left and its Device Session there, left behind, as `koko status` names one ([`koko leave`](device-connection.md#on-the-device)) |
| `koko status` | read | none: the koko server's version comes from its report; for the Owner, `user.current` on a device, then `server.show` | `{"server_address", "device_session", "socket", "cli_version", "server_version", "server_latest", "server_oldest", "server_error", "device_mux", "revocation", "include_warning", "names_read_as_addresses"}`, below |
| `koko version` | read | none | `{"version", "revision", "go", "platform"}` |
| `koko update [--check] [--version <vX.Y.Z>]` | reversible | none: it never contacts the koko server | `{"path", "from", "to", "updated"}`, or with `--check` `{"path", "installed", "target", "update_available"}` ([`koko update`](#koko-update)) |
| `koko web [--port <port>] [--no-open]` | irreversible | `web.start` | `{"address"}`, once it serves ([`koko web`](web-front.md#koko-web)) |
| `koko server install [--owner <user>] [--address <host[:port]>] [--port <port>] [--web-listen <host:port\|off>] [--kokod <file>]` | irreversible | `server.init`, after it runs `kokod install` | `{"upgraded", "address", "host_key_fingerprint", "root_key_file", "root_key_id", "listen_port", "join_code"}`, the Join Code as `device add` prints it; an upgrade has no `join_code`, and may have `devices_must_connect` ([the installer](installer.md#what-it-prints)). Without `--kokod` it runs the `kokod` installed of its build, or else downloads its release's ([the download](installer.md#the-download)) |
| `koko server upgrade [--check] [--version <vX.Y.Z>]` (Owner) | irreversible | `server.upgrade` and `server.show`; with `--check`, `server.check_update` and `server.show` | `{"server": {"from", "to", "state", "message"}, "cli"}`, or with `--check` `{"version", "latest", "checked_at", "update_available", "upgrade_way"}` ([`koko server upgrade`](#koko-server-upgrade)) |
| `koko server uninstall [--kokod <file>]` | irreversible | none: it runs `kokod uninstall` | `{"removed", "data_dir", "root_key_file", "system_user", "listen_port", "web_listen"}` ([the installer](installer.md#uninstall)). It gets `kokod` as install does, downloading it when none of its build is installed |
| `koko server show` | read | `server.show` | `{"address", "listen_port", "host_key_fingerprint", "version", "root_key_file", "root_key_id", "data_dir", "web_url", "web_listen", "update_check": {"enabled", "checked_at", "latest", "error"}, "upgrade_way", "last_upgrade"}`, `web_url` and `web_listen` `null` while unset and off ([the deployment afterwards](initialisation.md#the-deployment-afterwards)), the check for a new release, each of its fields but `enabled` `null` while unknown ([the update check](initialisation.md#the-update-check)), how the koko server upgrades, `systemd`, `image` or `none`, and the outcome of its last upgrade, `null` while there is none ([how it upgrades](initialisation.md#how-it-upgrades)) |
| `koko server set address=<host[:port]>`, `web_url=<https://host[:port]>`, `update_check=on\|off`, `--unset web_url` (Owner, over the Unix socket only) | reversible | `server.set` | the settings as `server show` prints them, as the change left them |
| `koko host add <host> address=<address> account=<account> [port=<port>] [space=<space>] [<key>=<value>...] [--note-file <file\|->]` | reversible | `host.add` | the Host: `{"id", "name", "space", "address", "port", "account", "state", "host_key", "attributes": {...}, "note", "created_at", "updated_at"}` ([the record](record.md)) |
| `koko host add`, with Hosts as JSON on stdin | reversible | `host.add` | the Host, for one object on stdin, or an array of the Hosts, for an array, in the order given ([Hosts as JSON on stdin](record.md#hosts-as-json-on-stdin)) |
| `koko host ls [<filter>...] [--sort <key>[.desc]]... [--with-note] [--all]` | read | `host.list` | an array of the Hosts the caller can see that match every filter term, by the sort keys and then by name, without `"note"` unless `--with-note`, and without the Retired Hosts unless `--all` ([filtering and sorting](record.md#filtering-and-sorting)) |
| `koko host show <host>` | read | `host.show` | the Host; its Services print for people only |
| `koko host set <host> [<key>=<value>...] [--unset <key>]... [--note-file <file\|->]` | reversible | `host.set` | the Host as the change left it |
| `koko host rename <host> <new-name>` | reversible | `host.rename` | the Host under its new name |
| `koko host move <host> <space>` | reversible | `host.move` | the Host in its new Space |
| `koko host retire <host>` | irreversible | `host.retire` | the Host as it left it, Retired, which only its id names from then on ([retiring a Host](record.md#retiring-a-host)) |
| `koko host rm <host>` | irreversible | `host.remove` | the Host it deleted, as it was, its Services deleted with it |
| `koko host enroll <host> [--password-stdin \| --key <file> \| --secret <space>/<secret>] [--host-key <fingerprint> \| --replace-host-key]` | irreversible | `host.enroll` | the Host, Managed, with its pinned `host_key` ([Enroll](enroll.md)) |
| `koko exec <host> <command...>` | irreversible | none: it opens a `tunnel@koko` channel | none: the command's stdout, stderr and exit status ([`koko exec`](gateway.md#koko-exec)) |
| `koko tunnel <ssh-name>` | irreversible | none: it opens a `tunnel@koko` channel | none: ssh's connection on its stdin and stdout, as its ProxyCommand ([`koko tunnel`](gateway.md#koko-tunnel)) |
| `koko service add <host>/<service> [ports=<port>[/<protocol>],...] [<key>=<value>...] [--note-file <file\|->]` | reversible | `service.add` | the Service: `{"id", "name", "host", "ports": ["443/tcp", ...], "attributes": {...}, "note", "created_at", "updated_at"}` ([the record](record.md#a-service)) |
| `koko service ls [<filter>...] [--sort <key>[.desc]]... [--with-note]` | read | `service.list` | an array of the Services on the Hosts the caller can see that match every filter term, by the sort keys and then by Host and name, without `"note"` unless `--with-note` |
| `koko service show <host>/<service>` | read | `service.show` | the Service |
| `koko service set <host>/<service> [<key>=<value>...] [--unset <key>]... [--note-file <file\|->]` | reversible | `service.set` | the Service as the change left it |
| `koko service rename <host>/<service> <new-name>` | reversible | `service.rename` | the Service under its new name, on the same Host |
| `koko service rm <host>/<service>` | irreversible | `service.remove` | the Service it deleted, as it was |
| `koko attr ls` | read | `attr.list` | an array of every Preset, then every other key in use on what the caller can see: `{"key", "type", "values", "min", "preset", "in_use"}` ([the record](record.md#attributes-and-the-preset-attributes)) |
| `koko skill` | read | none | `{"version", "commands", "filters", "presets", "error_codes", "exit_statuses"}`, [below](#koko-skill-koko-help-and-koko-completion) |
| `koko skill install [--dir <dir>]` | reversible | none | `{"path"}`, the stub's absolute path |
| `koko help [<command>...]` | read | none | an array of the commands shown, each as `koko skill` describes one |
| `koko completion <shell>` | read | none | `{"shell", "script"}`; without `--json`, the script alone |

`koko server run` is no command since the koko server became its own
program ([ADR 0040](../adr/0040-koko-is-the-cli-and-kokod-the-koko-server.md)):
it is refused with exit 2, `the koko server is kokod now: run kokod run,
with the same flags`, whatever flags follow, as is `koko help server run`
([`gone`](../../internal/client/cli/parse.go)). `koko help server` lists
the server commands and says that the koko server's process is `kokod run`,
and `koko skill` says it in one line. `kokod run` takes the flags and
`KOKO_*` variables `koko server run` took, with the same checks and exit
statuses ([`settings.go`](../../internal/server/kokod/settings.go)):
`--data-dir` (`KOKO_DATA_DIR`, default `/var/lib/koko`), `--root-key-file`
(`KOKO_ROOT_KEY_FILE`, default `$CREDENTIALS_DIRECTORY/root-key`, the file
systemd's `LoadCredential=root-key` hands the unit), `--listen`
(`KOKO_LISTEN`, default `:7822`), `--log-level` (`KOKO_LOG_LEVEL`), and
`--owner` (`KOKO_OWNER`) with `--address` (`KOKO_ADDRESS`), with which a
fresh koko server initialises itself at start
([initialisation at first start](initialisation.md#at-first-start)). It
refuses to run as root, with exit 2, naming the fix
([the Seal](seal.md#at-start-up)). The koko server logs to stderr in slog's
text format (`time=... level=INFO msg=... key=value`), one line per event,
for journald to keep.

`koko status` ([`status`](../../internal/client/cli/status.go)) reaches the
koko server as any command does, which may run the revocation cleanup,
then reads the device's files. Every field is always there:
`server_address` and `device_session` are this device's join, the Device
Session as `{"id", "user", "device"}` (as `device ls` names one in
`added_by`), both `null` when it has not joined; `socket` is the Unix socket
path when commands reach the koko server on its own machine, as the Local
caller; `cli_version` is this koko's version and `server_version` the one
the koko server reported on connecting, or `null`; `server_latest` is
the latest koko release the koko server's [update
check](initialisation.md#the-update-check) found, for the Owner only, or
`null` when unknown or for a Member; `server_oldest` is the oldest koko
the koko server works with, as its report names it
([the window](api.md#the-way-in)), or `null` when it was not reached or
names none, as one from before the window; `server_error` is the
error object (`{"code", "message"}`) any command that calls the koko server
would fail with now, such as `not_joined`, `revoked` or
`server_unreachable`, or, when it answered, `cli_too_old` or
`server_too_old` for a CLI and a koko server outside each other's window,
or `null`; `device_mux` is the
[Device Mux](device-mux.md) status reached the koko server through,
`{"socket", "state", "since", "last_error", "waiting_to_resume"}`, where
`state` is `connected` (since it opened its Device Connection, `last_error`
`null`) or `reconnecting` (since its Device Connection dropped or its first
try failed, `last_error` the error object of its last try, as
`server_error` shows it), and `waiting_to_resume` counts the SSH Sessions
through it whose Tunnels wait to Resume, which the human form shows on a
`Resuming:` line when there are any; or `null` when status went another way (the Unix socket, the
direct path) or found no join; `revocation` is the revocation record,
`{"server_address", "device_session", "learned_at"}` with the Device Session
in the same shape, or `null`; and `include_warning` says why ssh does not
read koko's ssh_config for every host and how to fix it (the Include line
missing or below a `Host` or `Match` line, or `~/.ssh/config` left alone or
unreadable, [the device's ssh](device-connection.md#ssh)), or is `null`,
also when the device has not joined; and `names_read_as_addresses` lists
the bare names in koko's ssh_config that ssh reads as an IPv4 address,
such as `201`, `010` or `0xab`, which `ssh <name>` does not reach through
koko ([the device's ssh](device-connection.md#ssh)), in the file's order:
`[]` when there are none, or no koko ssh_config for this `HOME`, and
`null` when koko cannot read it. The human form names the first three,
counts the rest and says to use `ssh <name>.koko`, or says that koko cannot
tell. It exits 0 whether or not the koko
server answered, and 1 only when the device's own files cannot be read.
Two versions are normal on every machine, the koko server's own included,
since a koko server upgraded from the web UI leaves the CLI there as it
was ([ADR 0049](../adr/0049-the-cli-and-the-koko-server-work-together-within-a-window-of-versions.md)).
The human form says how the two work together: `This CLI` ends `(the
koko server runs v0.5.0: koko update brings this CLI to it)` when the koko
server runs a newer release, not a development build, since `koko update`
installs releases; `(older than the oldest the koko server works with,
v0.5.0: run koko update)` when the CLI is below the koko server's oldest;
and the `koko server` line ends `(older than the oldest this CLI works
with, v0.3.0: upgrade the koko server)` when the koko server is below
`compat.Oldest`. Outside the window status still shows both versions and
exits 0, and calls nothing more.

For the Owner, once the koko server answered, status asks for the update
check: over the Unix socket the Local caller is the Owner, and on a device
`user.current` says whether its User is. Only then does it call
`server.show`, so a Member's status makes no `server.show` call. When
`server_latest` is newer than `server_version`, the `koko server` line
ends `(v0.5.0 is available: koko server upgrade)`. Either call failing,
or a koko server older than the check, leaves `server_latest` `null`, and
status still exits 0.
