package access_test

// The access matrix (S2): every API operation called as each kind of
// caller, against the answer the access check must give. Each area keeps its
// rows in a file of its own, matrix_<area>_test.go, and adds one line to
// areaRows; an operation in api.Operations without a row fails the test.

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
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
)

var callers = []caller{ownerDevice, local, member, personal, outsider}

func (c caller) String() string {
	return [...]string{
		"the Owner's device", "the Local caller", "a Member of the Space",
		"the User whose Personal Space it is", "a Member outside the Space",
	}[c]
}

// user is the User the caller acts as; the Local caller acts as the Owner.
func (c caller) user() string {
	return [...]string{harness.Owner, harness.Owner, "bob", "carol", "carol"}[c]
}

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
	// ownerOnly: only the Owner may make the call.
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

// ownerOnlyAnswers is the answers to an operation only the Owner may make:
// a Member is refused whatever the call names.
var ownerOnlyAnswers = map[caller]answer{
	ownerDevice: allowed, local: allowed, member: ownerOnly, personal: ownerOnly, outsider: ownerOnly,
}

// anyoneAnswers is the answers to an operation every caller may make.
var anyoneAnswers = map[caller]answer{
	ownerDevice: allowed, local: allowed, member: allowed, personal: allowed, outsider: allowed,
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
	for _, r := range slices.Concat(areaRows...) {
		for _, c := range callers {
			want, ok := r.want[c]
			if !ok {
				continue
			}
			t.Run(fmt.Sprintf("%s as %s", r, c), func(t *testing.T) {
				t.Parallel()
				status, body := newFixture(t, true).send(c, r)
				switch {
				case want.unknown:
					twinStatus, twinBody := newFixture(t, false).send(c, r)
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
// Members bob, carol and dave, who has not joined; and, unless the twin of a
// call answered as unknown, the shared Space ops with the Member bob.
type fixture struct {
	t *testing.T
	h *harness.Harness
	// withTarget says ops is recorded.
	withTarget bool
}

func newFixture(t *testing.T, withTarget bool) *fixture {
	t.Helper()
	h := harness.Start(t)
	for _, u := range []string{"bob", "carol", "dave"} {
		h.Koko("user", "invite", u).WantSuccess(t)
	}
	if withTarget {
		h.Koko("space", "add", targetSpace).WantSuccess(t)
		h.Koko("space", "member", "add", targetSpace, "bob").WantSuccess(t)
	}
	return &fixture{t: t, h: h, withTarget: withTarget}
}

// send makes r's call as c, joining a device of c's User for it, after r's
// setup where c's target Space is recorded.
func (f *fixture) send(c caller, r row) (int, []byte) {
	f.t.Helper()
	if r.setup != nil && (f.withTarget || c.target() != targetSpace) {
		r.setup(f, c.target())
	}
	body, err := json.Marshal(r.request(c.target()))
	if err != nil {
		f.t.Fatalf("encode the request of %s: %v", r, err)
	}
	if c == local {
		return f.h.Send(r.op, body)
	}
	return f.h.Join(c.user()).Send(r.op, body)
}
