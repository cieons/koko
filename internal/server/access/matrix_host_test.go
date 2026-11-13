package access_test

import (
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/record"
)

// addWeb1 records the Host web1 in the target Space, as the Local caller.
func addWeb1(f *fixture, target string) {
	f.h.Koko("host", "add", "web1", "space="+target, "address=203.0.113.10", "account=deploy").WantSuccess(f.t)
}

// hostAnswers is the answers to a call on a Host in the target Space: the
// Owner, the Administrators and anyone who can act in the Space may make
// it, and a Member outside it is answered as for a Host that does not
// exist.
var hostAnswers = map[caller]answer{
	ownerDevice: allowed, local: allowed, member: allowed, personal: allowed, outsider: unknown,
	ownerSignIn: allowed, memberSignIn: allowed,
	adminDevice: allowed, adminSignIn: allowed,
}

// unseenSpace is the answer to a call that names a second Space, one the
// caller cannot act in, which answers as a Space that does not exist.
var unseenSpace = answer{code: api.CodeUnknownSpace.Name}

var hostRows = []row{
	{
		op: api.HostAdd,
		request: func(target string) any {
			return api.HostAddRequest{Hosts: []api.NewHost{{Name: "web2", Space: &target, Address: "203.0.113.10", Account: "deploy"}}}
		},
		want: hostAnswers,
	},
	{
		op: api.HostAdd,
		// The one leak (ADR 0014): every caller learns that the name is
		// taken, whether they can see the Host that holds it or not.
		name:  "under a name a Host in the target Space holds",
		setup: addWeb1,
		request: func(string) any {
			return api.HostAddRequest{Hosts: []api.NewHost{{Name: "web1", Address: "203.0.113.10", Account: "deploy"}}}
		},
		want: map[caller]answer{
			ownerDevice: allowedBut(api.CodeNameTaken), local: allowedBut(api.CodeNameTaken), member: allowedBut(api.CodeNameTaken),
			personal: allowedBut(api.CodeNameTaken), outsider: allowedBut(api.CodeNameTaken),
			ownerSignIn: allowedBut(api.CodeNameTaken), memberSignIn: allowedBut(api.CodeNameTaken),
			adminDevice: allowedBut(api.CodeNameTaken), adminSignIn: allowedBut(api.CodeNameTaken),
		},
	},
	{
		op:      api.HostList,
		request: func(string) any { return api.HostListRequest{WithNote: true, All: true} },
		want:    anyoneAnswers,
	},
	{
		op:      api.HostShow,
		setup:   addWeb1,
		request: func(string) any { return api.HostShowRequest{Host: "web1"} },
		want:    hostAnswers,
	},
	{
		op:    api.HostSet,
		setup: addWeb1,
		request: func(string) any {
			return api.HostSetRequest{Host: "web1", Attributes: map[string]record.Value{"region": record.StringValue("fsn1")}}
		},
		want: hostAnswers,
	},
	{
		op:      api.HostRename,
		setup:   addWeb1,
		request: func(string) any { return api.HostRenameRequest{Host: "web1", NewName: "web9"} },
		want:    hostAnswers,
	},
	{
		op:      api.HostMove,
		name:    "within the target Space",
		setup:   addWeb1,
		request: func(target string) any { return api.HostMoveRequest{Host: "web1", Space: target} },
		want:    hostAnswers,
	},
	{
		op: api.HostMove,
		// A move needs both Spaces: dave's Personal Space is the Owner's
		// alone to act in, besides dave's.
		name:    "to a Space only the Owner can act in",
		setup:   addWeb1,
		request: func(string) any { return api.HostMoveRequest{Host: "web1", Space: "dave"} },
		want: map[caller]answer{
			ownerDevice: allowed, local: allowed, member: unseenSpace, personal: unseenSpace, outsider: unknown,
			ownerSignIn: allowed, memberSignIn: unseenSpace,
			adminDevice: allowed, adminSignIn: allowed,
		},
	},
	{
		op:      api.HostRetire,
		setup:   addWeb1,
		request: func(string) any { return api.HostRetireRequest{Host: "web1"} },
		want:    hostAnswers,
	},
	{
		op:      api.HostRemove,
		setup:   addWeb1,
		request: func(string) any { return api.HostRemoveRequest{Host: "web1"} },
		want:    hostAnswers,
	},
}
