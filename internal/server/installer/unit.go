package installer

import (
	_ "embed"
	"path/filepath"
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
// service's $CREDENTIALS_DIRECTORY, where `koko server run` reads it by
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
