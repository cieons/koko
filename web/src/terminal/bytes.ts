// What the resumable stream's frames and the terminal records share: Go's
// uvarint, and the bytes that arrived and are not yet decoded.

/** maxUvarintLength is the most bytes a uvarint takes: a 64-bit value's. */
const maxUvarintLength = 10;

/**
 * uvarint encodes value, a whole number of at most
 * Number.MAX_SAFE_INTEGER, as Go's binary.AppendUvarint does.
 */
export function uvarint(value: number): number[] {
  const bytes: number[] = [];
  for (; value >= 0x80; value = Math.floor(value / 0x80)) {
    bytes.push((value % 0x80) | 0x80);
  }
  bytes.push(value);
  return bytes;
}

/**
 * readUvarint reads one uvarint from bytes at at, as Go's
 * binary.ReadUvarint does, padded encodings such as 80 00 included: its
 * value and where the next byte is, "short" when bytes end inside it, or
 * "overflow" when it is past 64 bits. A value past
 * Number.MAX_SAFE_INTEGER comes back inexact, and still far above any
 * value the callers accept, so each refuses it by its own limit.
 */
export function readUvarint(bytes: Uint8Array, at: number): { value: number; next: number } | "short" | "overflow" {
  let value = 0;
  for (let i = 0; i < maxUvarintLength; i++) {
    const byte = bytes[at + i];
    if (byte === undefined) {
      return "short";
    }
    if (byte < 0x80) {
      if (i === maxUvarintLength - 1 && byte > 1) {
        return "overflow";
      }
      return { value: value + byte * 2 ** (7 * i), next: at + i + 1 };
    }
    value += (byte & 0x7f) * 2 ** (7 * i);
  }
  return "overflow";
}

/** bytesOf joins head, a frame's or a record's fields, and its body. */
export function bytesOf(head: number[], body?: Uint8Array): Uint8Array<ArrayBuffer> {
  const bytes = new Uint8Array(head.length + (body?.length ?? 0));
  bytes.set(head);
  if (body) {
    bytes.set(body, head.length);
  }
  return bytes;
}

/**
 * Unread holds the bytes that arrived and no decoder has taken yet: a frame
 * may span WebSocket messages, and a record may span frames.
 */
export class Unread {
  private bytes: Uint8Array = new Uint8Array(0);
  private at = 0;

  /** push adds more, which the caller no longer changes, after what is held. */
  push(more: Uint8Array): void {
    if (this.at === this.bytes.length) {
      this.bytes = more;
    } else {
      const rest = this.rest;
      const joined = new Uint8Array(rest.length + more.length);
      joined.set(rest);
      joined.set(more, rest.length);
      this.bytes = joined;
    }
    this.at = 0;
  }

  /** rest is what is held. */
  get rest(): Uint8Array {
    return this.bytes.subarray(this.at);
  }

  /** skip lets go of the first n bytes held. */
  skip(n: number): void {
    this.at += n;
  }
}
