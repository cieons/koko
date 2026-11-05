# `koko skill install` writes a koko directory in the skills directory

`koko skill install [--dir <dir>]` writes its stub as `<dir>/koko/SKILL.md`.
`--dir` names an agent's skills directory, the one that holds a directory per
skill, and koko writes into a `koko` directory of its own there. Without
`--dir`, `<dir>` is `~/.claude/skills`, so the stub lands in
`~/.claude/skills/koko/` and the default and `--dir` follow one rule. The
agents that read skills from files, Claude Code among them, keep each skill
as `<skills directory>/<name>/SKILL.md`, so `--dir ~/.codex/skills` or
`--dir .agents/skills` is all another agent needs.

## Considered Options

- **`--dir` names the stub's own directory** (`--dir ~/.claude/skills/koko`):
  any layout fits, but the default and `--dir` would then follow two rules,
  and `--dir` given the skills directory itself, the natural mistake, would
  write over a `SKILL.md` that belongs to another skill or to the person.

## Consequences

- koko writes only into a directory it owns, so it never touches another
  skill's file, as the code standards ask of every file koko writes.
- The meaning of `--dir` is part of the CLI's contract (ADR 0016): within a
  major version it stays the skills directory.
- An agent whose skills are not one directory per skill needs the stub
  copied by hand.
