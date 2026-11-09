package access_test

import "github.com/cieons/koko/internal/shared/api"

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
		},
	},
}
