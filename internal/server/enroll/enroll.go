// Package enroll is Enroll, the host.enroll operation: it makes a Registered
// Host Managed (P0 spec: reach, ADR 0003). The koko server logs in to the
// Host once as its Host Account with the password or private key the caller
// supplies, or that a Secret the caller can read holds, never with the koko
// key, checks the host key the Host offers, appends the koko key to the Host
// Account's authorized_keys and changes nothing else there, then records the
// pin and the Managed state. A Host with a Relay is reached through it, as
// the Gateway reaches it: the koko server logs in to the Relay with the koko
// key, checking the Relay's pin.
//
// The login runs before the write's transaction (op.HandlePrepared), so a
// slow Host holds up no other write, and a dropped caller cancels nothing.
package enroll

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/host"
	"github.com/cieons/koko/internal/server/hostconn"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/secret"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/record"
	"github.com/cieons/koko/internal/shared/timing"
)

// enroller carries out host.enroll.
type enroller struct {
	// rootKey opens the koko key and the Secrets that requests name.
	rootKey   *seal.RootKey
	connector hostconn.Connector
	// addKey bounds the command that appends the koko key.
	addKey time.Duration
	log    *slog.Logger
}

// Register adds host.enroll to the API. It opens the koko key and the
// Secrets that requests name with rootKey, waits as timings say (the zero
// Timings is the product's), and logs each Enroll, each re-pin and each
// failure to log.
func Register(a *op.API, rootKey *seal.RootKey, timings timing.Timings, log *slog.Logger) {
	e := &enroller{rootKey: rootKey, connector: hostconn.Connector{Timings: timings}, addKey: timings.Resolve().EnrollAddKey, log: log}
	a.HandlePrepared(api.HostEnroll, e.prepare, e.record)
}

// loggedIn is what prepare did on the Host, for record: who called, the
// Host as read before the login, and the host key it offered, which Enroll
// pins.
type loggedIn struct {
	who     access.Identity
	was     host.Reach
	offered ssh.PublicKey
}

// prepare logs in to the Host and appends the koko key, outside any
// transaction.
func (e *enroller) prepare(ctx context.Context, read op.Read, caller op.Caller, req api.HostEnrollRequest) (loggedIn, error) {
	var (
		who     access.Identity
		h       host.Reach
		in      input
		kokoKey ssh.Signer
	)
	// Who is calling comes first, so a Revoked Device Session learns
	// nothing else, whatever it sent.
	err := read(func(tx store.Tx) (err error) {
		if who, h, err = host.ReachOf(ctx, tx, caller, req.Host); err != nil {
			return err
		}
		if in, err = e.inputOf(ctx, tx, caller, req); err != nil {
			return err
		}
		if err := enrollable(h, req); err != nil {
			return err
		}
		if err := relayUsable(h); err != nil {
			return err
		}
		kokoKey, err = deployment.KokoSigner(ctx, tx, e.rootKey)
		return err
	})
	if err != nil {
		return loggedIn{}, err
	}
	target := hostconn.Target{Address: h.Address, Port: h.Port, Account: h.Account, Login: in.login, Pin: pinFor(h, req)}
	var relays []hostconn.Target
	if r := h.Relay; r != nil {
		// A Relay is always Managed: koko logs in to it as the Gateway does.
		relays = []hostconn.Target{{Address: r.Address, Port: r.Port, Account: r.Account, Login: hostconn.KeyLogin(kokoKey), Pin: hostconn.PinKey(r.Pin)}}
	}
	conn, err := e.connector.Connect(ctx, target, relays)
	if err != nil {
		return loggedIn{}, e.failed(who, h, connectRefusal(h, req, in, err), connectFailure(h, err)...)
	}
	defer func() { _ = conn.Close() }()
	if err := e.appendKey(ctx, conn, kokoKey.PublicKey()); err != nil {
		return loggedIn{}, e.failed(who, h, api.CodeKeyNotAdded.Errorf(
			"koko logged in to %s as %q but could not add the koko key to ~/.ssh/authorized_keys (%v), so it recorded nothing", h.Name, h.Account, err))
	}
	return loggedIn{who: who, was: h, offered: conn.HostKey}, nil
}

// record records the Host as Managed with the host key it offered, inside
// the write's transaction.
func (e *enroller) record(ctx context.Context, tx store.Tx, caller op.Caller, _ api.HostEnrollRequest, l loggedIn) (api.HostEnrollResponse, error) {
	who, h, err := host.Enrolled(ctx, tx, caller, l.was, l.offered)
	if changed, ok := errors.AsType[*api.Error](err); ok && changed.Code == api.CodeHostChanged.Name {
		// The koko key may be on the Host now, which the log must tell.
		return api.HostEnrollResponse{}, e.failed(l.who, l.was, changed)
	}
	if err != nil {
		return api.HostEnrollResponse{}, err
	}
	op.AfterTx(ctx, func(committed bool) {
		if !committed {
			return
		}
		attrs := logAttrs(who, h.Name, h.ID)
		if old := l.was.Pin; old != nil && ssh.FingerprintSHA256(old) != *h.HostKey {
			e.log.Info("host key re-pinned", append(attrs, "old", ssh.FingerprintSHA256(old), "new", *h.HostKey)...)
		}
		e.log.Info("host enrolled", append(attrs, "host_key", *h.HostKey)...)
	})
	return api.HostEnrollResponse{Host: h}, nil
}

// input is the Enroll input Enroll logs in with: a password or a private
// key, given in the request or held in the Secret it names.
type input struct {
	login hostconn.Login
	// refusedAs is how a refusal names the input, such as "the password"
	// or "the private key in the Secret ops/deploy-key", never by its value.
	refusedAs string
}

// inputOf checks the request and returns the input it gives: the password
// or private key it holds, or what the Secret it names holds, read in tx
// for caller.
func (e *enroller) inputOf(ctx context.Context, tx store.Tx, caller op.Caller, req api.HostEnrollRequest) (input, error) {
	if req.HostKey != nil {
		if req.ReplaceHostKey {
			return input{}, api.CodeInvalidRequest.Errorf(
				"host_key and replace_host_key are two ways to pin a new host key: give one of them")
		}
		if err := record.CheckHostKey(*req.HostKey); err != nil {
			return input{}, api.CodeInvalidValue.Errorf("host_key: %v", err)
		}
	}
	given := 0
	for _, g := range []bool{len(req.Password) > 0, len(req.PrivateKey) > 0, req.Secret != nil} {
		if g {
			given++
		}
	}
	if given != 1 {
		return input{}, api.CodeInvalidRequest.Errorf(
			"give host.enroll the Host Account's password, a private key it accepts, or a Secret holding either: exactly one of them")
	}
	if max(len(req.Password), len(req.PrivateKey)) > api.EnrollInputMaxSize {
		return input{}, api.CodeInvalidValue.Errorf(
			"the password or private key is longer than %d KiB, the most Enroll takes", api.EnrollInputMaxSize>>10)
	}
	switch {
	case len(req.Password) > 0:
		return input{login: hostconn.PasswordLogin(string(req.Password)), refusedAs: "the password"}, nil
	case len(req.PrivateKey) > 0:
		return keyInput(req.PrivateKey, "the private key",
			"Enroll takes it decrypted, as koko host enroll --key sends it once it has asked for the passphrase")
	}
	return e.inputFromSecret(ctx, tx, caller, *req.Secret)
}

// pemHeader begins every private key Enroll takes, in each of the PEM forms
// golang.org/x/crypto/ssh parses.
var pemHeader = []byte("-----BEGIN ")

// inputFromSecret reads the Secret ref for caller and returns what it holds:
// a private key when it begins with a PEM header, past any blank space, else
// a password. A value with a PEM header is never tried as a password.
func (e *enroller) inputFromSecret(ctx context.Context, tx store.Tx, caller op.Caller, ref api.SecretRef) (input, error) {
	value, err := secret.Open(ctx, tx, caller, e.rootKey, ref)
	if err != nil {
		return input{}, err
	}
	if key := bytes.TrimLeft(value, " \t\r\n"); bytes.HasPrefix(key, pemHeader) {
		return keyInput(key, "the private key in the Secret "+ref.String(),
			"Enroll takes a key decrypted, and koko keeps no passphrase: store the key decrypted, or Enroll with --key <file>, which asks for the passphrase on the device")
	}
	password := api.EnrollPassword(value)
	if len(password) == 0 {
		return input{}, api.CodeInvalidValue.Errorf(
			"the Secret %s holds only a line ending, which is neither a password nor a private key", ref)
	}
	return input{login: hostconn.PasswordLogin(string(password)), refusedAs: "the password in the Secret " + ref.String()}, nil
}

// keyInput parses key, the private key refusedAs names. It refuses an
// encrypted key, saying why with whenEncrypted, and one that does not
// parse.
func keyInput(key []byte, refusedAs, whenEncrypted string) (input, error) {
	signer, err := ssh.ParsePrivateKey(key)
	if _, encrypted := errors.AsType[*ssh.PassphraseMissingError](err); encrypted {
		return input{}, api.CodeInvalidValue.Errorf("%s is encrypted: %s", refusedAs, whenEncrypted)
	}
	if err != nil {
		return input{}, api.CodeInvalidValue.Errorf("%s does not parse: %v", refusedAs, err)
	}
	return input{login: hostconn.KeyLogin(signer), refusedAs: refusedAs}, nil
}

// enrollable refuses to Enroll a Host whose state does not allow it: a
// Registered Host Enrolls, a Managed one only to pin a new host key, and a
// Retired one never (P0 spec: the record).
func enrollable(h host.Reach, req api.HostEnrollRequest) error {
	switch {
	case h.State == api.HostRegistered:
		return nil
	case h.State == api.HostManaged && (req.HostKey != nil || req.ReplaceHostKey):
		return nil
	case h.State == api.HostManaged:
		return api.CodeAlreadyManaged.Errorf(
			"%s is Managed already, and koko logs in to it with the koko key: Enroll a Managed Host again only to pin a new host key, with --host-key SHA256:... or --replace-host-key", h.Name)
	case h.State == api.HostRetired:
		return api.CodeHostRetired.Errorf("%s (%s) is Retired, so it is gone and koko never logs in to it", h.Name, h.ID)
	}
	return fmt.Errorf("enroll %s: a Host in the state %q cannot be Enrolled", h.Name, h.State)
}

// relayUsable refuses to Enroll a Host whose Relay is not Managed, since
// koko passes through a Relay only with the koko key. It names the Relay
// only to a caller who can see it.
func relayUsable(h host.Reach) error {
	r := h.Relay
	switch {
	case r == nil || r.State == api.HostManaged:
		return nil
	case r.Seen:
		return api.CodeInvalidRelay.Errorf(
			"%s is not Managed, and koko passes through a Relay only once it is Enrolled: Enroll %s first, with koko host enroll %s", r.Of(h.Name), r.Name, r.Name)
	}
	return api.CodeInvalidRelay.Errorf("%s is not Managed, and koko passes through a Relay only once it is Enrolled", r.Of(h.Name))
}

// pinFor is the host key the Host must offer: the one --host-key names,
// any with --replace-host-key, else its pin, and any for a Host with none,
// whose key Enroll pins on first sight.
func pinFor(h host.Reach, req api.HostEnrollRequest) hostconn.Pin {
	switch {
	case req.HostKey != nil:
		return hostconn.PinFingerprint(*req.HostKey)
	case req.ReplaceHostKey || h.Pin == nil:
		return hostconn.Pin{}
	}
	return hostconn.PinKey(h.Pin)
}

// connectRefusal is the caller's answer to a failed login to the Host with
// in. The connector's errors name the Host's address and Host Account,
// which the caller may see: they can act on the Host. A failure on the leg
// to its Relay is relayRefusal's.
func connectRefusal(h host.Reach, req api.HostEnrollRequest, in input, err error) *api.Error {
	if _, ok := errors.AsType[*hostconn.RelayError](err); ok && h.Relay != nil {
		return relayConnectRefusal(h, err)
	}
	if m, ok := errors.AsType[*hostconn.HostKeyMismatchError](err); ok {
		if req.HostKey != nil {
			return api.CodeUnexpectedHostKey.Errorf(
				"the host key of %s does not match the one --host-key names (named %s, offered %s); koko did not log in. Check the fingerprint with %s's provider, or pin whatever key it offers with --replace-host-key",
				h.Name, m.Pinned, m.Offered, h.Name)
		}
		return api.CodeUnexpectedHostKey.Errorf(
			"the host key of %s does not match the one pinned at Enroll (pinned %s, offered %s); koko did not log in. If %s was rebuilt, pin its new key with --host-key <the fingerprint its provider shows>, or with --replace-host-key, which trusts whatever key it offers",
			h.Name, m.Pinned, m.Offered, h.Name)
	}
	if errors.Is(err, hostconn.ErrLoginRefused) {
		return api.CodeLoginRefused.Errorf("%s refused %s for its Host Account %q; koko changed nothing on it", h.Name, in.refusedAs, h.Account)
	}
	return api.CodeHostUnreachable.Errorf("koko could not connect to %s: %v", h.Name, err)
}

// relayConnectRefusal is the caller's answer when koko could not log in to
// the Relay of the Host h with the koko key, so it could not reach h:
// refused with CodeHostUnreachable, as is a Host koko cannot reach, since
// host_key and replace_host_key, which unexpected_host_key names, pin the
// Host's key and not its Relay's. It names the Relay, its fingerprints and
// its Host Account only to a caller who can see it, and the connector's
// error, which names addresses, never: connectFailure logs it.
func relayConnectRefusal(h host.Reach, err error) *api.Error {
	r := h.Relay
	relay := r.Of(h.Name)
	m, mismatch := errors.AsType[*hostconn.HostKeyMismatchError](err)
	var why string
	switch {
	case mismatch && r.Seen:
		why = fmt.Sprintf("the host key of %s does not match the one pinned at Enroll (pinned %s, offered %s). If %s was rebuilt, pin its new key with koko host enroll %s --host-key <the fingerprint its provider shows>, or with --replace-host-key",
			relay, m.Pinned, m.Offered, r.Name, r.Name)
	case mismatch:
		why = fmt.Sprintf("the host key of %s does not match the one pinned at Enroll", relay)
	case errors.Is(err, hostconn.ErrUnreachable):
		why = relay + " is down or unreachable"
	case errors.Is(err, hostconn.ErrLoginRefused) && r.Seen:
		why = fmt.Sprintf("%s refused the koko key for its Host Account %q", relay, r.Account)
	case errors.Is(err, hostconn.ErrLoginRefused):
		why = relay + " refused the koko key"
	default:
		why = "koko could not connect to " + relay
	}
	return api.CodeHostUnreachable.Errorf("koko could not reach %s through its Relay, so it did not log in to %s: %s", h.Name, h.Name, why)
}

// connectFailure is what the log adds about a failed login to the Host h
// with err: for a failure on its Relay's leg, which the refusal tells
// without addresses, the Relay and the connector's error.
func connectFailure(h host.Reach, err error) []any {
	if _, ok := errors.AsType[*hostconn.RelayError](err); ok && h.Relay != nil {
		return []any{"relay", h.Relay.Name, "relay_id", h.Relay.ID, "cause", err}
	}
	return nil
}

// failed logs a failed Enroll, with more, and returns its refusal, err.
func (e *enroller) failed(who access.Identity, h host.Reach, err *api.Error, more ...any) error {
	attrs := slices.Concat(logAttrs(who, h.Name, h.ID), []any{"code", err.Code, "err", err.Message}, more)
	if err.Code == api.CodeUnexpectedHostKey.Name {
		e.log.Warn("enroll refused the host key", attrs...)
	} else {
		e.log.Info("enroll failed", attrs...)
	}
	return err
}

// logAttrs are the fields of every log line about an Enroll of the Host
// named hostName, whose id is hostID: the Host, the User who ran it, and
// the Device Session or the Sign-in it came on as enrolled_by, or the Unix
// socket, as other lines name who acted.
func logAttrs(who access.Identity, hostName, hostID string) []any {
	return []any{"host", hostName, "host_id", hostID, "user", who.User.Name, "enrolled_by", who.By()}
}

// addKeyScript appends the koko key to the Host Account's authorized_keys.
// The Host runs it as `sh -s <key type> <key>`, reading it from stdin, so
// its words reach the Host's sh whatever the Host Account's login shell, and
// only the two words of the key, which hold no character a shell treats
// apart, pass through that shell. It changes nothing else: it creates
// ~/.ssh (0700) and authorized_keys (0600) when they are missing, ends a
// last line that lacks its newline before it appends, and leaves the file
// as it is when a line other than a comment holds the koko key already,
// whatever that line's options or comment. Like ssh-copy-id, it sets the
// SELinux context of what it created where restorecon exists, and leaves
// that of what was there before.
const addKeyScript = `cd || exit 1
umask 077
f=.ssh/authorized_keys
created=
[ -d .ssh ] || created=.ssh
[ -e "$f" ] || created="$created $f"
mkdir -p .ssh || exit 1
if [ -f "$f" ] && awk -v t="$1" -v k="$2" '
	/^[ \t]*(#|$)/ { next }
	{ for (i = 1; i < NF; i++) if ($i == t && $(i + 1) == k) found = 1 }
	END { exit !found }' "$f"
then
	exit 0
fi
if [ -s "$f" ] && [ -n "$(tail -c 1 "$f")" ]; then
	echo >>"$f" || exit 1
fi
printf '%s %s koko\n' "$1" "$2" >>"$f" || exit 1
if [ -n "$created" ] && command -v restorecon >/dev/null 2>&1; then
	restorecon -F $created >/dev/null 2>&1
fi
exit 0
`

// appendKey runs addKeyScript on the Host over conn to append the koko key,
// within e.addKey.
func (e *enroller) appendKey(ctx context.Context, conn *hostconn.Conn, kokoKey ssh.PublicKey) error {
	s, err := conn.NewSession()
	if err != nil {
		return fmt.Errorf("open a session: %w", err)
	}
	defer func() { _ = s.Close() }()
	actx, cancel := context.WithTimeoutCause(ctx, e.addKey, fmt.Errorf("no answer within %v", e.addKey))
	defer cancel()
	stop := context.AfterFunc(actx, func() { _ = conn.Close() })
	defer stop()
	stderr := &firstBytes{n: 1 << 10}
	s.Stdin = strings.NewReader(addKeyScript)
	s.Stderr = stderr
	err = s.Run("sh -s " + kokoKey.Type() + " " + base64.StdEncoding.EncodeToString(kokoKey.Marshal()))
	switch {
	case actx.Err() != nil:
		return context.Cause(actx)
	case err != nil && len(stderr.b) > 0:
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(stderr.b)))
	}
	return err
}

// firstBytes keeps the first n bytes written to it, for an error message.
type firstBytes struct {
	b []byte
	n int
}

func (w *firstBytes) Write(p []byte) (int, error) {
	if room := w.n - len(w.b); room > 0 {
		w.b = append(w.b, p[:min(room, len(p))]...)
	}
	return len(p), nil
}
