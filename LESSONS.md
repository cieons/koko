# Lessons from merged work

Each lesson is a rule that would have caught a mistake in merged work: the
rule, then why, then the evidence (commits, issues, PRs). Read this before
changing code.

## Recording a lesson

Record one after a `/retro`, a `/code-review` finding, or a fix to something
already merged. When the mistake matches an existing lesson, add the evidence
there and raise its **Seen** count by one for each separate day it recurs. A
lesson seen on 3 separate days moves into
[docs/code-standards.md](docs/code-standards.md) and leaves a one-line
pointer under [Moved to the code standards](#moved-to-the-code-standards).

```markdown
### <the rule>

**Why:** <what went wrong and what it cost>
**Seen:** 1 (2026-11-04)
**Evidence:** <commit, issue or PR>
```

## Lessons

### A Write returns only once its bytes are on the connection beneath it

**Why:** `wire.Conn` handed written bytes to a goroutine that copied them into
the SSH channel, so net/http counted a call answered while its answer was
still in that goroutine's hands. `closeRevoked` (#40) closed a Revoked Device
Session's connection at that moment, and about 1 in 140 devices revoking
their own Device Session got `EOF` instead of the answer.
**Seen:** 1 (2026-11-06)
**Evidence:** df63902 (#40); #76

### Lint the build of every OS koko builds for, not only the machine's own

**Why:** golangci-lint checks only the files one OS's build takes, so
`make check` on macOS skipped `cmd/koko/server_linux.go`. A gofumpt slip
there passed every local check, and CI on `main` stayed red from f42c347 on,
under every later push, so no push to `main` had its tests run on Linux.
**Seen:** 1 (2026-11-06)
**Evidence:** f42c347 (#42); check run 42033632619 and every run on `main`
after it; #76

## Moved to the code standards

_None yet._
