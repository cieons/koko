import { expect, test } from "vitest";

import cases from "../../../internal/server/signin/testdata/clients.json";
import { catalogues } from "../i18n/catalogues";
import { browserName } from "./browserName";
import type { Translate } from "./i18n";

// In English, browserName labels a browser as the koko server's
// api.ClientLabel does, through the table its TestClientsOfUserAgents runs
// too, so the two labels cannot drift.

const t: Translate = (key, params = {}) =>
  catalogues.en[key].replace(/\{(\w+)\}/g, (placeholder, name: string) =>
    name in params ? String(params[name]) : placeholder,
  );

test.each(cases)("$client", ({ browser, system, client }) => {
  expect(browserName(t, { browser: browser || undefined, system: system || undefined })).toBe(client);
});
