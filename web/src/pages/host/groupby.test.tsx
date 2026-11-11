import { configure, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { AttrList } from "../../api/attr";
import { expectAccessible } from "../../test/axe";
import { expectChosen, searchAndChoose } from "../../test/controls";
import { fakeApi } from "../../test/fakeApi";
import { setViewportWidth } from "../../test/media";
import { actAs, renderAt } from "../../test/pages";
import { alice, attributes, cells, host, query, records, service, visibleText } from "./records.fixture";

// The Hosts view's Group by against the fake API (docs/subsystems/record-pages.md,
// "The Hosts list"): each Space's Hosts under a subheading per value of an
// Attribute key, kept in the URL as group= and remembered with the view.

vi.setConfig({ testTimeout: 20_000 });
configure({ asyncUtilTimeout: 5_000 });

const node1 = host("node1", { attributes: { role: "node", price: 9 } });
const node2 = host("node2", { state: "registered", attributes: { role: "node", price: 100 } });
const relay1 = host("relay1", { attributes: { role: "relay-host", price: 10.5 } });
const misc1 = host("misc1", { state: "registered", attributes: { price: 9 } });
const build1 = host("build1", { space: "dev", attributes: { role: "node" } });
const fleet = { hosts: [misc1, node1, node2, relay1, build1], services: [service("nginx", { host: "relay1" })] };

/** served answers the Hosts page's reads, attr.list with the key role in use. */
function served() {
  const api = fakeApi();
  actAs(api, alice);
  records(api, fleet);
  api.on(AttrList, () => ({ attributes: [...attributes, { key: "role", type: "text", preset: false, in_use: true }] }));
  return api;
}

/** subheadings are the Group by subheadings, in order, each its value and count. */
function subheadings(): string[] {
  return screen.queryAllByRole("heading", { level: 3 }).map(visibleText);
}

/** where is the path and query the browser is at. */
function where(): string {
  return window.location.pathname + window.location.search;
}

test("Group by role puts each Host under its value within its Space, those without it last, keeping the sort", async () => {
  served();
  renderAt("/hosts?sort=name.desc");
  await screen.findByRole("table", { name: "ops" });
  await expectChosen("Group by", "None");
  expect(subheadings()).toEqual([]);

  await searchAndChoose("Group by", "rol", "role", ["role"]);

  expect(query()).toEqual([
    ["sort", "name.desc"],
    ["group", "role"],
  ]);
  await screen.findByRole("table", { name: "ops, node" });
  expect(subheadings()).toEqual(["node1", "node2", "relay-host1", "No role1"]);
  expect(cells("dev, node").map((row) => row[0])).toEqual(["build1"]);
  // host.list answered in its order; a subheading keeps it.
  expect(cells("ops, node").map((row) => row[0])).toEqual(["node1", "node2"]);
  expect(cells("ops, relay-host").map((row) => row[0])).toEqual(["relay1nginx"]);
  expect(cells("ops, No role").map((row) => row[0])).toEqual(["misc1"]);
  await expectAccessible();
});

test("a search or a Filter that leaves a subheading no Host hides it", async () => {
  served();
  const first = renderAt("/hosts?group=role");
  await screen.findByRole("table", { name: "ops, node" });

  fireEvent.change(screen.getByRole("searchbox", { name: "Search Hosts" }), { target: { value: "node" } });
  await waitFor(() => expect(subheadings()).toEqual(["node1", "node2"]));
  first.unmount();

  renderAt("/hosts?filter=state%3Dregistered&group=role");
  await screen.findByRole("table", { name: "ops, node" });
  expect(subheadings()).toEqual(["node1", "No role1"]);
  expect(screen.queryByRole("heading", { level: 2, name: /^dev/ })).toBeNull();
});

test("a typed key orders its subheadings by its type: a number by value, an enum in its declared order", async () => {
  const api = served();
  const first = renderAt("/hosts?group=price");
  await screen.findByRole("table", { name: "ops, 9" });
  // The dev Space comes first, its one Host without a price.
  expect(subheadings()).toEqual(["No price1", "92", "10.51", "1001"]);
  expect(cells("ops, 9").map((row) => row[0])).toEqual(["misc1", "node1"]);
  first.unmount();

  records(api, {
    hosts: [
      host("a", { attributes: { billing_cycle: "yearly" } }),
      host("b", { attributes: { billing_cycle: "biennial" } }),
      host("c", { attributes: { billing_cycle: "monthly" } }),
    ],
  });
  renderAt("/hosts?group=billing_cycle");
  await screen.findByRole("table", { name: "ops, monthly" });
  expect(subheadings()).toEqual(["monthly1", "yearly1", "biennial1"]);
});

test("a date orders its subheadings by the calendar", async () => {
  const api = served();
  records(api, {
    hosts: [
      host("a", { attributes: { expires_at: "2027-01-05" } }),
      host("b", { attributes: { expires_at: "2026-12-01" } }),
      host("c"),
    ],
  });
  renderAt("/hosts?group=expires_at");
  await screen.findByRole("table", { name: "ops, 2026-12-01" });
  expect(subheadings()).toEqual(["2026-12-011", "2027-01-051", "No expires_at1"]);
});

test("Group by stays when the page is left and opened again, and Reset view clears it", async () => {
  served();
  const first = renderAt("/hosts?group=role");
  await screen.findByRole("table", { name: "ops, node" });
  first.unmount();

  renderAt("/hosts");
  await screen.findByRole("table", { name: "ops, node" });
  expect(where()).toBe("/hosts?group=role");
  await expectChosen("Group by", "role");

  fireEvent.click(screen.getByRole("button", { name: "Reset view" }));
  await screen.findByRole("table", { name: "ops" });
  expect(where()).toBe("/hosts");
  expect(subheadings()).toEqual([]);
  await expectChosen("Group by", "None");
});

test("the Services view lists one panel, with no Group by, and the tab drops it", async () => {
  served();
  renderAt("/hosts?group=role");
  await screen.findByRole("table", { name: "ops, node" });

  fireEvent.click(screen.getByRole("tab", { name: /^Services/ }));
  await screen.findByRole("table", { name: "Services" });
  expect(query()).toEqual([["view", "services"]]);
  expect(screen.queryByRole("combobox", { name: "Group by" })).toBeNull();
});

test("Group by a field, which is no Attribute key, is named as a part of the URL the list does not take", async () => {
  served();
  renderAt("/hosts?group=state");
  expect(
    await screen.findByText("The address of this page holds state, which the list of Hosts does not take."),
  ).toBeDefined();
});

test("on a phone the same subheadings head the lists", async () => {
  setViewportWidth(390);
  served();
  renderAt("/hosts?group=role");

  const list = await screen.findByRole("list", { name: "ops, relay-host" });
  expect(subheadings()).toEqual(["node1", "node2", "relay-host1", "No role1"]);
  expect(within(list).getAllByRole("link").map(visibleText)).toEqual(["relay1Managedrelay1.example.com"]);
  await expectAccessible();
});
