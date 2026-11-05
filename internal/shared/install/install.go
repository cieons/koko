// Package install is what `koko server install` in the CLI and the installer
// in the koko server's code agree on: what install is asked, what it
// reports, and why it refuses. The CLI hands install to the installer
// through cli.Runner, so neither imports the other's group (ADR 0024); this
// package is the contract between them, as wire is between the CLI and the
// koko server.
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
	// Port is the TCP port the koko server listens on.
	Port int
}

// Initialise calls server.init over the koko server's Unix socket. The CLI
// gives it to the installer, which owns no API client.
type Initialise func(ctx context.Context, req api.ServerInitRequest) (api.ServerInitResponse, error)

// Report is what install did, for the CLI to print.
type Report struct {
	// Init is what initialisation set and made: the Server Address, the
	// host key's fingerprint and the Owner's first Join Code.
	Init api.ServerInitResponse
	// RootKeyFile is the Root Key's file, never the key.
	RootKeyFile string
	// RootKeyID is the id the Root Key's file names.
	RootKeyID string
	// Port is the port the koko server listens on, which the Owner opens
	// in the provider's firewall.
	Port int
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
	// Initialised: the koko server on this machine is initialised already.
	Initialised
)
