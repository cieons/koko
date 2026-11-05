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

	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/install"
	"github.com/cieons/koko/internal/shared/name"
	"github.com/cieons/koko/internal/shared/serveraddr"
)

// rootKeyCredential is the Root Key's credential in the koko unit,
// LoadCredential=root-key:/etc/koko/root.key, which systemd hands the service
// as a file of that name in $CREDENTIALS_DIRECTORY (P0 spec: install and run).
const rootKeyCredential = "root-key"

// defaultListenPort is the port the koko server listens on unless install's
// --port names another (P0 spec: install and run).
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
		Summary: "Install, start and initialise the koko server on this Linux machine, as root",
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
				Default: defaultListenPort,
				Summary: "the TCP port the koko server listens on; install never moves to another",
			},
		},
		// It prints a credential, the first Join Code (P0 spec: install and
		// run), and changes the machine.
		Tier: Irreversible,
		Op:   api.ServerInit,
		Run:  action(serverInstall),
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
	rootKey := in.Flag("root-key-file")
	if rootKey == defaultRootKeyFile {
		dir := in.inv.getenv("CREDENTIALS_DIRECTORY")
		if dir == "" {
			return usagef("give the Root Key's file with --root-key-file or KOKO_ROOT_KEY_FILE: its default, %s, needs CREDENTIALS_DIRECTORY, which systemd sets for the koko unit", defaultRootKeyFile)
		}
		rootKey = filepath.Join(dir, rootKeyCredential)
	}
	listen := in.Flag("listen")
	if !isListenAddr(listen) {
		return usagef("--listen (KOKO_LISTEN) takes host:port or :port, not %q", listen)
	}
	return in.runner.ServerRun(ctx, ServerSettings{
		DataDir:     in.Flag("data-dir"),
		RootKeyPath: rootKey,
		Listen:      listen,
		LogLevel:    level,
	}, in.inv.Stderr)
}

var serverMappings = []mapping{
	{api: reflect.TypeFor[api.ServerInitResponse](), cli: reflect.TypeFor[installedJSON]()},
}

// installedJSON is what koko server install prints once the koko server runs
// and is initialised: everything the Owner needs next.
type installedJSON struct {
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
	// JoinCode is the Owner's first Join Code, valid for 15 minutes.
	JoinCode joinCodeJSON `json:"join_code"`
}

func (r installedJSON) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, `The koko server is installed, running and initialised.

Server Address:  %s
Host key:        %s
Root Key:        %s (%s)
Firewall:        open TCP port %d in your provider's firewall; koko opens none itself

Keep a copy of %s off this machine, such as in a password manager:
without it, nothing sealed in the koko server's data can be opened again.

The first Join Code works once, within 15 minutes.
`, serveraddr.WithPort(r.Address, r.ListenPort), r.HostKeyFingerprint, r.RootKeyFile, r.RootKeyID, r.ListenPort, r.RootKeyFile)
	if err != nil {
		return err
	}
	return r.JoinCode.writeJoinLine(w)
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
	return installedJSON{
		Address:            report.Init.Address,
		HostKeyFingerprint: report.Init.HostKeyFingerprint,
		RootKeyFile:        report.RootKeyFile,
		RootKeyID:          report.RootKeyID,
		ListenPort:         report.Port,
		JoinCode:           joinCodeFromAPI(report.Init.JoinCode),
	}, nil
}

// installRequest reads install's flags, refusing a value that could never
// work before the installer touches the machine.
func installRequest(in *Input) (install.Request, error) {
	port, err := strconv.Atoi(in.Flag("port"))
	if err != nil || port < 1 || port > 65535 {
		return install.Request{}, usagef("--port takes a TCP port from 1 to 65535, not %q", in.Flag("port"))
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
