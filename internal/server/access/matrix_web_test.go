package access_test

import "github.com/cieons/koko/internal/shared/api"

var webRows = []row{
	{
		op:      api.WebStart,
		request: func(string) any { return api.WebStartRequest{} },
		want:    anyoneAnswers,
	},
}
