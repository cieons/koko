package cli

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/cieons/koko/internal/client/devicefiles"
	"github.com/cieons/koko/internal/shared/build"
	"github.com/cieons/koko/internal/shared/release"
)

var updateCommands = []Command{
	{
		Verb: "update",
		Summary: "Update this koko, the CLI alone, to the latest release, or to the release --version names, " +
			"older or newer; it never contacts or changes the koko server",
		Flags: []Flag{
			{Name: "check", Bool: true, Summary: "say which release koko update would install, and where, and change nothing"},
			{Name: "version", Value: "vX.Y.Z", Summary: "the release to install, such as v0.4.0, in place of the latest"},
		},
		Tier: Reversible,
		Run:  action(update),
	},
}

// The two places koko update replaces a koko (ADR 0047): install.sh's,
// for a user other than root, under their HOME, and root's, for every user
// of the machine.
const (
	userKokoDir = ".local/bin"
	rootKokoDir = "/usr/local/bin"
)

// firstWithUpdate is the minor version of v0 whose release is the first
// with koko update: going back to an earlier one leaves the install line as
// the way back up.
const firstWithUpdate = 4

// nothingChanged ends the message of every refusal and failure of koko
// update before its rename.
const nothingChanged = "; nothing was changed"

// codeUpdateUnsupported: koko update does not replace this koko, which it
// did not install: a development build, the image's, root's run by another
// user, or a file anywhere else; nothing was changed.
const codeUpdateUnsupported = "update_unsupported"

// updateTarget is the koko file koko update replaces, as the running koko
// is.
type updateTarget struct {
	// file is its real path, its directory's symlinks resolved, which koko
	// update renames over.
	file string
	// path is its path as the user knows it: under HOME, or
	// /usr/local/bin/koko.
	path string
	// shown is path as koko update prints it, with ~ for HOME.
	shown string
}

// target returns the file koko update may replace: the running koko,
// symlinks resolved, when it is a release at $HOME/.local/bin/koko run by a
// user other than root, or at /usr/local/bin/koko run by root, and not the
// image's. Anything else is refused with update_unsupported, naming the way
// to upgrade it instead.
func (in *Input) updateTarget(k Kokod) (updateTarget, error) {
	self := k.Self()
	refuse := func(format string, args ...any) error {
		return &cliError{code: codeUpdateUnsupported, message: fmt.Sprintf(format, args...) + nothingChanged}
	}
	if !self.Release() {
		return updateTarget{}, refuse("this koko is a development build (%s): build it again from the source", self.Version)
	}
	if in.inv.getenv("KOKO_IMAGE") != "" {
		return updateTarget{}, refuse("this koko is the container image's: upgrade the image (KOKO_VERSION in its Compose file)")
	}
	if in.runner.Executable == nil {
		return updateTarget{}, errors.New("this koko cannot find its own file")
	}
	exe, err := in.runner.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return updateTarget{}, fmt.Errorf("find this koko's own file: %w", err)
	}
	root := filepath.Join(rootKokoDir, "koko")
	if exe == resolvedFile(k.Path(rootKokoDir), "koko") {
		if !k.RunsAsRoot() {
			return updateTarget{}, refuse("this koko is root's, at %s: run sudo koko update, or install koko for this user with %s", root, release.InstallLine)
		}
		return updateTarget{file: exe, path: root, shown: root}, nil
	}
	if home := in.inv.getenv("HOME"); filepath.IsAbs(home) && !k.RunsAsRoot() &&
		exe == resolvedFile(filepath.Join(home, userKokoDir), "koko") {
		return updateTarget{file: exe, path: filepath.Join(home, userKokoDir, "koko"), shown: "~/" + userKokoDir + "/koko"}, nil
	}
	return updateTarget{}, refuse("koko update replaces only the koko install.sh put in ~/%s, or root's in %s; this koko is %s: upgrade it the way it was installed",
		userKokoDir, rootKokoDir, exe)
}

// resolvedFile is the path of name in dir, with dir's symlinks resolved
// when it is there.
func resolvedFile(dir, name string) string {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	return filepath.Join(dir, name)
}

// update is koko update: it replaces this koko with the latest release's,
// or the one --version names, downloaded and checked, then points koko's
// ssh_config at it. It needs no join and never contacts the koko server.
func update(ctx context.Context, in *Input) (updateJSON, error) {
	tag := in.Flag("version")
	if tag != "" && !(build.Info{Version: tag}).Release() {
		return updateJSON{}, usagef("--version is %q, and it takes a release, such as v0.4.0", tag)
	}
	k := in.runner.Kokod
	if k == nil {
		return updateJSON{}, errors.New("this koko has no way to read its own build")
	}
	t, err := in.updateTarget(k)
	if err != nil {
		return updateJSON{}, err
	}
	src := in.releases(k)
	if tag == "" {
		if tag, err = src.Latest(ctx); err != nil {
			return updateJSON{}, downloadFailed(err, nothingChanged)
		}
	}
	self := k.Self().Version
	out := updateJSON{check: in.Bool("check"), path: t.path, shown: t.shown, from: self, to: tag}
	if out.check || tag == self {
		return out, nil
	}

	dir, err := os.MkdirTemp(filepath.Dir(t.file), ".koko-update-") // mode 0700
	if err != nil {
		return updateJSON{}, fmt.Errorf("make a directory beside %s to download koko into: %w", t.path, err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	want := release.Want{Info: build.Info{Path: release.KokoPackage, Version: tag, Platform: k.Platform()}}
	file, err := src.Fetch(ctx, tag, release.KokoAsset(want.Platform), dir, want)
	if err != nil {
		return updateJSON{}, downloadFailed(err, nothingChanged)
	}
	if err := os.Chmod(file, 0o755); err != nil {
		return updateJSON{}, fmt.Errorf("make %s runnable: %w", file, err)
	}
	if err := os.Rename(file, t.file); err != nil {
		return updateJSON{}, fmt.Errorf("replace %s: %w%s", t.path, err, nothingChanged)
	}
	out.updated = true
	if _, err := devicefiles.PointProxyCommand(in.inv.getenv("HOME"), t.path); err != nil {
		in.log.Debug("koko update left koko's ssh_config as it was", "err", err)
	}
	return out, nil
}

// pointSSHAtSelf points koko's ssh_config's ProxyCommand at this koko, for
// a command run from the user's shell, when this koko is one koko update
// would replace, so that a koko at an older path, such as a v0.3.x
// /usr/local/bin/koko, can be removed once this one has run (ADR 0047).
// Neither koko tunnel nor the Device Mux does, whose path may be an old
// build's. It costs a file read, and a failure changes nothing.
func (in *Input) pointSSHAtSelf() {
	k := in.runner.Kokod
	if k == nil || in.runner.Executable == nil || !k.Self().Release() {
		return
	}
	t, err := in.updateTarget(k)
	if err != nil {
		return
	}
	if _, err := devicefiles.PointProxyCommand(in.inv.getenv("HOME"), t.path); err != nil {
		in.log.Debug("koko left koko's ssh_config's ProxyCommand as it was", "err", err)
	}
}

// updateJSON is what koko update prints: what it did, or with --check what
// it would do.
type updateJSON struct {
	check bool
	// path is the koko file koko update replaces, and shown the same as
	// it prints it for people.
	path, shown string
	// from is this koko's version, and to the release it installs.
	from, to string
	updated  bool
}

// updatedJSON is koko update's --json.
type updatedJSON struct {
	// Path is the koko file koko update replaces.
	Path string `json:"path"`
	// From is the version that was installed there, and To the one now.
	From string `json:"from"`
	To   string `json:"to"`
	// Updated says it replaced the file: false when To was installed
	// already.
	Updated bool `json:"updated"`
}

// checkedJSON is koko update --check's --json.
type checkedJSON struct {
	// Path is the koko file koko update would replace.
	Path string `json:"path"`
	// Installed is its version, and Target the release koko update would
	// install.
	Installed string `json:"installed"`
	Target    string `json:"target"`
	// UpdateAvailable says koko update would install Target.
	UpdateAvailable bool `json:"update_available"`
}

func (u updateJSON) MarshalJSONTo(enc *jsontext.Encoder) error {
	if u.check {
		return json.MarshalEncode(enc, checkedJSON{Path: u.path, Installed: u.from, Target: u.to, UpdateAvailable: u.from != u.to})
	}
	return json.MarshalEncode(enc, updatedJSON{Path: u.path, From: u.from, To: u.to, Updated: u.updated})
}

// jsonShape is what koko skill documents: what koko update did, or what it
// would do with --check.
func (updateJSON) jsonShape() string {
	return shape(reflect.TypeFor[updatedJSON]()) + " | " + shape(reflect.TypeFor[checkedJSON]())
}

func (u updateJSON) Human(w io.Writer) error {
	var b strings.Builder
	switch {
	case u.from == u.to:
		fmt.Fprintf(&b, "koko %s is installed at %s.\n", u.to, u.shown)
	case u.check:
		fmt.Fprintf(&b, "koko %s is installed at %s. koko update would install %s.\n", u.from, u.shown, u.to)
	default:
		fmt.Fprintf(&b, "Updated koko from %s to %s at %s.\n", u.from, u.to, u.shown)
		fmt.Fprintf(&b, "Commands from now on run %s. Open SSH Sessions, koko web and the %s Device Mux carry on, "+
			"and the Device Mux exits after 10 minutes idle.\n", u.to, u.from)
		if !hasUpdate(u.to) {
			fmt.Fprintf(&b, "%s has no koko update: to come back to a later release, run the install line again: %s\n", u.to, release.InstallLine)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// hasUpdate tells whether the release version has koko update: v0.4.0 and
// every later release.
func hasUpdate(version string) bool {
	major, rest, _ := strings.Cut(strings.TrimPrefix(version, "v"), ".")
	minor, _, _ := strings.Cut(rest, ".")
	ma, errMajor := strconv.Atoi(major)
	mi, errMinor := strconv.Atoi(minor)
	return errMajor != nil || errMinor != nil || ma > 0 || mi >= firstWithUpdate
}
