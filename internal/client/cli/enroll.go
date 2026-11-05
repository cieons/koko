package cli

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/record"
)

var enrollCommands = []Command{
	{
		Noun:    "host",
		Verb:    "enroll",
		Summary: "Make a Registered Host Managed: koko logs in once with a password or key you supply, pins its host key and adds the koko key",
		Args:    []Arg{{Name: "host"}},
		Flags: []Flag{
			{Name: "password-stdin", Bool: true, Summary: "Read the Host Account's password from stdin"},
			{Name: "key", Value: "file", Summary: "Log in with the private key in this file, asking for its passphrase if it is encrypted"},
			{Name: "secret", Value: "space/secret", Summary: "Log in with the password or private key this Secret holds, which the koko server reads: the value never reaches this device"},
			{Name: "host-key", Value: "fingerprint", Summary: "Accept and pin only the host key with this SHA256:... fingerprint"},
			{Name: "replace-host-key", Bool: true, Summary: "Pin whatever host key the Host offers now, in place of the pinned one"},
		},
		ReadsSecret: true,
		// It reaches a Host and changes its authorized_keys (ADR 0019).
		Tier: Irreversible,
		Op:   api.HostEnroll,
		Run:  action(hostEnroll),
	},
}

var enrollMappings = []mapping{
	{api: reflect.TypeFor[api.HostEnrollResponse]()},
}

// enrolledHost is what host enroll prints: the Host, Managed, with the
// fingerprint of the host key it pinned.
type enrolledHost hostJSON

func (h enrolledHost) Human(w io.Writer) error {
	hostKey := "none"
	if h.HostKey != nil {
		hostKey = *h.HostKey
	}
	_, err := fmt.Fprintf(w, "Enrolled Host %s (%s): it is Managed, with its host key %s pinned.\n", h.Name, h.ID, hostKey)
	return err
}

// passwordInput is the secret input of the password of the Host named
// hostName.
func passwordInput(hostName string) SecretInput {
	return SecretInput{
		What:      "the password of " + hostName,
		StdinFlag: "password-stdin",
		Others:    []string{"--key <file>", "--secret <space>/<secret>"},
		Max:       api.EnrollInputMaxSize,
	}
}

// secretArg is how --secret names a Secret: <space>/<secret>, as every
// command names one.
var secretArg = Arg{Name: "secret", Qualifier: "space"}

func hostEnroll(ctx context.Context, in *Input) (enrolledHost, error) {
	hostName := in.Arg("host")
	req := api.HostEnrollRequest{Host: hostName, ReplaceHostKey: in.Bool("replace-host-key")}
	if fp := in.Flag("host-key"); fp != "" {
		if err := record.CheckHostKey(fp); err != nil {
			return enrolledHost{}, usagef("--host-key %s: %v", fp, err)
		}
		if req.ReplaceHostKey {
			return enrolledHost{}, usagef("--host-key and --replace-host-key are two ways to pin a new host key: give one of them")
		}
		req.HostKey = &fp
	}
	var ways []string
	for _, f := range []string{"password-stdin", "key", "secret"} {
		if in.Flag(f) != "" {
			ways = append(ways, "--"+f)
		}
	}
	if len(ways) > 1 {
		return enrolledHost{}, usagef("%s are each a way to log in to %s: give one of them", strings.Join(ways, " and "), hostName)
	}
	switch keyFile, secretName := in.Flag("key"), in.Flag("secret"); {
	case secretName != "":
		// The koko server reads the Secret, so its value never reaches
		// this device.
		q, err := secretArg.split(secretName)
		if err != nil {
			return enrolledHost{}, usagef("--secret %v", err)
		}
		req.Secret = &api.SecretRef{Space: q.qualifier, Name: q.name}
	case keyFile != "":
		key, err := readKey(ctx, in, keyFile)
		if err != nil {
			return enrolledHost{}, err
		}
		req.PrivateKey = key
	default:
		password, err := in.ReadSecret(ctx, passwordInput(hostName))
		if err != nil {
			return enrolledHost{}, err
		}
		// A password piped in usually ends with the newline of echo.
		password = api.EnrollPassword(password)
		if len(password) == 0 {
			return enrolledHost{}, usagef("the password of %s cannot be empty: stdin holds only a line ending", hostName)
		}
		req.Password = password
	}
	resp, err := in.Call(ctx, api.HostEnroll, req)
	if err != nil {
		return enrolledHost{}, err
	}
	return enrolledHost(hostFromAPI(resp.Host)), nil
}

// readKey reads the private key in the file at path for Enroll, refusing
// before any call one koko cannot read. An encrypted key is decrypted here,
// with the passphrase it asks for on a terminal, so the passphrase never
// leaves the device; it goes to the koko server decrypted, in OpenSSH's
// format, as a key that is not encrypted goes as it is.
func readKey(ctx context.Context, in *Input, path string) ([]byte, error) {
	key, err := readFile(path, api.EnrollInputMaxSize)
	if err != nil {
		return nil, usagef("cannot read --key %s: %v", path, unwrapPath(err))
	}
	if len(key) > api.EnrollInputMaxSize {
		return nil, usagef("--key %s is longer than %d KiB, which no private key is", path, api.EnrollInputMaxSize>>10)
	}
	_, err = ssh.ParseRawPrivateKey(key)
	if err == nil {
		return key, nil
	}
	if _, encrypted := errors.AsType[*ssh.PassphraseMissingError](err); !encrypted {
		return nil, usagef("--key %s holds no private key koko can read: %v", path, err)
	}
	passphrase, err := in.ReadSecret(ctx, SecretInput{What: "the passphrase of " + path})
	if err != nil {
		return nil, err
	}
	raw, err := ssh.ParseRawPrivateKeyWithPassphrase(key, passphrase)
	switch {
	case errors.Is(err, x509.IncorrectPasswordError):
		return nil, usagef("the passphrase does not open %s", path)
	case err != nil:
		return nil, usagef("cannot decrypt --key %s: %v", path, err)
	}
	block, err := ssh.MarshalPrivateKey(raw, "")
	if err != nil {
		return nil, fmt.Errorf("encode the key of %s decrypted: %w", path, err)
	}
	return pem.EncodeToMemory(block), nil
}
