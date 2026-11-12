// Package devicefiles keeps what a device holds of the koko server it
// joined, in ~/.ssh/koko/ (mode 0700): its private key, an unencrypted
// OpenSSH-format ed25519 key (0600), and the client config (0600), which
// names the Server Address, the host keys pinned for it, and the User and
// device name the device joined as (P0 spec: people and Spaces, "Joining");
// and koko's ssh_config and known_hosts (0600), with the Include line in
// ~/.ssh/config that makes ssh read the former (P0 spec: reach).
//
// It is the one place a device's join is written and read, so whatever ends
// a join on the device (the revocation cleanup, leaving) removes it here.
// Once a device learns that its Device Session was Revoked, it keeps a
// revocation record (0600) in place of its key and client config, until it
// joins again. P0 allows one koko server per device, that is per OS user's
// home.
package devicefiles

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"
)

// The files, under the home directory.
const (
	// kokoDir holds koko's files on a device.
	kokoDir = ".ssh/koko"
	// keyFile is the device's private key, its Device Session's.
	keyFile = "device_key"
	// configFile is the client config. It is written last, so a device is
	// joined exactly when it exists.
	configFile = "client.json"
	// revocationFile is the revocation record, which a device keeps from
	// learning that its Device Session was Revoked until it joins again.
	revocationFile = "revoked.json"
)

// ErrNotJoined says the device holds no join: no client config is there.
var ErrNotJoined = errors.New("this device has not joined a koko server")

// Config is the client config: the koko server a device joined, and who
// the device is there.
type Config struct {
	// ServerAddress is the koko server's Server Address as host:port.
	ServerAddress string
	// HostKeys are the host keys pinned for the koko server: a Device
	// Connection takes any of them. A join pins the one the Join Code names.
	HostKeys []ssh.PublicKey
	// User is the name of the User the device acts as.
	User string
	// Device is the device's name.
	Device string
	// DeviceSession is the id of the device's Device Session.
	DeviceSession string
}

// configJSON is the client config as client.json holds it, the host keys
// written as in authorized_keys.
type configJSON struct {
	ServerAddress string   `json:"server_address"`
	HostKeys      []string `json:"host_keys"`
	User          string   `json:"user"`
	Device        string   `json:"device"`
	DeviceSession string   `json:"device_session"`
}

// dir returns koko's directory on the device whose home directory is home.
func dir(home string) string {
	return filepath.Join(home, kokoDir)
}

// checkHome refuses a home directory koko cannot place its files under.
func checkHome(home string) error {
	if !filepath.IsAbs(home) {
		return fmt.Errorf("%s is %q, not an absolute path, so koko cannot find this device's files in ~/%s", HomeVar, home, kokoDir)
	}
	return nil
}

// Read returns the join the device whose home directory is home holds: its
// client config and its key. It returns ErrNotJoined when there is no client
// config, or no home directory to hold one, and fails, naming the file, when
// a file cannot be read or does not parse: that never counts as not joined.
func Read(home string) (Config, ssh.Signer, error) {
	c, err := ReadConfig(home)
	if err != nil {
		return Config{}, nil, err
	}
	keyPath := filepath.Join(dir(home), keyFile)
	pemBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return Config{}, nil, fmt.Errorf("read this device's key: %w", err)
	}
	key, err := ssh.ParsePrivateKey(pemBytes)
	if err != nil {
		return Config{}, nil, fmt.Errorf("read this device's key %s: %w", keyPath, err)
	}
	return c, key, nil
}

// ReadConfig returns the client config of the join the device whose home
// directory is home holds, as Read does, without reading the device's key:
// for koko leave, which never uses the key.
func ReadConfig(home string) (Config, error) {
	if home == "" {
		return Config{}, ErrNotJoined
	}
	if err := checkHome(home); err != nil {
		return Config{}, err
	}
	path := filepath.Join(dir(home), configFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, ErrNotJoined
	}
	if err != nil {
		return Config{}, fmt.Errorf("read this device's client config: %w", err)
	}
	var cj configJSON
	if err := json.Unmarshal(b, &cj); err != nil {
		return Config{}, fmt.Errorf("read this device's client config %s: %w", path, err)
	}
	c := Config{ServerAddress: cj.ServerAddress, User: cj.User, Device: cj.Device, DeviceSession: cj.DeviceSession}
	for _, line := range cj.HostKeys {
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			return Config{}, fmt.Errorf("read this device's client config %s: a pinned host key: %w", path, err)
		}
		c.HostKeys = append(c.HostKeys, key)
	}
	if c.ServerAddress == "" || len(c.HostKeys) == 0 {
		return Config{}, fmt.Errorf("read this device's client config %s: it names no Server Address or no host key", path)
	}
	return c, nil
}

// Staged is a join begun on the device: its new key is written under a
// temporary name. Exactly one of Commit and Abort follows.
type Staged struct {
	home string
	// sshConfig is koko's ssh_config, rendered by Stage for Commit to write.
	sshConfig []byte
	// tmpKey is the key's temporary file.
	tmpKey string
	// made lists the directories Stage made, deepest last.
	made []string
	// wrote lists the files Commit made that were not there before.
	wrote []string
}

// Stage begins a join on the device whose home directory is home: it
// renders koko's ssh_config, makes ~/.ssh and ~/.ssh/koko when they are
// missing, makes ~/.ssh/koko private to this user, and writes key there
// under a temporary name. So a device that cannot keep its files, or whose
// paths ssh would misread, learns it before it spends a Join Code.
func Stage(home string, key ed25519.PrivateKey) (*Staged, error) {
	if err := checkHome(home); err != nil {
		return nil, err
	}
	s := &Staged{home: home}
	if err := s.stage(key); err != nil {
		s.Abort()
		return nil, err
	}
	return s, nil
}

func (s *Staged) stage(key ed25519.PrivateKey) error {
	var err error
	if s.sshConfig, err = renderSSHConfig(s.home, nil); err != nil {
		return err
	}
	if s.made, err = makeDir(s.home); err != nil {
		return err
	}
	block, err := ssh.MarshalPrivateKey(key, "koko device key")
	if err != nil {
		return fmt.Errorf("encode this device's key: %w", err)
	}
	s.tmpKey, err = writeTemp(dir(s.home), "."+keyFile+"-*", pem.EncodeToMemory(block))
	return err
}

// MakePrivate makes the file at path private to this OS user, as koko
// makes its own files: mode 0600 on Linux and macOS, and on Windows this
// user as its owner and a protected access control list for this user,
// SYSTEM and Administrators.
func MakePrivate(path string) error {
	return restrict(path, 0o600)
}

// CheckPrivateDir says why the directory at path, whose Lstat is fi, is
// not one that no other OS user can reach into, or returns nil when it is:
// a directory, not a link, that this user owns, of mode 0700 on Linux and
// macOS, and on Windows with a protected access control list that lets no
// principal but this user, SYSTEM and Administrators in. Its error reads
// after the directory's name, as in "has mode 0755, not 0700", for the
// caller to name it.
func CheckPrivateDir(path string, fi fs.FileInfo) error {
	switch {
	case !fi.IsDir():
		return errors.New("is not a directory")
	case !ownedByMe(path, fi):
		return errors.New("belongs to another user")
	}
	return closedToOthers(path, fi)
}

// makeDir makes ~/.ssh and ~/.ssh/koko/ under home, private to this user
// (mode 0700), when they are missing, and makes koko's own directory, never
// a ~/.ssh that was there, private even when it was there already. It
// returns the directories it made, deepest last, those it made before
// failing included.
func makeDir(home string) (made []string, err error) {
	for _, d := range []string{filepath.Dir(dir(home)), dir(home)} {
		err := os.Mkdir(d, 0o700)
		switch {
		case err == nil:
			made = append(made, d)
		case !errors.Is(err, fs.ErrExist):
			return made, fmt.Errorf("make %s: %w", d, err)
		case d != dir(home):
			continue
		}
		// Windows keeps no mode, so a directory just made is not private
		// yet there.
		if err := restrict(d, 0o700); err != nil {
			return made, err
		}
	}
	return made, nil
}

// Commit completes the join with the client config c: it puts the key in
// place, writes koko's known_hosts with the host keys c pins and koko's
// ssh_config without bare names, then writes the client config, which marks
// the device joined.
func (s *Staged) Commit(c Config) error {
	keyPath := filepath.Join(dir(s.home), keyFile)
	if err := Rename(s.tmpKey, keyPath); err != nil {
		s.Abort()
		return fmt.Errorf("put this device's key in place: %w", err)
	}
	s.tmpKey = ""
	for _, f := range []struct {
		name string
		data []byte
	}{{knownHostsFile, knownHosts(c.HostKeys)}, {sshConfigFile, s.sshConfig}} {
		if err := s.write(f.name, f.data); err != nil {
			s.undo(keyPath)
			return fmt.Errorf("write koko's %s: %w", f.name, err)
		}
	}
	if err := writeConfig(s.home, c); err != nil {
		s.undo(keyPath)
		return err
	}
	// The join holds: a revocation record of an earlier join explains
	// nothing any more. A record left behind is harmless, since the client
	// config is read first.
	_ = os.Remove(filepath.Join(dir(s.home), revocationFile))
	return nil
}

// writeConfig writes c as the client config of the device whose home is
// home, atomically: a new file renamed over the old.
func writeConfig(home string, c Config) error {
	cj := configJSON{ServerAddress: c.ServerAddress, User: c.User, Device: c.Device, DeviceSession: c.DeviceSession}
	for _, k := range c.HostKeys {
		cj.HostKeys = append(cj.HostKeys, string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(k))))
	}
	b, err := json.Marshal(cj, jsontext.WithIndent("  "))
	if err != nil {
		return fmt.Errorf("encode the client config: %w", err)
	}
	tmp, err := writeTemp(dir(home), "."+configFile+"-*", append(b, '\n'))
	if err == nil {
		if err = Rename(tmp, filepath.Join(dir(home), configFile)); err != nil {
			_ = os.Remove(tmp)
		}
	}
	if err != nil {
		return fmt.Errorf("write the client config: %w", err)
	}
	return nil
}

// SetServerAddress makes address, host:port, the Server Address of the join
// the device whose home is home holds, and keeps the rest of its client
// config, for koko connect <host:port> once the koko server answered there
// with a pinned host key. The pinned host keys, and so known_hosts, do not
// change. It fails with ErrNotJoined when the device holds no join.
func SetServerAddress(home, address string) error {
	c, _, err := Read(home)
	if err != nil {
		return err
	}
	c.ServerAddress = address
	return writeConfig(home, c)
}

// Revocation is the record a device keeps once it learns that its Device
// Session was Revoked: which one, at which koko server, and when it learned
// it. Later commands explain the revocation from it until the next join.
type Revocation struct {
	ServerAddress string
	User          string
	Device        string
	DeviceSession string
	// LearnedAt is when the device learned it, by its own clock.
	LearnedAt time.Time
}

// revocationJSON is the revocation record as revoked.json holds it.
type revocationJSON struct {
	ServerAddress string `json:"server_address"`
	User          string `json:"user"`
	Device        string `json:"device"`
	DeviceSession string `json:"device_session"`
	LearnedAt     string `json:"learned_at"`
}

// Revoke is the revocation cleanup on the device whose home directory is
// home, once it learns at learned that the Device Session its client config
// c names was Revoked: it writes the revocation record (0600), then deletes
// the client config, the device's key and koko's known_hosts, and rewrites
// koko's ssh_config without bare names (forget): *.koko still runs koko
// tunnel, which explains the revocation from the record.
func Revoke(home string, c Config, learned time.Time) error {
	if err := checkHome(home); err != nil {
		return err
	}
	rj := revocationJSON{
		ServerAddress: c.ServerAddress,
		User:          c.User,
		Device:        c.Device,
		DeviceSession: c.DeviceSession,
		LearnedAt:     learned.UTC().Truncate(time.Second).Format(time.RFC3339),
	}
	b, err := json.Marshal(rj, jsontext.WithIndent("  "))
	if err != nil {
		return fmt.Errorf("encode the revocation record: %w", err)
	}
	tmp, err := writeTemp(dir(home), "."+revocationFile+"-*", append(b, '\n'))
	if err == nil {
		if err = Rename(tmp, filepath.Join(dir(home), revocationFile)); err != nil {
			_ = os.Remove(tmp)
		}
	}
	if err != nil {
		return fmt.Errorf("write the revocation record: %w", err)
	}
	return forget(home, "delete the files of this device's Revoked Device Session")
}

// Leave ends the join the device whose home directory is home holds, on
// the device alone, for koko leave: it deletes what the revocation cleanup
// deletes, then any revocation record, and writes none, so the device is
// as it was before it first joined but for koko's ssh_config without bare
// names and the Include line in ~/.ssh/config, which a later join finds in
// place. A file already gone is no failure, so a leave cut short can be
// finished by joining again, which writes every file anew.
func Leave(home string) error {
	if err := checkHome(home); err != nil {
		return err
	}
	if err := forget(home, "delete this device's files of the koko server it left"); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir(home), revocationFile)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("delete this device's revocation record: %w", err)
	}
	return nil
}

// forget deletes the client config first, so the device is no longer
// joined, then the device's key, whose copies are now useless, and koko's
// known_hosts, whose lines are all that koko server's (P0 has one per
// device), a failure to delete one saying it was doing what; last it
// rewrites koko's ssh_config without bare names.
func forget(home, what string) error {
	for _, f := range []string{configFile, keyFile, knownHostsFile} {
		if err := os.Remove(filepath.Join(dir(home), f)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%s: %w", what, err)
		}
	}
	_, err := WriteSSHConfig(home, nil)
	return err
}

// ReadRevocation returns the revocation record the device whose home
// directory is home keeps. It fails with an error wrapping fs.ErrNotExist
// when there is none, or no home directory to hold one, and naming the file
// when it cannot be read or does not parse.
func ReadRevocation(home string) (Revocation, error) {
	if home == "" {
		return Revocation{}, fmt.Errorf("read this device's revocation record: no home directory: %w", fs.ErrNotExist)
	}
	if err := checkHome(home); err != nil {
		return Revocation{}, err
	}
	path := filepath.Join(dir(home), revocationFile)
	b, err := os.ReadFile(path)
	if err != nil {
		return Revocation{}, fmt.Errorf("read this device's revocation record: %w", err)
	}
	var rj revocationJSON
	if err := json.Unmarshal(b, &rj); err != nil {
		return Revocation{}, fmt.Errorf("read this device's revocation record %s: %w", path, err)
	}
	learned, err := time.Parse(time.RFC3339, rj.LearnedAt)
	if err != nil {
		return Revocation{}, fmt.Errorf("read this device's revocation record %s: %w", path, err)
	}
	return Revocation{
		ServerAddress: rj.ServerAddress,
		User:          rj.User,
		Device:        rj.Device,
		DeviceSession: rj.DeviceSession,
		LearnedAt:     learned,
	}, nil
}

// write writes data as koko's file name, 0600 and atomically, and notes it
// for undo when it was not there before.
func (s *Staged) write(name string, data []byte) error {
	path := filepath.Join(dir(s.home), name)
	_, err := os.Lstat(path)
	existed := err == nil
	if err := replace(path, data, 0o600); err != nil {
		return err
	}
	if !existed {
		s.wrote = append(s.wrote, path)
	}
	return nil
}

// undo removes what Commit made, the key it put in place at keyPath and the
// files it wrote that were not there before, then aborts. A file it
// rewrote, such as the ssh_config an earlier revocation left, keeps the
// bytes it wrote, which route the same names.
func (s *Staged) undo(keyPath string) {
	_ = os.Remove(keyPath)
	for _, f := range s.wrote {
		_ = os.Remove(f)
	}
	s.Abort()
}

// Abort removes what Stage wrote: the key's temporary file and the
// directories it made, so a failed join leaves the device as it was.
func (s *Staged) Abort() {
	if s.tmpKey != "" {
		_ = os.Remove(s.tmpKey)
		s.tmpKey = ""
	}
	for i := len(s.made) - 1; i >= 0; i-- {
		_ = os.Remove(s.made[i]) // only while empty
	}
	s.made = nil
}

// writeTemp writes data to a new file in dir, private to this user before
// it holds a byte, named after pattern as os.CreateTemp names one, and
// returns its path.
func writeTemp(dir, pattern string, data []byte) (string, error) {
	f, err := os.CreateTemp(dir, pattern) // mode 0600
	if err != nil {
		return "", err
	}
	// Windows keeps no mode: there the new file has what dir passes on
	// until restrict has run. For the key, dir is koko's own directory,
	// private already, so nobody else could open the file in between; the
	// one file written elsewhere, ~/.ssh/config, holds no secret.
	if err = restrict(f.Name(), 0o600); err == nil {
		_, err = f.Write(data)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("write %s: %w", f.Name(), err)
	}
	return f.Name(), nil
}
