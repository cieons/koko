package api

// JoinCode is a Join Code just issued, as the API returns it once: the code
// itself is never stored and never shown again.
type JoinCode struct {
	ID string `json:"id"`
	// Code is the koko_ string to paste into `koko join` on the new device.
	Code string `json:"code"`
	// User is the name of the User the new device joins as.
	User string `json:"user"`
	// Device is the new device's name, bound to the code.
	Device string `json:"device"`
	// ExpiresAt is when the code stops working, 15 minutes after its issue,
	// as RFC 3339 in UTC to the second.
	ExpiresAt string `json:"expires_at"`
}

// DeviceSession is a Device Session as the API returns it: the key one
// device registered when it joined, under a name that never changes.
type DeviceSession struct {
	ID string `json:"id"`
	// User is the name of the User the device acts as.
	User string `json:"user"`
	// Device is the device's name, which its Join Code bound.
	Device string `json:"device"`
	// CreatedAt is when the device joined, as RFC 3339 in UTC to the second.
	CreatedAt string `json:"created_at"`
}

// DeviceAddRequest issues a Join Code for one new device.
type DeviceAddRequest struct {
	// Device names the new device. Empty means device-<n>, the first such
	// name past those the User's devices and valid Join Codes hold.
	Device string `json:"device,omitempty"`
	// User is the User the code is for. Empty means the caller's own User;
	// for the Local caller, the Owner.
	User string `json:"user,omitempty"`
}

// DeviceAddResponse is the Join Code device.add issued.
type DeviceAddResponse struct {
	JoinCode JoinCode `json:"join_code"`
}

// The device operations.
var (
	// DeviceAdd issues a Join Code, valid for 15 minutes, for one new device
	// of the caller's own User or, for the Owner, of any User; a Member
	// naming another User is refused with CodeOwnerOnly. A device name
	// another live device or valid Join Code of that User holds is refused
	// with CodeNameTaken.
	DeviceAdd = Op[DeviceAddRequest, DeviceAddResponse]{Name: "device.add", Write: true}
)

var deviceOperations = []Operation{
	DeviceAdd,
}
