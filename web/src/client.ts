import type { Op } from "./api/api";
import type { ErrorResponse } from "./api/errors";
import type { AuthEndpoint } from "./api/signin";

/** ApiError is a call the koko server refused, with its stable error code. */
export class ApiError extends Error {
  readonly code: string;
  /**
   * retryAfter is the refusal's Retry-After header in seconds, when it has
   * one, as too_many_attempts does: how long to wait before trying again.
   */
  readonly retryAfter?: number;

  constructor(code: string, message: string, retryAfter?: number) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.retryAfter = retryAfter;
  }
}

/**
 * DroppedError is a call whose connection dropped, and dropped again on the
 * retry, as when koko web is not running. A write may or may not have been
 * applied; sending it again with the same Idempotency-Key applies it once.
 */
export class DroppedError extends Error {
  constructor(operation: string, cause: unknown) {
    super(`the connection to koko web dropped twice during ${operation}`, { cause });
    this.name = "DroppedError";
  }
}

/**
 * UnreachableError is an /auth/ call (post) whose connection failed: the
 * koko server could not be reached, and it may or may not have acted.
 */
export class UnreachableError extends Error {
  constructor(path: string, cause: unknown) {
    super(`the koko server could not be reached for ${path}`, { cause });
    this.name = "UnreachableError";
  }
}

/**
 * newIdempotencyKey makes the Idempotency-Key of one write: 128 random bits
 * as 32 lowercase hex characters, the form the koko server takes.
 */
export function newIdempotencyKey(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

/** CallOptions are the options of one call. */
export interface CallOptions {
  /**
   * key is a write's Idempotency-Key. A form gives the key of its last
   * attempt to submit the same request again; without one a write gets a
   * new key.
   */
  key?: string;
}

/** Call is how one call ended: its operation, and its response or error. */
export interface Call {
  op: Op<unknown, unknown>;
  response?: unknown;
  error?: unknown;
}

/** CallObserver hears how each call ended. */
export type CallObserver = (call: Call) => void;

const observers = new Set<CallObserver>();

/**
 * observeCalls makes observer hear how every call ends, until the returned
 * function is called. The shell turns a dropped koko web, a Revoked Device
 * Session or an ended cookie into its notices this way.
 */
export function observeCalls(observer: CallObserver): () => void {
  observers.add(observer);
  return () => {
    observers.delete(observer);
  };
}

/**
 * call makes one API call: a JSON POST of request to the operation's path,
 * a write carrying its Idempotency-Key. A call whose connection drops is
 * sent once more, with the same key, so a retried write applies once. It
 * returns the response, or throws an ApiError when the koko server refuses
 * and a DroppedError when the retry drops too.
 */
export async function call<Req, Resp>(
  op: Op<Req, Resp>,
  request: NoInfer<Req>,
  options: CallOptions = {},
): Promise<Resp> {
  try {
    const response = await send(op, request, options);
    notifyObservers({ op, response });
    return response;
  } catch (error) {
    notifyObservers({ op, error });
    throw error;
  }
}

async function send<Req, Resp>(op: Op<Req, Resp>, request: Req, options: CallOptions): Promise<Resp> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (op.write) {
    headers["Idempotency-Key"] = options.key ?? newIdempotencyKey();
  }
  const init: RequestInit = { method: "POST", headers, body: JSON.stringify(request) };
  let answer: { response: Response; body: string };
  try {
    answer = await attempt(op.path, init);
  } catch (error) {
    if (!(error instanceof TypeError)) {
      throw error;
    }
    try {
      answer = await attempt(op.path, init);
    } catch (retryError) {
      throw retryError instanceof TypeError ? new DroppedError(op.name, retryError) : retryError;
    }
  }
  return answered<Resp>(answer);
}

/**
 * answered is the response an answer holds, or the ApiError of a refusal.
 * An answer that is not JSON is unknown: it fails the call, never reads as
 * an empty response.
 */
function answered<Resp>({ response, body }: { response: Response; body: string }): Resp {
  const parsed: unknown = JSON.parse(body);
  if (!response.ok) {
    const { error } = parsed as ErrorResponse;
    const retryAfter = Number.parseInt(response.headers.get("Retry-After") ?? "", 10);
    throw new ApiError(error.code, error.message, Number.isNaN(retryAfter) ? undefined : retryAfter);
  }
  return parsed as Resp;
}

/**
 * post calls one of the web front's /auth/ endpoints on the web listener:
 * a JSON POST of request to its path. It returns the response, or throws
 * an ApiError when the koko server refuses, as call does. It is sent once:
 * an /auth/ call is no API write with an Idempotency-Key, and a Password
 * Link sent again after it was spent would be refused as used. A failed
 * connection throws an UnreachableError.
 */
export async function post<Req, Resp>(endpoint: AuthEndpoint<Req, Resp>, request: NoInfer<Req>): Promise<Resp> {
  let answer: { response: Response; body: string };
  try {
    answer = await attempt(endpoint.path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(request),
    });
  } catch (error) {
    throw error instanceof TypeError ? new UnreachableError(endpoint.path, error) : error;
  }
  return answered<Resp>(answer);
}

/**
 * attempt sends a request once and reads its whole answer, so that a
 * connection that drops before the answer ends fails the attempt with
 * fetch's TypeError, as one that drops before it begins does.
 */
async function attempt(path: string, init: RequestInit): Promise<{ response: Response; body: string }> {
  const response = await fetch(path, init);
  return { response, body: await response.text() };
}

function notifyObservers(call: Call): void {
  for (const observer of observers) {
    observer(call);
  }
}
