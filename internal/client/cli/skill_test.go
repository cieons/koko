package cli_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
)

// offline is a runner on a machine where no koko server runs and none was
// joined, so that a command that called one would fail.
func offline(t *testing.T) cli.Runner {
	return cli.Runner{SocketPath: filepath.Join(t.TempDir(), "none.sock")}
}

// koko skill install writes the stub under HOME, a file koko owns in a
// directory of its own, and prints where; the stub's bytes are fixed, so it
// never goes stale across upgrades.
func TestSkillInstallWritesTheStubUnderHome(t *testing.T) {
	home := t.TempDir()
	r := harness.Run(t, offline(t), []string{"HOME=" + home}, "skill", "install", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "skill-install.json", strings.ReplaceAll(r.Stdout, home, "<home>"))

	stub, err := os.ReadFile(filepath.Join(home, ".claude", "skills", "koko", "SKILL.md"))
	if err != nil {
		t.Fatalf("read the stub: %v", err)
	}
	harness.Golden(t, "skill-install-stub.md", string(stub))
	if !strings.Contains(string(stub), "\nname: koko\n") || !strings.Contains(string(stub), "Run `koko skill`") {
		t.Errorf("the stub = %q, want the name koko and koko skill to run", stub)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(home, ".claude", "skills", "koko", "SKILL.md"))
		if err != nil || info.Mode().Perm() != 0o644 {
			t.Errorf("the stub's mode = %v (%v), want -rw-r--r--", info.Mode(), err)
		}
	}
}

// --dir names another agent's skills directory: koko writes only its own
// koko/SKILL.md there, over the stub an earlier install wrote, and leaves
// every other file as it was.
func TestSkillInstallIntoDirTouchesOnlyItsOwnFile(t *testing.T) {
	dir := t.TempDir()
	others := map[string]string{
		"SKILL.md":          "another agent's own skill\n",
		"other/SKILL.md":    "another skill\n",
		"koko/notes.txt":    "the user's note beside koko's stub\n",
		"koko/SKILL.md":     "an older stub\n",
		"koko/.SKILL.md.12": "not a temporary file of this install\n",
	}
	for name, content := range others {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	r := harness.Run(t, offline(t), []string{"HOME=" + t.TempDir()}, "skill", "install", "--dir", dir)
	r.WantSuccess(t)
	stub := filepath.Join(dir, "koko", "SKILL.md")
	if !strings.Contains(r.Stdout, stub) {
		t.Errorf("koko skill install printed %q, want the stub's path %s", r.Stdout, stub)
	}
	got, err := os.ReadFile(stub)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "skill-install-stub.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("--dir's stub = %q, want the stub koko writes under HOME", got)
	}
	delete(others, "koko/SKILL.md")
	for name, content := range others {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(b) != content {
			t.Errorf("%s holds %q (%v) after the install, want %q as it was", name, b, err, content)
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "koko"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{".SKILL.md.12", "SKILL.md", "notes.txt"}) {
		t.Errorf("koko/ holds %v after the install, want no file left behind", names)
	}
}

// Without HOME, the default directory is unknown: the command line is
// refused, naming --dir, and nothing is written.
func TestSkillInstallWithoutHomeNamesDir(t *testing.T) {
	r := harness.Run(t, offline(t), nil, "skill", "install", "--json")
	if msg := r.WantError(t, 2, "invalid_usage"); !strings.Contains(msg, "--dir") {
		t.Errorf("the refusal %q does not name --dir", msg)
	}
}

// koko skill needs no koko server: it prints the reference of the binary.
func TestSkillIsOffline(t *testing.T) {
	r := harness.Run(t, offline(t), []string{"HOME=" + t.TempDir()}, "skill")
	r.WantSuccess(t)
	if !strings.HasPrefix(r.Stdout, "# koko\n") {
		t.Errorf("koko skill = %q, want the reference", r.Stdout[:min(len(r.Stdout), 200)])
	}
}

// koko help --json prints each command it shows as koko skill --json
// describes one.
func TestHelpJSON(t *testing.T) {
	r := harness.Run(t, offline(t), nil, "help", "skill", "install", "--json")
	r.WantSuccess(t)
	harness.Golden(t, "help-skill-install.json", r.Stdout)
}

// koko help refuses words that start no command, naming what they could
// continue with, and --help anywhere points to it.
func TestHelpRefusesWhatIsNoCommand(t *testing.T) {
	r := harness.Run(t, offline(t), nil, "help", "host", "frob", "--json")
	if msg := r.WantError(t, 2, "invalid_usage"); !strings.Contains(msg, "koko host frob is not a command") || !strings.Contains(msg, "ls") {
		t.Errorf("the refusal %q does not say koko host frob is no command and what koko host takes", msg)
	}
	for _, line := range [][]string{{"--help"}, {"host", "ls", "--help"}, {"host", "--help", "ls"}} {
		r := harness.Run(t, offline(t), nil, append(line, "--json")...)
		msg := r.WantError(t, 2, "invalid_usage")
		want := "koko help"
		if len(line) > 1 {
			want = "koko help host ls"
		}
		if !strings.Contains(msg, want) {
			t.Errorf("koko %s: the refusal %q does not point to %s", strings.Join(line, " "), msg, want)
		}
	}
}

var scriptValue = regexp.MustCompile(`"script": ".*"`)

// koko completion prints the script for bash or zsh, and refuses any other
// shell, naming the two.
func TestCompletionShells(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		r := harness.Run(t, offline(t), nil, "completion", shell)
		r.WantSuccess(t)
		if !strings.Contains(r.Stdout, "_koko_complete()") {
			t.Errorf("koko completion %s = %q, want the script", shell, r.Stdout[:min(len(r.Stdout), 200)])
		}
		r = harness.Run(t, offline(t), nil, "completion", shell, "--json")
		r.WantSuccess(t)
		// The script grows with the tree; the golden file pins the fields.
		harness.Golden(t, "completion-"+shell+".json", scriptValue.ReplaceAllString(r.Stdout, `"script": "<script>"`))
	}
	r := harness.Run(t, offline(t), nil, "completion", "fish", "--json")
	if msg := r.WantError(t, 2, "invalid_usage"); !strings.Contains(msg, "bash, zsh") {
		t.Errorf("the refusal %q does not name bash and zsh", msg)
	}
}
