package release

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/shared/compat"
)

// history is a git repository of releases, each a commit tagged with its
// version whose compat.go holds its compat.Oldest, for oldest-check.sh.
type history struct {
	t   *testing.T
	dir string
}

func newHistory(t *testing.T) *history {
	t.Helper()
	h := &history{t: t, dir: t.TempDir()}
	h.git("init", "-q")
	return h
}

// git runs git in the repository, apart from the user's own configuration.
func (h *history) git(args ...string) {
	h.t.Helper()
	cmd := exec.CommandContext(h.t.Context(), "git", append([]string{"-c", "user.name=koko", "-c", "user.email=koko@example.com", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	cmd.Dir = h.dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		h.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// release commits a compat.go whose compat.Oldest is oldest, on its one
// line, or in a const block for "block:<version>", or none when oldest is
// "", and tags the commit tag.
func (h *history) release(tag, oldest string) {
	h.t.Helper()
	file := filepath.Join(h.dir, "internal", "shared", "compat", "compat.go")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		h.t.Fatal(err)
	}
	content := "package compat\n"
	switch block, ok := strings.CutPrefix(oldest, "block:"); {
	case ok:
		content += "\nconst (\n\tOldest = \"" + block + "\"\n)\n"
	case oldest != "":
		content += "\nconst Oldest = \"" + oldest + "\"\n"
	}
	content += "\n// " + tag + "\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
	h.git("add", "-A")
	h.git("commit", "-q", "-m", tag)
	h.git("tag", tag)
}

// run runs the script name of this directory at the top of the repository
// and returns its exit status, its stdout and its stderr.
func (h *history) run(name string, args ...string) (status int, stdout, stderr string) {
	h.t.Helper()
	script, err := filepath.Abs(name)
	if err != nil {
		h.t.Fatal(err)
	}
	cmd := exec.CommandContext(h.t.Context(), "sh", append([]string{script}, args...)...)
	cmd.Dir = h.dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	var out, errs strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errs
	err = cmd.Run()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return exit.ExitCode(), out.String(), errs.String()
	} else if err != nil {
		h.t.Fatal(err)
	}
	return 0, out.String(), errs.String()
}

// check runs oldest-check.sh for tag and returns its exit status and
// output.
func (h *history) check(tag string) (int, string) {
	h.t.Helper()
	status, stdout, stderr := h.run("oldest-check.sh", tag)
	return status, stdout + stderr
}

// oldest-check.sh and the real-systemd workflow read compat.Oldest from
// its one line in compat.go, so that line keeps its form.
func TestCompatGoStatesOldestOnOneLine(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("..", "internal", "shared", "compat", "compat.go"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^const Oldest = "(v[0-9]+\.[0-9]+\.[0-9]+)"$`).FindAllSubmatch(b, -1)
	if len(m) != 1 || string(m[0][1]) != compat.Oldest {
		t.Errorf("compat.go states compat.Oldest as %q, want the one line const Oldest = %q", m, compat.Oldest)
	}
}

// The release job refuses a patch release, or a pre-release of one, whose
// compat.Oldest differs from the previous release's, naming both; a minor
// release may move it, and a patch release after one from before the
// window has nothing to compare with.
func TestOldestCheckRefusesAPatchReleaseThatMovesOldest(t *testing.T) {
	t.Parallel()
	h := newHistory(t)
	h.release("v0.4.0", "")
	h.release("v0.4.1", "v0.3.0")
	h.release("v0.4.2", "v0.3.0")
	h.release("v0.5.0", "v0.4.0")
	h.release("v0.5.1-rc.1", "v0.5.0")
	h.release("v0.5.1", "v0.4.0")
	h.release("v0.5.2", "v0.5.0")
	h.release("v0.10.0", "v0.10.0")
	h.release("v0.10.1", "v0.10.0")
	h.release("v0.10.2-no-oldest", "")
	h.release("v0.11.0", "block:v0.11.0")
	h.release("v0.11.1", "v0.11.0")
	for _, tc := range []struct {
		tag  string
		ok   bool
		says []string
	}{
		{"v0.4.1", true, []string{"v0.4.0", "nothing to compare"}},
		{"v0.4.2", true, []string{"v0.3.0", "v0.4.1"}},
		{"v0.5.0", true, []string{"not a patch release"}},
		{"v0.5.1-rc.1", false, []string{"v0.5.0", "v0.4.0"}},
		{"v0.5.1", true, []string{"v0.4.0", "v0.5.0"}},
		{"v0.5.2", false, []string{"v0.5.0", "v0.5.1", "v0.4.0"}},
		// By version, not by name: v0.10.0 is after v0.5.2.
		{"v0.10.1", true, []string{"v0.10.0"}},
		{"v0.10.2-no-oldest", false, []string{"no compat.Oldest"}},
		// Unknown is not "none": the previous release's line is unread.
		{"v0.11.1", false, []string{"v0.11.0", "cannot read"}},
		{"v0.9.9", false, []string{"no tag v0.9.9"}},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			status, out := h.check(tc.tag)
			if (status == 0) != tc.ok {
				t.Errorf("oldest-check.sh %s exited %d, want success %v:\n%s", tc.tag, status, tc.ok, out)
			}
			for _, want := range tc.says {
				if !strings.Contains(out, want) {
					t.Errorf("oldest-check.sh %s said %q, which does not name %q", tc.tag, out, want)
				}
			}
		})
	}
}
