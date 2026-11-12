import { expect, test } from "vitest";

import { routes } from "./routes";

// A page whose body is a table or a list fills the room beside the rail;
// every other page keeps the 76rem column (docs/web-design.md, "The
// frame"). A new list page joins this list on purpose.
test("the pages that fill the room beside the rail are Hosts, Secrets and Access", () => {
  expect(routes.filter((route) => route.fill).map((route) => route.path)).toEqual([
    "/hosts",
    "/secrets",
    "/access",
    "/access/spaces/:name",
    "/access/people/:name",
  ]);
});
