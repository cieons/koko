// Package install is what `koko server install` and `koko server uninstall`
// in the CLI and the installer in the koko server's code agree on: what
// install is asked, what each reports, and why install refuses. The CLI
// hands both to the installer through cli.Runner, so neither imports the
// other's group (ADR 0024); this package is the contract between them, as
// wire is between the CLI and the koko server.
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

// Refusal is install refusing, before it writes anything, for a reason the
// CLI reports with an error code of its own.
type Refusal struct {
	Reason  Reason
	Message string
}

func (r *Refusal) Error() string {
	return r.Message
}

// Reason is why install refused.
type Reason int

const (
	// Usage: the command line does not fit this machine, such as no
	// --owner for a koko server not initialised yet. Fix it and run
	// install again.
	Usage Reason = iota + 1
	// PortTaken: another process listens on the port.
	PortTaken
	// Unsupported: this machine cannot run the koko server, such as one
	// with systemd older than 247.
	Unsupported
	// Initialised: the koko server on this machine is initialised already,
	// so install upgrades it, which takes neither --owner nor --address.
	Initialised
)
