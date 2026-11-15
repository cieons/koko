// Code generated from internal/shared/api/identity.go by make generate; DO NOT EDIT.

import type { Op } from "./api";

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
