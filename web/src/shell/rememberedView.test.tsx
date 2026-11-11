import { act, configure, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { type Secret, SecretList } from "../api/secret";
import { alice, bob, host, pick, records, selectButton, service } from "../pages/host/records.fixture";
import { fakeApi } from "../test/fakeApi";
import { actAs, renderAt } from "../test/pages";

// A list remembers its last view in the browser (useRememberedView), for
// each User and each list, against the fake API: the Hosts view, the
// Services view and the Secrets page.

vi.setConfig({ testTimeout: 20_000 });
configure({ asyncUtilTimeout: 5_000 });

const hosts = {
  hosts: [host("web1"), host("db1", { state: "registered" }), host("build1", { space: "dev" })],
  services: [service("nginx", { host: "web1" }), service("postgres", { host: "db1" })],
};
const secrets: Secret[] = [
  { id: "secret_1", space: "ops", name: "token", created_at: "2026-11-04T09:00:00Z", updated_at: "2026-11-05T09:00:00Z" },
  { id: "secret_2", space: "dev", name: "tls-key", created_at: "2026-11-04T09:00:00Z", updated_at: "2026-11-05T09:00:00Z" },
];

/** served answers the Hosts page's and the Secrets page's reads, as user. */
function served(user = alice) {
  const api = fakeApi();
  actAs(api, user);
  records(api, hosts);
  api.on(SecretList, () => ({ secrets }));
  return api;
}

/** where is the path and query the browser is at. */
function where(): string {
  return window.location.pathname + window.location.search;
}

/** hostsShown waits for the Hosts view to list a Space's panel, and is its search's text. */
async function hostsShown(): Promise<string> {
  await screen.findAllByRole("table");
  return screen.getByRole<HTMLInputElement>("searchbox", { name: /^Search/ }).value;
}

/** search types q in the page's search. */
function search(q: string): void {
  fireEvent.change(screen.getByRole("searchbox", { name: /^Search/ }), { target: { value: q } });
}

/** place follows the top bar's link to the place named name. */
function place(name: string): void {
  fireEvent.click(within(screen.getByRole("navigation", { name: "Main" })).getByRole("link", { name }));
}

/** resetView is the Reset view button. */
function resetView(): HTMLButtonElement {
  return screen.getByRole<HTMLButtonElement>("button", { name: "Reset view" });
}

test("a view set, the page left and opened again from the top bar shows it, the URL replaced by it", async () => {
  served();
  renderAt("/hosts");
  expect(await hostsShown()).toBe("");
  expect(resetView().disabled).toBe(true);

  search("web");
  await pick("State", "Managed");
  const view = "/hosts?filter=state%3Dmanaged&q=web";
  expect(where()).toBe(view);

  place("Secrets");
  await screen.findByRole("heading", { level: 1, name: "Secrets" });
  const entries = window.history.length;
  place("Hosts");

  expect(await hostsShown()).toBe("web");
  expect(where()).toBe(view);
  // The top bar's visit is the one entry: the remembered view took its place.
  expect(window.history.length).toBe(entries + 1);
  expect(selectButton("State").textContent).toContain("Managed");
  expect(resetView().disabled).toBe(false);
});

test("a URL with a view shows it over the one remembered, which it replaces, and a reload keeps it", async () => {
  served();
  const first = renderAt("/hosts");
  await hostsShown();
  search("web");
  first.unmount();

  const second = renderAt("/hosts?q=db");
  expect(await hostsShown()).toBe("db");
  second.unmount();

  const reload = renderAt("/hosts?q=db");
  expect(await hostsShown()).toBe("db");
  reload.unmount();

  renderAt("/hosts");
  expect(await hostsShown()).toBe("db");
  expect(where()).toBe("/hosts?q=db");
});

test("Reset view shows the default view and forgets the one remembered, and Back returns to the view before it", async () => {
  served();
  const first = renderAt("/hosts?q=web");
  expect(await hostsShown()).toBe("web");

  fireEvent.click(resetView());
  await waitFor(() => expect(where()).toBe("/hosts"));
  expect(await hostsShown()).toBe("");
  expect(resetView().disabled).toBe(true);
  first.unmount();

  renderAt("/hosts");
  expect(await hostsShown()).toBe("");
  expect(where()).toBe("/hosts");

  act(() => window.history.back());
  await waitFor(() => expect(where()).toBe("/hosts?q=web"));
  expect(await hostsShown()).toBe("web");
});

test("Back to the default view the page showed shows it again, and leaves the view remembered since", async () => {
  served();
  renderAt("/hosts");
  await hostsShown();
  await pick("State", "Registered");
  expect(where()).toBe("/hosts?filter=state%3Dregistered");

  act(() => window.history.back());

  await waitFor(() => expect(where()).toBe("/hosts"));
  await waitFor(() => expect(selectButton("State").textContent).toContain("In use"));
  place("Secrets");
  await screen.findByRole("heading", { level: 1, name: "Secrets" });
  place("Hosts");
  await hostsShown();
  expect(where()).toBe("/hosts?filter=state%3Dregistered");
});

test("a tab that nothing is carried over to shows its own view remembered, and a search carried over wins", async () => {
  served();
  const first = renderAt("/hosts?view=services&q=nginx");
  await screen.findByRole("link", { name: /^nginx/ });
  first.unmount();

  renderAt("/hosts");
  await hostsShown();
  await pick("State", "Registered");
  fireEvent.click(screen.getByRole("tab", { name: /^Services/ }));
  await waitFor(() => expect(where()).toBe("/hosts?view=services&q=nginx"));
  await screen.findByRole("link", { name: /^nginx/ });

  fireEvent.click(screen.getByRole("tab", { name: /^Hosts/ }));
  await waitFor(() => expect(where()).toBe("/hosts?q=nginx"));
  search("web");
  fireEvent.click(screen.getByRole("tab", { name: /^Services/ }));
  await waitFor(() => expect(where()).toBe("/hosts?q=web&view=services"));
});

test("two Users of one browser each keep their own view", async () => {
  const api = served();
  const first = renderAt("/hosts");
  await hostsShown();
  search("web");
  first.unmount();

  actAs(api, bob);
  const second = renderAt("/hosts");
  expect(await hostsShown()).toBe("");
  search("db");
  second.unmount();

  actAs(api, alice);
  renderAt("/hosts");
  expect(await hostsShown()).toBe("web");
});

test("the Services view keeps a view of its own, which /hosts?view=services and /services show", async () => {
  served();
  const first = renderAt("/hosts?view=services&q=nginx");
  await screen.findByRole("link", { name: /^nginx/ });
  first.unmount();

  const hostsView = renderAt("/hosts");
  expect(await hostsShown()).toBe("");
  hostsView.unmount();

  const services = renderAt("/hosts?view=services");
  await screen.findByRole("link", { name: /^nginx/ });
  expect(where()).toBe("/hosts?view=services&q=nginx");
  expect(screen.queryByRole("link", { name: /^postgres/ })).toBeNull();

  fireEvent.click(resetView());
  await screen.findByRole("link", { name: /^postgres/ });
  expect(where()).toBe("/hosts?view=services");
  services.unmount();

  renderAt("/services");
  await waitFor(() => expect(where()).toBe("/hosts?view=services"));
});

test("the Secrets page remembers its search and Space, and Reset view forgets them", async () => {
  served();
  const first = renderAt("/secrets?q=token");
  await screen.findByRole("region", { name: "ops" });
  first.unmount();

  const second = renderAt("/secrets");
  await screen.findByRole("region", { name: "ops" });
  expect(where()).toBe("/secrets?q=token");
  expect(screen.queryByRole("region", { name: "dev" })).toBeNull();

  fireEvent.click(resetView());
  await screen.findByRole("region", { name: "dev" });
  expect(where()).toBe("/secrets");
  second.unmount();

  renderAt("/secrets");
  await screen.findByRole("region", { name: "dev" });
  expect(where()).toBe("/secrets");
});
