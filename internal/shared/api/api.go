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
	"reflect"
	"slices"
)

// PathPrefix comes before an operation's name in its URL path.
const PathPrefix = "/v1/"

// Op is one API operation, with its request and response types.
type Op[Req, Resp any] struct {
	// Name is "<noun>.<verb>", such as "space.add"; the operation is POST /v1/<Name>.
	Name string
	// Write says the operation may change the record, so it runs in an
	// IMMEDIATE transaction; a read runs in a read-only one.
	Write bool
}

// Info describes the operation without its types' parameters.
func (o Op[Req, Resp]) Info() OpInfo {
	return OpInfo{
		Name:     o.Name,
		Write:    o.Write,
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
	Name     string
	Write    bool
	Request  reflect.Type
	Response reflect.Type
}

// operations lists every area's operations; an area's first ticket adds its
// line.
var operations = [][]Operation{
	spaceOperations,
	serverOperations,
	deviceOperations,
	userOperations,
}

// Operations returns every API operation.
func Operations() []Operation {
	return slices.Concat(operations...)
}
