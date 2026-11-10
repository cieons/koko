import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { expect, test } from "vitest";

import type { Op } from "../api/api";
import { DeviceRevoke } from "../api/device";
import { CodeNameTaken } from "../api/errors";
import { type User, UserCurrent } from "../api/user";
import { expectAccessible } from "../test/axe";
import { type FakeApi, fakeApi, refusal } from "../test/fakeApi";
import { alerts, chooseInTopBar, findAlert, preferLanguages } from "../test/pages";
import { Refusal } from "../ui/states";
import { OwnerOnly } from "./caller";
import { useRead, useWrite } from "./data";
import { useEditForm } from "./form";
import { navigate, type PageProps, type Route, useSearchParams } from "./router";
import { Shell } from "./Shell";

// The shell's logic at the page level: pages written as a capability's
// would be, against the fake API, through the shell's own client and hooks:
// the retry and Idempotency-Key, edit forms, reloading, refusals in either
// language, Owner-only buttons, and page state in the URL.

const alice: User = { id: "user_1", name: "alice", role: "owner", created_at: "2026-11-01T09:00:00Z" };
const bob: User = { id: "user_2", name: "bob", role: "member", created_at: "2026-11-02T09:00:00Z" };

/** asDevice makes user.current answer user on a Device Session of their laptop. */
function asDevice(api: FakeApi, user: User): void {
  api.on(UserCurrent, () => ({
    user,
    device_session: { id: "device_1", user: user.name, device: "laptop", created_at: "2026-11-03T09:00:00Z" },
  }));
}

// A record of the test's own, read and edited as a capability's pages would
// do with theirs.

interface Thing {
  name: string;
  provider: string;
  note: string;
}

const ThingShow: Op<{ name: string }, { thing: Thing }> = {
  name: "thing.show",
  path: "/v1/thing.show",
  write: false,
};

const ThingSet: Op<{ name: string; provider?: string; note?: string }, { thing: Thing }> = {
  name: "thing.set",
  path: "/v1/thing.set",
  write: true,
};

const web1: Thing = { name: "web1", provider: "hetzner", note: "Line one\n  indented" };

function ThingPage({ params }: PageProps) {
  const show = useRead(ThingShow, { name: params["name"] ?? "" });
  if (!show.data) {
    return <Refusal error={show.error} />;
  }
  return <ThingEditor thing={show.data.thing} />;
}

function ThingEditor({ thing }: { thing: Thing }) {
  const form = useEditForm({ provider: thing.provider, note: thing.note });
  const save = useWrite(ThingSet);
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void save.submit({ name: thing.name, ...form.changes }).then((r) => r && form.saved());
      }}
    >
      <h1>{thing.name}</h1>
      <input aria-label="provider" value={form.values.provider} onChange={(e) => form.set("provider", e.target.value)} />
      <textarea aria-label="note" value={form.values.note} onChange={(e) => form.set("note", e.target.value)} />
      <button type="submit">Save</button>
      <Refusal error={save.error} />
      <OwnerOnly>
        <button type="button">Remove</button>
      </OwnerOnly>
    </form>
  );
}

/** A page that keeps its state, a filter, in the URL. */
function FilterPage() {
  const [params, setParams] = useSearchParams();
  const provider = params.get("provider") ?? "";
  return (
    <>
      <p>filter: {provider || "none"}</p>
      <button type="button" onClick={() => setParams(new URLSearchParams({ provider: "hetzner" }))}>
        hetzner only
      </button>
    </>
  );
}

/** A page that revokes a Device Session by name, as the Devices page does. */
function RevokePage() {
  const revoke = useWrite(DeviceRevoke);
  const [done, setDone] = useState(false);
  return (
    <>
      <button type="button" onClick={() => void revoke.submit({ device: "phone" }).then(() => setDone(true))}>
        revoke phone
      </button>
      {done && <p>revoked</p>}
    </>
  );
}

const routes: Route[] = [
  { path: "/things/:name", page: ThingPage },
  { path: "/filter", page: FilterPage },
  { path: "/revoke", page: RevokePage },
];

/** renderShell renders the shell at path. */
function renderShell(path: string) {
  window.history.replaceState(null, "", path);
  return render(<Shell routes={routes} />);
}

function keysOf(api: FakeApi, op: Op<unknown, unknown>): (string | undefined)[] {
  return api.calls.filter((c) => c.path === op.path).map((c) => c.headers["idempotency-key"]);
}

async function openThing(api: FakeApi): Promise<void> {
  api.on(ThingShow, () => ({ thing: web1 }));
  renderShell("/things/web1");
  await screen.findByRole("heading", { name: "web1" });
}

// The API client: retries and Idempotency-Key.

test("a write whose connection drops is retried once, with the same Idempotency-Key", async () => {
  const api = fakeApi();
  asDevice(api, alice);
  api.on(ThingSet, (r) => ({ thing: { ...web1, ...r } }));
  await openThing(api);

  fireEvent.change(screen.getByLabelText("provider"), { target: { value: "ovh" } });
  api.drop(1);
  fireEvent.click(screen.getByRole("button", { name: "Save" }));

  await waitFor(() => expect(keysOf(api, ThingSet)).toHaveLength(2));
  const [first, retry] = keysOf(api, ThingSet);
  expect(first).toMatch(/^[0-9a-f]{32}$/);
  expect(retry).toBe(first);
  expect(alerts()).toEqual([]);
});

test("a read whose connection drops is retried once, without an Idempotency-Key", async () => {
  const api = fakeApi();
  asDevice(api, alice);
  api.drop(1);

  renderShell("/filter");

  await screen.findByText("filter: none");
  expect(api.calls.map((c) => [c.method, c.path, c.request, c.headers["idempotency-key"]])).toEqual([
    ["POST", "/v1/user.current", {}, undefined],
    ["POST", "/v1/user.current", {}, undefined],
  ]);
});

test("submitting a form again after its write failed reuses its key; another request takes a new one", async () => {
  const api = fakeApi();
  asDevice(api, alice);
  api.on(ThingSet, () => {
    throw refusal(CodeNameTaken, 409);
  });
  await openThing(api);
  const save = screen.getByRole("button", { name: "Save" });
  fireEvent.change(screen.getByLabelText("provider"), { target: { value: "ovh" } });

  api.drop(2); // the write and its retry
  fireEvent.click(save);
  await waitFor(() => expect(alerts()).toContain("The connection to koko web was lost."));
  api.drop(0);
  fireEvent.click(save); // refused this time
  await waitFor(() => expect(keysOf(api, ThingSet)).toHaveLength(3));
  fireEvent.click(save);
  await waitFor(() => expect(keysOf(api, ThingSet)).toHaveLength(4));
  fireEvent.change(screen.getByLabelText("note"), { target: { value: "moved" } });
  fireEvent.click(save);
  await waitFor(() => expect(keysOf(api, ThingSet)).toHaveLength(5));

  const [first, retry, resubmitted, again, changed] = keysOf(api, ThingSet);
  expect([retry, resubmitted, again]).toEqual([first, first, first]);
  expect(changed).not.toBe(first);
});

test("a write that succeeded takes a new key when the same request is sent again", async () => {
  const api = fakeApi();
  asDevice(api, bob);
  api.on(DeviceRevoke, () => ({
    device_session: { id: "device_2", user: "bob", device: "phone", created_at: "", revoked_at: "" },
  }));
  renderShell("/revoke");

  fireEvent.click(await screen.findByRole("button", { name: "revoke phone" }));
  await screen.findByText("revoked");
  fireEvent.click(screen.getByRole("button", { name: "revoke phone" }));

  await waitFor(() => expect(keysOf(api, DeviceRevoke)).toHaveLength(2));
  const [first, second] = keysOf(api, DeviceRevoke);
  expect(second).not.toBe(first);
});

test("a write that succeeds while a later one is pending leaves the later one its key", async () => {
  const api = fakeApi();
  asDevice(api, alice);
  let answerFirst = () => {};
  api.on(ThingSet, (r) =>
    r.provider === "ovh"
      ? new Promise((resolve) => {
          answerFirst = () => resolve({ thing: { ...web1, ...r } });
        })
      : { thing: { ...web1, ...r } },
  );
  await openThing(api);
  const save = screen.getByRole("button", { name: "Save" });

  fireEvent.change(screen.getByLabelText("provider"), { target: { value: "ovh" } });
  fireEvent.click(save); // pending until answerFirst
  await waitFor(() => expect(keysOf(api, ThingSet)).toHaveLength(1));
  fireEvent.change(screen.getByLabelText("provider"), { target: { value: "aws" } });
  api.drop(2);
  fireEvent.click(save); // dropped, and dropped again
  await waitFor(() => expect(keysOf(api, ThingSet)).toHaveLength(3));
  await act(async () => answerFirst());
  api.drop(0);
  fireEvent.click(save); // the same request again

  await waitFor(() => expect(keysOf(api, ThingSet)).toHaveLength(4));
  const [first, later, laterRetry, laterAgain] = keysOf(api, ThingSet);
  expect(later).not.toBe(first);
  expect([laterRetry, laterAgain]).toEqual([later, later]);
});

// Edit forms.

test("an edit form sends only the fields that changed", async () => {
  const api = fakeApi();
  asDevice(api, alice);
  api.on(ThingSet, (r) => ({ thing: { ...web1, ...r } }));
  await openThing(api);

  fireEvent.change(screen.getByLabelText("note"), { target: { value: "renewed" } });
  fireEvent.change(screen.getByLabelText("provider"), { target: { value: "ovh" } });
  fireEvent.change(screen.getByLabelText("provider"), { target: { value: "hetzner" } }); // back as it was
  fireEvent.click(screen.getByRole("button", { name: "Save" }));

  await waitFor(() => expect(api.requests(ThingSet)).toHaveLength(1));
  expect(api.requests(ThingSet)).toEqual([{ name: "web1", note: "renewed" }]);
});

test("once a write succeeds, its values are the edit form's baseline", async () => {
  const api = fakeApi();
  asDevice(api, alice);
  api.on(ThingSet, (r) => ({ thing: { ...web1, ...r } }));
  await openThing(api);

  fireEvent.change(screen.getByLabelText("provider"), { target: { value: "ovh" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(api.requests(ThingSet)).toHaveLength(1));
  fireEvent.change(screen.getByLabelText("provider"), { target: { value: "hetzner" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));

  await waitFor(() => expect(api.requests(ThingSet)).toHaveLength(2));
  expect(api.requests(ThingSet)).toEqual([
    { name: "web1", provider: "ovh" },
    { name: "web1", provider: "hetzner" },
  ]);
});

// Owner-only buttons.

test("a Member sees no Owner-only button", async () => {
  const api = fakeApi();
  asDevice(api, bob);
  await openThing(api);

  expect(screen.queryByRole("button", { name: "Remove" })).toBeNull();
});

test.each([
  ["on a device", (api: FakeApi) => asDevice(api, alice)],
  ["over the Unix socket", (api: FakeApi) => api.on(UserCurrent, () => ({ user: alice }))],
])("the Owner %s sees the Owner-only button", async (_, as) => {
  const api = fakeApi();
  as(api);
  await openThing(api);

  expect(screen.getByRole("button", { name: "Remove" })).toBeDefined();
});

// Refusals and values in either language.

test("an API error shows in place, in its code's message in the current language", async () => {
  const api = fakeApi();
  asDevice(api, alice);
  api.on(ThingSet, () => {
    throw refusal(CodeNameTaken, 409);
  });
  await openThing(api);
  fireEvent.change(screen.getByLabelText("provider"), { target: { value: "ovh" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));

  expect(await findAlert()).toBe("This name is already in use.");
  await expectAccessible();
  await chooseInTopBar("Language", "简体中文");
  expect(alerts()).toEqual(["此名称已被占用。"]);
});

test("names, keys, values and Notes show as stored, in either language", async () => {
  preferLanguages("zh-CN");
  const api = fakeApi();
  asDevice(api, alice);
  await openThing(api);

  expect(screen.getByLabelText<HTMLInputElement>("provider").value).toBe("hetzner");
  expect(screen.getByLabelText<HTMLTextAreaElement>("note").value).toBe("Line one\n  indented");
});

// Reloading.

test("data reloads when the window regains focus and on every navigation", async () => {
  const api = fakeApi();
  asDevice(api, alice);
  let provider = "hetzner";
  api.on(ThingShow, () => ({ thing: { ...web1, provider } }));
  renderShell("/things/web1");
  await screen.findByRole("heading", { name: "web1" });

  provider = "ovh";
  fireEvent.focus(window);
  await waitFor(() => expect(api.requests(ThingShow)).toHaveLength(2));
  await waitFor(() => expect(api.requests(UserCurrent)).toHaveLength(2));

  act(() => navigate("/things/web1")); // the page it is on
  await waitFor(() => expect(api.requests(ThingShow)).toHaveLength(3));
});

// Page state in the URL.

test("a page keeps its state in the URL, and back returns to the earlier state", async () => {
  const api = fakeApi();
  asDevice(api, alice);
  renderShell("/filter");
  expect(await screen.findByText("filter: none")).toBeDefined();

  fireEvent.click(screen.getByRole("button", { name: "hetzner only" }));
  expect(await screen.findByText("filter: hetzner")).toBeDefined();
  expect(window.location.search).toBe("?provider=hetzner");

  act(() => window.history.back());
  expect(await screen.findByText("filter: none")).toBeDefined();
  expect(window.location.search).toBe("");
});
