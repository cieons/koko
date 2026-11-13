import { afterEach, beforeEach, expect, test, vi } from "vitest";

import { parseColor } from "./themeColors";
import { timing } from "./timing";
import { type Answer, type SocketState, type TextSocket, type WorkspaceTab, WorkspaceSocket } from "./workspaceSocket";

// The page's end of the Workspace socket, against a fake WebSocket the
// test plays the koko server on, and on a fake clock.

const start = Date.parse("2026-11-13T12:00:00Z");

/** FakeSocket is a WebSocket whose other end the test plays. */
class FakeSocket implements TextSocket {
  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;
  readonly at = Date.now() - start;
  closed = false;
  readonly sent: unknown[] = [];

  send(data: string): void {
    if (!this.closed) {
      this.sent.push(JSON.parse(data));
    }
  }

  close(): void {
    this.closed = true;
  }

  opened(): void {
    this.onopen?.(new Event("open"));
  }

  says(message: object): void {
    this.onmessage?.({ data: JSON.stringify(message) } as MessageEvent);
  }

  drop(): void {
    this.onclose?.({} as CloseEvent);
  }
}

let made: WorkspaceSocket[] = [];

beforeEach(() => {
  vi.useFakeTimers({ now: start });
});

afterEach(() => {
  for (const each of made.splice(0)) {
    each.close();
  }
  vi.useRealTimers();
});

const passed = (ms = 0) => vi.advanceTimersByTimeAsync(ms);

const tab: WorkspaceTab = { id: "a1", host: "web1", label: "web1", state: "running", session: "5e55a1" };

/** joined is a Workspace socket the koko server has taken and sent its Workspace on. */
async function joined() {
  const sockets: FakeSocket[] = [];
  const states: SocketState[] = [];
  const workspaces: WorkspaceTab[][] = [];
  const socket = new WorkspaceSocket(
    { onWorkspace: (tabs) => workspaces.push(tabs), onState: (state) => states.push(state) },
    () => {
      const each = new FakeSocket();
      sockets.push(each);
      return each;
    },
  );
  made.push(socket);
  const first = sockets[0] as FakeSocket;
  first.opened();
  expect(first.sent).toEqual([{ protocol: 2 }]);
  first.says({ tabs: [{ ...tab, opened_at: "2026-11-13T11:00:00Z", extra: true }] });
  await passed();
  return { socket, sockets, states, workspaces, latest: () => sockets.at(-1) as FakeSocket };
}

test("the Workspace's tabs are read as the koko server gives them, and an entry that is no tab is left out", async () => {
  const { workspaces, latest } = await joined();
  expect(workspaces).toEqual([[tab]]);
  latest().says({
    tabs: [
      { id: "b2", host: "db2", label: "db2", state: "ended", ended: { reason: "shell ended", exit_status: 0 } },
      { id: "c3", host: "cache3", label: "cache3", state: "refused", refused: { code: "host_unreachable", message: "down" } },
      { id: "d4", host: "web1", label: "web1 (2)", state: "something new" },
      "no tab",
    ],
  });
  expect(workspaces.at(-1)).toEqual([
    { id: "b2", host: "db2", label: "db2", state: "ended", ended: { reason: "shell ended", exitStatus: 0 } },
    { id: "c3", host: "cache3", label: "cache3", state: "refused", refused: { code: "host_unreachable", message: "down" } },
  ]);
});

test("the page probes after 15 s without sending, and takes 45 s of silence for a dead WebSocket", async () => {
  const { sockets, states, latest } = await joined();
  const first = latest();
  await passed(timing.probe - 1);
  expect(first.sent).toHaveLength(1);
  await passed(1);
  expect(first.sent.at(-1), "the probe").toEqual({});
  await passed(timing.dead - timing.probe - 1);
  expect(states).toEqual([{ status: "open" }]);
  await passed(1);
  expect(states.at(-1)).toEqual({ status: "reconnecting", stopping: false });
  expect(first.closed).toBe(true);
  await vi.advanceTimersToNextTimerAsync(); // a wait of 0 set within a timer is 1 ms on the fake clock
  expect(sockets, "a reconnect at once").toHaveLength(2);
});

test("a dropped WebSocket is reconnected at once, then after 250 ms doubling to 5 s, and gets the Workspace whole", async () => {
  const { sockets, states, workspaces, latest } = await joined();
  latest().drop();
  for (let i = 0; i < 8; i++) {
    await vi.advanceTimersToNextTimerAsync();
    latest().drop();
  }
  const waits = sockets.slice(1).map((each, i, all) => each.at - (all[i - 1]?.at ?? each.at));
  expect(waits).toEqual([0, 250, 500, 1000, 2000, 4000, 5000, 5000]);
  await vi.advanceTimersToNextTimerAsync();
  latest().opened();
  latest().says({ tabs: [] });
  expect(states.at(-1)).toEqual({ status: "open" });
  expect(workspaces.at(-1)).toEqual([]);
});

test("a close and a reopen on a WebSocket that dropped go again on the next, and an open is given up", async () => {
  const { socket, latest } = await joined();
  const answers: Answer[] = [];
  void socket.request({ open: "db2", cols: 80, rows: 24 }).then((answer) => answers.push(answer));
  void socket.request({ reopen: "a1", cols: 80, rows: 24 }).then((answer) => answers.push(answer));
  void socket.request({ close: "a1" }).then((answer) => answers.push(answer));
  expect(latest().sent.slice(1)).toEqual([
    { request: 1, open: "db2", cols: 80, rows: 24 },
    { request: 2, reopen: "a1", cols: 80, rows: 24 },
    { request: 3, close: "a1" },
  ]);
  latest().drop();
  await passed();
  expect(answers).toEqual([{ kind: "lost" }]);
  const next = latest();
  next.opened();
  expect(next.sent, "nothing goes before the Workspace").toEqual([{ protocol: 2 }]);
  next.says({ tabs: [tab] });
  expect(next.sent.slice(1)).toEqual([
    { request: 2, reopen: "a1", cols: 80, rows: 24 },
    { request: 3, close: "a1" },
  ]);
  next.says({ request: 2, tab: "a1" });
  next.says({ request: 3 });
  await passed();
  expect(answers).toEqual([{ kind: "lost" }, { kind: "tab", tab: "a1" }, { kind: "done" }]);
});

test("stopping is said on the reconnect, and a refusal of the first message is final", async () => {
  const { states, sockets, latest } = await joined();
  latest().says({ stopping: "the koko server is restarting" });
  latest().drop();
  expect(states.at(-1)).toEqual({ status: "reconnecting", stopping: true });
  await passed();
  latest().opened();
  latest().says({ refused: "signin_required", message: "" });
  await passed(60_000);
  expect(states.at(-1)).toEqual({ status: "refused", code: "signin_required", message: "" });
  expect(sockets).toHaveLength(2);
});

test("the theme's colours are read as RGB, oklch included", () => {
  expect(parseColor("#1d1f27")).toEqual([0x1d, 0x1f, 0x27]);
  expect(parseColor("#fff")).toEqual([255, 255, 255]);
  expect(parseColor("rgb(12, 34, 56)")).toEqual([12, 34, 56]);
  expect(parseColor("oklch(1 0 0)")).toEqual([255, 255, 255]);
  expect(parseColor(" oklch(0.145 0.005 265)")).toEqual([9, 10, 12]);
  expect(parseColor("oklch(62.8% 0.2577 29.23)"), "CSS's red").toEqual([255, 0, 0]);
  expect(parseColor("var(--elsewhere)")).toBeUndefined();
});
