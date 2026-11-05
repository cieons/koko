// Package enroll is Enroll, the host.enroll operation: it makes a Registered
// Host Managed (P0 spec: reach, ADR 0003). The koko server logs in to the
// Host once as its Host Account with the password or private key the caller
// supplies, never with the koko key, checks the host key the Host offers,
// appends the koko key to the Host Account's authorized_keys and changes
// nothing else there, then records the pin and the Managed state.
//
// The login runs before the write's transaction (op.HandlePrepared), so a
// slow Host holds up no other write, and a dropped caller cancels nothing.
package enroll

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/server/access"
	"github.com/cieons/koko/internal/server/deployment"
	"github.com/cieons/koko/internal/server/host"
	"github.com/cieons/koko/internal/server/hostconn"
	"github.com/cieons/koko/internal/server/op"
	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/record"
	"github.com/cieons/koko/internal/shared/timing"
)

// enroller carries out host.enroll.
type enroller struct {
	// rootKey opens the koko key.
	rootKey   *seal.RootKey
	connector hostconn.Connector
	// addKey bounds the command that appends the koko key.
	addKey time.Duration
	log    *slog.Logger
}

// Register adds host.enroll to the API. It opens the koko key with rootKey,
// waits as timings say (the zero Timings is the product's), and logs each
// Enroll, each re-pin and each failure to log.
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
		login   hostconn.Login
		kokoKey ssh.PublicKey
	)
	// Who is calling comes first, so a Revoked Device Session learns
	// nothing else, whatever it sent.
	err := read(func(tx store.Tx) (err error) {
		if who, h, err = host.ReachOf(ctx, tx, caller, req.Host); err != nil {
			return err
		}
		if login, err = loginOf(req); err != nil {
			return err
		}
		if err := enrollable(h, req); err != nil {
			return err
		}
		kokoKey, err = deployment.KokoKey(ctx, tx, e.rootKey)
		return err
	})
	if err != nil {
		return loggedIn{}, err
	}
	target := hostconn.Target{Address: h.Address, Port: h.Port, Account: h.Account, Login: login, Pin: pinFor(h, req)}
	conn, err := e.connector.Connect(ctx, target, nil)
	if err != nil {
		return loggedIn{}, e.failed(who, h, connectRefusal(h, req, err))
	}
	defer func() { _ = conn.Close() }()
	if err := e.appendKey(ctx, conn, kokoKey); err != nil {
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

// loginOf checks the request and returns how to log in to the Host with
// what it supplies.
func loginOf(req api.HostEnrollRequest) (hostconn.Login, error) {
	if req.HostKey != nil {
		if req.ReplaceHostKey {
			return hostconn.Login{}, api.CodeInvalidRequest.Errorf(
				"host_key and replace_host_key are two ways to pin a new host key: give one of them")
		}
		if err := record.CheckHostKey(*req.HostKey); err != nil {
			return hostconn.Login{}, api.CodeInvalidValue.Errorf("host_key: %v", err)
		}
	}
	if (len(req.Password) > 0) == (len(req.PrivateKey) > 0) {
		return hostconn.Login{}, api.CodeInvalidRequest.Errorf(
			"give host.enroll the Host Account's password or a private key it accepts: exactly one of them")
	}
	if max(len(req.Password), len(req.PrivateKey)) > api.EnrollInputMaxSize {
		return hostconn.Login{}, api.CodeInvalidValue.Errorf(
			"the password or private key is longer than %d KiB, the most Enroll takes", api.EnrollInputMaxSize>>10)
	}
	if len(req.Password) > 0 {
		return hostconn.PasswordLogin(string(req.Password)), nil
	}
	signer, err := ssh.ParsePrivateKey(req.PrivateKey)
	if _, encrypted := errors.AsType[*ssh.PassphraseMissingError](err); encrypted {
		return hostconn.Login{}, api.CodeInvalidValue.Errorf(
			"the private key is encrypted: Enroll takes it decrypted, as koko host enroll --key sends it once it has asked for the passphrase")
	}
	if err != nil {
		return hostconn.Login{}, api.CodeInvalidValue.Errorf("the private key does not parse: %v", err)
	}
	return hostconn.KeyLogin(signer), nil
}

// enrollable refuses to Enroll a Host whose state does not allow it: a
// Registered Host Enrolls, and a Managed one only to pin a new host key
// (P0 spec: the record).
func enrollable(h host.Reach, req api.HostEnrollRequest) error {
	switch {
	case h.State == api.HostRegistered:
		return nil
	case h.State == api.HostManaged && (req.HostKey != nil || req.ReplaceHostKey):
		return nil
	case h.State == api.HostManaged:
		return api.CodeAlreadyManaged.Errorf(
			"%s is Managed already, and koko logs in to it with the koko key: Enroll a Managed Host again only to pin a new host key, with --host-key SHA256:... or --replace-host-key", h.Name)
	}
	return fmt.Errorf("enroll %s: a Host in the state %q cannot be Enrolled", h.Name, h.State)
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

// connectRefusal is the caller's answer to a failed login to the Host. The
// connector's errors name the Host's address and Host Account, which the
// caller may see: they can act on the Host.
func connectRefusal(h host.Reach, req api.HostEnrollRequest, err error) *api.Error {
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
		what := "password"
		if len(req.PrivateKey) > 0 {
			what = "private key"
		}
		return api.CodeLoginRefused.Errorf("%s refused the %s for its Host Account %q; koko changed nothing on it", h.Name, what, h.Account)
	}
	return api.CodeHostUnreachable.Errorf("koko could not connect to %s: %v", h.Name, err)
}

// failed logs a failed Enroll and returns its refusal, err.
func (e *enroller) failed(who access.Identity, h host.Reach, err *api.Error) error {
	attrs := append(logAttrs(who, h.Name, h.ID), "code", err.Code, "err", err.Message)
	if err.Code == api.CodeUnexpectedHostKey.Name {
		e.log.Warn("enroll refused the host key", attrs...)
	} else {
		e.log.Info("enroll failed", attrs...)
	}
	return err
}

// logAttrs are the fields of every log line about an Enroll of the Host
// named hostName, whose id is hostID: the Host, the User who ran it, and
// the Device Session it came on as enrolled_by, or the Unix socket, as
// other lines name who acted.
func logAttrs(who access.Identity, hostName, hostID string) []any {
	by := "unix socket"
	if who.DeviceSession != nil {
		by = who.DeviceSession.ID
	}
	return []any{"host", hostName, "host_id", hostID, "user", who.User.Name, "enrolled_by", by}
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
