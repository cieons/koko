import { expect, test } from "vitest";

import { bytes, creationOptions, fakeAuthenticator, requestOptions } from "./passkeys.fixture";
import {
  assertion,
  cancelled,
  creationOptions as toCreation,
  fromBase64url,
  passkeysAvailable,
  registration,
  requestOptions as toRequest,
  toBase64url,
} from "./webauthn";

// The page's WebAuthn helper: base64url both ways, koko's options as
// navigator.credentials takes them, and the browser's answers as koko takes
// them.

/** list is the bytes of a buffer or view, to compare. */
function list(b: BufferSource | undefined): number[] {
  if (b === undefined) {
    return [];
  }
  return Array.from(ArrayBuffer.isView(b) ? new Uint8Array(b.buffer, b.byteOffset, b.byteLength) : new Uint8Array(b));
}

test("base64url is unpadded, uses - and _, and reads back with or without padding", () => {
  expect(toBase64url(new Uint8Array(Array.from({ length: 32 }, (_, i) => i)))).toBe(
    "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8",
  );
  expect(toBase64url(new Uint8Array([0xfb, 0xff, 0xbf]))).toBe("-_-_");
  expect(toBase64url(bytes("client data").buffer)).toBe("Y2xpZW50IGRhdGE");
  expect(toBase64url(new Uint8Array([9, 0x61, 0x62, 9]).subarray(1, 3))).toBe("YWI");
  expect(Array.from(fromBase64url("-_-_"))).toEqual([0xfb, 0xff, 0xbf]);
  expect(Array.from(fromBase64url("Y2xpZW50IGRhdGE"))).toEqual(Array.from(bytes("client data")));
  expect(Array.from(fromBase64url("YWI="))).toEqual([0x61, 0x62]);
  expect(Array.from(fromBase64url(""))).toEqual([]);
});

test("koko's options to make a passkey become create()'s, the binary members as bytes and the rest as given", () => {
  const options = toCreation(creationOptions);

  expect(list(options.challenge)).toEqual(Array.from({ length: 32 }, (_, i) => i));
  expect(list(options.user.id)).toEqual(Array.from(bytes("user_2")));
  expect(options.user.name).toBe("bob");
  expect(options.rp).toEqual({ id: "koko.example.com", name: "koko" });
  expect(options.excludeCredentials?.map((d) => [d.type, list(d.id)])).toEqual([
    ["public-key", Array.from(bytes("an earlier passkey"))],
  ]);
  expect(options.pubKeyCredParams.map((p) => p.alg)).toEqual([-8, -7, -257]);
  expect(options.authenticatorSelection).toEqual({
    residentKey: "required",
    requireResidentKey: true,
    userVerification: "required",
  });
  expect(options.attestation).toBe("none");
  expect(options.timeout).toBe(300000);
});

test("koko's options to sign in become get()'s", () => {
  const options = toRequest({ ...requestOptions, allowCredentials: [{ type: "public-key", id: "-_-_" }] });

  expect(list(options.challenge)).toEqual(Array.from({ length: 32 }, (_, i) => i));
  expect(options.rpId).toBe("koko.example.com");
  expect(options.userVerification).toBe("required");
  expect(options.timeout).toBe(300000);
  expect(options.allowCredentials?.map((d) => list(d.id))).toEqual([[0xfb, 0xff, 0xbf]]);
});

test("a new passkey's answer and a sign-in's go to koko in base64url, a missing userHandle as empty", () => {
  const made = {
    id: "bmV3",
    rawId: bytes("new").buffer,
    type: "public-key",
    response: { clientDataJSON: bytes("client data").buffer, attestationObject: new Uint8Array([0xfb, 0xff, 0xbf]).buffer },
  } as unknown as PublicKeyCredential;
  expect(registration(made)).toEqual({
    id: "bmV3",
    rawId: "bmV3",
    type: "public-key",
    response: { clientDataJSON: "Y2xpZW50IGRhdGE", attestationObject: "-_-_" },
  });

  const used = (userHandle: ArrayBuffer | null) =>
    ({
      id: "bmV3",
      rawId: bytes("new").buffer,
      type: "public-key",
      response: {
        clientDataJSON: bytes("client data").buffer,
        authenticatorData: bytes("ab").buffer,
        signature: new Uint8Array([0xfb, 0xff, 0xbf]).buffer,
        userHandle,
      },
    }) as unknown as PublicKeyCredential;
  expect(assertion(used(bytes("ab").buffer))).toEqual({
    id: "bmV3",
    rawId: "bmV3",
    type: "public-key",
    response: { clientDataJSON: "Y2xpZW50IGRhdGE", authenticatorData: "YWI", signature: "-_-_", userHandle: "YWI" },
  });
  expect(assertion(used(null)).response.userHandle).toBe("");
});

test("a ceremony cancelled, timed out or aborted is cancelled; any other failure is not", () => {
  expect(cancelled(new DOMException("", "NotAllowedError"))).toBe(true);
  expect(cancelled(new DOMException("", "AbortError"))).toBe(true);
  expect(cancelled(new DOMException("", "InvalidStateError"))).toBe(false);
  expect(cancelled(new DOMException("", "SecurityError"))).toBe(false);
  expect(cancelled(new Error("NotAllowedError"))).toBe(false);
});

test("passkeys are available only where the browser has PublicKeyCredential", () => {
  expect(passkeysAvailable()).toBe(false);
  fakeAuthenticator();
  expect(passkeysAvailable()).toBe(true);
});
