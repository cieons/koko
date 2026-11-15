// Code generated from internal/shared/api/identity.go by make generate; DO NOT EDIT.

import type { Op } from "./api";
import type { AuthEndpoint } from "./signin";

/**
 * IdentityProvider is the one OpenID Connect provider the Owner configured,
 * as server.oidc.show and server.oidc.set answer it. Its client secret is
 * never answered.
 */
export interface IdentityProvider {
  /**
   * Issuer is the provider's issuer URL, which its discovery document and
   * each ID token name byte for byte.
   */
  issuer: string;
  /** ClientID is the client koko is registered as at the provider. */
  client_id: string;
  /**
   * Name is the provider's name on the sign-in button, such as
   * "Authentik".
   */
  name: string;
  /**
   * ClientSecretSet is true: a provider is never configured without its
   * client secret, which is sealed and never answered.
   */
  client_secret_set: boolean;
  /**
   * LinkedIdentities is how many Users have linked their identity at
   * the provider: those a change of issuer, or a removal, unlinks.
   */
  linked_identities: number;
  /** SetAt is when the provider was last set, RFC 3339 in UTC. */
  set_at: string;
}

/** ServerOIDCShowRequest asks for the Identity Provider. */
export type ServerOIDCShowRequest = Record<string, never>;

/**
 * ServerOIDCShowResponse is the Identity Provider, and the redirect URI to
 * register at it.
 */
export interface ServerOIDCShowResponse {
  /** Provider is the Identity Provider; null while none is configured. */
  provider: IdentityProvider | null;
  /**
   * RedirectURI is the Web URL followed by RedirectPath, which the Owner
   * registers at the provider; null while the Web URL is not set.
   */
  redirect_uri: string | null;
}

/**
 * ServerOIDCSetRequest configures the Identity Provider: the settings it
 * names, and only those. The first set names all four.
 */
export interface ServerOIDCSetRequest {
  /**
   * Issuer is the provider's issuer URL: https:// only, a path allowed, no
   * query or fragment, at most MaxIssuerLength bytes.
   */
  issuer?: string;
  /**
   * ClientID is the client koko is registered as, 1 to MaxClientIDLength
   * bytes.
   */
  client_id?: string;
  /**
   * ClientSecret is the client's secret, 1 to MaxClientSecretLength bytes,
   * which the koko server seals and never answers.
   */
  client_secret?: string;
  /**
   * Name is the provider's name on the sign-in button, 1 to
   * MaxProviderNameLength characters.
   */
  name?: string;
}

/**
 * ServerOIDCSetResponse is the Identity Provider as server.oidc.set left
 * it.
 */
export interface ServerOIDCSetResponse {
  provider: IdentityProvider;
  /**
   * RedirectURI is the Web URL followed by RedirectPath, which the Owner
   * registers at the provider.
   */
  redirect_uri: string;
  /**
   * IdentitiesUnlinked is how many Linked Identities the set unlinked: every
   * one, when it changed the issuer, and none otherwise.
   */
  identities_unlinked: number;
}

/** ServerOIDCRemoveRequest removes the Identity Provider. */
export type ServerOIDCRemoveRequest = Record<string, never>;

/** ServerOIDCRemoveResponse is what removing the Identity Provider did. */
export interface ServerOIDCRemoveResponse {
  /**
   * IdentitiesUnlinked is how many Linked Identities it unlinked: every
   * one.
   */
  identities_unlinked: number;
}

/**
 * ServerOIDCShow returns the Identity Provider, without its client
 * secret, and the redirect URI to register at it. It answers the Owner
 * and the Administrators, the Local caller included.
 */
export const ServerOIDCShow: Op<ServerOIDCShowRequest, ServerOIDCShowResponse> = {
  name: "server.oidc.show",
  path: "/v1/server.oidc.show",
  write: false,
};

/**
 * ServerOIDCSet configures the Identity Provider. It is for the Owner
 * alone, the Local caller included, and refuses anyone else with
 * CodeOwnerOnly before it reads anything. It is refused with
 * CodeWebURLUnset while the Web URL is not set, with CodeInvalidRequest
 * for a first set that does not name all four settings, and with
 * CodeInvalidProvider for a setting koko does not take. A new or
 * changed issuer's discovery is checked first, outside any transaction:
 * one that is not valid is refused with CodeInvalidProvider, and a
 * provider that does not answer with CodeProviderUnreachable, changing
 * nothing. A change of issuer unlinks every Linked Identity and ends the
 * Sign-ins made through them; a change of the client or the name keeps
 * them.
 */
export const ServerOIDCSet: Op<ServerOIDCSetRequest, ServerOIDCSetResponse> = {
  name: "server.oidc.set",
  path: "/v1/server.oidc.set",
  write: true,
};

/**
 * ServerOIDCRemove removes the Identity Provider, unlinking every Linked
 * Identity and ending the Sign-ins made through them. It is for the
 * Owner alone, as ServerOIDCSet is, and is refused with
 * CodeProviderUnset while none is configured.
 */
export const ServerOIDCRemove: Op<ServerOIDCRemoveRequest, ServerOIDCRemoveResponse> = {
  name: "server.oidc.remove",
  path: "/v1/server.oidc.remove",
  write: true,
};

/**
 * Identity is a Linked Identity as the API returns it: a User's account at
 * the Identity Provider, known by its issuer and subject, which signs them
 * in to the web UI (ADR 0060). Every time is RFC 3339 in UTC to the
 * second.
 */
export interface Identity {
  id: string;
  /** User is the name of the User whose identity it is. */
  user: string;
  issuer: string;
  /**
   * Subject is the provider's id of the account, which with the issuer
   * is the identity.
   */
  subject: string;
  /**
   * Label is what the provider called the account when it was linked:
   * shown, never matched.
   */
  label: string;
  /**
   * LinkedBy is the name of the User who issued the Sign-in Link it was
   * linked through, the Owner for a link the koko server's machine
   * issued; null when its User linked it from a Sign-in of their own.
   */
  linked_by: string | null;
  linked_at: string;
  /** LastUsedAt is when it last signed a browser in; null until then. */
  last_used_at: string | null;
}

/** IdentityListRequest lists Linked Identities. */
export interface IdentityListRequest {
  /**
   * User lists only the Linked Identity of the User it names; left out,
   * the caller's own.
   */
  user?: string;
}

/**
 * IdentityListResponse is the Linked Identities identity.list selected: at
 * most one.
 */
export interface IdentityListResponse {
  identities: Identity[];
}

/**
 * IdentityUnlinkRequest names the User whose Linked Identity to unlink;
 * left out, the caller's own.
 */
export interface IdentityUnlinkRequest {
  user?: string;
}

/**
 * IdentityUnlinkResponse is the Linked Identity unlinked, and how many
 * Sign-ins made through it ended.
 */
export interface IdentityUnlinkResponse {
  identity: Identity;
  signins_ended: number;
}

/**
 * IdentityList lists the caller's own Linked Identity, or with User
 * another User's, which only the Owner and the Administrators see: a
 * Member naming another User gets none, whether or not the name is a
 * User's.
 */
export const IdentityList: Op<IdentityListRequest, IdentityListResponse> = {
  name: "identity.list",
  path: "/v1/identity.list",
  write: false,
};

/**
 * IdentityUnlink unlinks a User's Linked Identity and ends the Sign-ins
 * made through it, and no others: anyone's own; another User's for the
 * Owner and the Administrators, as access lets them act on that User,
 * the Owner's or another Administrator's, for an Administrator, being
 * refused with CodeOwnerOnly. A Member naming another User is refused
 * with CodeOwnerOnly before the name is read, and a User without one
 * with CodeNoIdentity.
 */
export const IdentityUnlink: Op<IdentityUnlinkRequest, IdentityUnlinkResponse> = {
  name: "identity.unlink",
  path: "/v1/identity.unlink",
  write: true,
};

/**
 * CodeInvalidProvider: a setting of the Identity Provider is not one
 * koko takes, or the issuer's discovery document is not valid; the
 * message says which.
 */
export const CodeInvalidProvider = "invalid_provider";

/**
 * CodeProviderUnreachable: the Identity Provider did not answer in
 * time.
 */
export const CodeProviderUnreachable = "provider_unreachable";

/** CodeProviderUnset: no Identity Provider is configured. */
export const CodeProviderUnset = "provider_unset";

/**
 * CodeProviderRefused: the Identity Provider answered the sign-in with
 * an error, such as the person cancelling it there.
 */
export const CodeProviderRefused = "provider_refused";

/**
 * CodeProviderFailed: the code exchange, or the ID token, failed a
 * check.
 */
export const CodeProviderFailed = "provider_failed";

/**
 * CodeIdentityNotLinked: no User has this identity linked; the same
 * whether or not anyone ever had.
 */
export const CodeIdentityNotLinked = "identity_not_linked";

/** CodeIdentityLinked: the User has a Linked Identity already. */
export const CodeIdentityLinked = "identity_linked";

/** CodeIdentityTaken: another User has this identity linked. */
export const CodeIdentityTaken = "identity_taken";

/** CodeNoIdentity: the User has no Linked Identity to unlink. */
export const CodeNoIdentity = "no_identity";

/**
 * CodeSignInExpired: the provider sent back a browser with no flow it
 * began, or after the flow's 10 minutes.
 */
export const CodeSignInExpired = "signin_expired";

/**
 * AuthOIDCBegin begins a flow through the Identity Provider: signing in
 * with next, linking to the request's Sign-in, or linking through the
 * Sign-in Link whose secret is Link. It sets the flow's binding cookie and
 * answers where to send the browser. It is refused with CodeProviderUnset
 * while no provider is configured, with CodeProviderUnreachable when the
 * provider's discovery does not answer, and for linking with
 * CodeSignInRequired without a live Sign-in, or with CodeLinkUsed or
 * CodeLinkInvalid for a link no longer valid.
 */
export const AuthOIDCBegin: AuthEndpoint<AuthOIDCBeginRequest, AuthOIDCBeginResponse> = {
  path: "/auth/oidc/begin",
};

/** AuthOIDCBeginRequest begins a flow through the Identity Provider. */
export interface AuthOIDCBeginRequest {
  /** Purpose is OIDCPurposeSignIn or OIDCPurposeLink. */
  purpose: string;
  /**
   * Next is the path of this origin a sign-in ends on, outside /signin
   * and /link; any other is /. Only for signing in.
   */
  next?: string;
  /**
   * Link is the secret of the Sign-in Link to link through; left out, a
   * link is to the request's Sign-in. Only for linking.
   */
  link?: string;
}

/**
 * AuthOIDCBeginResponse is where to send the browser: the provider's
 * authorization URL for the flow.
 */
export interface AuthOIDCBeginResponse {
  location: string;
}
