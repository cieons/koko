// Package devicefiles keeps what a device holds of the koko server it
// joined, in ~/.ssh/koko/ (mode 0700): its private key, an unencrypted
// OpenSSH-format ed25519 key (0600), and the client config (0600), which
// names the Server Address, the host keys pinned for it, and the User and
// device name the device joined as (P0 spec: people and Spaces, "Joining").
//
// It is the one place a device's join is written and read, so whatever ends
// a join on the device (the revocation cleanup, leaving) removes it here.
// P0 allows one koko server per device, that is per OS user's home.
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
		return fmt.Errorf("HOME is %q, not an absolute path, so koko cannot find this device's files in ~/%s", home, kokoDir)
	}
	return nil
}

// Read returns the join the device whose home directory is home holds: its
// client config and its key. It returns ErrNotJoined when there is no client
// config, or no home directory to hold one, and fails, naming the file, when
// a file cannot be read or does not parse: that never counts as not joined.
func Read(home string) (Config, ssh.Signer, error) {
	if home == "" {
		return Config{}, nil, ErrNotJoined
	}
	if err := checkHome(home); err != nil {
		return Config{}, nil, err
	}
	path := filepath.Join(dir(home), configFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil, ErrNotJoined
	}
	if err != nil {
		return Config{}, nil, fmt.Errorf("read this device's client config: %w", err)
	}
	var cj configJSON
	if err := json.Unmarshal(b, &cj); err != nil {
		return Config{}, nil, fmt.Errorf("read this device's client config %s: %w", path, err)
	}
	c := Config{ServerAddress: cj.ServerAddress, User: cj.User, Device: cj.Device, DeviceSession: cj.DeviceSession}
	for _, line := range cj.HostKeys {
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			return Config{}, nil, fmt.Errorf("read this device's client config %s: a pinned host key: %w", path, err)
		}
		c.HostKeys = append(c.HostKeys, key)
	}
	if c.ServerAddress == "" || len(c.HostKeys) == 0 {
		return Config{}, nil, fmt.Errorf("read this device's client config %s: it names no Server Address or no host key", path)
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

// Staged is a join begun on the device: its new key is written under a
// temporary name. Exactly one of Commit and Abort follows.
type Staged struct {
	home string
	// tmpKey is the key's temporary file.
	tmpKey string
	// made lists the directories Stage made, deepest last.
	made []string
}

// Stage begins a join on the device whose home directory is home: it makes
// ~/.ssh and ~/.ssh/koko when they are missing, makes ~/.ssh/koko private
// to this user, and writes key there under a temporary name. So a device
// that cannot keep its files learns it before it spends a Join Code.
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
	for _, d := range []string{filepath.Dir(dir(s.home)), dir(s.home)} {
		err := os.Mkdir(d, 0o700)
		switch {
		case err == nil:
			s.made = append(s.made, d)
		case !errors.Is(err, fs.ErrExist):
			return fmt.Errorf("make %s: %w", d, err)
		}
	}
	// koko's own directory, never the user's ~/.ssh, is made private even
	// when it was there already.
	if err := restrict(dir(s.home), 0o700); err != nil {
		return err
	}
	block, err := ssh.MarshalPrivateKey(key, "koko device key")
	if err != nil {
		return fmt.Errorf("encode this device's key: %w", err)
	}
	s.tmpKey, err = writeTemp(dir(s.home), "."+keyFile+"-*", pem.EncodeToMemory(block))
	return err
}

// Commit completes the join with the client config c: it puts the key in
// place, then writes the client config, which marks the device joined.
func (s *Staged) Commit(c Config) error {
	keyPath := filepath.Join(dir(s.home), keyFile)
	if err := os.Rename(s.tmpKey, keyPath); err != nil {
		s.Abort()
		return fmt.Errorf("put this device's key in place: %w", err)
	}
	s.tmpKey = ""
	cj := configJSON{ServerAddress: c.ServerAddress, User: c.User, Device: c.Device, DeviceSession: c.DeviceSession}
	for _, k := range c.HostKeys {
		cj.HostKeys = append(cj.HostKeys, string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(k))))
	}
	b, err := json.Marshal(cj, jsontext.WithIndent("  "))
	if err != nil {
		s.undo(keyPath)
		return fmt.Errorf("encode the client config: %w", err)
	}
	tmp, err := writeTemp(dir(s.home), "."+configFile+"-*", append(b, '\n'))
	if err == nil {
		if err = os.Rename(tmp, filepath.Join(dir(s.home), configFile)); err != nil {
			_ = os.Remove(tmp)
		}
	}
	if err != nil {
		s.undo(keyPath)
		return fmt.Errorf("write the client config: %w", err)
	}
	return nil
}

// undo removes the key Commit put in place at keyPath, then aborts.
func (s *Staged) undo(keyPath string) {
	_ = os.Remove(keyPath)
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

// writeTemp writes data to a new file in dir, readable by its owner only,
// named after pattern as os.CreateTemp names one, and returns its path.
func writeTemp(dir, pattern string, data []byte) (string, error) {
	f, err := os.CreateTemp(dir, pattern) // mode 0600
	if err != nil {
		return "", err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("write %s: %w", f.Name(), err)
	}
	return f.Name(), nil
}
