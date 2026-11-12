import { bytesOf, readUvarint, Unread, uvarint } from "./bytes";

// The terminal records: what a web terminal's resumable stream carries each
// way. The Go end is internal/shared/wire/terminal.go, and both ends are
// pinned by its testdata/terminal.golden, which stream.test.ts reads.

/**
 * terminalProtocol is the protocol this page speaks with the koko server:
 * the first messages, the records and the stream's frames as the terminal
 * uses them (wire.TerminalProtocol). The koko server takes only its own.
 */
export const terminalProtocol = 1;

/** maxRecord is the most bytes one record's payload holds. */
export const maxRecord = 32 << 10;

/** maxSide is the most columns, and the most rows, a resize carries; the least is 1. */
export const maxSide = 4096;

/** maxStatus is the largest exit status: a uint32's, as SSH's exit-status is. */
const maxStatus = 0xffff_ffff;

/** The type bytes, a record's first. */
const typeData = 0x00;
const typeResize = 0x01;
const typeExitStatus = 0x02;
const typeExitSignal = 0x03;

/** TerminalRecord is one terminal record. */
export type TerminalRecord =
  /** Bytes typed or pasted, from the page, or the shell's output, from the Gateway. */
  | { type: "data"; data: Uint8Array }
  /** The terminal's size, from the page. */
  | { type: "resize"; columns: number; rows: number }
  /** The shell's exit status, from the Gateway. */
  | { type: "exitStatus"; status: number }
  /** The name of the signal that ended the shell, such as KILL, from the Gateway. */
  | { type: "exitSignal"; signal: string };

/**
 * MalformedRecord is the error for bytes that are no terminal record: an
 * unknown type, a length past maxRecord, a resize out of range, or a payload
 * that its length cuts short or that goes on past its fields. The same error
 * refuses to encode such a record.
 */
export class MalformedRecord extends Error {
  constructor(what: string) {
    super(`malformed terminal record: ${what}`);
    this.name = "MalformedRecord";
  }
}

/** checkSize refuses a terminal size out of range. */
function checkSize(columns: number, rows: number): void {
  const fits = (side: number) => Number.isInteger(side) && side >= 1 && side <= maxSide;
  if (!fits(columns) || !fits(rows)) {
    throw new MalformedRecord(`a resize to ${columns}x${rows}, each side being 1 to ${maxSide}`);
  }
}

/** encodeRecord encodes one record; one the decoder would refuse fails with MalformedRecord. */
export function encodeRecord(record: TerminalRecord): Uint8Array<ArrayBuffer> {
  let type: number;
  let payload: Uint8Array;
  switch (record.type) {
    case "data":
      type = typeData;
      payload = record.data;
      break;
    case "resize":
      checkSize(record.columns, record.rows);
      type = typeResize;
      payload = bytesOf([...uvarint(record.columns), ...uvarint(record.rows)]);
      break;
    case "exitStatus":
      if (!Number.isInteger(record.status) || record.status < 0 || record.status > maxStatus) {
        throw new MalformedRecord(`an exit status of ${record.status}, which no uint32 holds`);
      }
      type = typeExitStatus;
      payload = bytesOf(uvarint(record.status));
      break;
    case "exitSignal":
      type = typeExitSignal;
      payload = new TextEncoder().encode(record.signal);
      break;
  }
  if (payload.length > maxRecord) {
    throw new MalformedRecord(`a payload of ${payload.length} bytes, more than ${maxRecord}`);
  }
  return bytesOf([type, ...uvarint(payload.length)], payload);
}

/** field reads one uvarint, which what names, off a payload at at. */
function field(payload: Uint8Array, at: number, what: string): { value: number; next: number } {
  const read = readUvarint(payload, at);
  if (typeof read === "string") {
    throw new MalformedRecord(`${what} cut short by the record's length, or past a uvarint`);
  }
  return read;
}

/** payloadEnd refuses a payload that goes on past its fields, which end at end. */
function payloadEnd(payload: Uint8Array, end: number): void {
  if (end < payload.length) {
    throw new MalformedRecord(`${payload.length - end} bytes after the payload's fields`);
  }
}

/** RecordReader decodes a stream of terminal records from the bytes pushed into it. */
export class RecordReader {
  private readonly unread = new Unread();

  /** push adds the stream's next bytes, which the caller no longer changes. */
  push(bytes: Uint8Array): void {
    this.unread.push(bytes);
  }

  /**
   * next returns the next whole record, or null until more bytes come.
   * Bytes that are no record fail with MalformedRecord, after which the
   * stream cannot be read further.
   */
  next(): TerminalRecord | null {
    const bytes = this.unread.rest;
    const type = bytes[0];
    if (type === undefined) {
      return null;
    }
    if (type > typeExitSignal) {
      throw new MalformedRecord(`type 0x${type.toString(16)}`);
    }
    const length = readUvarint(bytes, 1);
    if (length === "short") {
      return null;
    }
    if (length === "overflow") {
      throw new MalformedRecord("a length past a uvarint");
    }
    if (length.value > maxRecord) {
      throw new MalformedRecord(`a length of ${length.value}, more than ${maxRecord}`);
    }
    const end = length.next + length.value;
    if (bytes.length < end) {
      return null;
    }
    const payload = bytes.subarray(length.next, end);
    const record = decodePayload(type, payload);
    this.unread.skip(end);
    return record;
  }
}

/** decodePayload reads the payload of a record of the given type. */
function decodePayload(type: number, payload: Uint8Array): TerminalRecord {
  switch (type) {
    case typeResize: {
      const columns = field(payload, 0, "columns");
      const rows = field(payload, columns.next, "rows");
      payloadEnd(payload, rows.next);
      checkSize(columns.value, rows.value);
      return { type: "resize", columns: columns.value, rows: rows.value };
    }
    case typeExitStatus: {
      const status = field(payload, 0, "an exit status");
      payloadEnd(payload, status.next);
      if (status.value > maxStatus) {
        throw new MalformedRecord(`an exit status of ${status.value}, more than a uint32 holds`);
      }
      return { type: "exitStatus", status: status.value };
    }
    case typeExitSignal:
      return { type: "exitSignal", signal: new TextDecoder().decode(payload) };
    default:
      return { type: "data", data: payload };
  }
}
