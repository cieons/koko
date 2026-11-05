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

// addRootPassword stores enrollPassword as the Secret root-password in the
// Space named spaceName, as the Local caller.
func addRootPassword(f *fixture, spaceName string) {
	f.t.Helper()
	req := api.SecretAddRequest{Space: spaceName, Name: "root-password", Value: api.SecretValue(enrollPassword)}
	if _, err := f.h.Call(api.SecretAdd, req); err != nil {
		f.t.Fatalf("add %s/root-password: %v", spaceName, err)
	}
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
	{
		op:   api.HostEnroll,
		name: "with a Secret of the Host's Space",
		setup: func(f *fixture, target string) {
			addEnrollable(f, target)
			addRootPassword(f, target)
		},
		request: func(target string) any {
			return api.HostEnrollRequest{Host: "web1", Secret: &api.SecretRef{Space: target, Name: "root-password"}}
		},
		want: hostAnswers,
	},
	{
		// The Secret is in dave's Personal Space, which only the Owner can
		// act in: being able to act on the Host does not grant it.
		op:   api.HostEnroll,
		name: "with a Secret of a Space the caller cannot act in",
		setup: func(f *fixture, target string) {
			addEnrollable(f, target)
			addRootPassword(f, "dave")
		},
		request: func(string) any {
			return api.HostEnrollRequest{Host: "web1", Secret: &api.SecretRef{Space: "dave", Name: "root-password"}}
		},
		want: map[caller]answer{
			ownerDevice: allowed, local: allowed, member: unseenSpace, personal: unseenSpace, outsider: unknown,
		},
	},
}
