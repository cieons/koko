import { useEffect, useRef, useState } from "react";

import type { Op } from "../api/api";
import { call, newIdempotencyKey } from "../client";
import { useLocation } from "./router";

// Pages read and write through these hooks. The web UI holds no connection
// open and never polls: a read reloads on each navigation and whenever the
// window regains focus.

/** Read is what useRead holds of one read. */
export interface Read<Resp> {
  /** data is the last answer to this request, kept while it reloads. */
  data?: Resp;
  /** error is why the last load failed; data, if any, is older. */
  error?: unknown;
  /** reload loads it again, as after a write the page made. */
  reload: () => void;
}

/**
 * useRead calls a read operation with request and holds its answer. It
 * loads again when the request changes, on every navigation and when the
 * window regains focus. An answer to an earlier request never shows as this
 * one's. With enabled false it calls nothing and holds nothing, as for a
 * read only the Owner may make, asked for a Member.
 */
export function useRead<Req, Resp>(op: Op<Req, Resp>, request: NoInfer<Req>, { enabled = true } = {}): Read<Resp> {
  const { visit } = useLocation();
  const [focus, setFocus] = useState(0);
  const [reloads, setReloads] = useState(0);
  const [state, setState] = useState<{ body: string; data?: Resp; error?: unknown }>({ body: "" });

  useEffect(() => {
    const regained = () => setFocus((n) => n + 1);
    window.addEventListener("focus", regained);
    return () => window.removeEventListener("focus", regained);
  }, []);

  const body = JSON.stringify(request);
  useEffect(() => {
    if (!enabled) {
      return;
    }
    let latest = true;
    call(op, JSON.parse(body) as Req).then(
      (data) => {
        if (latest) {
          setState({ body, data });
        }
      },
      (error: unknown) => {
        if (latest) {
          setState((s) => ({ body, data: s.body === body ? s.data : undefined, error }));
        }
      },
    );
    return () => {
      latest = false;
    };
  }, [op, body, visit, focus, reloads, enabled]);

  const current = enabled && state.body === body ? state : undefined;
  return { data: current?.data, error: current?.error, reload: () => setReloads((n) => n + 1) };
}

/** Write is what useWrite holds of one form's write. */
export interface Write<Req, Resp> {
  /**
   * submit sends the write, and answers its response, or undefined when it
   * failed, with the reason in error.
   */
  submit: (request: Req) => Promise<Resp | undefined>;
  pending: boolean;
  error?: unknown;
}

/**
 * useWrite is one form's write operation. Submitting the same request again
 * after it failed, as after a dropped connection, reuses its
 * Idempotency-Key, so the koko server applies it once; a write that
 * succeeded, or another request, takes a new key.
 */
export function useWrite<Req, Resp>(op: Op<Req, Resp>): Write<Req, Resp> {
  // attempt is the request last submitted, with its key, until it succeeds.
  const attempt = useRef<{ body: string; key: string }>(undefined);
  const [inFlight, setInFlight] = useState(0);
  const [error, setError] = useState<unknown>();
  const submit = async (request: Req) => {
    const body = JSON.stringify(request);
    if (attempt.current?.body !== body) {
      attempt.current = { body, key: newIdempotencyKey() };
    }
    const { key } = attempt.current;
    setInFlight((n) => n + 1);
    setError(undefined);
    try {
      const response = await call(op, request, { key });
      if (attempt.current?.key === key) {
        attempt.current = undefined; // not a later request's key
      }
      return response;
    } catch (failure) {
      setError(failure);
      return undefined;
    } finally {
      setInFlight((n) => n - 1);
    }
  };
  return { submit, pending: inFlight > 0, error };
}
