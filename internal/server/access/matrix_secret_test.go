package access_test

import "github.com/cieons/koko/internal/shared/api"

// inSpaceAnswers is the answers to an operation on a record in the target
// Space: whoever can act in the Space may make it, and a Member outside it
// gets the answer for a Space that does not exist.
var inSpaceAnswers = map[caller]answer{
	ownerDevice: allowed, local: allowed, member: allowed, personal: allowed, outsider: unknown,
}

// addSecret stores the Secret db-password in the target Space.
func addSecret(f *fixture, target string) {
	f.t.Helper()
	req := api.SecretAddRequest{Space: target, Name: "db-password", Value: api.SecretValue("s3cret")}
	if _, err := f.h.Call(api.SecretAdd, req); err != nil {
		f.t.Fatalf("add %s/db-password: %v", target, err)
	}
}

// addData records the shared Space data, with the Members bob and carol:
// a second Space that each caller but the outsider can act in besides the
// target.
func addData(f *fixture) {
	f.t.Helper()
	f.h.Koko("space", "add", "data").WantSuccess(f.t)
	f.h.Koko("space", "member", "add", "data", "bob", "carol").WantSuccess(f.t)
}

var secretRows = []row{
	{
		op:      api.SecretList,
		name:    "of every Space",
		request: func(string) any { return api.SecretListRequest{} },
		want:    anyoneAnswers,
		setup:   addSecret,
	},
	{
		op:      api.SecretList,
		name:    "of the Space",
		request: func(target string) any { return api.SecretListRequest{Space: target} },
		want:    inSpaceAnswers,
		setup:   addSecret,
	},
	{
		op:      api.SecretShow,
		request: func(target string) any { return api.SecretShowRequest{Space: target, Name: "db-password"} },
		want:    inSpaceAnswers,
		setup:   addSecret,
	},
	{
		op:      api.SecretShow,
		name:    "of a name the Space does not hold",
		request: func(target string) any { return api.SecretShowRequest{Space: target, Name: "db-password"} },
		want: map[caller]answer{
			ownerDevice: allowedBut(api.CodeUnknownSecret), local: allowedBut(api.CodeUnknownSecret),
			member: allowedBut(api.CodeUnknownSecret), personal: allowedBut(api.CodeUnknownSecret), outsider: unknown,
		},
	},
	{
		op: api.SecretAdd,
		request: func(target string) any {
			return api.SecretAddRequest{Space: target, Name: "db-password", Value: api.SecretValue("s3cret")}
		},
		want: inSpaceAnswers,
	},
	{
		op:   api.SecretAdd,
		name: "under a name the Space holds",
		request: func(target string) any {
			return api.SecretAddRequest{Space: target, Name: "db-password", Value: api.SecretValue("s3cret")}
		},
		want: map[caller]answer{
			ownerDevice: allowedBut(api.CodeNameTaken), local: allowedBut(api.CodeNameTaken),
			member: allowedBut(api.CodeNameTaken), personal: allowedBut(api.CodeNameTaken), outsider: unknown,
		},
		setup: addSecret,
	},
	{
		op: api.SecretSet,
		request: func(target string) any {
			return api.SecretSetRequest{Space: target, Name: "db-password", Value: api.SecretValue("n3w")}
		},
		want:  inSpaceAnswers,
		setup: addSecret,
	},
	{
		op:   api.SecretSet,
		name: "of a name the Space does not hold",
		request: func(target string) any {
			return api.SecretSetRequest{Space: target, Name: "db-password", Value: api.SecretValue("n3w")}
		},
		want: map[caller]answer{
			ownerDevice: allowedBut(api.CodeUnknownSecret), local: allowedBut(api.CodeUnknownSecret),
			member: allowedBut(api.CodeUnknownSecret), personal: allowedBut(api.CodeUnknownSecret), outsider: unknown,
		},
	},
	{
		op: api.SecretRename,
		request: func(target string) any {
			return api.SecretRenameRequest{Space: target, Name: "db-password", NewName: "db-pass"}
		},
		want:  inSpaceAnswers,
		setup: addSecret,
	},
	{
		op: api.SecretMove,
		// A move needs both Spaces: here the caller can act in both, but
		// for the outsider, who cannot act in the target.
		name: "to another Space the caller can act in",
		request: func(target string) any {
			return api.SecretMoveRequest{Space: target, Name: "db-password", NewSpace: "data"}
		},
		want: inSpaceAnswers,
		setup: func(f *fixture, target string) {
			addSecret(f, target)
			addData(f)
		},
	},
	{
		op: api.SecretMove,
		// The source only: dave's Personal Space is the Owner's alone to
		// act in, besides dave's.
		name: "to a Space only the Owner can act in",
		request: func(target string) any {
			return api.SecretMoveRequest{Space: target, Name: "db-password", NewSpace: "dave"}
		},
		want: map[caller]answer{
			ownerDevice: allowed, local: allowed, member: unseenSpace, personal: unseenSpace, outsider: unknown,
		},
		setup: addSecret,
	},
	{
		op: api.SecretMove,
		// The target only: the Secret is in dave's Personal Space.
		name: "from a Space only the Owner can act in",
		request: func(target string) any {
			return api.SecretMoveRequest{Space: "dave", Name: "db-password", NewSpace: target}
		},
		want: map[caller]answer{
			ownerDevice: allowed, local: allowed, member: unseenSpace, personal: unseenSpace, outsider: unseenSpace,
		},
		setup: func(f *fixture, _ string) { addSecret(f, "dave") },
	},
	{
		op:   api.SecretMove,
		name: "onto a name the other Space holds",
		request: func(target string) any {
			return api.SecretMoveRequest{Space: target, Name: "db-password", NewSpace: "data"}
		},
		want: map[caller]answer{
			ownerDevice: allowedBut(api.CodeNameTaken), local: allowedBut(api.CodeNameTaken),
			member: allowedBut(api.CodeNameTaken), personal: allowedBut(api.CodeNameTaken), outsider: unknown,
		},
		setup: func(f *fixture, target string) {
			addSecret(f, target)
			addData(f)
			addSecret(f, "data")
		},
	},
	{
		op:      api.SecretRemove,
		request: func(target string) any { return api.SecretRemoveRequest{Space: target, Name: "db-password"} },
		want:    inSpaceAnswers,
		setup:   addSecret,
	},
}
