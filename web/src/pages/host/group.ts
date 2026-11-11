import type { KeyType } from "./view";

// Group by: the Hosts view's Hosts of a Space under one subheading per
// value of a key, in the order host.list sorts those values in, and a last
// one for the Hosts without the key.

/** Group is the records that hold one value of the key grouped by, or, with value undefined, none. */
export interface Group<R> {
  value: string | number | undefined;
  records: R[];
}

/**
 * groupBy is records grouped by the value valueOf gives each, as stored:
 * a group per value, in the order compareValues gives values of type, then
 * one of the records without a value. Each group keeps the records in the
 * order they came in.
 */
export function groupBy<R>(
  records: readonly R[],
  valueOf: (record: R) => string | number | undefined,
  type: KeyType,
): Group<R>[] {
  const valued = new Map<string, { value: string | number; records: R[] }>();
  const without: R[] = [];
  for (const record of records) {
    const value = valueOf(record);
    if (value === undefined) {
      without.push(record);
      continue;
    }
    const group = valued.get(String(value)) ?? { value, records: [] };
    group.records.push(record);
    valued.set(String(value), group);
  }
  const groups: Group<R>[] = [...valued.values()].sort((a, b) => compareValues(type, a.value, b.value));
  return without.length > 0 ? [...groups, { value: undefined, records: without }] : groups;
}

/**
 * compareValues compares two values of type, as stored, as the koko server
 * sorts a list (record.Type.Compare): numbers by value, dates by the
 * calendar, an enum's values in their declared order and text in natural
 * order, where runs of digits compare as numbers and the rest byte by byte.
 */
export function compareValues(type: KeyType, a: string | number, b: string | number): number {
  switch (type.type) {
    case "number":
      return Math.sign(Number(a) - Number(b));
    case "enum": {
      const values = type.values ?? [];
      return Math.sign(values.indexOf(String(a)) - values.indexOf(String(b)));
    }
    case "date":
      // A date is written YYYY-MM-DD, so its text sorts by the calendar.
      return compareBytes(utf8(String(a)), utf8(String(b)));
    default:
      return compareNatural(String(a), String(b));
  }
}

/** utf8 is text's UTF-8 bytes, which the koko server compares text by. */
function utf8(text: string): Uint8Array {
  return new TextEncoder().encode(text);
}

/** isDigit tells whether a byte is an ASCII digit. */
function isDigit(c: number | undefined): boolean {
  return c !== undefined && c >= 0x30 && c <= 0x39;
}

/** digitsEnd is where the run of digits that starts at i in s ends. */
function digitsEnd(s: Uint8Array, i: number): number {
  let end = i;
  while (isDigit(s[end])) {
    end++;
  }
  return end;
}

/** compareBytes compares two byte strings byte by byte, a shorter one first when it is a prefix of the other. */
function compareBytes(a: Uint8Array, b: Uint8Array): number {
  for (let i = 0; i < a.length && i < b.length; i++) {
    const c = (a[i] ?? 0) - (b[i] ?? 0);
    if (c !== 0) {
      return Math.sign(c);
    }
  }
  return Math.sign(a.length - b.length);
}

/** compareDigits compares two runs of digits by the whole numbers they write, of any length. */
function compareDigits(a: Uint8Array, b: Uint8Array): number {
  const trim = (s: Uint8Array) => {
    let zeros = 0;
    while (s[zeros] === 0x30) {
      zeros++;
    }
    return s.subarray(zeros);
  };
  const [x, y] = [trim(a), trim(b)];
  return Math.sign(x.length - y.length) || compareBytes(x, y);
}

/**
 * compareNatural compares text in natural order, as the koko server does: a
 * run of ASCII digits against a run of digits by its value, anything else
 * byte by byte, and a shorter text first when one is a prefix of the other.
 * Texts that differ only in leading zeros tie there and are told apart byte
 * by byte, so the order is total.
 */
function compareNatural(text: string, other: string): number {
  const a = utf8(text);
  const b = utf8(other);
  let i = 0;
  let j = 0;
  while (i < a.length && j < b.length) {
    if (isDigit(a[i]) && isDigit(b[j])) {
      const ei = digitsEnd(a, i);
      const ej = digitsEnd(b, j);
      const c = compareDigits(a.subarray(i, ei), b.subarray(j, ej));
      if (c !== 0) {
        return c;
      }
      i = ei;
      j = ej;
      continue;
    }
    const c = Math.sign((a[i] ?? 0) - (b[j] ?? 0));
    if (c !== 0) {
      return c;
    }
    i++;
    j++;
  }
  return Math.sign(a.length - i - (b.length - j)) || compareBytes(a, b);
}
