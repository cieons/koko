package api

import (
	"net/http"
	"time"
)

// How long a Join Code or a Password Link stays valid from its issue (ADR
// 0044): DefaultValidity, unless the request that issues it chooses from
// MinValidity to MaxValidity with valid_for.
const (
	DefaultValidity = 15 * time.Minute
	MinValidity     = time.Minute
	MaxValidity     = 7 * 24 * time.Hour
)

// Validity is how long the Join Code or Password Link a request issues
// stays valid, given the request's valid_for: that many seconds, or
// DefaultValidity when it is nil. Any number of seconds outside MinValidity
// to MaxValidity is refused with CodeInvalidValue.
func Validity(validFor *int) (time.Duration, error) {
	if validFor == nil {
		return DefaultValidity, nil
	}
	// The seconds are compared before they become a Duration, which a huge
	// number would overflow.
	s := *validFor
	if s < int(MinValidity/time.Second) || s > int(MaxValidity/time.Second) {
		return 0, CodeInvalidValue.Errorf("valid_for is %d seconds; it takes %d to %d seconds (1 minute to 7 days)",
			s, int(MinValidity/time.Second), int(MaxValidity/time.Second))
	}
	return time.Duration(s) * time.Second, nil
}

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
	// ExpiresAt is when the code stops working, as long after its issue as
	// the request's valid_for chose, 15 minutes by default, as RFC 3339 in
	// UTC to the second.
	ExpiresAt string `json:"expires_at"`
}

// DeviceSession is a Device Session as the API returns it: the key one
// device registered when it joined, under a name that never changes. Every
// time is RFC 3339 in UTC to the second.
type DeviceSession struct {
	ID string `json:"id"`
	// User is the name of the User the device acts as.
	User string `json:"user"`
	// Device is the device's name, which its Join Code bound.
	Device string `json:"device"`
	// AddedBy is the Device Session that issued the Join Code the device
	// joined with, left out when the Unix socket issued it.
	AddedBy *DeviceSessionRef `json:"added_by,omitzero"`
	// CreatedAt is when the device joined.
	CreatedAt string `json:"created_at"`
	// LastUsedAt is when a Device Connection of the Device Session last
	// opened a channel, kept to the minute; empty before the first.
	LastUsedAt string `json:"last_used_at,omitempty"`
	// RevokedAt is when the Device Session was Revoked; empty while it is
	// live.
	RevokedAt string `json:"revoked_at,omitempty"`
}

// DeviceSessionRef names a Device Session: by its id, which is unique for
// ever, and by its User and device name, which a later Device Session of
// that User may take once it is Revoked.
type DeviceSessionRef struct {
	ID     string `json:"id"`
	User   string `json:"user"`
	Device string `json:"device"`
}

// DeviceAddRequest issues a Join Code for one new device.
type DeviceAddRequest struct {
	// Device names the new device. Empty means device-<n>, the first such
	// name past those the User's devices and valid Join Codes hold.
	Device string `json:"device,omitempty"`
	// User is the User the code is for. Empty means the caller's own User;
	// for the Local caller, the Owner.
	User string `json:"user,omitempty"`
	// ValidFor is how long the code stays valid, in seconds, 60 to 604,800
	// (7 days); left out, 15 minutes.
	ValidFor *int `json:"valid_for,omitzero"`
}

// DeviceAddResponse is the Join Code device.add issued.
type DeviceAddResponse struct {
	JoinCode JoinCode `json:"join_code"`
}

// DeviceListRequest lists Device Sessions.
type DeviceListRequest struct {
	// User narrows the list to one User's Device Sessions. Empty means the
	// caller's own, or every User's for the Owner or an Administrator.
	User string `json:"user,omitempty"`
	// All adds the Revoked Device Sessions.
	All bool `json:"all,omitzero"`
}

// DeviceListResponse is every Device Session device.list selected, by User
// name and then in the order they joined.
type DeviceListResponse struct {
	DeviceSessions []DeviceSession `json:"device_sessions"`
}

// DeviceRevokeRequest revokes one live Device Session.
type DeviceRevokeRequest struct {
	// User is the User whose device it is. Empty means the caller's own; for
	// the Local caller, the Owner.
	User string `json:"user,omitempty"`
	// Device is the device's name.
	Device string `json:"device"`
	// Last confirms revoking the Owner's last live Device Session.
	Last bool `json:"last,omitzero"`
}

// DeviceRevokeResponse is the Device Session device.revoke Revoked.
type DeviceRevokeResponse struct {
	DeviceSession DeviceSession `json:"device_session"`
}

// The device operations.
var (
	// DeviceAdd issues a Join Code, valid for 15 minutes unless ValidFor
	// chooses otherwise, for one new device of the caller's own User or, for
	// the Owner, of any User, and for an Administrator, of any Member; a
	// Member naming another User, or an Administrator naming the Owner or
	// another Administrator, is refused with CodeOwnerOnly. A device name another live device or valid Join Code
	// of that User holds is refused with CodeNameTaken, and a ValidFor
	// outside 60 to 604,800 seconds with CodeInvalidValue.
	DeviceAdd = Op[DeviceAddRequest, DeviceAddResponse]{Name: "device.add", Write: true}
	// DeviceList lists the caller's own Device Sessions or, for the Owner or
	// an Administrator, every User's; a Member naming another User is
	// refused with CodeOwnerOnly.
	DeviceList = Op[DeviceListRequest, DeviceListResponse]{Name: "device.list"}
	// DeviceRevoke revokes one live Device Session of the caller's own User
	// or, for the Owner, of any User, and for an Administrator, of any
	// Member, in one transaction with voiding the valid Join Codes it
	// issued. A Member naming another User's is refused with
	// CodeUnknownDevice, as an unknown one is, and an Administrator naming
	// the Owner's or another Administrator's with CodeOwnerOnly; the Owner's
	// last live one, without Last, with CodeLastDeviceSession. Once it
	// commits, the Device Session's Device Connections close.
	DeviceRevoke = Op[DeviceRevokeRequest, DeviceRevokeResponse]{Name: "device.revoke", Write: true}
)

var deviceOperations = []Operation{
	DeviceAdd,
	DeviceList,
	DeviceRevoke,
}

// The device area's codes.
var (
	// CodeRevoked: the call came on a Device Session that is Revoked. The
	// CLI also answers it for a device whose Device Session the koko server
	// refused as Revoked, and for one that keeps the record of it.
	CodeRevoked = Code{"revoked", http.StatusUnauthorized}
	// CodeUnknownDevice: the caller has no live Device Session by the name
	// given that it may revoke; another User's, for a Member, is answered
	// the same.
	CodeUnknownDevice = Code{"unknown_device", http.StatusNotFound}
	// CodeLastDeviceSession: the call would revoke the Owner's last live
	// Device Session without confirming it, which would leave the Owner
	// only the koko server's own machine to add a device from.
	CodeLastDeviceSession = Code{"last_device_session", http.StatusConflict}
)

var deviceCodes = []Code{
	CodeRevoked,
	CodeUnknownDevice,
	CodeLastDeviceSession,
}
