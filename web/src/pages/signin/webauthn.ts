import { useCallback, useEffect, useRef, useState } from "react";

import type {
  PasskeyAssertion,
  PasskeyCreationOptions,
  PasskeyRegistration,
  PasskeyRequestOptions,
} from "../../api/passkey";
import { AuthMethods } from "../../api/signin";
import { post } from "../../client";

// The page's side of a passkey's ceremonies (docs/subsystems/signin.md,
// "Passkeys"): koko's options and answers carry every binary member in
// base64url, and navigator.credentials takes and gives them as bytes. The
// conversion is koko's own, with no npm package, so that every browser with
// WebAuthn works, not only one with WebAuthn Level 3's JSON methods.

/** passkeysAvailable says the browser makes and uses passkeys: without PublicKeyCredential the page offers no passkey action. */
export function passkeysAvailable(): boolean {
  return typeof window.PublicKeyCredential === "function" && typeof navigator.credentials === "object";
}

/**
 * usePasskeysOffered says whether a page offers passkey actions: the
 * browser has WebAuthn (passkeysAvailable) and /auth/methods answers that
 * passkeys work at the Web URL, whose host is no IP address. It is
 * undefined while /auth/methods has not answered, which a browser without
 * WebAuthn never asks, nor a page that asks with enabled false, as one
 * through koko web, which has no /auth/ endpoints. A failed /auth/methods
 * offers them, since the browser's own refusal still shows.
 */
export function usePasskeysOffered(enabled = true): boolean | undefined {
  const browser = enabled && passkeysAvailable();
  const [server, setServer] = useState<boolean>();
  useEffect(() => {
    if (!browser) {
      return;
    }
    let latest = true;
    post(AuthMethods, {}).then(
      ({ passkeys }) => latest && setServer(passkeys),
      () => latest && setServer(true),
    );
    return () => {
      latest = false;
    };
  }, [browser]);
  return browser ? server : false;
}

/** toBase64url is bytes in base64url without padding, as koko's API carries them. */
export function toBase64url(bytes: ArrayBuffer | ArrayBufferView): string {
  const view = ArrayBuffer.isView(bytes)
    ? new Uint8Array(bytes.buffer, bytes.byteOffset, bytes.byteLength)
    : new Uint8Array(bytes);
  let binary = "";
  for (const b of view) {
    binary += String.fromCharCode(b);
  }
  return btoa(binary).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/, "");
}

/** fromBase64url is the bytes of a base64url string, with or without padding. */
export function fromBase64url(text: string): Uint8Array<ArrayBuffer> {
  const base64 = text.replaceAll("-", "+").replaceAll("_", "/");
  const binary = atob(base64 + "=".repeat((4 - (base64.length % 4)) % 4));
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

/** creationOptions are koko's options to make a passkey, as navigator.credentials.create() takes them: every member as given, the binary ones as bytes. */
export function creationOptions(options: PasskeyCreationOptions): PublicKeyCredentialCreationOptions {
  return {
    ...options,
    challenge: fromBase64url(options.challenge),
    user: { ...options.user, id: fromBase64url(options.user.id) },
    pubKeyCredParams: options.pubKeyCredParams.map((p) => ({ ...p, type: p.type as PublicKeyCredentialType })),
    excludeCredentials: options.excludeCredentials.map((d) => ({
      type: d.type as PublicKeyCredentialType,
      id: fromBase64url(d.id),
    })),
    authenticatorSelection: {
      ...options.authenticatorSelection,
      residentKey: options.authenticatorSelection.residentKey as ResidentKeyRequirement,
      userVerification: options.authenticatorSelection.userVerification as UserVerificationRequirement,
    },
    attestation: options.attestation as AttestationConveyancePreference,
  };
}

/** requestOptions are koko's options to sign in with a passkey, as navigator.credentials.get() takes them. */
export function requestOptions(options: PasskeyRequestOptions): PublicKeyCredentialRequestOptions {
  return {
    ...options,
    challenge: fromBase64url(options.challenge),
    userVerification: options.userVerification as UserVerificationRequirement,
    allowCredentials: options.allowCredentials.map((d) => ({
      type: d.type as PublicKeyCredentialType,
      id: fromBase64url(d.id),
    })),
  };
}

/** registration is a new passkey's answer as koko takes it: its id, and the response's binary members in base64url. */
export function registration(credential: PublicKeyCredential): PasskeyRegistration {
  const response = credential.response as AuthenticatorAttestationResponse;
  return {
    id: credential.id,
    rawId: toBase64url(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: toBase64url(response.clientDataJSON),
      attestationObject: toBase64url(response.attestationObject),
    },
  };
}

/** assertion is a sign-in's answer as koko takes it; a userHandle the authenticator left out is "". */
export function assertion(credential: PublicKeyCredential): PasskeyAssertion {
  const response = credential.response as AuthenticatorAssertionResponse;
  return {
    id: credential.id,
    rawId: toBase64url(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: toBase64url(response.clientDataJSON),
      authenticatorData: toBase64url(response.authenticatorData),
      signature: toBase64url(response.signature),
      userHandle: response.userHandle === null ? "" : toBase64url(response.userHandle),
    },
  };
}

/**
 * NoCredential is what a ceremony throws when the browser answers no
 * credential at all, which WebAuthn allows; the page shows it as a
 * passkey that could not be made or used.
 */
export class NoCredential extends Error {
  constructor() {
    super("the browser answered no credential");
  }
}

/** makePasskey asks the browser to make a passkey with koko's options, and answers it as koko takes it. */
export async function makePasskey(options: PasskeyCreationOptions, signal: AbortSignal): Promise<PasskeyRegistration> {
  const credential = await navigator.credentials.create({ publicKey: creationOptions(options), signal });
  if (credential === null) {
    throw new NoCredential();
  }
  return registration(credential as PublicKeyCredential);
}

/** getPasskey asks the browser for a passkey with koko's options, and answers its assertion as koko takes it. */
export async function getPasskey(options: PasskeyRequestOptions, signal: AbortSignal): Promise<PasskeyAssertion> {
  const credential = await navigator.credentials.get({ publicKey: requestOptions(options), signal });
  if (credential === null) {
    throw new NoCredential();
  }
  return assertion(credential as PublicKeyCredential);
}

/**
 * cancelled says a ceremony ended because the person cancelled it, or let
 * it time out, which browsers report alike as NotAllowedError, or because
 * the page aborted it as it closed (AbortError): the page shows nothing.
 */
export function cancelled(error: unknown): boolean {
  return error instanceof DOMException && (error.name === "NotAllowedError" || error.name === "AbortError");
}

/**
 * Ceremony is one passkey ceremony a page or panel runs: signal aborts the
 * browser's prompt once the page has closed, and open tells, after each
 * wait, whether it still is, so that nothing is sent after it closed.
 */
export interface Ceremony {
  signal: AbortSignal;
  open: () => boolean;
}

/**
 * useCeremony gives a component's ceremonies what tells them it has
 * closed: begin answers a Ceremony while the component is mounted, and
 * undefined once it is not. Its signal is aborted as the component
 * unmounts, which ends the browser's prompt.
 */
export function useCeremony(): () => Ceremony | undefined {
  const page = useRef<AbortController>(undefined);
  useEffect(() => {
    const controller = new AbortController();
    page.current = controller;
    return () => {
      controller.abort();
      page.current = undefined;
    };
  }, []);
  return useCallback(() => {
    const controller = page.current;
    return controller && { signal: controller.signal, open: () => !controller.signal.aborted };
  }, []);
}
