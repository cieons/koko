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

/** SecretSetRequest replaces the value of a Secret its Space holds. */
export interface SecretSetRequest {
  space: string;
  name: string;
  /**
   * Value is the new value, 1 byte to 64 KiB, in standard base64
   * (RFC 4648, padded).
   */
  value_base64: string;
}

/** SecretSetResponse is the Secret secret.set changed, without its value. */
export interface SecretSetResponse {
  secret: Secret;
}

/** SecretRenameRequest gives a Secret a new name within its Space. */
export interface SecretRenameRequest {
  space: string;
  name: string;
  /** NewName is a name the Space does not hold. */
  new_name: string;
}

/** SecretRenameResponse is the Secret under its new name, without its value. */
export interface SecretRenameResponse {
  secret: Secret;
}

/** SecretMoveRequest moves a Secret, keeping its name, to another Space. */
export interface SecretMoveRequest {
  space: string;
  name: string;
  /** NewSpace names the Space to move it to, which must not hold its name. */
  new_space: string;
}

/** SecretMoveResponse is the Secret in its new Space, without its value. */
export interface SecretMoveResponse {
  secret: Secret;
}

/** SecretRemoveRequest names a Secret to delete. */
export interface SecretRemoveRequest {
  space: string;
  name: string;
}

/**
 * SecretRemoveResponse is the Secret secret.remove deleted, as it was,
 * without its value.
 */
export interface SecretRemoveResponse {
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

/**
 * SecretSet replaces a Secret's value, sealed afresh; the last call
 * wins. A name the Space does not hold is refused with
 * CodeUnknownSecret, never added, and the value is checked as
 * SecretAdd checks it.
 */
export const SecretSet: Op<SecretSetRequest, SecretSetResponse> = {
  name: "secret.set",
  path: "/v1/secret.set",
  write: true,
};

/**
 * SecretRename gives a Secret a new name within its Space, leaving its
 * sealed value as it is. A name the Space holds, its own included, is
 * refused with CodeNameTaken.
 */
export const SecretRename: Op<SecretRenameRequest, SecretRenameResponse> = {
  name: "secret.rename",
  path: "/v1/secret.rename",
  write: true,
};

/**
 * SecretMove moves a Secret, keeping its name and its sealed value, to
 * another Space; the caller must be able to act in both, and a NewSpace
 * they cannot act in is refused with CodeUnknownSpace too. A NewSpace
 * that holds the name is refused with CodeNameTaken; moving a Secret to
 * its own Space changes nothing.
 */
export const SecretMove: Op<SecretMoveRequest, SecretMoveResponse> = {
  name: "secret.move",
  path: "/v1/secret.move",
  write: true,
};

/** SecretRemove deletes a Secret and its value, for good. */
export const SecretRemove: Op<SecretRemoveRequest, SecretRemoveResponse> = {
  name: "secret.remove",
  path: "/v1/secret.remove",
  write: true,
};

/** CodeUnknownSecret: the Space holds no Secret with the name given. */
export const CodeUnknownSecret = "unknown_secret";

/**
 * CodeValueTooLarge: a Secret value is over SecretMaxSize; koko never
 * stores one cut short.
 */
export const CodeValueTooLarge = "value_too_large";

/**
 * CodeSecretUnreadable: a Secret's sealed value does not open under the
 * Root Key, for its own record: the database was edited or damaged. The
 * error names the Secret, never a value.
 */
export const CodeSecretUnreadable = "secret_unreadable";
