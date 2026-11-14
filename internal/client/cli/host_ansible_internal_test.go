package cli

import "testing"

// ansibleName keeps what Ansible takes in a variable or a group name and
// turns every other character, one for each, into '_'.
func TestAnsibleNameReplacesWhatAnsibleDoesNotTake(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"koko_expires_at": "koko_expires_at",
		"koko_a-b.c":      "koko_a_b_c",
		"space_eu-ops":    "space_eu_ops",
		"team_Core/Ops 2": "team_Core_Ops_2",
		"city_Zürich":     "city_Z_rich",
	} {
		if got := ansibleName(in); got != want {
			t.Errorf("ansibleName(%q) = %q, want %q", in, got, want)
		}
	}
}
