package cli

import (
	"runtime"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/client/devicefiles"
)

// The home directory is the value of one variable, the OS's own: HOME on
// Linux and macOS, and USERPROFILE on Windows, where HOME is never read,
// whatever it says (ADR 0051). The last entry of the name wins, as for any
// variable, and Windows ignores the name's case.
func TestTheHomeIsReadFromTheVariableOfTheOS(t *testing.T) {
	t.Parallel()
	variable, other := "HOME", "USERPROFILE"
	if runtime.GOOS == "windows" {
		variable, other = other, variable
	}
	if devicefiles.HomeVar != variable {
		t.Fatalf("the home's variable is %s on %s, want %s", devicefiles.HomeVar, runtime.GOOS, variable)
	}
	inv := Invocation{Env: []string{variable + "=first", other + "=elsewhere", variable + "=home"}}
	if got := inv.home(); got != "home" {
		t.Errorf("the home is %q, want %q, the last value of %s", got, "home", variable)
	}
	inv = Invocation{Env: []string{other + "=elsewhere"}}
	if got := inv.home(); got != "" {
		t.Errorf("the home is %q with only %s set, want none: koko never reads it on %s", got, other, runtime.GOOS)
	}

	inv = Invocation{Env: []string{"UserProfile=home"}}
	want := map[bool]string{true: "home", false: ""}[runtime.GOOS == "windows"]
	if got := inv.home(); got != want {
		t.Errorf("the home is %q with UserProfile set, want %q on %s", got, want, runtime.GOOS)
	}
}

// A home that is not an absolute path is refused naming the variable of
// the OS, and koko skill install without one names it too.
func TestARefusedHomeNamesTheVariableOfTheOS(t *testing.T) {
	t.Parallel()
	_, err := devicefiles.WriteSSHConfig("relative", nil)
	if want := devicefiles.HomeVar + ` is "relative", not an absolute path`; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("a relative home is refused with %v, want it to say %q", err, want)
	}

	var stderr strings.Builder
	r := Runner{}
	status := r.Run(t.Context(), Invocation{Args: []string{"skill", "install"}, Stdin: strings.NewReader(""), Stdout: &strings.Builder{}, Stderr: &stderr})
	if want := devicefiles.HomeVar + " is not set"; status != exitRefused || !strings.Contains(stderr.String(), want) {
		t.Errorf("koko skill install without a home exits %d saying %q, want %d and %q", status, stderr.String(), exitRefused, want)
	}
}
