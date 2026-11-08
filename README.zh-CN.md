# koko

> 你的服务器，都在这里。

[English](README.md)

koko 是自托管的。它记录你在各家厂商租用的服务器，也是人和 AI agent
访问这些服务器的唯一入口。

**状态：** 首个版本 v0.1.0，已经能做到下面列出的事；仍是早期版本，难免粗糙。

## 安装

[安装和运行 koko 服务器](docs/install.zh-CN.md)：在装有 systemd 的 Linux
机器上运行一行命令，以及升级、回退和恢复。
没有 systemd 时，用 Docker 运行镜像 `ghcr.io/cieons/koko`
（[在容器中运行](docs/install.zh-CN.md#在容器中运行)）。

## 功能

- 记录主机及其上运行的服务，用带类型的属性描述，比如厂商、地区、续费日期。
- 用 Space 决定谁能看到、访问哪些主机。
- Secret：把密码、密钥、令牌存放在 Space 中，用一把从不与数据放在一起的密钥加密封存，只有 `koko secret show` 会打印出来。凭据只放进 Secret：备注和属性都是明文存储的。
- 网关终结每一个 SSH 会话，只有 koko 持有主机信任的密钥。
- 接入主机只追加一个公钥，其他什么都不改。
- 只占用一个 SSH 端口（默认 7822），不需要证书；`ssh`、`scp`、`rsync` 无需改动即可使用。
- 以 CLI 为核心，内置给编程 agent 用的 skill（`koko skill`）。

## 许可证

[Apache-2.0](LICENSE)
