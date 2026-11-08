package web

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"maps"
	"sync"
	"time"

	"github.com/cieons/koko/internal/server/transport"
)

// subject is what a token and a cookie are bound to (ADR 0022): a Device
// Session, so that its cookie lasts until it is Revoked, or, for the Local
// caller, who has none, a run of koko web, which is its connection. It is a
// kind byte and then the Device Session's id or the run's 16 random bytes.
type subject string

const (
	kindDeviceSession = 'd'
	kindRun           = 'r'
)

// runs holds the runs of koko web over the Unix socket, the tokens koko web
// was given, and the Device Sessions Revoked since the koko server started,
// all in memory: a token outlives no koko server, a run ends with its
// connection, and a Revoked Device Session never connects again.
type runs struct {
	mu sync.Mutex
	// byConn holds the subject of the run on each connection of the Local
	// caller's.
	byConn map[*transport.Connection]subject
	// tokens holds each token by its SHA-256, until it expires or its
	// subject ends, so that a spent one is told apart from an unknown one.
	tokens map[[sha256.Size]byte]*token
	// revoked holds the Device Sessions Revoked while this koko server ran,
	// whose connections may still be open for a moment.
	revoked map[string]bool
}

// token is one token koko web was given.
type token struct {
	subject subject
	expires time.Time
	spent   bool
}

func newRuns() *runs {
	return &runs{
		byConn:  map[*transport.Connection]subject{},
		tokens:  map[[sha256.Size]byte]*token{},
		revoked: map[string]bool{},
	}
}

// issue returns a new token, issued at now and usable for TokenLifetime,
// for the subject of conn: its Device Session's, or, for the Local caller,
// the run on conn, which it starts if none runs there yet. The tokens
// expired by now are forgotten.
func (rs *runs) issue(conn *transport.Connection, now time.Time) string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	sub, ok := rs.subjectLocked(conn)
	if !ok {
		sub = newRunSubject()
		rs.byConn[conn] = sub
		context.AfterFunc(conn.Context(), func() { rs.end(conn) })
	}
	maps.DeleteFunc(rs.tokens, func(_ [sha256.Size]byte, t *token) bool { return !now.Before(t.expires) })
	text := rand.Text()
	rs.tokens[sha256.Sum256([]byte(text))] = &token{subject: sub, expires: now.Add(TokenLifetime)}
	return text
}

// end ends the run on conn, with its tokens.
func (rs *runs) end(conn *transport.Connection) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	sub := rs.byConn[conn]
	delete(rs.byConn, conn)
	rs.forgetLocked(sub)
}

// revoke records that the Device Sessions ids were Revoked, and forgets
// their tokens.
func (rs *runs) revoke(ids []string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	for _, id := range ids {
		rs.revoked[id] = true
		rs.forgetLocked(deviceSessionSubject(id))
	}
}

// forgetLocked forgets the tokens of sub.
func (rs *runs) forgetLocked(sub subject) {
	maps.DeleteFunc(rs.tokens, func(_ [sha256.Size]byte, t *token) bool { return t.subject == sub })
}

// subjectState is what a request's connection is bound to.
type subjectState int

const (
	// subjectFound: the connection has a subject.
	subjectFound subjectState = iota
	// subjectNone: the Local caller's connection runs no koko web.
	subjectNone
	// subjectRevoked: the connection's Device Session was Revoked.
	subjectRevoked
)

// subject returns the subject a request on conn is let in by.
func (rs *runs) subject(conn *transport.Connection) (subject, subjectState) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if id := conn.Caller().DeviceSession; rs.revoked[id] {
		return "", subjectRevoked
	}
	sub, ok := rs.subjectLocked(conn)
	if !ok {
		return "", subjectNone
	}
	return sub, subjectFound
}

// subjectLocked returns the subject of conn: its Device Session's, or the
// run on it of the Local caller's, if one runs there.
func (rs *runs) subjectLocked(conn *transport.Connection) (subject, bool) {
	if id := conn.Caller().DeviceSession; id != "" {
		return deviceSessionSubject(id), true
	}
	sub, ok := rs.byConn[conn]
	return sub, ok
}

// deviceSessionSubject is the subject of the Device Session id.
func deviceSessionSubject(id string) subject {
	return subject(append([]byte{kindDeviceSession}, id...))
}

// newRunSubject is the subject of a new run of koko web, named by 16
// random bytes.
func newRunSubject() subject {
	var id [16]byte
	_, _ = rand.Read(id[:]) // crypto/rand never fails
	return subject(append([]byte{kindRun}, id[:]...))
}

// spendResult is what spending a token found.
type spendResult int

const (
	// tokenTraded was spent now, for the cookie.
	tokenTraded spendResult = iota
	// tokenAlreadySpent was spent before.
	tokenAlreadySpent
	// tokenUnknown has expired, or was never issued for sub.
	tokenUnknown
)

// spend spends text, a token presented at now on a request let in by sub. A
// token is usable only before it expires.
func (rs *runs) spend(sub subject, text string, now time.Time) spendResult {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	t, ok := rs.tokens[sha256.Sum256([]byte(text))]
	switch {
	case !ok || t.subject != sub || !now.Before(t.expires):
		return tokenUnknown
	case t.spent:
		return tokenAlreadySpent
	}
	t.spent = true
	return tokenTraded
}

// The cookie is a version byte, the subject and an HMAC-SHA256 under the
// cookie key over both, in base64url without padding.
const (
	cookieVersion = 2
	cookieLabel   = "koko web cookie\x00"
)

// signCookie returns the cookie of sub.
func signCookie(key []byte, sub subject) string {
	payload := append([]byte{cookieVersion}, sub...)
	return base64.RawURLEncoding.EncodeToString(append(payload, cookieMAC(key, payload)...))
}

// verifyCookie tells whether value is the cookie of sub.
func verifyCookie(key []byte, value string, sub subject) bool {
	b, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(b) < 1+sha256.Size || b[0] != cookieVersion {
		return false
	}
	payload, mac := b[:len(b)-sha256.Size], b[len(b)-sha256.Size:]
	return hmac.Equal(mac, cookieMAC(key, payload)) && subject(payload[1:]) == sub
}

func cookieMAC(key, payload []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(cookieLabel))
	m.Write(payload)
	return m.Sum(nil)
}
