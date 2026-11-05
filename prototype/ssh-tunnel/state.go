// PROTOTYPE, throwaway: never merged. See README.md.
//
// state.go holds the files the prototype keeps under $KOKO_PROTO_HOME, a
// scratch directory: the koko server's keys and records, and the device's
// key and pinned server. Everything is plain files so a run is easy to read.
package main

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

func home() string {
	h := os.Getenv("KOKO_PROTO_HOME")
	if h == "" {
		fmt.Fprintln(os.Stderr, "kokoproto: KOKO_PROTO_HOME is not set")
		os.Exit(2)
	}
	return h
}

func path(parts ...string) string { return filepath.Join(append([]string{home()}, parts...)...) }

var (
	serverHostKeyPath = func() string { return path("server", "host_key") }
	kokoKeyPath       = func() string { return path("server", "koko_key") }
	devicesPath       = func() string { return path("server", "devices") }
	revokedPath       = func() string { return path("server", "revoked") }
	hostsPath         = func() string { return path("server", "hosts") }
	deviceKeyPath     = func() string { return path("device", "id_ed25519") }
	deviceServerPath  = func() string { return path("device", "server") }
	knownHostsPath    = func() string { return path("ssh", "known_hosts") }
)

// loadOrCreateKey reads an OpenSSH private key, generating an ed25519 one
// (and its .pub) the first time, as the koko server does with its host key.
func loadOrCreateKey(p string) (ssh.Signer, error) {
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		block, err := ssh.MarshalPrivateKey(priv, filepath.Base(p))
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, pem.EncodeToMemory(block), 0o600); err != nil {
			return nil, err
		}
		s, err := ssh.NewSignerFromKey(priv)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(p+".pub", ssh.MarshalAuthorizedKey(s.PublicKey()), 0o644); err != nil {
			return nil, err
		}
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(b)
}

// findKey looks a public key up in an authorized_keys-format file and
// returns the line's comment, which names the device ("alice/laptop").
func findKey(file string, key ssh.PublicKey) (string, bool) {
	b, err := os.ReadFile(file)
	if err != nil {
		return "", false
	}
	want := key.Marshal()
	for len(b) > 0 {
		pk, comment, _, rest, err := ssh.ParseAuthorizedKey(b)
		if err != nil {
			return "", false
		}
		if bytes.Equal(pk.Marshal(), want) {
			return comment, true
		}
		b = rest
	}
	return "", false
}

// hostRecord is one recorded, Managed Host: where it is, which Host Account
// koko logs in as, and its pinned host key.
type hostRecord struct {
	name    string
	addr    string
	account string
	hostKey ssh.PublicKey
}

func lookupHost(name string) (hostRecord, error) {
	f, err := os.Open(hostsPath())
	if err != nil {
		return hostRecord{}, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 5 || fields[0] != name {
			continue
		}
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.Join(fields[3:], " ")))
		if err != nil {
			return hostRecord{}, fmt.Errorf("parse host key of %s: %w", name, err)
		}
		return hostRecord{name: name, addr: fields[1], account: fields[2], hostKey: key}, nil
	}
	return hostRecord{}, fs.ErrNotExist
}
