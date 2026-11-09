package installer

import (
	_ "embed"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/cieons/koko/internal/shared/wire"
)

// unitTemplate is the koko service's systemd unit. The startup settings are
// Environment= lines, not flags, so that a drop-in's own Environment= lines
// override them and outlast the next install, which rewrites this unit.
//
//go:embed koko.service.tmpl
var unitTemplate string

var unit = template.Must(template.New("koko.service").Option("missingkey=error").Parse(unitTemplate))

// rootKeyCredential is the name systemd gives the Root Key's file in the
// service's $CREDENTIALS_DIRECTORY, where `kokod run` reads it by
// default.
const rootKeyCredential = "root-key"

// UnitSettings are the startup settings install writes into the unit.
type UnitSettings struct {
	// Port is the TCP port the koko server listens on, on every address.
	Port int
}

// Unit renders the koko service's unit with s.
func Unit(s UnitSettings) string {
	var b strings.Builder
	err := unit.Execute(&b, map[string]any{
		"Binary":      BinaryPath,
		"DataDir":     DataDir,
		"Port":        s.Port,
		"Credential":  rootKeyCredential,
		"RootKeyFile": RootKeyFile,
		"User":        User,
		// systemd makes these under /var/lib and /run, with the unit's
		// modes and its user as their owner.
		"StateDirectory":   strings.TrimPrefix(DataDir, "/var/lib/"),
		"RuntimeDirectory": strings.TrimPrefix(filepath.Dir(wire.SocketPath), "/run/"),
	})
	if err != nil {
		panic("installer: the unit template does not render: " + err.Error())
	}
	return b.String()
}

// listenEnv is the variable the unit sets the koko server's listen address
// with, kokod run's --listen.
const listenEnv = "KOKO_LISTEN"

// envPort reads the port of the koko server's listen address from env, the
// unit's environment as systemctl show prints it: the last KOKO_LISTEN
// wins. It returns 0 when env sets none, and an error when the address
// names no port.
func envPort(env string) (int, error) {
	return listenPort(assignments(env))
}

// dropInPort reads the port of the koko server's listen address that a
// drop-in's text sets in its Environment= lines, the last KOKO_LISTEN
// winning, as envPort does; comments are not Environment= lines.
func dropInPort(text string) (int, error) {
	var assigned []string
	for line := range strings.Lines(text) {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Environment="); ok {
			assigned = append(assigned, assignments(v)...)
		}
	}
	return listenPort(assigned)
}

// listenPort returns the port of the last KOKO_LISTEN among assigned, 0 when
// there is none, or an error when its address names no port.
func listenPort(assigned []string) (int, error) {
	listen, found := "", false
	for _, kv := range assigned {
		if v, ok := strings.CutPrefix(kv, listenEnv+"="); ok {
			listen, found = v, true
		}
	}
	if !found {
		return 0, nil
	}
	_, digits, _ := strings.CutLast(listen, ":")
	port, err := strconv.Atoi(digits)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("listen on %q, which names no port", listen)
	}
	return port, nil
}

// assignments splits s, the value of an Environment= setting or systemctl
// show's Environment, into its VAR=value assignments: separated by blanks,
// and any part of one in double or single quotes, which systemd uses for
// one holding a blank or another character a shell would take.
func assignments(s string) []string {
	var out []string
	var b strings.Builder
	var quote rune
	word := false
	for _, r := range s {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			b.WriteRune(r)
		case r == '"' || r == '\'':
			quote, word = r, true
		case r == ' ' || r == '\t':
			if word {
				out = append(out, b.String())
				b.Reset()
				word = false
			}
		default:
			b.WriteRune(r)
			word = true
		}
	}
	if word {
		out = append(out, b.String())
	}
	return out
}
