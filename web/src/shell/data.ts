import { useEffect, useRef, useState } from "react";

import type { Op } from "../api/api";
import { call, newIdempotencyKey } from "../client";
import { useLocation } from "./router";

// Pages read and write through these hooks. The web UI holds no connection
// open and never polls: a read reloads on each navigation and whenever the
// window regains focus.

/** readers are what loads each read on the page again. */
const readers = new Set<() => void>();

/**
 * reloadReads loads every read on the page again, as the window regaining
 * focus does: for a write that changed records several reads show, such as
 * a batch, whose records the page's counts and its list each read.
 */
export function reloadReads(): void {
  for (const reload of readers) {
    reload();
  }
}

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
    readers.add(regained);
    return () => {
      window.removeEventListener("focus", regained);
      readers.delete(regained);
    };
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

/** Outcome is how one write ended: its response, or why it failed. */
export type Outcome<Resp> = { response: Resp; error?: undefined } | { response?: undefined; error: unknown };

/** Write is what useWrite holds of one form's write. */
export interface Write<Req, Resp> {
  /**
   * submit sends the write, and answers its response, or undefined when it
   * failed, with the reason in error.
   */
  submit: (request: Req) => Promise<Resp | undefined>;
  /**
   * send sends the write as submit does, and answers its outcome, the
   * reason with it when it failed, for a form that acts on a refusal or
   * that may have closed before the answer came.
   */
  send: (request: Req) => Promise<Outcome<Resp>>;
  pending: boolean;
  error?: unknown;
}

/**
 * useWrite is one form's write operation. Submitting the same request again
 * after it failed, as after a dropped connection, reuses its
 * Idempotency-Key, so the koko server applies it once; a write that
 * succeeded, or another request, takes a new key.
 *
 * With credential, the request holds a password or a private key, which
 * the page keeps only until the call is sent: the hook remembers the
 * request only as its SHA-256, salted with bytes of its own, so the same
 * credential given again still reuses the key, and the form clears the
 * credential as it sends it. Where the browser has no SubtleCrypto,
 * outside a secure context, every send takes a new key.
 */
export function useWrite<Req, Resp>(op: Op<Req, Resp>, { credential = false } = {}): Write<Req, Resp> {
  // attempt is the request last sent, or its salted digest, with its key, until it succeeds.
  const attempt = useRef<{ body: string; key: string }>(undefined);
  const salt = useRef<Uint8Array>(undefined);
  const [inFlight, setInFlight] = useState(0);
  const [error, setError] = useState<unknown>();
  const send = async (request: Req): Promise<Outcome<Resp>> => {
    setInFlight((n) => n + 1);
    setError(undefined);
    try {
      const json = JSON.stringify(request);
      salt.current ??= crypto.getRandomValues(new Uint8Array(16));
      const body = credential ? await saltedDigest(salt.current, json) : json;
      if (attempt.current?.body !== body) {
        attempt.current = { body, key: newIdempotencyKey() };
      }
      const { key } = attempt.current;
      const response = await call(op, request, { key });
      if (attempt.current?.key === key) {
        attempt.current = undefined; // not a later request's key
      }
      return { response };
    } catch (failure) {
      setError(failure);
      return { error: failure };
    } finally {
      setInFlight((n) => n - 1);
    }
  };
  const submit = async (request: Req) => (await send(request)).response;
  return { submit, send, pending: inFlight > 0, error };
}

/**
 * saltedDigest is the SHA-256 of salt and text, in hex, by which a write
 * that holds a credential is told apart from the next without being kept;
 * where the browser has no SubtleCrypto it is a new random value, so no two
 * are alike.
 */
async function saltedDigest(salt: Uint8Array, text: string): Promise<string> {
  if (crypto.subtle === undefined) {
    return newIdempotencyKey();
  }
  const bytes = new Uint8Array([...salt, ...new TextEncoder().encode(text)]);
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", bytes));
  return Array.from(digest, (b) => b.toString(16).padStart(2, "0")).join("");
}
