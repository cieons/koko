# Code standards

Every change is written and reviewed against these rules. They grow out of
review: a lesson in [LESSONS.md](../LESSONS.md) seen on three separate days
moves here.

## What a change contains

| Rule | What a reviewer checks |
| --- | --- |
| A change does one goal. | Every change in behaviour serves that goal. A refactor, a second fix or a fix found on the way goes in a commit of its own. |
| A change holds on the whole path. | Follow it from the CLI or API input to the end effect: callers, stored state, files written, effects on remote hosts. One correct function doesn't make the operation correct. |
| A fix reaches every sibling. | Grep for the same pattern across subcommands, API handlers, transports and access modes, and fix each one in the same change. |
| A failed read or parse means "unknown". | It never means empty, absent or equal. A file that doesn't parse is copied aside before koko writes over it. |
| Secrets stay out of output. | Secrets, the koko key, a device's private key, Join Codes, and `koko web` tokens and cookies appear in no CLI output, log line, error message or test fixture, except from a command whose purpose is to show one. |
| A channel or connection closes only once every request on it has its answer. | What passes calls on, such as the in-process Host, the Gateway, the Device Mux and `koko web`'s forwarder, closes its side only once it has passed on what the other side sent before its end, bounded for a reader that does not read. An answer sent after the close is dropped, and the side that asked sees `EOF`; merged work did this on three separate days ([LESSONS.md](../LESSONS.md#moved-to-the-code-standards)). |
| koko writes only files it owns. | On a user's machine koko writes its own files (an include target, its skill file, its state) and joins them to the user's files with one include line. The user's own content stays byte-for-byte. |

## Tests

| Rule | What a reviewer checks |
| --- | --- |
| A fix comes with a test that fails without it. | Put the old code back and run the test: it fails with a `FAIL` that names the behaviour. A compile error or a skip doesn't count. |
| Tests stay off the real machine. | `make test` runs under a temporary HOME. A test reaches no real `~/.ssh`, `~/.claude`, OS keychain or server. |
| Tests stay off the network. | A remote host is an in-process `golang.org/x/crypto/ssh` server; an HTTP peer is `httptest`. |
| Tests check behaviour at the highest seam. | Prefer the API service layer, and the CLI driven against an in-process server. A refactor that keeps behaviour keeps the tests green. |
| Whatever a test starts ends with it. | Servers, goroutines and processes a test starts are stopped through `t.Cleanup` before it returns. |
| A test waits for the event itself, not for one that comes just before it. | Name what the test waits for before it acts or asserts: the outcome itself, such as a socket that accepts rather than its file, a countdown's interval running rather than its line shown, a page rendered rather than a fixed second for its chunk to load, or the focus moved rather than the step or dialog that moves it shown. A loaded runner stretches the gap between the two, as it did in merged work on four separate days ([LESSONS.md](../LESSONS.md#moved-to-the-code-standards)). |
| A red test on `main` is a bug now. | Fix it at its cause, or open an issue naming the cause. "Fails the same on main" is not a baseline. |

## Acceptance checks

A change is committed only after `make check` passes. The commit body says
what ran, and names the test that fails without the change. A check that
didn't run is listed as not run.

## Go

- Errors carry what was being done: `fmt.Errorf("load host %q: %w", name, err)`.
- A comment says what the code is for, in a sentence or two. Name functions
  rather than line numbers, which go stale.
- Packages live under `internal/` until one is deliberately made public.
- A new third-party dependency needs a reason in the commit body; the
  standard library and `golang.org/x` come first.

## Commits

A commit follows [Conventional Commits](https://www.conventionalcommits.org/):
one short subject line, then a blank line and a body when the change needs
one.

```
fix(gateway): end the open Tunnel when its Device Session is revoked (#12)

A revoked Device Session ended the next Tunnel it opened rather than the
one already open, so ...

Verified: TestRevokeEndsTheOpenTunnel fails without the fix.
```

- **Type.** Use one of:
  - `feat`: behaviour a person or an agent can use;
  - `fix`: a bug;
  - `docs`, `test`;
  - `refactor`: no change in behaviour;
  - `perf`, `build`, `ci`, `chore`.

  A change that breaks compatibility adds `!` after the scope and a
  `BREAKING CHANGE:` footer.
- **Scope.** The area, the subsystem: `cli`, `api`, `gateway`, `tunnel`,
  `enroll`, `space`, `secret`, `server`, `web`, `install`, `release`.
- **Subject.** Imperative, lower case after the colon, and no full stop.
  It ends with the issue, `(#12)`, and is at most 72 characters with it.
  It says what changes, not how.
- **Body.** Wrapped at 72 columns. It covers:
  - what changed and why, and the decisions taken;
  - how the change was verified, and which test fails without it;
  - for user-facing strings, each listed as English | Chinese.
