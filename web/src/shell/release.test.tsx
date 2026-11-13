import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { expect, onTestFinished, test, vi } from "vitest";

import {
  type LastUpgrade,
  type Server,
  ServerCheckUpdate,
  type ServerCheckUpdateResponse,
  ServerShow,
  ServerUpgrade,
} from "../api/server";
import { type User, UserCurrent } from "../api/user";
import { expectAccessible } from "../test/axe";
import { clipboard } from "../test/clipboard";
import { type FakeApi, fakeApi, refusal } from "../test/fakeApi";
import { alerts, frameButton } from "../test/pages";
import { Page as PageHead } from "../ui/Page";
import { reloadAfter } from "./release";
import { reloadPage } from "./reloadPage";
import type { Route } from "./router";
import { Shell } from "./Shell";

vi.mock("./reloadPage", () => ({ reloadPage: vi.fn() }));

/** upgradePoll is how often the page asks server.show while the koko server upgrades. */
const upgradePoll = 2000;

/** upgradeWait is how long the page follows an upgrade before it says where to look. */
const upgradeWait = 15 * 60 * 1000;

// What the shell tells the Owner and the Administrators of koko's releases: the notice above every
// page, the koko server's line in the account panel, and Check now in both,
// against the fake API.

const alice: User = { id: "user_1", name: "alice", role: "owner", created_at: "2026-11-01T09:00:00Z" };
const bob: User = { id: "user_2", name: "bob", role: "member", created_at: "2026-11-02T09:00:00Z" };
const erin: User = { id: "user_5", name: "erin", role: "administrator", created_at: "2026-11-02T10:00:00Z" };

function HostsPage() {
  return (
    <PageHead title="Hosts">
      <p>hosts page</p>
    </PageHead>
  );
}

const routes: Route[] = [{ path: "/hosts", page: HostsPage, nav: "shell.place.hosts" }];

/** server is the koko server server.show answers: v0.4.2, installed with systemd, which knows of no release yet. */
function server(changes: Partial<Server> = {}): Server {
  return {
    address: "koko.example.com",
    listen_port: 7822,
    host_key_fingerprint: "SHA256:fingerprint",
    host_key: "ssh-ed25519 AAAA",
    version: "v0.4.2",
    root_key_file: "/etc/koko/root.key",
    root_key_id: "rk_1",
    data_dir: "/var/lib/koko",
    web_url: null,
    web_listen: null,
    update_check: { enabled: true, checked_at: null, latest: null, error: null },
    upgrade_way: "systemd",
    last_upgrade: null,
    ...changes,
  };
}

/** found is an update_check that found latest, checked an hour ago. */
function found(latest: string): Server["update_check"] {
  return { enabled: true, checked_at: new Date(Date.now() - 3_600_000).toISOString(), latest, error: null };
}

/** serve makes user.current answer user over the Unix socket and server.show answer what current holds at each call. */
function serve(api: FakeApi, user: User, current: { server: Server }): void {
  api.on(UserCurrent, () => ({ user }));
  api.on(ServerShow, () => ({ server: current.server }));
}

function renderShell() {
  window.history.replaceState(null, "", "/hosts");
  return render(<Shell routes={routes} />);
}

/** notice is the release notice, once it shows. */
function findNotice(): Promise<HTMLElement> {
  return screen.findByRole("region", { name: /is available\.$/ });
}

function queryNotice(): HTMLElement | null {
  return screen.queryByRole("region", { name: /is available\.$/ });
}

/** openAccount opens the account panel and is it. */
async function openAccount(name: string): Promise<HTMLElement> {
  fireEvent.click(frameButton(`Account: ${name}`));
  return screen.findByRole("dialog", { name: `Account: ${name}` });
}

/** deferred is a promise a test settles when it chooses. */
function deferred<T>(): { promise: Promise<T>; resolve: (value: T) => void } {
  let resolve: (value: T) => void = () => undefined;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

/** forgetLater forgets, when the test ends, the release Later hid. */
function forgetLater(): void {
  onTestFinished(() => localStorage.removeItem("koko.release.later"));
}

// The notice: for the Owner and the Administrators, while a newer release is known.

test("the Owner sees which release is out and which version runs, with a link to its notes", async () => {
  const api = fakeApi();
  serve(api, alice, { server: server({ update_check: found("v0.5.0") }) });
  renderShell();

  const notice = await findNotice();

  expect(notice.textContent).toContain("koko v0.5.0 is available.");
  expect(notice.textContent).toContain("This koko server runs v0.4.2.");
  const notes = within(notice).getByRole("link", { name: "Release notes" });
  expect(notes.getAttribute("href")).toBe("https://github.com/cieons/koko/releases/tag/v0.5.0");
  expect(notes.getAttribute("target")).toBe("_blank");
  expect(within(notice).getByRole("button", { name: "Check now" })).toBeDefined();
  expect(within(notice).getByRole("button", { name: "Later" })).toBeDefined();
  expect(screen.getByText("hosts page")).toBeDefined();
  // The head of the page's column, above the page's title.
  expect(screen.getByRole("main").firstElementChild).toBe(notice);
  await expectAccessible();
});

test("an Administrator sees the notice, and the koko server's line with Check now in the account panel", async () => {
  const api = fakeApi();
  serve(api, erin, { server: server({ update_check: found("v0.5.0") }) });
  api.on(UserCurrent, () => ({
    user: erin,
    device_session: { id: "device_5", user: "erin", device: "laptop", created_at: "2026-11-03T09:00:00Z" },
  }));
  renderShell();

  const notice = await findNotice();
  expect(notice.textContent).toContain("koko v0.5.0 is available.");
  expect(within(notice).getByRole("button", { name: "Upgrade" })).toBeDefined();
  const panel = await openAccount("erin");
  expect(within(panel).getByText("Administrator")).toBeDefined();
  expect(within(panel).getByText("koko server")).toBeDefined();
  expect(within(panel).getByRole("button", { name: "Check now" })).toBeDefined();
});

test("a Member sees no notice, and the shell asks server.show nothing for them", async () => {
  const api = fakeApi();
  serve(api, bob, { server: server({ update_check: found("v0.5.0") }) });
  renderShell();
  await screen.findByText("hosts page");

  expect(queryNotice()).toBeNull();
  expect(api.requests(ServerShow)).toEqual([]);
  fireEvent.click(frameButton("Account: bob"));
  const panel = await screen.findByRole("dialog", { name: "Account: bob" });
  expect(within(panel).queryByText("koko server")).toBeNull();
  expect(within(panel).queryByRole("button", { name: "Check now" })).toBeNull();
});

test("with the check off, and before any check, there is no notice", async () => {
  const api = fakeApi();
  serve(api, alice, {
    server: server({ update_check: { enabled: false, checked_at: null, latest: null, error: null } }),
  });
  renderShell();
  await screen.findByText("hosts page");
  await waitFor(() => expect(api.requests(ServerShow)).toHaveLength(1));

  expect(queryNotice()).toBeNull();
});

test("with nothing newer than the version that runs there is no notice", async () => {
  const api = fakeApi();
  const current = { server: server({ update_check: found("v0.4.2") }) };
  serve(api, alice, current);
  const { unmount } = renderShell();
  await waitFor(() => expect(api.requests(ServerShow)).toHaveLength(1));
  expect(queryNotice()).toBeNull();
  unmount();

  // A development build is newer than every release.
  current.server = server({ version: "(devel)", update_check: found("v0.5.0") });
  renderShell();
  await waitFor(() => expect(api.requests(ServerShow)).toHaveLength(2));
  expect(queryNotice()).toBeNull();
});

test("Later hides the notice for that release in this browser, and a newer release brings it back", async () => {
  forgetLater();
  const api = fakeApi();
  const current = { server: server({ update_check: found("v0.5.0") }) };
  serve(api, alice, current);
  const { unmount } = renderShell();

  fireEvent.click(within(await findNotice()).getByRole("button", { name: "Later" }));

  await waitFor(() => expect(queryNotice()).toBeNull());
  unmount();
  renderShell();
  await waitFor(() => expect(api.requests(ServerShow)).toHaveLength(2));
  expect(queryNotice()).toBeNull();

  current.server = server({ update_check: found("v0.5.1") });
  fireEvent.focus(window);
  expect((await findNotice()).textContent).toContain("koko v0.5.1 is available.");
});

// How this koko server upgrades, by upgrade_way.

test("in the container image, How to upgrade shows the line for .env and the Compose command, each with Copy", async () => {
  const writeText = clipboard();
  const api = fakeApi();
  serve(api, alice, { server: server({ upgrade_way: "image", update_check: found("v0.5.0") }) });
  renderShell();
  const notice = await findNotice();
  expect(within(notice).queryByRole("group")).toBeNull();

  const howTo = within(notice).getByRole("button", { name: "How to upgrade" });
  expect(howTo.getAttribute("aria-expanded")).toBe("false");
  fireEvent.click(howTo);

  expect(howTo.getAttribute("aria-expanded")).toBe("true");
  expect(notice.textContent).toContain(
    "Set the release in .env, beside compose.yaml. Skip this step when .env sets KOKO_VERSION=latest.",
  );
  const env = within(notice).getByRole("group", { name: "Line for .env" });
  expect(within(env).getByRole("code").textContent).toBe("KOKO_VERSION=0.5.0");
  const compose = within(notice).getByRole("group", { name: "Compose command" });
  expect(within(compose).getByRole("code").textContent).toBe("$docker compose pull && docker compose up -d");
  fireEvent.click(within(env).getByRole("button", { name: "Copy" }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith("KOKO_VERSION=0.5.0"));
  fireEvent.click(within(compose).getByRole("button", { name: "Copy" }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith("docker compose pull && docker compose up -d"));
  expect(within(notice).queryByRole("button", { name: "Upgrade" })).toBeNull();
  await expectAccessible();
});

test("run by hand, the notice says to replace kokod and start it again, with no button", async () => {
  const api = fakeApi();
  serve(api, alice, { server: server({ upgrade_way: "none", update_check: found("v0.5.0") }) });
  renderShell();
  const notice = await findNotice();

  expect(notice.textContent).toContain(
    "This koko server was started by hand. Replace kokod with the one of v0.5.0, then start it again.",
  );
  expect(within(notice).queryByRole("button", { name: "Upgrade" })).toBeNull();
  expect(within(notice).queryByRole("button", { name: "How to upgrade" })).toBeNull();
});

test("installed with systemd, the notice offers Upgrade", async () => {
  const api = fakeApi();
  serve(api, alice, { server: server({ update_check: found("v0.5.0") }) });
  renderShell();
  const notice = await findNotice();

  expect(within(notice).getByRole("button", { name: "Upgrade" })).toBeDefined();
  expect(within(notice).queryByRole("button", { name: "How to upgrade" })).toBeNull();
});

// The account panel's koko server line, and Check now.

test("the account panel's koko server line says when the koko server last checked, or that it has not", async () => {
  const api = fakeApi();
  const current = { server: server({ update_check: found("v0.4.2") }) };
  serve(api, alice, current);
  renderShell();
  await waitFor(() => expect(api.requests(ServerShow)).toHaveLength(1));

  let panel = await openAccount("alice");
  expect(within(panel).getByText("koko server")).toBeDefined();
  expect(within(panel).getByText("v0.4.2")).toBeDefined();
  expect(within(panel).getByText("Checked 1 hour ago")).toBeDefined();
  await expectAccessible();
  fireEvent.keyDown(panel, { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

  current.server = server();
  fireEvent.focus(window);
  panel = await openAccount("alice");
  expect(await within(panel).findByText("Not checked yet")).toBeDefined();
  fireEvent.keyDown(panel, { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

  current.server = server({
    update_check: { enabled: true, checked_at: "2026-11-11T09:00:00Z", latest: null, error: "HEAD github.com: timeout" },
  });
  fireEvent.focus(window);
  panel = await openAccount("alice");
  expect(await within(panel).findByText("The last check failed: HEAD github.com: timeout")).toBeDefined();
});

test("Check now in the account panel shows that it checks, then that the version that runs is the newest", async () => {
  const api = fakeApi();
  serve(api, alice, { server: server() });
  const answer = deferred<ServerCheckUpdateResponse>();
  api.on(ServerCheckUpdate, () => answer.promise);
  renderShell();
  await waitFor(() => expect(api.requests(ServerShow)).toHaveLength(1));
  const panel = await openAccount("alice");

  fireEvent.click(within(panel).getByRole("button", { name: "Check now" }));

  expect(await within(panel).findByRole("status")).toHaveProperty("textContent", "Checking for a new release…");
  answer.resolve({ update_check: found("v0.4.2"), version: "v0.4.2" });
  await waitFor(() => expect(within(panel).getByRole("status").textContent).toBe("v0.4.2 is the newest release."));
  expect(queryNotice()).toBeNull();
});

test("Check now in the account panel that finds a newer release brings the notice", async () => {
  const api = fakeApi();
  const current = { server: server() };
  serve(api, alice, current);
  api.on(ServerCheckUpdate, () => {
    current.server = server({ update_check: found("v0.5.0") });
    return { update_check: current.server.update_check, version: "v0.4.2" };
  });
  renderShell();
  await waitFor(() => expect(api.requests(ServerShow)).toHaveLength(1));
  expect(queryNotice()).toBeNull();
  const panel = await openAccount("alice");

  fireEvent.click(within(panel).getByRole("button", { name: "Check now" }));

  await waitFor(() => expect(within(panel).getByRole("status").textContent).toBe("koko v0.5.0 is available."));
  expect(await findNotice()).toBeDefined();
});

test("Check now in the notice that finds a newer release shows it there", async () => {
  const api = fakeApi();
  const current = { server: server({ update_check: found("v0.5.0") }) };
  serve(api, alice, current);
  api.on(ServerCheckUpdate, () => {
    current.server = server({ update_check: found("v0.5.1") });
    return { update_check: current.server.update_check, version: "v0.4.2" };
  });
  renderShell();

  fireEvent.click(within(await findNotice()).getByRole("button", { name: "Check now" }));

  await waitFor(() => expect(queryNotice()?.textContent).toContain("koko v0.5.1 is available."));
  expect(api.requests(ServerCheckUpdate)).toEqual([{}]);
});

test("a failed Check now shows the refusal in place, in the account panel and in the notice", async () => {
  const api = fakeApi();
  serve(api, alice, { server: server({ update_check: found("v0.5.0") }) });
  api.on(ServerCheckUpdate, () => {
    throw refusal("update_check_failed", 502);
  });
  renderShell();
  const notice = await findNotice();

  fireEvent.click(within(notice).getByRole("button", { name: "Check now" }));
  expect(await within(notice).findByRole("alert")).toHaveProperty(
    "textContent",
    "The koko server could not find the latest koko release.",
  );

  const panel = await openAccount("alice");
  fireEvent.click(within(panel).getByRole("button", { name: "Check now" }));
  expect(await within(panel).findByRole("alert")).toHaveProperty(
    "textContent",
    "The koko server could not find the latest koko release.",
  );
  expect(alerts()).toHaveLength(2);
});

// The upgrade: the confirmation, then the progress to its outcome.

/** previous is an upgrade that failed before this one was asked for. */
const previous: LastUpgrade = {
  state: "failed",
  from: "v0.4.2",
  to: "v0.5.0",
  started_at: "2026-11-10T09:00:00Z",
  finished_at: "2026-11-10T09:01:00Z",
  message: "an earlier failure",
};

/** outcome is the helper's outcome of the upgrade from v0.4.2 to v0.5.0 asked for in the test. */
function outcome(state: string, message: string | null = null): LastUpgrade {
  return {
    state,
    from: "v0.4.2",
    to: "v0.5.0",
    started_at: "2026-11-11T09:00:00Z",
    finished_at: state === "running" ? null : "2026-11-11T09:02:00Z",
    message,
  };
}

/** upgradeServer is a systemd koko server at v0.4.2 that knows of v0.5.0, whose server.upgrade asks for it. */
function upgradeServer(api: FakeApi): { server: Server } {
  const current = { server: server({ update_check: found("v0.5.0"), last_upgrade: previous }) };
  serve(api, alice, current);
  api.on(ServerUpgrade, ({ version }) => ({ from: "v0.4.2", to: version ?? "v0.5.0", state: "requested" }));
  return current;
}

/** fakeTimers fakes setTimeout for the rest of the test, which advance moves on. */
function fakeTimers(): void {
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
  onTestFinished(() => {
    vi.useRealTimers();
  });
}

/**
 * confirmUpgrade presses Upgrade, then, with setTimeout faked from there on,
 * Upgrade and restart, and waits for the progress.
 */
async function confirmUpgrade(api: FakeApi): Promise<void> {
  fireEvent.click(within(await findNotice()).getByRole("button", { name: "Upgrade" }));
  const dialog = await screen.findByRole("alertdialog");
  fakeTimers();
  fireEvent.click(within(dialog).getByRole("button", { name: "Upgrade and restart" }));
  await until(() => screen.getByRole("region", { name: "Upgrading the koko server to v0.5.0…" }));
  expect(api.requests(ServerUpgrade)).toEqual([{ version: "v0.5.0" }]);
}

/** advance moves the faked clock on by ms, and lets what it set off settle. */
async function advance(ms: number): Promise<void> {
  await act(() => vi.advanceTimersByTimeAsync(ms));
}

/** setImmediate is Node's, which the DOM's types the tests compile against do not declare. */
const { setImmediate } = globalThis as unknown as { setImmediate: (callback: () => void) => void };

/**
 * until waits for check to pass while setTimeout is faked, yielding to the
 * fake API's answers with setImmediate, which stays real. vi.waitFor would
 * move the faked clock on at each try, and Testing Library's waitFor waits
 * on setTimeout itself.
 */
async function until(check: () => void): Promise<void> {
  for (let tries = 0; ; tries++) {
    try {
      check();
      return;
    } catch (error) {
      if (tries === 500) {
        throw error;
      }
    }
    await act(() => new Promise<void>((resolve) => setImmediate(resolve)));
  }
}

/** shows waits, while setTimeout is faked, until the page shows text. */
async function shows(text: string): Promise<void> {
  await until(() => expect(screen.getByText(text)).toBeDefined());
}

test("Upgrade asks first, saying the koko server restarts and SSH Sessions end; Cancel asks nothing", async () => {
  const api = fakeApi();
  upgradeServer(api);
  renderShell();
  const upgrade = within(await findNotice()).getByRole("button", { name: "Upgrade" });

  upgrade.focus();
  fireEvent.click(upgrade);

  const dialog = await screen.findByRole("alertdialog", { name: "Upgrade the koko server to v0.5.0?" });
  expect(dialog.getAttribute("aria-describedby")).not.toBeNull();
  expect(within(dialog).getByText(/^It restarts\./).textContent).toBe(
    "It restarts. Every open SSH Session through it ends, and connections through koko web reconnect. Devices keep working; a CLI can be brought to v0.5.0 with koko update.",
  );
  expect(within(dialog).getAllByRole("button").map((b) => b.textContent)).toEqual(["Cancel", "Upgrade and restart"]);
  await waitFor(() => expect(document.activeElement?.textContent).toBe("Cancel"));
  await expectAccessible();

  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));

  await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  expect(api.requests(ServerUpgrade)).toEqual([]);
  await waitFor(() => expect(document.activeElement).toBe(upgrade));
});

test("a refused upgrade shows why in the confirmation", async () => {
  const api = fakeApi();
  upgradeServer(api);
  api.on(ServerUpgrade, () => {
    throw refusal("upgrade_running", 409);
  });
  renderShell();
  fireEvent.click(within(await findNotice()).getByRole("button", { name: "Upgrade" }));
  const dialog = await screen.findByRole("alertdialog");

  fireEvent.click(within(dialog).getByRole("button", { name: "Upgrade and restart" }));

  expect(await within(dialog).findByRole("alert")).toHaveProperty(
    "textContent",
    "An upgrade of the koko server is already in progress.",
  );
  expect(screen.queryByText("Upgrading the koko server to v0.5.0…")).toBeNull();
});

test("the page follows the upgrade through the restart's failed calls to the new version, then reloads", async () => {
  const api = fakeApi();
  const current = upgradeServer(api);
  renderShell();
  await confirmUpgrade(api);
  const asked = api.requests(ServerShow).length;
  expect(screen.getByRole("status").textContent).toBe("Upgrading the koko server to v0.5.0…");

  // The koko server stops: the call drops, twice with its retry.
  api.drop(2);
  await advance(upgradePoll);
  await until(() => expect(api.requests(ServerShow)).toHaveLength(asked + 2));
  // The reverse proxy answers 502 while nothing listens.
  api.on(ServerShow, () => {
    throw refusal("bad_gateway", 502);
  });
  await advance(upgradePoll);
  await until(() => expect(api.requests(ServerShow)).toHaveLength(asked + 3));
  // The helper still runs the upgrade.
  serve(api, alice, current);
  current.server = server({ update_check: found("v0.5.0"), last_upgrade: outcome("running") });
  await advance(upgradePoll);
  await until(() => expect(api.requests(ServerShow)).toHaveLength(asked + 4));
  expect(screen.getByText("Upgrading the koko server to v0.5.0…")).toBeDefined();
  expect(alerts()).toEqual([]);

  current.server = server({ version: "v0.5.0", update_check: found("v0.5.0"), last_upgrade: outcome("succeeded") });
  await advance(upgradePoll);
  await shows("The koko server runs v0.5.0. This page reloads.");
  expect(reloadPage).not.toHaveBeenCalled();
  await advance(reloadAfter);
  expect(reloadPage).toHaveBeenCalledOnce();
});

test("a failed upgrade says why, which version runs, and Close brings the notice back", async () => {
  const api = fakeApi();
  const current = upgradeServer(api);
  renderShell();
  await confirmUpgrade(api);

  current.server = server({
    update_check: found("v0.5.0"),
    last_upgrade: outcome("failed", "the new kokod did not start; the previous kokod runs again"),
  });
  await advance(upgradePoll);

  await shows("The upgrade to v0.5.0 failed. The koko server runs v0.4.2.");
  expect(alerts()).toEqual([
    "The upgrade to v0.5.0 failed. The koko server runs v0.4.2.the new kokod did not start; the previous kokod runs again",
  ]);
  vi.useRealTimers();
  await expectAccessible();
  fireEvent.click(screen.getByRole("button", { name: "Close" }));
  expect(await findNotice()).toBeDefined();
  expect(reloadPage).not.toHaveBeenCalled();
});

test("an outcome from before the upgrade asked for is not taken for its own", async () => {
  const api = fakeApi();
  upgradeServer(api);
  renderShell();
  await confirmUpgrade(api);
  const asked = api.requests(ServerShow).length;

  await advance(upgradePoll);
  await until(() => expect(api.requests(ServerShow)).toHaveLength(asked + 1));

  expect(screen.getByText("Upgrading the koko server to v0.5.0…")).toBeDefined();
});

test("after 15 minutes with no outcome the page says where to look on the koko server's machine", async () => {
  const api = fakeApi();
  upgradeServer(api);
  renderShell();
  await confirmUpgrade(api);
  api.drop();

  await advance(upgradeWait - 1);
  expect(screen.getByText("Upgrading the koko server to v0.5.0…")).toBeDefined();
  await advance(1);

  await shows("The upgrade to v0.5.0 has not ended after 15 minutes. On the koko server's machine, this command shows why.");
  const log = screen.getByRole("group", { name: "Command that shows the upgrade's log" });
  expect(within(log).getByRole("code").textContent).toBe("$journalctl -u koko -u koko-upgrade");
  const polls = api.calls.length;
  await advance(upgradePoll * 3);
  expect(api.calls).toHaveLength(polls);
});

test("while the koko server restarts, a call through koko web that drops does not say koko web stopped", async () => {
  const api = fakeApi();
  upgradeServer(api);
  renderShell();
  await confirmUpgrade(api);
  api.drop();

  await advance(upgradePoll);
  await until(() => expect(api.calls.at(-1)?.path).toBe(ServerShow.path));
  fireEvent.focus(window);
  await advance(upgradePoll);

  expect(alerts()).toEqual([]);
  expect(screen.getByText("Upgrading the koko server to v0.5.0…")).toBeDefined();
});
