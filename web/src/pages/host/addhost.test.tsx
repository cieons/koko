import { act, configure, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";

import { HostAdd } from "../../api/host";
import { expectAccessible } from "../../test/axe";
import { clickOutside, expectChosen, searchAndChoose } from "../../test/controls";
import { fakeApi, refusal } from "../../test/fakeApi";
import { setViewportWidth } from "../../test/media";
import { actAs, alerts, renderAt } from "../../test/pages";
import { alice, host, pick, records, writes } from "./records.fixture";

// Add Host on the Hosts page, against the fake API: one host.add with only
// the fields given, Attributes and Note among them, in the caller's Personal
// Space unless another is chosen; then the new Host's page.

// A test here renders the whole page and drives several overlays in turn;
// its limits are ceilings for a slow runner, not waits: each step waits for
// its own outcome, and the first render of a file's page takes longest.
vi.setConfig({ testTimeout: 20_000 });
configure({ asyncUtilTimeout: 5_000 });

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllEnvs();
});

const web1 = host("web1", { space: "ops" });
const db1 = host("db1", { space: "ops", relay: { id: "host_web1", name: "web1" } });

/** openAddHost renders the Hosts page and opens Add Host. */
async function openAddHost(): Promise<HTMLElement> {
  renderAt("/hosts");
  await screen.findByRole("table", { name: "ops" });
  const add = screen.getByRole("button", { name: "Add Host" });
  act(() => add.focus());
  fireEvent.click(add);
  return screen.findByRole("dialog", { name: "Add Host" });
}

/** type types value into the text field named name. */
function type(name: string, value: string): void {
  fireEvent.change(screen.getByRole("textbox", { name }), { target: { value } });
}

test("a Host added with only the fields given, in the caller's Personal Space, and the page moves to it", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, { hosts: [web1] });
  api.on(HostAdd, ({ hosts }) => ({ hosts: hosts.map((h) => host(h.name, { space: h.space ?? "alice", state: "registered" })) }));

  const panel = await openAddHost();
  await waitFor(() => expect(document.activeElement).toBe(within(panel).getByRole("textbox", { name: "Name" })));
  await expectChosen(within(panel).getByRole("combobox", { name: "Space" }), "alice");
  expect((within(panel).getByRole("textbox", { name: "Host Account" }) as HTMLInputElement).value).toBe("root");
  expect((within(panel).getByRole("textbox", { name: "Port" }) as HTMLInputElement).value).toBe("22");
  await expectAccessible();

  type("Name", "web3");
  type("Address", "192.0.2.30");
  fireEvent.click(within(panel).getByRole("button", { name: "Add Host" }));

  await waitFor(() => expect(window.location.pathname).toBe("/hosts/web3"));
  expect(api.requests(HostAdd)).toEqual([
    { hosts: [{ name: "web3", space: "alice", address: "192.0.2.30", account: "root" }] },
  ]);
  expect(await screen.findByText("web3 is recorded.")).toBeDefined();
});

test("a Host added in another Space with its port, Relay, purchase, other Attributes and Note, in one host.add", async () => {
  vi.useFakeTimers({ toFake: ["Date"], shouldAdvanceTime: true });
  vi.setSystemTime(new Date("2026-11-06T20:00:00Z")); // already 2026-11-07 in Shanghai
  vi.stubEnv("TZ", "Asia/Shanghai");
  const api = fakeApi();
  actAs(api, alice);
  records(api, { hosts: [web1, db1] });
  api.on(HostAdd, ({ hosts }) => ({ hosts: hosts.map((h) => host(h.name, { space: h.space ?? "alice" })) }));

  const panel = await openAddHost();
  type("Name", "web3");
  await searchAndChoose(within(panel).getByRole("combobox", { name: "Space" }), "op", "ops");
  type("Address", "web3.example.com");
  type("Host Account", "deploy");
  type("Port", "2222");
  // A Relay is a Managed Host reached directly: web1, never db1, which has one.
  await searchAndChoose("Relay", "w", "web1");
  type("Provider", "hetzner");
  type("Region", "fsn1");
  type("Price", "4.50");
  await searchAndChoose("Currency", "eu", "EUR");
  await pick("Billing cycle", "yearly");
  type("Expires", "+1y");
  expect(screen.getByText("Today this is 2027-11-07.")).toBeDefined();
  fireEvent.click(screen.getByRole("switch", { name: "Renews automatically" }));
  fireEvent.click(screen.getByRole("button", { name: "Add an Attribute" }));
  await searchAndChoose("Attribute key", "cor", "cores");
  type("Value of cores", "8");
  fireEvent.click(screen.getByRole("button", { name: "Add an Attribute" }));
  const key = screen.getAllByRole("combobox", { name: "Attribute key" }).at(-1) as HTMLElement;
  fireEvent.change(key, { target: { value: "rack" } });
  type("Value of rack", "b2");
  type("Note", "Ask ops before a reboot.");
  await expectAccessible();
  fireEvent.click(within(panel).getByRole("button", { name: "Add Host" }));

  await waitFor(() => expect(window.location.pathname).toBe("/hosts/web3"));
  expect(api.requests(HostAdd)).toEqual([
    {
      hosts: [
        {
          name: "web3",
          space: "ops",
          address: "web3.example.com",
          account: "deploy",
          port: 2222,
          relay: "web1",
          attributes: {
            provider: "hetzner",
            region: "fsn1",
            price: 4.5,
            currency: "EUR",
            billing_cycle: "yearly",
            expires_at: "2027-11-07",
            cores: "8",
            rack: "b2",
            auto_renew: "yes",
          },
          note: "Ask ops before a reboot.",
        },
      ],
    },
  ]);
});

test("Add Host checks its fields when submitted, sends nothing at fault, and shows a refusal in place with the form kept", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, { hosts: [web1] });
  api.on(HostAdd, () => {
    throw refusal("name_taken", 409);
  });

  const panel = await openAddHost();
  type("Name", "Web 3");
  type("Port", "70000");
  type("Price", "-1");
  type("Expires", "tomorrow");
  fireEvent.click(within(panel).getByRole("button", { name: "Add Host" }));

  expect(
    await screen.findByText("Use 1 to 63 lowercase letters, digits and hyphens, starting and ending with a letter or a digit."),
  ).toBeDefined();
  expect(document.activeElement).toBe(screen.getByRole("textbox", { name: "Name" }));
  expect(screen.getByText("This field is required.")).toBeDefined();
  expect(screen.getByText("A port is a number from 1 to 65535.")).toBeDefined();
  expect(screen.getByText("This is not a value price takes.")).toBeDefined();
  expect(screen.getByText("tomorrow is not a date. Write a date such as 2026-12-01, today or +30d.")).toBeDefined();
  await expectAccessible();
  expect(writes(api)).toEqual([]);

  type("Name", "web1");
  type("Address", "192.0.2.30");
  type("Port", "22");
  type("Price", "");
  type("Expires", "");
  fireEvent.click(within(panel).getByRole("button", { name: "Add Host" }));
  await waitFor(() => expect(alerts()).toContain("This name is already in use."));
  expect((screen.getByRole("textbox", { name: "Address" }) as HTMLInputElement).value).toBe("192.0.2.30");
  expect(window.location.pathname).toBe("/hosts");
});

test("Cancel closes Add Host, sending nothing, and gives the focus back to Add Host", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, { hosts: [web1] });

  await openAddHost();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add Host" })).toBeNull());
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Add Host" })));
  expect(writes(api)).toEqual([]);
});

test("a click beside Add Host closes it while nothing in it has changed, giving the focus back, but not once something is typed or chosen", async () => {
  const api = fakeApi();
  actAs(api, alice);
  records(api, { hosts: [web1] });

  await openAddHost();
  await clickOutside();
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add Host" })).toBeNull());
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Add Host" })));

  // Typed in.
  fireEvent.click(screen.getByRole("button", { name: "Add Host" }));
  const typed = await screen.findByRole("dialog", { name: "Add Host" });
  type("Name", "web2");
  await clickOutside();
  expect(screen.getByRole("dialog", { name: "Add Host" })).toBe(typed);
  expect(screen.getByRole<HTMLInputElement>("textbox", { name: "Name" }).value).toBe("web2");
  fireEvent.keyDown(typed, { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Add Host" })).toBeNull());

  // Chosen: the Space, whose field keeps the focus the click takes.
  fireEvent.click(screen.getByRole("button", { name: "Add Host" }));
  const chosen = await screen.findByRole("dialog", { name: "Add Host" });
  await searchAndChoose(within(chosen).getByRole("combobox", { name: "Space" }), "ops", "ops");
  await clickOutside();
  expect(screen.getByRole("dialog", { name: "Add Host" })).toBe(chosen);
  expect(screen.queryByRole("listbox")).toBeNull();
  expect(writes(api)).toEqual([]);
});

test("on a phone Add Host comes up from the bottom", async () => {
  setViewportWidth(390);
  const api = fakeApi();
  actAs(api, alice);
  records(api, { hosts: [web1] });

  renderAt("/hosts");
  await screen.findByRole("list", { name: "ops" });
  fireEvent.click(screen.getByRole("button", { name: "Add Host" }));

  const panel = await screen.findByRole("dialog", { name: "Add Host" });
  expect(panel.closest("[data-placement]")?.getAttribute("data-placement")).toBe("bottom");
  await expectAccessible();
});
