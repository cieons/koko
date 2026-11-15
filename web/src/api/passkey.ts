// Code generated from internal/shared/api/passkey.go by make generate; DO NOT EDIT.

import type { Op } from "./api";
import type { AuthEndpoint, AuthSignInResponse } from "./signin";
import type { User } from "./user";

/**
 * Passkey is a Passkey as the API returns it: one a User's device or
 * password manager made for the Web URL's host, which signs them in to the
 * web UI there (ADR 0060). Every time is RFC 3339 in UTC to the second.
 */
export interface Passkey {
  id: string;
  /** User is the name of the User whose Passkey it is. */
  user: string;
  /**
   * Name is what its User called it when it was made: on Account, the
   * name they gave it, prefilled with the browser's label; through a
   * Sign-in Link, the browser's label.
   */
  name: string;
  /**
   * Host is the host it was made for, the Web URL's then; it signs in
   * only while the Web URL names that host.
   */
  host: string;
  /**
   * Synced says the authenticator backs it up, as a password manager that
   * syncs it does, as of its last use.
   */
  synced: boolean;
  /**
   * AddedBy is the name of the User who issued the Sign-in Link it was
   * made through, the Owner for a link the koko server's machine issued;
   * null when its User made it from a Sign-in of their own.
   */
  added_by: string | null;
  added_at: string;
  /** LastUsedAt is when it last signed a browser in; null until then. */
  last_used_at: string | null;
}

/** PasskeyListRequest lists Passkeys. */
export interface PasskeyListRequest {
  /**
   * User lists only the Passkeys of the User it names; left out, the
   * caller's own.
   */
  user?: string;
}

/**
 * PasskeyListResponse is every Passkey passkey.list selected, by when each
 * was made.
 */
export interface PasskeyListResponse {
  passkeys: Passkey[];
}

/** PasskeyRemoveRequest names the Passkey to remove by its id. */
export interface PasskeyRemoveRequest {
  id: string;
}

/**
 * PasskeyRemoveResponse is the Passkey removed, and how many Sign-ins made
 * with it ended.
 */
export interface PasskeyRemoveResponse {
  passkey: Passkey;
  signins_ended: number;
}

/**
 * PasskeyList lists the caller's own Passkeys, or with User another
 * User's, which only the Owner and the Administrators see: a Member
 * naming another User gets none, whether or not the name is a User's.
 */
export const PasskeyList: Op<PasskeyListRequest, PasskeyListResponse> = {
  name: "passkey.list",
  path: "/v1/passkey.list",
  write: false,
};

/**
 * PasskeyRemove removes a Passkey and ends the Sign-ins made with it,
 * and no others: anyone's own; another User's for the Owner and the
 * Administrators, as access lets them act on that User. Another User's,
 * for a Member, is refused with CodeUnknownPasskey, as an id no
 * Passkey holds, and the Owner's or another Administrator's, for an
 * Administrator, with CodeOwnerOnly.
 */
export const PasskeyRemove: Op<PasskeyRemoveRequest, PasskeyRemoveResponse> = {
  name: "passkey.remove",
  path: "/v1/passkey.remove",
  write: true,
};

/**
 * CodePasskeyRefused: a passkey's answer did not verify, named no
 * Passkey for this host, or came for a flow that was used, expired or
 * never begun; the answer is the same for each.
 */
export const CodePasskeyRefused = "passkey_refused";

/** CodeUnknownPasskey: no Passkey the caller may see has the id given. */
export const CodeUnknownPasskey = "unknown_passkey";

/**
 * AuthPasskeySignInBegin begins signing in with a passkey: it answers
 * the options for navigator.credentials.get() and the flow its answer
 * names. It counts for no rate limit.
 */
export const AuthPasskeySignInBegin: AuthEndpoint<AuthPasskeySignInBeginRequest, AuthPasskeySignInBeginResponse> = {
  path: "/auth/passkey/signin/begin",
};

/**
 * AuthPasskeySignIn signs in with a passkey's answer, setting the
 * Sign-in cookie. An answer that does not verify, names no Passkey for
 * this host, or names a flow used, expired or never begun, is refused
 * with CodePasskeyRefused, the same bytes for each; an address past its
 * limit with CodeTooManyAttempts.
 */
export const AuthPasskeySignIn: AuthEndpoint<AuthPasskeySignInRequest, AuthSignInResponse> = {
  path: "/auth/passkey/signin",
};

/**
 * AuthPasskeyAddBegin begins adding a passkey: through a Sign-in Link,
 * given Link, or given none, from the request's Sign-in. It answers the
 * options for navigator.credentials.create() and the flow its answer
 * names, spending nothing.
 */
export const AuthPasskeyAddBegin: AuthEndpoint<AuthPasskeyAddBeginRequest, AuthPasskeyAddBeginResponse> = {
  path: "/auth/passkey/add/begin",
};

/**
 * AuthPasskeyAdd records a passkey from its answer. Through a Sign-in
 * Link, it spends the link once the answer verifies, ends the User's
 * other Sign-ins and signs this browser in, setting the cookie; from a
 * Sign-in, it keeps the Sign-in. An answer that does not verify is
 * refused with CodePasskeyRefused.
 */
export const AuthPasskeyAdd: AuthEndpoint<AuthPasskeyAddRequest, AuthPasskeyAddResponse> = {
  path: "/auth/passkey/add",
};

/** AuthPasskeySignInBeginRequest begins signing in with a passkey. */
export type AuthPasskeySignInBeginRequest = Record<string, never>;

/**
 * AuthPasskeySignInBeginResponse is a sign-in's flow and the options to
 * pass navigator.credentials.get().
 */
export interface AuthPasskeySignInBeginResponse {
  /**
   * Flow names the ceremony; the answer posts it back. It is secret to
   * the page.
   */
  flow: string;
  options: PasskeyRequestOptions;
}

/** AuthPasskeySignInRequest is a passkey's answer to a sign-in's options. */
export interface AuthPasskeySignInRequest {
  flow: string;
  credential: PasskeyAssertion;
}

/**
 * AuthPasskeyAddBeginRequest begins adding a passkey: through the Sign-in
 * Link whose secret Link is, or, with Link left out, from the request's
 * Sign-in.
 */
export interface AuthPasskeyAddBeginRequest {
  link?: string;
}

/**
 * AuthPasskeyAddBeginResponse is a passkey's creation flow and the options
 * to pass navigator.credentials.create().
 */
export interface AuthPasskeyAddBeginResponse {
  flow: string;
  options: PasskeyCreationOptions;
}

/** AuthPasskeyAddRequest is a passkey's answer to a creation's options. */
export interface AuthPasskeyAddRequest {
  flow: string;
  credential: PasskeyRegistration;
  /**
   * Name is the Passkey's name, 1 to MaxPasskeyNameLength characters,
   * taken only from a Sign-in; left out, or through a Sign-in Link, the
   * browser's label.
   */
  name?: string;
}

/** AuthPasskeyAddResponse is the User signed in and the Passkey made. */
export interface AuthPasskeyAddResponse {
  user: User;
  passkey: Passkey;
}

/**
 * PasskeyRequestOptions is what koko asks of an authenticator to sign in,
 * as WebAuthn Level 3's PublicKeyCredentialRequestOptionsJSON: binary
 * members in base64url.
 */
export interface PasskeyRequestOptions {
  challenge: string;
  /** RPID is the Web URL's host. */
  rpId: string;
  timeout: number;
  userVerification: string;
  /**
   * AllowCredentials is empty: the authenticator offers its discoverable
   * passkeys for the host.
   */
  allowCredentials: PasskeyDescriptor[];
}

/**
 * PasskeyCreationOptions is what koko asks of an authenticator to make a
 * passkey, as WebAuthn Level 3's PublicKeyCredentialCreationOptionsJSON:
 * binary members in base64url.
 */
export interface PasskeyCreationOptions {
  rp: PasskeyRP;
  /** User names the User and nothing else about the person. */
  user: PasskeyUser;
  challenge: string;
  pubKeyCredParams: PasskeyParam[];
  timeout: number;
  /**
   * ExcludeCredentials are the User's Passkeys for this host, which the
   * authenticator does not make again.
   */
  excludeCredentials: PasskeyDescriptor[];
  authenticatorSelection: PasskeySelection;
  attestation: string;
}

/** PasskeyRP is the relying party a passkey is made for: the Web URL's host. */
export interface PasskeyRP {
  id: string;
  name: string;
}

/**
 * PasskeyUser is the User a passkey is made for: their id, in base64url,
 * and their name.
 */
export interface PasskeyUser {
  id: string;
  name: string;
  displayName: string;
}

/** PasskeyParam is one algorithm koko takes, by its COSE number. */
export interface PasskeyParam {
  type: string;
  alg: number;
}

/** PasskeyDescriptor names one passkey by its credential id, in base64url. */
export interface PasskeyDescriptor {
  type: string;
  id: string;
}

/**
 * PasskeySelection is what koko asks of the authenticator itself: a
 * discoverable passkey, and that it checks who is there.
 */
export interface PasskeySelection {
  residentKey: string;
  requireResidentKey: boolean;
  userVerification: string;
}

/**
 * PasskeyRegistration is a browser's answer to
 * navigator.credentials.create(), as PublicKeyCredential.toJSON() gives
 * it: binary members in base64url. Members koko does not read are taken
 * and left unread.
 */
export interface PasskeyRegistration {
  id: string;
  rawId: string;
  type: string;
  response: PasskeyRegistrationResponse;
}

/** PasskeyRegistrationResponse is a creation's AuthenticatorAttestationResponse. */
export interface PasskeyRegistrationResponse {
  clientDataJSON: string;
  attestationObject: string;
}

/**
 * PasskeyAssertion is a browser's answer to navigator.credentials.get(),
 * as PublicKeyCredential.toJSON() gives it: binary members in base64url.
 * Members koko does not read are taken and left unread.
 */
export interface PasskeyAssertion {
  id: string;
  rawId: string;
  type: string;
  response: PasskeyAssertionResponse;
}

/** PasskeyAssertionResponse is a sign-in's AuthenticatorAssertionResponse. */
export interface PasskeyAssertionResponse {
  clientDataJSON: string;
  authenticatorData: string;
  signature: string;
  userHandle: string;
}
