import type { AttrKey, Filter, SortKey } from "../../api/attr";
import { resolveDate } from "./reldate";

// A list's view: its filter terms, sort keys and columns, and whether it
// shows Retired records, kept in the page's URL so a bookmark is a saved
// view. Terms and sort keys are written as the CLI takes them
// (internal/client/cli/filter.go): expires_at.le=+30d, --sort price.desc.
// A relative date stays as written and is resolved before each call.

/** KeyType is the type of a key's values, as attr.list gives it. */
export interface KeyType {
  type: "text" | "number" | "date" | "enum";
  /** values are an enum's values, in their declared order. */
  values?: readonly string[];
  /** min is the least value of a number that has one, such as price's 0. */
  min?: number;
}

/** Fields are the fields a list filters and sorts on, by key, with their types. */
export type Fields = Readonly<Record<string, KeyType>>;

/** FilterOp is a filter term's comparison. */
export type FilterOp = "eq" | "ne" | "lt" | "le" | "gt" | "ge" | "has" | "lacks";

/**
 * opsFor are the comparisons a key of type offers: = and != for every type,
 * the orderings for numbers and dates, and has and lacks for every type.
 */
export function opsFor(type: KeyType["type"]): FilterOp[] {
  const ordered: FilterOp[] = type === "number" || type === "date" ? ["lt", "le", "gt", "ge"] : [];
  return ["eq", "ne", ...ordered, "has", "lacks"];
}

/** takesValue tells whether a term with op compares with a value. */
export function takesValue(op: FilterOp): boolean {
  return op !== "has" && op !== "lacks";
}

/** writeTerm writes a term as the CLI takes it: key=value, key!=value, key.le=value or key.has. */
export function writeTerm(key: string, op: FilterOp, value = ""): string {
  switch (op) {
    case "eq":
      return `${key}=${value}`;
    case "ne":
      return `${key}!=${value}`;
    case "has":
    case "lacks":
      return `${key}.${op}`;
    default:
      return `${key}.${op}=${value}`;
  }
}

/** Term is a filter term as written, read into its parts. */
export interface Term {
  key: string;
  op: FilterOp;
  value?: string;
}

/** readTerm reads a term written as the CLI takes it, or undefined when it is malformed. */
function readTerm(written: string): Term | undefined {
  const eq = written.indexOf("=");
  const left = eq < 0 ? written : written.slice(0, eq);
  const value = eq < 0 ? undefined : written.slice(eq + 1);
  if (value !== undefined && left.endsWith("!")) {
    return value === "" ? undefined : { key: left.slice(0, -1), op: "ne", value };
  }
  const dot = left.lastIndexOf(".");
  if (dot < 0) {
    return value ? { key: left, op: "eq", value } : undefined;
  }
  const key = left.slice(0, dot);
  const op = left.slice(dot + 1);
  if (op === "has" || op === "lacks") {
    return value === undefined ? { key, op } : undefined;
  }
  if (op === "lt" || op === "le" || op === "gt" || op === "ge") {
    return value ? { key, op, value } : undefined;
  }
  return undefined;
}

/**
 * reserved are the names of a Host's and a Service's fields, which no
 * Attribute key may be (package record).
 */
const reserved = new Set([
  "id",
  "name",
  "space",
  "address",
  "port",
  "account",
  "relay",
  "state",
  "host_key",
  "host",
  "ports",
  "attributes",
  "note",
  "created_at",
  "updated_at",
]);

/** attrKey is the Attribute key rule: snake_case, 1 to 63 characters, starting with a letter. */
const attrKey = /^[a-z][a-z0-9_]{0,62}$/;

/** KeyTypes gives the type of each key a list takes, or undefined for a key it does not. */
export type KeyTypes = (key: string) => KeyType | undefined;

/**
 * keyTypes are the keys of a list with fields: each field, and every
 * Attribute key, of its Preset's type, as attr.list answers it, or text. The
 * name of another field, such as the Note's, and a key that breaks the key
 * rule are none of its keys.
 */
export function keyTypes(fields: Fields, attrs: readonly AttrKey[]): KeyTypes {
  const attrTypes = new Map(attrs.map((a) => [a.key, attrType(a)]));
  return (key) => {
    if (Object.hasOwn(fields, key)) {
      return fields[key];
    }
    if (reserved.has(key) || !attrKey.test(key)) {
      return undefined;
    }
    return attrTypes.get(key) ?? { type: "text" };
  };
}

/** attrType is the type of the Attribute key attr.list answered with a. */
export function attrType(a: AttrKey): KeyType {
  const type = a.type === "number" || a.type === "date" || a.type === "enum" ? a.type : "text";
  return { type, values: a.values, min: a.min === undefined ? undefined : Number(a.min) };
}

/** Clock is the browser's clock and time zone, which a relative date resolves in. */
export interface Clock {
  now: Date;
  timeZone: string;
}

/**
 * readValue reads value, as written, as a value of type in the JSON type
 * the API takes it in, or undefined when it is no such value: a number as a
 * JSON number, an exact decimal of at most 15 digits, 6 after the point
 * (ADR 0029); a date, resolved if relative; an enum's value; or one line of
 * free text of at most 1 KiB.
 */
export function readValue(type: KeyType, value: string, clock: Clock): string | number | undefined {
  switch (type.type) {
    case "number": {
      const decimal = /^-?(\d+)(?:\.(\d+))?$/.exec(value);
      if (!decimal) {
        return undefined;
      }
      const whole = (decimal[1] ?? "").replace(/^0+/, "");
      const fraction = (decimal[2] ?? "").replace(/0+$/, "");
      const n = Number(value);
      if (whole.length + fraction.length > 15 || fraction.length > 6 || (type.min !== undefined && n < type.min)) {
        return undefined;
      }
      return n;
    }
    case "date":
      return resolveDate(value, clock.now, clock.timeZone);
    case "enum":
      return type.values?.includes(value) ? value : undefined;
    default:
      // Free text is one line with no control character.
      return value !== "" && new TextEncoder().encode(value).length <= 1024 && !/[\u0000-\u001f\u007f-\u009f]/.test(value)
        ? value
        : undefined;
  }
}

/**
 * readFilter reads a term into the API's Filter, or undefined when the list
 * does not take it: a key it does not filter on, or a value not of the
 * key's type.
 */
function readFilter(term: Term, types: KeyTypes, clock: Clock): Filter | undefined {
  const type = types(term.key);
  if (!type) {
    return undefined;
  }
  if (term.value === undefined) {
    return { key: term.key, op: term.op };
  }
  const value = readValue(type, term.value, clock);
  return value === undefined ? undefined : { key: term.key, op: term.op, value };
}

/** readSortKey reads a sort key written key or key.desc. */
export function readSortKey(written: string): SortKey {
  return written.endsWith(".desc") ? { key: written.slice(0, -".desc".length), desc: true } : { key: written };
}

/** writeSortKey writes a sort key as key or key.desc. */
export function writeSortKey({ key, desc }: SortKey): string {
  return desc ? `${key}.desc` : key;
}

/** View is a list's view, as its URL holds it. */
export interface View {
  /** filters are the terms, as written. */
  filters: string[];
  /** sort are the sort keys, as written. */
  sort: string[];
  /** columns are the keys shown, in order, after the name. */
  columns: string[];
  /** all shows the Retired records too. */
  all: boolean;
}

/**
 * readView reads a list's view from its URL's query: each term a filter=,
 * each sort key a sort=, the columns one columns= of keys separated by
 * commas, and all=1; with no columns=, the columns are defaultColumns.
 */
export function readView(params: URLSearchParams, defaultColumns: readonly string[]): View {
  const columns = params.get("columns");
  return {
    filters: params.getAll("filter"),
    sort: params.getAll("sort"),
    columns: columns === null ? [...defaultColumns] : columns.split(",").filter((c) => c !== ""),
    all: params.get("all") === "1",
  };
}

/** writeView writes a list's view as its URL's query, leaving out what is as by default. */
export function writeView(view: View, defaultColumns: readonly string[]): URLSearchParams {
  const params = new URLSearchParams();
  for (const f of view.filters) {
    params.append("filter", f);
  }
  for (const s of view.sort) {
    params.append("sort", s);
  }
  if (view.columns.join(",") !== defaultColumns.join(",")) {
    params.set("columns", view.columns.join(","));
  }
  if (view.all) {
    params.set("all", "1");
  }
  return params;
}

/** ListQuery is what a view asks a list operation for. */
export interface ListQuery {
  filters?: Filter[];
  sort?: SortKey[];
  all?: boolean;
}

/** ReadTerm is one of a view's terms: as written, its parts, and the API's Filter it reads as. */
export interface ReadTerm {
  written: string;
  /** term is undefined for a malformed term. */
  term?: Term;
  /** filter is undefined for a term the list does not take. */
  filter?: Filter;
}

/**
 * Columns are a list's columns besides those of its keys: fixed, the ones it
 * always shows first, such as a Host's name, and unsorted, the ones it may
 * show but neither filters nor sorts on, such as a Service's ports.
 */
export interface Columns {
  fixed: readonly string[];
  unsorted: readonly string[];
}

/** QueryRead is a view read by readQuery. */
export interface QueryRead {
  query: ListQuery;
  /** terms are the view's terms, read. */
  terms: ReadTerm[];
  /** faults are the parts of the view the list does not take, as written. */
  faults: string[];
}

/**
 * readQuery reads view into what the list operation takes, its relative
 * dates resolved by clock, and lists each part the list does not take: a
 * term, a sort key, or a column that is a fixed one, shown twice, or
 * neither a key of the list's nor one of its unsorted columns. Nothing is
 * to be called while any part is at fault, as the CLI refuses a bad term
 * before any call.
 */
export function readQuery(view: View, types: KeyTypes, clock: Clock, columns: Columns): QueryRead {
  const faults: string[] = [];
  const terms = view.filters.map((written): ReadTerm => {
    const term = readTerm(written);
    return { written, term, filter: term && readFilter(term, types, clock) };
  });
  const filters: Filter[] = [];
  for (const { written, filter } of terms) {
    if (filter) {
      filters.push(filter);
    } else {
      faults.push(written);
    }
  }
  const sort: SortKey[] = [];
  for (const written of view.sort) {
    const key = readSortKey(written);
    if (!types(key.key) || sort.some((k) => k.key === key.key)) {
      faults.push(written);
    } else {
      sort.push(key);
    }
  }
  faults.push(
    ...view.columns.filter(
      (c, i) =>
        columns.fixed.includes(c) || view.columns.indexOf(c) !== i || (!types(c) && !columns.unsorted.includes(c)),
    ),
  );
  const query: ListQuery = {};
  if (filters.length > 0) {
    query.filters = filters;
  }
  if (sort.length > 0) {
    query.sort = sort;
  }
  if (view.all) {
    query.all = true;
  }
  return { query, terms, faults };
}

/**
 * sortBy is the sort after a click on the header of key: a click sorts by
 * key alone, or turns it round when the list is sorted by key alone, and a
 * Shift-click adds key as a further sort key, or turns it round when the
 * list is sorted by it already, like repeated --sort.
 */
export function sortBy(sort: readonly string[], key: string, further: boolean): string[] {
  const keys = sort.map(readSortKey);
  const at = keys.findIndex((k) => k.key === key);
  const turned = (k: SortKey): SortKey => ({ key: k.key, desc: !k.desc });
  if (!further) {
    const only = keys.length === 1 ? keys[0] : undefined;
    return [writeSortKey(only?.key === key ? turned(only) : { key })];
  }
  return at < 0 ? [...sort, key] : keys.map((k, i) => writeSortKey(i === at ? turned(k) : k));
}
