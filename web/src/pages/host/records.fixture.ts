import { fireEvent, screen, within } from "@testing-library/react";
import { onTestFinished, vi } from "vitest";

import type { AttrKey } from "../../api/attr";
import type { Host } from "../../api/host";
import type { Service } from "../../api/service";
import type { User } from "../../api/user";
import type { FakeApi } from "../../test/fakeApi";

// What the record's page tests share: sample Users, Attribute keys, Hosts
// and Services, and the steps a person takes in PowerSearch and the column
// options.

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

/**
 * addFilter adds a term in the PowerSearch named filter, as a person does:
 * typing the key, choosing its comparison, then giving the value, if it
 * takes one, and applying it. A value is typed, or for an enum chosen.
 */
export async function addFilter(filter: string, key: string, op: string, value?: string): Promise<void> {
  const input = await screen.findByRole("combobox", { name: filter });
  fireEvent.focus(input);
  fireEvent.change(input, { target: { value: key } });
  fireEvent.click(await screen.findByRole("option", { name: `${key} ${op}` }));
  // A comparison that takes no value, has or lacks, adds its token at once.
  if (value === undefined) {
    return;
  }
  const apply = await screen.findByRole("button", { name: "Apply" });
  const field =
    screen.queryByRole("textbox", { name: "Date" }) ??
    screen.queryByRole("textbox", { name: "Value" }) ??
    screen.queryByRole("spinbutton", { name: "Value" });
  if (field) {
    // A number commits as it leaves its field.
    fireEvent.change(field, { target: { value } });
    fireEvent.blur(field);
  } else {
    fireEvent.click(enumValue());
    fireEvent.click(await screen.findByRole("option", { name: value }));
  }
  fireEvent.click(apply);
}

/** visibleText is the text an element shows, leaving out what it hides, such as a sort key's priority. */
export function visibleText(element: Element): string {
  const shown = element.cloneNode(true) as Element;
  for (const hidden of shown.querySelectorAll("[aria-hidden='true'], [hidden]")) {
    hidden.remove();
  }
  return shown.textContent ?? "";
}

/** enumValue is the selector of an enum's value in PowerSearch's editor. */
export function enumValue(): HTMLElement {
  const found = screen.getAllByRole("combobox").find((c) => c.textContent === "Select…");
  if (!found) {
    throw new Error("PowerSearch's editor shows no selector of an enum's value");
  }
  return found;
}

/** offered are the keys and comparisons PowerSearch offers once key is typed. */
export async function offered(filter: string, key: string): Promise<string[]> {
  const input = await screen.findByRole("combobox", { name: filter });
  fireEvent.focus(input);
  fireEvent.change(input, { target: { value: key } });
  await screen.findAllByRole("option");
  const options = screen.getAllByRole("option").map((o) => o.textContent ?? "");
  fireEvent.change(input, { target: { value: "" } });
  return options;
}

/** tokens are the accessible names of PowerSearch's tokens, each a term. */
export function tokens(filter: string): string[] {
  return within(screen.getByRole("group", { name: filter }))
    .queryAllByRole("button")
    .map((b) => b.getAttribute("aria-label") ?? "")
    .filter((label) => label !== "" && !label.startsWith("Remove ") && label !== "Clear all");
}

/** openColumns opens a list's column options, and is them. */
export async function openColumns(): Promise<HTMLElement> {
  fireEvent.click(screen.getByRole("button", { name: "Columns" }));
  return screen.findByRole("dialog", { name: "Columns" });
}

/** clipboard stands in for the browser's clipboard for one test. */
export function clipboard() {
  const writeText = vi.fn((_: string) => Promise.resolve());
  Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
  onTestFinished(() => {
    Reflect.deleteProperty(navigator, "clipboard");
  });
  return writeText;
}

/** writes are the paths of every write the page sent, in order. */
export function writes(api: FakeApi): string[] {
  return api.calls.filter((c) => c.headers["idempotency-key"] !== undefined).map((c) => c.path);
}

/** metadata are the key and value of each item of the metadata list headed title. */
export function metadata(title: string): string[][] {
  const heading = screen.getByRole("heading", { name: title, level: 2 });
  const list = heading.closest(".astryx-metadata-list") ?? heading.parentElement?.parentElement;
  if (!list) {
    throw new Error(`no metadata list is headed ${title}`);
  }
  return within(list as HTMLElement)
    .getAllByRole("term")
    .map((dt) => [dt.textContent ?? "", dt.nextElementSibling?.textContent ?? ""]);
}

/** actions are the labels of the buttons in a record page's summary header, its actions. */
export function actions(): string[] {
  const heading = screen.getByRole("heading", { level: 1 });
  const header = heading.closest(".astryx-layout-header");
  if (!header) {
    throw new Error("the page's heading is in no summary header");
  }
  return within(header as HTMLElement)
    .queryAllByRole("button")
    .map((b) => b.textContent ?? "");
}
