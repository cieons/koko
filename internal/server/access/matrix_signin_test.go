package access_test

import (
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

// signinRows run on a fixture whose Web URL is not set, but for the
// callers through a Sign-in, whose browser sets it; so password.link is
// refused web_url_unset where the Owner may issue one without it.
var signinRows = []row{
	{
		op:      api.PasswordLinkIssue,
		request: func(string) any { return api.PasswordLinkRequest{User: "dave"} },
		want: map[caller]answer{
			ownerDevice: allowedBut(api.CodeWebURLUnset), local: allowedBut(api.CodeWebURLUnset),
			member: ownerOnly, personal: ownerOnly, outsider: ownerOnly,
			ownerSignIn: allowed, memberSignIn: ownerOnly,
			adminDevice: allowedBut(api.CodeWebURLUnset), adminSignIn: allowed,
		},
	},
	{
		// dave has no Password.
		op:      api.PasswordRemove,
		request: func(string) any { return api.PasswordRemoveRequest{User: "dave"} },
		want: map[caller]answer{
			ownerDevice: allowedBut(api.CodeNoPassword), local: allowedBut(api.CodeNoPassword),
			member: ownerOnly, personal: ownerOnly, outsider: ownerOnly,
			ownerSignIn: allowedBut(api.CodeNoPassword), memberSignIn: ownerOnly,
			adminDevice: allowedBut(api.CodeNoPassword), adminSignIn: allowedBut(api.CodeNoPassword),
		},
	},
	{
		op:      api.SignInList,
		request: func(string) any { return api.SignInListRequest{All: true} },
		want:    anyoneAnswers,
	},
	{
		// An id no Sign-in holds: as for another User's, to a Member.
		op:      api.SignInRevoke,
		request: func(string) any { return api.SignInRevokeRequest{ID: "signin_aaaaaaaa"} },
		want: map[caller]answer{
			ownerDevice: allowedBut(api.CodeUnknownSignIn), local: allowedBut(api.CodeUnknownSignIn),
			member: allowedBut(api.CodeUnknownSignIn), personal: allowedBut(api.CodeUnknownSignIn),
			outsider:    allowedBut(api.CodeUnknownSignIn),
			ownerSignIn: allowedBut(api.CodeUnknownSignIn), memberSignIn: allowedBut(api.CodeUnknownSignIn),
			adminDevice: allowedBut(api.CodeUnknownSignIn), adminSignIn: allowedBut(api.CodeUnknownSignIn),
		},
	},
	{
		// Reading is not acting: an Administrator lists the Owner's.
		op:      api.SignInList,
		name:    "of the Owner",
		request: func(string) any { return api.SignInListRequest{User: harness.Owner, All: true} },
		want:    anyoneAnswers,
	},
	{
		op:   api.SignInRevoke,
		name: "the Owner's",
		requestIn: func(f *fixture, _ string) any {
			return api.SignInRevokeRequest{ID: f.signIns[harness.Owner]}
		},
		want: revokeSignInAnswers,
	},
	{
		op:   api.SignInRevoke,
		name: "another Administrator's",
		requestIn: func(f *fixture, _ string) any {
			return api.SignInRevokeRequest{ID: f.signIns[otherAdministrator]}
		},
		want: revokeSignInAnswers,
	},
	{
		op:      api.PasswordLinkIssue,
		name:    "for the Owner",
		request: func(string) any { return api.PasswordLinkRequest{User: harness.Owner} },
		want:    linkOthersAnswers,
	},
	{
		op:      api.PasswordLinkIssue,
		name:    "for another Administrator",
		request: func(string) any { return api.PasswordLinkRequest{User: otherAdministrator} },
		want:    linkOthersAnswers,
	},
	{
		op:      api.PasswordRemove,
		name:    "of the Owner",
		request: func(string) any { return api.PasswordRemoveRequest{User: harness.Owner} },
		want:    ownerOnlyAnswers,
	},
	{
		op:      api.PasswordRemove,
		name:    "of another Administrator",
		request: func(string) any { return api.PasswordRemoveRequest{User: otherAdministrator} },
		want:    ownerOnlyAnswers,
	},
}

// revokeSignInAnswers is the answers to signin.revoke of a Sign-in of the
// Owner or another Administrator: only the Owner's callers end it, and a
// Member gets unknown, as for any other User's.
var revokeSignInAnswers = map[caller]answer{
	ownerDevice: allowed, local: allowed,
	member: allowedBut(api.CodeUnknownSignIn), personal: allowedBut(api.CodeUnknownSignIn),
	outsider:    allowedBut(api.CodeUnknownSignIn),
	ownerSignIn: allowed, memberSignIn: allowedBut(api.CodeUnknownSignIn),
	adminDevice: ownerOnly, adminSignIn: ownerOnly,
}

// linkOthersAnswers is the answers to password.link for the Owner or
// another Administrator: only the Owner's callers are let through, refused
// for want of a Web URL but through a Sign-in.
var linkOthersAnswers = map[caller]answer{
	ownerDevice: allowedBut(api.CodeWebURLUnset), local: allowedBut(api.CodeWebURLUnset),
	member: ownerOnly, personal: ownerOnly, outsider: ownerOnly,
	ownerSignIn: allowed, memberSignIn: ownerOnly,
	adminDevice: ownerOnly, adminSignIn: ownerOnly,
}
