import { vi } from "vitest";

import type { Op } from "../api/api";
import type { ErrorResponse } from "../api/errors";

/** FakeCall is one request the fake API received. */
export interface FakeCall {
  method: string;
  path: string;
  /** The parsed JSON body, or undefined when there was none. */
  request: unknown;
}

/**
 * FakeApi stands in for the koko server's API behind fetch, so a page test
 * runs the web UI's real client. Each operation answers with the handler a
 * test gives it, typed by the generated API types, and every request is
 * recorded in calls.
 */
export class FakeApi {
  readonly calls: FakeCall[] = [];
  readonly #handlers = new Map<string, (request: unknown) => unknown>();

  /** on answers every call of op with what handler returns. */
  on<Req, Resp>(op: Op<Req, Resp>, handler: (request: Req) => Resp): void {
    this.#handlers.set(op.path, (request) => handler(request as Req));
  }

  /** fetch answers one request, as the koko server's API would. */
  readonly fetch = async (path: string, init: RequestInit = {}): Promise<Response> => {
    const body = typeof init.body === "string" ? init.body : "";
    const request: unknown = body === "" ? undefined : JSON.parse(body);
    this.calls.push({ method: init.method ?? "GET", path, request });
    const handler = this.#handlers.get(path);
    if (!handler) {
      const refusal: ErrorResponse = {
        error: { code: "unknown_operation", message: `the fake API has no handler for ${path}` },
      };
      return Response.json(refusal, { status: 404 });
    }
    return Response.json(handler(request));
  };
}

/** fakeApi puts a new FakeApi in place of fetch until the test ends. */
export function fakeApi(): FakeApi {
  const api = new FakeApi();
  vi.stubGlobal("fetch", api.fetch);
  return api;
}
