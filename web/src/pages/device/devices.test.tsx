import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { expect, type MockInstance, onTestFinished, test, vi } from "vitest";

import {
  CodeLastDeviceSession,
  DeviceAdd,
  DeviceList,
  DeviceRevoke,
  type DeviceSession,
  type JoinCode,
} from "../../api/device";
import { SpaceList } from "../../api/space";
import { CodeNameTaken } from "../../api/errors";
import { type User, UserCurrent, UserList } from "../../api/user";
import { navigate } from "../../shell/router";
import { expectAccessible } from "../../test/axe";
import { chooseOption, chooseRowAction } from "../../test/controls";
import { type FakeApi, fakeApi, refusal } from "../../test/fakeApi";
import { actAs, findAlert, renderAt, statuses, tableRows } from "../../test/pages";

// The Devices page, against the fake API, as the Owner and as a Member, and
// how a Join Code shows once in the Add device wizard.

const alice: User = { id: "user_1", name: "alice", role: "owner", created_at: "2026-11-01T09:00:00Z" };
const bob: User = { id: "user_2", name: "bob", role: "member", created_at: "2026-11-02T09:00:00Z" };

// Device Sessions, made afresh for each use, so no test sees another's changes.

function bobLaptop(): DeviceSession {
  return {
    id: "device_1",
    user: "bob",
    device: "laptop",
    created_at: "2026-11-03T09:00:00Z",
    last_used_at: "2026-11-06T08:59:00Z",
  };
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
  return {
    id: "device_3",
    user: "alice",
    device: "laptop",
    created_at: "2026-11-01T09:00:00Z",
    last_used_at: "2026-11-06T09:00:00Z",
  };
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
  intervals = vi.spyOn(window, "setInterval");
  vi.setSystemTime(new Date(instant));
  onTestFinished(() => {
    vi.useRealTimers();
  });
}

/** intervals records each setInterval since clockAt, with its period. */
let intervals: MockInstance<typeof window.setInterval> | undefined;

/**
 * advanceCountdown moves the stopped clock on by ms once the shown Join
 * Code's countdown runs, its own interval of a second and not another, such
 * as one of Astryx's components: React starts it in an effect that may run
 * only after the line shows, and a clock moved on before then moves no
 * countdown (#79). It looks with vi.waitUntil, on the real clock, since a
 * testing-library wait starts a faked interval of its own; an interval of 0
 * keeps vi.waitUntil from moving the faked clock.
 */
async function advanceCountdown(ms: number): Promise<void> {
  await vi.waitUntil(() => intervals?.mock.calls.some(([, period]) => period === 1000), {
    interval: 0,
    message: "the Join Code's countdown never started",
  });
  act(() => vi.advanceTimersByTime(ms));
}

/** openWizard opens the Add device wizard, and is its dialog. */
function openWizard(): HTMLElement {
  fireEvent.click(screen.getByRole("button", { name: "Add device" }));
  return screen.getByRole("dialog", { name: "Add device" });
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
  fireEvent.click(within(openWizard()).getByRole("button", { name: "Issue Join Code" }));
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
    ["laptopThis device", "The koko server's machine", "2026-11-03", "2026-11-06", ""],
    ["phone", "bob/laptop", "2026-11-04", "Never", ""],
  ]);
  expect(screen.getByRole("button", { name: "Actions for laptop" })).toBeDefined();
  await expectAccessible();
  const wizard = openWizard();
  expect(within(wizard).queryByRole("combobox", { name: "User" })).toBeNull();
  expect(within(wizard).getByRole("textbox", { name: /^Device name/ })).toBeDefined();
  expect(api.requests(DeviceList)).toEqual([{}]);
  await expectAccessible();
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

  const wizard = openWizard();
  fireEvent.change(within(wizard).getByRole("textbox", { name: /^Device name/ }), { target: { value: "desktop" } });
  fireEvent.click(within(wizard).getByRole("button", { name: "Issue Join Code" }));

  const line = await within(wizard).findByText(`koko join ${desktopCode.code}`);
  expect(screen.queryByRole("alertdialog")).toBeNull();
  expect(api.requests(DeviceAdd)).toEqual([{ device: "desktop" }]);
  const shown = within(wizard).getByRole("region", { name: "Join Code" });
  expect(shown.textContent).toContain("Run this line on bob's new device desktop:");
  expect(shown.textContent).toContain("It expires in 15:00.");
  await expectAccessible();

  await advanceCountdown(61_000);
  expect(shown.textContent).toContain("It expires in 13:59.");

  fireEvent.click(within(shown).getByRole("button", { name: "Copy" }));
  expect(await within(shown).findByText("Copied.")).toBeDefined();
  expect(writeText.mock.calls).toEqual([[line.textContent]]);

  // Closing the wizard forgets the line.
  fireEvent.click(within(wizard).getByRole("button", { name: "Done" }));
  expect(screen.queryByRole("dialog", { name: "Add device" })).toBeNull();
  expect(document.body.textContent).not.toContain(desktopCode.code);
  expect(window.location.href).not.toContain(desktopCode.code);
  expect(localStorage.length + sessionStorage.length).toBe(0);
  expect(within(openWizard()).getByRole<HTMLInputElement>("textbox", { name: /^Device name/ }).value).toBe("");
});

test("leaving the page loses the line", async () => {
  await issueAsBob();

  act(() => navigate("/spaces"));
  await screen.findByRole("heading", { name: "Spaces" });
  act(() => navigate("/devices"));
  await screen.findByRole("table", { name: "Devices" });

  expect(screen.queryByRole("region", { name: "Join Code" })).toBeNull();
  expect(document.body.textContent).not.toContain(desktopCode.code);
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

  const said = "This browser did not allow copying. Select the text and copy it.";
  await waitFor(() => expect(statuses()).toContain(said));
  expect(within(shown).getByText(said)).toBeDefined();
  expect(shown.textContent).toContain(`koko join ${desktopCode.code}`);
});

test("a Join Code that expired is no longer shown, and the wizard says to issue a new one", async () => {
  clockAt("2026-11-06T09:00:00Z");
  const { api, shown } = await issueAsBob();

  await advanceCountdown(14 * 60_000 + 59_000);
  expect(shown.textContent).toContain("It expires in 0:01.");
  await advanceCountdown(1_000);

  await waitFor(() => expect(shown.textContent).toBe("This Join Code has expired. Issue a new one with Add device."));
  expect(document.body.textContent).not.toContain(desktopCode.code);
  expect(api.requests(DeviceAdd)).toEqual([{}]);
});

test("a refused Add device says why on its first step", async () => {
  const api = fakeApi();
  actAs(api, bob, bobLaptop());
  api.on(DeviceList, () => ({ device_sessions: [bobLaptop()] }));
  api.on(DeviceAdd, () => {
    throw refusal(CodeNameTaken, 409);
  });
  renderAt("/devices");
  await screen.findByRole("table", { name: "Devices" });

  const wizard = openWizard();
  fireEvent.change(within(wizard).getByRole("textbox", { name: /^Device name/ }), { target: { value: "laptop" } });
  fireEvent.click(within(wizard).getByRole("button", { name: "Issue Join Code" }));

  expect(await within(wizard).findByText("This name is already in use.")).toBeDefined();
  expect(within(wizard).queryByRole("region", { name: "Join Code" })).toBeNull();
  await expectAccessible();
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
    ["alice", "laptopThis device", "The koko server's machine", "2026-11-01", "2026-11-06", ""],
    ["bob", "laptop", "The koko server's machine", "2026-11-03", "2026-11-06", ""],
    ["bob", "phone", "bob/laptop", "2026-11-04", "Never", ""],
  ]);
  expect(screen.getByRole("button", { name: "Actions for bob/phone" })).toBeDefined();
  await expectAccessible();

  const wizard = openWizard();
  const user = within(wizard).getByRole("combobox", { name: "User" });
  expect(user.textContent).toContain("alice");
  await chooseOption(user, "bob", ["alice", "bob"]);
  await expectAccessible();
  fireEvent.click(within(wizard).getByRole("button", { name: "Issue Join Code" }));

  const shown = await within(wizard).findByRole("region", { name: "Join Code" });
  expect(shown.textContent).toContain("Run this line on bob's new device device-3:");
  expect(api.requests(DeviceAdd)).toEqual([{ user: "bob" }]);
  expect(screen.queryByRole("alertdialog")).toBeNull();
});

test.each([
  [
    "another device",
    "phone",
    "phone is disconnected at once and can never connect again. The devices it added keep working.",
  ],
  [
    "the device this page runs through",
    "laptop",
    "laptop is the device this page runs through. Once it is revoked, this page can take no further action and this device deletes its key. The devices it added keep working.",
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

  await chooseRowAction(`Actions for ${device}`, "Revoke");
  const dialog = screen.getByRole("alertdialog", { name: `Revoke ${device}` });
  expect(dialog.textContent).toContain(consequence);
  expect(within(dialog).queryByRole("textbox")).toBeNull();
  await expectAccessible();
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  expect(api.requests(DeviceRevoke)).toEqual([]);

  await chooseRowAction(`Actions for ${device}`, "Revoke");
  fireEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Revoke" }));

  await waitFor(() => expect(rows()).toHaveLength(1));
  expect(api.requests(DeviceRevoke)).toEqual([{ user: "bob", device }]);
});

test.each([
  [
    "on that device",
    (api: FakeApi) => actAs(api, alice, aliceLaptop()),
    "laptop is the Owner's last device and the one this page runs through. Once it is revoked, this page can take no further action, this device deletes its key, and only koko device add on the koko server's own machine can add a device for the Owner.",
  ],
  [
    "over the Unix socket",
    (api: FakeApi) => api.on(UserCurrent, () => ({ user: alice })),
    "laptop is the Owner's last device. Once it is revoked, only koko device add on the koko server's own machine can add a device for the Owner.",
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

    await chooseRowAction("Actions for bob/laptop", "Revoke");
    const bobs = screen.getByRole("alertdialog", { name: "Revoke bob/laptop" });
    expect(within(bobs).queryByRole("textbox")).toBeNull();
    fireEvent.click(within(bobs).getByRole("button", { name: "Revoke" }));
    await waitFor(() => expect(api.requests(DeviceRevoke)).toEqual([{ user: "bob", device: "laptop" }]));

    await chooseRowAction("Actions for laptop", "Revoke");
    const dialog = screen.getByRole("alertdialog", { name: "Revoke laptop" });
    expect(dialog.textContent).toContain(consequence);
    const confirm = within(dialog).getByRole<HTMLButtonElement>("button", { name: "Revoke" });
    expect(confirm.disabled).toBe(true);
    await expectAccessible();
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

  await chooseRowAction("Actions for desktop", "Revoke");
  const dialog = screen.getByRole("alertdialog", { name: "Revoke desktop" });
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

  await chooseRowAction("Actions for desktop", "Revoke");
  fireEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Revoke" }));

  expect(await findAlert()).toBe("This is the Owner's last device. Type its name to revoke it.");
  await waitFor(() => expect(rows()).toHaveLength(1));
  await expectAccessible();
  await chooseRowAction("Actions for desktop", "Revoke");
  expect(within(screen.getByRole("alertdialog")).getByLabelText("Type desktop to confirm.")).toBeDefined();
});

test("Revoked Device Sessions show only when asked for, with when they were Revoked", async () => {
  const api = fakeApi();
  actAs(api, bob, bobLaptop());
  const revokedPhone = { ...bobPhone(), revoked_at: "2026-11-05T10:00:00Z" };
  api.on(DeviceList, ({ all }) => ({ device_sessions: all ? [bobLaptop(), revokedPhone] : [bobLaptop()] }));
  renderAt("/devices");
  await screen.findByRole("table", { name: "Devices" });
  expect(rows().map((r) => r[0])).toEqual(["laptopThis device"]);

  fireEvent.click(screen.getByRole("switch", { name: "Show Revoked devices" }));

  await waitFor(() =>
    expect(rows()).toEqual([
      ["laptopThis device", "The koko server's machine", "2026-11-03", "2026-11-06", "", ""],
      ["phone", "bob/laptop", "2026-11-04", "Never", "2026-11-05", ""],
    ]),
  );
  // A Revoked Device Session has nothing left to revoke.
  expect(screen.queryByRole("button", { name: "Actions for phone" })).toBeNull();
  expect(window.location.search).toBe("?all=1");
  expect(api.requests(DeviceList)).toContainEqual({ all: true });
});
