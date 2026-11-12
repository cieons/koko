package cli_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/cieons/koko/internal/harness"
)

// On a joined Windows device koko status says, each only when it applies,
// in --json and in text: that HOME names another directory than the
// profile directory koko writes under, and that no ssh is on PATH, with
// the line that adds the OpenSSH client.
func TestStatusWarnsOfAnotherHomeAndNoSSHOnWindows(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	d := h.Join(harness.Owner)
	home := d.Home()
	other := t.TempDir()
	noSSH := t.TempDir()
	withSSH := t.TempDir()
	if err := os.WriteFile(filepath.Join(withSSH, "ssh.exe"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	const addSSH = "Add-WindowsCapability -Online -Name OpenSSH.Client~~~~0.0.1.0"

	for _, c := range []struct {
		name         string
		env          []string
		home, client bool
	}{
		{name: "HOME unset, ssh on the test's PATH"},
		{name: "HOME another directory", env: []string{"HOME=" + other}, home: true},
		{name: "HOME the profile, written otherwise", env: []string{"HOME=" + strings.ToUpper(filepath.ToSlash(home)) + "/"}},
		{name: "HOME empty", env: []string{"HOME="}},
		{name: "no ssh on PATH", env: []string{"PATH=" + noSSH}, client: true},
		{name: "ssh.exe on PATH", env: []string{"PATH=" + noSSH + ";" + withSSH}},
		{name: "both", env: []string{"HOME=" + other, "PATH=" + noSSH}, home: true, client: true},
	} {
		s, _ := status(t, func(args ...string) harness.Result { return d.KokoEnv(c.env, args...) })
		text := d.KokoEnv(c.env, "status")
		text.WantSuccess(t)
		switch w := s.HomeWarning; {
		case c.home && (w == nil || !strings.Contains(*w, other) || !strings.Contains(*w, home)):
			t.Errorf("%s: home_warning %v, want one naming %s and %s", c.name, deref(w), other, home)
		case c.home && !strings.Contains(text.Stdout, "\nWarning: "+*w+".\n"):
			t.Errorf("%s: koko status printed\n%s\nwithout the warning %q", c.name, text.Stdout, *w)
		case !c.home && w != nil:
			t.Errorf("%s: home_warning %q, want null", c.name, *w)
		}
		switch w := s.SSHClientWarning; {
		case c.client && (w == nil || !strings.Contains(*w, addSSH)):
			t.Errorf("%s: ssh_client_warning %v, want one with %q", c.name, deref(w), addSSH)
		case c.client && !strings.Contains(text.Stdout, "\nWarning: "+*w+".\n"):
			t.Errorf("%s: koko status printed\n%s\nwithout the warning %q", c.name, text.Stdout, *w)
		case !c.client && w != nil:
			t.Errorf("%s: ssh_client_warning %q, want null", c.name, *w)
		}
	}
}

// On a joined Windows device koko status says when OpenSSH for Windows
// would refuse ~/.ssh/config because another principal may write it,
// naming the principal and the icacls line that takes the access away;
// once that line has run, it says nothing. Another principal that may only
// read it is no reason.
func TestStatusWarnsOfAConfigOpenSSHForWindowsRefuses(t *testing.T) {
	t.Parallel()
	h := harness.Start(t)
	d := h.Join(harness.Owner)
	config := filepath.Join(d.Home(), ".ssh", "config")
	const users = "S-1-5-32-545" // BUILTIN\Users
	icacls := func(line string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "icacls")
		// The line as a person types it, quotes and all.
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: line}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", line, err, out)
		}
	}
	warning := func() *string {
		t.Helper()
		s, _ := status(t, d.Koko)
		return s.IncludeWarning
	}

	icacls(`icacls "` + config + `" /grant *` + users + `:(R)`)
	if w := warning(); w != nil {
		t.Errorf("with BUILTIN\\Users let read ~/.ssh/config, include_warning %q, want null", *w)
	}

	icacls(`icacls "` + config + `" /grant *` + users + `:(W)`)
	w := warning()
	if w == nil || !strings.Contains(*w, "("+users+") may write") {
		t.Fatalf("with BUILTIN\\Users let write ~/.ssh/config, include_warning %v, want one naming %s", deref(w), users)
	}
	text := d.Koko("status")
	if !strings.Contains(text.Stdout, "\nWarning: "+*w+".\n") {
		t.Errorf("koko status printed\n%s\nwithout the warning %q", text.Stdout, *w)
	}
	_, line, ok := strings.Cut(*w, ": run ")
	line, ok2 := strings.CutSuffix(line, " to take that access away")
	if !ok || !ok2 {
		t.Fatalf("include_warning %q gives no icacls line", *w)
	}
	icacls(line)
	if w := warning(); w != nil {
		t.Errorf("after %s, include_warning %q, want null", line, *w)
	}
}
