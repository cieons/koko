// Command kokod is the koko server's program, on Linux, as koko is the CLI
// (ADR 0040): systemd and the image run it as kokod run. Its command line is
// internal/server/kokod's. Other OSes build a stub that refuses, so that
// make cross and the per-OS lint cover this package too.
package main
