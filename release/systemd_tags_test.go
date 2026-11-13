package release

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/mod/semver"

	"github.com/cieons/koko/internal/shared/compat"
)

// The real-systemd check's older build is the commit before the latest
// migration step, and its two builds are tagged so that this commit's is
// always the newer as Go stamps them, with the highest tag on each commit,
// and both are within the window: an older commit that carries a release
// tag, one with no tag near it, a pre-release, a release's own tag, a
// compat.Oldest above every tag and a clone without tags.
func TestSystemdTagsKeepThisCommitsBuildTheNewer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// make builds the history; the commit checked out is its last.
		make func(h *history)
		// older names the commit the older build is expected from.
		older string
		// want are the older and the newer build's tags.
		want [2]string
		says string
	}{
		{
			name: "an older commit that carries a release tag",
			make: func(h *history) {
				h.release("v0.8.0", "v0.3.0")
				h.migration("a")
				h.release("v0.8.1", "v0.3.0")
				h.migration("b")
				h.commit("c", "v0.3.0")
			},
			older: "v0.8.1",
			want:  [2]string{"v0.8.2-check.1", "v0.8.2-check.2"},
		},
		{
			name: "an older commit with no tag near it",
			make: func(h *history) {
				h.release("v0.8.1", "v0.3.0")
				h.commit("c", "v0.3.0")
				h.migration("a")
				h.commit("d", "v0.3.0")
			},
			older: "HEAD~2",
			want:  [2]string{"v0.8.2-check.1", "v0.8.2-check.2"},
		},
		{
			name: "a release's own tag, as the release workflow runs it",
			make: func(h *history) {
				h.release("v0.8.1", "v0.3.0")
				h.migration("a")
				h.release("v0.8.2", "v0.3.0")
			},
			older: "v0.8.1",
			want:  [2]string{"v0.8.2-check.1", "v0.8.2-check.2"},
		},
		{
			name: "a pre-release on the older commit",
			make: func(h *history) {
				h.release("v0.8.1", "v0.3.0")
				h.release("v0.9.0-rc.1", "v0.3.0")
				h.migration("a")
				h.commit("c", "v0.3.0")
			},
			older: "v0.9.0-rc.1",
			want:  [2]string{"v0.9.1-check.1", "v0.9.1-check.2"},
		},
		{
			name: "tags after the older commit do not count",
			make: func(h *history) {
				h.release("v0.8.1", "v0.3.0")
				h.migration("a")
				h.release("v0.9.0", "v0.3.0")
				h.commit("c", "v0.3.0")
			},
			older: "v0.8.1",
			want:  [2]string{"v0.8.2-check.1", "v0.8.2-check.2"},
		},
		{
			name: "compat.Oldest above every tag",
			make: func(h *history) {
				h.release("v0.8.1", "v0.3.0")
				h.migration("a")
				h.commit("c", "v0.9.0")
			},
			older: "v0.8.1",
			want:  [2]string{"v0.9.1-check.1", "v0.9.1-check.2"},
		},
		{
			name: "a clone without tags",
			make: func(h *history) {
				h.commit("c", "v0.3.0")
				h.migration("a")
				h.commit("d", "v0.3.0")
			},
			older: "HEAD~2",
			want:  [2]string{"v0.3.1-check.1", "v0.3.1-check.2"},
		},
		{
			name: "no migration step",
			make: func(h *history) {
				h.release("v0.8.1", "v0.3.0")
				h.commit("c", "v0.3.0")
			},
			says: "no commit added a migration step",
		},
		{
			name: "no compat.Oldest to read",
			make: func(h *history) {
				h.release("v0.8.1", "v0.3.0")
				h.migration("a")
				h.commit("c", "block:v0.3.0")
			},
			says: "found no compat.Oldest",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHistory(t)
			tc.make(h)
			status, stdout, stderr := h.run("systemd-tags.sh")
			if tc.says != "" {
				if status == 0 || !strings.Contains(stderr, tc.says) {
					t.Fatalf("systemd-tags.sh exited %d and said %q, want a failure saying %q", status, stderr, tc.says)
				}
				return
			}
			want := "OLDER_COMMIT=" + h.git("rev-parse", tc.older+"^{commit}") + "\n" +
				"OLDER_TAG=" + tc.want[0] + "\n" +
				"NEWER_TAG=" + tc.want[1] + "\n"
			if status != 0 || stdout != want {
				t.Fatalf("systemd-tags.sh exited %d and printed %q, want %q:\n%s", status, stdout, want, stderr)
			}
			older, newer := h.stamp(tc.older, tc.want[0]), h.stamp("HEAD", tc.want[1])
			if !compat.Newer(newer, older) {
				t.Errorf("this commit's build is %s, not newer than the older build, %s", newer, older)
			}
			oldest := regexp.MustCompile(`const Oldest = "(.*)"`).FindStringSubmatch(h.git("show", "HEAD:internal/shared/compat/compat.go"))[1]
			if compat.Newer(oldest, older) {
				t.Errorf("the older build, %s, is below compat.Oldest, %s", older, oldest)
			}
		})
	}
}

// commit commits a compat.go whose compat.Oldest is oldest, as release
// does, with name to tell it apart, and no tag.
func (h *history) commit(name, oldest string) {
	h.t.Helper()
	h.release(name, oldest)
	h.git("tag", "-d", name)
}

// migration commits a migration step, a file added to the store's
// migrations.
func (h *history) migration(name string) {
	h.t.Helper()
	file := filepath.Join(h.dir, "internal", "server", "store", "migrations", name+".sql")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("-- "+name+"\n"), 0o644); err != nil {
		h.t.Fatal(err)
	}
	h.git("add", "-A")
	h.git("commit", "-q", "-m", "migration "+name)
}

// stamp returns the version Go stamps on a build of the commit rev once
// it is tagged tag: the highest of the tags on it.
func (h *history) stamp(rev, tag string) string {
	h.t.Helper()
	highest := tag
	for _, other := range strings.Fields(h.git("tag", "--points-at", rev)) {
		if semver.IsValid(other) && semver.Compare(other, highest) > 0 {
			highest = other
		}
	}
	return highest
}
