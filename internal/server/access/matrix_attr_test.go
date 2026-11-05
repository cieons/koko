package access_test

import "github.com/cieons/koko/internal/shared/api"

// attrRows: anyone may list the Attribute keys, each seeing the keys in use
// in the Spaces they can act in (TestAttrLsListsTheKeysInUseOnWhatTheCallerCanSee).
var attrRows = []row{
	{
		op:      api.AttrList,
		setup:   addWeb1,
		request: func(string) any { return api.AttrListRequest{} },
		want:    anyoneAnswers,
	},
}
