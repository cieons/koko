// Code generated from internal/shared/api/user.go by make generate; DO NOT EDIT.

import type { Op } from "./api";
import type { DeviceSession, JoinCode } from "./device";
import type { CurrentSignIn, PasswordLink } from "./signin";

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
  /**
   * PasswordSetAt is when the User last set their Password, RFC 3339 in
   * UTC to the second, as user.list answers it; empty while they have
   * none, and in every other answer.
   */
  password_set_at?: string;
}

/** UserInviteRequest invites a new User under a name no User or Space holds. */
export interface UserInviteRequest {
  name: string;
  /**
   * ValidFor is how long the Invite stays valid, in seconds, 60 to
   * 604,800 (7 days); left out, 15 minutes.
   */
  valid_for?: number;
  /**
   * Web makes the Invite a Password Link, for a person who will use the
   * web UI in a browser, instead of a Join Code.
   */
  web?: boolean;
}

/**
 * UserInviteResponse is the User user.invite created and their Invite: their
 * first Join Code, or with web their first Password Link.
 */
export interface UserInviteResponse {
  user: User;
  /** JoinCode is the Invite without web; left out with it. */
  join_code?: JoinCode;
  /** PasswordLink is the Invite with web; left out without it. */
  password_link?: PasswordLink;
}

/** UserCurrentRequest asks who the call acts as. */
export type UserCurrentRequest = Record<string, never>;

/**
 * UserCurrentResponse is who the call acts as: its User and the Device
 * Session or the Sign-in it came on.
 */
export interface UserCurrentResponse {
  user: User;
  /**
   * DeviceSession is the Device Session the call came on, left out for the
   * Local caller, which has none and acts as the Owner, and for a Sign-in.
   */
  device_session?: DeviceSession;
  /**
   * SignIn is the Sign-in the call came on, from a browser on the web
   * listener; left out for every other caller.
   */
  signin?: CurrentSignIn;
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
 * first Join Code, or with Web their first Password Link, in one
 * transaction. A name any User, Removed ones included, or any Space
 * holds is refused with CodeNameTaken, a ValidFor outside 60 to 604,800
 * seconds with CodeInvalidValue, and Web while the Web URL is not set
 * with CodeWebURLUnset.
 */
export const UserInvite: Op<UserInviteRequest, UserInviteResponse> = {
  name: "user.invite",
  path: "/v1/user.invite",
  write: true,
};

/**
 * UserCurrent answers who the call acts as: the User of the Device
 * Session or the Sign-in it came on, or the Owner for the Local caller. `koko join`
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
 * UserRemove deletes a User who never used koko, with their
 * memberships, their Join Codes, their Password Links and their
 * Personal Space, which must hold nothing (CodeSpaceNotEmpty), and
 * Removes any other User, in one transaction: it revokes their Device
 * Sessions, voids their valid Join Codes and those their Device
 * Sessions issued, ends their Sign-ins, deletes their Password, voids
 * their unused Password Links, takes them out of every Space,
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
