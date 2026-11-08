# 安装和运行 koko 服务器

[English](install.md)

koko 服务器运行在一台由 systemd 管理的 Linux 机器上，或者用它的镜像 `ghcr.io/cieons/koko` 运行在容器中（[在容器中运行](#在容器中运行)）。本页介绍如何安装、校验发布版本、升级和回退、保管根密钥、恢复到另一台机器，以及没有根密钥时怎么办，然后是容器中的同样内容。每一步的原理见[安装器](subsystems/installer.md)和[发布](subsystems/release.md)的参考文档（英文）。

## 机器的要求

- amd64 或 arm64 的 Linux，由 systemd 247 或更高版本运行：Debian 11、Ubuntu 22.04、RHEL 9 或更高版本。不由 systemd 运行的容器会被拒绝：请改在那里运行[镜像](#在容器中运行)。
- `/var/lib/koko` 位于本地文件系统上，不能是 NFS 或其他网络文件系统。
- 一个空闲的 TCP 端口，默认 7822，可用 `--port` 另选。
- `curl`、`sha256sum`、`awk` 和 `mktemp`，以及 root 权限：以 root 用户运行，或通过 `sudo`。

## 安装

在这台机器上以 root 运行下面这行，指定你自己为所有者，以及你的设备访问它所用的地址：

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sh -s -- --owner alice --address koko.example.com
```

不是 root？改为通过管道交给 `sudo sh`，而不是 `sh`。

- **`--address`**：最好用 DNS 名称。它是每个加入码携带的服务器地址，机器的 IP 变了，设备仍然通过它访问 koko 服务器。不带 `--address` 时，安装程序会取机器的公网 IP，并拒绝私有 IP（10.x、192.168.x、100.64.x 之类），因为你的设备无法访问它。如果机器在 NAT 后面且映射到另一个端口，写上那个端口：`--address koko.example.com:8443`。
- **`--port`**：koko 服务器监听的端口，默认 7822。安装程序从不自行换用其他端口：端口被占用时，它会指出占用端口的进程并停止。只允许网页流量（80 和 443 端口）出站的网络无法访问 7822：如果你的设备在这样的网络后面，而这台机器上没有别的程序占用 443，请选择 `--port 443`。
- **`KOKO_VERSION`** 指定某个发布版本，而不是最新版：`curl … | sudo env KOKO_VERSION=v0.3.0 sh -s -- …`。单纯的 `sudo` 会丢掉这个变量，所以要用 `env`。
- **以 `noexec` 挂载的 `/tmp`**：install.sh 在临时目录里运行二进制文件，所以要把 `TMPDIR` 指向一个允许运行程序的目录：`curl … | TMPDIR=/var/tmp sh -s -- …`，或者通过 `sudo`：`curl … | sudo env TMPDIR=/var/tmp sh -s -- …`。

install.sh 下载适合这台机器的二进制文件，用发布版本的 `SHA256SUMS` 校验，只有校验一致才运行它。它会打印：

- 服务器地址和主机密钥的指纹；
- 根密钥所在的位置和它的 id（[留一份副本](#保管根密钥)）；
- 需要开放的端口：**在你的厂商的防火墙中开放这个 TCP 端口**，koko 自己不会开放任何端口；
- 你的第一个加入码，只能使用一次，15 分钟内有效，以及配合它运行的 `koko join` 命令。

在你自己的电脑上，把 koko CLI 放进 `PATH`（见下文），然后运行打印出的 `koko join koko_…` 命令。加入码丢了或过期了？在这台机器上运行 `koko device add` 会打印一个新的。

### 你电脑上的 CLI

同一个发布版本里也有 CLI：Apple Silicon Mac 用 `koko-darwin-arm64`，Linux 用 `koko-linux-amd64` 或 `koko-linux-arm64`。以 Mac 为例：

```sh
base=https://github.com/cieons/koko/releases/latest/download
curl -fsSL -O "$base/koko-darwin-arm64" -O "$base/SHA256SUMS"
grep ' koko-darwin-arm64$' SHA256SUMS | shasum -a 256 -c
sudo install -m 0755 koko-darwin-arm64 /usr/local/bin/koko
```

## 校验发布版本

发布版本的每个文件都由构建它的 GitHub workflow 做了证明（attestation）。用 [GitHub CLI](https://cli.github.com) 可以检查下载的文件是否由 koko 的发布 workflow 构建：

```sh
gh attestation verify koko-linux-amd64 --repo cieons/koko
```

`SHA256SUMS` 列出的每个文件都可以这样检查，`install.sh` 也不例外。koko 服务器所在的机器不需要 `gh`：install.sh 只校验 SHA-256。

## 升级

再运行一次安装命令，不带 `--owner`，也不带 `--address`：

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sudo sh -s --
```

它会替换二进制文件、重新写入 systemd unit 并重启 koko 服务器，服务器的记录、所有者、服务器地址和密钥都会保留。每个打开的 SSH 会话都会结束，并显示一行提示，说明 koko 服务器正在重启。升级时 `--owner` 和 `--address` 会被拒绝：要更改服务器地址，请改用 `koko server set address=<host[:port]>`。

升级会保留 koko 服务器监听的端口，即使它是用 drop-in（`systemctl edit koko`）设置的；用 drop-in 把它改到 1024 以下后，请再运行一次安装命令，让 koko 服务器能够绑定它。`--port` 会更换端口。当服务器地址没有写明自己的端口时，设备会跟随监听端口，所以换端口后，输出会给出需要在每台设备上运行的 `koko connect <host:port>` 命令。

当新版 koko 更改了数据库的 schema 时，koko 服务器会先把原来的数据库复制到 `/var/lib/koko/koko.db.before-<n>`，即快照，并且只保留最新的一份。

## 回退

要回到升级前的 koko，安装它的发布版本。在这台机器上以 root 运行：

```sh
curl -fsSL https://github.com/cieons/koko/releases/download/v0.3.0/install.sh | env KOKO_VERSION=v0.3.0 sh -s --
```

把 `v0.3.0` 换成你升级前的版本。如果升级没有更改 schema，这样就完成了。如果更改了，较旧的 koko 会拒绝安装到较新的数据库上，不写入任何东西，较新的 koko 服务器仍在运行。它的拒绝信息会指出应放回原位的快照：

```text
koko: /var/lib/koko/koko.db: it is at schema version 14, which this koko (schema version 13) predates: run the newer koko that migrated it, or go back by putting koko.db.before-14 in place of koko.db; nothing was written
```

把它放回原位，然后再安装一次旧的发布版本：

```sh
systemctl stop koko
cd /var/lib/koko
mkdir /var/lib/koko-newer
mv koko.db /var/lib/koko-newer/
mv koko.db-wal koko.db-shm /var/lib/koko-newer/ 2>/dev/null # there only after a crash
mv koko.db.before-<n> koko.db
curl -fsSL https://github.com/cieons/koko/releases/download/v0.3.0/install.sh | env KOKO_VERSION=v0.3.0 sh -s --
```

把 `koko.db.before-<n>` 换成拒绝信息指出的快照，把 `v0.3.0` 换成上面的同一个版本。升级之后写入的内容会随较新的数据库一起丢失，那个数据库留在 `/var/lib/koko-newer` 中。

## 保管根密钥

`/etc/koko/root.key` 封存了每个机密以及 koko 服务器自己的密钥。没有它，封存的东西都无法再打开，所以**在这台机器之外留一份副本**，比如放在密码管理器里。这个文件是一行文本，写明了它自己的 id `rootkey_…`，`koko server show` 也会打印这个 id：

```sh
sudo cat /etc/koko/root.key
```

没有根密钥的 `/var/lib/koko` 副本不会泄露任何机密或私钥，所以放在哪里都安全；[恢复](#恢复到另一台机器)需要两者都在。

## 恢复到另一台机器

koko 还没有备份命令。在 koko 服务器停止时复制的数据目录，加上根密钥，就能恢复它：

1. 在旧机器上：`systemctl stop koko`，把 `/var/lib/koko` 作为整个目录复制出来，和你的根密钥副本放在一起：恢复需要两者都在。
2. 在新机器上以 root 把两者都放回去：目录放到 `/var/lib/koko`，根密钥放到 `/etc/koko/root.key`，属主为 root，权限 0600，所在的 `/etc/koko` 权限 0700。
3. 运行不带 `--owner` 的安装命令；如果旧的 koko 服务器监听的不是 7822 端口，加上 `--port <port>`。它会找到 koko 服务器的记录并启动它。

主机密钥随数据目录一起迁移，所以设备仍然信任 koko 服务器。如果它的地址变了，用 `koko server set address=<host[:port]>` 设置新地址，并在每台设备上运行 `koko connect <host:port>`。

当数据目录本身属于别的用户时（比如由 root 放回的副本），systemd 会把它和其中的一切交给 `koko` 用户。如果你把文件复制进了一个已经属于 `koko` 的 `/var/lib/koko`，请在安装前运行 `chown -R koko:koko /var/lib/koko`。

## 没有根密钥时

如果根密钥及其所有副本都丢失了，记录就再也无法打开。唯一的出路是从头开始：把 `/var/lib/koko` 移到一边，连同任何无法加载的 `/etc/koko/root.key`，然后带 `--owner` 重新安装。代价是所有封存的数据和所有已加入的设备：每台主机都要重新添加并纳管，每个机密都要重新添加，每台设备都要运行 `koko leave`，再用新的加入码加入。

## 卸载

`koko server uninstall` 停止 koko 服务器，并删除它的 unit 和二进制文件。它保留数据目录、根密钥和 `koko` 用户，并打印它监听过的端口：不带 `--owner`、带上那个 `--port` 再安装一次，就能在同样的数据上运行它。

## 在容器中运行

镜像 `ghcr.io/cieons/koko` 让 koko 服务器在没有 systemd 的地方运行，比如在 Docker 里和你的其他容器放在一起。每个发布版本都会推送 amd64 和 arm64 的镜像，里面是该版本自己的 `koko-linux-<arch>` 二进制文件。容器只在把它唯一的目录交给 uid 65532 时以 root 运行，之后以 uid 65532 提供服务，不持有任何 capability。这样设计的原因见 [ADR 0037](adr/0037-the-container-keeps-its-data-and-its-root-key-in-one-directory.md) 和 [ADR 0038](adr/0038-a-container-initialises-at-its-first-start-and-gives-its-first-join-code-to-docker-exec.md)（英文）。

### Docker 宿主机的要求

- 带 Compose v2（`docker compose`）的 Docker，amd64 或 arm64。
- 一个给 koko 用的目录，下文为 `/srv/koko`，位于本地文件系统上，不能是 NFS 或其他网络文件系统：koko 服务器会在启动时拒绝它。
- 一个空闲的 TCP 端口用来发布，下文为 7822。**在你的厂商的防火墙中开放它**，koko 自己不会开放任何端口。

Docker 宿主机上不需要准备别的：不需要用户，不需要密钥，也不需要目录。`/srv/koko` 不存在时 Docker 会创建它，容器会把它交给 uid 65532，权限 0700，所以在 Docker 宿主机上只有 root 能读取它。

### 启动

把 [`release/compose.yaml`](../release/compose.yaml) 保存为单独目录中的 `compose.yaml`：

```yaml
# The koko server in a container: see docs/install.md, "In a container".
# Before the first start, beside this file:
#   echo KOKO_VERSION=0.2.0 > .env    # the release to run, without its v
# After it: docker exec koko koko device add    # your first Join Code
services:
  koko:
    image: ghcr.io/cieons/koko:${KOKO_VERSION:?set KOKO_VERSION in .env, such as KOKO_VERSION=0.2.0}
    container_name: koko
    restart: unless-stopped
    stop_grace_period: 30s
    # Root only long enough to give /koko to uid 65532, then never again.
    cap_drop: [ALL]
    cap_add: [CHOWN, DAC_OVERRIDE, FOWNER, SETGID, SETUID]
    security_opt: [no-new-privileges:true]
    ports:
      - "7822:7822"        # with another host port, name it in KOKO_ADDRESS
    volumes:
      - /srv/koko:/koko    # the data and the Root Key; Docker makes it if missing
    environment:
      KOKO_OWNER: alice                  # read only at the first start
      KOKO_ADDRESS: koko.example.com     # host[:published port]; read only at the first start
```

在 `KOKO_OWNER` 中填你自己作为所有者，在 `KOKO_ADDRESS` 中填你的设备访问 koko 服务器所用的地址：和 [`--address`](#安装) 一样，最好用 DNS 名称。然后在这个文件旁边运行：

```sh
echo KOKO_VERSION=0.2.0 > .env     # the release to run, without its v
docker compose up -d
docker exec koko koko device add   # your first Join Code
```

`KOKO_VERSION` 是必填的，所以容器运行的总是你选定的发布版本。首次启动时，koko 服务器会生成根密钥 `/srv/koko/root.key`，并完成自身的初始化。之后 `docker logs koko` 会显示 `koko server initialised`，以及主机密钥的指纹、监听地址、根密钥的路径和 id，还有接下来要运行的 `koko device add` 命令。日志里不会出现根密钥或加入码。没有 `KOKO_ADDRESS` 的全新 koko 服务器会拒绝启动，并指出这个设置：容器自己的 IP 是私有的，没有设备能访问它。

`koko device add` 会打印你的第一个加入码，只能使用一次，15 分钟内有效，以及在你的电脑上配合它运行的 `koko join` 命令（[你电脑上的 CLI](#你电脑上的-cli)）。加入码丢了或过期了？再运行一次同样的命令，丢失了所有设备之后也是这样。

不用 Compose 时，用 `docker run` 运行同样的容器：

```sh
docker run -d --name koko --restart unless-stopped --stop-timeout 30 \
  --cap-drop ALL --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add FOWNER \
  --cap-add SETGID --cap-add SETUID --security-opt no-new-privileges:true \
  -p 7822:7822 -v /srv/koko:/koko \
  -e KOKO_OWNER=alice -e KOKO_ADDRESS=koko.example.com \
  ghcr.io/cieons/koko:0.2.0
```

### 所有者命令

`docker exec koko koko <command>` 在容器内运行 koko CLI，它通过 koko 服务器的 Unix socket 以本地调用方（Local caller）的身份访问服务器，本地调用方以所有者的身份行事。所以所有者不需要设备就能操作：

```sh
docker exec koko koko server show
docker exec koko koko server set address=<host[:port]>
docker exec koko koko device add
docker exec koko koko user invite bob
```

Docker 宿主机自己的 shell 不是本地调用方，因为 socket 留在容器内：在 Docker 宿主机上请用 `docker exec`，或者把它作为设备加入。不支持把容器的 `/run/koko` 挂载到 Docker 宿主机上。

### 端口

容器监听 7822，所以任何其他端口，包括 443，都只是 Docker 宿主机上的端口映射。发布的端口是 7822 时，`KOKO_ADDRESS: koko.example.com` 就够了。端口不同时，要在地址中写上发布的端口，因为设备连接的是这个端口：

```yaml
    ports:
      - "443:7822"
    environment:
      KOKO_ADDRESS: koko.example.com:443
```

`KOKO_OWNER` 和 `KOKO_ADDRESS` 只在首次启动时读取。之后的启动中，如果它们的值与记录不同，会记录一条警告，什么也不更改。以后要更换发布的端口，修改映射和 `KOKO_ADDRESS`，运行 `docker compose up -d`，然后运行 `docker exec koko koko server set address=koko.example.com:<port>`，并在每台设备上运行 `koko connect koko.example.com:<port>`。

使用 `network_mode: host` 时，容器的端口就是机器的端口：去掉 `ports:`，要监听 7822 以外的端口就设置 `KOKO_LISTEN`（比如 `:8443`）。这个端口必须是 1024 或以上，因为 koko 服务器不持有任何 capability。当 `KOKO_ADDRESS` 没有写明端口时，设备会连接 koko 服务器监听的端口。

### 备份和根密钥

`/srv/koko` 保存容器的全部数据：根密钥 `root.key`，一行写明了它自己 id 的文本；以及数据目录 `data/`，里面有 `koko.db` 和最新的快照 `koko.db.before-<n>`。

根密钥封存了每个机密以及 koko 服务器自己的密钥。单独的 `data/` 副本不会泄露任何机密或私钥。**整个目录的副本会把记录和它的密钥放在一起**，而且 koko 进程可以读取、更改或删除 `root.key`，在 systemd 安装的 koko 服务器上这是做不到的。所以：

1. 首次启动后立即把 `root.key` 复制到这台机器之外，比如放进密码管理器。它只有一行：`sudo cat /srv/koko/root.key`。`docker exec koko koko server show` 会打印它的 id。
2. 单独备份 `data/`，在容器停止时以 root 复制（`docker compose stop`，之后 `docker compose start`）。这份副本放在哪里都可以。
3. 整个目录的副本，只放在你愿意存放这些机密本身的地方。
4. 机器的磁盘快照同时包含两者，但你的厂商本来就控制着这台机器。

koko 服务器从不在已封存的数据上生成根密钥：缺失或不对的 `root.key` 会在启动时被拒绝，并指出数据需要的 id。如果它的所有副本都丢了，出路和[没有根密钥时](#没有根密钥时)一样：把 `data/` 移到一边，从头开始。

### 升级容器

修改 `.env` 中的 `KOKO_VERSION`，然后再运行一次 `docker compose up -d`。Compose 会拉取新镜像并替换容器，容器的记录、所有者、服务器地址和密钥都保留在 `/srv/koko` 中。每个打开的 SSH 会话都会结束，并显示一行提示，说明 koko 服务器正在重启。当新版 koko 更改了数据库的 schema 时，koko 服务器会先把原来的数据库复制到 `/srv/koko/data/koko.db.before-<n>`，并且只保留最新的一份快照。

使用 `docker run` 时，运行 `docker stop koko` 和 `docker rm koko`，它们会保留 `/srv/koko`，然后用新版本运行 `docker run` 命令。

### 在容器中回退

要回到升级前的 koko，在 `.env` 中设置它的版本，然后运行 `docker compose up -d`。如果升级没有更改 schema，这样就完成了。如果更改了，旧镜像会在启动时拒绝，不写入任何东西，`docker logs koko` 会指出它需要的版本以及应放回原位的快照。在 Docker 宿主机上把它放回原位：在 root shell（`sudo -s`）中，在 `compose.yaml` 所在的目录运行：

```sh
docker compose down
mkdir /srv/koko-newer
mv /srv/koko/data/koko.db /srv/koko-newer/
mv /srv/koko/data/koko.db-wal /srv/koko/data/koko.db-shm /srv/koko-newer/ 2>/dev/null # there only after a crash
mv /srv/koko/data/koko.db.before-<n> /srv/koko/data/koko.db
docker compose up -d
```

把 `koko.db.before-<n>` 换成拒绝信息指出的快照。升级之后写入的内容会随较新的数据库一起丢失，那个数据库留在 `/srv/koko-newer` 中。

### 恢复或迁移容器

1. 在旧机器上：`docker compose down`，然后以 root 把 `/srv/koko` 作为整个目录复制出来。也可以只复制 `data/`，和你的 `root.key` 副本放在一起：恢复需要两者都在。
2. 在新机器上把两者都放回 `/srv/koko`，`root.key` 放在 `data/` 旁边，属主是谁都可以：容器启动时会把它们交给 uid 65532。
3. 在那里用同样的 `compose.yaml` 和 `.env` 运行 `docker compose up -d`。

主机密钥封存在 `data/` 中一起迁移，所以设备仍然信任 koko 服务器。如果它的地址变了，运行 `docker exec koko koko server set address=<host[:port]>`，并在每台设备上运行 `koko connect <host:port>`。

### 从 systemd 迁移过来

`koko server uninstall` 会停止已安装的 koko 服务器，并保留它的数据目录和根密钥。以 root 把它们放进容器的目录（其中还没有 `data/`）：

```sh
koko server uninstall
mkdir -p /srv/koko
cp -a /var/lib/koko /srv/koko/data
cp /etc/koko/root.key /srv/koko/root.key
```

然后按上文启动 Compose 文件，不需要 `docker exec … device add`：已加入的设备仍然可用。把 `KOKO_OWNER` 和 `KOKO_ADDRESS` 设为它的所有者和服务器地址，否则每次启动都会记录一条警告，说明它们不同。如果已安装的 koko 服务器监听的不是 7822 端口，就发布那个端口（443 就是 `"443:7822"`），并且当它的服务器地址没有写明端口时，在 `KOKO_ADDRESS` 中写上端口，并运行 `docker exec koko koko server set address=koko.example.com:443`。

反过来迁移时，把 `data/` 放到 `/var/lib/koko`，把 `root.key` 放到 `/etc/koko/root.key`，然后从第 2 步开始按[恢复到另一台机器](#恢复到另一台机器)操作；如果发布的端口不是 7822，就把 `--port` 设为那个端口。

### 校验镜像

每个镜像都带有 GitHub 的证明（attestation），可以用 [GitHub CLI](https://cli.github.com) 检查：

```sh
gh attestation verify oci://ghcr.io/cieons/koko:0.2.0 --repo cieons/koko
```

镜像中的 `/usr/local/bin/koko` 与发布版本的 `koko-linux-<arch>` 逐字节相同，就是 `SHA256SUMS` 列出的那个文件。

### 以 uid 65532 启动

在不允许镜像以 root 启动的地方，比如 Kubernetes 的 `runAsNonRoot`，从一开始就以 uid 65532 运行它，并使用 uid 65532 已经能写入的目录：在 Compose 文件中写 `user: "65532:65532"`，或者 `docker run --user 65532:65532`，或者在 pod 中设置 `runAsUser: 65532`、`runAsGroup: 65532` 和 `fsGroup: 65532`。Docker 宿主机上的目录要先以 root 创建：`install -d -o 65532 -g 65532 -m 0700 /srv/koko`。这时 koko 服务器什么都不准备，也不降权，不需要任何 capability。它无法写入的目录会在启动时被拒绝，并指出这个目录以及它运行所用的 uid 和 gid。其他 uid 只有在 `/koko` 和镜像的 `/run/koko` 都能被它写入时才可用。

### 把根密钥分开存放

要让根密钥不在你备份的目录中，从别处以只读方式挂载它，并让 `KOKO_ROOT_KEY_FILE` 指向它。首次启动后，运行 `docker compose down`，然后以 root 把 `/srv/koko/root.key` 移到一个单独的目录，比如 `/etc/koko-key/root.key`（先 `mkdir /etc/koko-key`），属主仍是 uid 65532，并对它执行 `chmod 0400`。然后在 `volumes:` 中加上 `/etc/koko-key/root.key:/run/secrets/koko-root.key:ro`，在 `environment:` 中加上 `KOKO_ROOT_KEY_FILE: /run/secrets/koko-root.key`，再运行 `docker compose up -d`。koko 服务器不会改动只读挂载上的文件；它无法读取的文件会在启动时被拒绝。
