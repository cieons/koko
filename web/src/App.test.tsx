import { screen, within } from "@testing-library/react";
import { expect, test } from "vitest";

import { AttrList } from "./api/attr";
import { HostList } from "./api/host";
import { UserCurrent } from "./api/user";
import { fakeApi } from "./test/fakeApi";
import { renderAt } from "./test/pages";

test("the web UI opens on the Hosts page, which lists the Hosts the API answers with", async () => {
  const api = fakeApi();
  api.on(UserCurrent, () => ({
    user: { id: "user_1", name: "alice", role: "owner", created_at: "2026-11-01T09:00:00Z" },
  }));
  api.on(AttrList, () => ({ attributes: [] }));
  api.on(HostList, () => ({
    hosts: ["db1", "web1"].map((name) => ({
      id: `host_${name}`,
      name,
      space: "ops",
      address: `${name}.example.com`,
      port: 22,
      account: "deploy",
      relay: null,
      state: "managed",
      host_key: null,
      attributes: {},
      created_at: "2026-11-01T09:00:00Z",
      updated_at: "2026-11-01T09:00:00Z",
    })),
  }));

  renderAt("/");

  const table = await screen.findByRole("table", { name: "Hosts" });
  expect(within(table).getAllByRole("link").map((a) => a.textContent)).toEqual(["db1", "web1"]);
  expect(window.location.pathname).toBe("/hosts");
  expect(api.calls.map((c) => [c.method, c.path, c.request])).toEqual([
    ["POST", "/v1/user.current", {}],
    ["POST", "/v1/attr.list", {}],
    ["POST", "/v1/host.list", {}],
  ]);
});
