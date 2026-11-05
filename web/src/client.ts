import type { Op } from "./api/api";
import type { ErrorResponse } from "./api/errors";

/** ApiError is a call the koko server refused, with its stable error code. */
export class ApiError extends Error {
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.code = code;
  }
}

/**
 * call makes one API call: a JSON POST of request to the operation's path. It
 * returns the response, or throws an ApiError when the koko server refuses.
 */
export async function call<Req, Resp>(op: Op<Req, Resp>, request: NoInfer<Req>): Promise<Resp> {
  const response = await fetch(op.path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });
  const body: unknown = await response.json();
  if (!response.ok) {
    const { error } = body as ErrorResponse;
    throw new ApiError(error.code, error.message);
  }
  return body as Resp;
}
