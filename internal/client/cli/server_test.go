package cli_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
)

// fakeServer records what `koko server run` hands the koko server.
type fakeServer struct {
	calls    int
	settings cli.ServerSettings
	err      error
}

func (f *fakeServer) run(_ context.Context, s cli.ServerSettings, log io.Writer) error {
	f.calls++
	f.settings = s
	_, _ = io.WriteString(log, "level=INFO msg=started\n")
	return f.err
}

// credentialsDirectory is the variable systemd sets for the koko unit, whose
// LoadCredential=root-key hands it the Root Key's file.
const credentialsDirectory = "CREDENTIALS_DIRECTORY=/run/credentials/koko.service"

func TestServerRunTakesItsSettingsFromFlagsThenEnvironment(t *testing.T) {
	t.Parallel()
	// The Root Key's file is systemd's copy of the one the koko unit loads.
	defaults := cli.ServerSettings{
		DataDir:       "/var/lib/koko",
		RootKeyPath:   "/run/credentials/koko.service/root-key",
		RootKeySource: "/etc/koko/root.key",
		Listen:        ":7822",
		LogLevel:      slog.LevelInfo,
	}
	for _, tc := range []struct {
		name string
		env  []string
		args []string
		want cli.ServerSettings
	}{
		{"defaults", []string{credentialsDirectory}, nil, defaults},
		{
			"flags", nil,
			[]string{"--data-dir", "/srv/koko", "--root-key-file", "/etc/koko/root.key", "--listen", "203.0.113.7:8443", "--log-level", "debug"},
			cli.ServerSettings{DataDir: "/srv/koko", RootKeyPath: "/etc/koko/root.key", Listen: "203.0.113.7:8443", LogLevel: slog.LevelDebug},
		},
		{
			"flags with =", nil,
			[]string{"--data-dir=/srv/koko", "--root-key-file=/etc/koko/root.key", "--listen=[::1]:8443", "--log-level=warn"},
			cli.ServerSettings{DataDir: "/srv/koko", RootKeyPath: "/etc/koko/root.key", Listen: "[::1]:8443", LogLevel: slog.LevelWarn},
		},
		{
			"environment",
			[]string{"KOKO_DATA_DIR=/env/koko", "KOKO_ROOT_KEY_FILE=/env/root.key", "KOKO_LISTEN=:8443", "KOKO_LOG_LEVEL=error"},
			nil,
			cli.ServerSettings{DataDir: "/env/koko", RootKeyPath: "/env/root.key", Listen: ":8443", LogLevel: slog.LevelError},
		},
		{
			"flag over environment",
			[]string{credentialsDirectory, "KOKO_DATA_DIR=/env/koko", "KOKO_ROOT_KEY_FILE=/env/root.key", "KOKO_LISTEN=:8443"},
			[]string{"--data-dir", "/srv/koko", "--root-key-file", "/etc/koko/root.key", "--listen", ":9443"},
			cli.ServerSettings{DataDir: "/srv/koko", RootKeyPath: "/etc/koko/root.key", Listen: ":9443", LogLevel: slog.LevelInfo},
		},
		{
			"the Owner and the Server Address as flags",
			[]string{credentialsDirectory},
			[]string{"--owner", "alice", "--address", "koko.example.com:443"},
			withInit(defaults, "alice", "koko.example.com:443"),
		},
		{
			"the Owner and the Server Address in the environment",
			[]string{credentialsDirectory, "KOKO_OWNER=alice", "KOKO_ADDRESS=[2001:db8::7]"},
			nil,
			withInit(defaults, "alice", "[2001:db8::7]"),
		},
		{
			"the Owner and the Server Address, flag over environment",
			[]string{credentialsDirectory, "KOKO_OWNER=bob", "KOKO_ADDRESS=env.example.com"},
			[]string{"--owner", "alice", "--address", "koko.example.com"},
			withInit(defaults, "alice", "koko.example.com"),
		},
		{
			"empty variable is unset",
			[]string{credentialsDirectory, "KOKO_DATA_DIR=", "KOKO_ROOT_KEY_FILE=", "KOKO_LISTEN="},
			nil,
			defaults,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeServer{}
			r := harness.Run(t, cli.Runner{ServerRun: f.run}, tc.env, append([]string{"server", "run"}, tc.args...)...)
			if r.Status != 0 || r.Stdout != "" {
				t.Fatalf("status %d, stdout %q, stderr %q", r.Status, r.Stdout, r.Stderr)
			}
			if f.calls != 1 || f.settings != tc.want {
				t.Errorf("koko server ran %d time(s) with %+v, want once with %+v", f.calls, f.settings, tc.want)
			}
			if r.Stderr != "level=INFO msg=started\n" {
				t.Errorf("the koko server's log did not reach stderr: %q", r.Stderr)
			}
		})
	}
}

// withInit is s with the Owner and the Server Address a fresh koko server
// initialises itself with.
func withInit(s cli.ServerSettings, owner, address string) cli.ServerSettings {
	s.Owner, s.Address = owner, address
	return s
}

func TestServerRunRefusesBadSettings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		env     []string
		args    []string
		message string
	}{
		{"unknown level", nil, []string{"--log-level", "verbose"}, `--log-level must be one of debug, info, warn, error, not "verbose"`},
		{"unknown level in the environment", []string{"KOKO_LOG_LEVEL=verbose"}, nil, `KOKO_LOG_LEVEL must be one of debug, info, warn, error, not "verbose"`},
		{"empty data directory", nil, []string{"--data-dir="}, "--data-dir needs a value; it cannot be empty"},
		{"data directory without a value", nil, []string{"--data-dir"}, "--data-dir needs a value"},
		{"data directory followed by a flag", nil, []string{"--data-dir", "--json"}, "--data-dir needs a value"},
		{"data directory twice", nil, []string{"--data-dir", "/a", "--data-dir", "/b"}, "--data-dir is given twice"},
		{"empty Root Key file", nil, []string{"--root-key-file="}, "--root-key-file needs a value; it cannot be empty"},
		{"listen address without a port", []string{credentialsDirectory}, []string{"--listen", "0.0.0.0"}, `--listen (KOKO_LISTEN) takes host:port or :port, not "0.0.0.0"`},
		{"listen port out of range", []string{credentialsDirectory}, []string{"--listen", ":65536"}, `--listen (KOKO_LISTEN) takes host:port or :port, not ":65536"`},
		{"listen port by name", []string{credentialsDirectory, "KOKO_LISTEN=:https"}, nil, `--listen (KOKO_LISTEN) takes host:port or :port, not ":https"`},
		{"Owner without Server Address", []string{credentialsDirectory}, []string{"--owner", "alice"}, "--owner (KOKO_OWNER) needs --address (KOKO_ADDRESS)"},
		{"Owner without Server Address in the environment", []string{credentialsDirectory, "KOKO_OWNER=alice"}, nil, "--owner (KOKO_OWNER) needs --address (KOKO_ADDRESS)"},
		{"Server Address without Owner", []string{credentialsDirectory}, []string{"--address", "koko.example.com"}, "--address (KOKO_ADDRESS) needs --owner (KOKO_OWNER)"},
		{"Server Address without Owner in the environment", []string{credentialsDirectory, "KOKO_ADDRESS=koko.example.com"}, nil, "--address (KOKO_ADDRESS) needs --owner (KOKO_OWNER)"},
		{"Owner that is not a User name", []string{credentialsDirectory}, []string{"--owner", "Alice", "--address", "koko.example.com"}, "--owner (KOKO_OWNER) takes the Owner's User name"},
		{"Server Address that is not one", []string{credentialsDirectory, "KOKO_OWNER=alice", "KOKO_ADDRESS=koko.example.com:0"}, nil, "--address (KOKO_ADDRESS) takes the Server Address"},
		{
			"no Root Key file outside systemd", nil, nil,
			"give the Root Key's file with --root-key-file or KOKO_ROOT_KEY_FILE: its default, $CREDENTIALS_DIRECTORY/root-key, needs CREDENTIALS_DIRECTORY",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeServer{}
			r := harness.Run(t, cli.Runner{ServerRun: f.run}, tc.env, append([]string{"server", "run", "--json"}, tc.args...)...)
			msg := r.WantError(t, 2, "invalid_usage")
			if !strings.Contains(msg, tc.message) {
				t.Errorf("message %q does not say %q", msg, tc.message)
			}
			if f.calls != 0 {
				t.Error("the koko server ran despite the refusal")
			}
		})
	}
}

// A build without the koko server (every OS but Linux) refuses to run it.
func TestServerRunWhereTheBuildHasNoServer(t *testing.T) {
	t.Parallel()
	r := harness.Run(t, cli.Runner{}, nil, "server", "run", "--json")
	msg := r.WantError(t, 1, "unsupported_platform")
	if !strings.Contains(msg, "Linux only") {
		t.Errorf("message %q does not say the koko server runs on Linux only", msg)
	}
}

func TestServerRunFailureExitsOne(t *testing.T) {
	t.Parallel()
	f := &fakeServer{err: errors.New("listen on /run/koko/koko.sock: permission denied")}
	r := harness.Run(t, cli.Runner{ServerRun: f.run}, []string{credentialsDirectory}, "server", "run")
	if r.Status != 1 || r.Stdout != "" || !strings.HasSuffix(r.Stderr, "koko: listen on /run/koko/koko.sock: permission denied\n") {
		t.Errorf("status %d, stdout %q, stderr %q", r.Status, r.Stdout, r.Stderr)
	}
}
