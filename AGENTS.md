# Notes for coding agents

koko is a self-hosted asset gateway for a fleet of rented servers. It keeps
the inventory, holds every credential, and is the one door people and agents
use to reach the machines. Agents write most of its code.

## Before changing code

Read [LESSONS.md](LESSONS.md): rules learned from mistakes in merged work.

Before changing a documented subsystem, read its
[reference](docs/subsystems/README.md) and verify the source it links.

## Writing and reviewing a change

Write and review every change against
[docs/code-standards.md](docs/code-standards.md): what a change contains,
test rules, acceptance checks and the commit format.

Before editing Go code, load the `use-modern-go` skill when it is available.

## Language

Everything committed to this repo is in English: code, comments, docs,
commit messages, issues and PRs. This overrides any personal preference for
commit language. `README.zh-CN.md` is the one Chinese file; it mirrors
`README.md`, and the two change together.

## Parallel sessions

A session running beside others works in a worktree of its own
(`git worktree add ../koko-<topic>`) and commits only its own files. After
committing, `git show --stat HEAD` lists those files and nothing else.
