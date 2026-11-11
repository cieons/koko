// Package op is the operation framework: the API's HTTP handler. It serves
// each registered operation at POST /v1/<name>, decodes the request (refusing
// fields it does not know), runs the operation inside a transaction it opens
// and finishes itself (IMMEDIATE for a write, read-only for a read), and
// writes the response or the error object. Until the koko server is
// initialised, it refuses every operation but the ones registered with
// BeforeInit. Every write carries an Idempotency-Key, whose response it
// stores in the write's own transaction (idempotency.go), and no write is
// cancelled because its caller dropped.
//
// An operation never begins, commits or rolls back: it gets a store.Tx, and
// the transaction commits only when the operation returns no error. It reads
// the time from Now, which the framework fixes once per call. A write whose
// work takes long, such as logging in to a Host, does that work in a Prepare
// before its transaction (HandlePrepared), so it never holds the store's
// write lock while it waits.
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

	"github.com/cieons/koko/internal/server/seal"
	"github.com/cieons/koko/internal/server/store"
	"github.com/cieons/koko/internal/shared/api"
)

// Caller is who a call comes from, as the transport or the web listener
// saw it: the Local caller, a Device Connection's Device Session, or a
// browser's Sign-in. Who that is, its User, comes from the current records
// on every call (package access).
type Caller struct {
	// Local is set for a call over the Unix socket. The Local caller acts as
	// the Owner (ADR 0008).
	Local bool
	// DeviceSession is the id of the Device Session a Device Connection
	// authenticated as, for a call over TCP.
	DeviceSession string
	// SignIn is the id of the Sign-in whose cookie a call on the web
	// listener carried (ADR 0043).
	SignIn string
	// CLIVersion is the version the CLI stated in its SSH version string,
	// v0.3.0 for one that stated none (compat.CLIVersion), for a call over
	// the Unix socket or a Device Connection.
	CLIVersion string
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

// Prepare is the part of a write that runs before its transaction, outside
// any, so that it may take long, as logging in to a Host does, without
// holding the store's write lock, which every other write would wait for
// and give up on after SQLite's busy timeout. It reads through read, each
// time in a read-only transaction of its own, and returns what the write's
// transaction needs. It runs once per write: a retry answered with the
// stored response never prepares again, and a write's caller dropping does
// not cancel it. A Prepare that fails ends the write, which applies and
// stores nothing.
type Prepare[Req, Prep any] func(ctx context.Context, read Read, caller Caller, req Req) (Prep, error)

// PreparedFunc carries out a prepared write inside its transaction, given
// what its Prepare returned. The record may have changed since Prepare read
// it, so it checks again what it relies on.
type PreparedFunc[Req, Prep, Resp any] func(ctx context.Context, tx store.Tx, caller Caller, req Req, prep Prep) (Resp, error)

// Read runs fn in a read-only transaction that the framework opens and
// finishes, for a Prepare.
type Read func(fn func(tx store.Tx) error) error

// call is one decoded request, ready to run.
type call struct {
	// prepare, when set, runs before the write's transaction, outside any.
	prepare func(ctx context.Context, read Read, caller Caller) error
	// run carries the call out inside its transaction.
	run func(ctx context.Context, tx store.Tx, caller Caller) (any, error)
}

type operation struct {
	write bool
	// revision is the operation's revision (api.Op.Revision), at least 1.
	revision int
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
	// rootKey seals the responses stored for Idempotency-Keys.
	rootKey *seal.RootKey
	log     *slog.Logger
	// initialised tells, inside a call's transaction, whether the koko
	// server is initialised.
	initialised func(context.Context, store.Tx) (bool, error)
	// clock is the koko server's clock, which Now reads once per call.
	clock    func() time.Time
	ops      map[string]operation
	inflight inflight
}

// New returns an API with no operations, whose calls run against s, which
// seals the responses it stores for Idempotency-Keys with rootKey, asks
// initialised, inside each call's transaction, whether the koko server is
// initialised, and reads the time of each call from clock.
func New(s *store.Store, rootKey *seal.RootKey, log *slog.Logger, initialised func(context.Context, store.Tx) (bool, error), clock func() time.Time) *API {
	return &API{
		store: s, rootKey: rootKey, log: log, initialised: initialised, clock: clock,
		ops: map[string]operation{}, inflight: inflight{running: map[idempotencyKey]chan struct{}{}},
	}
}

// Handle registers fn as the operation o, changed by opts. Each area
// registers its operations from its own Register function; registering a
// name twice panics.
func (a *API) Handle[Req, Resp any](o api.Op[Req, Resp], fn Func[Req, Resp], opts ...Option) {
	a.register(o.Info(), func(body []byte) (call, error) {
		req, err := decode[Req](body)
		if err != nil {
			return call{}, err
		}
		return call{run: func(ctx context.Context, tx store.Tx, caller Caller) (any, error) {
			return fn(ctx, tx, caller, req)
		}}, nil
	}, opts)
}

// HandlePrepared registers the write o in two parts: prepare, before the
// write's transaction and outside any, then fn inside it with what prepare
// returned. Registering a read this way panics: a read holds no lock.
func (a *API) HandlePrepared[Req, Prep, Resp any](o api.Op[Req, Resp], prepare Prepare[Req, Prep], fn PreparedFunc[Req, Prep, Resp], opts ...Option) {
	if !o.Write {
		panic(fmt.Sprintf("op: %s is a read, which needs no Prepare", o.Name))
	}
	a.register(o.Info(), func(body []byte) (call, error) {
		req, err := decode[Req](body)
		if err != nil {
			return call{}, err
		}
		var prep Prep
		return call{
			prepare: func(ctx context.Context, read Read, caller Caller) (err error) {
				prep, err = prepare(ctx, read, caller, req)
				return err
			},
			run: func(ctx context.Context, tx store.Tx, caller Caller) (any, error) {
				return fn(ctx, tx, caller, req, prep)
			},
		}, nil
	}, opts)
}

// register adds the operation o, whose request bodies bind decodes,
// changed by opts.
func (a *API) register(o api.OpInfo, bind func(body []byte) (call, error), opts []Option) {
	if _, ok := a.ops[o.Name]; ok {
		panic(fmt.Sprintf("op: operation %s registered twice", o.Name))
	}
	entry := operation{write: o.Write, revision: o.Revision, bind: bind}
	for _, opt := range opts {
		opt(&entry)
	}
	a.ops[o.Name] = entry
}

// decode reads a request body into its type, refusing a member the type
// does not know.
func decode[Req any](body []byte) (Req, error) {
	var req Req
	err := json.Unmarshal(body, &req, json.RejectUnknownMembers(true))
	return req, err
}

// Names returns the names of the registered operations, sorted: what the
// koko server reports it serves (wire.Report).
func (a *API) Names() []string {
	return slices.Sorted(maps.Keys(a.ops))
}

// Revisions returns the revision of each registered operation above
// revision 1, by name: what the koko server reports of them
// (wire.Report.Revisions).
func (a *API) Revisions() map[string]int {
	revs := map[string]int{}
	for name, o := range a.ops {
		if o.revision > 1 {
			revs[name] = o.revision
		}
	}
	return revs
}

// ServeHTTP answers one call.
func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, replayed, err := a.serve(r)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	attrs := []any{"path", r.URL.Path, "status", http.StatusOK}
	if replayed {
		attrs = append(attrs, "replayed", true)
	}
	a.log.Debug("api call", attrs...)
	writeBody(w, http.StatusOK, body)
}

// serve runs one call and returns its encoded response, which replayed says
// was stored for the write's Idempotency-Key by an earlier attempt.
func (a *API) serve(r *http.Request) (body []byte, replayed bool, err error) {
	ctx := r.Context()
	caller, ok := CallerFrom(ctx)
	if !ok {
		return nil, false, errors.New("a call reached the API without a caller")
	}
	name, versioned := strings.CutPrefix(r.URL.Path, api.PathPrefix)
	o, known := a.ops[name]
	if !versioned || !known {
		return nil, false, api.CodeUnknownOperation.Errorf("no operation is served at %s", r.URL.Path)
	}
	if r.Method != http.MethodPost {
		return nil, false, api.CodeMethodNotAllowed.Errorf("%s is not allowed: every operation is a POST", r.Method)
	}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		return nil, false, api.CodeUnsupportedMediaType.Errorf("the request body must be application/json")
	}
	reqBody, err := io.ReadAll(io.LimitReader(r.Body, api.MaxRequestBody+1))
	if err != nil {
		return nil, false, api.CodeInvalidRequest.Errorf("read the request body: %v", err)
	}
	if len(reqBody) > api.MaxRequestBody {
		return nil, false, api.CodeRequestTooLarge.Errorf("the request body is over the koko server's limit of %d MiB", api.MaxRequestBody>>20)
	}
	c, err := o.bind(reqBody)
	if err != nil {
		return nil, false, decodeError(name, err)
	}
	if o.localOnly && !caller.Local {
		return nil, false, api.CodeLocalOnly.Errorf("%s answers only over the koko server's Unix socket: run it on the koko server's machine", name)
	}
	var write keyedWrite
	if o.write {
		k := r.Header.Get(api.IdempotencyKeyHeader)
		if !api.ValidIdempotencyKey(k) {
			return nil, false, api.CodeInvalidRequest.Errorf("%s is a write, and every write carries an %s header: 128 random bits in 32 lowercase hex characters", name, api.IdempotencyKeyHeader)
		}
		write = newKeyedWrite(caller, k, name, reqBody)
		// A write runs to its end even when its caller drops, and stores
		// its response for the caller's retry.
		ctx = context.WithoutCancel(ctx)
		leave := a.inflight.enter(write.key, func() {
			a.log.Debug("api call waits for its first attempt", "path", r.URL.Path)
		})
		defer leave()
	}

	ctx, finish := a.begin(ctx)
	now := Now(ctx)
	committed := false
	// Deferred, so an operation that panics still undoes what it began.
	defer func() { finish(committed) }()
	if c.prepare != nil {
		// A prepared write prepares outside any transaction, unless an
		// earlier attempt has stored its response already.
		err := a.store.Read(ctx, func(tx store.Tx) (err error) {
			if err := a.checkInitialised(ctx, tx, o); err != nil {
				return err
			}
			body, replayed, err = a.lookupResponse(ctx, tx, write, now)
			return err
		})
		if err != nil || replayed {
			return body, replayed, err
		}
		read := func(fn func(store.Tx) error) error { return a.store.Read(ctx, fn) }
		if err := c.prepare(ctx, read, caller); err != nil {
			return nil, false, err
		}
	}
	run := a.store.Read
	if o.write {
		run = a.store.Write
	}
	err = run(ctx, func(tx store.Tx) error {
		if err := a.checkInitialised(ctx, tx, o); err != nil {
			return err
		}
		if o.write {
			stored, found, err := a.findResponse(ctx, tx, write, now)
			if err != nil || found {
				body, replayed = stored, found
				return err
			}
		}
		resp, err := c.run(ctx, tx, caller)
		if err != nil {
			return err
		}
		// Encoded inside the transaction, so a response that cannot be
		// written rolls the write back.
		if body, err = json.Marshal(resp); err != nil {
			return fmt.Errorf("encode the %s response: %w", name, err)
		}
		if o.write {
			return a.keepResponse(ctx, tx, write, body, now)
		}
		return nil
	})
	committed = err == nil
	return body, replayed, err
}

// Do runs fn as a write of the koko server's own, which no caller asked
// for: in a transaction of its own, which commits only when fn returns no
// error, with Now and AfterTx as an operation has them. Start-up uses it to
// initialise a fresh koko server before it serves anything (ADR 0038), and
// the signin area for the web listener's /auth/ endpoints, which come
// before any call.
func (a *API) Do(ctx context.Context, fn func(ctx context.Context, tx store.Tx) error) error {
	ctx, finish := a.begin(ctx)
	committed := false
	// Deferred, so a step that panics still undoes what it began.
	defer func() { finish(committed) }()
	err := a.store.Write(ctx, func(tx store.Tx) error { return fn(ctx, tx) })
	committed = err == nil
	return err
}

// begin starts a call's context: it fixes the call's time from the koko
// server's clock, for Now, and holds its AfterTx hooks, which finish runs
// once the call's transaction has ended, told whether it committed.
func (a *API) begin(ctx context.Context) (_ context.Context, finish func(committed bool)) {
	ctx = context.WithValue(ctx, nowKey{}, a.clock().UTC().Truncate(time.Second))
	var hooks []func(bool)
	ctx = context.WithValue(ctx, afterTxKey{}, &hooks)
	return ctx, func(committed bool) {
		for _, f := range hooks {
			f(committed)
		}
	}
}

// checkInitialised refuses, inside tx, a call to o while the koko server is
// not initialised, unless o answers before initialisation.
func (a *API) checkInitialised(ctx context.Context, tx store.Tx, o operation) error {
	if o.beforeInit {
		return nil
	}
	ok, err := a.initialised(ctx, tx)
	if err != nil {
		return fmt.Errorf("tell whether the koko server is initialised: %w", err)
	}
	if !ok {
		return api.CodeNotInitialised.Errorf("the koko server is not initialised yet: sudo koko server install --owner <user> initialises a koko service, " +
			"and kokod run, such as a container's, initialises its koko server at start given --owner <user> and --address <host[:port]>, or KOKO_OWNER and KOKO_ADDRESS")
	}
	return nil
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
	body, _ := json.Marshal(api.ErrorResponse{Error: *e}) // strings and ints: never fails
	writeBody(w, code.Status, body)
}

// writeBody answers with status and body, a JSON object.
func writeBody(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// internalError is what a caller sees of a failure the koko server did not
// expect; the log holds the cause.
func internalError() *api.Error {
	return api.CodeInternal.Errorf("the koko server failed; its log says why")
}
