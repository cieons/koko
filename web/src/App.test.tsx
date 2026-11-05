import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";

import { SpaceList } from "./api/space";
import { App } from "./App";
import { fakeApi } from "./test/fakeApi";

test("the page lists the Spaces the API answers with", async () => {
  const api = fakeApi();
  api.on(SpaceList, () => ({
    spaces: [
      { id: "space_3f9k2m7q", name: "ops", personal: false },
      { id: "space_8h2j4k6m", name: "web", personal: false },
    ],
  }));

  render(<App />);

  const items = await screen.findAllByRole("listitem");
  expect(items.map((li) => li.textContent)).toEqual(["ops", "web"]);
  expect(api.calls.map((c) => [c.method, c.path, c.request])).toEqual([["POST", "/v1/space.list", {}]]);
});
