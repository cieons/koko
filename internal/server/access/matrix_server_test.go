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
			adminDevice: localOnly, adminSignIn: localOnly,
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
			adminDevice: localOnly, adminSignIn: localOnly,
		},
	},
	{
		op:      api.ServerCheckUpdate,
		request: func(string) any { return api.ServerCheckUpdateRequest{} },
		// The Owner is let through, and the check fails, since the
		// fixture's koko server finds no release.
		want: map[caller]answer{
			ownerDevice: allowedBut(api.CodeUpdateCheckFailed), local: allowedBut(api.CodeUpdateCheckFailed),
			member: ownerOnly, personal: ownerOnly, outsider: ownerOnly,
			ownerSignIn: allowedBut(api.CodeUpdateCheckFailed), memberSignIn: ownerOnly,
			adminDevice: allowedBut(api.CodeUpdateCheckFailed), adminSignIn: allowedBut(api.CodeUpdateCheckFailed),
		},
	},
	{
		op:      api.ServerUpgrade,
		request: func(string) any { return api.ServerUpgradeRequest{Version: "v99.0.0"} },
		// The Owner is let through, and refused, since the fixture's koko
		// server runs without the upgrade helper.
		want: map[caller]answer{
			ownerDevice: allowedBut(api.CodeUpgradeUnsupported), local: allowedBut(api.CodeUpgradeUnsupported),
			member: ownerOnly, personal: ownerOnly, outsider: ownerOnly,
			ownerSignIn: allowedBut(api.CodeUpgradeUnsupported), memberSignIn: ownerOnly,
			adminDevice: allowedBut(api.CodeUpgradeUnsupported), adminSignIn: allowedBut(api.CodeUpgradeUnsupported),
		},
	},
}
