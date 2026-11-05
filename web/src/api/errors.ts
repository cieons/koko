// Code generated from internal/shared/api/errors.go by make generate; DO NOT EDIT.

/**
 * Error is the error object of a failed call. Its code is stable: within a
 * koko major version codes are only added, never renamed or removed.
 */
export interface Error {
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

/** CodeInvalidName: a name is not 1 to 63 of a-z, 0-9 and '-' (package name). */
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
 * CodeOwnerOnly: only the Owner may do what the call asks, such as
 * issuing a Join Code for another User's device.
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
