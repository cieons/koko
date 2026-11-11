package release

import (
	"strings"
	"testing"
)

// The skew check's pairs are this build with the previous release and
// with compat.Oldest's, both ways, each release once: the previous release
// is the highest release tag before the commit's own, by version, and a
// release that is not there yet, or is below compat.Oldest, has no pair.
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
			name:     "a release's own tag is not its previous release",
			releases: [][2]string{{"v0.3.0", ""}, {"v0.3.1", "v0.3.0"}, {"v0.3.2", "v0.3.0"}},
			want:     pairs("v0.3.1", "v0.3.0"),
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
			says:     "compat.Oldest (v0.4.0) is no release before this commit",
		},
		{
			name:     "no compat.Oldest to read",
			releases: [][2]string{{"v0.3.0", "block:v0.3.0"}},
			says:     "found no compat.Oldest",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHistory(t)
			for _, r := range tc.releases {
				h.release(r[0], r[1])
			}
			if tc.commit != "" {
				h.release("after", tc.commit)
				h.git("tag", "-d", "after")
			}
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
