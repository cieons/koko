package cli_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
)

// koko server run keeps working beside kokod run, which takes the same flags
// (package serverrun) and whose own tests cover every setting, refusal and
// start-up check; these cover koko server run's own path.

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
	for _, tc := range []struct {
		name string
		env  []string
		args []string
		want cli.ServerSettings
	}{
		{
			"defaults",
			[]string{credentialsDirectory},
			nil,
			cli.ServerSettings{
				DataDir:       "/var/lib/koko",
				RootKeyPath:   "/run/credentials/koko.service/root-key",
				RootKeySource: "/etc/koko/root.key",
				Listen:        ":7822",
				LogLevel:      slog.LevelInfo,
			},
		},
		{
			"flag over environment",
			[]string{"KOKO_DATA_DIR=/env/koko", "KOKO_ROOT_KEY_FILE=/env/root.key", "KOKO_LISTEN=:8443", "KOKO_OWNER=bob", "KOKO_ADDRESS=env.example.com"},
			[]string{"--data-dir", "/srv/koko", "--listen", ":9443", "--log-level", "debug", "--owner", "alice"},
			cli.ServerSettings{
				DataDir: "/srv/koko", RootKeyPath: "/env/root.key", Listen: ":9443", LogLevel: slog.LevelDebug,
				Owner: "alice", Address: "env.example.com",
			},
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
		{"Owner without Server Address", []string{credentialsDirectory}, []string{"--owner", "alice"}, "--owner (KOKO_OWNER) needs --address (KOKO_ADDRESS)"},
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

// koko server run starts the koko server's real start-up with its Root Key,
// and a start-up refusal keeps its code and exit status.
func TestServerRunStartsTheKokoServer(t *testing.T) {
	t.Parallel()
	keyFile := harness.NewRootKeyFile(t)
	r := harness.Run(t, cli.Runner{ServerRun: harness.ServerStart(t)}, nil,
		"server", "run", "--data-dir", t.TempDir(), "--root-key-file", keyFile)
	if r.Status != 0 || !strings.Contains(r.Stderr, `msg="koko server started"`) {
		t.Fatalf("exit status %d, stderr %q", r.Status, r.Stderr)
	}
	r = harness.Run(t, cli.Runner{ServerRun: harness.ServerStartWith(t, harness.ProcessUIDs(0, 0, 0))}, nil,
		"server", "run", "--json", "--data-dir", t.TempDir(), "--root-key-file", keyFile)
	if msg := r.WantError(t, 2, "invalid_usage"); !strings.Contains(msg, "does not run as root") {
		t.Errorf("as root: message %q does not say it does not run as root", msg)
	}
}

// --owner and --address belong to koko server run, which stays in the
// irreversible tier (ADR 0019): with them it makes a Root Key and
// initialises the koko server.
func TestServerRunWithItsInitialisationSettingsIsIrreversible(t *testing.T) {
	t.Parallel()
	for _, c := range cli.Tree() {
		if c.Path() != "server run" {
			continue
		}
		if c.Tier != cli.Irreversible {
			t.Errorf("koko server run is in the %v tier, want irreversible", c.Tier)
		}
		var envs []string
		for _, f := range c.Flags {
			envs = append(envs, f.Env)
		}
		for _, env := range []string{"KOKO_OWNER", "KOKO_ADDRESS"} {
			if !slices.Contains(envs, env) {
				t.Errorf("koko server run takes no setting from %s", env)
			}
		}
		return
	}
	t.Error("the command tree lacks koko server run")
}

// contents reads every file under dir, by path relative to it.
func contents(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		rel, _ := filepath.Rel(dir, path)
		files[rel] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
