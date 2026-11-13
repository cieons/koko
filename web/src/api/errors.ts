// Code generated from internal/shared/api/errors.go by make generate; DO NOT EDIT.

/**
 * Error is the error object of a failed call. Its code is stable: within a
 * koko major version codes are only added, never renamed or removed.
 */
export interface Error {
  code: string;
  message: string;
  /**
   * Items, in the refusal of a batch of several items, holds each item at
   * fault, in the order of the batch; every other error leaves it out.
   */
  items?: ItemError[];
}

/**
 * ItemError is one item at fault in a refused batch: its index in the
 * batch, counted from 0, with the code and message it would be refused with
 * alone.
 */
export interface ItemError {
  index: number;
  code: string;
  message: string;
}

/** ErrorResponse is the body of a failed call: {"error": {"code": ..., "message": ...}}. */
export interface ErrorResponse {
  error: Error;
}

/** CodeInvalidRequest: the body is not a JSON object of the request's type. */
export const CodeInvalidRequest = "invalid_request";

/**
 * CodeUnknownField: the request holds a field the koko server does not
 * know, so a newer CLI's option is never silently dropped.
 */
export const CodeUnknownField = "unknown_field";

/** CodeUnknownOperation: no operation has this name. */
export const CodeUnknownOperation = "unknown_operation";

/** CodeMethodNotAllowed: every operation is a POST. */
export const CodeMethodNotAllowed = "method_not_allowed";

/** CodeUnsupportedMediaType: every request body is application/json. */
export const CodeUnsupportedMediaType = "unsupported_media_type";

/** CodeInternal: the koko server failed in a way it did not expect; its log says why. */
export const CodeInternal = "internal_error";

/**
 * CodeInvalidName: a name breaks its rule, such as 1 to 63 of a-z, 0-9
 * and '-', with '_' too in a Secret's name (package name).
 */
export const CodeInvalidName = "invalid_name";

/** CodeNameTaken: a record of this kind already holds the name. */
export const CodeNameTaken = "name_taken";

/**
 * CodeNotInitialised: the koko server is not initialised yet, so it
 * answers no operation but server.init.
 */
export const CodeNotInitialised = "not_initialised";

/**
 * CodeLocalOnly: the operation answers only the Local caller, over the
 * koko server's Unix socket.
 */
export const CodeLocalOnly = "local_only";

/**
 * CodeOwnerOnly: only the Owner or an Administrator may do what the
 * call asks, such as issuing a Join Code for another User's device; or
 * only the Owner may, since it acts on the Owner or an Administrator,
 * such as promoting a User or revoking an Administrator's device.
 */
export const CodeOwnerOnly = "owner_only";

/**
 * CodeIdempotencyKeyReused: the caller's Idempotency-Key came with
 * another request within the last 10 minutes; a retry sends the same
 * request with the same key, and a new write a new key.
 */
export const CodeIdempotencyKeyReused = "idempotency_key_reused";

/**
 * CodeRequestTooLarge: the request body is over the koko server's limit
 * (MaxRequestBody).
 */
export const CodeRequestTooLarge = "request_too_large";

/**
 * CodeCLITooOld: the CLI is older than the oldest koko the koko server
 * works with (wire.Report's Oldest), so the koko server answers none of
 * its calls, and the CLI, once it reads the report, sends none; koko
 * update upgrades it.
 */
export const CodeCLITooOld = "cli_too_old";

/**
 * CodeInvalidBatch: the records at fault in a batch, a write that names
 * several records all or none, are at fault for different reasons; the
 * error's items give each its own code (BatchError).
 */
export const CodeInvalidBatch = "invalid_batch";
