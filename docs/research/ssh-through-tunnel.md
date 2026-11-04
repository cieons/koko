# How do ssh, scp and rsync reach a Host through the Tunnel unchanged?

Research note for issue #6. Checked on 2026-11-05 against the OpenSSH
manuals and source (openssh-portable at commit `3c6ec4c`, 2026-11-03,
version 10.6p1), the OpenSSH release notes, the Win32-OpenSSH fork, rsync,
openrsync and Apple's published sources, distro package pages, ICANN and the
IANA root zone. Each claim links to what it rests on. Observations made on
this machine (macOS 27.0.1, `OpenSSH_10.3p1, LibreSSL 3.3.6`) are marked as
local and list the exact command in [Local observations](#local-observations).

## Question

People and agents reach a Host with the tools they already use: `ssh`,
`scp`, `sftp` and `rsync`, with their command lines unchanged. Every SSH
connection ends at the Gateway
([ADR 0002](../adr/0002-every-ssh-session-ends-at-the-gateway.md)), which
the CLI reaches through a Tunnel over WSS on the one HTTPS port
([ADR 0004](../adr/0004-one-https-port-and-wss-tunnels.md)). The Tunnel is
opened with the device's Device Session, so the Gateway accepts SSH `none`
authentication on it
([ADR 0005](../adr/0005-device-session-authenticates-the-ssh-session.md)).
There is no overlay network
([ADR 0009](../adr/0009-no-overlay-network.md)). On a user's machine koko
writes only its own files and joins them to the user's files with one
include line ([code standards](../code-standards.md#what-a-change-contains)).

What must koko put in `ssh_config`, and where, so that the OpenSSH client
runs the Tunnel as a `ProxyCommand`, offers only `none`, trusts the
Gateway's host key, and does the same for `scp`, `sftp` and `rsync`, on the
OpenSSH versions shipped by Linux, macOS and Windows?

## Summary and recommendation

koko writes two files of its own under `~/.ssh/koko/` and puts one line at
the very top of `~/.ssh/config`. Nothing else in the user's file changes.

```
Include ~/.ssh/koko/ssh_config
```

The koko-owned `~/.ssh/koko/ssh_config` (a sketch; the server name and the
binary path are examples):

```
# Written by koko for https://koko.example.com. koko rewrites this file;
# edits here are lost. Put your own settings in ~/.ssh/config.
Host *.koko
    ProxyCommand "/usr/local/bin/koko" tunnel '%h'
    HostKeyAlias koko.example.com
    UserKnownHostsFile ~/.ssh/koko/known_hosts
    StrictHostKeyChecking yes
    UpdateHostKeys no
    PreferredAuthentications none
    ForwardAgent no
```

The koko-owned `~/.ssh/koko/known_hosts` holds one line, the Gateway's
public host key under the alias:

```
koko.example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA...
```

On Windows the ProxyCommand runs without a shell, so the line is
`ProxyCommand "C:\Users\alice\AppData\Local\Programs\koko\koko.exe" tunnel %h`
(quoted `.exe` path, no quotes around `%h`) ([[9]](#p9)).

`koko tunnel <name>` opens the WSS Tunnel with the stored Device Session,
copies stdin to the Tunnel and the Tunnel to stdout, writes its own errors
to stderr, and exits on stdin EOF or SIGHUP ([[1]](#p1), [[3]](#p3)).

| Line | Why it is there | Without it (or if the user's value won) |
| --- | --- | --- |
| first line of `~/.ssh/config` | the first value obtained for a keyword wins ([[11]](#i1)) | the user's `Host *` defaults override koko's values (local [[54]](#o4)) |
| `Host *.koko` | `Host` matches the name typed on the command line ([[33]](#n5)) | nothing scopes the lines; above it they would apply to every host |
| `ProxyCommand ... tunnel '%h'` | the client hands the connection to the Tunnel over stdio ([[1]](#p1)) | ssh resolves `web1.koko` in DNS and fails |
| `HostKeyAlias` | the Gateway shows one key for every Host; the alias is looked up verbatim, with no port ([[24]](#k2)) | one known_hosts line per name and per non-22 port |
| `UserKnownHostsFile` | the key lives in a file koko owns ([[23]](#k1)) | the Gateway's key would have to sit in the user's own `~/.ssh/known_hosts` |
| `StrictHostKeyChecking yes` | never prompt, never add a key ([[23]](#k1)) | the default `ask` prompts and writes on first use |
| `UpdateHostKeys no` | ssh never rewrites koko's file ([[23]](#k1)) | a user's `UpdateHostKeys yes` lets ssh add keys to it |
| `PreferredAuthentications none` | after a refused `none`, try nothing else ([[17]](#a1)) | ssh goes on to offer the user's keys and agent identities to the Gateway ([[19]](#a3), local [[56]](#o6)) |
| `ForwardAgent no` | the user's agent never reaches the Gateway; scp and sftp already pass it themselves ([[35]](#s2)) | a user's `ForwardAgent yes` asks the Gateway to forward the agent |

Every keyword in the file has existed since OpenSSH 7.3, the release that
added `Include` ([[14]](#i4)), so the same file parses on every client in the
[version table](#behaviour-across-versions-and-platforms), down to Windows
10's 7.7p1. `scp`, `sftp` and `rsync` start the system `ssh` and so read the
same config with no change ([[35]](#s2), [[37]](#s4)). Port forwards and
`ControlMaster` work because they ride inside the one SSH connection the
Tunnel carries ([[41]](#f3)).

Left out on purpose: `IdentitiesOnly`, `IdentityAgent none` and `IdentityFile
none` (they only matter once public-key authentication runs, which
`PreferredAuthentications none` prevents, and `IdentityFile` is additive so
`none` does not remove the user's files, [[18]](#a2), [[20]](#a4)),
`GlobalKnownHostsFile none` (only understood since 8.5, [[27]](#k5)),
`CheckHostIP` (already off for a proxy command, [[24]](#k2)), `%p` and `%r`
(the alias makes host-key lookup ignore the port, and the user name reaches
the Gateway in every authentication request, [[17]](#a1)), and `User`,
`Port`, `ServerAliveInterval` and `ControlMaster`, which stay the user's
choice.

The `.koko` suffix is not a delegated top-level domain today, and ssh does
not resolve the name to connect when a ProxyCommand is set ([[29]](#n1),
[[33]](#n5)). It is not reserved, though; `.koko.alt` or `.koko.internal`
are the reserved alternatives if the CLI ticket wants a guarantee
([[30]](#n2), [[31]](#n3)).

## ProxyCommand: tokens and stdio

- <a id="p1"></a>[1] `ssh_config(5)`, ProxyCommand: "The command string
  extends to the end of the line, and is executed using the user's shell
  `exec` directive to avoid a lingering shell process." The command "should
  read from its standard input and write to its standard output". "Host key
  management will be done using the Hostname of the host being connected."
  "CheckHostIP is not available for connections using a proxy command."
  `none` disables the option
  (<https://man.openbsd.org/ssh_config.5#ProxyCommand>, OpenBSD-current,
  dated 2026-10-30).
- <a id="p2"></a>[2] `ssh_config(5)`, TOKENS: "ProxyCommand and ProxyJump
  accept the tokens %%, %h, %n, %p, and %r." `%h` is "The remote hostname",
  `%n` "The original remote hostname, as given on the command line", `%p`
  the remote port, `%r` the remote username. "Tokens are expanded without
  quoting or escaping of shell characters. It is the user's responsibility
  to ensure they are safe in the context of their use"
  (<https://man.openbsd.org/ssh_config.5#TOKENS>).
- <a id="p3"></a>[3] `sshconnect.c`: `expand_proxy_command` prepends
  `exec ` and expands `h`, `k`, `n`, `p` and `r` (`%k`, the HostKeyAlias, is
  expanded but not documented for ProxyCommand). `ssh_proxy_connect` runs
  `$SHELL -c` (falling back to `/bin/sh`) with one pipe on the child's stdin
  and one on its stdout. "Stderr is left for non-ControlPersist connections
  is so error messages may be printed on the user's terminal"; with
  ControlPersist it goes to `/dev/null`. `ssh_kill_proxy_command` sends
  SIGHUP to the proxy when ssh exits
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/sshconnect.c#L124-L337>).
- <a id="p4"></a>[4] `ssh.c` `main`: after the config is read, a `HostName`
  replaces the host, which is then lowercased unless it is an address; this
  is the `%h` given to the proxy
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/ssh.c#L1204-L1222>).
  Local [[52]](#o2): `ssh -p 2222 bob@DB1.koko` gave the proxy
  `db1.koko 2222 bob DB1.koko` for `%h %p %r %n`, and a user line
  `HostName 10.0.0.5` for `web1.koko` turned `%h` into `10.0.0.5`.
- <a id="p5"></a>[5] `ssh.c` `main` refuses a user or host name from the
  command line that fails `ssh_valid_hostname` / `ssh_valid_ruser`
  ("hostname contains invalid characters"). `readconf.c`
  `ssh_valid_hostname` rejects a leading `-`, whitespace, control characters
  and any of `` '`"$\;&<>|(){}, ``
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/ssh.c#L1137-L1144>,
  <https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/readconf.c#L3485-L3498>).
  The ban came in commit `7ef3787c` "ban user/hostnames with most shell
  metacharacters" (2023-12-18), first released in 9.6p1. The 9.6 notes: a
  host name with shell metacharacters referenced "via %u, %h or similar
  expansion token" in a ProxyCommand "could potentially perform command
  injection depending on what quoting was present", which "could arise in
  the case of git submodules". The latest Win32-OpenSSH `ssh.c` has the same
  check
  (<https://github.com/PowerShell/openssh-portable/blob/0172cc6adb99d8b4324b8f0391b5b1fd6f39b2c6/ssh.c#L1147>).
  10.1, 10.3 and
  10.6 tightened the user-name checks further, each adding "Mitigations such
  as this can not be absolute given the variety of shells and user
  configurations in use" (<https://www.openssh.com/releasenotes.html>).
  Local [[52]](#o2): `ssh -G 'a$(id).koko'` fails with that message.
- <a id="p6"></a>[6] `ssh.c` builds the ProxyCommand for ProxyJump as
  `ssh ... -W '[%h]:%p' host`: upstream single-quotes `%h` in the command it
  writes itself
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/ssh.c#L1321-L1340>).
- <a id="p7"></a>[7] `readconf.c` stores the rest of the ProxyCommand line
  verbatim, so quotes and backslashes reach the shell (or, on Windows,
  CreateProcess) unchanged
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/readconf.c#L1582-L1594>).
  ProxyJump "will compete with the ProxyCommand option - whichever is
  specified first will prevent later instances of the other from taking
  effect" (<https://man.openbsd.org/ssh_config.5#ProxyJump>). Local
  [[53]](#o3): a `Host *` `ProxyJump` in the user's file after the include
  did not apply to `web1.koko`; `-J` on the command line replaced koko's
  ProxyCommand.
- <a id="p8"></a>[8] `ProxyUseFdpass`: the ProxyCommand "will pass a
  connected file descriptor back to ssh(1) instead of continuing to execute
  and pass data" (<https://man.openbsd.org/ssh_config.5#ProxyUseFdpass>).
  `ssh_proxy_fdpass_connect` receives one socket and waits for the child to
  exit ([[3]](#p3)). A Tunnel is a WebSocket inside TLS, not a socket ssh
  can speak SSH on, so fd passing does not apply: `koko tunnel` stays
  running and relays bytes.
- <a id="p9"></a>[9] Windows. Win32-OpenSSH's `sshconnect.c`, under
  `FORK_NOT_SUPPORTED`, skips the leading `exec ` and passes the whole
  command string to `posix_spawnp`; no shell runs. `posix_spawn_internal`
  hands it to `build_commandline_string`, which keeps a string that starts
  with `"` as written, puts quotes around the part up to `.exe` when the
  string contains `.exe `, and otherwise quotes the whole string as one
  program name
  (<https://github.com/PowerShell/openssh-portable/blob/0172cc6adb99d8b4324b8f0391b5b1fd6f39b2c6/sshconnect.c#L254-L278>,
  <https://github.com/PowerShell/openssh-portable/blob/0172cc6adb99d8b4324b8f0391b5b1fd6f39b2c6/contrib/win32/win32compat/w32fd.c#L1415>,
  <https://github.com/PowerShell/openssh-portable/blob/0172cc6adb99d8b4324b8f0391b5b1fd6f39b2c6/contrib/win32/win32compat/misc.c#L1841-L2010>,
  branch `latestw_all`, 2026-10-30). So on Windows the binary path must be
  quoted and `'%h'` would arrive with its quotes. ProxyCommand over stdio
  arrived in Win32-OpenSSH v7.6.0.0p1-Beta (2018-03-01): "support for
  `ProxyCommand` using stdio redirection"
  (<https://github.com/PowerShell/Win32-OpenSSH/releases/tag/v7.6.0.0p1-Beta>).
- <a id="p10"></a>[10] Release notes: 8.1 "Allow %n to be expanded in
  ProxyCommand strings"; 8.0 "Redirect stderr of ProxyCommands to /dev/null
  when ssh is started with ControlPersist"
  (<https://www.openssh.com/releasenotes.html>).

What this means for `koko tunnel`:

- It takes exactly one argument, the name, and refuses anything else. A
  pre-9.6 client on Windows does not reject a host name with spaces, and
  CreateProcess would split it into extra arguments ([[5]](#p5),
  [[9]](#p9)).
- `%h`, not `%n`: `%n` in ProxyCommand needs 8.1 ([[10]](#p10)) and RHEL 8
  ships 8.0p1 ([[46]](#v5)). `%h` is lowercased and honours a `HostName` the
  user set for a `.koko` name ([[4]](#p4)).
- On POSIX, `'%h'` in single quotes, as upstream does for ProxyJump
  ([[6]](#p6)). A 9.6+ client (or a distro backport, see the
  [table](#behaviour-across-versions-and-platforms)) already refuses names
  with quotes or `$`; the quotes add a layer for an unpatched older client.
- The binary path is absolute and double-quoted, so it works with spaces
  and does not depend on the `PATH` of whatever program started `ssh`.
- Its stderr is the user's error channel, except under ControlPersist, where
  ssh discards it ([[3]](#p3)).

## Include: where the one line goes

- <a id="i1"></a>[11] `ssh_config(5)`: ssh reads command-line options, then
  `~/.ssh/config`, then `/etc/ssh/ssh_config`. "Unless noted otherwise, for
  each configuration directive, the first specified value will be used."
  "Since the first obtained value for each directive is used, more
  host-specific declarations should be given near the beginning of the file,
  and general defaults at the end." `Include` takes globs and tokens; "Files
  without absolute paths are assumed to be in ~/.ssh if included in a user
  configuration file or /etc/ssh if included from the system configuration
  file. Include directive may appear inside a Match or Host block to perform
  conditional inclusion" (<https://man.openbsd.org/ssh_config.5>).
- <a id="i2"></a>[12] `readconf.c`, `oInclude`: a relative path is joined to
  `~/.ssh`; a path that matches no file (`GLOB_NOMATCH`) is logged at debug
  level and skipped; an included file is read with the permission check; an
  `Include` inside an inactive `Host` or `Match` block reads the file with
  `SSHCONF_NEVERMATCH`, so none of its blocks apply; after the include the
  including file's `Host`/`Match` state is restored ("don't let Match in
  includes clobber the containing file's Match state")
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/readconf.c#L2115-L2201>).
  The same save-and-restore is in the 7.3 code (`git show V_7_3_P1:readconf.c`).
- <a id="i3"></a>[13] `readconf.c`, `read_config_file_depth`: with the
  permission check, a file not owned by root or the user, or writable by
  group or others, is fatal: "Bad owner or permissions on ..."
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/readconf.c#L2690-L2698>).
  On Windows the same check is an ACL check: owned by the user,
  Administrators or SYSTEM, and nobody else may write
  (<https://github.com/PowerShell/openssh-portable/blob/0172cc6adb99d8b4324b8f0391b5b1fd6f39b2c6/readconf.c#L2652-L2660>).
  `~` on Windows is the profile directory read from the registry
  (<https://github.com/PowerShell/openssh-portable/blob/0172cc6adb99d8b4324b8f0391b5b1fd6f39b2c6/contrib/win32/win32compat/pwd.c#L266-L281>).
- <a id="i4"></a>[14] OpenSSH 7.3 (2016-08-01): "Add an Include directive
  for ssh_config(5) files" (<https://www.openssh.com/releasenotes.html>).
- <a id="i5"></a>[15] `ssh_config(5)`, FILES: `~/.ssh/config` "must have
  strict permissions: read/write for the user, and not writable by others";
  `/etc/ssh/ssh_config` "provides defaults for those values that are not
  specified in the user's configuration file"
  (<https://man.openbsd.org/ssh_config.5#FILES>).
- <a id="i6"></a>[16] System-wide drop-ins. Debian 13's `/etc/ssh/ssh_config`
  starts with `Include /etc/ssh/ssh_config.d/*.conf`, then `Host *` with
  `SendEnv LANG LC_* COLORTERM NO_COLOR`, `HashKnownHosts yes` and
  `GSSAPIAuthentication yes`
  (<https://sources.debian.org/src/openssh/1:10.0p1-7+deb13u4/ssh_config/>).
  Apple's `100-macos-ssh.conf` drop-in is `Host *` / `SendEnv LANG LC_*` /
  `Include /etc/ssh/crypto.conf`
  (<https://github.com/apple-oss-distributions/OpenSSH/blob/OpenSSH-354.120.2/conf/100-macos-ssh.conf>);
  locally, `/etc/ssh/ssh_config` has `Include /etc/ssh/ssh_config.d/*` on
  line 22, before any `Host`. Win32-OpenSSH's system file is
  `%programdata%\ssh\ssh_config`
  (<https://github.com/PowerShell/Win32-OpenSSH/releases/tag/V8.6.0.0p1-Beta>).

Where the line goes, and why:

- At the top of `~/.ssh/config`. Local [[54]](#o4): with the include first,
  koko's values held for `db1.koko` and the user's `Host *` filled the rest;
  with the include last, the user's `StrictHostKeyChecking accept-new`,
  `PreferredAuthentications publickey` and `ForwardAgent yes` won for
  `db1.koko`. A global line after the include (`ServerAliveInterval 30`)
  still applied to `example.com`, so koko's `Host *.koko` does not swallow
  the user's file ([[12]](#i2)).
- Never appended at the end. If the user's last block is `Host foo`, the
  include becomes conditional on `foo` and koko's file never applies
  ([[12]](#i2)).
- A missing koko file is harmless: the line is skipped (local [[55]](#o5)).
  Uninstalling can delete `~/.ssh/koko/` and leave the line.
- A broken koko file breaks every `ssh` the user runs, not only `.koko`
  hosts. Local [[55]](#o5): a group-writable include stopped `ssh -G
  example.com` with "Bad owner or permissions", and an unknown keyword
  inside `Host *.koko` stopped it with "Bad configuration option". So koko
  writes its file with mode 0600 by rename, uses only keywords from 7.3 or
  earlier, and puts no directive above its `Host` line (one there would
  apply to every host the user reaches).
- Not `/etc/ssh/ssh_config.d/`. It needs root, it is read after the user's
  file so the user's `Host *` wins ([[11]](#i1)), it is shared by every
  account on the machine, and Windows has no such directory by default
  ([[16]](#i6)).
- Why `~/.ssh/koko/`: the same relative place on every OS, no space in the
  path (macOS's `~/Library/Application Support` has one), and `~` works in
  both `Include` and `UserKnownHostsFile` ([[12]](#i2), [[23]](#k1)).

## Authentication: `none` first, nothing else offered

- <a id="a1"></a>[17] `sshconnect2.c`: `ssh_userauth2` sets the first method
  to `none` before anything else, and `userauth_none` sends the user name,
  the service and `none`. In the `authmethods` table the `none` entry has no
  enable flag, and `authmethod_is_enabled` returns false for that, so `none`
  is never picked again from `PreferredAuthentications`. After a failure,
  `authmethod_get` picks the next name that is both in
  `PreferredAuthentications` and in the server's list; when none is left it
  logs "No more authentication methods to try"
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/sshconnect2.c#L387-L488>,
  <https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/sshconnect2.c#L1026-L1039>,
  <https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/sshconnect2.c#L2407-L2480>).
  The same table and first request are in the 7.3 and 8.0 sources.
- <a id="a2"></a>[18] `pubkey_prepare`, which loads keys and asks the agent
  for its identities, runs only from `userauth_pubkey` (and from `ssh -Z`,
  which lists "the public keys that would be attempted"), so it never runs
  if `none` succeeds or if publickey is never tried
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/sshconnect2.c#L1991-L2003>).
- <a id="a3"></a>[19] `ssh_config(5)`: the default
  `PreferredAuthentications` is `gssapi-with-mic,hostbased,publickey,keyboard-interactive,password`;
  `IdentitiesOnly` limits agent keys to configured files; `IdentityAgent
  none` "disables the use of an authentication agent"; `IdentityFile` "none
  may be used to indicate no identity files should be loaded", but "Multiple
  IdentityFile directives will add to the list"
  (<https://man.openbsd.org/ssh_config.5#PreferredAuthentications>,
  <https://man.openbsd.org/ssh_config.5#IdentityFile>).
- <a id="a4"></a>[20] `ssh.c` `load_public_identity_files` skips only the
  entries named `none`; other entries still load. `IdentityAgent none`
  unsets `SSH_AUTH_SOCK` in ssh's own environment
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/ssh.c#L2417-L2423>,
  <https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/ssh.c#L1662-L1667>).
  Local [[54]](#o4): with koko's `IdentityFile none` first and the user's
  `IdentityFile ~/.ssh/id_test` later, `ssh -G` listed both.
- <a id="a5"></a>[21] ADR 0005: "A Tunnel is opened with that Device
  Session, so the Gateway already knows who is on it and accepts SSH `none`
  authentication for connections that arrive through a Tunnel"
  ([ADR 0005](../adr/0005-device-session-authenticates-the-ssh-session.md)).
- <a id="a6"></a>[22] Gateway side, for the prototype: `golang.org/x/crypto/ssh`
  `ServerConfig.NoClientAuth` allows `none`, and `NoClientAuthCallback`
  "is called when a user attempts to authenticate with auth method none"
  and returns the `Permissions` for the connection
  (<https://github.com/golang/crypto/blob/3d426ad4de978123f3b985c1447bdba85d3bea04/ssh/server.go#L192-L202>).

What this means:

- If the Gateway accepts `none`, the client offers nothing else: no key, no
  agent request, no passphrase or security-key touch, no password prompt
  ([[17]](#a1), [[18]](#a2)). Local [[56]](#o6): against `sshd -i` run as
  the ProxyCommand, the server log shows one request, `method none`.
- `PreferredAuthentications none` only shapes the failure case. Local
  [[56]](#o6): with it, a refused `none` ended at once with "No more
  authentication methods to try" and "Permission denied"; without it, the
  client went on to "Next authentication method: publickey". A refusal
  should still be rare: the Tunnel is refused before SSH starts if the
  Device Session is bad.
- `IdentitiesOnly`, `IdentityAgent none` and `IdentityFile none` add nothing
  once publickey never runs, and `IdentityFile none` would not remove the
  user's own files anyway ([[19]](#a3), [[20]](#a4)).
- The Device Session never travels in SSH authentication. It goes with the
  Tunnel's WSS handshake, as ADR 0005 says ([[21]](#a5)); how (for example
  a header on the upgrade request, which coder/websocket's `DialOptions`
  can carry, per the issue #3 note) is for the prototype. The SSH user name
  still reaches the Gateway in every authentication request ([[17]](#a1)),
  so the ProxyCommand does not need `%r`.

## Host keys: the Gateway's key

- <a id="k1"></a>[23] `ssh_config(5)`: `HostKeyAlias` "Specifies an alias
  that should be used instead of the real host name when looking up or
  saving the host key in the host key database files and when validating
  host certificates. This option is useful for tunneling SSH connections".
  `StrictHostKeyChecking yes`: ssh "will never automatically add host keys
  ... and refuses to connect to hosts whose host key has changed".
  `UserKnownHostsFile` takes `~`, tokens and `none`. `UpdateHostKeys` "is
  enabled by default if the user has not overridden the default
  UserKnownHostsFile setting and has not enabled VerifyHostKeyDNS".
  `HashKnownHosts` affects names "when they are added". `CheckHostIP`
  defaults to `no`
  (<https://man.openbsd.org/ssh_config.5#HostKeyAlias>,
  <https://man.openbsd.org/ssh_config.5#StrictHostKeyChecking>,
  <https://man.openbsd.org/ssh_config.5#UpdateHostKeys>).
- <a id="k2"></a>[24] `sshconnect.c` `get_hostfile_hostname_ipaddr`: with a
  HostKeyAlias the lookup name is the alias as is; without one it is
  `[host]:port` for any port but 22; with a proxy command there is no IP
  ("no hostip for proxy command"). `check_host_key` turns `CheckHostIP` off
  for a proxy command. `ssh.c` lowercases the alias
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/sshconnect.c#L675-L727>,
  <https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/sshconnect.c#L1065-L1071>,
  <https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/ssh.c#L1352-L1353>).
- <a id="k3"></a>[25] `hostfile.c` `check_hostkeys_by_key_or_type`: user and
  system files are loaded into one list, and any line with the matching key
  makes the host OK, even if another line for the same name holds a
  different key
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/hostfile.c#L349-L400>).
- <a id="k4"></a>[26] `sshd(8)`, SSH_KNOWN_HOSTS FILE FORMAT: host names are
  patterns with `*` and `?`; ssh matches "the value of the ssh(1)
  HostkeyAlias if it was specified"; a line may carry the `@cert-authority`
  or `@revoked` marker (<https://man.openbsd.org/sshd.8#SSH_KNOWN_HOSTS_FILE_FORMAT>).
- <a id="k5"></a>[27] OpenSSH 8.5 (2021-03-03): "disable CheckHostIP by
  default", "enables UpdateHostkeys by default subject to some conservative
  preconditions", "allow UserKnownHostsFile=none", "add a ssh_config
  KnownHostsCommand option" (<https://www.openssh.com/releasenotes.html>).
  `ssh.c` treats `GlobalKnownHostsFile none` specially from the 8.5 tag on,
  not at 8.4 (`git show V_8_4_P1:ssh.c`, `git show V_8_5_P1:ssh.c`); before
  that, `none` is just a file name.
- <a id="k6"></a>[28] `readconf.c` `fill_default_options`: `UpdateHostKeys`
  becomes `yes` only when `UserKnownHostsFile` is unset or is the one
  default file, and VerifyHostKeyDNS is off; `CheckHostIP` defaults to 0
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/readconf.c#L2952-L3001>).

What this means:

- The Gateway ends every SSH connection
  ([ADR 0002](../adr/0002-every-ssh-session-ends-at-the-gateway.md)), so
  the client sees the Gateway's key for every Host. `HostKeyAlias` makes one
  known_hosts line match every `.koko` name and every `-p` port
  ([[24]](#k2)). A wildcard line `*.koko ...` would miss `[web1.koko]:2222`
  and any `HostName` the user set.
- `StrictHostKeyChecking yes` and `UpdateHostKeys no` mean ssh only reads
  koko's file. `UpdateHostKeys` would already default to `no` here
  ([[28]](#k6)), but the explicit line beats a user's `UpdateHostKeys yes`.
  `HashKnownHosts yes` from Debian's defaults ([[16]](#i6)) never touches
  the file, since ssh never writes it. Local [[57]](#o7): with a wrong key
  ssh printed the "REMOTE HOST IDENTIFICATION HAS CHANGED" warning and
  "Host key verification failed."; with an empty file, "No ED25519 host key
  is known for koko-gw.test and you have requested strict checking."
- `GlobalKnownHostsFile` stays at its default. `none` only works from 8.5
  ([[27]](#k5)), and a stray system entry for the alias cannot override
  koko's matching line ([[25]](#k3)).
- `KnownHostsCommand` (8.5+) would let `koko` print the key on demand and
  rotate it without rewriting a file, and `@cert-authority` with a
  Gateway host certificate whose principal is the alias would let the key
  rotate under a fixed CA key ([[26]](#k4)). The first is ruled out by
  RHEL 8's 8.0p1 and Windows 10's 8.1p1; the second needs a koko host CA.
  Neither is needed for one Gateway key in P0.

## The Host pattern and the `.koko` name

- <a id="n1"></a>[29] The IANA list of top-level domains, version
  2026110304 (last updated 2026-11-04), has no `KOKO`, `INTERNAL` or `ALT`
  (<https://data.iana.org/TLD/tlds-alpha-by-domain.txt>);
  <https://www.iana.org/domains/root/db/koko.html> returns 404.
- <a id="n2"></a>[30] ICANN Board resolution 2024.07.29.06: "the Board
  reserves .INTERNAL from delegation in the DNS root zone permanently to
  provide for its use in private-use applications"
  (<https://www.icann.org/en/board-activities-and-meetings/materials/approved-resolutions-special-meeting-of-the-icann-board-29-07-2024-en>).
- <a id="n3"></a>[31] RFC 9476 (September 2023, Standards Track) "reserves a
  Top-Level Domain (TLD) label "alt" to be used in non-DNS contexts"
  (<https://www.rfc-editor.org/rfc/rfc9476>).
- <a id="n4"></a>[32] ICANN's 2026 round closed on 2026-08-12 and published
  its 1,615 applications on Reveal Day, 2026-10-07
  (<https://www.icann.org/en/announcements/details/icann-2026-round-application-window-closes-12-august-11-08-2026-en>,
  <https://www.icann.org/en/announcements/details/icann-reveals-2026-round-applications-for-new-generic-top-level-domains-07-10-2026-en>).
  The Reveal Day contention-set file (891 rows) has no `koko`
  (<https://newgtldprogram.icann.org/en/application-rounds/round2/reveal-day-contention-sets.csv>).
- <a id="n5"></a>[33] `ssh_config(5)`: `Host` matches "the hostname argument
  given on the command line"; `Match host` matches "after any substitution
  by the Hostname or CanonicalizeHostname options" and `Match originalhost`
  "the hostname as it was specified on the command-line"; `Match final`
  "requests that the configuration be re-parsed"; `Match exec` "executes the
  specified command under the user's shell"; patterns have only `*`,
  `?` and `!`. `CanonicalizeHostname yes` applies only "for connections that
  do not use a ProxyCommand or ProxyJump"; `always` applies it to proxied
  connections too (<https://man.openbsd.org/ssh_config.5#Host>,
  <https://man.openbsd.org/ssh_config.5#Match>,
  <https://man.openbsd.org/ssh_config.5#CanonicalizeHostname>,
  <https://man.openbsd.org/ssh_config.5#PATTERNS>). `ssh.c`
  `resolve_canonicalize` returns early for a proxied name unless the
  setting is `always`
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/ssh.c#L456-L466>).

What this means:

- With a ProxyCommand ssh does not resolve the name to connect, skips
  `CheckHostIP`, and skips canonicalization unless the user chose `always`
  ([[24]](#k2), [[33]](#n5)). Local [[52]](#o2) ran with names that exist
  in no DNS. The suffix matters mainly to programs that ignore
  `ssh_config`.
- `.koko` is not delegated today and is not in the 2026 round's contention
  sets ([[29]](#n1), [[32]](#n4)), but nothing reserves it. `.internal` is
  reserved for private use ([[30]](#n2)) and so is shared with other
  private namespaces; `Host *.internal` would capture them, while
  `*.koko.internal` would not. `.alt` is reserved for names outside DNS
  ([[31]](#n3)), which is what these names are.
- `Host *.koko` over `koko-*`: a suffix cannot clash with ordinary
  short host names, reads as "a Host in koko", and allows more labels later
  (`web1.team.koko`).
- `Host`, not `Match host`. A user alias `Host web` / `HostName web1.koko`
  does not reach koko's block, because `Host` sees `web`. Catching it would
  need `Match final host *.koko`, which makes ssh parse the whole config
  twice for every host the user reaches ([[33]](#n5)). Not worth it in P0.
- Not `Match exec`. Asking `koko` whether a bare name is a Host would run a
  command under the user's shell for every host the user reaches, before
  ssh connects ([[33]](#n5)).
- A user who sets `CanonicalizeHostname always` together with
  `CanonicalizeFallbackLocal no` would see `.koko` names fail, since no
  DNS suffix resolves them ([[33]](#n5)); koko's block could add
  `CanonicalizeHostname no` if that ever shows up.

## scp, sftp and rsync

- <a id="s1"></a>[34] `scp(1)`: "scp uses the SFTP protocol over an ssh(1)
  connection"; "Since OpenSSH 9.0, scp has used the SFTP protocol for
  transfers by default"; `-O` uses the legacy protocol, which "requires
  execution of the remote user's shell"; `-F` "is directly passed to
  ssh(1)"; `-S program` must "understand ssh(1) options"; `-3` (through the
  local host) "is the default"; `-R` instead runs scp on the origin host,
  which must "authenticate to the destination host without requiring a
  password" (<https://man.openbsd.org/scp.1>).
- <a id="s2"></a>[35] `scp.c` starts `ssh -x -oPermitLocalCommand=no
  -oClearAllForwardings=yes -oRemoteCommand=none -oRequestTTY=no
  -oControlMaster=no`, adds `-oForwardAgent=no` last, and in SFTP mode adds
  `-s` and the subsystem `sftp`
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/scp.c#L481-L487>,
  <https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/scp.c#L626-L629>,
  <https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/scp.c#L260-L332>).
  `sftp.c` passes `-oForwardX11 no -oPermitLocalCommand no
  -oClearAllForwardings yes -oControlMaster no ... -oForwardAgent no -s sftp`
  (<https://github.com/openssh/openssh-portable/blob/3c6ec4c637342c51d9c54bbeecef25fd185cfdb8/sftp.c#L2551-L2554>).
  Local [[58]](#o8) shows the exact argv.
- <a id="s3"></a>[36] Release notes: 8.7 made remote-to-remote copies go
  through the local host by default and added experimental SFTP mode
  (`scp -s`); 9.0 made SFTP the default; 10.0 passes `ControlMaster no`
  from scp and sftp, which "will not prevent scp/sftp from using an
  existing multiplexing session"; 10.6 begins deprecating `scp -R`
  (<https://www.openssh.com/releasenotes.html>).
- <a id="s4"></a>[37] rsync `main.c` `do_cmd`: the remote shell is `-e`,
  else `RSYNC_RSH`, else `ssh`; the string is split on spaces with quote
  handling; then `-l user` if a user was given, `-4`/`-6` if the program is
  named `ssh`, the host, the remote `rsync` path and `--server` options.
  A comment notes that a `host:path` name "belongs to the user's ssh, which
  may be matching it against an ssh_config Host pattern". `rsync.h`:
  `PROTOCOL_VERSION 33`, `MIN_PROTOCOL_VERSION 20`. Latest release v3.5.2
  (2026-11-03)
  (<https://github.com/RsyncProject/rsync/blob/a6b31adfa495513abf7ae002b81fabe29bc50986/main.c#L520-L625>,
  <https://github.com/RsyncProject/rsync/blob/a6b31adfa495513abf7ae002b81fabe29bc50986/rsync.h>,
  <https://github.com/RsyncProject/rsync/releases/tag/v3.5.2>).
- <a id="s5"></a>[38] openrsync on macOS. Apple's `rsync-184` project builds
  its `openrsync` target as `/usr/bin/rsync` and the samba.org rsync only as
  `rsync.samba` under `/AppleInternal/Tests`
  (<https://github.com/apple-oss-distributions/rsync/blob/rsync-184/rsync.xcodeproj/project.pbxproj>,
  tag dated 2026-06-15). Its `fargs.c` `fargs_cmdline` runs `-e`, else
  `RSYNC_RSH`, else `ssh`, then the host (with any `user@`), then `rsync
  --server`
  (<https://github.com/apple-oss-distributions/rsync/blob/rsync-184/openrsync/fargs.c>).
  The installed `openrsync(1)`: "compatible with rsync protocol versions 27
  - 29"; `--rsync-path` names the remote program. Locally, `/usr/bin/rsync
  --version` prints "openrsync: protocol version 29 / rsync version 2.6.9
  compatible".

What this means:

- `scp`, `sftp` and `rsync` start the system `ssh` with the host name the
  user typed, so `Host *.koko` and the ProxyCommand apply with no flags
  ([[35]](#s2), [[37]](#s4), [[38]](#s5)).
- The Gateway must pass two kinds of request through to the Host: the
  `sftp` subsystem (scp 9.0+, sftp) and exec requests (`scp -t`/`scp -f`
  from legacy scp, the default before 9.0; `rsync --server ...`). Local
  [[58]](#o8) shows both.
- The Host needs what the remote side runs: an `sftp` subsystem in its
  sshd, the `scp` binary for legacy scp, the `rsync` binary for rsync.
  koko does not change the Host
  ([ADR 0003](../adr/0003-enroll-only-adds-the-koko-key.md)), so a missing
  one is an error the user sees from the Host.
- A macOS client speaks rsync protocol 29 and the Host's rsync 3.x accepts
  anything from 20 up ([[37]](#s4), [[38]](#s5)), so the pair negotiates
  down to 29.
- Copies between two `.koko` Hosts go through the local machine by default
  and so use two Tunnels; `scp -R` would need koko on the first Host and is
  being deprecated anyway ([[34]](#s1), [[36]](#s3)).

## Port forwards and multiplexing

- <a id="f1"></a>[39] `ssh_config(5)`, ControlMaster, ControlPath,
  ControlPersist: later sessions "will try to reuse the master instance's
  network connection"; ControlPersist keeps the master "open in the
  background" (<https://man.openbsd.org/ssh_config.5#ControlMaster>).
  With ControlPersist, ssh sends the proxy's stderr to `/dev/null`
  ([[3]](#p3), [[10]](#p10)).
- <a id="f2"></a>[40] VS Code Remote-SSH "will look for the `ssh` command in
  the PATH", reads `~/.ssh/config` (or `remote.SSH.configFile`), and "makes
  use of an SSH tunnel to facilitate communication with the host", failing
  with "administratively prohibited" when TCP forwarding is off
  (<https://code.visualstudio.com/docs/remote/troubleshooting>).
- <a id="f3"></a>[41] ADR 0004: "Port forwards travel inside the SSH Session,
  so a Tunnel only has to carry one reliable byte stream"
  ([ADR 0004](../adr/0004-one-https-port-and-wss-tunnels.md)).

What this means:

- `-L`, `-D` and `-R` are channels and requests inside the one SSH
  connection, so the Tunnel carries them unchanged; the Gateway has to
  relay them over its own connection to the Host ([[41]](#f3)). `ssh -J
  web1.koko other` works the same way: the outer ssh reaches `web1.koko`
  through the Tunnel and asks it for a TCP channel. VS Code Remote-SSH needs
  exactly this ([[40]](#f2)).
- One control master is one SSH connection and so one Tunnel. Sessions that
  join it share that Tunnel; revoking the Device Session cuts the Tunnel and
  with it the master and every session on it
  ([ADR 0005](../adr/0005-device-session-authenticates-the-ssh-session.md)).
- Under ControlPersist the Tunnel outlives the first session and `koko
  tunnel`'s stderr is discarded ([[39]](#f1)), so koko's own log is the only
  place its errors land in that mode.

## Behaviour across versions and platforms

| Platform | OpenSSH client | Rejects metacharacters in host names | `%n` in ProxyCommand (8.1) | `KnownHostsCommand`, `UserKnownHostsFile none` (8.5) | scp default | ProxyCommand runs via |
| --- | --- | --- | --- | --- | --- | --- |
| Upstream | 10.6p1, 2026-10-06 [[42]](#v1) | yes (9.6+) [[5]](#p5) | yes | yes | SFTP | `$SHELL -c "exec ..."` [[3]](#p3) |
| macOS 27.0.1 | 10.3p1 (local); Apple source tag 10.2p1 [[48]](#v7) | yes | yes | yes | SFTP | `$SHELL -c "exec ..."` |
| Ubuntu 22.04 | 1:8.9p1-3ubuntu0.18 [[43]](#v2) | backported in 1:8.9p1-3ubuntu0.6 [[45]](#v4) | yes | yes | legacy; SFTP with `-s` | `$SHELL -c` |
| Ubuntu 24.04 | 1:9.6p1-3ubuntu13.20 [[43]](#v2) | yes | yes | yes | SFTP | `$SHELL -c` |
| Ubuntu 26.04 | 1:10.2p1-2ubuntu3.8 [[43]](#v2) | yes | yes | yes | SFTP | `$SHELL -c` |
| Debian 12 | 1:9.2p1-2+deb12u10 [[44]](#v3) | backported [[45]](#v4) | yes | yes | SFTP | `$SHELL -c` |
| Debian 13 | 1:10.0p1-7+deb13u4 [[44]](#v3) | yes | yes | yes | SFTP | `$SHELL -c` |
| RHEL 8 | 8.0p1 (CentOS Stream 8 spec) [[46]](#v5) | patch in spec [[46]](#v5) | **no** | **no** | legacy only | `$SHELL -c` |
| RHEL 9 | 8.7p1 at GA; CentOS Stream 9 rebased to 9.9p1 on 2025-09-10 [[46]](#v5) | patch in spec [[46]](#v5) | yes | yes | 8.7: legacy, `-s`; 9.9: SFTP | `$SHELL -c` |
| RHEL 10 | 9.9p1 (CentOS Stream 10 spec) [[46]](#v5) | yes | yes | yes | SFTP | `$SHELL -c` |
| Windows 10 1809, LTSC 2019 | 7.7p1 (community count) [[47]](#v6) | **no** | **no** | **no** | legacy only | CreateProcess, no shell [[9]](#p9) |
| Windows 10 21H2/22H2, Server 2022 | 8.1p1 (community count) [[47]](#v6) | **no** | yes | **no** | legacy only | CreateProcess |
| Windows 11 22H2/23H2 | 8.6p1 (community count) [[47]](#v6) | **no** | yes | yes | legacy only | CreateProcess |
| Windows 11 24H2 | 9.5p1 (unconfirmed report) [[47]](#v6) | **no** | yes | yes | SFTP | CreateProcess |
| Win32-OpenSSH from GitHub | 10.4.0.0p1-Preview, 2026-10-20 [[47]](#v6) | yes | yes | yes | SFTP | CreateProcess |

Every row is 7.3 or later, so every row has `Include` and every keyword
koko's file uses ([[14]](#i4)). "Backported" means the
distro patched CVE-2023-51385 into its older version; on Windows the
missing check matters less because no shell runs, and `koko tunnel`'s
one-argument rule covers argument splitting ([[9]](#p9)).

- <a id="v1"></a>[42] OpenSSH 10.6/10.6p1 was released on 2026-10-06; 9.0 on
  2022-04-08; 8.5 on 2021-03-03; 8.1 on 2019-10-09; 7.3 on 2016-08-01
  (<https://www.openssh.com/releasenotes.html>).
- <a id="v2"></a>[43] `openssh-client` in Ubuntu jammy, noble and resolute
  (<https://packages.ubuntu.com/jammy/openssh-client>,
  <https://packages.ubuntu.com/noble/openssh-client>,
  <https://packages.ubuntu.com/resolute/openssh-client>).
- <a id="v3"></a>[44] `openssh-client` in Debian bookworm, trixie and forky
  (forky has 1:10.6p1-1)
  (<https://packages.debian.org/bookworm/openssh-client>,
  <https://packages.debian.org/trixie/openssh-client>,
  <https://packages.debian.org/forky/openssh-client>).
- <a id="v4"></a>[45] CVE-2023-51385, "In ssh in OpenSSH before 9.6, OS
  command injection might occur if a user name or host name has shell
  metacharacters": Ubuntu 22.04 fixed in 1:8.9p1-3ubuntu0.6, 24.04 in
  1:9.6p1-3ubuntu1 (<https://ubuntu.com/security/CVE-2023-51385>); Debian
  lists bullseye, bookworm and trixie as fixed
  (<https://security-tracker.debian.org/tracker/CVE-2023-51385>).
- <a id="v5"></a>[46] CentOS Stream specs: `c8s` has `openssh_ver 8.0p1` and
  `openssh-9.6p1-CVE-2023-51385.patch`; `c9s` has `openssh_ver 9.9p1`, with
  the changelog entry "Wed Sep 10 2025 ... 9.9p1-1 - Rebase to version 9.9"
  and an earlier 8.7p1 entry "Resolves: CVE-2023-51385"; `c10s` has
  `openssh_ver 9.9p1`
  (<https://gitlab.com/redhat/centos-stream/rpms/openssh/-/blob/c8s/openssh.spec>,
  <https://gitlab.com/redhat/centos-stream/rpms/openssh/-/blob/c9s/openssh.spec>,
  <https://gitlab.com/redhat/centos-stream/rpms/openssh/-/blob/c10s/openssh.spec>).
- <a id="v6"></a>[47] Windows. Microsoft says OpenSSH is available
  "Beginning with Windows 10 build 1809 and Windows Server 2019" but
  publishes no version table (page dated 2025-02-20,
  <https://learn.microsoft.com/en-us/windows-server/administration/openssh/openssh-overview>).
  Win32-OpenSSH issue #2053 "Which Win32-OpenSSH versions released with
  which Windows releases?" is open since 2023-04-12; a commenter's table
  from Windows ISOs gives 7.7p1, 8.1p1 and 8.6p1 as in the rows above, and
  notes an OpenSSH update shipped silently in a cumulative update
  (KB5003173); 9.5p1 for Windows 11 24H2 is a second-hand report there
  (<https://github.com/PowerShell/Win32-OpenSSH/issues/2053>). Latest
  Win32-OpenSSH release: 10.4.0.0p1-Preview, 2026-10-20
  (<https://github.com/PowerShell/Win32-OpenSSH/releases>).
- <a id="v7"></a>[48] Apple's latest published OpenSSH source, tag
  `OpenSSH-354.120.2` (2026-06-15), defines `OpenSSH_10.2` `p1`
  (<https://github.com/apple-oss-distributions/OpenSSH/blob/OpenSSH-354.120.2/openssh/version.h>).

## Tools that read ssh_config themselves

A note only. Tools that run the system `ssh` (VS Code Remote-SSH, git,
rsync, scp) get koko's block for free ([[40]](#f2), [[5]](#p5),
[[37]](#s4), [[35]](#s2)). Libraries that speak SSH themselves do not:

- <a id="t1"></a>[49] `golang.org/x/crypto/ssh` has no `ssh_config` parser;
  its `ssh` directory has no such file
  (<https://github.com/golang/crypto/tree/3d426ad4de978123f3b985c1447bdba85d3bea04/ssh>).
- <a id="t2"></a>[50] `github.com/kevinburke/ssh_config` parses `Include`
  but "the `Match` directive is currently unsupported", and it does not run
  a ProxyCommand (<https://github.com/kevinburke/ssh_config#readme>).

Such a tool reaches a `.koko` name only if it runs the ProxyCommand itself.

## Local observations

All on macOS 27.0.1 with `OpenSSH_10.3p1, LibreSSL 3.3.6`, `-F` pointing at
test files in a scratch directory `$S`, so no real `~/.ssh` file was read
or written and no network was used. `$K` is `$S/dir with space/argv.sh`, a
script that logs its arguments and exits 1; `$L` is `$S/local`.

- <a id="o1"></a>[51] `/usr/bin/rsync --version` printed "openrsync: protocol
  version 29" and "rsync version 2.6.9 compatible".
- <a id="o2"></a>[52] With `ProxyCommand "$K" %h %p %r %n` under `Host
  *.koko`: `ssh -F $S/exec.conf -p 2222 bob@DB1.koko true` logged
  `db1.koko`, `2222`, `bob`, `DB1.koko`; the parent of the script was `ssh`
  itself (the shell had `exec`ed); stderr "koko: test proxy refusing"
  reached the terminal, then "Connection closed by UNKNOWN port 65535",
  exit 255. With `Host web1.koko` / `HostName 10.0.0.5` below it, `ssh -F
  $S/exec.conf web1.koko true` logged `10.0.0.5`, `22`, `cieons`,
  `web1.koko`. `ssh -G -F $S/exec.conf 'a$(id).koko'` printed "hostname
  contains invalid characters"; `-l 'x;id'` printed "remote username
  contains invalid characters".
- <a id="o3"></a>[53] With the sketch file included first and a user
  `Host *` with `ProxyJump bastion.example.net`, `ssh -G -F
  $S/sketch/user-config Web1.koko` showed koko's `proxycommand` and no
  `proxyjump`, while `example.com` showed the ProxyJump. `ssh -G -F
  $S/sketch/user-config -J jump.example.net web1.koko` showed only
  `proxyjump jump.example.net`. Running `ssh -F $S/sketch/user-config
  Web1.koko true` with `$K` as the binary logged `tunnel`, `web1.koko` for
  `tunnel '%h'`.
- <a id="o4"></a>[54] `ssh -G -F $S/user-top.conf db1.koko` (include first)
  versus `ssh -G -F $S/user-bottom.conf db1.koko` (include after `Host *`):
  first gave `stricthostkeychecking true`, `preferredauthentications none`,
  `forwardagent no`; last gave `accept-new`, `publickey`, `yes`. Both listed
  `identityfile none` and `identityfile ~/.ssh/id_test`.
  `serveraliveinterval 30`, a global line after the include, applied to
  `example.com`.
- <a id="o5"></a>[55] `Include $S/nonexistent.conf` was skipped silently.
  An include with mode 0664 made `ssh -G -F $S/gw.conf example.com` fail
  with "Bad owner or permissions on .../koko-gw.conf". `FooBarBaz yes`
  inside `Host *.koko` in an included file made `ssh -G -F
  $S/unknown.conf example.com` stop with "Bad configuration option:
  foobarbaz" and "terminating, 1 bad configuration options".
- <a id="o6"></a>[56] `ssh -vvv -F $L/client.conf web1.koko true` with
  `ProxyCommand /usr/sbin/sshd -i -f $L/sshd_config -E $L/sshd.log`,
  `HostKeyAlias koko-gw.test`, `UserKnownHostsFile $L/known_hosts`,
  `StrictHostKeyChecking yes`, `PreferredAuthentications none`: the client
  logged "using hostkeyalias: koko-gw.test", "Host 'koko-gw.test' is known
  and matches the ED25519 host key", one `send packet: type 50`, "No more
  authentication methods to try" and "Permission denied
  (publickey,password,keyboard-interactive)"; the server log showed
  "userauth-request for user cieons service ssh-connection method none" and
  nothing else. Without the `PreferredAuthentications` line (and with
  `-o BatchMode=yes -o IdentityAgent=none`) the client logged "Next
  authentication method: publickey".
- <a id="o7"></a>[57] Same setup with a different key under `koko-gw.test`:
  the "IT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!" warning, "Host
  key for koko-gw.test has changed and you have requested strict checking."
  and "Host key verification failed." With an empty file: "No ED25519 host
  key is known for koko-gw.test and you have requested strict checking."
  The known_hosts file was unchanged afterwards (same MD5).
- <a id="o8"></a>[58] With `-S $L/fake-ssh.sh` (prints its argv): `scp
  web1.koko:/etc/hosts ...` ran `-x -oPermitLocalCommand=no
  -oClearAllForwardings=yes -oRemoteCommand=none -oRequestTTY=no
  -oControlMaster=no -oForwardAgent=no -s -- web1.koko sftp`; `scp -O
  bob@web1.koko:/etc/hosts ...` ended `-l bob -- web1.koko scp -f
  /etc/hosts`; `sftp web1.koko` ran `-oForwardX11 no -oPermitLocalCommand
  no -oClearAllForwardings yes -oControlMaster no -oForwardAgent no -s --
  web1.koko sftp`. `/usr/bin/rsync -av -e $L/fake-ssh.sh
  alice@web1.koko:/srv/data/ ...` ran `alice@web1.koko rsync --server
  --sender -g -l -o -p -D -r -t -v --dirs . /srv/data/`. `scp -v -F
  $L/client.conf web1.koko:/etc/hosts ...` showed scp's ssh reading the
  same config and running the same ProxyCommand.

## What the choice costs

- One line at the top of the user's `~/.ssh/config`. If the user moves it
  below their `Host *`, their defaults beat koko's ([[54]](#o4)). koko can
  detect this without the network by reading `ssh -G <name>.koko` and
  checking its own `proxycommand` and `hostkeyalias` come back.
- koko's file can break every `ssh` on the machine, not just `.koko` hosts:
  a wrong mode, a wrong Windows ACL or a keyword an old client does not know
  ([[13]](#i3), [[55]](#o5)). koko writes it atomically, mode 0600, with
  keywords from 7.3 or earlier, and every directive under `Host *.koko`.
- One `koko` process, one WSS handshake and one TLS handshake per SSH
  connection. `ControlMaster` in the user's own config amortises it
  ([[39]](#f1)).
- The absolute binary path is baked into the file; a move or a package
  manager that changes the path needs koko to rewrite it.
- `%h` is expanded unquoted into a shell command ([[2]](#p2)). On an
  unpatched client older than 9.6, a host name chosen by someone else (for
  example in a git submodule URL ending in `.koko`) could inject shell
  commands; single quotes cover every such name without a `'`. Patched
  distro clients and 9.6+ refuse those names outright ([[5]](#p5),
  [[45]](#v4)).
- The Gateway's key is pinned in a file koko maintains. Rotating the
  Gateway key means rewriting that file on every device before the old key
  goes ([[23]](#k1)).
- `.koko` is not reserved ([[29]](#n1)). If it is ever delegated, programs
  that ignore `ssh_config` would resolve `.koko` names on the Internet; ssh
  itself would not.
- Programs that parse `ssh_config` themselves or speak SSH as a library
  bypass the Tunnel ([[49]](#t1), [[50]](#t2)).
- Hosts need `sftp-server`, `scp` or `rsync` for the matching tool, and the
  Gateway must relay subsystem and exec requests ([[35]](#s2),
  [[58]](#o8)).

## Open questions

- **Which Host a name means.** How the labels before `.koko` map to a Host
  across Spaces (uniqueness, a Space label, case: `%h` is always lowercase,
  [[4]](#p4)). Handed to
  [CLI command tree](https://github.com/cieons/koko/issues/12) (#12) and
  [Data model and the Preset Attribute list](https://github.com/cieons/koko/issues/8)
  (#8).
- **The suffix.** `.koko` (short, not reserved) against `.koko.alt` or
  `.koko.internal` (reserved, longer). Handed to
  [CLI command tree](https://github.com/cieons/koko/issues/12) (#12),
  which already owns "what the ssh_config Include that koko writes
  contains".
- **Writing the include.** Where koko's files live (this note suggests
  `~/.ssh/koko/`), creating `~/.ssh/config` when absent, keeping its mode
  and line endings, and what to do when it is a read-only symlink (as dotfile
  managers make it): print the line instead of writing. Whether WSL, Git for
  Windows or MSYS2 `ssh`, each with its own home, get their own include.
  Handed to [CLI command tree](https://github.com/cieons/koko/issues/12)
  (#12).
- **What the Tunnel carries and how `koko tunnel` fails.** How the Device
  Session and the Host name travel in the WSS handshake, what `koko tunnel`
  prints when the Device Session is missing or revoked, and that it exits on
  stdin EOF and SIGHUP; what the client really does with `none`, and the
  latency. Handed to
  [Prototype: a WSS Tunnel and terminating Gateway carrying ssh, scp and rsync](https://github.com/cieons/koko/issues/15)
  (#15).
- **Requests the Gateway receives anyway.** `SendEnv LANG LC_*` from Debian
  and Apple defaults ([[16]](#i6)); agent forwarding (blocked here by
  `ForwardAgent no`, but the Gateway should refuse it too, and whether it is
  ever wanted); X11; the SSH user name against the Host Account; `-p`, which
  koko ignores; exec and `sftp` subsystem pass-through; `-L`, `-R`, `-D` and
  the direct TCP channels VS Code needs; a refusal banner when `none` is
  denied. Handed to
  [Gateway and Tunnel behaviour details](https://github.com/cieons/koko/issues/16)
  (#16).
- **Getting and rotating the Gateway key.** koko can fetch it over the
  authenticated API at sign-in, but a proxy that ends TLS in front of the
  koko server sees that answer too, so the pin then protects only against
  everything outside that proxy. Whether to show the fingerprint for an
  out-of-band check, and how the file is refreshed on rotation
  (`KnownHostsCommand` and `@cert-authority` are later options,
  [[26]](#k4), [[27]](#k5)). Handed to
  [Gateway and Tunnel behaviour details](https://github.com/cieons/koko/issues/16)
  (#16) and
  [Install and run the koko server](https://github.com/cieons/koko/issues/14)
  (#14).
- **The oldest client koko supports.** 7.3 is the floor for `Include`;
  whether koko also states a floor for the metacharacter check (9.6 or a
  distro backport) on POSIX. Handed to
  [CLI command tree](https://github.com/cieons/koko/issues/12) (#12).

## Not verified

- Nothing was run on Linux or Windows. Older OpenSSH behaviour was read from
  the source at release tags, not run. Windows ProxyCommand behaviour was
  read from the `latestw_all` branch only; the inbox builds (7.7p1 to 9.5p1)
  may differ.
- Windows inbox versions come from a community table and a second-hand
  report in Win32-OpenSSH issue #2053; Microsoft publishes none, and updates
  have shipped inside cumulative updates.
- RHEL versions come from CentOS Stream specs, not RHEL errata; which RHEL 9
  minor release first carries 9.9p1, and RHEL's own CVE-2023-51385 status
  (Red Hat's CVE page showed no product table), were not checked.
- Whether any 2026-round application asks for `koko`: only the contention
  set file was searched; the full list of 1,615 applications was not.
- ProxyCommand under a user `$SHELL` that is not POSIX-like (fish, nushell,
  PowerShell) was not tested; `exec` and double quotes are assumed.
- Which macOS release first shipped openrsync as `/usr/bin/rsync`.
- That the Windows `~` and ACL rules accept a file koko creates under
  `%USERPROFILE%\.ssh\koko\` with inherited permissions.
- VS Code Remote-SSH's own parsing of `Include` for its host list.
