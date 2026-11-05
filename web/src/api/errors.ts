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
