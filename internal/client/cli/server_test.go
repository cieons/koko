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
