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

### A command a test runs on the in-process Host ends when its SSH Session does

**Why:** the in-process Host runs a command through `/bin/sh -c`, and the
test's end kills only that shell. On Linux, dash forks `sleep 30` rather
than exec'ing it, so the sleep held the command's stdout and the Host's
cleanup waited 30 s for it. Four subtests that kept a session open with
`sleep 30` cost 2 minutes, two `koko exec` tests 1 more, and the cli
package passed its 5-minute limit in CI at c152887. Keep a session open
with a command that ends with it, such as `cat`, whose stdin closes when
the SSH Session ends, or `exec` the command so the kill reaches it.
**Seen:** 1 (2026-11-06)
**Evidence:** 0ac2b3d (#64); check run 42166495220; 2e5b9f4 (#59)

### A change that makes something happen by itself re-reads every test that counts it

**Why:** #54 made the Device Mux reconnect at once after a drop, where it
had reconnected only when a command asked. `TestTheDeviceMuxCarriesChannelsAcrossADrop`
counted the Device Connections right after a drop, as the old behaviour
left them; it passed locally whenever the reconnect lost the race, failed
on both CI runners, and left `main` red at c152887.
**Seen:** 1 (2026-11-06)
**Evidence:** c152887 (#54); check runs 42166495220 and 42167225724

### Every SSH connection koko opens channels on goes through sshconn.Wrap

**Why:** x/crypto's `OpenChannel` on a connection that has just ended can
wait for ever: the connection fails the opens it holds as it ends, then its
writes, and an open that comes in between is failed by neither. A Tunnel
whose channel failed with its Device Connection Resumed on that same
connection before koko saw it end, and `koko exec` with an upload in flight
hung there instead of reconnecting: about 1 in 30 runs of its test, and CI
on `main` went red at 0d192f9.
**Seen:** 1 (2026-11-08)
**Evidence:** 1e3a865 (#70); check run 42450143194; #84

### A wrapper's own failure wraps no error its callers read as an outcome, such as io.EOF's "never sent"

**Why:** `sshconn.Wrap` failed an open still waiting for its answer, when
its connection ended, with an error wrapping the end's `io.EOF`. The Device
Mux reads `io.EOF` from an open as "it never went out", so it sent the open
again on its next Device Connection, though the koko server may have acted
on it. That happened in about 1 in 5 runs of
`TestTheDeviceMuxCarriesChannelsAcrossADrop`, a failure first put down to
the test's cut.
**Seen:** 1 (2026-11-08)
**Evidence:** 2b9137c (#84); #85; #86

### Ctrl-C's watch covers every wait until the outcome, not only the step it was set for

**Why:** `koko exec` closed its Tunnel on Ctrl-C only while its SSH
handshake ran, and watched for Ctrl-C again only once its command ran. A
Ctrl-C between the two, while the command's channel was opening, closed
nothing, so `koko exec` waited on a slow Host; one that came as the
handshake ended closed the Tunnel after all, and the open failed as "the
SSH Session failed" with x/crypto's raw text. Watch once, from the first
wait to the outcome, and read any failure after Ctrl-C as interrupted.
**Seen:** 1 (2026-11-08)
**Evidence:** #90

### A channel a write fails on is given up only once its reads have failed too

**Why:** an SSH channel the other end has closed fails every write at once,
while its reads still return what arrived before the close. The resumable
stream took a failed write for its channel's end and came off it unread, so
when `koko exec`'s Ctrl-C sent the Tunnel's close frame and closed the
channel just as the Gateway sent a frame, the Gateway dropped the close
frame and held the Tunnel Detached for its grace instead of logging the SSH
Session's end: about 1 in 1,000 runs of
`TestExecEndsTheSSHSessionWhenInterrupted` under load, and CI on `main` went
red at fae944b, which changed no Go code.
**Seen:** 1 (2026-11-09)
**Evidence:** 4ca37b3 (#31); surfaced by 1a0b0ec (#90) in check run
42539501558; #99

### A test times a wait from the moment the product counts it from

**Why:** the probe tests of both ends of a Device Connection timed the
drop from the moment they stalled the path, but each end counts
`DeviceDead` or `ServerDead` from the last byte that reached it, which a
loaded machine may have delivered several probes before the stall. The
test allowed two probe intervals for that and no more, so a drop the Device
Mux declared on time looked early to the test, and CI's macOS job on `main`
went red at 71971a9, which touched nothing near the probe. The Device Mux's
test also left its real wake and route watches on, and a probe either
sends at once gets less time than `DeviceDead`, so the moment it counts
from can be another one still. `TestASilentConnectionIsDeadAfterDead`
counted the probes that reached the peer after it shut the path, which it
could shut while a probe's answer was on its way; `liveness.Watch` sends
no probe while one is unanswered, so none came after, and a local
`make check` failed.
**Seen:** 2 (2026-11-09, 2026-11-10)
**Evidence:** c152887 (#54); check run 42542720470; #100; the liveness
test's fix of 2026-11-10

### Code that compares versions is tested with the versions CI's builds carry

**Why:** d6db4d8 made the CLI and the koko server turn away each other below
`compat.Oldest`, and `make check` passed locally, where a clone has tags
and a build carries a pseudo-version with a release before it
(`v0.4.2-0.<time>-<revision>`). CI's shallow checkout has no tags, so its
builds carry `v0.0.0-<time>-<revision>`, which sorted below every release:
the image's own koko was refused by its own kokod, `TestRealOpenSSH` failed
with `cli_too_old`, and the real-systemd check's builds, tagged `v0.0.1` and
`v0.0.2`, fell below the window too. `main` went red in the check, container
and systemd workflows at d6db4d8 and stayed red at 0ff30a4. Test such code
with a build from a tagless clone, `(devel)` and the tags CI's checks give,
not only with the local clone's version.
**Seen:** 1 (2026-11-11)
**Evidence:** d6db4d8 (#143); runs 42982103558 and 42982103626; fixed by
7fe9f3d and 6791357

### What a fake remembers about a file by its inode holds the file open

**Why:** the fake machine recorded each file's owner by device and inode,
as `os.SameFile` compares them, and kept the record after the file was
removed. Linux then gave the freed inode of a temporary file the installer
had given to `root` to a later `upgrade.request`, the fake answered `root`
for it, and the upgrade helper refused the request:
`TestTheUpgradeHelperPutsThePreviousKokodBack` failed on CI's ubuntu job at
c36357e, which touched nothing near the helper. An inode is one file's only
while that file exists or is held open.
**Seen:** 1 (2026-11-11)
**Evidence:** check run 43006445436; #150

### Squash work in progress onto the branch's merge-base, never onto origin/main

**Why:** #149's first slice was squashed with `git reset --soft
origin/main`. Another worktree's fetch had moved `origin/main` on to
c1f0138 (#150) since the branch was cut, so the squashed commit's tree
still held the files as they were before #150, and it took #150's changes
back out: its harness files, caught in `git show --stat` before the push,
and its lesson in this file, which was pushed and restored a commit later.
`git reset --soft $(git merge-base HEAD origin/main)` squashes only the
branch's own commits; the rebase then brings in the rest. After a squash,
`git show --stat HEAD` lists the ticket's files and nothing else.
**Seen:** 1 (2026-11-11)
**Evidence:** ff44d9e (#149); restored by 2a6135b

### An SSH authentication step that failed with its connection is no refusal

**Why:** x/crypto counts a method whose step failed with the connection as
tried, as it counts one the peer refused, and koko's `AuthCallback` read a
tried method as the koko server's no. A `koko join` cut at its wait in SSH
authentication said "the koko server refused this device", with
`invalid_join_code`, and the flake it hid was first looked for in the
koko server.
**Seen:** 1 (2026-11-12)
**Evidence:** check run 43128685688 on 08ba4ad; #163

### An ending never waits for a lock that a blocked write holds

**Why:** a web terminal's shell writes its output into the resumable
stream under a lock, so that records take turns, and a write waits for
room in the stream's 2 MiB buffer, which a page that has left never frees.
The endings without a notice (a page's `end`, the grace, `koko web`
ending) took that lock before they closed the stream, which alone ends the
write, so a busy shell's terminal never ended: its Host connection and its
place in the Sign-in's limit stayed held, and `server.Close` hung on it.
Close what the blocked write waits on first, or wait for the lock only
beside a way out.
**Seen:** 1 (2026-11-12)
**Evidence:** c9681cd (#172), found by the ticket's review before its
second slice; the fix in that slice's commit

### The web UI reads and writes the browser's storage only where a refusal is caught

**Why:** the rail read whether it was folded from `localStorage` in the
frame's first render with no `try`, though the language, the mode, a
list's view and the release's Later each catch a refusal and remember
nothing. A browser that refuses site storage throws `SecurityError` there,
so every page in the frame would have shown nothing. Read and write it
through a guarded helper, as `storedMode` does, and test the frame with a
storage that throws.
**Seen:** 1 (2026-11-12)
**Evidence:** 6ca3d57 (#182), found by the ticket's review; fixed by
b9e663c

### A process that lost a race to start gives up once the winner serves

**Why:** a Device Mux that found its build's lock taken waited the whole
`DeviceMuxStart` for it, even while the holder served, as the loser of a
race to start does. When `koko disconnect` stopped the winner within that
wait, the loser took the lock and ran with nothing to carry, unseen by
that `koko disconnect`, since it was not yet listening. On Windows it held
its lock open, and `TestRealOpenSSHOnWindows` could not remove its
directory: the Git for Windows block starts two Device Muxes at once and
disconnects within seconds. Wait only for a holder that is going.
**Seen:** 1 (2026-11-12)
**Evidence:** check run 43160373344 on cd22008; #181

## Moved to the code standards

- Wait for the event itself, not for one that comes just before it: [Tests](docs/code-standards.md#tests) (seen 2026-11-06, 2026-11-08, 2026-11-09, 2026-11-10, 2026-11-11; #38, #43, #49, #54, #60, #70, #90, #119, #120, #128, #147; check run 42951455550).
- A channel or connection closes only once every request on it has its answer: [What a change contains](docs/code-standards.md#what-a-change-contains) (seen 2026-11-06, 2026-11-08, 2026-11-10; #49, #59, #64, #81, #127).
- A test shortens a wait only where what it waits for never comes: [Tests](docs/code-standards.md#tests) (seen 2026-11-06, 2026-11-11, 2026-11-12; #38, #77, #147, #163, #167; check runs 42081318152 and 43128685688).
