package installer

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/cieons/koko/internal/shared/api"
)

// The upgrade helper's files (ADR 0048), as the machine names them, before
// the Machine's root is prefixed: kokod reads them, and the helper, run as
// root, writes them.
const (
	// UpgradePathUnit is the helper's path unit, which kokod install writes
	// with the koko unit; while it is there, the koko server upgrades
	// through the helper.
	UpgradePathUnit = "/etc/systemd/system/" + UpgradePathName
	// UpgradeServiceUnit is the helper's oneshot unit, which runs kokod
	// upgrade as root.
	UpgradeServiceUnit = "/etc/systemd/system/" + UpgradeServiceName
	// UpgradePathName and UpgradeServiceName are the helper's units' names.
	UpgradePathName    = "koko-upgrade.path"
	UpgradeServiceName = "koko-upgrade.service"
	// RequestFile is the koko server's request for an upgrade, which it
	// writes in its runtime directory, beside its socket, and whose
	// presence starts the helper.
	RequestFile = "/run/koko/upgrade.request"
	// UpgradeDir is root's directory of the helper's files, mode 0755.
	UpgradeDir = "/var/lib/koko-upgrade"
	// OutcomeFile is the outcome of the last upgrade, mode 0644, which the
	// helper writes, a temporary file renamed into place, and kokod reads.
	OutcomeFile = UpgradeDir + "/outcome.json"
	// previousFile is the kokod the helper replaced, which it installs again
	// when the new one does not start.
	previousFile = UpgradeDir + "/kokod.previous"
)

// The states of an upgrade, as Outcome.State names them: the API's.
const (
	UpgradeRunning           = api.UpgradeRunning
	UpgradeSucceeded         = api.UpgradeSucceeded
	UpgradeFailed            = api.UpgradeFailed
	UpgradeFailedNotRestored = api.UpgradeFailedNotRestored
)

// Outcome is the outcome of the last upgrade, as outcome.json holds it.
type Outcome struct {
	// State is one of UpgradeRunning, UpgradeSucceeded, UpgradeFailed and
	// UpgradeFailedNotRestored.
	State string `json:"state"`
	// From and To are the versions of kokod before the upgrade and the one
	// it asked for, such as v0.4.0 and v0.5.0.
	From string `json:"from"`
	To   string `json:"to"`
	// StartedAt and FinishedAt are when the helper took the request and
	// when it finished, RFC 3339 in UTC; FinishedAt is empty while running.
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at,omitempty"`
	// Message says why the upgrade failed, empty when it succeeded.
	Message string `json:"message,omitempty"`
}

// ReadOutcome reads the outcome of the last upgrade from file, and returns
// nil when there is none. A file that does not read or parse is an error:
// what it says is unknown, not that no upgrade ran.
func ReadOutcome(file string) (*Outcome, error) {
	b, err := os.ReadFile(file)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("read the outcome of the last upgrade: %w", err)
	}
	var o Outcome
	if err := json.Unmarshal(b, &o); err != nil {
		return nil, fmt.Errorf("read the outcome of the last upgrade in %s: %w", file, err)
	}
	return &o, nil
}
