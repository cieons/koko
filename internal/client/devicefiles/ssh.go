package devicefiles

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/shared/name"
)

// koko's ssh integration (P0 spec: reach, "Device files and ssh
// integration"): its own ssh_config, which sends every SSH Name through
// koko tunnel, its known_hosts, and the one line in the user's ~/.ssh/config
// that includes the former.
const (
	// sshConfigFile is koko's own ssh_config, which koko rewrites whole.
	sshConfigFile = "ssh_config"
	// knownHostsFile holds the koko server's host keys under hostKeyAlias.
	knownHostsFile = "known_hosts"
	// hostKeyAlias is the name ssh looks the koko server's host key up
	// under, so that one known_hosts line covers every SSH Name and port.
	hostKeyAlias = "koko"
	// userConfig is the user's own ssh configuration, under the home
	// directory.
	userConfig = ".ssh/config"
	// maxLine is the longest line koko writes into an ssh_config, its
	// newline included: OpenSSH before 7.x read each line into a 1 KiB
	// buffer.
	maxLine = 1024
)

// IncludeLine is the line that makes ssh read koko's ssh_config. koko puts
// it first in ~/.ssh/config, so that koko's values win for its names and
// the user's file keeps its own scope after it. It keeps its "~", which
// OpenSSH expands.
const IncludeLine = "Include ~/" + kokoDir + "/" + sshConfigFile

// sshConfigHeader opens koko's ssh_config; a comment is no directive.
const sshConfigHeader = "# Written by koko; it rewrites this file. Your own entries go in ~/.ssh/config.\n"

// proxyCommand and proxyTunnel frame the double-quoted koko binary on the
// ProxyCommand line of koko's ssh_config, which sshConfig writes and
// proxyKoko reads back.
const (
	proxyCommand = "ProxyCommand "
	proxyTunnel  = " tunnel '%h'"
)

// WriteSSHConfig rewrites koko's ssh_config on the device whose home
// directory is home, whole and atomically, with mode 0600: a Host block for
// every *.koko name and the bare names given, in their order, the names of
// Hosts the User can see (ADR 0014), whose ProxyCommand runs this very koko
// binary as koko tunnel. A name list too long for one Host line is split
// across identical blocks, so that no line passes 1 KiB. Every keyword is
// one OpenSSH 7.3 knows, the release that added Include, since an unknown
// one breaks every ssh the user runs.
//
// A file that holds those bytes already, as a regular file with mode 0600,
// it leaves alone, so a bare-name refresh that finds the record as it was
// writes nothing; it tells whether it wrote. A file it cannot read counts
// as changed.
func WriteSSHConfig(home string, names []string) (wrote bool, err error) {
	if err := checkHome(home); err != nil {
		return false, err
	}
	b, err := renderSSHConfig(home, names)
	if err != nil {
		return false, err
	}
	return replaceChanged(filepath.Join(dir(home), sshConfigFile), b)
}

// SetUpSSH writes koko's ssh files for the OS user whose home directory is
// home on the koko server's own machine, where nothing joins, for koko ssh
// setup: it makes ~/.ssh (0700) when it is missing and ~/.ssh/koko private
// to this user, as a join does, writes koko's known_hosts with hostKeys,
// the host keys the Gateway presents, then koko's ssh_config with the bare
// names given, as WriteSSHConfig does. Written again, it rewrites both.
func SetUpSSH(home string, hostKeys []ssh.PublicKey, names []string) error {
	if err := checkHome(home); err != nil {
		return err
	}
	// Rendered first, so that a path ssh would misread writes nothing.
	b, err := renderSSHConfig(home, names)
	if err != nil {
		return err
	}
	if _, err := makeDir(home); err != nil {
		return err
	}
	if err := replace(filepath.Join(dir(home), knownHostsFile), knownHosts(hostKeys), 0o600); err != nil {
		return fmt.Errorf("write koko's %s: %w", knownHostsFile, err)
	}
	_, err = replaceChanged(SSHConfigPath(home), b)
	return err
}

// SSHConfigPath returns the path of koko's ssh_config for the OS user whose
// home directory is home.
func SSHConfigPath(home string) string {
	return filepath.Join(dir(home), sshConfigFile)
}

// HasSSHConfig tells whether koko's ssh_config may be there for the OS user
// whose home directory is home, as a join or koko ssh setup leaves it: over
// the Unix socket, the caller's own Host changes rewrite its bare names only
// then, so a command there never writes into a home that did not ask for
// koko's ssh files. Only a file that is not there, or a HOME koko never
// writes under, counts as none: one koko cannot look at may be there, and
// the rewrite then says why it failed.
func HasSSHConfig(home string) bool {
	if checkHome(home) != nil {
		return false
	}
	_, err := os.Lstat(SSHConfigPath(home))
	return !errors.Is(err, fs.ErrNotExist)
}

// AddressNames returns the bare names in koko's ssh_config, for the OS user
// whose home directory is home, that ssh reads as an IPv4 address, in the
// file's order: ssh <name> does not reach those Hosts through koko, since
// ssh connects with the address, such as 0.0.0.201 for 201, whatever
// HostName says, and only ssh <name>.koko does. A file that is not there,
// or a HOME koko never writes under, holds none, an empty list; one koko
// cannot read is an error, never none.
func AddressNames(home string) ([]string, error) {
	out := []string{}
	if checkHome(home) != nil {
		return out, nil
	}
	b, err := readRegular(SSHConfigPath(home), "koko's ssh_config")
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return out, nil
	case err != nil:
		return nil, err
	}
	for line := range strings.Lines(string(b)) {
		names, ok := strings.CutPrefix(strings.TrimSuffix(line, "\n"), "Host ")
		if !ok {
			continue
		}
		for n := range strings.FieldsSeq(names) {
			if readsAsIPv4(n) {
				out = append(out, n)
			}
		}
	}
	return out, nil
}

// readsAsIPv4 tells whether the name n, a lowercase DNS label, is an IPv4
// address written as one number, as the C library's inet_aton reads one,
// and with it the getaddrinfo ssh asks: decimal, octal after a 0, or hex
// after 0x, within 32 bits. A larger number is left out: glibc reads it as
// no address, though macOS's C library reads it modulo 2^32.
func readsAsIPv4(n string) bool {
	digits, base := n, 10
	switch {
	case strings.HasPrefix(n, "0x"):
		digits, base = n[2:], 16
	case strings.HasPrefix(n, "0"):
		base = 8
	}
	_, err := strconv.ParseUint(digits, base, 32)
	return err == nil
}

// RefreshSSHConfig rewrites koko's ssh_config as WriteSSHConfig does, for
// the Device Mux's bare-name refresh, but its ProxyCommand runs koko by the
// path the file names already, when it names one koko would write: which
// koko ssh runs is for a command run from the user's shell to choose, a
// join or a Host change, never for a process in the background, whose own
// path may be an old build's after an upgrade, or one found without the
// user's PATH. A file that names none, or cannot be read, gets this binary.
func RefreshSSHConfig(home string, names []string) (wrote bool, err error) {
	if err := checkHome(home); err != nil {
		return false, err
	}
	path := filepath.Join(dir(home), sshConfigFile)
	old, err := os.ReadFile(path)
	if err != nil {
		return WriteSSHConfig(home, names)
	}
	koko, ok := proxyKoko(old)
	if !ok {
		return WriteSSHConfig(home, names)
	}
	b, err := sshConfig(koko, filepath.Join(dir(home), knownHostsFile), names)
	if err != nil {
		return false, fmt.Errorf("write koko's ssh_config: %w", err)
	}
	return replaceChanged(path, b)
}

// replaceChanged writes b as koko's ssh_config at path, as replace does,
// unless path is a regular file of mode 0600 that holds b already, and
// tells whether it wrote. A file it cannot read counts as changed.
func replaceChanged(path string, b []byte) (bool, error) {
	if fi, err := os.Lstat(path); err == nil && fi.Mode().IsRegular() && fi.Mode().Perm() == 0o600 {
		if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, b) {
			return false, nil
		}
	}
	if err := replace(path, b, 0o600); err != nil {
		return false, fmt.Errorf("write koko's ssh_config: %w", err)
	}
	return true, nil
}

// proxyKoko returns the koko binary the ProxyCommand of koko's ssh_config b
// runs, and whether b names one as sshConfig writes it.
func proxyKoko(b []byte) (string, bool) {
	for line := range strings.Lines(string(b)) {
		arg, ok := strings.CutPrefix(line, "  "+proxyCommand)
		if !ok {
			continue
		}
		arg, ok = strings.CutSuffix(arg, proxyTunnel+"\n")
		koko := strings.TrimSuffix(strings.TrimPrefix(arg, `"`), `"`)
		if quoted, err := quotePath(koko); ok && err == nil && quoted == arg {
			return koko, true
		}
		return "", false
	}
	return "", false
}

// renderSSHConfig returns koko's ssh_config for the device whose home
// directory is home, with the bare names given.
func renderSSHConfig(home string, names []string) ([]byte, error) {
	koko, err := kokoPath()
	if err != nil {
		return nil, fmt.Errorf("find this koko binary for koko's ssh_config: %w", err)
	}
	b, err := sshConfig(koko, filepath.Join(dir(home), knownHostsFile), names)
	if err != nil {
		return nil, fmt.Errorf("write koko's ssh_config: %w", err)
	}
	return b, nil
}

// kokoPath returns the path ssh is to run this koko binary by: the koko a
// PATH lookup finds, when that is this binary, such as a package manager's
// link that outlives an upgrade; otherwise the path the OS gives for it,
// which on Linux has every symlink resolved.
func kokoPath() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	found, err := exec.LookPath("koko")
	if err != nil || !filepath.IsAbs(found) {
		return self, nil
	}
	a, errA := os.Stat(found)
	b, errB := os.Stat(self)
	if errA != nil || errB != nil || !os.SameFile(a, b) {
		return self, nil
	}
	return found, nil
}

// sshConfig returns koko's ssh_config, whose ProxyCommand runs the koko
// binary at koko and whose known_hosts file is knownHosts, with Host lines
// for *.koko and names.
func sshConfig(koko, knownHosts string, names []string) ([]byte, error) {
	kokoArg, err := quotePath(koko)
	if err != nil {
		return nil, err
	}
	knownHostsArg, err := quotePath(knownHosts)
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		if err := name.Check(n); err != nil {
			return nil, fmt.Errorf("a Host's name: %w", err)
		}
	}
	// HostName %h keeps %h the SSH Name whatever HostName a later block
	// for the same name sets (docs/subsystems/device-connection.md, "ssh").
	// %h is single-quoted: ssh lowercases it and drops the user and -p,
	// and clients before 9.6 pass shell metacharacters in it to the shell.
	// The keepalives make ssh wait 150 s, longer than the 1-minute grace, so
	// ssh never ends an SSH Session that could still Resume; they move with
	// the grace.
	body := []string{
		"HostName %h",
		proxyCommand + kokoArg + proxyTunnel,
		"HostKeyAlias " + hostKeyAlias,
		"UserKnownHostsFile " + knownHostsArg,
		"StrictHostKeyChecking yes",
		"UpdateHostKeys no",
		"PreferredAuthentications none",
		"ForwardAgent no",
		"User koko",
		"ServerAliveInterval 30",
		"ServerAliveCountMax 4",
	}
	var block bytes.Buffer
	for _, l := range body {
		if len("  "+l+"\n") > maxLine {
			return nil, fmt.Errorf("its line %q would pass %d bytes", l, maxLine)
		}
		block.WriteString("  " + l + "\n")
	}

	var out bytes.Buffer
	out.WriteString(sshConfigHeader)
	host := "Host"
	for _, p := range append([]string{"*.koko"}, names...) {
		if len(host+" "+p+"\n") > maxLine {
			out.WriteString(host + "\n")
			out.Write(block.Bytes())
			host = "Host"
		}
		host += " " + p
	}
	out.WriteString(host + "\n")
	out.Write(block.Bytes())
	return out.Bytes(), nil
}

// quotePath returns path double-quoted for an ssh_config. It refuses a
// relative path, which ssh would read against another directory, and a
// character that ssh or the shell running ProxyCommand would read as
// something else inside double quotes.
func quotePath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%q is not an absolute path", path)
	}
	if i := strings.IndexFunc(path, func(r rune) bool {
		return r < ' ' || r == 0x7f || strings.ContainsRune("\"$`\\%", r)
	}); i >= 0 {
		return "", fmt.Errorf("the path %q holds %q, which ssh or its shell would not read as written", path, path[i])
	}
	return `"` + path + `"`, nil
}

// knownHosts returns koko's known_hosts: one line for each of keys, the
// host keys pinned for the koko server, under the alias koko.
func knownHosts(keys []ssh.PublicKey) []byte {
	var b bytes.Buffer
	for _, k := range keys {
		b.WriteString(hostKeyAlias + " ")
		b.Write(ssh.MarshalAuthorizedKey(k))
	}
	return b.Bytes()
}

// AddInclude makes IncludeLine the first line of ~/.ssh/config on the
// device whose home directory is home: it creates the file with mode 0600
// when it is absent, and does nothing when it includes koko's ssh_config
// already, wherever. Otherwise it writes the line, then the file's bytes as
// they were, in its place atomically with its mode. A file that is a
// symlink, not a regular file or read-only, or that koko cannot read or
// write, it leaves byte for byte, and returns why with the line to add.
func AddInclude(home string) error {
	if err := checkHome(home); err != nil {
		return err
	}
	path := filepath.Join(home, userConfig)
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := replace(path, []byte(IncludeLine+"\n"), 0o600); err != nil {
			return leftAlone(fmt.Sprintf("koko cannot write ~/%s (%v)", userConfig, err))
		}
		return nil
	}
	if err != nil {
		return leftAlone(fmt.Sprintf("koko cannot read ~/%s (%v)", userConfig, err))
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		// The file it leads to may include koko's ssh_config already.
		if b, err := readRegular(path, "~/"+userConfig); err == nil && scanConfig(b, home).include > 0 {
			return nil
		}
		return leftAlone("~/" + userConfig + " is a symlink")
	}
	b, err := readRegular(path, "~/"+userConfig)
	switch {
	case err != nil:
		return leftAlone(err.Error())
	case scanConfig(b, home).include > 0:
		return nil
	case !writable(path, fi):
		return leftAlone("~/" + userConfig + " is read-only")
	}
	if err := replace(path, append([]byte(IncludeLine+"\n"), b...), fi.Mode().Perm()); err != nil {
		return leftAlone(fmt.Sprintf("koko cannot write ~/%s (%v)", userConfig, err))
	}
	return nil
}

// leftAlone says why koko left ~/.ssh/config as it was, and what to add.
func leftAlone(why string) error {
	return fmt.Errorf("%s, so koko left it as it is: add the line %q at its top, so that ssh reaches Hosts through koko", why, IncludeLine)
}

// readRegular reads the ssh configuration at path, which its errors call
// what, through a symlink, only when it is a regular file: a named pipe
// would never end.
func readRegular(path, what string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("koko cannot read %s (%w)", what, err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", what)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("koko cannot read %s (%w)", what, err)
	}
	return b, nil
}

// CheckInclude tells whether ssh reads koko's ssh_config for every host on
// the device whose home directory is home: it returns nil when
// ~/.ssh/config includes it before any Host or Match line, and otherwise
// an error saying what is wrong and how to fix it. A file it cannot read
// is reported as such, never as one without the line.
func CheckInclude(home string) error {
	if err := checkHome(home); err != nil {
		return err
	}
	b, err := readRegular(filepath.Join(home, userConfig), "~/"+userConfig)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w, so koko cannot tell whether ssh reaches Hosts through koko", err)
	}
	s := scanConfig(b, home)
	switch {
	case s.include == 0:
		return fmt.Errorf("~/%s does not include koko's ssh_config, so ssh does not reach Hosts through koko: add the line %q at its top", userConfig, IncludeLine)
	case s.block > 0 && s.block < s.include:
		return fmt.Errorf("~/%s includes koko's ssh_config on line %d, below line %d (%q), so ssh reads it only for the hosts that line matches: move %q to the top",
			userConfig, s.include, s.block, s.blockLine, IncludeLine)
	}
	return nil
}

// configScan is where an ssh configuration includes koko's ssh_config and
// where its first Host or Match block starts: 1-based line numbers, 0 for
// none.
type configScan struct {
	include   int
	block     int
	blockLine string
}

// scanConfig reads the user's ssh configuration b as OpenSSH does: a
// keyword, case-insensitive, then its arguments after spaces or one '=',
// each bare or double-quoted; blank lines and comments skipped.
func scanConfig(b []byte, home string) configScan {
	var s configScan
	n := 0
	for line := range strings.Lines(string(b)) {
		n++
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		keyword, args := line, ""
		if i := strings.IndexAny(line, " \t="); i >= 0 {
			keyword, args = line[:i], strings.TrimPrefix(strings.TrimLeft(line[i:], " \t"), "=")
		}
		switch strings.ToLower(keyword) {
		case "host", "match":
			if s.block == 0 {
				s.block, s.blockLine = n, line
			}
		case "include":
			for _, a := range configArgs(args) {
				if s.include == 0 && includesKoko(a, home) {
					s.include = n
				}
			}
		}
	}
	return s
}

// configArgs splits an ssh_config line's arguments at spaces and tabs
// outside double quotes, and drops the quotes.
func configArgs(args string) []string {
	var out []string
	var arg strings.Builder
	quoted, started := false, false
	for _, r := range args {
		switch {
		case r == '"':
			quoted, started = !quoted, true
		case (r == ' ' || r == '\t') && !quoted:
			if started {
				out = append(out, arg.String())
				arg.Reset()
				started = false
			}
		default:
			arg.WriteRune(r)
			started = true
		}
	}
	if started {
		out = append(out, arg.String())
	}
	return out
}

// includesKoko tells whether the Include argument arg, a path or a glob,
// names koko's ssh_config on the device whose home directory is home, once
// resolved as OpenSSH resolves it: a leading ~/ from the home directory, a
// relative path from ~/.ssh.
func includesKoko(arg, home string) bool {
	switch {
	case strings.HasPrefix(arg, "~/"):
		arg = filepath.Join(home, arg[2:])
	case !filepath.IsAbs(arg):
		arg = filepath.Join(home, ".ssh", arg)
	}
	ok, err := filepath.Match(filepath.Clean(arg), filepath.Join(dir(home), sshConfigFile))
	return err == nil && ok
}

// replace writes data to path atomically, through a new file of the given
// mode in the same directory renamed over it, so that a reader sees the old
// bytes or the new ones and never a file half written.
func replace(path string, data []byte, mode fs.FileMode) error {
	tmp, err := writeTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*", data)
	if err != nil {
		return err
	}
	err = restrict(tmp, mode)
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
