package access_test

import "github.com/cieons/koko/internal/shared/api"

// Each caller's device is device-2: the fixture joins it for the call, with
// a code issued over the socket, while initialisation's code (the Owner's)
// or the Invite (a Member's) still holds device-1. The Local caller has no
// device of its own.
const callersDevice = "device-2"

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
	{
		op:      api.DeviceList,
		name:    "of the caller's own User",
		request: func(string) any { return api.DeviceListRequest{All: true} },
		want:    anyoneAnswers,
	},
	{
		op:      api.DeviceList,
		name:    "of another User",
		request: func(string) any { return api.DeviceListRequest{User: "dave"} },
		want:    ownerOnlyAnswers,
	},
	{
		// Anyone revokes their own device, the one in use included; the
		// Owner's last one needs last. The Local caller and a Sign-in come
		// on no device, so the caller's device is none of theirs.
		op:      api.DeviceRevoke,
		name:    "the caller's own device",
		request: func(string) any { return api.DeviceRevokeRequest{Device: callersDevice} },
		want: map[caller]answer{
			ownerDevice: allowedBut(api.CodeLastDeviceSession),
			local:       allowedBut(api.CodeUnknownDevice),
			member:      allowed, personal: allowed, outsider: allowed,
			ownerSignIn: allowedBut(api.CodeUnknownDevice), memberSignIn: allowedBut(api.CodeUnknownDevice),
		},
	},
	{
		op:      api.DeviceRevoke,
		name:    "the Owner's own last device, confirmed",
		request: func(string) any { return api.DeviceRevokeRequest{Device: callersDevice, Last: true} },
		want: map[caller]answer{
			ownerDevice: allowed,
			local:       allowedBut(api.CodeUnknownDevice),
			member:      allowed, personal: allowed, outsider: allowed,
			ownerSignIn: allowedBut(api.CodeUnknownDevice), memberSignIn: allowedBut(api.CodeUnknownDevice),
		},
	},
	{
		// dave never joined, so the Owner finds nothing to revoke either; a
		// Member naming another User's device gets that same answer, never
		// owner_only, whether the device is there or not.
		op:      api.DeviceRevoke,
		name:    "another User's device",
		request: func(string) any { return api.DeviceRevokeRequest{User: "dave", Device: "device-1"} },
		want: map[caller]answer{
			ownerDevice: allowedBut(api.CodeUnknownDevice), local: allowedBut(api.CodeUnknownDevice),
			member: allowedBut(api.CodeUnknownDevice), personal: allowedBut(api.CodeUnknownDevice),
			outsider:    allowedBut(api.CodeUnknownDevice),
			ownerSignIn: allowedBut(api.CodeUnknownDevice), memberSignIn: allowedBut(api.CodeUnknownDevice),
		},
	},
}
