# Local callers on the koko server act as the Owner

Anything that calls the API over the local Unix socket acts as the Owner, with
no OS-user isolation between the koko server and other local processes.
Whoever has a shell on that machine controls it already, so separate OS users
would guard against no real threat while complicating every install.

## Consequences

An agent running on the koko server has full Owner rights; its own harness's
permission rules are the guard against its mistakes. Secrets still stay out of
CLI output by default, because whatever reaches an agent's context is sent to
its model provider.
