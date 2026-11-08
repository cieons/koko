# A container initialises at its first start and gives its first Join Code to docker exec

A container has no `koko server install` to initialise its koko server
over the Unix socket (ADR 0020). So `koko server run` takes the Owner's
name and the Server Address as two more startup settings, `--owner`
(`KOKO_OWNER`) and `--address` (`KOKO_ADDRESS`), the Owner's choice of
2026-11-08. On a data directory that holds no initialised koko server, it
makes the Root Key when the file is missing (ADR 0037), then initialises
itself before it serves anything. Once the koko server is initialised, it
uses neither setting. This first start issues no Join Code. The Owner
takes the first one with `docker exec koko koko device add`, over the
container's Unix socket as the Local caller, the same way as recovery
(ADR 0013).

## Considered Options

- **An explicit `koko server init` run with `docker exec`**: one step that
  prints everything, as install does. But `docker compose up` alone would
  not bring up a working koko server, and a second command would
  initialise where install already does.
- **The first Join Code in a file in the directory, such as
  `/koko/join-code`**: no command to run. But a credential would sit in the
  directory the Owner backs up, and its 15 minutes start at the first
  start, which a slow pull or `up -d` can spend before the Owner looks. A
  code the koko server has used or let expire would still be there to be
  tried. `koko device add` issues a code when the Owner is ready, and
  shows it only to whoever ran it.
- **The first Join Code in the log**: no log line holds a Join Code (code
  standards), and `docker logs` is kept and often shipped elsewhere.
- **A code issued at the first start and shown later by a command**: the
  koko server keeps only a code's hash, so it cannot show one again.

## Consequences

- `KOKO_OWNER` and `KOKO_ADDRESS` stay in the Compose file after the first
  start. A later start whose values differ from the record logs a warning
  that names `koko server set address=` and changes nothing, so a restart
  never fails over them.
- A fresh container needs `KOKO_ADDRESS`: the container's own IP is always
  private, so the address check that install falls back to has nothing to
  find. When the published port differs from the one the container listens
  on, the address names the published one.
- An Owner name that is not a User name, or an address that is not a
  Server Address, is refused before anything is written.
- Without `KOKO_OWNER`, a fresh koko server starts uninitialised, as under
  systemd before install initialises it. A missing Root Key is then
  refused with a hint naming `KOKO_OWNER`.
- The settings work wherever `koko server run` runs, but only the image's
  docs use them. An installed koko server's unit sets neither.
- Initialising no longer happens only over the Unix socket, so the
  glossary's **Initialise** names the first start too.
- `install.sh` and `koko server install` keep refusing a container that
  systemd does not run, and now name the image.
