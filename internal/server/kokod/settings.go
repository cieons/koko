package kokod

// The koko server's start-up settings as a command line: the flags of kokod
// run, each with its KOKO_* variable, and the checks that turn their values
// into settings.

import (
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"strconv"

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

// defaultListenPort is the port the koko server listens on unless --listen
// names another; the unit install writes names its own (P0 spec: install
// and run, ADR 0036).
const defaultListenPort = "7822"

// defaultRootKeyFile is --root-key-file's default, as help shows it. Only this
// value is expanded: a path given is taken as it is, even one holding '$'.
const defaultRootKeyFile = "$CREDENTIALS_DIRECTORY/" + rootKeyCredential

// flag is one of a command's flags, as help shows it.
type flag struct {
	// Name is the flag without its dashes, such as "data-dir".
	Name string
	// Value names the value in help, such as "dir".
	Value string
	// Env is the flag's KOKO_* environment variable, used when the flag is
	// not given; an empty variable counts as not set.
	Env string
	// Default is the value when neither the flag nor Env is given, or empty.
	Default string
	// Choices, when set, are the only values accepted.
	Choices []string
	// Summary says in one line what the flag does.
	Summary string
}

// runFlags are kokod run's flags, the start-up settings, in the order help
// lists them. Each takes a value, never an empty one, and is given at most
// once.
var runFlags = []flag{
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
		Summary: "the Root Key's file, outside the data directory; a fresh koko server given --owner makes it when it is missing",
	},
	{
		Name:    "owner",
		Value:   "user",
		Env:     "KOKO_OWNER",
		Summary: "the Owner's User name, with which a fresh koko server initialises itself at start; an initialised one does not use it",
	},
	{
		Name:    "address",
		Value:   "host[:port]",
		Env:     "KOKO_ADDRESS",
		Summary: "the Server Address devices connect to, with which a fresh koko server initialises itself at start; given with --owner, and not used once initialised",
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
}

// settings are what the koko server starts with, from runFlags.
type settings struct {
	DataDir string
	// RootKeyPath is the Root Key's file, never the key itself.
	RootKeyPath string
	// RootKeySource is the file RootKeyPath is a copy of, when it is the
	// credential systemd hands the koko unit: the Root Key's file as the
	// Owner keeps it, which koko server show names. Empty when RootKeyPath
	// is that file itself.
	RootKeySource string
	// Owner and Address are the Owner's User name and the Server Address
	// that a fresh koko server initialises itself with at start; both
	// empty, it waits for server.init over its socket.
	Owner   string
	Address string
	// Listen is the TCP listen address, such as :7822.
	Listen   string
	LogLevel slog.Level
}

// readSettings makes the settings of runFlags' values, refusing with an
// install.Refusal for install.Usage a value that could never work, before
// the koko server writes anything. value returns a flag's value, from the
// command line, its variable or its default, already checked against its
// Choices and never empty when given; getenv reads CREDENTIALS_DIRECTORY.
func readSettings(value func(name string) string, getenv func(key string) string) (settings, error) {
	owner, address, err := ownerAndAddress(value("owner"), value("address"))
	if err != nil {
		return settings{}, err
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(value("log-level"))); err != nil {
		return settings{}, fmt.Errorf("read the log level: %w", err)
	}
	rootKey, source := value("root-key-file"), ""
	if rootKey == defaultRootKeyFile {
		dir := getenv("CREDENTIALS_DIRECTORY")
		if dir == "" {
			return settings{}, usagef("give the Root Key's file with --root-key-file or KOKO_ROOT_KEY_FILE: its default, %s, needs CREDENTIALS_DIRECTORY, which systemd sets for the koko unit", defaultRootKeyFile)
		}
		rootKey, source = filepath.Join(dir, rootKeyCredential), rootKeySource
	}
	listen := value("listen")
	if !isListenAddr(listen) {
		return settings{}, usagef("--listen (KOKO_LISTEN) takes host:port or :port, not %q", listen)
	}
	return settings{
		DataDir:       value("data-dir"),
		RootKeyPath:   rootKey,
		RootKeySource: source,
		Owner:         owner,
		Address:       address,
		Listen:        listen,
		LogLevel:      level,
	}, nil
}

// ownerAndAddress checks --owner and --address, which a fresh koko server
// initialises itself with, refusing a name that is not a User name, an
// address that is not a Server Address, and either one without the other,
// before the koko server writes anything (P1 spec: the container image).
func ownerAndAddress(owner, address string) (string, string, error) {
	switch {
	case owner == "" && address == "":
		return "", "", nil
	case address == "":
		return "", "", usagef("--owner (KOKO_OWNER) needs --address (KOKO_ADDRESS): a fresh koko server initialises itself with both, and devices connect to the Server Address")
	case owner == "":
		return "", "", usagef("--address (KOKO_ADDRESS) needs --owner (KOKO_OWNER): a fresh koko server initialises itself with both")
	}
	if err := name.Check(owner); err != nil {
		return "", "", usagef("--owner (KOKO_OWNER) takes the Owner's User name: %v", err)
	}
	if err := serveraddr.Check(address); err != nil {
		return "", "", usagef("--address (KOKO_ADDRESS) takes the Server Address, host or host:port: %v", err)
	}
	return owner, address, nil
}

// isListenAddr tells whether addr is a TCP listen address: an optional host
// and a port number, as :7822 or 203.0.113.7:7822.
func isListenAddr(addr string) bool {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	n, err := strconv.ParseUint(port, 10, 16)
	return err == nil && strconv.FormatUint(n, 10) == port
}
