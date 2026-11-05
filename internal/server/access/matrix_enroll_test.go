package access_test

import (
	"strconv"

	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/shared/api"
)

// enrollPassword is the Host Account's password on the in-process Host that
// the rows of host.enroll Enroll.
const enrollPassword = "correct horse battery staple"

// addEnrollable records the Host web1 in the target Space, as the Local
// caller, at an in-process Host that takes enrollPassword.
func addEnrollable(f *fixture, target string) {
	host := sshhost.Start(f.t, sshhost.Password(enrollPassword))
	f.h.Koko("host", "add", "web1", "space="+target, "address="+host.Address(), "port="+strconv.Itoa(host.Port()),
		"account="+sshhost.Account).WantSuccess(f.t)
}

var enrollRows = []row{
	{
		op:    api.HostEnroll,
		setup: addEnrollable,
		request: func(string) any {
			return api.HostEnrollRequest{Host: "web1", Password: api.EnrollInput(enrollPassword)}
		},
		want: hostAnswers,
	},
}
