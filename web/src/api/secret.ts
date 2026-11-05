// Code generated from internal/shared/api/secret.go by make generate; DO NOT EDIT.

import type { Op } from "./api";

/**
 * Secret is a Secret as the API names it: its name in its Space, and never
 * its value.
 */
export interface Secret {
  id: string;
  /** Space is the name of the Space that holds it. */
  space: string;
  /**
   * Name is unique within its Space only: 1 to 63 of a-z, 0-9, '-' and
   * '_', starting with a letter or a digit.
   */
  name: string;
  /** CreatedAt is when it was added, as RFC 3339 in UTC to the second. */
  created_at: string;
  /** UpdatedAt is when it last changed, as RFC 3339 in UTC to the second. */
  updated_at: string;
}

/**
 * SecretListRequest asks for the Secrets of one Space, or of every Space the
 * caller can act in.
 */
export interface SecretListRequest {
  /**
   * Space names the one Space to list. Empty means every Space the caller
   * can act in.
   */
  space?: string;
}

/**
 * SecretListResponse holds the Secrets listed, by Space name, then by name,
 * without their values.
 */
export interface SecretListResponse {
  secrets: Secret[];
}

/** SecretShowRequest names a Secret to show, by its Space and its name. */
export interface SecretShowRequest {
  space: string;
  name: string;
}

/**
 * SecretShowResponse is the Secret secret.show names, with its value: the
 * one response that holds a value.
 */
export interface SecretShowResponse {
  secret: Secret;
  /** Value is the value as stored, in standard base64 (RFC 4648, padded). */
  value_base64: string;
}

/** SecretAddRequest stores a new Secret under a name its Space does not hold. */
export interface SecretAddRequest {
  space: string;
  name: string;
  /**
   * Value is the value to store, 1 byte to 64 KiB, in standard base64
   * (RFC 4648, padded).
   */
  value_base64: string;
}

/** SecretAddResponse is the Secret secret.add stored, without its value. */
export interface SecretAddResponse {
  secret: Secret;
}

/**
 * SecretList lists Secrets without their values: those of one Space, or
 * of every Space the caller can act in.
 */
export const SecretList: Op<SecretListRequest, SecretListResponse> = {
  name: "secret.list",
  path: "/v1/secret.list",
  write: false,
};

/**
 * SecretShow shows a Secret with its value. A name the Space does not
 * hold is refused with CodeUnknownSecret, and a sealed value that does
 * not open with CodeSecretUnreadable.
 */
export const SecretShow: Op<SecretShowRequest, SecretShowResponse> = {
  name: "secret.show",
  path: "/v1/secret.show",
  write: false,
};

/**
 * SecretAdd stores a new Secret, its value sealed. A name the Space
 * holds is refused with CodeNameTaken, an empty value with
 * CodeInvalidRequest, and one over SecretMaxSize with
 * CodeValueTooLarge.
 */
export const SecretAdd: Op<SecretAddRequest, SecretAddResponse> = {
  name: "secret.add",
  path: "/v1/secret.add",
  write: true,
};
