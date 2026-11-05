package access_test

import "github.com/cieons/koko/internal/shared/api"

var userRows = []row{
	{
		op:      api.UserInvite,
		request: func(string) any { return api.UserInviteRequest{Name: "erin"} },
		want:    ownerOnlyAnswers,
	},
	{
		op:      api.UserCurrent,
		request: func(string) any { return api.UserCurrentRequest{} },
		want:    anyoneAnswers,
	},
}
