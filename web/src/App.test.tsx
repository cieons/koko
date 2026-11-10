import { screen, within } from "@testing-library/react";
import { expect, test } from "vitest";

import { expectAccessible } from "./test/axe";
import { fakeApi } from "./test/fakeApi";
import { actAs, renderAt } from "./test/pages";

test("the web UI opens on the frame, with the User's account in the top bar", async () => {
  const api = fakeApi();
  actAs(api, { id: "user_1", name: "alice", role: "owner", created_at: "2026-11-01T09:00:00Z" });

  // A path with no page, so the frame is all the web UI calls for.
  renderAt("/nowhere");

  expect(await within(screen.getByRole("banner")).findByRole("button", { name: "Account: alice" })).toBeDefined();
  expect(within(screen.getByRole("banner")).getByRole("link", { name: "koko" })).toBeDefined();
  expect(screen.getByRole("main")).toBeDefined();
  expect(screen.getByRole("heading", { level: 1, name: "Page not found" })).toBeDefined();
  expect(api.calls.map((c) => [c.method, c.path, c.request])).toEqual([["POST", "/v1/user.current", {}]]);
  await expectAccessible();
});
