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
	{
		op:      api.UserList,
		request: func(string) any { return api.UserListRequest{All: true} },
		want:    ownerOnlyAnswers,
	},
	{
		// dave never joined, so the Owner deletes him.
		op:      api.UserRemove,
		request: func(string) any { return api.UserRemoveRequest{Name: "dave"} },
		want:    ownerOnlyAnswers,
	},
}
