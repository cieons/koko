---
status: its start as root, giving the directory to uid 65532, superseded by ADR-0039
---

# The container keeps its data and its Root Key in one directory

The koko server's container image keeps everything it needs in one
directory, mounted at `/koko`: the data directory `/koko/data` and the Root
Key `/koko/root.key` beside it. Mapping one host directory, such as `-v
/srv/koko:/koko`, is all a container needs: no step on the host, no secret
to mount, no key to make first, the Owner's choice of 2026-11-08. The
container starts as root only to make that directory and give it to uid
65532 (0700, with `root.key` and `data/` inside). It then drops to uid
65532 for good, before it reads the Root Key, opens the store or listens.
As uid 65532, the koko server makes the Root Key at its first start when
the file is missing and the data directory holds no sealed data, and never
over sealed data.

This weakens ADR 0018 in the container, and only there. The Root Key is
still outside the data directory, so a copy of `/koko/data` alone reveals
no Secret and no private key. But a copy of the whole directory carries
the data and its key together, and the koko process can read, change or
delete its own Root Key's file, which systemd's `LoadCredential=` prevents
on an installed koko server.

## Considered Options

- **Running as uid 65532 from the start, with the host directory given to
  it by hand first** (`install -d -o 65532 …`): the image would never run
  as root. But Docker makes a missing bind-mount source as root, so every
  first start would need a step on the host, and mapping one directory
  would not be enough.
- **A Root Key made on the host with a koko command and mounted read-only
  as a Compose secret**: the closest to ADR 0018, since the key sits apart
  and the process cannot change it. But there are two things to mount, and
  a step on the host before the first start.
- **The key in a second Docker volume, made at the first start**: no step
  on the host. But the data and the key are still together, now in
  Docker's storage, where the Owner is less likely to see the key or keep
  a copy of it.
- **The key inside the data directory**: one directory too. But the koko
  server refuses a Root Key inside its data directory (P0 spec: install and
  run), so that no drop-in puts it back beside the data on a systemd
  install. A subdirectory keeps that one rule for both, and lets the Owner
  back up `data/` on its own.
- **An entrypoint that drops privileges with `gosu` or `su-exec`**: no
  privilege code in koko. But distroless has no shell to run the script,
  and a second binary would sit in the image that no release attests.

## Consequences

- The image starts as root, which image scanners flag and Kubernetes'
  `runAsNonRoot` refuses. `koko server run` gains a startup setting,
  `--run-as <uid>:<gid>` (`KOKO_RUN_AS`), which the image sets to
  `65532:65532`. Started as root, it refuses to run without that setting,
  so no koko server ever serves as root.
- What runs as root is short and touches only directories. It makes the
  data directory, the Root Key's directory and the socket's directory. It
  gives them, and what is in them, to the run-as user, without following a
  symlink. It then drops every group and both ids. It reads no file's
  content but the mount table, opens no network connection, and is done
  before the Root Key is read or anything listens. After the drop, the
  process holds no capability and cannot regain root.
- Started as uid 65532 instead, as `docker run --user 65532:65532` or a
  `runAsNonRoot` pod with `runAsUser: 65532` does, the koko server skips
  all of this. It works when the directory is already writable by uid
  65532, such as one given to it by hand or by `fsGroup`. A start that
  cannot write it is refused before anything is written, naming the fix.
- The docs tell the Owner to keep a copy of `root.key` off the machine at
  once, such as in a password manager, to back up `data/` on its own,
  copied while the container is stopped, and to keep a copy of the whole
  directory only where every Secret could be kept. A disk snapshot of the
  machine holds both, but the provider controls the machine already
  (ADR 0018).
- Without a copy of `root.key`, the way on is ADR 0018's: move `data/`
  aside and start again, recording and Enrolling every Host anew.
- `KOKO_ROOT_KEY_FILE` still names any file. An Owner who wants the key
  apart mounts it elsewhere, read-only, and points the variable at it. The
  image's default is the one directory.
- An installed koko server on systemd is unchanged: its unit starts it as
  `koko` and sets no `KOKO_RUN_AS`.
