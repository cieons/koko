// Code generated from internal/shared/api/signin.go by make generate; DO NOT EDIT.

import type { Op } from "./api";

/**
 * PasswordLink is a Password Link just issued, as the API returns it once:
 * its secret is never stored and never shown again.
 */
export interface PasswordLink {
  id: string;
  /** User is the name of the User whose Password the link sets. */
  user: string;
  /** Link is the link itself: the Web URL followed by /password#<secret>. */
  link: string;
  /**
   * ExpiresAt is when the link stops working, as long after its issue as
   * the request's valid_for chose, 15 minutes by default, as RFC 3339 in
   * UTC to the second.
   */
  expires_at: string;
}

/**
 * SignIn is a Sign-in as the API returns it: one browser signed in with a
 * User's Password. Every time is RFC 3339 in UTC to the second.
 */
export interface SignIn {
  id: string;
  /** User is the name of the User the browser is signed in as. */
  user: string;
  /**
   * Client is a short label of the browser, made from its User-Agent at
   * sign-in, such as "Safari on iPhone", or "Browser".
   */
  client: string;
  /**
   * Address is the client's address when the Sign-in was last used, as
   * the reverse proxy passed it on.
   */
  address: string;
  /** StartedAt is when the browser signed in. */
  started_at: string;
  /** LastUsedAt is when the Sign-in was last used, kept to the minute. */
  last_used_at: string;
  /**
   * ExpiresAt is when the Sign-in ends unless it is used again first: 7
   * days after it was last used, and never later than 30 days after it
   * began.
   */
  expires_at: string;
  /** EndedAt is when the Sign-in ended; empty while it is live. */
  ended_at?: string;
  /**
   * EndReason is why it ended: "signed_out", "expired", "revoked",
   * "password_changed", "password_removed" or "user_removed"; empty while
   * it is live.
   */
  end_reason?: string;
}

/** CurrentSignIn is the Sign-in a call came on, as user.current names it. */
export interface CurrentSignIn {
  id: string;
  /** Client is the browser's label, such as "Safari on iPhone". */
  client: string;
  started_at: string;
  expires_at: string;
}

/** PasswordLinkRequest issues a Password Link for the User Name names. */
export interface PasswordLinkRequest {
  user: string;
  /**
   * ValidFor is how long the link stays valid, in seconds, 60 to 604,800
   * (7 days); left out, 15 minutes.
   */
  valid_for?: number;
}

/** PasswordLinkResponse is the Password Link password.link issued. */
export interface PasswordLinkResponse {
  password_link: PasswordLink;
}

/** PasswordRemoveRequest names the User whose Password to remove. */
export interface PasswordRemoveRequest {
  user: string;
}

/** PasswordRemoveResponse names the User whose Password was removed. */
export interface PasswordRemoveResponse {
  user: string;
}

/** SignInListRequest lists Sign-ins. */
export interface SignInListRequest {
  /** User lists only the Sign-ins of the User it names. */
  user?: string;
  /** All adds the Sign-ins that have ended. */
  all?: boolean;
}

/**
 * SignInListResponse is every Sign-in signin.list selected, by User name
 * and then by when each began.
 */
export interface SignInListResponse {
  signins: SignIn[];
}

/** SignInRevokeRequest names the Sign-in to revoke by its id. */
export interface SignInRevokeRequest {
  id: string;
}

/** SignInRevokeResponse is the Sign-in as signin.revoke left it. */
export interface SignInRevokeResponse {
  signin: SignIn;
}

/**
 * PasswordLinkIssue issues a Password Link for a User who is not Removed,
 * the Owner included, voiding their earlier unused one. Only the Owner
 * issues one, and a Member is refused before the name is read. It is
 * refused with CodeWebURLUnset while the Web URL is not set, and a
 * ValidFor outside 60 to 604,800 seconds with CodeInvalidValue.
 */
export const PasswordLinkIssue: Op<PasswordLinkRequest, PasswordLinkResponse> = {
  name: "password.link",
  path: "/v1/password.link",
  write: true,
};

/**
 * PasswordRemove removes a User's Password, ends their Sign-ins and
 * voids their unused Password Link, keeping the User and their
 * devices. A User without a Password is refused with CodeNoPassword.
 * Only the Owner removes one.
 */
export const PasswordRemove: Op<PasswordRemoveRequest, PasswordRemoveResponse> = {
  name: "password.remove",
  path: "/v1/password.remove",
  write: true,
};

/**
 * SignInList lists the caller's own Sign-ins, and everyone's for the
 * Owner, without the ended ones unless All asks for them.
 */
export const SignInList: Op<SignInListRequest, SignInListResponse> = {
  name: "signin.list",
  path: "/v1/signin.list",
  write: false,
};

/**
 * SignInRevoke ends a live Sign-in: anyone's own, and anyone's for the
 * Owner. Another User's, for a Member, is refused with
 * CodeUnknownSignIn, as an id no Sign-in holds.
 */
export const SignInRevoke: Op<SignInRevokeRequest, SignInRevokeResponse> = {
  name: "signin.revoke",
  path: "/v1/signin.revoke",
  write: true,
};

/**
 * CodeWebURLUnset: a Password Link would name no address, since the
 * Owner has not set the Web URL (koko server set web_url=).
 */
export const CodeWebURLUnset = "web_url_unset";

/**
 * CodeInvalidPassword: a new Password breaks the rule: 15 to 128
 * characters, and not the User's name.
 */
export const CodeInvalidPassword = "invalid_password";

/**
 * CodeWrongPassword: the User name or the Password given is wrong; the
 * answer is the same for both.
 */
export const CodeWrongPassword = "wrong_password";

/**
 * CodeTooManyAttempts: too many failed attempts came from this address,
 * or for this User name; the Retry-After header says when to try again.
 */
export const CodeTooManyAttempts = "too_many_attempts";

/** CodeLinkUsed: the Password Link was used already. */
export const CodeLinkUsed = "link_used";

/**
 * CodeLinkInvalid: the Password Link expired, was voided by a newer
 * one, is for a Removed User, or was never issued.
 */
export const CodeLinkInvalid = "link_invalid";

/** CodeUnknownSignIn: no Sign-in the caller may see has the id given. */
export const CodeUnknownSignIn = "unknown_signin";

/** CodeNoPassword: the User has no Password to remove. */
export const CodeNoPassword = "no_password";
