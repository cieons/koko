# koko

> 你的服务器，都在ここ。

[English](README.md)

koko 是一个自托管系统。它记录在各家云服务商租用的服务器，并作为人与 AI agent 访问这些服务器的唯一入口。

**状态：** v0.3.0，首个将 `koko`（CLI）与 `kokod`（koko 服务器）拆分为两个程序的版本。已提供下列功能，仍处于早期阶段。

## 安装

koko 由两个程序组成：`koko` 为 CLI，支持 Linux 和 macOS；`kokod` 为 koko 服务器，仅支持 Linux。

安装 CLI，适用于 Linux（amd64 或 arm64）和 macOS（Apple Silicon）：

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sh
```

在由 systemd 管理的 Linux 机器上同时安装 koko 服务器，并指定所有者以及设备访问它所用的地址：

```sh
curl -fsSL https://github.com/cieons/koko/releases/latest/download/install.sh | sh -s -- --owner alice --address koko.example.com
```

非 root 用户请将任一行命令中管道后的 `sh` 改为 `sudo sh`。

[安装 koko](docs/install.zh-CN.md) 介绍各行命令的作用，以及升级、回退和恢复。没有 systemd 的环境中，可使用 Docker 运行镜像 `ghcr.io/cieons/koko`（见[在容器中运行](docs/install.zh-CN.md#在容器中运行)）。

在任一已加入的设备上运行 `koko web` 即可打开 Web 界面。不使用 CLI 的人（例如使用手机的团队成员）则可在浏览器中使用 Web 界面：koko 服务器将其提供给持有域名及证书的反向代理（例如 Caddy），每个人以密码登录，密码通过所有者签发的一次性密码链接设置。登录后，用户可维护其空间所允许的记录，但无法通过 SSH 访问主机（见[在浏览器中使用 Web 界面](docs/install.zh-CN.md#在浏览器中使用-web-界面)）。

## 功能

- 记录主机及其上运行的服务，并以带类型的属性描述，例如云服务商、地区和续费日期。
- 通过空间控制谁能查看和访问哪些主机。
- 机密：密码、密钥和令牌保存在空间中，以一把从不与数据存放在一起的密钥密封，只有 `koko secret show` 会输出其内容。凭据只应存入机密：备注和属性均以明文存储。
- 网关终结每一个 SSH 会话，因此只有 koko 持有主机所信任的密钥。
- 接入主机时只添加一个公钥，不作其他任何更改。
- 只使用一个 SSH 端口（默认 7822），无需证书；`ssh`、`scp` 和 `rsync` 可直接经由该端口使用，无需改动。
- 以 CLI 为核心，内置供编程 agent 使用的 skill（`koko skill`）。

## 许可证

[Apache-2.0](LICENSE)
