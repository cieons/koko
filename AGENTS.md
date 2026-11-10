# Notes for coding agents

koko keeps the record of a fleet of rented servers and is the one door people
and agents use to reach them. Agents write most of its code. Its vocabulary is
in [GLOSSARY.md](GLOSSARY.md); its decisions are in [docs/adr/](docs/adr/).

## Before changing code

Read [LESSONS.md](LESSONS.md): rules learned from mistakes in merged work.

Before changing a documented subsystem, read its
[reference](docs/subsystems/README.md) and verify the source it links.

## Writing and reviewing a change

Write and review every change against
[docs/code-standards.md](docs/code-standards.md): what a change contains,
test rules, acceptance checks and the commit format.

Before editing Go code, load the `use-modern-go` skill when it is available.

Work in `web/` reads [web/AGENTS.md](web/AGENTS.md) too: the web UI's
design, HeroUI's documentation at the version koko pins, and the parts the
shell gives every page.

## Language

Everything committed to this repo is in English: code, comments, docs,
commit messages, issues and PRs. This overrides any personal preference for
commit language. Three things are in Chinese, and each changes with its
English twin: `README.zh-CN.md` mirrors `README.md`, `docs/install.zh-CN.md`
mirrors `docs/install.md` section for section, and the web UI's Simplified
Chinese message catalogue mirrors its English one key for key.

## Parallel sessions

A session running beside others works in a worktree of its own
(`git worktree add ../koko-<topic>`) and commits only its own files. After
committing, `git show --stat HEAD` lists those files and nothing else.

## Agent skills

### Issue tracker

Issues and specs live in GitHub Issues on `cieons/koko`, driven with the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

The five default triage roles, each label named after its role. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `GLOSSARY.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.
