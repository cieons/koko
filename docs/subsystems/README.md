# Subsystem references

Each page describes one subsystem as it is now: responsibilities and
boundaries, source of truth, runtime paths, state transitions, constraints,
failure behaviour, and how to verify it. Why a design was chosen lives in an
ADR; a proposed change lives in its issue or PR.

| Subsystem | Reference |
| --- | --- |
| _None yet._ | |

## Maintaining a reference

Update the owning reference in the same change when that change alters a
documented responsibility, interface, state transition, runtime path,
compatibility rule or failure behaviour. A refactor that keeps these
contracts needs no prose change.

Keep each fact in one reference and link to it from elsewhere. Link source
files and named symbols rather than copying implementation detail that
changes often.
