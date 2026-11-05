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
}
