# 安装 koko

[English](install.md)

koko 的每个发布版本包含两个程序：CLI `koko`，支持 Linux 和 macOS；koko 服务器 `kokod`，仅支持 Linux。一行命令即可安装 CLI；同一行命令加上 `--owner`，会在一台由 systemd 管理的 Linux 机器上同时安装 koko 服务器。没有 systemd 的环境中，可使用镜像 `ghcr.io/cieons/koko` 在容器中运行 koko 服务器（见[在容器中运行](#在容器中运行)）。本文介绍安装、校验发布版本、升级与回退、保管根密钥、恢复到另一台机器，以及根密钥丢失时的处理，随后介绍容器部署下的相应操作，最后介绍如何经由反向代理向浏览器提供 Web 界面（见[在浏览器中使用 Web 界面](#在浏览器中使用-web-界面)）。各步骤的实现原理见[安装器](subsystems/installer.md)和[发布](subsystems/release.md)参考文档（英文）。

## 机器要求

- amd64 或 arm64 架构的 Linux，由 systemd 247 或更高版本管理：Debian 11、Ubuntu 22.04、RHEL 9 或更高版本。对于不由 systemd 管理的容器，`koko server install` 会在下载任何文件之前拒绝安装，此时请改用[镜像](#在容器中运行)。
- `/var/lib/koko` 位于本地文件系统，而非 NFS 或其他网络文件系统。
- 一个空闲的 TCP 端口，默认为 7822，可通过 `--port` 另行指定。
- `curl`、`sha256sum`、`awk` 和 `mktemp`，以及 root 权限（以 root 用户运行，或通过 `sudo`）。

## 安装

在这台机器上以 root 运行以下命令，指定所有者以及设备访问 koko 服务器所用的地址：

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sudo sh -s -- --owner alice --address koko.example.com
```

以 root 用户运行时，请去掉 `sudo`。

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

不带参数运行同一行命令，即只安装 CLI，适用于 Linux（amd64 或 arm64）和 macOS（Apple Silicon）。该命令为运行它的用户安装 CLI，无需 `sudo`：

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sh
```

该命令下载适用于本机的 `koko`，使用发布版本的 `SHA256SUMS` 校验后安装为 `~/.local/bin/koko`；`~/.local/bin` 不存在时会先创建该目录。该命令既不需要 root 权限，也不依赖 systemd，因此也可在容器中使用。运行时无需交互输入，只向 stderr 输出，并且不修改 `~/.local/bin` 之外的任何文件。此后可使用 `koko update` 升级（见[升级](#升级)）。

多数 Linux 发行版会在 `~/.local/bin` 存在时将其加入 `PATH`，macOS 则不会。`~/.local/bin` 不在 `PATH` 中时，install.sh 会针对 `SHELL` 所指的 shell 输出将其加入 `PATH` 的命令，以及在新 shell 启动之前运行 `koko` 所用的完整路径。zsh 所用的命令如下：

```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zprofile
```

bash 使用同一行命令，但在 Linux 上写入 `~/.bashrc`，在 macOS 上写入 `~/.bash_profile`；fish 使用 `fish_add_path ~/.local/bin`；其他 shell 使用其中的 `export` 行，写入该 shell 的 profile 文件。`PATH` 中排在前面的是另一个 `koko` 时（例如早期版本安装的 `/usr/local/bin/koko`），install.sh 会指出该文件（见[从 v0.3.x 升级](#从-v03x-升级)）。

以 root 运行同一行命令时，CLI 安装为 `/usr/local/bin/koko`，供本机所有未自行安装 CLI 的用户使用，并使用 `sudo koko update` 升级。有 `sudo` 权限的用户可使用 `curl … | sudo sh` 安装该文件。以 root 运行的智能体使用该文件；以其他用户运行的智能体使用其自身的 `~/.local/bin/koko`，install.sh 会输出该文件的完整路径，供不读取登录 shell 的 `PATH` 的运行环境使用。

不使用 install.sh 时，请下载适用于本机的文件并校验、安装：Mac 使用 `koko-darwin-arm64`，Linux 使用 `koko-linux-amd64` 或 `koko-linux-arm64`。Mac 的操作如下：

```sh
base=https://github.com/cieons/koko/releases/latest/download
curl -fsSL -O "$base/koko-darwin-arm64" -O "$base/SHA256SUMS"
grep ' koko-darwin-arm64$' SHA256SUMS | shasum -a 256 -c
mkdir -p ~/.local/bin
install -m 0755 koko-darwin-arm64 ~/.local/bin/koko
```

Linux（amd64）的操作如下：

```sh
base=https://github.com/cieons/koko/releases/latest/download
curl -fsSL -O "$base/koko-linux-amd64" -O "$base/SHA256SUMS"
grep ' koko-linux-amd64$' SHA256SUMS | sha256sum -c
mkdir -p ~/.local/bin
install -m 0755 koko-linux-amd64 ~/.local/bin/koko
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

CLI 可自行升级，无需 root 权限：

```sh
koko update
```

该命令下载最新发布版本中适用于本机的 `koko`，使用发布版本的 `SHA256SUMS` 及其构建信息校验，然后替换当前运行的文件。已打开的 SSH 会话、`koko web` 和正在运行的命令均不受影响。`koko update --check` 只说明将执行的操作，不做任何更改。root 的 `/usr/local/bin/koko` 使用 `sudo koko update` 升级。

通过 systemd 安装的 koko 服务器可使用一条命令就地升级，所有者可在其任一设备或 koko 服务器所在的机器上运行：

```sh
koko server upgrade            # 升级到最新发布版本
koko server upgrade --check    # 说明将升级到的版本，不做任何更改
```

`koko server upgrade` 请求 koko 服务器升级，koko 服务器再请求以 root 运行的升级助手 `koko-upgrade.service`。升级助手从 koko 服务器处只接收一个比已安装版本更新的发布版本号。它自行下载该发布版本的 `kokod`，使用发布版本的 `SHA256SUMS` 及其构建信息校验，将已安装的 `kokod` 保留为 `/var/lib/koko-upgrade/kokod.previous`，然后安装新的 `kokod`，koko 服务器随之重启。所有经由 koko 服务器的 SSH 会话都会结束。新的 `kokod` 无法启动时，升级助手会将其停止；如果它已更改数据库 schema，则放回数据库快照，然后重新安装原先的 `kokod`。`koko server upgrade` 等待升级结果，随后在 `koko update` 可替换运行该命令的 CLI 时，将其升级到同一版本，否则说明升级方法。升级失败时，该命令以 `upgrade_failed` 退出并给出原因，CLI 保持不变。Web 界面为所有者提供同样的升级。`koko server show` 显示上一次升级的结果，在该机器上运行 `journalctl -u koko -u koko-upgrade` 可查看详细信息。

以 root 不带参数再次运行安装命令同样可以升级 koko 服务器，并为尚无升级助手的 koko 服务器安装升级助手：

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sudo sh
```

该命令先升级 koko 服务器，再升级 root 的 CLI。`koko server install` 将 `kokod` 替换为新发布版本中的文件，重新写入 systemd unit 并重启 koko 服务器；服务器的记录、所有者、服务器地址和密钥均保持不变。所有已打开的 SSH 会话都会结束，并显示一行提示，说明 koko 服务器正在重启。此后 install.sh 才替换 `koko`。如果 koko 服务器升级失败，install.sh 会就此停止，CLI 保持不变，因此 CLI 的版本不会领先于同一机器上的 koko 服务器。执行 `koko server upgrade` 之后，koko 服务器的版本可能新于同一机器上 root 的 CLI，两者可以正常配合。此时 `koko server install` 会拒绝以该 CLI 对应的较旧 `kokod` 替换较新的 `kokod`，并给出 `sudo koko update`，用于先将 CLI 升级（见[回退](#回退)）。

在 koko 服务器所在的机器上不使用 `sudo` 运行安装命令时，CLI 会安装到运行者自身的 `~/.local/bin` 中，root 的 `/usr/local/bin/koko` 保持不变：在该机器上请使用 `| sudo sh` 运行。可免密码使用 `sudo` 的用户仍会通过 `sudo` 升级 koko 服务器。对于两者均不满足的用户，不带参数的安装命令会使 koko 服务器保持不变，并给出相应提示，指出 `koko server upgrade`；带参数时则会拒绝，且不写入任何内容。

由 v0.1.0 或 v0.2.0 安装的 koko 服务器（当时 `koko` 和 `kokod` 尚未拆分为两个程序）同样按此方式升级：升级后其 unit 运行 `/usr/local/bin/kokod run`，`/usr/local/bin/koko` 则为 CLI。

升级时不接受 `--owner` 和 `--address`：如需更改服务器地址，请使用 `koko server set address=<host[:port]>`。

升级会保留 koko 服务器的监听端口，包括通过 drop-in（`systemctl edit koko`）设置的端口；通过 drop-in 将端口改为 1024 以下后，请再次运行安装命令，使 koko 服务器能够绑定该端口。使用 `--port` 可更换端口。服务器地址未写明端口时，设备使用监听端口，因此更换端口后，输出中会给出需要在每台设备上运行的 `koko connect <host:port>` 命令。

新版本的 koko 更改数据库 schema 时，koko 服务器会先将原数据库复制为 `/var/lib/koko/koko.db.before-<n>`，即快照，并且只保留最新的一份。

### 从 v0.3.x 升级

v0.3.x 及更早版本的 CLI 安装命令以 root 安装 `/usr/local/bin/koko`。在存在该文件的设备上，请依次执行以下步骤：

1. 不使用 `sudo` 运行 CLI 安装命令（见[仅安装 CLI](#仅安装-cli)）。该命令安装 `~/.local/bin/koko`，随后指出 `/usr/local/bin/koko`（保留在原处），并给出删除该文件的命令。
2. 如果在新 shell 中 `command -v koko` 仍指向 `/usr/local/bin/koko`，请在 `PATH` 中将 `~/.local/bin` 置于 `/usr/local/bin` 之前；`~/.local/bin` 不在 `PATH` 中时，install.sh 会输出所需的命令。
3. 使用新的 `koko` 运行任一 `koko` 命令，例如 `koko status`。该命令会将 koko 的 ssh_config 指向其自身，使 `ssh <host>.koko` 同样使用新的 `koko`。
4. 本机不再有其他用户使用旧文件后，将其删除：

   ```sh
   sudo rm /usr/local/bin/koko
   ```

如需为本机所有用户保留同一个 `koko`，请改用 `| sudo sh` 运行安装命令：该命令与以往一样替换 `/usr/local/bin/koko`，此后使用 `sudo koko update` 升级。

对于由 systemd 管理的 v0.3.x koko 服务器，请按上文以 root 运行安装命令。该命令升级 koko 服务器并安装升级助手，此后即可使用 `koko server upgrade` 和 Web 界面中的升级。该机器上的 CLI 仍为 root 的 `/usr/local/bin/koko`。

## 回退

如需回退 CLI，请指定发布版本：

```sh
koko update --version v0.3.0
```

将 `v0.3.0` 替换为要回退到的版本。早于 `koko update` 的发布版本（例如 v0.3.0）不包含 `koko update`：如需从该版本重新升级，请再次运行安装命令（见[仅安装 CLI](#仅安装-cli)）。

如需在 koko 服务器所在的机器上回退到升级前运行的发布版本，请使用最新的 install.sh 安装该版本，并在 `KOKO_VERSION` 中指定。在这台机器上以 root 运行：

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | env KOKO_VERSION=v0.3.0 sh
```

将 `v0.3.0` 替换为要回退到的版本。也可以使用该版本自带的 install.sh（位于 `releases/download/v0.3.0/install.sh`），并指定相同的 `KOKO_VERSION`。`koko server upgrade` 和升级助手只会升级到更新的版本，因此回退始终使用此命令。如果升级未更改 schema，回退即已完成。如果更改了 schema，旧版本的 koko 会拒绝在较新的数据库上安装，不写入任何内容，较新的 koko 服务器继续运行，install.sh 也不会改动 CLI。拒绝信息中会指出需要放回原位的快照：

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

`koko server install` 会拒绝替换比其自身 CLI 更新的 `kokod`（例如执行 `koko server upgrade` 之后的情况），以免使用较旧的 CLI 更改设置时使 koko 服务器回退。拒绝信息会指出两个版本以及 `sudo koko update`，且不写入任何内容。`KOKO_VERSION` 指定 CLI 自身的版本时则允许继续，上述回退命令即依此工作：install.sh 会将 `KOKO_VERSION` 传递给 `koko server install`。手动运行时，对应的命令为 `sudo env KOKO_VERSION=v0.3.0 koko server install`。

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
3. 以 root 安装 CLI，即将不带参数的安装命令通过管道交给 `sudo sh`（见[仅安装 CLI](#仅安装-cli)），然后运行不带 `--owner` 的 `sudo koko server install`；如果旧的 koko 服务器监听的不是 7822 端口，请加上 `--port <port>`。该命令会找到 koko 服务器的记录，安装与自身同一版本的 `kokod` 并启动 koko 服务器。

主机密钥随数据目录一同迁移，因此设备仍然信任 koko 服务器。如果地址发生变化，请使用 `koko server set address=<host[:port]>` 设置新地址，并在每台设备上运行 `koko connect <host:port>`。

当数据目录本身属于其他用户时（例如由 root 放回的副本），systemd 会将该目录及其中的所有内容交给 `koko` 用户。如果文件被复制到一个已属于 `koko` 的 `/var/lib/koko` 中，请在安装前运行 `chown -R koko:koko /var/lib/koko`。

## 根密钥丢失时

如果根密钥及其所有副本均已丢失，记录将无法再打开。唯一的办法是重新开始：将 `/var/lib/koko` 连同无法加载的 `/etc/koko/root.key` 移到别处，然后带 `--owner` 重新安装。代价是所有已密封的内容和所有已加入的设备：每台主机都需要重新添加并接入，每个机密都需要重新添加，每台设备都需要运行 `koko leave`，再使用新的加入码加入。

## 卸载

`koko server uninstall` 停止 koko 服务器，并删除其 unit、升级助手的 unit 及其目录 `/var/lib/koko-upgrade`，以及 `/usr/local/bin/kokod`，不在机器上留下任何以 root 运行的 koko 组件。它保留 CLI `/usr/local/bin/koko`、数据目录、根密钥和 `koko` 用户，并输出 koko 服务器原先监听的端口，以及原先设有的 Web 监听地址。如需在同一份数据上再次运行 koko 服务器，请运行不带 `--owner`、带上该 `--port`（若输出了 Web 监听地址，一并带上该 `--web-listen`）的 `sudo koko server install`：它会再次安装与自身同一版本的 `kokod`。

CLI 只有一个文件：使用 `rm ~/.local/bin/koko` 删除；root 的 CLI 使用 `sudo rm /usr/local/bin/koko` 删除。

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
#   echo KOKO_VERSION=0.4.2 > .env    # the release to run, without its v
#   sudo install -d -o 65532 -g 65532 -m 0700 state    # what koko keeps
# After it: docker exec koko koko device add    # your first Join Code
# To upgrade, set the new release in .env, then run
#   docker compose pull && docker compose up -d
# KOKO_VERSION=latest is allowed too: the pull and up alone then upgrade,
# going back means pinning a version again, and a release that changes the
# database migrates it at the first start after the pull that brings it.
# up -d alone never pulls, so nothing upgrades by surprise. A pinned
# version is the default, so that every upgrade is chosen.
services:
  koko:
    image: ghcr.io/cieons/koko:${KOKO_VERSION:?set KOKO_VERSION in .env, such as KOKO_VERSION=0.4.2}
    container_name: koko
    restart: unless-stopped
    stop_grace_period: 30s
    # uid 65532 from the start, with no capability and none to gain.
    cap_drop: [ALL]
    security_opt: [no-new-privileges:true]
    ports:
      - "7822:7822"        # with another host port, name it in KOKO_ADDRESS
      # - "127.0.0.1:7823:7823"            # the web UI, for a reverse proxy on this machine alone
    volumes:
      - ./state:/koko      # the data and the Root Key, beside this file: make it with install -d (above), or use a named volume
    environment:
      KOKO_OWNER: alice                  # read only at the first start
      KOKO_ADDRESS: koko.example.com     # host[:published port]; read only at the first start
      # KOKO_WEB_LISTEN: 0.0.0.0:7823      # the web UI: see docs/install.md, "The web UI in a browser"
```

将 `KOKO_OWNER` 设为所有者的用户名，将 `KOKO_ADDRESS` 设为设备访问 koko 服务器所用的地址：与 [`--address`](#安装) 相同，建议使用 DNS 名称。然后在该文件所在目录中运行：

```sh
echo KOKO_VERSION=0.4.2 > .env     # the release to run, without its v
sudo install -d -o 65532 -g 65532 -m 0700 state   # once, unless a named volume is used
docker compose up -d
docker exec koko koko device add   # your first Join Code
```

`KOKO_VERSION` 为必填项，确保容器运行的始终是明确选定的发布版本。该值也可以是 `latest`，即最新发布版本：此时升级只需运行 `docker compose pull && docker compose up -d`，无需修改任何文件；单独运行 `docker compose up -d` 不会拉取镜像，因此不会意外升级。此时如需回退，须重新指定具体版本；更改数据库 schema 的发布版本会在拉取后首次启动时迁移数据库。默认做法仍是指定具体版本，以确保每次升级均经过选择。首次启动时，koko 服务器会生成根密钥 `state/root.key` 并完成初始化。此后 `docker logs koko` 会显示 `koko server initialised`，以及主机密钥的指纹、监听地址、根密钥的路径和 id，还有下一步要运行的 `koko device add` 命令。日志中不会出现根密钥或加入码。未设置 `KOKO_ADDRESS` 的全新 koko 服务器会拒绝启动并指出该设置：容器自身的 IP 是私有地址，设备无法访问。

`koko device add` 会输出第一个加入码（仅可使用一次，15 分钟内有效），以及在所有者的计算机上配合它使用的 `koko join` 命令（见[仅安装 CLI](#仅安装-cli)）。如果加入码遗失或已过期，请再次运行同一命令；所有设备均已丢失时，也按此操作。

不使用 Compose 时，可在 `/srv/koko` 中使用 `docker run` 运行同样的容器：

```sh
docker run -d --name koko --restart unless-stopped --stop-timeout 30 \
  --cap-drop ALL --security-opt no-new-privileges:true \
  -p 7822:7822 -v "$PWD/state:/koko" \
  -e KOKO_OWNER=alice -e KOKO_ADDRESS=koko.example.com \
  ghcr.io/cieons/koko:0.4.2
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

有更新的发布版本时，Web 界面会在每个页面上方向所有者显示通知，其中的“升级方法”列出两个步骤，每步均可复制；`koko server show` 也会给出这两个步骤。在 `.env` 中设置新的发布版本，例如 `KOKO_VERSION=0.5.0`，然后在 `compose.yaml` 所在目录中运行：

```sh
docker compose pull && docker compose up -d
```

若设置为 `KOKO_VERSION=latest`，只需运行这一行即可升级。Compose 会拉取新镜像并替换容器，容器的记录、所有者、服务器地址和密钥均保留在 `state/` 中。所有已打开的 SSH 会话都会结束，并显示一行提示，说明 koko 服务器正在重启。新版本的 koko 更改数据库 schema 时，koko 服务器会先将原数据库复制为 `state/data/koko.db.before-<n>`，并且只保留最新的一份快照。

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
gh attestation verify oci://ghcr.io/cieons/koko:0.4.2 --repo cieons/koko
```

镜像中的 `/usr/local/bin/kokod` 和 `/usr/local/bin/koko` 与发布版本的 `kokod-linux-<arch>` 和 `koko-linux-<arch>` 逐字节相同，即 `SHA256SUMS` 所列的文件。

### Kubernetes 与其他用户

镜像以数字 uid 65532 运行，因此 Kubernetes 的 `runAsNonRoot` 可直接接受；请确保其卷可由 uid 65532 写入，例如设置 `fsGroup: 65532`。koko 服务器无法写入的目录会在启动时被拒绝，并指出该目录以及自身运行所用的 uid 和 gid；koko 服务器也拒绝以 root 运行，因此请勿传入 `--user 0` 或 `user: root`。使用其他 uid 时，`/koko` 和 `/run/koko` 须可由该 uid 写入，例如在 `/run/koko` 上挂载一个属于该 uid 的 tmpfs。

### 将根密钥分开存放

如需将根密钥存放在备份目录之外，可从其他位置以只读方式挂载根密钥，并将 `KOKO_ROOT_KEY_FILE` 指向它。首次启动后，运行 `docker compose down`，然后以 root 将 `state/root.key` 移到一个单独的目录，例如 `/etc/koko-key/root.key`（先运行 `mkdir /etc/koko-key`），属主仍为 uid 65532，并执行 `chmod 0400`。然后在 `volumes:` 中加入 `/etc/koko-key/root.key:/run/secrets/koko-root.key:ro`，在 `environment:` 中加入 `KOKO_ROOT_KEY_FILE: /run/secrets/koko-root.key`，再运行 `docker compose up -d`。koko 服务器只读取该文件；无法读取时，会在启动时拒绝运行。

## 在浏览器中使用 Web 界面

Web 界面有两种访问方式。`koko web` 在已加入的设备上或 koko 服务器所在的机器上，向运行它的人提供 Web 界面，无需其他准备。不使用 CLI 的人（例如通过手机登记主机的团队成员）则在 **Web 地址**以用户名和密码登录。为此，koko 服务器通过第二个监听器，即 Web 监听器，以明文 HTTP 向反向代理（例如 Caddy）提供 Web 界面，由反向代理持有域名及其 TLS 证书，因为 koko 自身不处理 TLS（见 [ADR 0042](adr/0042-the-web-ui-is-also-served-over-plain-http-to-the-owners-reverse-proxy.md)，英文）。

**切勿将 Web 监听器直接暴露在网络上。** 它提供的是明文 HTTP，发送给它的密码将以未加密的形式在网络上传输。此外，它以 `X-Forwarded-For` 请求头的最后一项（由反向代理追加）作为客户端地址，直接访问它的客户端因此可以自行指定按地址计数的登录限制所统计的地址，届时仅按用户名计数的限制仍然有效。请如下文所示将其绑定到回环地址，且不要在云服务商的防火墙中开放其端口。

### 开启监听器

Web 监听器在设置地址之前处于关闭状态。对于通过 systemd 安装的机器，请在 koko 服务器所在的机器上以 root 运行 `koko server install`，并通过 `--web-listen` 指定地址：

```sh
sudo koko server install --web-listen 127.0.0.1:7823
```

该命令与安装命令一样，将 koko 服务器原地升级到 CLI 的构建并保留其记录，随后重启 koko 服务器，使 Web 监听器监听 `127.0.0.1:7823`，仅同一机器上的反向代理能够访问。此后的升级会保留该地址；`--web-listen off` 可再次关闭监听器。端口被其他进程占用时，安装程序会拒绝执行，并指出该进程。

在容器中，监听器监听容器的所有地址，再由端口映射将其仅发布到 Docker 宿主机的回环地址上。在 `compose.yaml`（见[启动](#启动)）中，删除两行含有 7823 端口的配置开头的 `# `，使其如下所示：

```yaml
    ports:
      - "7822:7822"        # with another host port, name it in KOKO_ADDRESS
      - "127.0.0.1:7823:7823"            # the web UI, for a reverse proxy on this machine alone
    environment:
      KOKO_WEB_LISTEN: 0.0.0.0:7823      # the web UI: see docs/install.md, "The web UI in a browser"
```

然后运行 `docker compose up -d`。使用 `docker run` 时，加上 `-p 127.0.0.1:7823:7823 -e KOKO_WEB_LISTEN=0.0.0.0:7823`。如果反向代理同样运行在容器中，并与 `koko` 容器共用一个 Docker 网络，则无需发布该端口：反向代理可通过 `koko:7823` 访问监听器。

每次启动时，koko 服务器会记录一行 `web listener` 日志，写明监听器的地址和 Web 地址（见 `journalctl -u koko` 或 `docker logs koko`），`koko server show` 也会显示这两项。

### 反向代理

使用 Caddy 时，在 Caddyfile 中为该域名添加一个站点：

```caddyfile
koko.example.com {
	encode zstd gzip
	reverse_proxy 127.0.0.1:7823
}
```

Caddy 会为该域名获取证书，按浏览器发送的原样转发 `Host` 请求头，并设置 `X-Forwarded-For`，以客户端地址作为其最后一项，即满足 koko 的全部要求。koko 提供的 Web 界面文件未经压缩，`encode zstd gzip` 负责压缩这些文件。Web 界面在域名的根路径提供，不支持在子路径下提供（例如 `https://example.com/koko`）。

> **nginx** 默认以上游地址作为 `Host`，且不设置 `X-Forwarded-For`。请在转发至 Web 监听器的 `location` 中加入：
>
> ```nginx
> proxy_set_header Host $host;
> proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
> ```
>
> 缺少 `Host` 一行时，koko 服务器会以 `421` 和一个写明其 Web 地址的页面回应所有请求。缺少 `X-Forwarded-For` 一行时，所有浏览器的地址均为反向代理的地址，任何人失败 10 次后，所有人的登录请求都将在 15 分钟内被拒绝。

### 设置 Web 地址

Web 地址是反向代理提供服务的 `https://` 地址，端口不是 443 时才写明端口，且不含路径。在 koko 服务器所在的机器上运行：

```sh
koko server set web_url=https://koko.example.com
```

在容器中运行：`docker exec koko koko server set web_url=https://koko.example.com`。与服务器地址一样，Web 地址只能在 koko 服务器所在的机器上设置，设备和浏览器均无法设置。设置之前，Web 监听器以一个写明该命令的页面回应所有请求。设置之后，koko 服务器只回应 `Host` 请求头与 Web 地址的主机名和端口一致的请求。更改 Web 地址立即生效：为原地址签发的密码链接随即失效，所有浏览器均需重新登录。`koko server set --unset web_url` 可停止浏览器访问，同时保留监听器。

### 邀请浏览器用户

对于将在浏览器中使用 koko 的人，所有者以密码链接代替加入码发出邀请：

```sh
koko user invite bob --web
```

该命令将 bob 创建为成员，并输出一个链接 `https://koko.example.com/password#…`，仅可使用一次，15 分钟内有效。`--valid` 可将有效期设为 1 分钟至 7 天，例如对稍后才会打开的链接使用 `--valid 1d`；`koko device add` 同样接受 `--valid`。在 Web 界面中，同样的邀请位于“访问权限”页面：依次选择“邀请”“通过浏览器”和“有效期”。所有者可在任一设备上、在 koko 服务器所在的机器上（容器中使用 `docker exec koko`），或在以所有者身份登录的浏览器中执行上述操作。bob 与其他成员一样加入空间：运行 `koko space member add <space> bob`，或在邀请时直接选择空间。

在浏览器中打开该链接后，页面显示用户名并要求设置密码：至少 15 个字符，至多 128 个字符，可使用除用户名本身以外的任意字符。设置完成后，该浏览器即已登录。此后，bob 在 `https://koko.example.com` 以用户名 `bob` 和该密码登录。

### 重置与移除密码

用户遗失密码时，可获得新的密码链接；同时希望使用浏览器的 CLI 用户（包括所有者）也通过同样的方式获得密码链接：

```sh
koko password link bob
```

该命令与邀请一样接受 `--valid`，并使 bob 此前未使用的链接失效。通过该链接设置密码后，以 bob 身份登录的所有浏览器均会退出登录。在 Web 界面中，对应的操作是“访问权限”页面上 bob 面板中的“签发密码链接”；该页面上每个人所在的行会注明其是否已设置密码。

如需取消浏览器访问，同时保留该用户及其空间和设备，请运行：

```sh
koko password rm bob
```

该命令删除密码，并使以 bob 身份登录的所有浏览器退出登录；在 Web 界面中，对应 bob 面板中的“移除密码”。`koko user remove bob` 则完全移除 bob，其浏览器的登录会话随之结束；如果 bob 从未使用过 koko，则直接删除该用户。

### 登录会话

每个已登录的浏览器即一个登录会话。浏览器退出登录、7 天未使用、开始 30 天后、用户的密码被重新设置或移除，或登录会话被吊销时，登录会话即结束：

```sh
koko signin ls
koko signin revoke <id>
```

`koko signin ls` 列出调用者自己的登录会话，所有者可看到所有用户的登录会话；加上 `--all` 时，一并列出已结束的登录会话。在 Web 界面中，每个人可在“账户”页面的“浏览器登录会话”中查看自己的登录会话；该页面的“修改密码”用于修改本人的密码，并使本人的其他浏览器退出登录。所有者可在“访问权限”页面上某人的面板中查看其登录会话，并通过“吊销”结束它们。

同一用户名连续 10 次密码错误后，以该用户名登录的请求将在 15 分钟内被拒绝，通过密码链接设置密码后即解除。同一地址在 15 分钟内失败 10 次后，该地址的请求将被拒绝，直至最早一次失败已满 15 分钟。已登录的浏览器不受影响。

### 浏览器用户可执行的操作

登录后，用户在 Web 界面中可执行的操作与在其设备上相同，完全取决于其空间成员身份：可维护其所在空间的主机、服务和机密；所有者可执行其在设备上能够执行的全部操作。与在设备上一样，`koko server set` 仍只能在 koko 服务器所在的机器上运行。浏览器无法访问主机：它不会打开 SSH 会话或隧道，因为只有设备才能经由网关连接。需要使用 SSH 的浏览器用户，可在“账户”页面通过“添加设备”签发加入码，再在安装了 CLI 的计算机上以 `koko join` 使用该加入码；此后该计算机与该用户的其他设备一样可以访问主机。

使用 CLI 的所有者可继续使用无需密码的 `koko web`。
