# The CLI and the koko server work together within a window of versions

A device's CLI need not match the koko server's version, the Owner's choice
of 2026-11-11. One constant, `compat.Oldest`, `v0.3.0` at first, is the
oldest koko that both programs of a build work with, in both directions: a
koko server turns away a CLI below it, and a CLI turns away a koko server
below it. It moves only at a minor release, whose notes say so.

The CLI states its version in its SSH version string, as a comment RFC 4253
allows: `SSH-2.0-koko v0.4.0`. The koko server reads it at authentication,
before any channel. A CLI that sends none is v0.3.x or older and is taken as
`v0.3.0`. `report@koko` (ADR 0027) gains `oldest`, and `revisions`: the
revision of each operation and channel that has changed, every other at 1.
An operation's revision rises with each change to what it takes; a
channel's, with each change to its open payload or, for `tunnel@koko`, to
the resumable stream's frames. A CLI never sends a request field, or a
channel revision, the koko server's report does not take: it fails with
`server_too_old`, naming the option, before anything is sent. A CLI below
the window fails every command with `cli_too_old`, by itself when it reads
`oldest`, and otherwise because the koko server answers every call and
refuses every channel with it, naming the version needed and how to
upgrade.

This refines ADR 0040, which rejected a compatibility rule between the two
programs: one module and one version stay, and across machines the window
is the rule. It refines ADR 0027 with the report's new fields. ADR 0041's
rule that `koko server install` installs the `kokod` of its own build
stays.

## Considered Options

- **A `client@koko` global request carrying the CLI's version:** encrypted,
  and in the style of `report@koko`, but x/crypto delivers global requests
  and channels on separate Go channels, so the koko server would have to
  hold every channel until the request came, and wait for ever on a CLI
  that never sends one.
- **No window, every pair of versions supported:** no constant to move, but
  the koko server would carry every revision of every channel for ever.
- **A version per operation, compared with the koko server's version:**
  the CLI would need a table of which release added each field, and
  development builds would compare wrongly.
- **Capability names instead of revision numbers:** self-describing, but a
  list that only grows, where a number says the same in one field.

## Consequences

- A revision 1 payload is today's. From revision 2 a channel's open payload
  ends with its revision, which only a koko server that reported it
  receives, since x/crypto refuses bytes left at the end of a payload.
- Golden tests list every operation's request fields with their revisions,
  and each channel's payloads and frames, so a change without a revision
  fails.
- The CLI's version goes out before encryption, as OpenSSH's does. The
  koko server's version still goes only after authentication.
- `koko status` no longer reports two versions on the koko server's machine
  as drift; it says when the koko server is newer than the CLI, or the CLI
  below the window, naming `koko update`.
- `koko server install` refuses to replace a newer `kokod`, unless
  `KOKO_VERSION` names its own version, so changing a setting with an older
  CLI never downgrades the koko server.
- CI runs real binaries against each other: this CLI against the previous
  release's kokod and `compat.Oldest`'s, and the reverse.
- A patch release never moves `compat.Oldest`.
