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
  /**
   * RemovedAt is when the User was Removed, RFC 3339 in UTC to the
   * second; empty while they belong to the koko server.
   */
  removed_at?: string;
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

/** UserListRequest lists the Users. */
export interface UserListRequest {
  /** All adds the Removed Users. */
  all?: boolean;
}

/** UserListResponse is every User user.list selected, by name. */
export interface UserListResponse {
  users: User[];
}

/** UserRemoveRequest names a User to delete or Remove. */
export interface UserRemoveRequest {
  name: string;
}

/**
 * UserRemoveResponse is what user.remove did to the User: deleted them, or
 * Removed them.
 */
export interface UserRemoveResponse {
  /**
   * Outcome says what the call did: "deleted" (OutcomeDeleted) or
   * "removed" (OutcomeRemoved).
   */
  outcome: string;
  /**
   * User is the User as the call left them: as they were, when deleted,
   * and with their Removed time, when Removed.
   */
  user: User;
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

/**
 * UserList lists the Users, without the Removed ones unless All asks
 * for them. Only the Owner lists Users.
 */
export const UserList: Op<UserListRequest, UserListResponse> = {
  name: "user.list",
  path: "/v1/user.list",
  write: false,
};

/**
 * UserRemove deletes a User who never joined, with their memberships,
 * their Join Codes and their Personal Space, which must hold nothing
 * (CodeSpaceNotEmpty), and Removes any other User, in one transaction:
 * it revokes their Device Sessions, voids their valid Join Codes and
 * those their Device Sessions issued, takes them out of every Space,
 * and turns their Personal Space into an ordinary shared Space with no
 * Members, which only the Owner sees. Once it commits, their Device
 * Sessions' Device Connections close. Only the Owner removes Users, and
 * the Owner is never removed (CodeOwnerNotRemovable).
 */
export const UserRemove: Op<UserRemoveRequest, UserRemoveResponse> = {
  name: "user.remove",
  path: "/v1/user.remove",
  write: true,
};

/** CodeUnknownUser: no User has the name given. */
export const CodeUnknownUser = "unknown_user";

/**
 * CodeUserRemoved: the User named is Removed, so they are given no Join
 * Code and made a Member of no Space, and are not removed again.
 */
export const CodeUserRemoved = "user_removed";

/**
 * CodeOwnerNotRemovable: the call would delete or Remove the Owner,
 * whom the koko server always keeps.
 */
export const CodeOwnerNotRemovable = "owner_not_removable";
