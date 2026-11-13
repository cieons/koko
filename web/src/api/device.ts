// Code generated from internal/shared/api/device.go by make generate; DO NOT EDIT.

import type { Op } from "./api";

/**
 * JoinCode is a Join Code just issued, as the API returns it once: the code
 * itself is never stored and never shown again.
 */
export interface JoinCode {
  id: string;
  /** Code is the koko_ string to paste into `koko join` on the new device. */
  code: string;
  /** User is the name of the User the new device joins as. */
  user: string;
  /** Device is the new device's name, bound to the code. */
  device: string;
  /**
   * ExpiresAt is when the code stops working, as long after its issue as
   * the request's valid_for chose, 15 minutes by default, as RFC 3339 in
   * UTC to the second.
   */
  expires_at: string;
}

/**
 * DeviceSession is a Device Session as the API returns it: the key one
 * device registered when it joined, under a name that never changes. Every
 * time is RFC 3339 in UTC to the second.
 */
export interface DeviceSession {
  id: string;
  /** User is the name of the User the device acts as. */
  user: string;
  /** Device is the device's name, which its Join Code bound. */
  device: string;
  /**
   * AddedBy is the Device Session that issued the Join Code the device
   * joined with, left out when the Unix socket issued it.
   */
  added_by?: DeviceSessionRef;
  /** CreatedAt is when the device joined. */
  created_at: string;
  /**
   * LastUsedAt is when a Device Connection of the Device Session last
   * opened a channel, kept to the minute; empty before the first.
   */
  last_used_at?: string;
  /**
   * RevokedAt is when the Device Session was Revoked; empty while it is
   * live.
   */
  revoked_at?: string;
}

/**
 * DeviceSessionRef names a Device Session: by its id, which is unique for
 * ever, and by its User and device name, which a later Device Session of
 * that User may take once it is Revoked.
 */
export interface DeviceSessionRef {
  id: string;
  user: string;
  device: string;
}

/** DeviceAddRequest issues a Join Code for one new device. */
export interface DeviceAddRequest {
  /**
   * Device names the new device. Empty means device-<n>, the first such
   * name past those the User's devices and valid Join Codes hold.
   */
  device?: string;
  /**
   * User is the User the code is for. Empty means the caller's own User;
   * for the Local caller, the Owner.
   */
  user?: string;
  /**
   * ValidFor is how long the code stays valid, in seconds, 60 to 604,800
   * (7 days); left out, 15 minutes.
   */
  valid_for?: number;
}

/** DeviceAddResponse is the Join Code device.add issued. */
export interface DeviceAddResponse {
  join_code: JoinCode;
}

/** DeviceListRequest lists Device Sessions. */
export interface DeviceListRequest {
  /**
   * User narrows the list to one User's Device Sessions. Empty means the
   * caller's own, or every User's for the Owner or an Administrator.
   */
  user?: string;
  /** All adds the Revoked Device Sessions. */
  all?: boolean;
}

/**
 * DeviceListResponse is every Device Session device.list selected, by User
 * name and then in the order they joined.
 */
export interface DeviceListResponse {
  device_sessions: DeviceSession[];
}

/** DeviceRevokeRequest revokes one live Device Session. */
export interface DeviceRevokeRequest {
  /**
   * User is the User whose device it is. Empty means the caller's own; for
   * the Local caller, the Owner.
   */
  user?: string;
  /** Device is the device's name. */
  device: string;
  /** Last confirms revoking the Owner's last live Device Session. */
  last?: boolean;
}

/** DeviceRevokeResponse is the Device Session device.revoke Revoked. */
export interface DeviceRevokeResponse {
  device_session: DeviceSession;
}

/**
 * DeviceAdd issues a Join Code, valid for 15 minutes unless ValidFor
 * chooses otherwise, for one new device of the caller's own User or, for
 * the Owner, of any User, and for an Administrator, of any Member; a
 * Member naming another User, or an Administrator naming the Owner or
 * another Administrator, is refused with CodeOwnerOnly. A device name another live device or valid Join Code
 * of that User holds is refused with CodeNameTaken, and a ValidFor
 * outside 60 to 604,800 seconds with CodeInvalidValue.
 */
export const DeviceAdd: Op<DeviceAddRequest, DeviceAddResponse> = {
  name: "device.add",
  path: "/v1/device.add",
  write: true,
};

/**
 * DeviceList lists the caller's own Device Sessions or, for the Owner or
 * an Administrator, every User's; a Member naming another User is
 * refused with CodeOwnerOnly.
 */
export const DeviceList: Op<DeviceListRequest, DeviceListResponse> = {
  name: "device.list",
  path: "/v1/device.list",
  write: false,
};

/**
 * DeviceRevoke revokes one live Device Session of the caller's own User
 * or, for the Owner, of any User, and for an Administrator, of any
 * Member, in one transaction with voiding the valid Join Codes it
 * issued. A Member naming another User's is refused with
 * CodeUnknownDevice, as an unknown one is, and an Administrator naming
 * the Owner's or another Administrator's with CodeOwnerOnly; the Owner's
 * last live one, without Last, with CodeLastDeviceSession. Once it
 * commits, the Device Session's Device Connections close.
 */
export const DeviceRevoke: Op<DeviceRevokeRequest, DeviceRevokeResponse> = {
  name: "device.revoke",
  path: "/v1/device.revoke",
  write: true,
};

/**
 * CodeRevoked: the call came on a Device Session that is Revoked. The
 * CLI also answers it for a device whose Device Session the koko server
 * refused as Revoked, and for one that keeps the record of it.
 */
export const CodeRevoked = "revoked";

/**
 * CodeUnknownDevice: the caller has no live Device Session by the name
 * given that it may revoke; another User's, for a Member, is answered
 * the same.
 */
export const CodeUnknownDevice = "unknown_device";

/**
 * CodeLastDeviceSession: the call would revoke the Owner's last live
 * Device Session without confirming it, which would leave the Owner
 * only the koko server's own machine to add a device from.
 */
export const CodeLastDeviceSession = "last_device_session";
