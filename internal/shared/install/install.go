// Package install is what `koko server install` and `koko server uninstall`
// in the CLI and the installer in the koko server's code agree on: what
// install is asked, what each reports, and why install, or the koko
// server's start-up, refuses, with the error codes koko and kokod both
// report a refusal with. The CLI hands both to the installer through
// cli.Runner, so neither imports the other's group (ADR 0040); this package
// is the contract between them, as wire is between the CLI and the koko
// server.
package install

import (
	"context"

	"github.com/cieons/koko/internal/shared/api"
)

// Request is what `koko server install` is asked, from its flags.
type Request struct {
	// Owner is the Owner's User name, for a koko server not initialised
	// yet; empty when --owner was not given.
	Owner string
	// Address is the Server Address, host or host:port (package
	// serveraddr); empty when --address was not given.
	Address string
	// Port is the TCP port the koko server listens on, or 0 when --port
	// was not given: a fresh koko server then listens on 7822, and an
	// upgrade keeps the port the koko unit names.
	Port int
}

// Initialise calls server.init over the koko server's Unix socket. The CLI
// gives it to the installer, which owns no API client.
type Initialise func(ctx context.Context, req api.ServerInitRequest) (api.ServerInitResponse, error)

// Report is what install did, for the CLI to print.
type Report struct {
	// Upgraded tells whether install found the koko server initialised
	// already and upgraded it, which issues no Join Code, rather than
	// initialising it.
	Upgraded bool
	// Address is the Server Address, as stored: without a port, its port
	// is Port.
	Address string
	// HostKeyFingerprint is the SHA-256 fingerprint of the koko server's
	// host key, as OpenSSH prints it.
	HostKeyFingerprint string
	// JoinCode is the Owner's first Join Code, which initialisation issued;
	// zero on an upgrade.
	JoinCode api.JoinCode
	// RootKeyFile is the Root Key's file, never the key.
	RootKeyFile string
	// RootKeyID is the id the Root Key's file names.
	RootKeyID string
	// Port is the port the koko server listens on, which the Owner opens
	// in the provider's firewall.
	Port int
	// DevicesMustConnect is set by an upgrade that leaves the koko server
	// listening on another port than its unit named before, or on a port
	// it cannot know was the same, when the Server Address names no port of
	// its own: it is the Server Address with the new port, which devices
	// joined before must take by hand (koko connect). Empty otherwise.
	DevicesMustConnect string
}

// UninstallReport is what uninstall did, for the CLI to print.
type UninstallReport struct {
	// Removed are the files uninstall removed, the unit and the binary,
	// each only when it was there.
	Removed []string
	// DataDir, RootKeyFile and SystemUser are what uninstall keeps: the
	// data directory, the Root Key's file and the system user the koko
	// service ran as.
	DataDir     string
	RootKeyFile string
	SystemUser  string
	// ListenPort is the port the koko service listened on, which install
	// takes again with --port, or 0 when that is unknown.
	ListenPort int
}

// Refusal is install, or the koko server's start-up, refusing before it
// writes anything, for a reason koko and kokod report with the error code
// Code names.
type Refusal struct {
	Reason  Reason
	Message string
}

func (r *Refusal) Error() string {
	return r.Message
}

// The error codes of koko's own that kokod reports too, in the shape of
// koko's --json errors ({"error": {"code", "message"}}, ADR 0016), so that
// both programs name them from here. kokod's other codes are the API's
// (package api): already_initialised and internal_error. Like the API's,
// they are only ever added within a koko major version.
const (
	// CodeInvalidUsage: the command line was refused before anything ran
	// (exit 2).
	CodeInvalidUsage = "invalid_usage"
	// CodeUnsupportedPlatform: the command does not run on this OS, or on
	// this machine, such as the koko server on a network filesystem.
	CodeUnsupportedPlatform = "unsupported_platform"
	// CodePortTaken: another process holds the port the command listens
	// on, or, at the koko server's start-up, something answers on its
	// socket.
	CodePortTaken = "port_taken"
)

// Code is the error code r is reported with: CodeInvalidUsage, which exits
// 2, or another, which exits 1.
func (r *Refusal) Code() string {
	switch r.Reason {
	case Usage:
		return CodeInvalidUsage
	case PortTaken:
		return CodePortTaken
	case Unsupported:
		return CodeUnsupportedPlatform
	case Initialised:
		return api.CodeAlreadyInitialised.Name
	}
	return api.CodeInternal.Name
}

// Reason is why install refused.
type Reason int

const (
	// Usage: the command line does not fit this machine, such as no
	// --owner for a koko server not initialised yet. Fix it and run
	// install again.
	Usage Reason = iota + 1
	// PortTaken: another process listens on the port, or, at the koko
	// server's start-up, something answers on its socket.
	PortTaken
	// Unsupported: this machine cannot run the koko server, such as one
	// with systemd older than 247, or a data directory on a network
	// filesystem.
	Unsupported
	// Initialised: the koko server on this machine is initialised already,
	// so install upgrades it, which takes neither --owner nor --address.
	Initialised
)
