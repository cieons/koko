import { vi } from "vitest";

import type { Op } from "../api/api";
import type { ErrorResponse, ItemError } from "../api/errors";
import type { AuthEndpoint } from "../api/signin";

/**
 * OpOrEndpoint is what the fake answers: an API operation, or one of the
 * web front's /auth/ endpoints, each typed by its generated request and
 * response types.
 */
export type OpOrEndpoint<Req, Resp> = Op<Req, Resp> | AuthEndpoint<Req, Resp>;

/** FakeCall is one request the fake API received. */
export interface FakeCall {
  method: string;
  path: string;
  /** The parsed JSON body, or undefined when there was none. */
  request: unknown;
  /** The request's headers, by lowercase name. */
  headers: Record<string, string>;
}

/**
 * Refusal is what a handler throws to refuse a call, as the koko server
 * refuses one: the fake answers the error object with the code and status.
 */
class Refusal extends Error {
  readonly code: string;
  readonly status: number;
  readonly retryAfter?: number;
  readonly items?: ItemError[];

  constructor(code: string, status: number, retryAfter?: number, items?: ItemError[]) {
    super(`the fake API refused with ${code}`);
    this.code = code;
    this.status = status;
    this.retryAfter = retryAfter;
    this.items = items;
  }
}

/**
 * refusal makes the Refusal a handler throws to refuse with code; with
 * retryAfter, in seconds, it carries a Retry-After header, as
 * too_many_attempts does.
 */
export function refusal(code: string, status = 400, retryAfter?: number): Refusal {
  return new Refusal(code, status, retryAfter);
}

/**
 * batchRefusal makes the Refusal a handler throws to refuse a batch of
 * several records as the koko server does (ADR 0050): faults names each
 * record at fault by its index in the batch, with the code it would be
 * refused with alone, and the refusal's own code is the one they all share,
 * or else invalid_batch.
 */
export function batchRefusal(faults: Readonly<Record<number, string>>): Refusal {
  const items = Object.entries(faults).map(([index, code]) => ({
    index: Number(index),
    code,
    message: `the fake API refused item ${index} with ${code}`,
  }));
  const codes = new Set(items.map((item) => item.code));
  const [shared] = codes;
  return new Refusal(codes.size === 1 && shared !== undefined ? shared : "invalid_batch", 400, undefined, items);
}

/**
 * FakeApi stands in for the koko server's API behind fetch, so a page test
 * runs the web UI's real client. Each operation answers with the handler a
 * test gives it, typed by the generated API types, and every request is
 * recorded in calls.
 */
export class FakeApi {
  readonly calls: FakeCall[] = [];
  readonly #handlers = new Map<string, (request: unknown) => unknown | Promise<unknown>>();
  #drops = 0;

  /**
   * on answers every call of op with what handler returns, or once the
   * promise it returns settles; a handler that throws a refusal refuses the
   * call.
   */
  on<Req, Resp>(op: OpOrEndpoint<Req, Resp>, handler: (request: Req) => Resp | Promise<Resp>): void {
    this.#handlers.set(op.path, (request) => handler(request as Req));
  }

  /** requests are the parsed bodies of every call of op, in the order they came. */
  requests<Req, Resp>(op: OpOrEndpoint<Req, Resp>): unknown[] {
    return this.calls.filter((c) => c.path === op.path).map((c) => c.request);
  }

  /**
   * drop makes the next times requests, of any operation, fail as a dropped
   * connection does: fetch rejects with a TypeError. Each is still recorded
   * in calls. With no count every request fails, as when koko web stopped,
   * until drop(0).
   */
  drop(times = Infinity): void {
    this.#drops = times;
  }

  /** fetch answers one request, as the koko server's API would. */
  readonly fetch = async (path: string, init: RequestInit = {}): Promise<Response> => {
    const body = typeof init.body === "string" ? init.body : "";
    const request: unknown = body === "" ? undefined : JSON.parse(body);
    const headers = Object.fromEntries(new Headers(init.headers).entries());
    this.calls.push({ method: init.method ?? "GET", path, request, headers });
    if (this.#drops > 0) {
      this.#drops--;
      throw new TypeError("Failed to fetch");
    }
    const handler = this.#handlers.get(path);
    if (!handler) {
      return refuse(new Refusal("unknown_operation", 404), `the fake API has no handler for ${path}`);
    }
    try {
      return Response.json(await handler(request));
    } catch (error) {
      if (error instanceof Refusal) {
        return refuse(error, error.message);
      }
      throw error;
    }
  };
}

function refuse({ code, status, retryAfter, items }: Refusal, message: string): Response {
  const answer: ErrorResponse = { error: { code, message, ...(items && { items }) } };
  const headers = retryAfter === undefined ? undefined : { "Retry-After": String(retryAfter) };
  return Response.json(answer, { status, headers });
}

/** fakeApi puts a new FakeApi in place of fetch until the test ends. */
export function fakeApi(): FakeApi {
  const api = new FakeApi();
  vi.stubGlobal("fetch", api.fetch);
  return api;
}
