package installer

import (
	"strings"
	"testing"
)

// unitEnvironment returns the unit's Environment= assignments as systemctl
// show prints them, separated by spaces.
func unitEnvironment(unit string) string {
	var env []string
	for line := range strings.Lines(unit) {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Environment="); ok {
			env = append(env, v)
		}
	}
	return strings.Join(env, " ")
}

// An upgrade reads the port the koko service listens on from its
// environment as systemd shows it: the port of the unit install wrote, the
// last KOKO_LISTEN winning, none when it sets none, and an error for an
// address without a port.
func TestEnvPortReadsTheService(t *testing.T) {
	for _, port := range []int{1, 443, 8443, 65535} {
		if got, err := envPort(unitEnvironment(Unit(UnitSettings{Port: port}))); err != nil || got != port {
			t.Errorf("the unit written for port %d reads as port %d (%v)", port, got, err)
		}
	}
	for env, want := range map[string]int{
		"KOKO_LISTEN=0.0.0.0:8443":                           8443,
		`KOKO_DATA_DIR=/var/lib/koko "KOKO_LISTEN=[::]:443"`: 443,
		"KOKO_LISTEN=:443 KOKO_LISTEN=:8443":                 8443,
		`"NOTE=a b" KOKO_LISTEN=:8443`:                       8443,
		"":                                                   0,
		"KOKO_DATA_DIR=/var/lib/koko":                        0,
	} {
		if got, err := envPort(env); err != nil || got != want {
			t.Errorf("%q reads as port %d (%v), want %d", env, got, err, want)
		}
	}
	for _, env := range []string{"KOKO_LISTEN=:https", "KOKO_LISTEN=:0", "KOKO_LISTEN=:443 KOKO_LISTEN="} {
		if got, err := envPort(env); err == nil {
			t.Errorf("%q reads as port %d, want an error", env, got)
		}
	}
}

// An upgrade reads the port a drop-in sets from its Environment= lines,
// in any of the forms systemd takes, and never from a comment.
func TestDropInPortReadsEnvironmentLines(t *testing.T) {
	for text, want := range map[string]int{
		"[Service]\nEnvironment=KOKO_LISTEN=:8443\n":                               8443,
		"[Service]\nEnvironment=KOKO_LOG_LEVEL=debug KOKO_LISTEN=:8443\n":          8443,
		"[Service]\nEnvironment=\"KOKO_LISTEN=[::]:8443\"\n":                       8443,
		"[Service]\n  Environment='KOKO_LISTEN=0.0.0.0:8443'\n":                    8443,
		"[Service]\n#Environment=KOKO_LISTEN=:9443\n":                              0,
		"[Service]\n; Environment=KOKO_LISTEN=:9443\n":                             0,
		"[Service]\nEnvironment=KOKO_LOG_LEVEL=debug\n":                            0,
		"[Service]\nEnvironment=KOKO_LISTEN=:443\nEnvironment=KOKO_LISTEN=:8443\n": 8443,
	} {
		if got, err := dropInPort(text); err != nil || got != want {
			t.Errorf("%q reads as port %d (%v), want %d", text, got, err, want)
		}
	}
	if got, err := dropInPort("[Service]\nEnvironment=KOKO_LISTEN=:https\n"); err == nil {
		t.Errorf("a drop-in listening on :https reads as port %d, want an error", got)
	}
}

// The web terminal's emulator runs under wazero's compiler, which writes
// machine code to memory and then executes it (internal/server/vt, ADR
// 0058). MemoryDenyWriteExecute= refuses that, and the interpreter left
// would be about 100 times slower, so the unit never sets it, nor a
// system call filter that could take mprotect away.
func TestTheUnitLetsTheEmulatorCompile(t *testing.T) {
	unit := Unit(UnitSettings{Port: 7822})
	for _, setting := range []string{"MemoryDenyWriteExecute=", "SystemCallFilter="} {
		if strings.Contains(unit, setting) {
			t.Errorf("the unit sets %s, which keeps wazero's compiler from running", setting)
		}
	}
}
