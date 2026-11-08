// A Secret value crosses the API as standard base64 of its bytes (ADR 0028).
// A person types a value as text, and reads one as text when its bytes are
// UTF-8; any other value shows as the base64 it came in, as `koko secret
// show --json` prints it, since no text stands for its bytes.

/** encodeValue is the base64 of text's UTF-8 bytes, as secret.add and secret.set take a value. */
export function encodeValue(text: string): string {
  return toBase64(new TextEncoder().encode(text));
}

/** ShownValue is a revealed value as the page shows it. */
export interface ShownValue {
  /** text is the value as text, or its base64 when its bytes are not UTF-8. */
  text: string;
  /** utf8 tells whether text is the value itself, not its base64. */
  utf8: boolean;
}

/** showValue is how the page shows the value secret.show answered as base64. */
export function showValue(base64: string): ShownValue {
  const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
  try {
    return { text: new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(bytes), utf8: true };
  } catch {
    return { text: base64, utf8: false };
  }
}

function toBase64(bytes: Uint8Array): string {
  // btoa takes a string of one character per byte; a value of up to 64 KiB
  // goes through String.fromCharCode in chunks, so no call takes too many
  // arguments.
  let binary = "";
  for (let i = 0; i < bytes.length; i += 0x2000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x2000));
  }
  return btoa(binary);
}
