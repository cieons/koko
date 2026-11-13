package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The skew check's pairs are this build with the previous release and
// with compat.Oldest's, both ways, each release once: the previous release
// is the highest published release tag before the commit's own, by
// version, and a release that is not there yet, is not yet published, or
// is below compat.Oldest, has no pair.
func TestSkewPairsAreThePreviousReleaseAndOldestBothWays(t *testing.T) {
	t.Parallel()
	pairs := func(versions ...string) string {
		var list []string
		for _, v := range versions {
			list = append(list, `{"cli":"this","kokod":"`+v+`"}`, `{"cli":"`+v+`","kokod":"this"}`)
		}
		return "[" + strings.Join(list, ",") + "]\n"
	}
	for _, tc := range []struct {
		name string
		// releases are the tags, in the order they were made, each with
		// its compat.Oldest; the commit checked out is the last one.
		releases [][2]string
		// commit, when set, is a commit after the last release, with this
		// compat.Oldest.
		commit string
		// published are the tags whose GitHub Release is published, every
		// release's when nil.
		published []string
		// unread, when set, is what gh says as it fails to list them.
		unread string
		want   string
		says   string
	}{
		{
			name:     "a commit after the previous release",
			releases: [][2]string{{"v0.3.0", ""}, {"v0.3.1", "v0.3.0"}},
			commit:   "v0.3.0",
			want:     pairs("v0.3.1", "v0.3.0"),
		},
		{
			name:      "a release's own tag, not yet published, is not its previous release",
			releases:  [][2]string{{"v0.3.0", ""}, {"v0.3.1", "v0.3.0"}, {"v0.3.2", "v0.3.0"}},
			published: []string{"v0.3.0", "v0.3.1"},
			want:      pairs("v0.3.1", "v0.3.0"),
		},
		{
			name:      "a commit after a release still being built",
			releases:  [][2]string{{"v0.3.0", ""}, {"v0.3.1", "v0.3.0"}, {"v0.3.2", "v0.3.0"}},
			commit:    "v0.3.0",
			published: []string{"v0.3.0", "v0.3.1"},
			want:      pairs("v0.3.1", "v0.3.0"),
			says:      "v0.3.2 has no published release yet",
		},
		{
			name:     "the previous release is compat.Oldest's, once",
			releases: [][2]string{{"v0.3.0", "v0.3.0"}},
			commit:   "v0.3.0",
			want:     pairs("v0.3.0"),
		},
		{
			name:     "by version, and never a pre-release",
			releases: [][2]string{{"v0.5.0", "v0.5.0"}, {"v0.9.0", "v0.5.0"}, {"v0.10.0", "v0.5.0"}, {"v0.10.1-rc.1", "v0.5.0"}},
			commit:   "v0.5.0",
			want:     pairs("v0.10.0", "v0.5.0"),
		},
		{
			name:     "compat.Oldest moved to a release that is not there yet",
			releases: [][2]string{{"v0.3.0", "v0.3.0"}, {"v0.3.1", "v0.3.0"}},
			commit:   "v0.4.0",
			want:     pairs(),
			says:     "v0.3.1, is below compat.Oldest (v0.4.0)",
		},
		{
			name:     "compat.Oldest's own release",
			releases: [][2]string{{"v0.3.1", "v0.3.0"}, {"v0.4.0", "v0.4.0"}},
			want:     pairs(),
			says:     "compat.Oldest (v0.4.0) is no published release before this commit",
		},
		{
			name:     "no compat.Oldest to read",
			releases: [][2]string{{"v0.3.0", "block:v0.3.0"}},
			says:     "found no compat.Oldest",
		},
		{
			name:     "the published releases cannot be read",
			releases: [][2]string{{"v0.3.0", ""}, {"v0.3.1", "v0.3.0"}},
			commit:   "v0.3.0",
			unread:   "HTTP 401: Bad credentials",
			says:     "could not read the published releases with gh release list: HTTP 401: Bad credentials",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHistory(t)
			var tags []string
			for _, r := range tc.releases {
				h.release(r[0], r[1])
				tags = append(tags, r[0])
			}
			if tc.commit != "" {
				h.release("after", tc.commit)
				h.git("tag", "-d", "after")
			}
			if tc.published != nil {
				tags = tc.published
			}
			h.env = fakeGH(t, tags, tc.unread)
			status, stdout, stderr := h.run("skew-pairs.sh")
			if (status == 0) != (tc.want != "") || stdout != tc.want {
				t.Errorf("skew-pairs.sh exited %d and printed %q, want %q:\n%s", status, stdout, tc.want, stderr)
			}
			if !strings.Contains(stderr, tc.says) {
				t.Errorf("skew-pairs.sh said %q, which does not say %q", stderr, tc.says)
			}
		})
	}
}

// fakeGH returns the environment of a PATH whose gh lists published as the
// tags of the published releases, the newest first as gh does, or, when
// unread is set, fails saying so.
func fakeGH(t *testing.T, published []string, unread string) []string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"[ \"$1 $2\" = 'release list' ] || { echo \"fake gh: unexpected gh $*\" >&2; exit 2; }\n"
	if unread != "" {
		script += "echo '" + unread + "' >&2\nexit 1\n"
	}
	for i := len(published) - 1; i >= 0; i-- {
		script += "echo " + published[i] + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return []string{"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")}
}
