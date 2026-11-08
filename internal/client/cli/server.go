package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"

	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/install"
	"github.com/cieons/koko/internal/shared/name"
	"github.com/cieons/koko/internal/shared/serveraddr"
)

// rootKeyCredential is the Root Key's credential in the koko unit,
// LoadCredential=root-key:/etc/koko/root.key, which systemd hands the service
// as a file of that name in $CREDENTIALS_DIRECTORY (P0 spec: install and run).
const rootKeyCredential = "root-key"

// rootKeySource is the file the koko unit's LoadCredential= hands the
// service as rootKeyCredential, which install writes: the Root Key's file
// as the Owner keeps it, which koko server show names.
const rootKeySource = "/etc/koko/root.key"

// defaultListenPort is the port koko server run listens on unless --listen
// names another; the unit install writes names its own (P0 spec: install
// and run).
const defaultListenPort = "443"

// defaultRootKeyFile is --root-key-file's default, as help shows it. Only this
// value is expanded: a path given is taken as it is, even one holding '$'.
const defaultRootKeyFile = "$CREDENTIALS_DIRECTORY/" + rootKeyCredential

var serverCommands = []Command{
	{
		Noun:    "server",
		Verb:    "run",
		Summary: "Run the koko server in the foreground, as its systemd unit does",
		Flags: []Flag{
			{
				Name:    "data-dir",
				Value:   "dir",
				Env:     "KOKO_DATA_DIR",
				Default: "/var/lib/koko",
				Summary: "the data directory, which holds koko.db",
			},
			{
				Name:    "root-key-file",
				Value:   "file",
				Env:     "KOKO_ROOT_KEY_FILE",
				Default: defaultRootKeyFile,
				Summary: "the Root Key's file, outside the data directory",
			},
			{
				Name:    "listen",
				Value:   "addr",
				Env:     "KOKO_LISTEN",
				Default: ":" + defaultListenPort,
				Summary: "where devices reach the koko server over TCP, once it is initialised",
			},
			{
				Name:    "log-level",
				Value:   "level",
				Env:     "KOKO_LOG_LEVEL",
				Default: "info",
				Choices: []string{"debug", "info", "warn", "error"},
				Summary: "the lowest level the log on stderr holds",
			},
		},
		// It runs the koko server and migrates its database (P0 spec: overview).
		Tier: Irreversible,
		Run:  quietAction(serverRun),
	},
	{
		Noun:    "server",
		Verb:    "install",
		Summary: "Install, start and initialise the koko server on this Linux machine, as root, or upgrade the one here",
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
				Summary: "the TCP port the koko server listens on: 443 on a fresh install, and on an upgrade the port it listens on now; install never moves to another by itself",
			},
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
		Summary: "Stop the koko server on this Linux machine and remove its unit and binary, as root; its data, Root Key and system user stay",
		// It stops the koko server and removes it from the machine (P0
		// spec: install and run).
		Tier: Irreversible,
		Run:  action(serverUninstall),
	},
	{
		Noun:    "server",
		Verb:    "show",
		Summary: "Show the koko server's Server Address, port, host key, version, Root Key file and data directory",
		Tier:    Read,
		Op:      api.ServerShow,
		Run:     action(serverShow),
	},
	{
		Noun:    "server",
		Verb:    "set",
		Summary: "Change the Server Address that Join Codes issued from now on carry, on the koko server's machine",
		Pairs:   true,
		Tier:    Reversible,
		// Only the Owner, as the Local caller on the koko server's machine,
		// may run it: the koko server answers it only over its Unix socket.
		OwnerOnly: true,
		Op:        api.ServerSet,
		Run:       action(serverSet),
	},
}

// noServer is the refusal of a command that runs the koko server's own code
// in a build that does not carry it: the koko server runs on Linux only
// (ADR 0024).
func noServer() error {
	return &cliError{code: codeUnsupportedPlatform, message: fmt.Sprintf(
		"the koko server runs on Linux only; this koko is built for %s/%s", runtime.GOOS, runtime.GOARCH)}
}

func serverRun(ctx context.Context, in *Input) error {
	if in.runner.ServerRun == nil {
		return noServer()
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(in.Flag("log-level"))); err != nil {
		return fmt.Errorf("read the log level: %w", err)
	}
	rootKey, source := in.Flag("root-key-file"), ""
	if rootKey == defaultRootKeyFile {
		dir := in.inv.getenv("CREDENTIALS_DIRECTORY")
		if dir == "" {
			return usagef("give the Root Key's file with --root-key-file or KOKO_ROOT_KEY_FILE: its default, %s, needs CREDENTIALS_DIRECTORY, which systemd sets for the koko unit", defaultRootKeyFile)
		}
		rootKey, source = filepath.Join(dir, rootKeyCredential), rootKeySource
	}
	listen := in.Flag("listen")
	if !isListenAddr(listen) {
		return usagef("--listen (KOKO_LISTEN) takes host:port or :port, not %q", listen)
	}
	return in.runner.ServerRun(ctx, ServerSettings{
		DataDir:       in.Flag("data-dir"),
		RootKeyPath:   rootKey,
		RootKeySource: source,
		Listen:        listen,
		LogLevel:      level,
	}, in.inv.Stderr)
}

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

// serverInstall is koko server install: it checks its flags, then hands the
// machine's part to the installer in the koko server's code, giving it the
// one API call it makes, server.init over the Unix socket.
func serverInstall(ctx context.Context, in *Input) (installedJSON, error) {
	if in.runner.ServerInstall == nil {
		return installedJSON{}, noServer()
	}
	req, err := installRequest(in)
	if err != nil {
		return installedJSON{}, err
	}
	initialise := func(ctx context.Context, r api.ServerInitRequest) (api.ServerInitResponse, error) {
		return in.Call(ctx, api.ServerInit, r)
	}
	report, err := in.runner.ServerInstall(ctx, req, initialise)
	if err != nil {
		return installedJSON{}, installError(err)
	}
	out := installedJSON{
		Upgraded:           report.Upgraded,
		Address:            report.Address,
		HostKeyFingerprint: report.HostKeyFingerprint,
		RootKeyFile:        report.RootKeyFile,
		RootKeyID:          report.RootKeyID,
		ListenPort:         report.Port,
		DevicesMustConnect: report.DevicesMustConnect,
	}
	if !report.Upgraded {
		out.JoinCode = joinCodeFromAPI(report.JoinCode)
	}
	return out, nil
}

// installRequest reads install's flags, refusing a value that could never
// work before the installer touches the machine. A port not given is 0,
// which the installer reads as 443 on a fresh machine and as the port the
// koko server listens on now on an upgrade.
func installRequest(in *Input) (install.Request, error) {
	port := 0
	if p := in.Flag("port"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return install.Request{}, usagef("--port takes a TCP port from 1 to 65535, not %q", p)
		}
		port = n
	}
	owner, address := in.Flag("owner"), in.Flag("address")
	if owner != "" {
		if err := name.Check(owner); err != nil {
			return install.Request{}, usagef("--owner takes the Owner's User name: %v", err)
		}
	}
	if address != "" {
		if err := serveraddr.Check(address); err != nil {
			return install.Request{}, usagef("--address takes the Server Address, host or host:port: %v", err)
		}
	}
	return install.Request{Owner: owner, Address: address, Port: port}, nil
}

// installError gives the installer's refusals the CLI's codes and exit
// statuses.
func installError(err error) error {
	r, ok := errors.AsType[*install.Refusal](err)
	if !ok {
		return err
	}
	switch r.Reason {
	case install.Usage:
		return &usageError{message: r.Message}
	case install.PortTaken:
		return &cliError{code: codePortTaken, message: r.Message}
	case install.Unsupported:
		return &cliError{code: codeUnsupportedPlatform, message: r.Message}
	case install.Initialised:
		return api.CodeAlreadyInitialised.Errorf("%s", r.Message)
	}
	return err
}

// isListenAddr tells whether addr is a TCP listen address: an optional host
// and a port number, as :443 or 203.0.113.7:443.
func isListenAddr(addr string) bool {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	n, err := strconv.ParseUint(port, 10, 16)
	return err == nil && strconv.FormatUint(n, 10) == port
}

// uninstalledJSON is what koko server uninstall prints: what it removed, and
// where what it kept is.
type uninstalledJSON struct {
	// Removed are the files uninstall removed, the unit and the binary;
	// empty when neither was there.
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
// install koko again on it.
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
	again := "To run the koko server again on them, install koko again without --owner"
	if r.ListenPort != 0 {
		again += fmt.Sprintf(", with --port %d", r.ListenPort)
	}
	b.WriteString("\n" + again + ".\n")
	_, err := io.WriteString(w, b.String())
	return err
}

// serverUninstall is koko server uninstall: it hands the machine's part to
// the installer in the koko server's code.
func serverUninstall(ctx context.Context, in *Input) (uninstalledJSON, error) {
	if in.runner.ServerUninstall == nil {
		return uninstalledJSON{}, noServer()
	}
	report, err := in.runner.ServerUninstall(ctx)
	if err != nil {
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
