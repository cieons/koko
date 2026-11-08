# The koko server listens on 7822 by default

The koko server listens for SSH on TCP port 7822 unless `koko server install
--port` names another, the Owner's choice of 2026-11-08. This supersedes
the port ADR 0012 chose, 443, and nothing else in it: one port, SSH end to
end, no TLS. 443 is often held by a web server on the same machine, and a
fresh install then stopped with `port_taken` until its Owner found a port
of their own. Any port still works with `--port`, and install never moves
an installed koko server's port by itself.

## Considered Options

- **443, as ADR 0012 chose**: it passes firewalls that let only web traffic
  out. But it is the port a machine that already serves the web holds, so
  the default failed exactly where a koko server shares a machine, and the
  `koko` user could bind it only with `CAP_NET_BIND_SERVICE`.
- **22**: the machine's own sshd keeps it, as ADR 0012 says.
- **A common alternative SSH port, such as 2222**: other SSH servers and
  containers on the same machine reach for those first, so a default there
  meets the same `port_taken`.

## Consequences

- A network that lets only ports 80 and 443 out does not reach a koko
  server on 7822. Its Owner picks `--port 443` for it, and the install docs
  say so.
- P1's SSH-over-WSS fallback, which ADR 0012 planned to share port 443 with
  SSH, told apart by the client's first bytes, no longer has 443 to itself
  by default. It shares whichever one port the Owner picked: on `--port
  443` it works as planned, and on 7822 it helps against a proxy that only
  speaks HTTP but not against a firewall that lets only 443 out. P1 decides
  whether that case needs a second listener on 443; this ADR does not add
  one.
- The unit grants `CAP_NET_BIND_SERVICE` only when its port is below 1024,
  so the default koko service runs with no capability at all. A drop-in
  that moves `KOKO_LISTEN` below 1024 needs `koko server install` run
  again, which reads the drop-in's port and grants it.
- koko v0.1.0 listened on 443 by default, but no koko server ran it when
  this changed, so nothing keeps reading a port left out as 443: the
  installer and `koko server run` take 7822, and a Server Address without
  a port names the port the koko server listens on, as before. A v0.1.0
  koko server upgraded later keeps its port, since install never moves it
  by itself; one restored without its unit needs `--port 443`.
