// Package op is the operation framework: the API's HTTP handler. It serves
// each registered operation at POST /v1/<name>, decodes the request (refusing
// fields it does not know), runs the operation inside a transaction it opens
// and finishes itself (IMMEDIATE for a write, read-only for a read), and
// writes the response or the error object. Until the koko server is
// initialised, it refuses every operation but the ones registered with
// BeforeInit.
//
// An operation never begins, commits or rolls back: it gets a store.Tx, and
// the transaction commits only when the operation returns no error. It reads
// the time from Now, which the framework fixes once per call.
package op

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"mime"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// Caller is who a call comes from, as the transport saw it: the Local
// caller, or a Device Connection's Device Session. Who that is, its User,
// comes from the current records on every call (package access).
type Caller struct {
	// Local is set for a call over the Unix socket. The Local caller acts as
	// the Owner (ADR 0008).
	Local bool
	// DeviceSession is the id of the Device Session a Device Connection
	// authenticated as, for a call over TCP.
	DeviceSession string
}

type callerKey struct{}

// WithCaller returns ctx carrying the caller, as the transport sets it for
// every call on a connection.
func WithCaller(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

// CallerFrom returns the caller ctx carries, if any.
func CallerFrom(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	return c, ok
}

type nowKey struct{}

// Now returns the time of the call ctx belongs to: the koko server's clock
// when the call began, in UTC to the second, as every stored time is. Every
// time an operation records or compares is this one, so a call that records
// two times records the same, and the clock hook moves them all. It panics
// outside a call, which only a bug reaches.
func Now(ctx context.Context) time.Time {
	t, ok := ctx.Value(nowKey{}).(time.Time)
	if !ok {
		panic("op: Now outside an operation's call")
	}
	return t
}

type afterTxKey struct{}

// AfterTx has f run once the transaction of the call ctx belongs to has
// ended, told whether it committed: so an operation starts what must outlive
// the call, such as a listener, only once its write holds, and undoes what it
// began when the write is rolled back. Hooks run in the order they were added.
// It panics outside a call, which only a bug reaches.
func AfterTx(ctx context.Context, f func(committed bool)) {
	hooks, ok := ctx.Value(afterTxKey{}).(*[]func(bool))
	if !ok {
		panic("op: AfterTx outside an operation's call")
	}
	*hooks = append(*hooks, f)
}

// Func carries out one operation inside its transaction.
type Func[Req, Resp any] func(ctx context.Context, tx store.Tx, caller Caller, req Req) (Resp, error)

// call is one decoded request, ready to run in its transaction.
type call func(ctx context.Context, tx store.Tx, caller Caller) (any, error)

type operation struct {
	write bool
	// beforeInit lets the operation answer before initialisation.
	beforeInit bool
	// localOnly refuses every caller but the Local caller.
	localOnly bool
	// bind decodes a request body into the call it asks for.
	bind func(body []byte) (call, error)
}

// Option changes who an operation answers, for Handle.
type Option func(*operation)

var (
	// BeforeInit lets an operation answer before the koko server is
	// initialised. Every other operation is refused with not_initialised
	// until then.
	BeforeInit Option = func(o *operation) { o.beforeInit = true }
	// LocalOnly makes an operation answer only the Local caller, over the
	// Unix socket: every other caller is refused with local_only.
	LocalOnly Option = func(o *operation) { o.localOnly = true }
)

// API serves the registered operations.
type API struct {
	store *store.Store
	log   *slog.Logger
	// initialised tells, inside a call's transaction, whether the koko
	// server is initialised.
	initialised func(context.Context, store.Tx) (bool, error)
	// clock is the koko server's clock, which Now reads once per call.
	clock func() time.Time
	ops   map[string]operation
}

// New returns an API with no operations, whose calls run against s, which
// asks initialised, inside each call's transaction, whether the koko server
// is initialised, and which reads the time of each call from clock.
func New(s *store.Store, log *slog.Logger, initialised func(context.Context, store.Tx) (bool, error), clock func() time.Time) *API {
	return &API{store: s, log: log, initialised: initialised, clock: clock, ops: map[string]operation{}}
}

// Handle registers fn as the operation o, changed by opts. Each area
// registers its operations from its own Register function; registering a
// name twice panics.
func (a *API) Handle[Req, Resp any](o api.Op[Req, Resp], fn Func[Req, Resp], opts ...Option) {
	if _, ok := a.ops[o.Name]; ok {
		panic(fmt.Sprintf("op: operation %s registered twice", o.Name))
	}
	entry := operation{
		write: o.Write,
		bind: func(body []byte) (call, error) {
			var req Req
			if err := json.Unmarshal(body, &req, json.RejectUnknownMembers(true)); err != nil {
				return nil, err
			}
			return func(ctx context.Context, tx store.Tx, caller Caller) (any, error) {
				return fn(ctx, tx, caller, req)
			}, nil
		},
	}
	for _, opt := range opts {
		opt(&entry)
	}
	a.ops[o.Name] = entry
}

// Names returns the names of the registered operations, sorted.
func (a *API) Names() []string {
	return slices.Sorted(maps.Keys(a.ops))
}

// ServeHTTP answers one call.
func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resp, err := a.serve(r)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	a.log.Debug("api call", "path", r.URL.Path, "status", http.StatusOK)
	a.writeJSON(w, http.StatusOK, resp)
}

func (a *API) serve(r *http.Request) (any, error) {
	ctx := r.Context()
	caller, ok := CallerFrom(ctx)
	if !ok {
		return nil, errors.New("a call reached the API without a caller")
	}
	name, versioned := strings.CutPrefix(r.URL.Path, api.PathPrefix)
	o, known := a.ops[name]
	if !versioned || !known {
		return nil, api.CodeUnknownOperation.Errorf("no operation is served at %s", r.URL.Path)
	}
	if r.Method != http.MethodPost {
		return nil, api.CodeMethodNotAllowed.Errorf("%s is not allowed: every operation is a POST", r.Method)
	}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		return nil, api.CodeUnsupportedMediaType.Errorf("the request body must be application/json")
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, api.CodeInvalidRequest.Errorf("read the request body: %v", err)
	}
	c, err := o.bind(body)
	if err != nil {
		return nil, decodeError(name, err)
	}
	if o.localOnly && !caller.Local {
		return nil, api.CodeLocalOnly.Errorf("%s answers only over the koko server's Unix socket: run it on the koko server's machine", name)
	}

	ctx = context.WithValue(ctx, nowKey{}, a.clock().UTC().Truncate(time.Second))
	var hooks []func(bool)
	ctx = context.WithValue(ctx, afterTxKey{}, &hooks)
	committed := false
	// Deferred, so an operation that panics still undoes what it began.
	defer func() {
		for _, f := range hooks {
			f(committed)
		}
	}()
	run := a.store.Read
	if o.write {
		run = a.store.Write
	}
	var resp any
	err = run(ctx, func(tx store.Tx) error {
		if !o.beforeInit {
			ok, err := a.initialised(ctx, tx)
			if err != nil {
				return fmt.Errorf("tell whether the koko server is initialised: %w", err)
			}
			if !ok {
				return api.CodeNotInitialised.Errorf("the koko server is not initialised yet: koko server install --owner <owner> initialises it")
			}
		}
		var err error
		resp, err = c(ctx, tx, caller)
		return err
	})
	committed = err == nil
	return resp, err
}

// decodeError turns a request that does not decode into its coded error.
func decodeError(name string, err error) *api.Error {
	if se, ok := errors.AsType[*json.SemanticError](err); ok && errors.Is(se.Err, json.ErrUnknownName) {
		field := strings.TrimPrefix(string(se.JSONPointer), "/")
		return api.CodeUnknownField.Errorf("%s has no request field %q", name, field)
	}
	return api.CodeInvalidRequest.Errorf("the request body is not a JSON object of %s's request: %v", name, err)
}

// writeError answers with the error's code and status. An error with no
// registered code is a bug: the caller gets internal_error and the log says why.
func (a *API) writeError(w http.ResponseWriter, r *http.Request, err error) {
	e, coded := errors.AsType[*api.Error](err)
	code, registered := api.Code{}, false
	if coded {
		code, registered = api.LookupCode(e.Code)
	}
	if !registered {
		a.log.Error("api call failed", "path", r.URL.Path, "err", err)
		code, e = api.CodeInternal, internalError()
	}
	a.log.Debug("api call", "path", r.URL.Path, "status", code.Status, "code", code.Name)
	a.writeJSON(w, code.Status, api.ErrorResponse{Error: *e})
}

func (a *API) writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		a.log.Error("encode api response", "err", err)
		status = api.CodeInternal.Status
		body, _ = json.Marshal(api.ErrorResponse{Error: *internalError()})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// internalError is what a caller sees of a failure the koko server did not
// expect; the log holds the cause.
func internalError() *api.Error {
	return api.CodeInternal.Errorf("the koko server failed; its log says why")
}
