package access_test

import "github.com/cieons/koko/internal/shared/api"

var serverRows = []row{
	{
		op:      api.ServerInit,
		request: func(string) any { return api.ServerInitRequest{Owner: "erin", Address: "koko.test"} },
		// The fixture is initialised, so the Local caller is refused by
		// initialisation's own rule.
		want: map[caller]answer{
			ownerDevice: localOnly, local: allowedBut(api.CodeAlreadyInitialised),
			member: localOnly, personal: localOnly, outsider: localOnly,
			ownerSignIn: localOnly, memberSignIn: localOnly,
		},
	},
	{
		op:      api.ServerShow,
		request: func(string) any { return api.ServerShowRequest{} },
		want:    anyoneAnswers,
	},
	{
		op:      api.ServerSet,
		request: func(string) any { return api.ServerSetRequest{Address: new("koko.test")} },
		// No device, the Owner's own included, moves where devices connect.
		want: map[caller]answer{
			ownerDevice: localOnly, local: allowed, member: localOnly, personal: localOnly, outsider: localOnly,
			ownerSignIn: localOnly, memberSignIn: localOnly,
		},
	},
}
