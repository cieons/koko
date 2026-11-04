# koko

> 你的服务器，都在这里。

[English](README.md)

koko 是一个自托管的资产网关，管理你在各家厂商租用的服务器。它保存资产清单，
托管所有凭证，是人和 AI agent 访问这些机器的唯一入口。

**状态：** 早期设计阶段，还不能使用。

## 规划

- 资产清单：厂商、主机、服务、域名、续费日期。
- 凭证不离开服务端；主机只信任短期 SSH 证书。
- 终结式 SSH 网关，只占用一个 HTTPS 端口；`ssh`、`scp`、`rsync`、Ansible
  无需改动即可使用。
- 动态 Ansible inventory 与 runbook。
- 以 CLI 为核心，内置给编程 agent 用的 skill（`koko skill`）。

## 许可证

[Apache-2.0](LICENSE)
