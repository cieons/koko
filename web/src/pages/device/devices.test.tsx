import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, onTestFinished, test, vi } from "vitest";

import {
  CodeLastDeviceSession,
  DeviceAdd,
  DeviceList,
  DeviceRevoke,
  type DeviceSession,
  type JoinCode,
} from "../../api/device";
import { SpaceList } from "../../api/space";
import { type User, UserCurrent, UserList } from "../../api/user";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { actAs, renderAt, tableRows } from "../../test/pages";

// The Devices page, against the fake API, as the Owner and as a Member, and
// how a Join Code shows once.

const alice: User = { id: "user_1", name: "alice", role: "owner", created_at: "2026-11-01T09:00:00Z" };
const bob: User = { id: "user_2", name: "bob", role: "member", created_at: "2026-11-02T09:00:00Z" };

// Device Sessions, made afresh for each use, so no test sees another's changes.

function bobLaptop(): DeviceSession {
  return { id: "device_1", user: "bob", device: "laptop", created_at: "2026-11-03T09:00:00Z", last_used_at: "2026-11-06T08:59:00Z" };
}

function bobPhone(): DeviceSession {
  return {
    id: "device_2",
    user: "bob",
    device: "phone",
    added_by: { id: "device_1", user: "bob", device: "laptop" },
    created_at: "2026-11-04T09:00:00Z",
  };
}

function aliceLaptop(): DeviceSession {
  return { id: "device_3", user: "alice", device: "laptop", created_at: "2026-11-01T09:00:00Z", last_used_at: "2026-11-06T09:00:00Z" };
}

function aliceDesktop(): DeviceSession {
  return { id: "device_4", user: "alice", device: "desktop", created_at: "2026-11-01T09:00:00Z" };
}

const desktopCode: JoinCode = {
  id: "joincode_1",
  code: "koko_testcodeaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  user: "bob",
  device: "desktop",
  expires_at: "2026-11-06T09:15:00Z",
};

const rows = () => tableRows("Devices");

/**
 * clipboard stands in for the browser's clipboard for one test, or, when
 * not available, takes it away, as a browser without the API does.
 */
function clipboard(available = true) {
  const writeText = vi.fn((_: string) => Promise.resolve());
  Object.defineProperty(navigator, "clipboard", { value: available ? { writeText } : undefined, configurable: true });
  onTestFinished(() => {
    Reflect.deleteProperty(navigator, "clipboard");
  });
  return writeText;
}

/**
 * clockAt stops the browser's clock at instant, for one test; only Date and
 * intervals are faked, so promises and the testing library's waits still
 * run.
 */
function clockAt(instant: string): void {
  vi.useFakeTimers({ toFake: ["Date", "setInterval", "clearInterval"] });
  vi.setSystemTime(new Date(instant));
  onTestFinished(() => {
    vi.useRealTimers();
  });
}

/**
 * advanceCountdown moves the stopped clock on by ms once the shown Join
 * Code's countdown runs: React starts its interval, the page's only one, in
 * an effect that may run only after the line shows, and a clock moved on
 * before then moves no countdown (#79). It looks with vi.waitUntil, on the
 * real clock, since a testing-library wait starts a faked interval of its
 * own; an interval of 0 keeps vi.waitUntil from moving the faked clock.
 */
async function advanceCountdown(ms: number): Promise<void> {
  await vi.waitUntil(() => vi.getTimerCount() > 0, { interval: 0, message: "the Join Code's countdown never started" });
  act(() => vi.advanceTimersByTime(ms));
}

/** issueAsBob renders the Devices page as bob on his laptop and adds a device, whose Join Code is joinCode. */
async function issueAsBob(joinCode: JoinCode = desktopCode): Promise<{ api: FakeApi; shown: HTMLElement }> {
  const api = fakeApi();
  actAs(api, bob, bobLaptop());
  api.on(DeviceList, () => ({ device_sessions: [bobLaptop()] }));
  api.on(DeviceAdd, () => ({ join_code: joinCode }));
  api.on(SpaceList, () => ({ spaces: [] }));
  renderAt("/devices");
  await screen.findByRole("table", { name: "Devices" });
  fireEvent.click(screen.getByRole("button", { name: "Add a device" }));
  const shown = await screen.findByRole("region", { name: "Join Code" });
  return { api, shown };
}

test("a Member sees their own Device Sessions and no Owner-only control", async () => {
  const api = fakeApi();
  actAs(api, bob, bobLaptop());
  api.on(DeviceList, () => ({ device_sessions: [bobLaptop(), bobPhone()] }));

  renderAt("/devices");

  await screen.findByRole("table", { name: "Devices" });
  expect(rows()).toEqual([
    ["laptop (this device)", "the koko server's machine", "2026-11-03", "2026-11-06", "Revoke"],
    ["phone", "bob/laptop", "2026-11-04", "never", "Revoke"],
  ]);
  expect(screen.queryByRole("combobox", { name: "User" })).toBeNull();
  expect(screen.getByRole("textbox", { name: "Device name (optional)" })).toBeDefined();
  expect(api.requests(DeviceList)).toEqual([{}]);
});

test("adding a device never asks, and shows its koko join line once, with copy and its countdown", async () => {
  clockAt("2026-11-06T09:00:00Z");
  const writeText = clipboard();
  const api = fakeApi();
  actAs(api, bob, bobLaptop());
  api.on(DeviceList, () => ({ device_sessions: [bobLaptop()] }));
  api.on(DeviceAdd, () => ({ join_code: desktopCode }));
  api.on(SpaceList, () => ({ spaces: [] }));
  renderAt("/devices");
  await screen.findByRole("table", { name: "Devices" });

  fireEvent.change(screen.getByRole("textbox", { name: "Device name (optional)" }), { target: { value: "desktop" } });
  fireEvent.click(screen.getByRole("button", { name: "Add a device" }));

  const line = await screen.findByText(`koko join ${desktopCode.code}`);
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(api.requests(DeviceAdd)).toEqual([{ device: "desktop" }]);
  const shown = screen.getByRole("region", { name: "Join Code" });
  expect(shown.textContent).toContain("On bob's new device desktop, run this line:");
  expect(shown.textContent).toContain("It expires in 15:00.");
  expect(screen.getByRole<HTMLInputElement>("textbox", { name: "Device name (optional)" }).value).toBe("");

  await advanceCountdown(61_000);
  expect(shown.textContent).toContain("It expires in 13:59.");

  fireEvent.click(within(shown).getByRole("button", { name: "Copy" }));
  expect(await within(shown).findByText("Copied.")).toBeDefined();
  expect(writeText.mock.calls).toEqual([[line.textContent]]);

  // Leaving the page and coming back loses the line.
  fireEvent.click(within(screen.getByRole("banner")).getByRole("link", { name: "Spaces" }));
  await screen.findByRole("heading", { name: "Spaces" });
  fireEvent.click(within(screen.getByRole("banner")).getByRole("link", { name: "Devices" }));
  await screen.findByRole("table", { name: "Devices" });
  expect(screen.queryByRole("region", { name: "Join Code" })).toBeNull();
  expect(document.body.textContent).not.toContain(desktopCode.code);
  expect(window.location.href).not.toContain(desktopCode.code);
  expect(localStorage.length + sessionStorage.length).toBe(0);
});

test("leaving the web UI loses the line, even when the browser keeps the page to come back to", async () => {
  const { shown } = await issueAsBob();
  expect(shown.textContent).toContain(desktopCode.code);

  fireEvent(window, new Event("pagehide")); // the browser leaves for another site

  expect(screen.queryByRole("region", { name: "Join Code" })).toBeNull();
  expect(document.body.textContent).not.toContain(desktopCode.code);
});

test("the countdown runs from when the page got the code, whatever the browser's clock says of the server's", async () => {
  clockAt("2026-11-06T09:20:00Z"); // 20 minutes ahead of the koko server's clock

  const { shown } = await issueAsBob();

  expect(shown.textContent).toContain(`koko join ${desktopCode.code}`);
  expect(shown.textContent).toContain("It expires in 15:00.");
});

test("a browser that cannot copy says to select the line instead", async () => {
  clipboard(false);
  const { shown } = await issueAsBob();

  fireEvent.click(within(shown).getByRole("button", { name: "Copy" }));

  expect((await within(shown).findByRole("status")).textContent).toBe("Could not copy: select the line and copy it.");
  expect(shown.textContent).toContain(`koko join ${desktopCode.code}`);
});

test("a Join Code that expired is no longer shown, and the page says to issue a new one", async () => {
  clockAt("2026-11-06T09:00:00Z");
  const { api, shown } = await issueAsBob();

  await advanceCountdown(14 * 60_000 + 59_000);
  expect(shown.textContent).toContain("It expires in 0:01.");
  await advanceCountdown(1_000);

  await waitFor(() => expect(shown.textContent).toBe("This Join Code expired. Add a device again for a new one."));
  expect(document.body.textContent).not.toContain(desktopCode.code);
  expect(api.requests(DeviceAdd)).toEqual([{}]);
});

test("the Owner sees everyone's Device Sessions, and adds a device for another User", async () => {
  const api = fakeApi();
  actAs(api, alice, aliceLaptop());
  api.on(DeviceList, () => ({ device_sessions: [aliceLaptop(), bobLaptop(), bobPhone()] }));
  api.on(UserList, () => ({ users: [alice, bob] }));
  api.on(DeviceAdd, ({ user }) => ({ join_code: { ...desktopCode, user: user ?? "alice", device: "device-3" } }));
  renderAt("/devices");
  await screen.findByRole("table", { name: "Devices" });

  expect(rows()).toEqual([
    ["alice", "laptop (this device)", "the koko server's machine", "2026-11-01", "2026-11-06", "Revoke"],
    ["bob", "laptop", "the koko server's machine", "2026-11-03", "2026-11-06", "Revoke"],
    ["bob", "phone", "bob/laptop", "2026-11-04", "never", "Revoke"],
  ]);
  const user = screen.getByRole<HTMLSelectElement>("combobox", { name: "User" });
  expect(user.value).toBe("alice");
  await waitFor(() => expect(within(user).getAllByRole("option").map((o) => o.textContent)).toEqual(["alice", "bob"]));

  fireEvent.change(user, { target: { value: "bob" } });
  fireEvent.click(screen.getByRole("button", { name: "Add a device" }));

  const shown = await screen.findByRole("region", { name: "Join Code" });
  expect(shown.textContent).toContain("On bob's new device device-3, run this line:");
  expect(api.requests(DeviceAdd)).toEqual([{ user: "bob" }]);
  expect(screen.queryByRole("dialog")).toBeNull();
});

test.each([
  ["another device", "phone", "phone is cut off at once and can never connect again. The devices it added keep working."],
  [
    "the device this page runs through",
    "laptop",
    "laptop is the device this page runs through: once it is revoked, this page can do nothing more and this device deletes its key. The devices it added keep working.",
  ],
])("revoking %s asks first, stating the consequence", async (_, device, consequence) => {
  const api = fakeApi();
  actAs(api, bob, bobLaptop());
  let sessions = [bobLaptop(), bobPhone()];
  api.on(DeviceList, () => ({ device_sessions: sessions }));
  api.on(DeviceRevoke, (r) => {
    const revoked = sessions.find((s) => s.device === r.device)!;
    sessions = sessions.filter((s) => s !== revoked);
    return { device_session: { ...revoked, revoked_at: "2026-11-06T09:00:00Z" } };
  });
  renderAt("/devices");
  await screen.findByRole("table", { name: "Devices" });

  fireEvent.click(screen.getByRole("button", { name: `Revoke ${device}` }));
  const dialog = screen.getByRole("dialog", { name: `Revoke ${device}?` });
  expect(dialog.textContent).toContain(consequence);
  expect(within(dialog).queryByRole("textbox")).toBeNull();
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  expect(api.requests(DeviceRevoke)).toEqual([]);

  fireEvent.click(screen.getByRole("button", { name: `Revoke ${device}` }));
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Revoke" }));

  await waitFor(() => expect(rows()).toHaveLength(1));
  expect(api.requests(DeviceRevoke)).toEqual([{ user: "bob", device }]);
});

test.each([
  [
    "on that device",
    (api: FakeApi) => actAs(api, alice, aliceLaptop()),
    "laptop is the Owner's last device and the one this page runs through: once it is revoked, this page can do nothing more, this device deletes its key, and only koko device add on the koko server's own machine adds a device for the Owner.",
  ],
  [
    "over the Unix socket",
    (api: FakeApi) => api.on(UserCurrent, () => ({ user: alice })),
    "laptop is the Owner's last device: once it is revoked, only koko device add on the koko server's own machine adds a device for the Owner.",
  ],
])(
  "revoking the Owner's own last Device Session %s needs its name typed; another User's last one does not",
  async (_, as, consequence) => {
    const api = fakeApi();
    as(api);
    api.on(DeviceList, () => ({ device_sessions: [aliceLaptop(), bobLaptop()] }));
    api.on(UserList, () => ({ users: [alice, bob] }));
    api.on(DeviceRevoke, (r) => ({
      device_session: { ...(r.user === "bob" ? bobLaptop() : aliceLaptop()), revoked_at: "2026-11-06T09:00:00Z" },
    }));
    renderAt("/devices");
    await screen.findByRole("table", { name: "Devices" });

    fireEvent.click(screen.getByRole("button", { name: "Revoke bob/laptop" }));
    const bobs = screen.getByRole("dialog", { name: "Revoke bob/laptop?" });
    expect(within(bobs).queryByRole("textbox")).toBeNull();
    fireEvent.click(within(bobs).getByRole("button", { name: "Revoke" }));
    await waitFor(() => expect(api.requests(DeviceRevoke)).toEqual([{ user: "bob", device: "laptop" }]));

    fireEvent.click(screen.getByRole("button", { name: "Revoke laptop" }));
    const dialog = screen.getByRole("dialog", { name: "Revoke laptop?" });
    expect(dialog.textContent).toContain(consequence);
    const confirm = within(dialog).getByRole<HTMLButtonElement>("button", { name: "Revoke" });
    expect(confirm.disabled).toBe(true);
    fireEvent.change(within(dialog).getByLabelText("Type laptop to confirm."), { target: { value: "laptop" } });
    expect(confirm.disabled).toBe(false);
    fireEvent.click(confirm);

    await waitFor(() => expect(api.requests(DeviceRevoke)).toHaveLength(2));
    expect(api.requests(DeviceRevoke)[1]).toEqual({ user: "alice", device: "laptop", last: true });
  },
);

test("the Owner with another live device revokes this one without typing its name", async () => {
  const api = fakeApi();
  actAs(api, alice, aliceLaptop());
  api.on(DeviceList, () => ({ device_sessions: [aliceDesktop(), aliceLaptop()] }));
  api.on(UserList, () => ({ users: [alice] }));
  api.on(DeviceRevoke, () => ({ device_session: { ...aliceDesktop(), revoked_at: "2026-11-06T09:00:00Z" } }));
  renderAt("/devices");
  await screen.findByRole("table", { name: "Devices" });

  fireEvent.click(screen.getByRole("button", { name: "Revoke desktop" }));
  const dialog = screen.getByRole("dialog", { name: "Revoke desktop?" });
  expect(within(dialog).queryByRole("textbox")).toBeNull();
  fireEvent.click(within(dialog).getByRole("button", { name: "Revoke" }));

  await waitFor(() => expect(api.requests(DeviceRevoke)).toEqual([{ user: "alice", device: "desktop" }]));
});

test("a revoke refused because the list was out of date says why, and the list is put right", async () => {
  const api = fakeApi();
  api.on(UserCurrent, () => ({ user: alice }));
  let sessions = [aliceDesktop(), aliceLaptop()];
  api.on(DeviceList, () => ({ device_sessions: sessions }));
  api.on(UserList, () => ({ users: [alice] }));
  api.on(DeviceRevoke, () => {
    sessions = [aliceDesktop()]; // laptop was revoked elsewhere meanwhile
    throw refusal(CodeLastDeviceSession, 409);
  });
  renderAt("/devices");
  await screen.findByRole("table", { name: "Devices" });

  fireEvent.click(screen.getByRole("button", { name: "Revoke desktop" }));
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Revoke" }));

  expect((await screen.findByRole("alert")).textContent).toBe(
    "This is the Owner's last device: revoking it needs its name typed.",
  );
  await waitFor(() => expect(rows()).toHaveLength(1));
  fireEvent.click(screen.getByRole("button", { name: "Revoke desktop" }));
  expect(within(screen.getByRole("dialog")).getByLabelText("Type desktop to confirm.")).toBeDefined();
});

test("Revoked Device Sessions show only when asked for, with when they were Revoked", async () => {
  const api = fakeApi();
  actAs(api, bob, bobLaptop());
  const revokedPhone = { ...bobPhone(), revoked_at: "2026-11-05T10:00:00Z" };
  api.on(DeviceList, ({ all }) => ({ device_sessions: all ? [bobLaptop(), revokedPhone] : [bobLaptop()] }));
  renderAt("/devices");
  await screen.findByRole("table", { name: "Devices" });
  expect(rows().map((r) => r[0])).toEqual(["laptop (this device)"]);

  fireEvent.click(screen.getByRole("checkbox", { name: "Show Revoked devices" }));

  await waitFor(() =>
    expect(rows()).toEqual([
      ["laptop (this device)", "the koko server's machine", "2026-11-03", "2026-11-06", "", "Revoke"],
      ["phone", "bob/laptop", "2026-11-04", "never", "2026-11-05", ""],
    ]),
  );
  expect(window.location.search).toBe("?all=1");
  expect(api.requests(DeviceList)).toContainEqual({ all: true });
});
