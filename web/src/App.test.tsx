import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";

import { SpaceList } from "./api/space";
import { UserCurrent } from "./api/user";
import { App } from "./App";
import { fakeApi } from "./test/fakeApi";

test("the web UI opens on the Spaces page, which lists the Spaces the API answers with", async () => {
  const api = fakeApi();
  api.on(UserCurrent, () => ({
    user: { id: "user_1", name: "alice", role: "owner", created_at: "2026-11-01T09:00:00Z" },
  }));
  api.on(SpaceList, () => ({
    spaces: [
      { id: "space_3f9k2m7q", name: "ops", personal: false },
      { id: "space_8h2j4k6m", name: "web", personal: false },
    ],
  }));

  render(<App />);

  const items = await screen.findAllByRole("listitem");
  expect(items.map((li) => li.textContent)).toEqual(["ops", "web"]);
  expect(window.location.pathname).toBe("/spaces");
  expect(api.calls.map((c) => [c.method, c.path, c.request])).toEqual([
    ["POST", "/v1/user.current", {}],
    ["POST", "/v1/space.list", {}],
  ]);
});
