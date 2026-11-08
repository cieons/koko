# The container runs as uid 65532 from the start, and koko changes no owner

The koko server's image runs as uid:gid 65532:65532 from the start, the
Owner's choice of 2026-11-09. Its base is distroless's `:nonroot` variant,
pinned by digest, whose default user is the numeric 65532, and it sets no
other user. koko never changes a file's owner: it has no `--run-as`,
prepares no directory as root, calls no `lchown` and drops no privilege,
and the container needs no capability at any time. This
supersedes ADR 0037's root-then-drop start and its "no step on the host",
and nothing else in it: one directory at `/koko` still holds `data/` and
`root.key`, and the koko server still makes the Root Key at its first start.

`koko server run` refuses to start when its real, effective or saved uid is
0, wherever it runs, before it writes anything, and names the fix: start it
as a non-root user, and in a container pass no `--user 0` or `user: root`.
So no koko server serves as root, with no privilege code in koko. The
systemd unit already starts it as `koko`.

## Considered Options

- **Root at start, `lchown` the directory, then drop to 65532** (ADR 0037,
  and #96 as first built): mapping one directory would be enough, even one
  Docker made as root. But `koko server run` would carry privilege code: a
  walk that must never follow a symlink or a hard link, a guard so that
  `--root-key-file /etc/koko.key` does not give `/etc` away, and the drop
  with its checks. The container would hold `CHOWN`, `DAC_OVERRIDE`,
  `FOWNER`, `SETGID` and `SETUID` at start, and the image would start as
  root, which scanners flag and `runAsNonRoot` refuses. All of it saves one
  line on the host. Classic images such as postgres and redis do this in a
  shell entrypoint; newer ones such as Grafana, Prometheus, Bitnami's and
  distroless-based images run as a fixed non-root uid and document the
  host step, as koko now does.
- **Running as root throughout:** no drop to get wrong, but a flaw in koko
  would hold root in the container, and every file mounted into it.

## Consequences

- A bind mount needs one step on the host, once, before the first start:
  `sudo install -d -o 65532 -g 65532 -m 0700 /srv/koko`. Without it, Docker
  makes the directory as root, 0755, and start-up refuses with its
  writable-directory refusal, naming the directory and uid and gid 65532,
  and writes nothing. A named volume needs no step: Docker copies the
  image's `/koko`, 65532's and 0700, into a fresh volume.
- A restore, a move, or a move from systemd copies files that must be
  65532's. The docs show `chown -R 65532:65532 /srv/koko`, run as root on
  the Docker host after copying. koko never does it.
- The Compose file keeps `cap_drop: [ALL]` and `no-new-privileges:true`,
  and adds no capability.
- `docker exec koko koko …` runs as the image's default user, 65532, not
  root. The CLI still finds the socket at its fixed path, mode 0666, and is
  the Local caller (ADR 0008). Distroless gives that user
  `HOME=/home/nonroot`.
- Another uid, through `--user` or `user:`, works only where `/koko` and
  `/run/koko` are writable by it, such as with a tmpfs on `/run/koko` owned
  by that uid. 65532 is the documented path. Kubernetes' `runAsNonRoot`
  accepts the image as it is, since its user is numeric and not 0.
