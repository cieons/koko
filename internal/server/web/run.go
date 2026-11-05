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

// runID names one run of koko web in its cookie.
type runID [16]byte

// runs holds the runs of koko web and their tokens, in memory: a run ends
// with its connection, and nothing about it outlives the koko server.
type runs struct {
	mu     sync.Mutex
	byConn map[*transport.Connection]runID
	// tokens holds each token by its SHA-256, until it expires or its run
	// ends, so that a spent one is told apart from an unknown one.
	tokens map[[sha256.Size]byte]*token
}

// token is one token koko web was given.
type token struct {
	conn    *transport.Connection
	expires time.Time
	spent   bool
}

func newRuns() *runs {
	return &runs{
		byConn: map[*transport.Connection]runID{},
		tokens: map[[sha256.Size]byte]*token{},
	}
}

// issue starts the run on conn, if none runs there yet, and returns a new
// token for it, issued at now and usable for TokenLifetime. The tokens
// expired by now are forgotten.
func (rs *runs) issue(conn *transport.Connection, now time.Time) string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if _, ok := rs.byConn[conn]; !ok {
		var id runID
		_, _ = rand.Read(id[:]) // crypto/rand never fails
		rs.byConn[conn] = id
		context.AfterFunc(conn.Context(), func() { rs.end(conn) })
	}
	maps.DeleteFunc(rs.tokens, func(_ [sha256.Size]byte, t *token) bool { return !now.Before(t.expires) })
	text := rand.Text()
	rs.tokens[sha256.Sum256([]byte(text))] = &token{conn: conn, expires: now.Add(TokenLifetime)}
	return text
}

// end ends the run on conn, with its tokens.
func (rs *runs) end(conn *transport.Connection) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	delete(rs.byConn, conn)
	maps.DeleteFunc(rs.tokens, func(_ [sha256.Size]byte, t *token) bool { return t.conn == conn })
}

// id returns the id of the run on conn.
func (rs *runs) id(conn *transport.Connection) (runID, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	id, ok := rs.byConn[conn]
	return id, ok
}

// spendResult is what spending a token found.
type spendResult int

const (
	// tokenTraded was spent now, for the cookie.
	tokenTraded spendResult = iota
	// tokenAlreadySpent was spent before.
	tokenAlreadySpent
	// tokenUnknown has expired, or was never issued for the run on this
	// connection.
	tokenUnknown
)

// spend spends text, a token presented on conn at now, and returns the id of
// the run it lets in. A token is usable only before it expires.
func (rs *runs) spend(conn *transport.Connection, text string, now time.Time) (runID, spendResult) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	t, ok := rs.tokens[sha256.Sum256([]byte(text))]
	id, running := rs.byConn[conn]
	switch {
	case !ok || !running || t.conn != conn || !now.Before(t.expires):
		return runID{}, tokenUnknown
	case t.spent:
		return runID{}, tokenAlreadySpent
	}
	t.spent = true
	return id, tokenTraded
}

// The cookie is a version byte, the run's id and an HMAC-SHA256 under the
// cookie key over both, in base64url without padding.
const (
	cookieVersion = 1
	cookieLabel   = "koko web cookie\x00"
	cookieSize    = 1 + len(runID{}) + sha256.Size
)

// signCookie returns the cookie of the run id.
func signCookie(key []byte, id runID) string {
	payload := append([]byte{cookieVersion}, id[:]...)
	return base64.RawURLEncoding.EncodeToString(append(payload, cookieMAC(key, payload)...))
}

// verifyCookie tells whether value is the cookie of the run id.
func verifyCookie(key []byte, value string, id runID) bool {
	b, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(b) != cookieSize || b[0] != cookieVersion {
		return false
	}
	payload, mac := b[:1+len(id)], b[1+len(id):]
	return hmac.Equal(mac, cookieMAC(key, payload)) && runID(payload[1:]) == id
}

func cookieMAC(key, payload []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(cookieLabel))
	m.Write(payload)
	return m.Sum(nil)
}
