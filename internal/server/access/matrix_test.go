package access_test

// The access matrix (S2): every API operation called as each kind of
// caller, against the answer the access check must give. Each area keeps its
// rows in a file of its own, matrix_<area>_test.go, and adds one line to
// areaRows; an operation in api.Operations without a row fails the test.

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"os"
	"slices"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

// areaRows lists every area's rows; an area's first operation adds its line.
var areaRows = [][]row{
	spaceRows,
	userRows,
	deviceRows,
	serverRows,
	webRows,
	hostRows,
	secretRows,
	attrRows,
	enrollRows,
	serviceRows,
	signinRows,
}

// A row states, for one operation, the answer each caller gets.
type row struct {
	op api.Operation
	// name tells apart rows of one operation, such as "for another User";
	// empty for an operation's only row.
	name string
	// request is the call's JSON body, given the name of the Space the
	// caller acts in: the target (see caller).
	request func(target string) any
	// requestIn, set in place of request, makes the body from the
	// fixture's records too, such as the id of a Sign-in its template
	// holds.
	requestIn func(f *fixture, target string) any
	// want is each caller's answer; a row states every caller's.
	want map[caller]answer
	// setup, when set, records what the call needs before it is made, as
	// the Local caller, given the name of the target Space, which it records
	// into: a Host for host.show, say. It runs only where the target Space
	// is recorded, so the twin of an unknown answer holds none of it.
	setup func(f *fixture, target string)
}

func (r row) String() string {
	if r.name == "" {
		return r.op.Info().Name
	}
	return r.op.Info().Name + " " + r.name
}

// caller is one kind of caller, each acting in a target Space of the
// fixture.
type caller int

const (
	// ownerDevice is the Owner, alice, through a Device Session, in ops.
	ownerDevice caller = iota
	// local is the Local caller, over the Unix socket, in ops.
	local
	// member is bob, a Member of ops, in ops.
	member
	// personal is carol, a Member of no shared Space, in her own Personal
	// Space.
	personal
	// outsider is carol, in ops, which she is no Member of.
	outsider
	// ownerSignIn is the Owner, alice, through a Sign-in in a browser on
	// the web listener, in ops.
	ownerSignIn
	// memberSignIn is bob, a Member of ops, through a Sign-in, in ops.
	memberSignIn
	// adminDevice is eve, an Administrator and a Member of no shared
	// Space, through a Device Session, in ops.
	adminDevice
	// adminSignIn is eve, an Administrator, through a Sign-in, in ops.
	adminSignIn
)

var callers = []caller{ownerDevice, local, member, personal, outsider, ownerSignIn, memberSignIn, adminDevice, adminSignIn}

func (c caller) String() string {
	return [...]string{
		"the Owner's device", "the Local caller", "a Member of the Space",
		"the User whose Personal Space it is", "a Member outside the Space",
		"the Owner's Sign-in", "a Member of the Space's Sign-in",
		"an Administrator's device", "an Administrator's Sign-in",
	}[c]
}

// user is the User the caller acts as; the Local caller acts as the Owner.
func (c caller) user() string {
	return [...]string{harness.Owner, harness.Owner, "bob", "carol", "carol", harness.Owner, "bob", administrator, administrator}[c]
}

// signedIn tells whether the caller calls through a Sign-in.
func (c caller) signedIn() bool {
	return c == ownerSignIn || c == memberSignIn || c == adminSignIn
}

// administrator is the Administrator the Administrators' callers act as,
// and otherAdministrator the other Administrator of the fixture, whom only
// the Owner acts on.
const (
	administrator      = "eve"
	otherAdministrator = "frank"
)

// target is the Space the caller acts in.
func (c caller) target() string {
	if c == personal {
		return "carol"
	}
	return targetSpace
}

// targetSpace is the shared Space of the fixture, which bob is a Member of.
const targetSpace = "ops"

// answer is what a call gets.
type answer struct {
	// code is the error code of a refusal, or "" for success.
	code string
	// unknown says the call is answered, byte for byte, as the same call
	// on a koko server where the target Space was never recorded.
	unknown bool
}

var (
	// allowed: the call succeeds.
	allowed = answer{}
	// ownerOnly: only the Owner, or only the Owner or an Administrator, may
	// make the call: the code owner_only.
	ownerOnly = answer{code: api.CodeOwnerOnly.Name}
	// localOnly: only the Local caller may make the call.
	localOnly = answer{code: api.CodeLocalOnly.Name}
	// unknown: what the caller cannot see answers as what does not exist.
	unknown = answer{unknown: true}
)

// allowedBut is the answer to a call the access check lets through and the
// operation refuses by a rule of its own, with code.
func allowedBut(code api.Code) answer {
	return answer{code: code.Name}
}

// administersAnswers is the answers to an operation only the Owner or an
// Administrator may make, on a Member if it acts on a User: a Member is
// refused whatever the call names.
var administersAnswers = map[caller]answer{
	ownerDevice: allowed, local: allowed, member: ownerOnly, personal: ownerOnly, outsider: ownerOnly,
	ownerSignIn: allowed, memberSignIn: ownerOnly,
	adminDevice: allowed, adminSignIn: allowed,
}

// ownerOnlyAnswers is the answers to a call only the Owner may make, such
// as one that acts on the Owner or another Administrator: anyone else is
// refused whatever the call names.
var ownerOnlyAnswers = map[caller]answer{
	ownerDevice: allowed, local: allowed, member: ownerOnly, personal: ownerOnly, outsider: ownerOnly,
	ownerSignIn: allowed, memberSignIn: ownerOnly,
	adminDevice: ownerOnly, adminSignIn: ownerOnly,
}

// anyoneAnswers is the answers to an operation every caller may make.
var anyoneAnswers = map[caller]answer{
	ownerDevice: allowed, local: allowed, member: allowed, personal: allowed, outsider: allowed,
	ownerSignIn: allowed, memberSignIn: allowed,
	adminDevice: allowed, adminSignIn: allowed,
}

// Every operation has a row, and every row states each caller's answer.
func TestEveryOperationHasRowsInTheAccessMatrix(t *testing.T) {
	rows := slices.Concat(areaRows...)
	for _, o := range api.Operations() {
		if !slices.ContainsFunc(rows, func(r row) bool { return r.op.Info().Name == o.Info().Name }) {
			t.Errorf("%s has no row in the access matrix: add its rows to matrix_<area>_test.go", o.Info().Name)
		}
	}
	for _, r := range rows {
		for _, c := range callers {
			if _, ok := r.want[c]; !ok {
				t.Errorf("the row %s states no answer for %s", r, c)
			}
		}
	}
}

// Each operation, called as each caller, gets the answer its row states.
func TestAccessMatrix(t *testing.T) {
	rows := slices.Concat(areaRows...)
	templates := newTemplates(t, rows)
	for _, r := range rows {
		for _, c := range callers {
			want, ok := r.want[c]
			if !ok {
				continue
			}
			t.Run(fmt.Sprintf("%s as %s", r, c), func(t *testing.T) {
				t.Parallel()
				status, body := newFixture(t, templates[keyOf(true, c)], c).send(c, r)
				switch {
				case want.unknown:
					twinStatus, twinBody := newFixture(t, templates[keyOf(false, c)], c).send(c, r)
					if twinStatus == http.StatusOK {
						t.Fatalf("with no %s recorded the call succeeded: %s", c.target(), twinBody)
					}
					if status != twinStatus || string(body) != string(twinBody) {
						t.Errorf("answered %d %s, want it as for a Space never recorded: %d %s", status, body, twinStatus, twinBody)
					}
				case want.code == "":
					if status != http.StatusOK {
						t.Errorf("answered %d %s, want success", status, body)
					}
				default:
					code, _ := api.LookupCode(want.code)
					var e api.ErrorResponse
					if err := json.Unmarshal(body, &e); err != nil || status != code.Status || e.Error.Code != want.code {
						t.Errorf("answered %d %s, want %d %s", status, body, code.Status, want.code)
					}
				}
			})
		}
	}
}

// fixture is a koko server with the matrix's records: the Owner alice; the
// Members bob, carol and dave, who has not joined; the Administrators eve
// and frank, who have not joined either; a Sign-in each of alice and frank;
// and, unless the twin of a call answered as unknown, the shared Space ops
// with the Member bob. For a caller through a Sign-in, its web listener is
// on and its User is signed in; for any other the Web URL is not set.
type fixture struct {
	t *testing.T
	h *harness.Harness
	// withTarget says ops is recorded.
	withTarget bool
	// cookie is the Sign-in cookie of a caller through a Sign-in.
	cookie string
	// signIns are the ids of the Sign-ins of alice and frank, and of the
	// caller's User through a Sign-in, by User.
	signIns map[string]string
}

// A template is the data directory of a stopped koko server holding a
// fixture's records, and its Root Key file. Each fixture starts its koko
// server on a copy, so the matrix records them, and hashes a Sign-in's
// Password, once per template rather than once per fixture: hundreds of
// times, which took the package to its time limit on CI (#186).
type template struct {
	dataDir, rootKey string
	// withTarget says ops is recorded.
	withTarget bool
	// cookie is the Sign-in cookie of the template's User signed in.
	cookie string
	// signIns are the ids of the template's Sign-ins, by User.
	signIns map[string]string
}

// templateKey names a template: whether ops is recorded, and the User
// signed in in a browser, or "" for none. A Sign-in sets the Web URL, which
// password.link's row needs unset for every other caller, so a template
// with a Sign-in serves only the callers through it.
type templateKey struct {
	withTarget bool
	signedIn   string
}

// keyOf is the key of the template c's fixture starts from, with ops
// recorded or not.
func keyOf(withTarget bool, c caller) templateKey {
	k := templateKey{withTarget: withTarget}
	if c.signedIn() {
		k.signedIn = c.user()
	}
	return k
}

// newTemplates makes every template the fixtures of rows start from.
func newTemplates(t *testing.T, rows []row) map[templateKey]template {
	t.Helper()
	templates := map[templateKey]template{}
	add := func(k templateKey) {
		if _, ok := templates[k]; !ok {
			templates[k] = newTemplate(t, k)
		}
	}
	for _, r := range rows {
		for c, want := range r.want {
			add(keyOf(true, c))
			if want.unknown {
				add(keyOf(false, c))
			}
		}
	}
	return templates
}

func newTemplate(t *testing.T, k templateKey) template {
	t.Helper()
	tmpl := template{dataDir: t.TempDir(), rootKey: harness.NewRootKeyFile(t), withTarget: k.withTarget, signIns: map[string]string{}}
	h := harness.Start(t, harness.DataDir(tmpl.dataDir), harness.RootKeyFile(tmpl.rootKey), harness.WebListen("127.0.0.1:0"))
	for _, u := range []string{"bob", "carol", "dave", administrator, otherAdministrator} {
		h.Koko("user", "invite", u).WantSuccess(t)
	}
	for _, u := range []string{administrator, otherAdministrator} {
		h.Koko("user", "promote", u).WantSuccess(t)
	}
	if k.withTarget {
		h.Koko("space", "add", targetSpace).WantSuccess(t)
		h.Koko("space", "member", "add", targetSpace, "bob").WantSuccess(t)
	}
	// Setting a User's Password ends their other Sign-ins, so each User is
	// signed in once, the caller's among them.
	signIn := []string{harness.Owner, otherAdministrator}
	if k.signedIn != "" && !slices.Contains(signIn, k.signedIn) {
		signIn = append(signIn, k.signedIn)
	}
	for _, u := range signIn {
		b := h.SignIn(u)
		tmpl.signIns[u] = signInID(t, b)
		if u == k.signedIn {
			tmpl.cookie = b.Cookie
		}
	}
	if k.signedIn == "" {
		// password.link's rows need the Web URL unset for every caller
		// but those through a Sign-in, whose browser sets it.
		if _, err := h.Call(api.ServerSet, api.ServerSetRequest{Unset: []string{"web_url"}}); err != nil {
			t.Fatalf("clear the Web URL: %v", err)
		}
	}
	h.Stop()
	return tmpl
}

// signInID is the id of the Sign-in b holds.
func signInID(t *testing.T, b *harness.Browser) string {
	t.Helper()
	status, body := b.Send(api.UserCurrent, []byte(`{}`))
	var resp api.UserCurrentResponse
	if err := json.Unmarshal(body, &resp); status != http.StatusOK || err != nil || resp.SignIn == nil {
		t.Fatalf("user.current from a browser signed in: %d %s", status, body)
	}
	return resp.SignIn.ID
}

// newFixture starts c's fixture on a copy of tmpl.
func newFixture(t *testing.T, tmpl template, c caller) *fixture {
	t.Helper()
	dataDir := t.TempDir()
	if err := os.CopyFS(dataDir, os.DirFS(tmpl.dataDir)); err != nil {
		t.Fatalf("copy the template's data directory: %v", err)
	}
	opts := []harness.Option{harness.NoInit(), harness.DataDir(dataDir), harness.RootKeyFile(tmpl.rootKey)}
	if c.signedIn() {
		opts = append(opts, harness.WebListen("127.0.0.1:0"))
	}
	return &fixture{t: t, h: harness.Start(t, opts...), withTarget: tmpl.withTarget, cookie: tmpl.cookie, signIns: tmpl.signIns}
}

// send makes r's call as c, joining a device of c's User for it, or
// calling from a browser holding its Sign-in, after r's setup where c's
// target Space is recorded.
func (f *fixture) send(c caller, r row) (int, []byte) {
	f.t.Helper()
	if r.setup != nil && (f.withTarget || c.target() != targetSpace) {
		r.setup(f, c.target())
	}
	var req any
	if r.requestIn != nil {
		req = r.requestIn(f, c.target())
	} else {
		req = r.request(c.target())
	}
	body, err := json.Marshal(req)
	if err != nil {
		f.t.Fatalf("encode the request of %s: %v", r, err)
	}
	switch {
	case c == local:
		return f.h.Send(r.op, body)
	case c.signedIn():
		b := f.h.Browser()
		b.Cookie = f.cookie
		return b.Send(r.op, body)
	}
	return f.h.Join(c.user()).Send(r.op, body)
}
