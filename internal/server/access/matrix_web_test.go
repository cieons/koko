package access_test

import "github.com/cieons/koko/internal/shared/api"

var webRows = []row{
	{
		op:      api.WebStart,
		request: func(string) any { return api.WebStartRequest{} },
		want: map[caller]answer{
			ownerDevice: localOnly, local: allowed, member: localOnly, personal: localOnly, outsider: localOnly,
		},
	},
}
