package access_test

import "github.com/cieons/koko/internal/shared/api"

var spaceRows = []row{
	{
		op:      api.SpaceAdd,
		request: func(string) any { return api.SpaceAddRequest{Name: "infra"} },
		want:    administersAnswers,
	},
	{
		op:      api.SpaceList,
		request: func(string) any { return api.SpaceListRequest{} },
		want:    anyoneAnswers,
	},
	{
		op:      api.SpaceShow,
		request: func(target string) any { return api.SpaceShowRequest{Name: target} },
		want: map[caller]answer{
			ownerDevice: allowed, local: allowed, member: allowed, personal: allowed, outsider: unknown,
			ownerSignIn: allowed, memberSignIn: allowed,
			adminDevice: allowed, adminSignIn: allowed,
		},
	},
	{
		op:      api.SpaceRename,
		request: func(target string) any { return api.SpaceRenameRequest{Name: target, NewName: "infra"} },
		want:    administersAnswers,
	},
	{
		op:      api.SpaceRemove,
		request: func(target string) any { return api.SpaceRemoveRequest{Name: target} },
		want:    administersAnswers,
	},
	{
		op:      api.SpaceMemberAdd,
		request: func(target string) any { return api.SpaceMemberAddRequest{Space: target, Users: []string{"dave"}} },
		want:    administersAnswers,
	},
	{
		op:      api.SpaceMemberRemove,
		request: func(target string) any { return api.SpaceMemberRemoveRequest{Space: target, Users: []string{"bob"}} },
		want:    administersAnswers,
	},
}
