// Code generated from internal/shared/api/attr.go by make generate; DO NOT EDIT.

import type { Op } from "./api";

/**
 * Filter is one term of a list's filter. A list returns the records that
 * match every term: terms are ANDed. A term compares by the key's type
 * (package record): numbers by value, dates by the calendar, enum values in
 * their declared order, and text in natural order, case-sensitive. A
 * record that lacks the key matches only ne and lacks.
 */
export interface Filter {
  /**
   * Key is a field the list filters on, such as space or state, or an
   * Attribute key.
   */
  key: string;
  /** Op is eq, ne, lt, le, gt, ge, has or lacks (FilterEq to FilterLacks). */
  op: string;
  /**
   * Value is what the term compares with, in the JSON type of the key's
   * values: a number as a JSON number, and every other value, a date
   * always written YYYY-MM-DD, as a JSON string. FilterHas and FilterLacks
   * take none.
   */
  value?: string | number;
}

/**
 * SortKey is one key a list sorts by: a field the list filters on or an
 * Attribute key, compared as Filter compares it. A record that lacks the key
 * sorts last in either direction.
 */
export interface SortKey {
  key: string;
  /** Desc sorts from the greatest value down. */
  desc?: boolean;
}

/** AttrListRequest asks for the Attribute keys koko attr ls prints. */
export type AttrListRequest = Record<string, never>;

/** AttrKey is one Attribute key: a Preset, or a key in use. */
export interface AttrKey {
  key: string;
  /** Type is the kind of the key's values: text, number, date or enum. */
  type: string;
  /**
   * Values are an enum's values in their declared order, which is the
   * order they sort in; left out for the other types.
   */
  values?: string[];
  /**
   * Min is the least value of a number that has one, such as price's 0;
   * left out for any other key.
   */
  min?: string | number;
  /** Preset tells whether the key is a Preset Attribute's. */
  preset: boolean;
  /**
   * InUse tells whether a Host or a Service the caller can see has the
   * key.
   */
  in_use: boolean;
}

/**
 * AttrListResponse is every Preset, in the order of the Preset table, then
 * every other key in use on the Hosts and Services the caller can see, by
 * key.
 */
export interface AttrListResponse {
  attributes: AttrKey[];
}

/**
 * AttrList lists the Preset Attributes with their types, and every other
 * Attribute key in use on what the caller can see: in a Space the caller
 * cannot act in, a key is answered as if nothing held it.
 */
export const AttrList: Op<AttrListRequest, AttrListResponse> = {
  name: "attr.list",
  path: "/v1/attr.list",
  write: false,
};
