// Code generated from internal/shared/api/user.go by make generate; DO NOT EDIT.

import type { Op } from "./api";
import type { DeviceSession, JoinCode } from "./device";

/** User is a User as the API returns it. */
export interface User {
  id: string;
  name: string;
  /** Role is "owner" for the Owner and "member" for every other User. */
  role: string;
  /** CreatedAt is RFC 3339 in UTC to the second. */
  created_at: string;
}

/** UserInviteRequest invites a new User under a name no User or Space holds. */
export interface UserInviteRequest {
  name: string;
}

/**
 * UserInviteResponse is the User user.invite created and their Invite: their
 * first Join Code.
 */
export interface UserInviteResponse {
  user: User;
  join_code: JoinCode;
}

/** UserCurrentRequest asks who the call acts as. */
export type UserCurrentRequest = Record<string, never>;

/**
 * UserCurrentResponse is who the call acts as: its User and the Device
 * Session it came on.
 */
export interface UserCurrentResponse {
  user: User;
  /**
   * DeviceSession is the Device Session the call came on, left out for the
   * Local caller, which has none and acts as the Owner.
   */
  device_session?: DeviceSession;
}

/**
 * UserInvite creates a Member with their Personal Space and issues their
 * first Join Code, in one transaction. A name any User, Removed ones
 * included, or any Space holds is refused with CodeNameTaken.
 */
export const UserInvite: Op<UserInviteRequest, UserInviteResponse> = {
  name: "user.invite",
  path: "/v1/user.invite",
  write: true,
};

/**
 * UserCurrent answers who the call acts as: the User of the Device
 * Session it came on, or the Owner for the Local caller. `koko join`
 * asks it, to learn the User and device name its Join Code bound.
 */
export const UserCurrent: Op<UserCurrentRequest, UserCurrentResponse> = {
  name: "user.current",
  path: "/v1/user.current",
  write: false,
};

/** CodeUnknownUser: no User has the name given. */
export const CodeUnknownUser = "unknown_user";
