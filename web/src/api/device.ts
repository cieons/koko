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
   * ExpiresAt is when the code stops working, 15 minutes after its issue,
   * as RFC 3339 in UTC to the second.
   */
  expires_at: string;
}

/**
 * DeviceSession is a Device Session as the API returns it: the key one
 * device registered when it joined, under a name that never changes.
 */
export interface DeviceSession {
  id: string;
  /** User is the name of the User the device acts as. */
  user: string;
  /** Device is the device's name, which its Join Code bound. */
  device: string;
  /** CreatedAt is when the device joined, as RFC 3339 in UTC to the second. */
  created_at: string;
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
}

/** DeviceAddResponse is the Join Code device.add issued. */
export interface DeviceAddResponse {
  join_code: JoinCode;
}

/**
 * DeviceAdd issues a Join Code, valid for 15 minutes, for one new device
 * of the caller's own User or, for the Owner, of any User; a Member
 * naming another User is refused with CodeOwnerOnly. A device name
 * another live device or valid Join Code of that User holds is refused
 * with CodeNameTaken.
 */
export const DeviceAdd: Op<DeviceAddRequest, DeviceAddResponse> = {
  name: "device.add",
  path: "/v1/device.add",
  write: true,
};
