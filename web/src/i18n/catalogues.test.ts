import { expect, test } from "vitest";

import { catalogues, languages } from "./catalogues";

// The completeness test: every catalogue file, as written, against its twin
// in the other language and against the API's registered error codes, which
// the generated files in web/src/api declare as Code... constants.

type Messages = Record<string, string>;

const files: Record<string, Record<string, Messages>> = {
  en: byArea(import.meta.glob<Messages>("./en/*.ts", { eager: true, import: "default" })),
  "zh-CN": byArea(import.meta.glob<Messages>("./zh-CN/*.ts", { eager: true, import: "default" })),
};

const codes: Record<string, string[]> = Object.fromEntries(
  Object.entries(byArea(import.meta.glob<Record<string, unknown>>("../api/*.ts", { eager: true }))).map(
    ([area, module]) => [
      area,
      Object.entries(module).flatMap(([name, value]) =>
        name.startsWith("Code") && typeof value === "string" ? [value] : [],
      ),
    ],
  ),
);

/** byArea keys a glob's modules by file name: ./en/space.ts is space. */
function byArea<T>(modules: Record<string, T>): Record<string, T> {
  return Object.fromEntries(
    Object.entries(modules).map(([path, module]) => [path.replace(/^.*\//, "").replace(/\.ts$/, ""), module]),
  );
}

function keys(messages: Messages | undefined): string[] {
  return Object.keys(messages ?? {}).sort();
}

test("the generated files declare the registered error codes", () => {
  expect(codes["errors"]).toContain("internal_error");
  expect(codes["device"]).toContain("revoked");
});

test("both languages have a catalogue file for the same areas", () => {
  expect(Object.keys(files["zh-CN"] ?? {}).sort()).toEqual(Object.keys(files["en"] ?? {}).sort());
});

test("each area's catalogues hold the same keys in both languages, none empty", () => {
  for (const area of Object.keys(files["en"] ?? {})) {
    const en = keys(files["en"]?.[area]);
    const zh = keys(files["zh-CN"]?.[area]);
    expect({ area, missingInZhCN: en.filter((k) => !zh.includes(k)), missingInEn: zh.filter((k) => !en.includes(k)) }).toEqual({
      area,
      missingInZhCN: [],
      missingInEn: [],
    });
  }
  for (const language of languages) {
    for (const [area, messages] of Object.entries(files[language] ?? {})) {
      const empty = Object.entries(messages).flatMap(([key, message]) => (message.trim() === "" ? [key] : []));
      expect({ language, area, empty }).toEqual({ language, area, empty: [] });
    }
  }
});

test("every registered API error code has a message in its area's catalogue, in each language", () => {
  for (const language of languages) {
    for (const [area, areaCodes] of Object.entries(codes)) {
      const messages = files[language]?.[area] ?? {};
      const missing = areaCodes.filter((code) => !(`error.${code}` in messages));
      expect({ file: `web/src/i18n/${language}/${area}.ts`, missing }).toEqual({
        file: `web/src/i18n/${language}/${area}.ts`,
        missing: [],
      });
    }
  }
});

test("an area's catalogue holds only its own keys: <area>.* and the messages for its codes", () => {
  for (const language of languages) {
    for (const [area, messages] of Object.entries(files[language] ?? {})) {
      const foreign = Object.keys(messages).filter(
        (key) => !key.startsWith(`${area}.`) && !(codes[area] ?? []).some((code) => key === `error.${code}`),
      );
      expect({ language, area, foreign }).toEqual({ language, area, foreign: [] });
    }
  }
});

test("each language's catalogue joins every one of its area files", () => {
  for (const language of languages) {
    expect(catalogues[language]).toEqual(Object.assign({}, ...Object.values(files[language] ?? {})));
  }
});
