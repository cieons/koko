# koko does not depend on an overlay network

koko reaches Hosts at their ordinary addresses, directly or through a Relay,
and is itself reached over SSH on one ordinary port (ADR 0012). Not every Host
can join Tailscale or a similar network, so koko cannot assume one; supporting
one later would add a way to reach a Host, not a requirement.
