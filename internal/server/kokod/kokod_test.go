package kokod_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server"
	"github.com/cieons/koko/internal/server/kokod"
	"github.com/cieons/koko/internal/shared/build"
)

// settings are what kokod run hands the koko server in its config from its
// command line.
type settings struct {
	DataDir, RootKeyPath, RootKeySource, Owner, Address, Listen, WebListen string
	LogLevel                                                               slog.Level
}

// fakeServer records the settings kokod run hands the koko server in its
// config.
type fakeServer struct {
	calls    int
	settings settings
	err      error
}

func (f *fakeServer) program() kokod.Program {
	return kokod.Program{Serve: func(_ context.Context, c server.Config) error {
		f.calls++
		_, _ = io.WriteString(c.Log, "level=INFO msg=started\n")
		f.settings = settings{
			DataDir:       c.DataDir,
			RootKeyPath:   c.RootKeyPath,
			RootKeySource: c.RootKeySource,
			Owner:         c.Owner,
			Address:       c.Address,
			Listen:        c.Listen,
			WebListen:     c.WebListen,
			LogLevel:      c.LogLevel,
		}
		return f.err
	}}
}

// credentialsDirectory is the variable systemd sets for the koko unit, whose
// LoadCredential=root-key hands it the Root Key's file.
const credentialsDirectory = "CREDENTIALS_DIRECTORY=/run/credentials/koko.service"

func TestRunTakesItsSettingsFromFlagsThenEnvironment(t *testing.T) {
	t.Parallel()
	// The Root Key's file is systemd's copy of the one the koko unit loads.
	defaults := settings{
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
		want settings
	}{
		{"defaults", []string{credentialsDirectory}, nil, defaults},
		{
			"flags", nil,
			[]string{"--data-dir", "/srv/koko", "--root-key-file", "/etc/koko/root.key", "--listen", "203.0.113.7:8443", "--log-level", "debug"},
			settings{DataDir: "/srv/koko", RootKeyPath: "/etc/koko/root.key", Listen: "203.0.113.7:8443", LogLevel: slog.LevelDebug},
		},
		{
			"flags with =", nil,
			[]string{"--data-dir=/srv/koko", "--root-key-file=/etc/koko/root.key", "--listen=[::1]:8443", "--log-level=warn"},
			settings{DataDir: "/srv/koko", RootKeyPath: "/etc/koko/root.key", Listen: "[::1]:8443", LogLevel: slog.LevelWarn},
		},
		{
			"environment",
			[]string{"KOKO_DATA_DIR=/env/koko", "KOKO_ROOT_KEY_FILE=/env/root.key", "KOKO_LISTEN=:8443", "KOKO_LOG_LEVEL=error"},
			nil,
			settings{DataDir: "/env/koko", RootKeyPath: "/env/root.key", Listen: ":8443", LogLevel: slog.LevelError},
		},
		{
			"flag over environment",
			[]string{credentialsDirectory, "KOKO_DATA_DIR=/env/koko", "KOKO_ROOT_KEY_FILE=/env/root.key", "KOKO_LISTEN=:8443"},
			[]string{"--data-dir", "/srv/koko", "--root-key-file", "/etc/koko/root.key", "--listen", ":9443"},
			settings{DataDir: "/srv/koko", RootKeyPath: "/etc/koko/root.key", Listen: ":9443", LogLevel: slog.LevelInfo},
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
			"the web listener as a flag",
			[]string{credentialsDirectory},
			[]string{"--web-listen", "127.0.0.1:7823"},
			withWebListen(defaults, "127.0.0.1:7823"),
		},
		{
			"the web listener in the environment",
			[]string{credentialsDirectory, "KOKO_WEB_LISTEN=0.0.0.0:7823"},
			nil,
			withWebListen(defaults, "0.0.0.0:7823"),
		},
		{
			"the web listener, flag over environment",
			[]string{credentialsDirectory, "KOKO_WEB_LISTEN=0.0.0.0:7823"},
			[]string{"--web-listen=[::1]:7823"},
			withWebListen(defaults, "[::1]:7823"),
		},
		{
			"empty variable is unset",
			[]string{credentialsDirectory, "KOKO_DATA_DIR=", "KOKO_ROOT_KEY_FILE=", "KOKO_LISTEN=", "KOKO_WEB_LISTEN="},
			nil,
			defaults,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeServer{}
			r := harness.RunKokod(t, f.program(), tc.env, append([]string{"run"}, tc.args...)...)
			if r.Status != 0 || r.Stdout != "" {
				t.Fatalf("status %d, stdout %q, stderr %q", r.Status, r.Stdout, r.Stderr)
			}
			if f.calls != 1 || f.settings != tc.want {
				t.Errorf("the koko server ran %d time(s) with %+v, want once with %+v", f.calls, f.settings, tc.want)
			}
			if r.Stderr != "level=INFO msg=started\n" {
				t.Errorf("the koko server's log did not reach stderr: %q", r.Stderr)
			}
		})
	}
}

// withWebListen is s with the web listener on at addr.
func withWebListen(s settings, addr string) settings {
	s.WebListen = addr
	return s
}

// withInit is s with the Owner and the Server Address a fresh koko server
// initialises itself with.
func withInit(s settings, owner, address string) settings {
	s.Owner, s.Address = owner, address
	return s
}

func TestRunRefusesBadSettings(t *testing.T) {
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
		{"unknown flag", nil, []string{"--port", "7822"}, "unknown flag --port for kokod run"},
		{"single dash", nil, []string{"-v"}, "unknown flag -v for kokod run"},
		{"argument", nil, []string{"serve"}, `unexpected argument "serve": kokod run takes flags only`},
		{"--json twice", nil, []string{"--json"}, "--json is given twice"},
		{"--json with a value", nil, []string{"--json=yes"}, "--json takes no value"},
		{"listen address without a port", []string{credentialsDirectory}, []string{"--listen", "0.0.0.0"}, `--listen (KOKO_LISTEN) takes host:port or :port, not "0.0.0.0"`},
		{"listen port out of range", []string{credentialsDirectory}, []string{"--listen", ":65536"}, `--listen (KOKO_LISTEN) takes host:port or :port, not ":65536"`},
		{"listen port by name", []string{credentialsDirectory, "KOKO_LISTEN=:https"}, nil, `--listen (KOKO_LISTEN) takes host:port or :port, not ":https"`},
		{"web listener without a port", []string{credentialsDirectory}, []string{"--web-listen", "127.0.0.1"}, `--web-listen (KOKO_WEB_LISTEN) takes host:port, such as 127.0.0.1:7823, not "127.0.0.1"`},
		{"web listener with a URL", []string{credentialsDirectory}, []string{"--web-listen", "http://127.0.0.1:7823"}, `--web-listen (KOKO_WEB_LISTEN) takes host:port`},
		{"web listener port by name", []string{credentialsDirectory, "KOKO_WEB_LISTEN=127.0.0.1:http"}, nil, `--web-listen (KOKO_WEB_LISTEN) takes host:port, such as 127.0.0.1:7823, not "127.0.0.1:http"`},
		{"web listener without a host", []string{credentialsDirectory}, []string{"--web-listen", ":7823"}, `--web-listen (KOKO_WEB_LISTEN) takes host:port, such as 127.0.0.1:7823, not ":7823"`},
		{"web listener off by word", []string{credentialsDirectory, "KOKO_WEB_LISTEN=off"}, nil, `--web-listen (KOKO_WEB_LISTEN) takes host:port`},
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
			r := harness.RunKokod(t, f.program(), tc.env, append([]string{"run", "--json"}, tc.args...)...)
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

// A failure exits 1, said as kokod's own on stderr, or as the error object
// koko's --json prints, with the code internal_error.
func TestRunFailureExitsOne(t *testing.T) {
	t.Parallel()
	f := &fakeServer{err: errors.New("listen on /run/koko/koko.sock: permission denied")}
	r := harness.RunKokod(t, f.program(), []string{credentialsDirectory}, "run")
	if r.Status != 1 || r.Stdout != "" || !strings.HasSuffix(r.Stderr, "kokod: listen on /run/koko/koko.sock: permission denied\n") {
		t.Errorf("status %d, stdout %q, stderr %q", r.Status, r.Stdout, r.Stderr)
	}
	r = harness.RunKokod(t, f.program(), []string{credentialsDirectory}, "run", "--json")
	r.Stderr = strings.TrimPrefix(r.Stderr, "level=INFO msg=started\n")
	if msg := r.WantError(t, 1, "internal_error"); msg != "listen on /run/koko/koko.sock: permission denied" {
		t.Errorf("message %q", msg)
	}
}

// A command line kokod does not take is refused with exit 2, as text or,
// with --json, as an error object, before anything runs.
func TestKokodRefusesACommandLineItDoesNotTake(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		args    []string
		message string
	}{
		{"no command", nil, "give a command: kokod run, kokod install, kokod uninstall, kokod version or kokod help; kokod help lists them"},
		{"unknown command", []string{"serve", "--json"}, "kokod serve is not a command; kokod takes kokod run, kokod install, kokod uninstall, kokod version or kokod help"},
		{"flags first", []string{"--json", "run"}, "give a command before its flags: kokod run, kokod install, kokod uninstall, kokod version or kokod help"},
		{"help for an unknown command", []string{"help", "serve", "--json"}, "kokod serve is not a command"},
		{"version with a flag it does not take", []string{"version", "--data-dir", "/srv/koko", "--json"}, "unknown flag --data-dir for kokod version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeServer{}
			r := harness.RunKokod(t, f.program(), nil, tc.args...)
			if !strings.Contains(strings.Join(tc.args, " "), "--json") {
				if r.Status != 2 || r.Stdout != "" || r.Stderr != "kokod: "+tc.message+"\n" {
					t.Errorf("status %d, stdout %q, stderr %q, want 2 and kokod: %s", r.Status, r.Stdout, r.Stderr, tc.message)
				}
				return
			}
			if msg := r.WantError(t, 2, "invalid_usage"); !strings.Contains(msg, tc.message) {
				t.Errorf("message %q does not say %q", msg, tc.message)
			}
			if f.calls != 0 {
				t.Error("the koko server ran despite the refusal")
			}
		})
	}
}

// kokod help and kokod --help list the commands and say that kokod install
// and uninstall are the machine's part of koko server install and
// uninstall; kokod run --help, and kokod help run, list its flags and
// variables.
func TestKokodHelp(t *testing.T) {
	t.Parallel()
	p := (&fakeServer{}).program()
	r := harness.RunKokod(t, p, nil, "help")
	r.WantSuccess(t)
	harness.Golden(t, "kokod-help.txt", r.Stdout)
	if again := harness.RunKokod(t, p, nil, "--help"); again != r {
		t.Errorf("kokod --help = %+v, want what kokod help prints", again)
	}

	run := harness.RunKokod(t, p, nil, "run", "--help")
	run.WantSuccess(t)
	harness.Golden(t, "kokod-run-help.txt", run.Stdout)
	if again := harness.RunKokod(t, p, nil, "help", "run"); again != run {
		t.Errorf("kokod help run = %+v, want what kokod run --help prints", again)
	}
	version := harness.RunKokod(t, p, nil, "version", "--help")
	version.WantSuccess(t)
	if !strings.HasPrefix(version.Stdout, "kokod version [--json]\n") {
		t.Errorf("kokod version --help = %q", version.Stdout)
	}
}

// kokod version prints the build koko version prints, naming kokod, as text
// or as JSON with the same fields.
func TestKokodVersionIsKokosBuild(t *testing.T) {
	t.Parallel()
	p := (&fakeServer{}).program()
	r := harness.RunKokod(t, p, nil, "version")
	r.WantSuccess(t)
	if want := "kokod " + build.Read().String() + "\n"; r.Stdout != want {
		t.Errorf("kokod version = %q, want %q", r.Stdout, want)
	}

	type versionOut struct {
		Version  string `json:"version"`
		Revision string `json:"revision"`
		Go       string `json:"go"`
		Platform string `json:"platform"`
	}
	var kokodV, kokoV versionOut
	r = harness.RunKokod(t, p, nil, "version", "--json")
	r.WantSuccess(t)
	if err := json.Unmarshal([]byte(r.Stdout), &kokodV, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("kokod version --json = %s: %v", r.Stdout, err)
	}
	k := harness.Run(t, cli.Runner{}, nil, "version", "--json")
	k.WantSuccess(t)
	if err := json.Unmarshal([]byte(k.Stdout), &kokoV, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("koko version --json = %s: %v", k.Stdout, err)
	}
	if kokodV != kokoV || kokodV.Version == "" {
		t.Errorf("kokod version --json = %+v, koko version --json = %+v, want the same build", kokodV, kokoV)
	}
}
