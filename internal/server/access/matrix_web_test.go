package access_test

import "github.com/cieons/koko/internal/shared/api"

var webRows = []row{
	{
		op:      api.WebStart,
		request: func(string) any { return api.WebStartRequest{} },
		// A Sign-in runs no koko web.
		want: map[caller]answer{
			ownerDevice: allowed, local: allowed, member: allowed, personal: allowed, outsider: allowed,
			ownerSignIn: allowedBut(api.CodeDeviceOnly), memberSignIn: allowedBut(api.CodeDeviceOnly),
			adminDevice: allowed, adminSignIn: allowedBut(api.CodeDeviceOnly),
		},
	},
}
