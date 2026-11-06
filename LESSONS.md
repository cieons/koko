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

### A test shortens a wait only where what it waits for never comes

**Why:** `TestServerInstallAgainAfterTheServiceDidNotStart` set the fake
machine's socket wait to 200 ms for a koko service that never opens its
socket, and the setting stayed for the test's next run, whose real koko
server then had to open its socket within 200 ms. Under CI load it took
longer, and `main` went red at 68c9ad8, which touched nothing near the test.
**Seen:** 1 (2026-11-06)
**Evidence:** 3cae7b1 (#38); check run 42081318152 on 68c9ad8; #77

### Wait for the event itself, not for one that comes just before it

**Why:** `koko server install` took the socket's file for the koko server
listening, but `net.Listen` binds the file before it listens. A loaded
machine stretches the moment between, and `server.init` dialled into it and
got `connection refused`: 4 of 240 runs of the install test under load. The
upgrade test, in turn, read the koko server's start from its log once
install returned, though the koko server logs it just after its socket
accepts: 3 failures under load.
**Seen:** 1 (2026-11-06)
**Evidence:** 3cae7b1 (#38); 4232f09 (#43); #77; ecff11b (#49); #78

### A channel closes only once every request on it has its answer

**Why:** an SSH answer sent after its channel closed is dropped, and the
side that asked sees `EOF` or a refusal. The in-process Host started an
exec's command before answering it, so an `echo` that ended at once closed
the channel first, and the Gateway refused the exec: CI on `main` went red
at 0ac2b3d. The Gateway, in turn, closed the client's channel as soon as the
Host's closed, while the Host's answer to the client's exec was still being
passed back: 1 in about 1,600 runs of the exec tests in a Linux container
got `EOF`.
**Seen:** 1 (2026-11-06)
**Evidence:** 0ac2b3d (#64); 2e5b9f4 (#59); check run 42164460501

## Moved to the code standards

_None yet._
