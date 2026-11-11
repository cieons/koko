package cli

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/build"
	"github.com/cieons/koko/internal/shared/compat"
	"github.com/cieons/koko/internal/shared/install"
)

var serverUpgradeCommand = Command{
	Noun: "server",
	Verb: "upgrade",
	Summary: "Upgrade the koko server in place to the latest release, or the one --version names, through its upgrade helper, " +
		"then this koko to the same release; the koko server restarts, and every open SSH Session through it ends",
	Flags: []Flag{
		{Name: "check", Bool: true, Summary: "check for a new release at once and say what the koko server would upgrade to, and change nothing"},
		{Name: "version", Value: "vX.Y.Z", Summary: "the release to upgrade to, such as v0.5.0, newer than the koko server's, in place of the latest"},
	},
	// It restarts the koko server, which ends every open SSH Session (ADR
	// 0048); --check only lowers its risk, so the path keeps it here.
	Tier:      Irreversible,
	OwnerOnly: true,
	Op:        api.ServerUpgrade,
	Run:       action(serverUpgrade),
}

// The wait for the koko server's upgrade: its outcome is read every
// upgradePoll, through the restart, for at most upgradeWait, which covers
// the helper's 10-minute download and its two installs.
const (
	upgradeWait = 15 * time.Minute
	upgradePoll = time.Second
)

// codeUpgradeFailed: the koko server's upgrade failed, or did not end in
// time; the message says why, and whether the previous kokod runs again.
// The CLI was left as it was. kokod upgrade reports it too, so it is
// install's.
const codeUpgradeFailed = install.CodeUpgradeFailed

// serverUpgrade is koko server upgrade: with --check, the koko server's
// check at once; otherwise the upgrade, its outcome waited for, then this
// koko brought to the same release.
func serverUpgrade(ctx context.Context, in *Input) (serverUpgradeJSON, error) {
	tag := in.Flag("version")
	if in.Bool("check") {
		if tag != "" {
			return serverUpgradeJSON{}, usagef("--check takes no --version: it says what the koko server would upgrade to, the latest release")
		}
		return checkServerUpgrade(ctx, in)
	}
	if tag != "" && !(build.Info{Version: tag}).Release() {
		return serverUpgradeJSON{}, usagef("--version is %q, and it takes a release, such as v0.5.0", tag)
	}
	before, err := in.Call(ctx, api.ServerShow, api.ServerShowRequest{})
	if err != nil {
		return serverUpgradeJSON{}, err
	}
	asked, err := in.Call(ctx, api.ServerUpgrade, api.ServerUpgradeRequest{Version: tag})
	if err != nil {
		return serverUpgradeJSON{}, err
	}
	// For people: with --json, stderr holds an error object alone, and the
	// result names both versions.
	if !in.line.json {
		_, _ = fmt.Fprintf(in.inv.Stderr, "Asked the koko server to upgrade from %s to %s. It restarts, and every open SSH Session through it ends.\n",
			asked.From, asked.To)
	}
	o, err := in.waitUpgrade(ctx, before.Server.LastUpgrade, asked.To)
	if err != nil {
		return serverUpgradeJSON{}, err
	}
	out := serverUpgradeJSON{Server: serverUpgradeOutcome{From: o.From, To: o.To, State: o.State, Message: o.Message}}
	if o.State != api.UpgradeSucceeded {
		return serverUpgradeJSON{}, &cliError{code: codeUpgradeFailed, message: fmt.Sprintf(
			"the koko server's upgrade from %s to %s failed (%s): %s; this koko was left as it was", o.From, o.To, o.State, orNone(o.Message, "no reason recorded"))}
	}
	var note string
	out.CLI, note = in.followServer(ctx, o.To)
	// On success stderr holds no error object, so the note goes there with
	// --json too, for an agent to read why and the line.
	if note != "" {
		_, _ = fmt.Fprintln(in.inv.Stderr, note)
	}
	return out, nil
}

// waitUpgrade reads server.show's last_upgrade until the upgrade to asked
// for is final: an outcome other than before, the one there when it asked,
// that no longer runs. The calls that fail while the koko server restarts
// are tried again, until upgradeWait has passed.
func (in *Input) waitUpgrade(ctx context.Context, before *api.LastUpgrade, to string) (api.LastUpgrade, error) {
	deadline := time.Now().Add(upgradeWait)
	for {
		resp, err := in.Call(ctx, api.ServerShow, api.ServerShowRequest{})
		if err == nil {
			if o := resp.Server.LastUpgrade; o != nil && !sameOutcome(o, before) && o.To == to && o.State != api.UpgradeRunning {
				return *o, nil
			}
		} else {
			in.log.Debug("the koko server did not answer while it upgrades", "err", err)
		}
		if ctx.Err() != nil {
			return api.LastUpgrade{}, ctx.Err()
		}
		if time.Now().After(deadline) {
			return api.LastUpgrade{}, &cliError{code: codeUpgradeFailed, message: fmt.Sprintf(
				"the koko server's upgrade to %s did not end within %v: see why on its machine with journalctl -u koko -u koko-upgrade, and its outcome with koko server show; this koko was left as it was",
				to, upgradeWait)}
		}
		select {
		case <-ctx.Done():
			return api.LastUpgrade{}, ctx.Err()
		case <-time.After(upgradePoll):
		}
	}
}

// sameOutcome tells whether a and b are the same outcome of an upgrade.
func sameOutcome(a, b *api.LastUpgrade) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.State == b.State && a.From == b.From && a.To == b.To && a.StartedAt == b.StartedAt &&
		orNone(a.FinishedAt, "") == orNone(b.FinishedAt, "")
}

// followServer brings this koko to the release the koko server now runs,
// to, by koko update's steps, when it is older and koko update may replace
// it. It returns what koko update did, or nil and why it did nothing, with
// the line that does it.
func (in *Input) followServer(ctx context.Context, to string) (*updateJSON, string) {
	k := in.runner.Kokod
	if k == nil {
		return nil, "this koko cannot read its own build"
	}
	self := k.Self()
	if !compat.Newer(to, self.Version) {
		return nil, fmt.Sprintf("This koko is %s, which works with the koko server as it is.", self.Version)
	}
	t, err := in.updateTarget(k)
	if err != nil {
		return nil, fmt.Sprintf("This koko stays %s: %v. Bring it to %s the way it was installed, or with koko update --version %s where koko update replaces it.",
			self.Version, strings.TrimSuffix(errorMessage(err), nothingChanged), to, to)
	}
	u, err := in.updateTo(ctx, k, t, to)
	if err != nil {
		sudo := ""
		if k.RunsAsRoot() {
			sudo = "sudo "
		}
		return nil, fmt.Sprintf("This koko stays %s: %v. Bring it to %s with %skoko update --version %s.",
			self.Version, strings.TrimSuffix(errorMessage(err), nothingChanged), to, sudo, to)
	}
	return &u, ""
}

// errorMessage is err's message as the CLI prints it: a cliError's own.
func errorMessage(err error) string {
	if e, ok := errors.AsType[*cliError](err); ok {
		return e.message
	}
	return err.Error()
}

// checkServerUpgrade is koko server upgrade --check: the koko server checks
// for a new release at once, and says how it upgrades. It changes nothing.
func checkServerUpgrade(ctx context.Context, in *Input) (serverUpgradeJSON, error) {
	checked, err := in.Call(ctx, api.ServerCheckUpdate, api.ServerCheckUpdateRequest{})
	if err != nil {
		return serverUpgradeJSON{}, err
	}
	shown, err := in.Call(ctx, api.ServerShow, api.ServerShowRequest{})
	if err != nil {
		return serverUpgradeJSON{}, err
	}
	c := serverCheckJSON{
		Version:    checked.Version,
		Latest:     checked.UpdateCheck.Latest,
		CheckedAt:  checked.UpdateCheck.CheckedAt,
		UpgradeWay: shown.Server.UpgradeWay,
	}
	c.UpdateAvailable = c.Latest != nil && compat.Newer(*c.Latest, c.Version)
	return serverUpgradeJSON{check: &c}, nil
}

// serverUpgradeJSON is what koko server upgrade prints: the koko server's
// upgrade and this koko's, or with --check what the check found.
type serverUpgradeJSON struct {
	// Server is the outcome of the koko server's upgrade.
	Server serverUpgradeOutcome `json:"server"`
	// CLI is what koko update did to this koko, null when it did nothing.
	CLI *updateJSON `json:"cli"`
	// check is what --check found, which it prints in place of the rest.
	check *serverCheckJSON
}

// serverUpgradeOutcome is the outcome of the koko server's upgrade.
type serverUpgradeOutcome struct {
	From    string  `json:"from"`
	To      string  `json:"to"`
	State   string  `json:"state"`
	Message *string `json:"message"`
}

// serverCheckJSON is koko server upgrade --check's --json.
type serverCheckJSON struct {
	// Version is the koko server's version.
	Version string `json:"version"`
	// Latest is the latest release the check found, null while unknown.
	Latest *string `json:"latest"`
	// CheckedAt is when the check ended, RFC 3339 in UTC.
	CheckedAt *string `json:"checked_at"`
	// UpdateAvailable says Latest is newer than Version.
	UpdateAvailable bool `json:"update_available"`
	// UpgradeWay is how the koko server upgrades: "systemd", "image" or
	// "none".
	UpgradeWay string `json:"upgrade_way"`
}

func (u serverUpgradeJSON) MarshalJSONTo(enc *jsontext.Encoder) error {
	if u.check != nil {
		return json.MarshalEncode(enc, u.check)
	}
	return json.MarshalEncode(enc, struct {
		Server serverUpgradeOutcome `json:"server"`
		CLI    *updateJSON          `json:"cli"`
	}{u.Server, u.CLI})
}

// jsonShape is what koko skill documents: the upgrade's outcome, or what
// --check found.
func (serverUpgradeJSON) jsonShape() string {
	type upgraded struct {
		Server serverUpgradeOutcome `json:"server"`
		CLI    *updatedJSON         `json:"cli"`
	}
	return shape(reflect.TypeFor[upgraded]()) + " | " + shape(reflect.TypeFor[serverCheckJSON]())
}

func (u serverUpgradeJSON) Human(w io.Writer) error {
	var b strings.Builder
	if c := u.check; c != nil {
		server := serverJSON{Version: c.Version, UpgradeWay: c.UpgradeWay, UpdateCheck: updateCheckJSON{Latest: c.Latest}}
		latest := "unknown"
		if c.Latest != nil {
			latest = *c.Latest
			if c.CheckedAt != nil {
				at := *c.CheckedAt
				if t, err := time.Parse(time.RFC3339, at); err == nil {
					at = t.UTC().Format("2006-01-02 15:04 UTC")
				}
				latest += ", checked at " + at
			}
		}
		fmt.Fprintf(&b, "koko server:  %s\nLatest:       %s\nUpgrade:      %s\n", c.Version, latest, server.upgradeLine())
		switch {
		case !c.UpdateAvailable:
			b.WriteString("The koko server runs the latest release.\n")
		case c.UpgradeWay == api.UpgradeWaySystemd:
			fmt.Fprintf(&b, "%s is available: koko server upgrade installs it.\n", *c.Latest)
		default:
			fmt.Fprintf(&b, "%s is available.\n", *c.Latest)
		}
		_, err := io.WriteString(w, b.String())
		return err
	}
	fmt.Fprintf(&b, "The koko server runs %s, upgraded from %s.\n", u.Server.To, u.Server.From)
	if u.CLI != nil {
		if err := u.CLI.Human(&b); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
