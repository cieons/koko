package access_test

import "github.com/cieons/koko/internal/shared/api"

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
		// The Owner is let through, and refused, since no provider is set.
		want: map[caller]answer{
			ownerDevice: allowedBut(api.CodeProviderUnset), local: allowedBut(api.CodeProviderUnset),
			member: ownerOnly, personal: ownerOnly, outsider: ownerOnly,
			ownerSignIn: allowedBut(api.CodeProviderUnset), memberSignIn: ownerOnly,
			adminDevice: ownerOnly, adminSignIn: ownerOnly,
		},
	},
}
