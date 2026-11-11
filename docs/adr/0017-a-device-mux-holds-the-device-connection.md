---
status: its same-user check on Windows, refined by ADR-0051
---

# A Device Mux holds each device's Device Connection

On a device, every koko command and every `koko tunnel` reach the koko server
through the Device Mux, a background process that holds the device's one
Device Connection. The first command that finds none running starts it, and
it exits after ten minutes with no channel open. Opening a Device Connection
costs about five round trips: at 40 ms RTT, `ssh web1.koko true` took 623 ms
with one per invocation, 353 ms over a shared one and 411 ms direct to the
Host, and agents run many commands in a row.

## Considered Options

- **One Device Connection per command and per `ssh`, sharing later**: no
  state on the device, but every call pays the setup. The prototype chose this
  for P0; this decision revises it.
- **A user service under launchd or systemd**: always warm, but containers
  and headless Linux machines often have no user services, and every device
  would hold a connection all day.
- **OpenSSH's ControlMaster in koko's `ssh_config` block**: it hides `koko
  tunnel`'s errors, overrides the user's own choice, and serves `ssh` but not
  koko commands.

## Consequences

- Its socket sits in the user's private runtime directory:
  `$XDG_RUNTIME_DIR/koko/` on Linux, `$TMPDIR/koko/` on macOS, and otherwise
  `/tmp/koko-<uid>/`, used only if the user owns it with mode 0700. The path
  stays under macOS's 104-byte limit, and a peer-credential check admits only
  the same OS user, who can read the device's key anyway.
- Each CLI build runs its own Device Mux, named by the build in its socket, so
  an upgrade never cuts a live SSH Session and the local protocol needs no
  compatibility rules. After an upgrade two Device Connections overlap until
  the old Device Mux idles out.
- When the koko server says its Device Session was Revoked, the Device Mux
  deletes the device's key and config, leaves a record that later commands
  explain, and exits.
- When its Device Connection drops, it opens a new one and its Tunnels Resume
  on it (ADR 0021). It carries every Tunnel's bytes but holds none of their
  state, which lives in the process that opened each Tunnel, so a Device Mux
  crash ends no SSH Session. A Tunnel waiting to Resume counts as an open
  channel.
- When it cannot start, as in a sandbox that forbids background processes, a
  command opens its own Device Connection with the same code. An environment
  variable forces that path.
