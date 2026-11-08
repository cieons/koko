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
accepts: 3 failures under load. The Devices page's countdown test, in
turn, moved its faked clock on once the Join Code line showed, though
React starts the countdown's interval in an effect it may run just after:
CI on `main` went red at 4b0d50b, which touched nothing under `web/`.
The test of killing the Device Mux with `KOKO_DIRECT=1`, in turn, armed
its cut once the Device Mux had stopped, to fall on the Tunnel's own
Device Connection, though the koko server's answer to the Device Mux's last
probe could still be on its way and spend the cut on the dead connection:
nothing Resumed in about 1 in 30 runs under load, and CI's macOS job on
`main` went red at d5e0d77, which only met the race the test had from 1e3a865.
The test of Ctrl-C ending `koko exec`, in turn, interrupted it once the
Gateway logged the SSH Session's start, though `koko exec` opens its
command's channel just after: 1 in 3 full `make check` runs got "the SSH
Session failed" instead of "interrupted".
**Seen:** 2 (2026-11-06, 2026-11-08)
**Evidence:** 3cae7b1 (#38); 4232f09 (#43); #77; ecff11b (#49); #78;
c152887 (#54); check runs 42166495220 and 42167225724; 4f5156b (#60);
#79; check run 42436925411; 1e3a865 (#70); check run 42461764144; #87;
#90

### A channel closes only once every request on it has its answer

**Why:** an SSH answer sent after its channel closed is dropped, and the
side that asked sees `EOF` or a refusal. The in-process Host started an
exec's command before answering it, so an `echo` that ended at once closed
the channel first, and the Gateway refused the exec: CI on `main` went red
at 0ac2b3d. The Gateway, in turn, closed the client's channel as soon as the
Host's closed, while the Host's answer to the client's exec was still being
passed back: 1 in about 1,600 runs of the exec tests in a Linux container
got `EOF`. The Device Mux, likewise, closed a command's channel and
connection once a revocation ended its Device Connection, before it had
passed on the koko server's answer to the call that revoked the device: 1
in about 400 runs of revoking the device in use under load got `revoked`
instead of the answer.
**Seen:** 2 (2026-11-06, 2026-11-08)
**Evidence:** 0ac2b3d (#64); 2e5b9f4 (#59); check run 42164460501;
ecff11b (#49); #81; check run 42443377692

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

## Moved to the code standards

_None yet._
