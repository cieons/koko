import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect } from "vitest";

import { type AttrKey, AttrList } from "../../api/attr";
import { type Host, HostList } from "../../api/host";
import { type Service, ServiceList } from "../../api/service";
import { type Space, SpaceList } from "../../api/space";
import type { User } from "../../api/user";
import type { FakeApi } from "../../test/fakeApi";

// What the record's page tests share: sample Users, Attribute keys, Hosts
// and Services, and what reads the URL and the calls a page made.

export const alice: User = { id: "user_1", name: "alice", role: "owner", created_at: "2026-11-01T09:00:00Z" };
export const bob: User = { id: "user_2", name: "bob", role: "member", created_at: "2026-11-02T09:00:00Z" };

/** attributes are what attr.list answers: the Presets, then a key in use. */
export const attributes: AttrKey[] = [
  { key: "provider", type: "text", preset: true, in_use: true },
  { key: "provider_account", type: "text", preset: true, in_use: false },
  { key: "region", type: "text", preset: true, in_use: true },
  { key: "price", type: "number", min: 0, preset: true, in_use: true },
  {
    key: "currency",
    type: "enum",
    values: ["AUD", "CAD", "CHF", "CNY", "EUR", "GBP", "HKD", "JPY", "SGD", "USD"],
    preset: true,
    in_use: true,
  },
  {
    key: "billing_cycle",
    type: "enum",
    values: ["monthly", "quarterly", "semiannual", "yearly", "biennial", "triennial"],
    preset: true,
    in_use: false,
  },
  { key: "expires_at", type: "date", preset: true, in_use: true },
  { key: "auto_renew", type: "enum", values: ["no", "yes"], preset: true, in_use: false },
  { key: "cores", type: "text", preset: false, in_use: true },
];

export function host(name: string, fields: Partial<Host> = {}): Host {
  return {
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
    ...fields,
  };
}

export function service(name: string, fields: Partial<Service> = {}): Service {
  return {
    id: `service_${name}`,
    name,
    host: "web1",
    ports: [],
    attributes: {},
    created_at: "2026-11-01T09:00:00Z",
    updated_at: "2026-11-01T09:00:00Z",
    ...fields,
  };
}

/** query is the page's URL query, decoded, as name=value pairs. */
export function query(): string[][] {
  return [...new URLSearchParams(window.location.search).entries()];
}

/** lastRequest is the last request of op. */
export function lastRequest(api: FakeApi, op: Parameters<FakeApi["requests"]>[0]): unknown {
  return api.requests(op).at(-1);
}

/** visibleText is the text an element shows, leaving out what it hides from sight or from a screen reader. */
export function visibleText(element: Element): string {
  const shown = element.cloneNode(true) as Element;
  for (const hidden of shown.querySelectorAll("[aria-hidden='true'], [hidden], .sr-only")) {
    hidden.remove();
  }
  return shown.textContent ?? "";
}

/** writes are the paths of every write the page sent, in order. */
export function writes(api: FakeApi): string[] {
  return api.calls.filter((c) => c.headers["idempotency-key"] !== undefined).map((c) => c.path);
}

/** spaces are what space.list answers alice: her Personal Space, then ops and dev. */
export const spaces: Space[] = [
  { id: "space_1", name: "alice", personal: true },
  { id: "space_2", name: "ops", personal: false },
  { id: "space_3", name: "dev", personal: false },
];

/** Records are what a list page's fake API answers with. */
export interface Records {
  hosts: Host[];
  services?: Service[];
  spaces?: Space[];
}

/**
 * records answers attr.list, space.list, host.list with the Hosts given, the
 * Retired ones only under all, and service.list with the Services given.
 * host.list applies a term state= or space= alone, and neither list sorts:
 * a test reads what was asked.
 */
export function records(api: FakeApi, { hosts, services = [], spaces: listed = spaces }: Records): void {
  api.on(AttrList, () => ({ attributes }));
  api.on(SpaceList, () => ({ spaces: listed }));
  api.on(HostList, ({ all, filters = [] }) => ({
    hosts: hosts.filter(
      (h) =>
        (all || h.state !== "retired") &&
        filters.every((f) => f.op !== "eq" || (f.key !== "state" && f.key !== "space") || h[f.key] === f.value),
    ),
  }));
  api.on(ServiceList, () => ({ services }));
}

/** listRequests are the host.list requests the page made for its view, leaving out those for its counts of every Host. */
export function listRequests(api: FakeApi): unknown[] {
  return api.requests(HostList).filter((r) => JSON.stringify(r) !== '{"all":true}');
}

/** cells are the text of each cell of each row of the table named name, its header row aside, the name first. */
export function cells(name: string): string[][] {
  return within(screen.getByRole("table", { name }))
    .getAllByRole("row")
    .slice(1)
    .map((row) => [...row.querySelectorAll("th, td")].map((cell) => visibleText(cell)));
}

/** headers are the text of the headers of the table named name. */
export function headers(name: string): string[] {
  return within(screen.getByRole("table", { name }))
    .getAllByRole("columnheader")
    .map((h) => visibleText(h));
}

/** selectButton is the button of the select named label, which opens its list. */
export function selectButton(label: string): HTMLElement {
  const button = screen
    .getAllByRole("button", { name: new RegExp(`\\b${label}\\b`) })
    .find((b) => b.getAttribute("aria-haspopup") === "listbox");
  if (!button) {
    throw new Error(`no select named ${label}`);
  }
  return button;
}

/** pick opens the select named label and chooses its option named option. */
export async function pick(label: string, option: string): Promise<void> {
  fireEvent.click(selectButton(label));
  const listbox = await screen.findByRole("listbox");
  fireEvent.click(within(listbox).getByRole("option", { name: option }));
  await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
}

/** chips are the names of the buttons that take each of the view's terms away, as their chips show the terms. */
export function chips(): string[] {
  const list = screen.queryByRole("list", { name: "Filters" });
  return list === null
    ? []
    : within(list)
        .getAllByRole("listitem")
        .map((item) => item.textContent ?? "");
}
