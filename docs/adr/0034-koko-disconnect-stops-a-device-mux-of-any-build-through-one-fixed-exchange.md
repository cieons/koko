# koko disconnect stops a Device Mux of any build through one fixed exchange

`koko disconnect` stops every Device Mux of the OS user, of every build, and
says how many commands each carried, which it ended. Each CLI talks only to
the Device Mux of its own build, so the local protocol has no compatibility
rules (ADR 0017); this one exchange is the exception, answered the same way
by every build: `koko disconnect` connects to each socket in koko's runtime
directory and opens SSH with the user name `disconnect`, which no home is,
and the Device Mux answers in the authentication banner with
`{"state": "stopped", "commands": <n>}`, refuses the authentication, and
stops. Before it closes each command's connection it sends it
`disconnected@koko`, so an SSH Session it carried ends for good rather than
Resume.

## Considered Options

- **SIGTERM to each Device Mux's process**, found by the peer credentials of
  its socket (`SO_PEERCRED`, `LOCAL_PEERPID`): no exchange to keep, and a
  Device Mux already stops cleanly on SIGTERM. But a signal carries no
  answer, so `koko disconnect` could not say what it ended, and it needs a
  second per-OS file.
- **A request in each build's own local protocol**: no rule across builds,
  but `koko disconnect` would stop only its own build's Device Mux, while an
  upgrade leaves the old one running until it idles out.
- **A file the Device Mux writes when it stops**, holding its count, read
  once its lock is free: a file format across builds instead of an
  exchange, and a wait on the lock for every Device Mux.

## Consequences

- The user name `disconnect`, the banner's `state` and `commands`, and the
  `disconnected@koko` request never change. A later build may add fields to
  the banner; every other part of the local protocol stays free to change.
- Only the same OS user can stop a Device Mux: the peer-credential check
  runs before the banner.
- A Device Mux that does not answer, such as the socket a crashed one left,
  is passed over; one that answers anything else, as a build from before
  this exchange does, fails `koko disconnect` once it has asked every other,
  naming that socket and saying what it stopped.
- The count is of the commands it carried when it stopped, those past the
  authentication: SSH Sessions, `koko web` runs and calls in flight alike,
  since the Device Mux carries their channels without reading them. A
  command still connecting gets no state, and starts a new Device Mux.
