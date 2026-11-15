import { act } from "@testing-library/react";
import { onTestFinished, vi } from "vitest";

import {
  AuthPasskeyAdd,
  AuthPasskeyAddBegin,
  AuthPasskeySignIn,
  AuthPasskeySignInBegin,
  type PasskeyCreationOptions,
  type PasskeyRequestOptions,
} from "../../api/passkey";
import { AuthMethods } from "../../api/signin";
import type { User } from "../../api/user";
import type { FakeApi } from "../../test/fakeApi";
import { passkey, type World } from "../space/access.fixture";

// A browser's authenticator and the koko server's passkey ceremonies, for
// the tests of Sign in, a Sign-in Link's page and Account: the fake
// authenticator stands in for navigator.credentials and PublicKeyCredential,
// which jsdom lacks, and answers fixed bytes the tests read back encoded.
// /auth/methods, and a call a test holds until it settles it, serve the
// tests of the Identity Provider's pages too.

/** bytes are text's UTF-8 bytes, as an authenticator's answer carries them. */
export function bytes(text: string): Uint8Array<ArrayBuffer> {
  return new TextEncoder().encode(text);
}

/** base64url is text's bytes, or the bytes given, in base64url without padding, as koko's API carries them. */
export function base64url(text: string | readonly number[]): string {
  const b = typeof text === "string" ? bytes(text) : text;
  return btoa(String.fromCharCode(...b))
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replace(/=+$/, "");
}

/** challenge is the bytes of every ceremony's challenge: 0 to 31. */
export const challenge = Array.from({ length: 32 }, (_, i) => i);

/** creationOptions are the options /auth/passkey/add/begin answers for bob at koko.example.com, excluding one passkey of his. */
export const creationOptions: PasskeyCreationOptions = {
  rp: { id: "koko.example.com", name: "koko" },
  user: { id: base64url("user_2"), name: "bob", displayName: "bob" },
  challenge: base64url(challenge),
  pubKeyCredParams: [
    { type: "public-key", alg: -8 },
    { type: "public-key", alg: -7 },
    { type: "public-key", alg: -257 },
  ],
  timeout: 300000,
  excludeCredentials: [{ type: "public-key", id: base64url("an earlier passkey") }],
  authenticatorSelection: { residentKey: "required", requireResidentKey: true, userVerification: "required" },
  attestation: "none",
};

/** requestOptions are the options /auth/passkey/signin/begin answers at koko.example.com. */
export const requestOptions: PasskeyRequestOptions = {
  challenge: base64url(challenge),
  rpId: "koko.example.com",
  timeout: 300000,
  userVerification: "required",
  allowCredentials: [],
};

/** made is the credential the fake authenticator makes: its answer's members, as bytes. */
const made = {
  id: base64url("new passkey"),
  rawId: bytes("new passkey").buffer,
  type: "public-key",
  response: { clientDataJSON: bytes("client data").buffer, attestationObject: bytes("attestation").buffer },
};

/** used is the credential the fake authenticator signs in with; userHandle is null where the authenticator leaves it out. */
function used(userHandle: ArrayBuffer | null) {
  return {
    id: base64url("bob's passkey"),
    rawId: bytes("bob's passkey").buffer,
    type: "public-key",
    response: {
      clientDataJSON: bytes("client data").buffer,
      authenticatorData: bytes("authenticator data").buffer,
      signature: bytes("signature").buffer,
      userHandle,
    },
  };
}

/** registration is what the page posts of made: every member in base64url. */
export const registration = {
  id: base64url("new passkey"),
  rawId: base64url("new passkey"),
  type: "public-key",
  response: { clientDataJSON: base64url("client data"), attestationObject: base64url("attestation") },
};

/** assertion is what the page posts of the passkey used, with userHandle "" for none. */
export function assertion(userHandle = base64url("user_2")) {
  return {
    id: base64url("bob's passkey"),
    rawId: base64url("bob's passkey"),
    type: "public-key",
    response: {
      clientDataJSON: base64url("client data"),
      authenticatorData: base64url("authenticator data"),
      signature: base64url("signature"),
      userHandle,
    },
  };
}

/** Authenticator is the fake navigator.credentials: each call's options are what the page passed. */
export interface Authenticator {
  create: ReturnType<typeof vi.fn<(options: CredentialCreationOptions) => Promise<unknown>>>;
  get: ReturnType<typeof vi.fn<(options: CredentialRequestOptions) => Promise<unknown>>>;
}

/**
 * fakeAuthenticator gives the browser WebAuthn for one test: a
 * PublicKeyCredential, and navigator.credentials whose create makes made and
 * whose get signs in with bob's passkey, its userHandle bob's id. A test
 * makes either cancel, fail or wait with mockImplementation.
 */
export function fakeAuthenticator(): Authenticator {
  vi.stubGlobal("PublicKeyCredential", class PublicKeyCredential {});
  const create = vi.fn(async (_: CredentialCreationOptions): Promise<unknown> => made);
  const get = vi.fn(async (_: CredentialRequestOptions): Promise<unknown> => used(bytes("user_2").buffer));
  Object.defineProperty(navigator, "credentials", { value: { create, get }, configurable: true });
  onTestFinished(() => {
    Reflect.deleteProperty(navigator, "credentials");
  });
  return { create, get };
}

/** signsInWithoutUserHandle makes a's get answer with no userHandle, as some authenticators do. */
export function signsInWithoutUserHandle(a: Authenticator): void {
  a.get.mockImplementation(async () => used(null));
}

/** cancel is how a browser reports a ceremony the person cancelled or let time out. */
export function cancel(): DOMException {
  return new DOMException("The operation either timed out or was not allowed.", "NotAllowedError");
}

/**
 * serveMethods makes /auth/methods answer whether passkeys work at the Web
 * URL, as the koko server does: false for a host that is an IP address;
 * and the Identity Provider's name, or null for none configured.
 */
export function serveMethods(api: FakeApi, passkeys = true, provider: string | null = null): void {
  api.on(AuthMethods, () => ({ passkeys, provider: provider === null ? null : { name: provider } }));
}

/**
 * serveCeremonies makes the fake API answer /auth/methods with passkeys
 * working, and begin and answer passkey ceremonies
 * as the koko server does for user: signing in, and adding one through a
 * link or from a Sign-in, which adds it to w's passkeys, named name or,
 * through a link or with none, Firefox on Linux.
 */
export function serveCeremonies(api: FakeApi, w: World, user: User): void {
  serveMethods(api);
  api.on(AuthPasskeySignInBegin, () => ({ flow: "flow_signin", options: requestOptions }));
  api.on(AuthPasskeySignIn, () => ({ user }));
  api.on(AuthPasskeyAddBegin, () => ({ flow: "flow_add", options: creationOptions }));
  api.on(AuthPasskeyAdd, ({ name }) => {
    const added = passkey(user.name, name ?? "Firefox on Linux", { added_at: "2026-11-08T09:00:00Z" });
    w.passkeys = [...w.passkeys, added];
    return { user, passkey: added };
  });
}

/** Waiting is a call the test holds: settle answers it, and answered is that answer once given. */
export interface Waiting<T> {
  settle: (answer: T) => void;
  answered: Promise<T>;
}

/** waiting is a Waiting for one call, whose promise the call returns as its answer. */
export function waiting<T>(): Waiting<T> & { promise: Promise<T> } {
  let settle: (answer: T) => void = () => undefined;
  const promise = new Promise<T>((resolve) => (settle = resolve));
  return { settle: (answer) => settle(answer), answered: promise, promise };
}

/**
 * settled settles a held call with answer and waits, inside act, until the
 * page has done all it does with it: the answer itself, then one turn of
 * the event loop, by which every continuation the answer queued has run,
 * a post the page would send among them. It waits for no fixed time.
 */
export async function settled<T>(call: Waiting<T>, answer: T): Promise<void> {
  await act(async () => {
    call.settle(answer);
    await call.answered;
  });
  await act(() => new Promise<void>((resolve) => setTimeout(resolve, 0)));
}
