// Package install is what `koko server install` and `koko server uninstall`
// in the CLI and `kokod install` and `kokod uninstall` in the koko server
// agree on (ADR 0041): what install is asked, what each reports, as the JSON
// kokod prints for koko to read, the koko server's paths and platforms both
// check, and why install, or the koko server's start-up, refuses, with the
// error codes koko and kokod both report a refusal with. koko runs only a
// kokod of its own build, so this contract has no compatibility rules
// between versions: it is the one both are built from.
package install

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/name"
	"github.com/cieons/koko/internal/shared/serveraddr"
)

// The koko server's program on its machine, as koko looks for it and kokod
// install puts it there.
const (
	// KokodPath is where kokod install puts kokod, which the unit runs, and
	// where koko server install looks for the kokod of its own build.
	KokodPath = "/usr/local/bin/kokod"
	// KokodPackage is kokod's main package, which a kokod file's Go build
	// information names, so that koko tells kokod from another program,
	// such as koko itself, before it runs it.
	KokodPackage = "github.com/cieons/koko/cmd/kokod"
)

// KokodPlatforms are the platforms a release has a kokod for, as GOOS/GOARCH
// name them: the koko server runs on Linux only (ADR 0040).
var KokodPlatforms = []string{"linux/amd64", "linux/arm64"}

// The image runs the koko server where systemd does not, as on macOS or in
// a container, as ContainerDocs shows.
const (
	ContainerImage = "ghcr.io/cieons/koko"
	ContainerDocs  = "https://github.com/cieons/koko/blob/main/docs/install.md#in-a-container"
)

// SystemdRunDir is there only while systemd runs the machine, as
// sd_booted(3) tells.
const SystemdRunDir = "/run/systemd/system"

// NoSystemd is the refusal of a machine that systemd does not run, as in a
// container whose init is something else, where systemctl may still be
// there and would fail only after the writes. Both koko server install,
// before it gets kokod, and kokod install refuse with it.
func NoSystemd() *Refusal {
	return Refuse(Unsupported, "the koko server runs only on Linux with systemd, and systemd is not running this machine (there is no %s): to run it in a container, use the image %s (%s)",
		SystemdRunDir, ContainerImage, ContainerDocs)
}

// Request is what `koko server install` is asked, from its flags, and what
// it asks `kokod install`.
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

// ParseRequest reads install's flags, each "" when not given, refusing with
// Usage a value that could never work, before anything asks the machine. A
// port not given is 0.
func ParseRequest(owner, address, port string) (Request, error) {
	req := Request{Owner: owner, Address: address}
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return Request{}, usagef("--port takes a TCP port from 1 to 65535, not %q", port)
		}
		req.Port = n
	}
	if owner != "" {
		if err := name.Check(owner); err != nil {
			return Request{}, usagef("--owner takes the Owner's User name: %v", err)
		}
	}
	if address != "" {
		if err := serveraddr.Check(address); err != nil {
			return Request{}, usagef("--address takes the Server Address, host or host:port: %v", err)
		}
	}
	return req, nil
}

// usagef is a refusal of a flag's value, which exits 2.
func usagef(format string, args ...any) *Refusal {
	return &Refusal{Reason: Usage, Message: fmt.Sprintf(format, args...)}
}

// Flags are r as install's flags, those given only, in the order help lists
// them: what koko server install hands kokod install, and what a person
// gives koko server install again.
func (r Request) Flags() []string {
	var flags []string
	if r.Owner != "" {
		flags = append(flags, "--owner", r.Owner)
	}
	if r.Address != "" {
		flags = append(flags, "--address", r.Address)
	}
	if r.Port != 0 {
		flags = append(flags, "--port", strconv.Itoa(r.Port))
	}
	return flags
}

// Report is what kokod install did, as it prints it for koko server install,
// which initialises a fresh koko server and prints the outcome.
type Report struct {
	// Initialise is what a koko server not initialised yet is to be
	// initialised with, over its Unix socket, once it runs; nil on an
	// upgrade, of a koko server initialised already.
	Initialise *Initialise `json:"initialise,omitempty"`
	// Address is an upgraded koko server's Server Address, as stored:
	// without a port, its port is Port. Empty before initialisation.
	Address string `json:"address,omitempty"`
	// HostKeyFingerprint is an upgraded koko server's host key, as OpenSSH
	// prints its SHA-256 fingerprint. Empty before initialisation, which
	// makes the host key.
	HostKeyFingerprint string `json:"host_key_fingerprint,omitempty"`
	// RootKeyFile is the Root Key's file, never the key.
	RootKeyFile string `json:"root_key_file"`
	// RootKeyID is the id the Root Key's file names.
	RootKeyID string `json:"root_key_id"`
	// Port is the port the koko server listens on, which the Owner opens
	// in the provider's firewall.
	Port int `json:"port"`
	// DevicesMustConnect is set by an upgrade that leaves the koko server
	// listening on another port than its unit named before, or on a port
	// it cannot know was the same, when the Server Address names no port of
	// its own: it is the Server Address with the new port, which devices
	// joined before must take by hand (koko connect). Empty otherwise.
	DevicesMustConnect string `json:"devices_must_connect,omitempty"`
}

// Initialise is what initialises a fresh koko server: server.init's Owner
// and Server Address, which koko server install calls it with.
type Initialise struct {
	// Owner is the Owner's User name, --owner's.
	Owner string `json:"owner"`
	// Address is the Server Address: --address's, or else the machine's
	// public IP, which kokod install found.
	Address string `json:"address"`
	// Next is the command that initialises it, for a person who ran kokod
	// install by hand: sudo koko server install with the same flags, which
	// finds the koko server installed, runs the kokod installed and
	// initialises it.
	Next string `json:"next"`
}

// NewInitialise is what initialises the fresh koko server kokod install
// installed for req, at the Server Address address.
func NewInitialise(req Request, address string) *Initialise {
	return &Initialise{
		Owner:   req.Owner,
		Address: address,
		Next:    strings.Join(append([]string{"sudo", "koko", "server", "install"}, req.Flags()...), " "),
	}
}

// UninstallReport is what kokod uninstall did, as it prints it for koko
// server uninstall.
type UninstallReport struct {
	// Removed are the files uninstall removed, the unit and kokod, each
	// only when it was there.
	Removed []string `json:"removed"`
	// DataDir, RootKeyFile and SystemUser are what uninstall keeps: the
	// data directory, the Root Key's file and the system user the koko
	// service ran as.
	DataDir     string `json:"data_dir"`
	RootKeyFile string `json:"root_key_file"`
	SystemUser  string `json:"system_user"`
	// ListenPort is the port the koko service listened on, which install
	// takes again with --port, or 0 when that is unknown.
	ListenPort int `json:"listen_port,omitzero"`
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

// Refuse makes a Refusal with a formatted message, saying that nothing was
// written, which holds for every refusal of install's.
func Refuse(reason Reason, format string, args ...any) *Refusal {
	return &Refusal{Reason: reason, Message: fmt.Sprintf(format, args...) + "; nothing was written"}
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
