package access_test

import (
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

var identityRows = []row{
	{
		op:      api.ServerOIDCShow,
		request: func(string) any { return api.ServerOIDCShowRequest{} },
		want:    administersAnswers,
	},
	{
		op: api.ServerOIDCSet,
		request: func(string) any {
			return api.ServerOIDCSetRequest{
				Issuer: new("https://idp.example.com/"), ClientID: new("koko"), ClientSecret: new("secret"), Name: new("Authentik"),
			}
		},
		// The Owner alone is let through: then refused for the Web URL,
		// which only a Sign-in's fixture sets, and through a Sign-in for
		// the provider, which no fixture's koko server reaches.
		want: map[caller]answer{
			ownerDevice: allowedBut(api.CodeWebURLUnset), local: allowedBut(api.CodeWebURLUnset),
			member: ownerOnly, personal: ownerOnly, outsider: ownerOnly,
			ownerSignIn: allowedBut(api.CodeProviderUnreachable), memberSignIn: ownerOnly,
			adminDevice: ownerOnly, adminSignIn: ownerOnly,
		},
	},
	{
		op:      api.ServerOIDCRemove,
		request: func(string) any { return api.ServerOIDCRemoveRequest{} },
		want:    ownerOnlyAnswers,
	},
	{
		op:      api.IdentityList,
		request: func(string) any { return api.IdentityListRequest{} },
		want:    anyoneAnswers,
	},
	{
		// A Member naming another User gets none: answered, not refused.
		op:      api.IdentityList,
		name:    "of another User",
		request: func(string) any { return api.IdentityListRequest{User: "dave"} },
		want:    anyoneAnswers,
	},
	{
		// Reading is not acting: an Administrator lists the Owner's.
		op:      api.IdentityList,
		name:    "of the Owner",
		request: func(string) any { return api.IdentityListRequest{User: harness.Owner} },
		want:    anyoneAnswers,
	},
	{
		// One's own: the Owner and the other Administrator have one, and
		// bob, carol and eve none.
		op:      api.IdentityUnlink,
		request: func(string) any { return api.IdentityUnlinkRequest{} },
		want: map[caller]answer{
			ownerDevice: allowed, local: allowed,
			member: allowedBut(api.CodeNoIdentity), personal: allowedBut(api.CodeNoIdentity),
			outsider:    allowedBut(api.CodeNoIdentity),
			ownerSignIn: allowed, memberSignIn: allowedBut(api.CodeNoIdentity),
			adminDevice: allowedBut(api.CodeNoIdentity), adminSignIn: allowedBut(api.CodeNoIdentity),
		},
	},
	{
		// A name no User holds: a Member is refused before it is read.
		op:      api.IdentityUnlink,
		name:    "of no User",
		request: func(string) any { return api.IdentityUnlinkRequest{User: "zed"} },
		want: map[caller]answer{
			ownerDevice: allowedBut(api.CodeUnknownUser), local: allowedBut(api.CodeUnknownUser),
			member: ownerOnly, personal: ownerOnly, outsider: ownerOnly,
			ownerSignIn: allowedBut(api.CodeUnknownUser), memberSignIn: ownerOnly,
			adminDevice: allowedBut(api.CodeUnknownUser), adminSignIn: allowedBut(api.CodeUnknownUser),
		},
	},
	{
		op:      api.IdentityUnlink,
		name:    "a Member's",
		request: func(string) any { return api.IdentityUnlinkRequest{User: "dave"} },
		want:    administersAnswers,
	},
	{
		op:      api.IdentityUnlink,
		name:    "the Owner's",
		request: func(string) any { return api.IdentityUnlinkRequest{User: harness.Owner} },
		want:    ownerOnlyAnswers,
	},
	{
		op:      api.IdentityUnlink,
		name:    "another Administrator's",
		request: func(string) any { return api.IdentityUnlinkRequest{User: otherAdministrator} },
		want:    ownerOnlyAnswers,
	},
}
