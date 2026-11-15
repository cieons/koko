package access_test

import (
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

// passkeyRows act on the Passkeys of the Owner, of the other Administrator
// frank and of the Member dave, which every template holds.
var passkeyRows = []row{
	{
		op:      api.PasskeyList,
		request: func(string) any { return api.PasskeyListRequest{} },
		want:    anyoneAnswers,
	},
	{
		// A Member naming another User gets none: answered, not refused.
		op:      api.PasskeyList,
		name:    "of another User",
		request: func(string) any { return api.PasskeyListRequest{User: "dave"} },
		want:    anyoneAnswers,
	},
	{
		// Reading is not acting: an Administrator lists the Owner's.
		op:      api.PasskeyList,
		name:    "of the Owner",
		request: func(string) any { return api.PasskeyListRequest{User: harness.Owner} },
		want:    anyoneAnswers,
	},
	{
		// An id no Passkey holds: as another User's, to a Member.
		op:      api.PasskeyRemove,
		request: func(string) any { return api.PasskeyRemoveRequest{ID: "passkey_aaaaaaaa"} },
		want: map[caller]answer{
			ownerDevice: allowedBut(api.CodeUnknownPasskey), local: allowedBut(api.CodeUnknownPasskey),
			member: allowedBut(api.CodeUnknownPasskey), personal: allowedBut(api.CodeUnknownPasskey),
			outsider:    allowedBut(api.CodeUnknownPasskey),
			ownerSignIn: allowedBut(api.CodeUnknownPasskey), memberSignIn: allowedBut(api.CodeUnknownPasskey),
			adminDevice: allowedBut(api.CodeUnknownPasskey), adminSignIn: allowedBut(api.CodeUnknownPasskey),
		},
	},
	{
		op:   api.PasskeyRemove,
		name: "a Member's",
		requestIn: func(f *fixture, _ string) any {
			return api.PasskeyRemoveRequest{ID: f.passkeys["dave"]}
		},
		want: map[caller]answer{
			ownerDevice: allowed, local: allowed,
			member: allowedBut(api.CodeUnknownPasskey), personal: allowedBut(api.CodeUnknownPasskey),
			outsider:    allowedBut(api.CodeUnknownPasskey),
			ownerSignIn: allowed, memberSignIn: allowedBut(api.CodeUnknownPasskey),
			adminDevice: allowed, adminSignIn: allowed,
		},
	},
	{
		op:   api.PasskeyRemove,
		name: "the Owner's",
		requestIn: func(f *fixture, _ string) any {
			return api.PasskeyRemoveRequest{ID: f.passkeys[harness.Owner]}
		},
		want: removeOthersPasskeyAnswers,
	},
	{
		op:   api.PasskeyRemove,
		name: "another Administrator's",
		requestIn: func(f *fixture, _ string) any {
			return api.PasskeyRemoveRequest{ID: f.passkeys[otherAdministrator]}
		},
		want: removeOthersPasskeyAnswers,
	},
}

// removeOthersPasskeyAnswers is the answers to passkey.remove of a Passkey
// of the Owner or another Administrator: only the Owner's callers remove
// it, an Administrator is refused owner_only, and a Member gets unknown, as
// for any other User's.
var removeOthersPasskeyAnswers = map[caller]answer{
	ownerDevice: allowed, local: allowed,
	member: allowedBut(api.CodeUnknownPasskey), personal: allowedBut(api.CodeUnknownPasskey),
	outsider:    allowedBut(api.CodeUnknownPasskey),
	ownerSignIn: allowed, memberSignIn: allowedBut(api.CodeUnknownPasskey),
	adminDevice: ownerOnly, adminSignIn: ownerOnly,
}
