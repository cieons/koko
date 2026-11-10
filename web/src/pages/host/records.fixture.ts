import type { AttrKey } from "../../api/attr";
import type { Host } from "../../api/host";
import type { Service } from "../../api/service";
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
  { key: "currency", type: "enum", values: ["CNY", "EUR", "USD"], preset: true, in_use: true },
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

/** visibleText is the text an element shows, leaving out what it hides, such as a sort key's priority. */
export function visibleText(element: Element): string {
  const shown = element.cloneNode(true) as Element;
  for (const hidden of shown.querySelectorAll("[aria-hidden='true'], [hidden]")) {
    hidden.remove();
  }
  return shown.textContent ?? "";
}

/** writes are the paths of every write the page sent, in order. */
export function writes(api: FakeApi): string[] {
  return api.calls.filter((c) => c.headers["idempotency-key"] !== undefined).map((c) => c.path);
}
