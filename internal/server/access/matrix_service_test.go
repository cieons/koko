package access_test

import (
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/record"
)

// addNginx records the Host web1 in the target Space and the Service
// web1/nginx on it, as the Local caller.
func addNginx(f *fixture, target string) {
	addWeb1(f, target)
	f.h.Koko("service", "add", "web1/nginx", "ports=443").WantSuccess(f.t)
}

// serviceRows: a Service lives in its Host's Space, so whoever can act on
// web1 acts on its Services, and a Member outside the Space is answered as
// for a Host that does not exist (hostAnswers).
var serviceRows = []row{
	{
		op:    api.ServiceAdd,
		setup: addWeb1,
		request: func(string) any {
			return api.ServiceAddRequest{Host: "web1", Name: "nginx", Ports: []string{"443/tcp"}}
		},
		want: hostAnswers,
	},
	{
		op: api.ServiceAdd,
		// Only a caller who can see web1 learns that its Service is named
		// nginx.
		name:    "under a name a Service on the Host holds",
		setup:   addNginx,
		request: func(string) any { return api.ServiceAddRequest{Host: "web1", Name: "nginx"} },
		want: map[caller]answer{
			ownerDevice: allowedBut(api.CodeNameTaken), local: allowedBut(api.CodeNameTaken), member: allowedBut(api.CodeNameTaken),
			personal: allowedBut(api.CodeNameTaken), outsider: unknown,
			ownerSignIn: allowedBut(api.CodeNameTaken), memberSignIn: allowedBut(api.CodeNameTaken),
		},
	},
	{
		op:      api.ServiceList,
		setup:   addNginx,
		request: func(string) any { return api.ServiceListRequest{WithNote: true} },
		want:    anyoneAnswers,
	},
	{
		op:      api.ServiceShow,
		setup:   addNginx,
		request: func(string) any { return api.ServiceShowRequest{Host: "web1", Name: "nginx"} },
		want:    hostAnswers,
	},
	{
		op:    api.ServiceSet,
		setup: addNginx,
		request: func(string) any {
			return api.ServiceSetRequest{Host: "web1", Name: "nginx", Attributes: map[string]record.Value{"kind": record.StringValue("proxy")}}
		},
		want: hostAnswers,
	},
	{
		op:      api.ServiceRename,
		setup:   addNginx,
		request: func(string) any { return api.ServiceRenameRequest{Host: "web1", Name: "nginx", NewName: "web"} },
		want:    hostAnswers,
	},
	{
		op:      api.ServiceRemove,
		setup:   addNginx,
		request: func(string) any { return api.ServiceRemoveRequest{Host: "web1", Name: "nginx"} },
		want:    hostAnswers,
	},
}
