package access_test

import (
	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/shared/api"
)

var userRows = []row{
	{
		op:      api.UserInvite,
		request: func(string) any { return api.UserInviteRequest{Name: "erin"} },
		want:    administersAnswers,
	},
	{
		op:      api.UserCurrent,
		request: func(string) any { return api.UserCurrentRequest{} },
		want:    anyoneAnswers,
	},
	{
		op:      api.UserList,
		request: func(string) any { return api.UserListRequest{All: true} },
		want:    administersAnswers,
	},
	{
		// dave never joined, so the Owner deletes him.
		op:      api.UserRemove,
		request: func(string) any { return api.UserRemoveRequest{Name: "dave"} },
		want:    administersAnswers,
	},
	{
		op:      api.UserRemove,
		name:    "the Owner",
		request: func(string) any { return api.UserRemoveRequest{Name: harness.Owner} },
		// The Owner is never removed, whoever asks.
		want: map[caller]answer{
			ownerDevice: allowedBut(api.CodeOwnerNotRemovable), local: allowedBut(api.CodeOwnerNotRemovable),
			member: ownerOnly, personal: ownerOnly, outsider: ownerOnly,
			ownerSignIn: allowedBut(api.CodeOwnerNotRemovable), memberSignIn: ownerOnly,
			adminDevice: allowedBut(api.CodeOwnerNotRemovable), adminSignIn: allowedBut(api.CodeOwnerNotRemovable),
		},
	},
	{
		op:      api.UserRemove,
		name:    "another Administrator",
		request: func(string) any { return api.UserRemoveRequest{Name: otherAdministrator} },
		want:    ownerOnlyAnswers,
	},
	{
		// The Administrators' callers name themselves: only the Owner
		// removes an Administrator, and none steps down by themselves.
		op:      api.UserRemove,
		name:    "the Administrator who calls",
		request: func(string) any { return api.UserRemoveRequest{Name: administrator} },
		want:    ownerOnlyAnswers,
	},
	{
		op:      api.UserPromote,
		request: func(string) any { return api.UserPromoteRequest{Name: "dave"} },
		want:    ownerOnlyAnswers,
	},
	{
		op:      api.UserPromote,
		name:    "the Owner",
		request: func(string) any { return api.UserPromoteRequest{Name: harness.Owner} },
		want:    roleOfTheOwnerAnswers,
	},
	{
		op:      api.UserDemote,
		request: func(string) any { return api.UserDemoteRequest{Name: otherAdministrator} },
		want:    ownerOnlyAnswers,
	},
	{
		op:      api.UserDemote,
		name:    "the Owner",
		request: func(string) any { return api.UserDemoteRequest{Name: harness.Owner} },
		want:    roleOfTheOwnerAnswers,
	},
}

// roleOfTheOwnerAnswers is the answers to a call that would change the
// Owner's role: only the Owner's callers are let through, to be refused.
var roleOfTheOwnerAnswers = map[caller]answer{
	ownerDevice: allowedBut(api.CodeOwnerNotRemovable), local: allowedBut(api.CodeOwnerNotRemovable),
	member: ownerOnly, personal: ownerOnly, outsider: ownerOnly,
	ownerSignIn: allowedBut(api.CodeOwnerNotRemovable), memberSignIn: ownerOnly,
	adminDevice: ownerOnly, adminSignIn: ownerOnly,
}
