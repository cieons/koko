// Package api holds the koko server's API as Go types, shared by the koko
// server and the CLI (ADR 0015): every operation with its request and
// response types, the error object and the error codes.
//
// Each operation is POST /v1/<noun>.<verb> with a JSON body, inside an
// api@koko SSH channel. A response is always a JSON object, so a list is
// wrapped in one. The koko server refuses request fields it does not know;
// clients ignore response fields they do not know.
//
// Each area (space, user, device, host, ...) declares its types, operations
// and error codes in a file of its own, and adds one line for its lists to
// the central lists in this file and errors.go.
package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// PathPrefix comes before an operation's name in its URL path.
const PathPrefix = "/v1/"

// IdempotencyKeyHeader is the HTTP header every write carries: its
// Idempotency-Key, which NewIdempotencyKey makes, new for each write and the
// same on its retry. The koko server keeps a write's response for 10 minutes
// per caller and answers a retry with it, so a retried write applies once.
// A write without a valid key is refused with CodeInvalidRequest; a read's
// key is ignored.
const IdempotencyKeyHeader = "Idempotency-Key"

// idempotencyKeySize is an Idempotency-Key's length in bytes: 128 random
// bits.
const idempotencyKeySize = 16

// NewIdempotencyKey returns a new Idempotency-Key: 128 random bits in 32
// lowercase hex characters.
func NewIdempotencyKey() string {
	var b [idempotencyKeySize]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails
	return hex.EncodeToString(b[:])
}

// ValidIdempotencyKey tells whether k is an Idempotency-Key as
// NewIdempotencyKey writes one.
func ValidIdempotencyKey(k string) bool {
	b, err := hex.DecodeString(k)
	return err == nil && len(b) == idempotencyKeySize && hex.EncodeToString(b) == k
}

// Op is one API operation, with its request and response types.
type Op[Req, Resp any] struct {
	// Name is "<noun>.<verb>", such as "space.add"; the operation is POST /v1/<Name>.
	Name string
	// Write says the operation may change the record, so it runs in an
	// IMMEDIATE transaction; a read runs in a read-only one.
	Write bool
	// Revision is the operation's revision, 1 when left out (ADR 0049). It
	// rises with each change to what the operation takes: a new request
	// field, or a new value an existing field takes. A response only gains
	// fields, and raises nothing. The koko server reports it, and a CLI
	// sends no field that a koko server's revision lacks.
	Revision int
	// Added names each request field added after revision 1, by its path,
	// with the revision that added it. A path joins JSON names with ".",
	// a list's name followed by "[]" for the fields of its items, such as
	// "hosts[].ttl". Each is left out of a request when unset (omitzero or
	// omitempty), so that a CLI sends it only when an option sets it. A
	// type that several operations take, such as Filter, gains a field in
	// each of them, at each one's own next revision. A new value of an
	// existing field has no entry: a command checks the koko server's
	// revision itself before it sends one.
	Added map[string]int
}

// Info describes the operation without its types' parameters.
func (o Op[Req, Resp]) Info() OpInfo {
	return OpInfo{
		Name:     o.Name,
		Write:    o.Write,
		Revision: max(o.Revision, 1),
		Added:    o.Added,
		Request:  reflect.TypeFor[Req](),
		Response: reflect.TypeFor[Resp](),
	}
}

// Path is the operation's URL path.
func (o Op[Req, Resp]) Path() string {
	return PathPrefix + o.Name
}

// Operation is any Op, for lists that hold operations of different types.
type Operation interface {
	Info() OpInfo
}

// OpInfo describes one operation.
type OpInfo struct {
	Name  string
	Write bool
	// Revision is Op.Revision, at least 1.
	Revision int
	// Added is Op.Added.
	Added    map[string]int
	Request  reflect.Type
	Response reflect.Type
}

// FieldAfter returns a request field that body, a request of the
// operation in JSON, sets, and that was added after revision, a koko
// server's revision of the operation; ok is false when body sets none. A
// field is set when body holds its name, in any of its list's items for
// one inside a list. Of several, it returns the first by path.
func (o OpInfo) FieldAfter(body []byte, revision int) (field string, ok bool, err error) {
	var later []string
	for path, added := range o.Added {
		if added > revision {
			later = append(later, path)
		}
	}
	if len(later) == 0 {
		return "", false, nil
	}
	slices.Sort(later)
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return "", false, fmt.Errorf("read %s request: %w", o.Name, err)
	}
	for _, path := range later {
		if sets(v, path) {
			return path, true, nil
		}
	}
	return "", false, nil
}

// sets tells whether v, a request decoded from JSON, holds the field at
// path, as Op.Added names fields.
func sets(v any, path string) bool {
	name, rest, nested := strings.Cut(path, ".")
	name, each := strings.CutSuffix(name, "[]")
	obj, ok := v.(map[string]any)
	if !ok {
		return false
	}
	x, ok := obj[name]
	switch {
	case !ok:
		return false
	case !nested:
		return true
	case each:
		items, _ := x.([]any)
		return slices.ContainsFunc(items, func(item any) bool { return sets(item, rest) })
	}
	return sets(x, rest)
}

// operations lists every area's operations; an area's first ticket adds its
// line.
var operations = [][]Operation{
	spaceOperations,
	serverOperations,
	deviceOperations,
	userOperations,
	webOperations,
	hostOperations,
	secretOperations,
	attrOperations,
	enrollOperations,
	serviceOperations,
	signinOperations,
}

// Operations returns every API operation.
func Operations() []Operation {
	return slices.Concat(operations...)
}
