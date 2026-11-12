package release

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness/skip"
	sharedrelease "github.com/cieons/koko/internal/shared/release"
)

// install.ps1's tests on a fake Windows machine are in
// install_ps1_windows_test.go, which only Windows builds. These read its
// text, on every OS.

func readPS1(t *testing.T) string {
	t.Helper()
	script, err := os.ReadFile("install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	return string(script)
}

// install.ps1 is ASCII, with no byte order mark: Windows PowerShell 5.1
// reads other bytes in the system's code page, and Invoke-Expression would
// run something else than what was written.
func TestPS1IsASCII(t *testing.T) {
	t.Parallel()
	for n, line := range strings.Split(readPS1(t), "\n") {
		for _, r := range line {
			if r > 0x7e || (r < 0x20 && r != '\t') {
				t.Errorf("install.ps1 line %d holds %q, which is not printable ASCII:\n%s", n+1, r, line)
				break
			}
		}
	}
}

// Everything in install.ps1 is in Install-Koko, called on the last line and
// nowhere else, so a download cut short runs nothing: outside the function
// there are only comments. It never calls exit, which would close the
// PowerShell the line was pasted into.
func TestPS1RunsNothingUntilItsLastLine(t *testing.T) {
	t.Parallel()
	lines := strings.Split(strings.TrimSuffix(readPS1(t), "\n"), "\n")
	if last := lines[len(lines)-1]; last != "Install-Koko" {
		t.Fatalf("install.ps1 ends with %q, want the call of Install-Koko", last)
	}
	inside := false
	for n, line := range lines[:len(lines)-1] {
		switch {
		case line == "function Install-Koko {":
			inside = true
		case line == "}":
			inside = false
		case !inside && line != "" && !strings.HasPrefix(line, "#"):
			t.Errorf("install.ps1 line %d is outside Install-Koko:\n%s", n+1, line)
		case inside && slices.Contains(strings.Fields(line), "exit"):
			t.Errorf("install.ps1 line %d calls exit:\n%s", n+1, line)
		}
	}
}

// The fake machine stands in for the cmdlets install.ps1 reaches the
// network and the registry through. Another way there, a different cmdlet
// or .NET itself, would reach the real ones from the tests, so none is in
// its text; and the machine's PATH is named nowhere.
func TestPS1ReachesTheNetworkAndTheRegistryOnlyThroughWhatItsTestsFake(t *testing.T) {
	t.Parallel()
	script := readPS1(t)
	for n, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		for _, other := range []string{
			"irm ", "iwr ", "Invoke-RestMethod", "WebClient", "HttpClient", "WebRequest]", "Start-BitsTransfer", "curl",
			"Win32.Registry]", "EnvironmentVariable(", "HKLM", "Registry::", "Get-ItemProperty", "New-ItemProperty", "Set-Item ", "reg.exe", "setx",
		} {
			if strings.Contains(line, other) {
				t.Errorf("install.ps1 line %d uses %s, which its fake machine does not stand in for:\n%s", n+1, strings.TrimSpace(other), line)
			}
		}
	}
	for _, faked := range []string{"Invoke-WebRequest -Uri ", "Get-Item -LiteralPath 'HKCU:\\Environment'", "Set-ItemProperty -LiteralPath 'HKCU:\\Environment'"} {
		if !strings.Contains(script, faked) {
			t.Errorf("install.ps1 no longer calls %q: its fake machine must follow it", faked)
		}
	}
}

// The file install.ps1 installs is the one koko update asks for on Windows.
func TestPS1InstallsTheAssetKokoUpdateAsksFor(t *testing.T) {
	t.Parallel()
	if want := "$asset = '" + sharedrelease.KokoAsset("windows/amd64") + "'"; !strings.Contains(readPS1(t), want) {
		t.Errorf("install.ps1 does not say %s", want)
	}
}

// make dist builds the koko of every release platform under the name
// release.KokoAsset gives it, which koko update asks for, with both install
// scripts, and SHA256SUMS lists them all.
func TestMakeDistNamesEachKokoAsKokoUpdateAsksForIt(t *testing.T) {
	t.Parallel()
	skip.OnWindows(t, "the Makefile needs GNU Make, which the Windows runner's image does not have")
	cmd := exec.CommandContext(t.Context(), "make", "-n", "dist")
	cmd.Dir = ".."
	// As asked for here, not as a make that runs the tests was.
	cmd.Env = append(os.Environ(), "MAKEFLAGS=", "MFLAGS=")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("make -n dist: %v\n%s", err, out)
	}
	var listed []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if names, ok := strings.CutPrefix(line, "cd dist && sha256sum "); ok {
			names, _, _ = strings.Cut(names, " >")
			listed = strings.Fields(names)
		}
	}
	var kokos []string
	for _, name := range listed {
		platform, isKoko := strings.CutPrefix(strings.TrimSuffix(name, ".exe"), "koko-")
		if !isKoko {
			continue
		}
		kokos = append(kokos, name)
		platform = strings.Replace(platform, "-", "/", 1)
		if want := sharedrelease.KokoAsset(platform); name != want {
			t.Errorf("make dist names the koko of %s %s, and koko update asks for %s", platform, name, want)
		}
	}
	if want := []string{"koko-linux-amd64", "koko-linux-arm64", "koko-darwin-arm64", "koko-windows-amd64.exe"}; !slices.Equal(kokos, want) {
		t.Errorf("make dist builds koko as %v, want %v", kokos, want)
	}
	for _, name := range []string{"kokod-linux-amd64", "kokod-linux-arm64", "install.sh", "install.ps1"} {
		if !slices.Contains(listed, name) {
			t.Errorf("SHA256SUMS lists %v, want %s among them", listed, name)
		}
	}
	if !strings.Contains(string(out), "cp release/install.sh release/install.ps1 dist/") {
		t.Errorf("make dist does not copy both install scripts into dist:\n%s", out)
	}
}
