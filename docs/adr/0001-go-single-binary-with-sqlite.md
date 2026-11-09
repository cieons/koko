---
status: its one binary superseded by ADR-0040
---

# Go, one binary, embedded SQLite

koko ships as one Go binary that is the koko server, the CLI and the Gateway,
and it keeps its record in an embedded SQLite file. Go's
`golang.org/x/crypto/ssh` is the most proven library for writing both an SSH
server and an SSH client, and one self-contained binary is what makes a
self-hosted tool easy to install and upgrade.

## Considered Options

- **Rust**: `russh` is capable but less proven, and the speed Rust buys is not
  where koko spends its time; the network is.
- **A separate database server**: unneeded at the size of a personal or small
  team fleet, and one more thing to run.
