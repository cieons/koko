import { PowerSearch } from "@astryxdesign/core/PowerSearch";
import { render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";

import type { Translate } from "../../shell/i18n";
import { Providers } from "../../shell/providers";
import { filterConfig, termOf, tokenOf } from "./filter";
import { hostFields } from "./record";
import { attributes } from "./records.fixture";
import { type KeyType, keyTypes, opsFor, readWritten, writeTerm } from "./view";

// The Hosts filter's PowerSearch tokens: every comparison of every type
// round-trips between a token and the term the URL holds, as the CLI writes
// it, a relative date included, kept as written.

/** t names a message by its key's last part, so an operator's label is its op, such as le. */
const t: Translate = (key) => key.split(".").at(-1) ?? key;
const clock = { now: new Date("2026-11-08T12:00:00Z"), timeZone: "UTC" };
const types = keyTypes(hostFields, attributes);

/** values are values of each type, as the CLI writes them, that a key of that type takes. */
const values: Record<KeyType["type"], string[]> = {
  text: ["db1", "a b=c"],
  number: ["10", "-2.5", "0.000001"],
  date: ["2026-12-01", "today", "+30d", "-2w", "+3m", "+1y"],
  enum: ["EUR"],
};

/** keys are a key of each type: a field, or an Attribute key, a Preset's or one in use. */
const keys: Record<KeyType["type"], string> = { text: "cores", number: "port", date: "expires_at", enum: "currency" };

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllEnvs();
});

test("every comparison of every type round-trips between a token and the URL's term", () => {
  for (const [kind, key] of Object.entries(keys) as [KeyType["type"], string][]) {
    const type = types(key);
    expect(type?.type, key).toBe(kind);
    for (const op of opsFor(kind)) {
      for (const value of op === "has" || op === "lacks" ? [undefined] : values[kind]) {
        const written = writeTerm(key, op, value);
        const { term, filter } = readWritten(written, types, clock);
        expect(filter, written).toBeDefined();
        const token = tokenOf(term!, type!);
        expect([token.field, token.operator], written).toEqual([key, op]);
        expect(termOf(token), written).toBe(written);
      }
    }
  }
});

test("a token holds its value in the form PowerSearch edits for its key's type, a date as written", () => {
  const token = (written: string) => {
    const { term } = readWritten(written, types, clock);
    return tokenOf(term!, types(term!.key)!);
  };
  expect(token("price.gt=10").value).toEqual({ type: "float", value: 10 });
  expect(token("currency=EUR").value).toEqual({ type: "enum", value: "EUR" });
  expect(token("region!=fsn1").value).toEqual({ type: "string", value: "fsn1" });
  expect(token("expires_at.le=+30d").value).toEqual({ type: "custom", value: "+30d" });
  expect(token("region.lacks").value).toEqual({ type: "empty" });
  // A field's type is the list's: state is an enum of its states, port a number.
  expect(token("state=retired").value).toEqual({ type: "enum", value: "retired" });
  expect(token("port.ge=1024").value).toEqual({ type: "float", value: 1024 });
});

test("the config offers each field, Preset, key in use and other key a term names, each with its type's comparisons", () => {
  const config = filterConfig(t, "Hosts", hostFields, attributes, types, ["region", "rack"]);
  expect(config.fields.map((f) => [f.key, f.group])).toEqual([
    ...Object.keys(hostFields).map((k) => [k, "fields"]),
    ...attributes.filter((a) => a.preset).map((a) => [a.key, "presets"]),
    ...attributes.filter((a) => !a.preset).map((a) => [a.key, "inUse"]),
    ["rack", "inUse"],
  ]);
  const ops = (key: string) => config.fields.find((f) => f.key === key)?.operators.map((o) => [o.key, o.value.type]);
  expect(ops("price")).toEqual([
    ["eq", "float"],
    ["ne", "float"],
    ["lt", "float"],
    ["le", "float"],
    ["gt", "float"],
    ["ge", "float"],
    ["has", "empty"],
    ["lacks", "empty"],
  ]);
  expect(ops("expires_at")?.map(([, value]) => value)).toEqual([
    "custom",
    "custom",
    "custom",
    "custom",
    "custom",
    "custom",
    "empty",
    "empty",
  ]);
  expect(ops("currency")).toEqual([
    ["eq", "enum"],
    ["ne", "enum"],
    ["has", "empty"],
    ["lacks", "empty"],
  ]);
  expect(ops("rack")).toEqual([
    ["eq", "string"],
    ["ne", "string"],
    ["has", "empty"],
    ["lacks", "empty"],
  ]);
  expect(config.fields.find((f) => f.key === "price")?.operators[0]?.value).toEqual({ type: "float", minValue: 0 });
});

test("a date's token shows a relative date with the date it resolves to today, and keeps it as written", () => {
  vi.useFakeTimers({ toFake: ["Date"], shouldAdvanceTime: true });
  vi.setSystemTime(new Date("2026-11-08T12:00:00Z"));
  vi.stubEnv("TZ", "UTC");
  const config = filterConfig(t, "Hosts", hostFields, attributes, types, []);
  const filters = ["expires_at.le=+30d", "expires_at.ge=2026-12-01"].map((written) => {
    const { term } = readWritten(written, types, clock);
    return tokenOf(term!, types("expires_at")!);
  });

  render(
    <Providers>
      <PowerSearch config={config} filters={filters} onChange={() => {}} label="Filter Hosts" />
    </Providers>,
  );

  expect(screen.getByRole("button", { name: "expires_at: le +30d (2026-12-08)" })).toBeDefined();
  expect(screen.getByRole("button", { name: "expires_at: ge 2026-12-01" })).toBeDefined();
  expect(filters.map(termOf)).toEqual(["expires_at.le=+30d", "expires_at.ge=2026-12-01"]);
});
