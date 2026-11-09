package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/install"
	"github.com/cieons/koko/internal/shared/serveraddr"
)

var serverCommands = []Command{
	{
		Noun:    "server",
		Verb:    "install",
		Summary: "Install, start and initialise the koko server, kokod, on this Linux machine, as root, or upgrade the one here",
		Flags: []Flag{
			{
				Name:    "owner",
				Value:   "user",
				Summary: "the Owner's User name, which a koko server not initialised yet needs",
			},
			{
				Name:    "address",
				Value:   "host[:port]",
				Summary: "the Server Address devices connect to (a DNS name outlives an IP); without it, this machine's public IP",
			},
			{
				Name:    "port",
				Value:   "port",
				Summary: "the TCP port the koko server listens on: 7822 on a fresh install, and on an upgrade the port it listens on now; install never moves to another by itself",
			},
			{
				Name:    "web-listen",
				Value:   "host:port|off",
				Summary: "where the koko server serves the web UI to a reverse proxy on this machine, such as 127.0.0.1:7823, or off: off on a fresh install, and kept on an upgrade",
			},
			kokodFlag,
		},
		// It prints a credential, the first Join Code (P0 spec: install and
		// run), and changes the machine.
		Tier: Irreversible,
		Op:   api.ServerInit,
		Run:  action(serverInstall),
	},
	{
		Noun:    "server",
		Verb:    "uninstall",
		Summary: "Stop the koko server on this Linux machine and remove its unit and kokod, as root; its data, Root Key, system user and koko stay",
		Flags:   []Flag{kokodFlag},
		// It stops the koko server and removes it from the machine (P0
		// spec: install and run).
		Tier: Irreversible,
		Run:  action(serverUninstall),
	},
	{
		Noun:    "server",
		Verb:    "show",
		Summary: "Show the koko server's Server Address, port, web listener, Web URL, host key, version, Root Key file and data directory",
		Tier:    Read,
		Op:      api.ServerShow,
		Run:     action(serverShow),
	},
	{
		Noun:    "server",
		Verb:    "set",
		Summary: "Change the Server Address that Join Codes issued from now on carry, or the Web URL the web listener answers at, on the koko server's machine",
		Pairs:   true,
		Unset:   true,
		Tier:    Reversible,
		// Only the Owner, as the Local caller on the koko server's machine,
		// may run it: the koko server answers it only over its Unix socket.
		OwnerOnly: true,
		Op:        api.ServerSet,
		Run:       action(serverSet),
	},
}

// serverNote is what koko help server says beside the commands: the koko
// server's own process is another program.
const serverNote = "The koko server's process is kokod run, which systemd and the image run; koko server install installs it."

var serverMappings = []mapping{
	{api: reflect.TypeFor[api.ServerInitResponse](), cli: reflect.TypeFor[installedJSON]()},
	{api: reflect.TypeFor[api.ServerShowResponse]()},
	{api: reflect.TypeFor[api.ServerSetResponse]()},
	{
		api: reflect.TypeFor[api.Server](),
		cli: reflect.TypeFor[serverJSON](),
		omitted: map[string]string{
			"host_key": "koko server show names the host key by its fingerprint, as a Join Code pins it; koko ssh setup writes the key itself into koko's known_hosts",
		},
	},
}

// installedJSON is what koko server install prints once the koko server
// runs: on a fresh machine, initialised, everything the Owner needs next; on
// an upgrade, the same without a Join Code.
type installedJSON struct {
	// Upgraded tells whether install found the koko server initialised
	// already and upgraded it, rather than initialising it.
	Upgraded bool `json:"upgraded"`
	// Address is the Server Address as stored: without a port, its port is
	// ListenPort.
	Address string `json:"address"`
	// HostKeyFingerprint is the koko server's host key, as OpenSSH prints
	// its SHA-256 fingerprint.
	HostKeyFingerprint string `json:"host_key_fingerprint"`
	// RootKeyFile is the Root Key's file, never the key, and RootKeyID the
	// id it names, so the Owner can tell which copy fits.
	RootKeyFile string `json:"root_key_file"`
	RootKeyID   string `json:"root_key_id"`
	// ListenPort is the TCP port the koko server listens on, which the Owner
	// opens in the provider's firewall.
	ListenPort int `json:"listen_port"`
	// JoinCode is the Owner's first Join Code, valid for 15 minutes; an
	// upgrade issues none, and leaves it out.
	JoinCode joinCodeJSON `json:"join_code,omitzero"`
	// DevicesMustConnect is the address devices joined before must now take
	// by hand, with koko connect, after an upgrade moved the listen port
	// under a Server Address that names no port of its own; left out when
	// they need not.
	DevicesMustConnect string `json:"devices_must_connect,omitempty"`
}

// Human writes where the koko server is, what to open and how to ssh from
// its own machine, then on a fresh
// machine what to keep and the first Join Code, and after an upgrade that
// moved the port devices connect to, what they must do.
func (r installedJSON) Human(w io.Writer) error {
	var b strings.Builder
	if r.Upgraded {
		b.WriteString("The koko server is upgraded and running, with its record as it was.\n")
	} else {
		b.WriteString("The koko server is installed, running and initialised.\n")
	}
	fmt.Fprintf(&b, `
Server Address:  %s
Host key:        %s
Root Key:        %s (%s)
Firewall:        open TCP port %d in your provider's firewall; koko opens none itself
ssh here:        run koko ssh setup once as each user who runs ssh <host>.koko on this machine
`, serveraddr.WithPort(r.Address, r.ListenPort), r.HostKeyFingerprint, r.RootKeyFile, r.RootKeyID, r.ListenPort)
	switch {
	case !r.Upgraded:
		fmt.Fprintf(&b, `
Keep a copy of %s off this machine, such as in a password manager:
without it, nothing sealed in the koko server's data can be opened again.

The first Join Code works once, within 15 minutes.
`, r.RootKeyFile)
		_ = r.JoinCode.writeJoinLine(&b) // a strings.Builder takes every write
	case r.DevicesMustConnect != "":
		fmt.Fprintf(&b, `
Devices that joined on another port must take the new address by hand:
the Server Address names no port of its own, so it follows the listen
port, now %d. On each of them, run
koko connect %s
`, r.ListenPort, r.DevicesMustConnect)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// serverInstall is koko server install: it checks its flags and the
// machine, gets the kokod of its own build, which does the machine's part
// (kokod install), then initialises a fresh koko server over its Unix
// socket and prints what the Owner needs next.
func serverInstall(ctx context.Context, in *Input) (installedJSON, error) {
	req, err := install.ParseRequest(in.Flag("owner"), in.Flag("address"), in.Flag("port"), in.Flag("web-listen"))
	if err != nil {
		return installedJSON{}, refusalError(err)
	}
	kokod, done, err := kokodToRun(ctx, in)
	if err != nil {
		return installedJSON{}, err
	}
	defer done()
	var report install.Report
	if err := runKokod(ctx, in, kokod, append([]string{"install"}, req.Flags()...), &report); err != nil {
		return installedJSON{}, err
	}
	out := installedJSON{
		Upgraded:           report.Initialise == nil,
		Address:            report.Address,
		HostKeyFingerprint: report.HostKeyFingerprint,
		RootKeyFile:        report.RootKeyFile,
		RootKeyID:          report.RootKeyID,
		ListenPort:         report.Port,
		DevicesMustConnect: report.DevicesMustConnect,
	}
	if fresh := report.Initialise; fresh != nil {
		resp, err := in.Call(ctx, api.ServerInit, api.ServerInitRequest{Owner: fresh.Owner, Address: fresh.Address})
		if err != nil {
			return installedJSON{}, fmt.Errorf("initialise the koko server: %w", err)
		}
		out.Address, out.HostKeyFingerprint, out.JoinCode = resp.Address, resp.HostKeyFingerprint, joinCodeFromAPI(resp.JoinCode)
	}
	return out, nil
}

// refusalError gives install's refusals the codes and exit statuses kokod
// gives them too.
func refusalError(err error) error {
	r, ok := errors.AsType[*install.Refusal](err)
	if !ok {
		return err
	}
	if r.Reason == install.Usage {
		return &usageError{message: r.Message}
	}
	return &cliError{code: r.Code(), message: r.Message}
}

// uninstalledJSON is what koko server uninstall prints: what it removed, and
// where what it kept is.
type uninstalledJSON struct {
	// Removed are the files uninstall removed, the unit and kokod; empty
	// when neither was there.
	Removed []string `json:"removed"`
	// DataDir is the data directory, kept with the record in it.
	DataDir string `json:"data_dir"`
	// RootKeyFile is the Root Key's file, kept, never the key.
	RootKeyFile string `json:"root_key_file"`
	// SystemUser is the system user the koko service ran as, kept.
	SystemUser string `json:"system_user"`
	// ListenPort is the TCP port the koko server listened on, which
	// install takes again with --port; left out when unknown.
	ListenPort int `json:"listen_port,omitzero"`
}

// Human writes what uninstall removed, where what it kept is, and how to
// run the koko server again on it.
func (r uninstalledJSON) Human(w io.Writer) error {
	var b strings.Builder
	if len(r.Removed) == 0 {
		b.WriteString("No koko server is installed here: nothing to remove.\n")
	} else {
		b.WriteString("The koko server is uninstalled: its service is stopped, and these are removed:\n")
		for _, path := range r.Removed {
			fmt.Fprintf(&b, "  %s\n", path)
		}
	}
	fmt.Fprintf(&b, `
Kept, with the record sealed under the Root Key:
Data directory:  %s
Root Key:        %s
System user:     %s
`, r.DataDir, r.RootKeyFile, r.SystemUser)
	again := "To run the koko server again on them, run sudo koko server install without --owner"
	if r.ListenPort != 0 {
		again += fmt.Sprintf(", with --port %d", r.ListenPort)
	}
	b.WriteString("\n" + again + ".\n")
	_, err := io.WriteString(w, b.String())
	return err
}

// serverUninstall is koko server uninstall: it gets the kokod of its own
// build, as install does, which does the machine's part (kokod uninstall).
func serverUninstall(ctx context.Context, in *Input) (uninstalledJSON, error) {
	kokod, done, err := kokodToRun(ctx, in)
	if err != nil {
		return uninstalledJSON{}, err
	}
	defer done()
	var report install.UninstallReport
	if err := runKokod(ctx, in, kokod, []string{"uninstall"}, &report); err != nil {
		return uninstalledJSON{}, err
	}
	return uninstalledJSON{
		Removed:     report.Removed,
		DataDir:     report.DataDir,
		RootKeyFile: report.RootKeyFile,
		SystemUser:  report.SystemUser,
		ListenPort:  report.ListenPort,
	}, nil
}
