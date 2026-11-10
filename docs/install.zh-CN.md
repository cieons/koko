# 安装 koko

[English](install.md)

koko 的每个发布版本包含两个程序：CLI `koko`，支持 Linux 和 macOS；koko 服务器 `kokod`，仅支持 Linux。一行命令即可安装 CLI；同一行命令加上 `--owner`，会在一台由 systemd 管理的 Linux 机器上同时安装 koko 服务器。没有 systemd 的环境中，可使用镜像 `ghcr.io/cieons/koko` 在容器中运行 koko 服务器（见[在容器中运行](#在容器中运行)）。本文介绍安装、校验发布版本、升级与回退、保管根密钥、恢复到另一台机器，以及根密钥丢失时的处理，随后介绍容器部署下的相应操作。各步骤的实现原理见[安装器](subsystems/installer.md)和[发布](subsystems/release.md)参考文档（英文）。

## 机器要求

- amd64 或 arm64 架构的 Linux，由 systemd 247 或更高版本管理：Debian 11、Ubuntu 22.04、RHEL 9 或更高版本。对于不由 systemd 管理的容器，`koko server install` 会在下载任何文件之前拒绝安装，此时请改用[镜像](#在容器中运行)。
- `/var/lib/koko` 位于本地文件系统，而非 NFS 或其他网络文件系统。
- 一个空闲的 TCP 端口，默认为 7822，可通过 `--port` 另行指定。
- `curl`、`sha256sum`、`awk` 和 `mktemp`，以及 root 权限（以 root 用户运行，或通过 `sudo`）。

## 安装

在这台机器上以 root 运行以下命令，指定所有者以及设备访问 koko 服务器所用的地址：

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sh -s -- --owner alice --address koko.example.com
```

非 root 用户请将管道后的 `sh` 改为 `sudo sh`。

- **`--address`**：建议使用 DNS 名称。它是每个加入码携带的服务器地址；机器的 IP 变更后，设备仍可通过它访问 koko 服务器。未指定 `--address` 时，安装程序使用机器的公网 IP，并拒绝私有 IP（10.x、192.168.x、100.64.x 等），因为设备无法访问这类地址。如果机器位于 NAT 之后且映射到其他端口，请在地址中写明该端口，例如 `--address koko.example.com:8443`。
- **`--port`**：koko 服务器监听的端口，默认为 7822。安装程序不会自行改用其他端口：端口被占用时，会指出占用该端口的进程并停止。仅允许网页流量（80 和 443 端口）出站的网络无法访问 7822 端口：如果设备位于此类网络之后，且这台机器上没有其他程序占用 443 端口，请指定 `--port 443`。
- **`KOKO_VERSION`**：指定要安装的发布版本，而非最新版本，例如 `curl … | sudo env KOKO_VERSION=v0.3.0 sh -s -- …`。单独使用 `sudo` 不会传递该变量，因此需要 `env`。
- **以 `noexec` 方式挂载的 `/tmp`**：install.sh 在临时目录中运行 `koko`，`koko` 也在临时目录中运行 `kokod`，因此需要将 `TMPDIR` 指向允许执行程序的目录：以 root 运行时使用 `curl … | TMPDIR=/var/tmp sh -s -- …`，否则使用 `curl … | sudo env TMPDIR=/var/tmp sh -s -- …`，因为 `sudo` 同样不会传递 `TMPDIR`。

install.sh 下载适用于本机的 `koko`，使用发布版本的 `SHA256SUMS` 校验，仅在校验一致时以给定参数运行 `koko server install`。`koko server install` 随即下载同一版本的 `kokod`（即 `kokod-linux-<arch>`），使用同一份 `SHA256SUMS` 校验，仅在校验一致时运行它。`kokod` 将自身安装为 `/usr/local/bin/kokod`，写入 systemd unit `koko.service` 并启动 koko 服务器，随后由 `koko` 完成初始化。以上步骤全部成功后，install.sh 才将 `koko` 安装为 `/usr/local/bin/koko`。安装完成后输出以下内容：

- 服务器地址和主机密钥的指纹；
- 根密钥的位置及其 id（请[保留副本](#保管根密钥)）；
- 需要开放的端口：**请在云服务商的防火墙中开放该 TCP 端口**，koko 不会自行开放任何端口；
- 第一个加入码（仅可使用一次，15 分钟内有效），以及配合它使用的 `koko join` 命令。

在所有者的计算机上安装 CLI（见[下文](#仅安装-cli)），然后运行输出中的 `koko join koko_…` 命令。如果加入码遗失或已过期，在这台机器上运行 `koko device add` 即可获得新的加入码。

### 仅安装 CLI

不带参数运行同一行命令，即只安装 CLI，适用于 Linux（amd64 或 arm64）和 macOS（Apple Silicon）：

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sh
```

该命令下载适用于本机的 `koko`，使用发布版本的 `SHA256SUMS` 校验后安装为 `/usr/local/bin/koko`。写入该位置需要 root 权限：以 root 用户运行，或由 `sudo` 免密码授权的用户运行；否则，请将管道后的 `sh` 改为 `sudo sh`。该命令不依赖 systemd，因此也可在容器中使用。运行时无需交互输入，并且只向 stderr 输出。

不使用 install.sh 时，请下载适用于本机的文件并校验、安装：Mac 使用 `koko-darwin-arm64`，Linux 使用 `koko-linux-amd64` 或 `koko-linux-arm64`。Mac 的操作如下：

```sh
base=https://github.com/cieons/koko/releases/latest/download
curl -fsSL -O "$base/koko-darwin-arm64" -O "$base/SHA256SUMS"
grep ' koko-darwin-arm64$' SHA256SUMS | shasum -a 256 -c
sudo install -m 0755 koko-darwin-arm64 /usr/local/bin/koko
```

Linux（amd64）的操作如下：

```sh
base=https://github.com/cieons/koko/releases/latest/download
curl -fsSL -O "$base/koko-linux-amd64" -O "$base/SHA256SUMS"
grep ' koko-linux-amd64$' SHA256SUMS | sha256sum -c
sudo install -m 0755 koko-linux-amd64 /usr/local/bin/koko
```

### 离线安装

在无法访问 GitHub 的机器上，`koko server install` 无法下载 `kokod`，会给出相应提示并指出 `--kokod`，且不写入任何内容。此时需使用复制到该机器上的文件安装 koko 服务器。先在可访问 GitHub 的机器上，从同一个发布版本下载与目标机器架构相符的 `koko-linux-<arch>`、`kokod-linux-<arch>` 和 `SHA256SUMS`，以下以 amd64 为例：

```sh
base=https://github.com/cieons/koko/releases/latest/download
curl -fsSL -O "$base/koko-linux-amd64" -O "$base/kokod-linux-amd64" -O "$base/SHA256SUMS"
```

将这三个文件复制到目标机器，在文件所在目录中校验，然后依次安装 koko 服务器和 CLI：

```sh
grep -E ' kokod?-linux-amd64$' SHA256SUMS | sha256sum -c
chmod 0755 koko-linux-amd64 kokod-linux-amd64
sudo ./koko-linux-amd64 server install --kokod ./kokod-linux-amd64 --owner alice --address koko.example.com
sudo install -m 0755 koko-linux-amd64 /usr/local/bin/koko
```

`koko server install --kokod` 先在不运行该文件的情况下读取其构建信息，仅当它与当前运行的 `koko` 属于同一构建时才运行：来自其他发布版本的 `kokod` 会被拒绝，提示中会列出双方的版本，且不运行任何程序。该文件在原位置运行，因此需先用上面的 `chmod` 赋予执行权限。其输出与安装命令相同。

## 校验发布版本

发布版本中的每个文件都附有构建它的 GitHub workflow 所生成的证明（attestation）。使用 [GitHub CLI](https://cli.github.com) 可检查下载的文件是否由 koko 的发布 workflow 构建：

```sh
gh attestation verify kokod-linux-amd64 --repo cieons/koko
```

`SHA256SUMS` 列出的所有文件均可按此检查，包括 `koko-<os>-<arch>`、`kokod-linux-<arch>` 和 `install.sh`。koko 服务器所在的机器无需安装 `gh`：install.sh 和 `koko server install` 仅校验 SHA-256。

## 升级

不带参数再次运行安装命令：

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sudo sh
```

在 koko 服务器所在的机器上，该命令先升级 koko 服务器，再升级 CLI。`koko server install` 将 `kokod` 替换为新发布版本中的文件，重新写入 systemd unit 并重启 koko 服务器；服务器的记录、所有者、服务器地址和密钥均保持不变。所有已打开的 SSH 会话都会结束，并显示一行提示，说明 koko 服务器正在重启。此后 install.sh 才替换 `koko`。如果 koko 服务器升级失败，install.sh 会就此停止，CLI 保持不变，因此 CLI 的版本不会领先于同一机器上的 koko 服务器。在其他机器上，同一行命令只升级 CLI。如果两者仍出现不一致（例如手动安装 CLI 之后），在 koko 服务器所在的机器上运行 `koko status`，会提示该机器上的 koko 服务器属于另一构建，并给出 `sudo koko server install`，运行该命令即可使 koko 服务器与 CLI 的构建一致。

由 v0.1.0 或 v0.2.0 安装的 koko 服务器（当时 `koko` 和 `kokod` 尚未拆分为两个程序）同样按此方式升级：升级后其 unit 运行 `/usr/local/bin/kokod run`，`/usr/local/bin/koko` 则为 CLI。

升级时不接受 `--owner` 和 `--address`：如需更改服务器地址，请使用 `koko server set address=<host[:port]>`。

升级会保留 koko 服务器的监听端口，包括通过 drop-in（`systemctl edit koko`）设置的端口；通过 drop-in 将端口改为 1024 以下后，请再次运行安装命令，使 koko 服务器能够绑定该端口。使用 `--port` 可更换端口。服务器地址未写明端口时，设备使用监听端口，因此更换端口后，输出中会给出需要在每台设备上运行的 `koko connect <host:port>` 命令。

新版本的 koko 更改数据库 schema 时，koko 服务器会先将原数据库复制为 `/var/lib/koko/koko.db.before-<n>`，即快照，并且只保留最新的一份。

## 回退

如需回退到升级前运行的发布版本，请使用最新的 install.sh 安装该版本，并在 `KOKO_VERSION` 中指定。在这台机器上以 root 运行：

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | env KOKO_VERSION=v0.3.0 sh
```

将 `v0.3.0` 替换为要回退到的版本。也可以使用该版本自带的 install.sh（位于 `releases/download/v0.3.0/install.sh`），并指定相同的 `KOKO_VERSION`。如果升级未更改 schema，回退即已完成。如果更改了 schema，旧版本的 koko 会拒绝在较新的数据库上安装，不写入任何内容，较新的 koko 服务器继续运行，install.sh 也不会改动 CLI。拒绝信息中会指出需要放回原位的快照：

```text
koko: /var/lib/koko/koko.db: it is at schema version 14, which this koko (schema version 13) predates: run the newer koko that migrated it, or go back by putting koko.db.before-14 in place of koko.db; nothing was written
```

将快照放回原位，然后再次安装旧版本：

```sh
systemctl stop koko
cd /var/lib/koko
mkdir /var/lib/koko-newer
mv koko.db /var/lib/koko-newer/
mv koko.db-wal koko.db-shm /var/lib/koko-newer/ 2>/dev/null # there only after a crash
mv koko.db.before-<n> koko.db
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | env KOKO_VERSION=v0.3.0 sh
```

其中，将 `koko.db.before-<n>` 替换为拒绝信息中指出的快照，将 `v0.3.0` 替换为上文中的同一版本。升级后写入的数据会随较新的数据库一同丢失，该数据库保留在 `/var/lib/koko-newer` 中。

回退到 v0.2.0 或 v0.1.0（当时 `koko` 和 `kokod` 尚未拆分为两个程序）时，无论是否需要恢复快照，都需要多执行一行命令。旧版本的 koko 会安装其自身的二进制文件和 unit，而 `/usr/local/bin/kokod` 会被保留且不再使用。旧版本的 koko 服务器开始运行后，删除 `/usr/local/bin/kokod`：

```sh
rm /usr/local/bin/kokod
```

## 保管根密钥

`/etc/koko/root.key` 用于密封所有机密以及 koko 服务器自身的密钥。没有它，任何已密封的内容都无法再打开，因此**请在这台机器之外保留一份副本**，例如存放在密码管理器中。该文件为一行文本，其中包含它自己的 id `rootkey_…`，`koko server show` 也会显示该 id：

```sh
sudo cat /etc/koko/root.key
```

不含根密钥的 `/var/lib/koko` 副本不会泄露任何机密或私钥，因此可以存放在任何位置；[恢复](#恢复到另一台机器)时两者缺一不可。

## 恢复到另一台机器

koko 目前没有备份命令。在 koko 服务器停止时复制的数据目录，配合根密钥，即可用于恢复：

1. 在旧机器上：运行 `systemctl stop koko`，将 `/var/lib/koko` 作为整个目录复制出来，并与根密钥的副本一同保存：恢复时两者缺一不可。
2. 在新机器上以 root 将两者放回：目录放到 `/var/lib/koko`，根密钥放到 `/etc/koko/root.key`，属主为 root，权限为 0600，所在目录 `/etc/koko` 的权限为 0700。
3. 使用不带参数的安装命令安装 CLI（见[仅安装 CLI](#仅安装-cli)），然后运行不带 `--owner` 的 `sudo koko server install`；如果旧的 koko 服务器监听的不是 7822 端口，请加上 `--port <port>`。该命令会找到 koko 服务器的记录，安装与自身同一版本的 `kokod` 并启动 koko 服务器。

主机密钥随数据目录一同迁移，因此设备仍然信任 koko 服务器。如果地址发生变化，请使用 `koko server set address=<host[:port]>` 设置新地址，并在每台设备上运行 `koko connect <host:port>`。

当数据目录本身属于其他用户时（例如由 root 放回的副本），systemd 会将该目录及其中的所有内容交给 `koko` 用户。如果文件被复制到一个已属于 `koko` 的 `/var/lib/koko` 中，请在安装前运行 `chown -R koko:koko /var/lib/koko`。

## 根密钥丢失时

如果根密钥及其所有副本均已丢失，记录将无法再打开。唯一的办法是重新开始：将 `/var/lib/koko` 连同无法加载的 `/etc/koko/root.key` 移到别处，然后带 `--owner` 重新安装。代价是所有已密封的内容和所有已加入的设备：每台主机都需要重新添加并接入，每个机密都需要重新添加，每台设备都需要运行 `koko leave`，再使用新的加入码加入。

## 卸载

`koko server uninstall` 停止 koko 服务器，并删除其 unit 和 `/usr/local/bin/kokod`。它保留 CLI `/usr/local/bin/koko`、数据目录、根密钥和 `koko` 用户，并输出 koko 服务器原先监听的端口，以及原先设有的 Web 监听地址。如需在同一份数据上再次运行 koko 服务器，请运行不带 `--owner`、带上该 `--port`（若输出了 Web 监听地址，一并带上该 `--web-listen`）的 `sudo koko server install`：它会再次安装与自身同一版本的 `kokod`。

## 在容器中运行

镜像 `ghcr.io/cieons/koko` 用于在没有 systemd 的环境中运行 koko 服务器，例如在 Docker 中与其他容器一同运行。每个发布版本都会推送 amd64 和 arm64 的镜像，其中包含该版本的两个程序：`kokod-linux-<arch>` 位于 `/usr/local/bin/kokod`，`koko-linux-<arch>` 位于 `/usr/local/bin/koko`。镜像以 `kokod run` 运行 koko 服务器，因此未设置 `entrypoint` 和 `command` 的 Compose 文件（例如下文中的文件）在更换发布版本时只需修改 `KOKO_VERSION`。容器在启动时即以 uid 65532 运行，从不以 root 运行，不持有任何 capability，koko 也不会更改任何文件的属主。相关设计原因见 [ADR 0037](adr/0037-the-container-keeps-its-data-and-its-root-key-in-one-directory.md)、[ADR 0038](adr/0038-a-container-initialises-at-its-first-start-and-gives-its-first-join-code-to-docker-exec.md) 和 [ADR 0039](adr/0039-the-container-runs-as-uid-65532-from-the-start-and-koko-changes-no-owner.md)（英文）。

### Docker 宿主机要求

- 带有 Compose v2（`docker compose`）的 Docker，amd64 或 arm64 架构。
- 一个 koko 专用、当前用户可写入的目录，下文为 `/srv/koko`（`sudo install -d -o "$USER" /srv/koko`），位于本地文件系统，而非 NFS 或其他网络文件系统：koko 服务器会在启动时拒绝网络文件系统。整个部署都在该目录中：Compose 文件、它的 `.env`，以及保存容器全部数据的 `state/`。
- 一个用于发布的空闲 TCP 端口，下文为 7822。**请在云服务商的防火墙中开放该端口**，koko 不会自行开放任何端口。

Docker 宿主机上无需准备用户或密钥，只需在首次启动前准备一次 `state/`：属主为 uid 65532，权限为 0700，使 Docker 宿主机上只有 root 能够读取。在 `/srv/koko` 中运行：

```sh
sudo install -d -o 65532 -g 65532 -m 0700 state
```

如果省略这一步，Docker 会以 root 身份创建权限为 0755 的 `state/`，koko 服务器将拒绝启动，不写入任何内容，并指出该目录以及自身运行所用的 uid 和 gid 65532。如需完全免去准备工作，可改用命名卷：以 `koko:/koko` 代替 `./state:/koko`，并在文件末尾加上 `volumes: {koko: {}}`。Docker 会用镜像中的 `/koko` 填充新建的卷，其属主已是 uid 65532，并将卷保存在 Docker 自己的目录下（见 `docker volume inspect`）。下文的命令均在 `/srv/koko` 中运行，并以 `state/` 为例。

### 启动

将 [`release/compose.yaml`](../release/compose.yaml) 保存为 `/srv/koko` 中的 `compose.yaml`：

```yaml
# The koko server in a container: see docs/install.md, "In a container".
# Keep this file in a folder of its own, such as /srv/koko, and there,
# before the first start:
#   echo KOKO_VERSION=0.2.0 > .env    # the release to run, without its v
#   sudo install -d -o 65532 -g 65532 -m 0700 state    # what koko keeps
# After it: docker exec koko koko device add    # your first Join Code
services:
  koko:
    image: ghcr.io/cieons/koko:${KOKO_VERSION:?set KOKO_VERSION in .env, such as KOKO_VERSION=0.2.0}
    container_name: koko
    restart: unless-stopped
    stop_grace_period: 30s
    # uid 65532 from the start, with no capability and none to gain.
    cap_drop: [ALL]
    security_opt: [no-new-privileges:true]
    ports:
      - "7822:7822"        # with another host port, name it in KOKO_ADDRESS
    volumes:
      - ./state:/koko      # the data and the Root Key, beside this file: make it with install -d (above), or use a named volume
    environment:
      KOKO_OWNER: alice                  # read only at the first start
      KOKO_ADDRESS: koko.example.com     # host[:published port]; read only at the first start
```

将 `KOKO_OWNER` 设为所有者的用户名，将 `KOKO_ADDRESS` 设为设备访问 koko 服务器所用的地址：与 [`--address`](#安装) 相同，建议使用 DNS 名称。然后在该文件所在目录中运行：

```sh
echo KOKO_VERSION=0.2.0 > .env     # the release to run, without its v
sudo install -d -o 65532 -g 65532 -m 0700 state   # once, unless a named volume is used
docker compose up -d
docker exec koko koko device add   # your first Join Code
```

`KOKO_VERSION` 为必填项，确保容器运行的始终是明确选定的发布版本。首次启动时，koko 服务器会生成根密钥 `state/root.key` 并完成初始化。此后 `docker logs koko` 会显示 `koko server initialised`，以及主机密钥的指纹、监听地址、根密钥的路径和 id，还有下一步要运行的 `koko device add` 命令。日志中不会出现根密钥或加入码。未设置 `KOKO_ADDRESS` 的全新 koko 服务器会拒绝启动并指出该设置：容器自身的 IP 是私有地址，设备无法访问。

`koko device add` 会输出第一个加入码（仅可使用一次，15 分钟内有效），以及在所有者的计算机上配合它使用的 `koko join` 命令（见[仅安装 CLI](#仅安装-cli)）。如果加入码遗失或已过期，请再次运行同一命令；所有设备均已丢失时，也按此操作。

不使用 Compose 时，可在 `/srv/koko` 中使用 `docker run` 运行同样的容器：

```sh
docker run -d --name koko --restart unless-stopped --stop-timeout 30 \
  --cap-drop ALL --security-opt no-new-privileges:true \
  -p 7822:7822 -v "$PWD/state:/koko" \
  -e KOKO_OWNER=alice -e KOKO_ADDRESS=koko.example.com \
  ghcr.io/cieons/koko:0.2.0
```

### 所有者命令

`docker exec koko koko <command>` 以镜像的 uid 65532 在容器内运行 koko CLI。CLI 通过 koko 服务器的 Unix socket 以本地调用方（Local caller）的身份访问服务器，本地调用方以所有者身份操作。因此，所有者无需设备即可操作：

```sh
docker exec koko koko server show
docker exec koko koko server set address=<host[:port]>
docker exec koko koko device add
docker exec koko koko user invite bob
```

Docker 宿主机自身的 shell 不是本地调用方，因为 socket 位于容器内部：在 Docker 宿主机上请使用 `docker exec`，或将宿主机作为设备加入。不支持将容器的 `/run/koko` 挂载到 Docker 宿主机上。

### 端口

容器监听 7822 端口，因此任何其他端口（包括 443）都只是 Docker 宿主机上的端口映射。发布端口为 7822 时，`KOKO_ADDRESS: koko.example.com` 即可。发布端口不同时，需要在地址中写明发布端口，因为设备连接的是该端口：

```yaml
    ports:
      - "443:7822"
    environment:
      KOKO_ADDRESS: koko.example.com:443
```

`KOKO_OWNER` 和 `KOKO_ADDRESS` 仅在首次启动时读取。此后启动时，如果它们的值与记录不同，koko 服务器会记录一条警告，不作任何更改。以后如需更换发布端口，请修改端口映射和 `KOKO_ADDRESS`，运行 `docker compose up -d`，然后运行 `docker exec koko koko server set address=koko.example.com:<port>`，并在每台设备上运行 `koko connect koko.example.com:<port>`。

使用 `network_mode: host` 时，容器的端口即机器的端口：请省略 `ports:`；如需监听 7822 以外的端口，请设置 `KOKO_LISTEN`（例如 `:8443`）。该端口必须不小于 1024，因为 koko 服务器不持有任何 capability。`KOKO_ADDRESS` 未写明端口时，设备连接 koko 服务器的监听端口。

### 备份与根密钥

`state/` 保存容器的全部数据：根密钥 `state/root.key`（一行包含其自身 id 的文本），以及数据目录 `state/data/`，其中包含 `koko.db` 和最新的快照 `koko.db.before-<n>`。

根密钥用于密封所有机密以及 koko 服务器自身的密钥。单独的 `state/data/` 副本不会泄露任何机密或私钥。**而整个 `state/` 的副本同时包含记录及其密钥**；此外，koko 进程能够读取、修改或删除 `root.key`，而在通过 systemd 安装的 koko 服务器上，systemd 会阻止这些操作。因此：

1. 首次启动后，立即将 `root.key` 复制到这台机器之外，例如存放在密码管理器中。该文件只有一行：`sudo cat state/root.key`。`docker exec koko koko server show` 会显示其 id。
2. 单独备份 `state/data/`：在容器停止时以 root 复制（先 `docker compose stop`，复制后 `docker compose start`）。该副本可以存放在任何位置。
3. 整个 `state/` 的副本，只应存放在适合存放这些机密本身的位置。
4. 机器的磁盘快照同时包含两者，但云服务商本就控制着这台机器。

koko 服务器从不在已密封的数据上生成新的根密钥：缺失或不匹配的 `root.key` 会在启动时被拒绝，并指出数据所需的 id。如果根密钥的所有副本均已丢失，处理方式与[根密钥丢失时](#根密钥丢失时)相同：将 `state/data/` 移到别处，重新开始。

### 升级容器

修改 `.env` 中的 `KOKO_VERSION`，然后再次运行 `docker compose up -d`。Compose 会拉取新镜像并替换容器，容器的记录、所有者、服务器地址和密钥均保留在 `state/` 中。所有已打开的 SSH 会话都会结束，并显示一行提示，说明 koko 服务器正在重启。新版本的 koko 更改数据库 schema 时，koko 服务器会先将原数据库复制为 `state/data/koko.db.before-<n>`，并且只保留最新的一份快照。

使用 `docker run` 时，先运行 `docker stop koko` 和 `docker rm koko`（两者都会保留 `state/`），再使用新版本运行 `docker run` 命令。

### 在容器中回退

如需回退到升级前运行的发布版本，请在 `.env` 中设置该版本，然后运行 `docker compose up -d`。如果升级未更改 schema，回退即已完成。如果更改了 schema，旧版本的镜像会在启动时拒绝运行，不写入任何内容，`docker logs koko` 会指出所需的版本以及需要放回原位的快照。在 Docker 宿主机上将快照放回原位：在 root shell（`sudo -s`）中，于 `compose.yaml` 所在目录运行：

```sh
docker compose down
mkdir newer
mv state/data/koko.db newer/
mv state/data/koko.db-wal state/data/koko.db-shm newer/ 2>/dev/null # there only after a crash
mv state/data/koko.db.before-<n> state/data/koko.db
docker compose up -d
```

其中，将 `koko.db.before-<n>` 替换为拒绝信息中指出的快照。升级后写入的数据会随较新的数据库一同丢失，该数据库保留在 `newer/` 中。

### 恢复或迁移容器

1. 在旧机器上：运行 `docker compose down`，然后以 root 将 `/srv/koko` 作为整个目录复制出来，使 Compose 文件和 `.env` 与 `state/` 一同迁移。也可以只复制 `state/data/`，并与 `root.key` 的副本一同保存：恢复时两者缺一不可。
2. 在新机器上将它们放回 `/srv/koko`：整个目录，或 `compose.yaml`、`.env`、`state/data/` 和 `state/root.key`。由于 koko 不会更改属主，请在 `/srv/koko` 中以 root 将 `state/` 交给 uid 65532：`chown -R 65532:65532 state` 和 `chmod 0700 state`。
3. 在该目录中使用同样的 `compose.yaml` 和 `.env` 运行 `docker compose up -d`。

主机密钥以密封形式随 `state/data/` 一同迁移，因此设备仍然信任 koko 服务器。如果地址发生变化，请运行 `docker exec koko koko server set address=<host[:port]>`，并在每台设备上运行 `koko connect <host:port>`。

### 从 systemd 迁移

`koko server uninstall` 会停止已安装的 koko 服务器，并保留其数据目录和根密钥。在 `/srv/koko` 中以 root 将它们放入容器的 `state/`（其中尚无 `data/`）：

```sh
koko server uninstall
install -d -o 65532 -g 65532 -m 0700 state
cp -a /var/lib/koko state/data
cp /etc/koko/root.key state/root.key
chown -R 65532:65532 state
```

然后按上文启动 Compose 文件，无需运行 `docker exec … device add`：已加入的设备仍可继续使用。请将 `KOKO_OWNER` 和 `KOKO_ADDRESS` 设为原有的所有者和服务器地址，否则每次启动都会记录一条警告，说明两者不一致。如果原先安装的 koko 服务器监听的不是 7822 端口，请发布该端口（443 端口对应 `"443:7822"`）；如果其服务器地址未写明端口，还需在 `KOKO_ADDRESS` 中写明端口，并运行 `docker exec koko koko server set address=koko.example.com:443`。

反向迁移时，将 `state/data/` 放到 `/var/lib/koko`，将 `state/root.key` 放到 `/etc/koko/root.key`，然后从第 2 步开始按照[恢复到另一台机器](#恢复到另一台机器)操作；如果发布端口不是 7822，请将 `--port` 设为该端口。

### 校验镜像

每个镜像都附有 GitHub 的制品证明（artifact attestation），可使用 [GitHub CLI](https://cli.github.com) 检查：

```sh
gh attestation verify oci://ghcr.io/cieons/koko:0.2.0 --repo cieons/koko
```

镜像中的 `/usr/local/bin/kokod` 和 `/usr/local/bin/koko` 与发布版本的 `kokod-linux-<arch>` 和 `koko-linux-<arch>` 逐字节相同，即 `SHA256SUMS` 所列的文件。

### Kubernetes 与其他用户

镜像以数字 uid 65532 运行，因此 Kubernetes 的 `runAsNonRoot` 可直接接受；请确保其卷可由 uid 65532 写入，例如设置 `fsGroup: 65532`。koko 服务器无法写入的目录会在启动时被拒绝，并指出该目录以及自身运行所用的 uid 和 gid；koko 服务器也拒绝以 root 运行，因此请勿传入 `--user 0` 或 `user: root`。使用其他 uid 时，`/koko` 和 `/run/koko` 须可由该 uid 写入，例如在 `/run/koko` 上挂载一个属于该 uid 的 tmpfs。

### 将根密钥分开存放

如需将根密钥存放在备份目录之外，可从其他位置以只读方式挂载根密钥，并将 `KOKO_ROOT_KEY_FILE` 指向它。首次启动后，运行 `docker compose down`，然后以 root 将 `state/root.key` 移到一个单独的目录，例如 `/etc/koko-key/root.key`（先运行 `mkdir /etc/koko-key`），属主仍为 uid 65532，并执行 `chmod 0400`。然后在 `volumes:` 中加入 `/etc/koko-key/root.key:/run/secrets/koko-root.key:ro`，在 `environment:` 中加入 `KOKO_ROOT_KEY_FILE: /run/secrets/koko-root.key`，再运行 `docker compose up -d`。koko 服务器只读取该文件；无法读取时，会在启动时拒绝运行。
