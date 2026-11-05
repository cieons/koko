package access_test

import "github.com/cieons/koko/internal/shared/api"

var deviceRows = []row{
	{
		op:      api.DeviceAdd,
		name:    "for the caller's own User",
		request: func(string) any { return api.DeviceAddRequest{} },
		want:    anyoneAnswers,
	},
	{
		op:      api.DeviceAdd,
		name:    "for another User",
		request: func(string) any { return api.DeviceAddRequest{User: "dave"} },
		want:    ownerOnlyAnswers,
	},
}
