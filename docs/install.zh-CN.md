# 安装和运行 koko 服务器

[English](install.md)

koko 服务器运行在一台由 systemd 管理的 Linux 机器上。本页介绍如何安装、校验发布版本、升级和回退、保管根密钥、恢复到另一台机器，以及没有根密钥时怎么办。每一步的原理见[安装器](subsystems/installer.md)和[发布](subsystems/release.md)的参考文档（英文）。

## 机器的要求

- amd64 或 arm64 的 Linux，由 systemd 247 或更高版本运行：Debian 11、Ubuntu 22.04、RHEL 9 或更高版本。不由 systemd 运行的容器会被拒绝。
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

要回到升级前的 koko，先把快照放回原位，再安装旧的发布版本。如果升级没有留下 `koko.db.before-<n>`，说明它没有更改 schema：这时只需安装旧的发布版本，即下面的最后一行。在这台机器上以 root 运行：

```sh
systemctl stop koko
cd /var/lib/koko
mkdir /var/lib/koko-newer
mv koko.db /var/lib/koko-newer/
mv koko.db-wal koko.db-shm /var/lib/koko-newer/ 2>/dev/null # there only after a crash
mv koko.db.before-* koko.db
curl -fsSL https://github.com/cieons/koko/releases/download/v0.3.0/install.sh | env KOKO_VERSION=v0.3.0 sh -s --
```

把 `v0.3.0` 换成你升级前的版本。升级之后写入的内容会随较新的数据库一起丢失，那个数据库留在 `/var/lib/koko-newer` 中。较旧的 koko 不会安装到比它新的数据库上：它在写入任何东西之前就拒绝，并指出应放回原位的快照。

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
