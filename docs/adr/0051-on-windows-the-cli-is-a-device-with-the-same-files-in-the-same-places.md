# On Windows the CLI is a device with the same files in the same places

People on Windows use koko as a device, the Owner's choice of 2026-11-11:
the CLI runs on Windows 10 1809 and later, on x64, with every command a
device has on Linux and macOS, and the koko server stays Linux-only. A
device is still an OS user's home, which on Windows is the profile
directory, `%USERPROFILE%`; `HOME` is never read there, since OpenSSH for
Windows takes the home from the profile too. Under it the layout is the
one Linux and macOS have: the CLI is `.local\bin\koko.exe`, and the
device's files are in `.ssh\koko\`, beside the configuration `ssh` reads.
Where Linux and macOS set modes 0700 and 0600, koko sets a protected access
control list, one that inherits nothing, with full control for this user,
`SYSTEM` and `Administrators`, the principals OpenSSH for Windows itself
accepts on a private key.

The Device Mux listens on a Unix socket, which Windows has had since
Windows 10 1803, one per build, in `%LOCALAPPDATA%\koko\run\`, a directory
private in the same way. It is started detached from the console, and takes
its lock with `LockFileEx`. `ssh` is OpenSSH for Windows, the client
Microsoft ships. koko's ssh_config is written in a form Git for Windows'
`ssh` reads as well: the koko path with forward slashes, known_hosts by
`~`, and `'%h'` kept, whose single quotes `koko tunnel` removes on Windows,
where no shell does. CI runs both clients, and the docs call Git's "also
working".

This refines ADR 0017: on Windows no peer-credential check admits a
command, since Windows gives a Unix socket none, and the private directory
is the only gate. It refines ADR 0040: the Windows build of `koko`,
compiled only until now, is released.

## Considered Options

- **`%APPDATA%` or `%LOCALAPPDATA%` for the device's files:** where Windows
  programs keep their data, but koko's ssh_config and known_hosts must sit
  where `ssh` finds them, so the key and the client config would be split
  from them, and the docs would describe two layouts.
- **`%LOCALAPPDATA%\Programs\koko` for the CLI:** Microsoft's folder for a
  user's own programs, but a second path rule in `koko update`, the docs
  and every message. The Owner chose `.local\bin`.
- **A named pipe for the Device Mux:** the usual local transport on
  Windows, but it needs a third-party package, a second transport in the
  Device Mux, a guard against another user creating the pipe's name first,
  and a way for `koko disconnect` to list pipes.
- **A Windows service for the Device Mux:** always warm, but installing one
  needs an administrator, and ADR 0017 rejected a user service already.
- **Only OpenSSH for Windows, or both clients promised alike:** the first
  leaves `ssh web1` broken in Git Bash, which many people on Windows use;
  the second binds every later change of koko's ssh_config to a client
  koko does not choose.

## Consequences

- Each OS user installs and joins for themselves. Windows has no
  counterpart of root's `/usr/local/bin/koko`.
- A user whose `HOME` names another directory than the profile may have an
  `ssh` that reads another configuration than the one koko wrote.
  `koko status` says so; koko writes under the profile only.
- The runtime directory is refused, and commands go direct, unless this
  user owns it and its list gives no other principal access. The Device
  Mux checks it again at each start.
- A Device Mux started inside a job that ends its processes ends with that
  job. No SSH Session is lost: each Tunnel's state is in its own command,
  which Resumes through the next Device Mux.
- A missing OpenSSH client blocks only `ssh`: `koko join`, `koko exec` and
  `koko web` work without it, and `koko status` names the feature to add.
- WSL is a Linux machine: the Linux koko is installed in it, as a device of
  its own.
- CI gains a Windows job, which runs the CLI's tests against the koko
  server in process, without the race detector, and the real OpenSSH test
  with both clients. The koko server's own packages are not tested there.
