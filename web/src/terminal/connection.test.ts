import { afterEach, beforeEach, expect, test, vi } from "vitest";

import { type ConnectionState, type TerminalSocket, Terminals, timing } from "./connection";
import { encodeRecord, RecordReader, type TerminalRecord } from "./records";
import { encodeFrame, type Frame, FrameReader } from "./stream";

// The page's end of a web terminal's SSH Session, against a fake WebSocket
// the test plays the koko server on, and on a fake clock.

const text = (bytes: Uint8Array) => new TextDecoder().decode(bytes);
const bytes = (s: string) => new TextEncoder().encode(s);

const start = Date.parse("2026-11-12T12:00:00Z");
const id = "0123456789abcdef0123456789abcdef";
const keptKey = "koko.terminal.sessions";
const kept = () => JSON.parse(sessionStorage.getItem(keptKey) ?? "[]") as string[];

/**
 * FakeLocks is the browser's Web Locks, which every document a test plays
 * shares, as the documents of one browser do.
 */
class FakeLocks {
  readonly held = new Set<string>();
  /** queues holds, for each lock asked for, when its last request lets it go. */
  private readonly queues = new Map<string, Promise<void>>();

  /** request runs callback once the lock is free, and holds it until callback's promise settles. */
  request(name: string, callback: () => Promise<void>): Promise<void> {
    const granted = (this.queues.get(name) ?? Promise.resolve()).then(() => {
      this.held.add(name);
      return callback().finally(() => this.held.delete(name));
    });
    this.queues.set(
      name,
      granted.catch(() => {}),
    );
    return granted;
  }

  async query(): Promise<LockManagerSnapshot> {
    return { held: [...this.held].map((name) => ({ name, mode: "exclusive" })), pending: [] };
  }
}

let locks = new FakeLocks();

/** withoutLocks makes the browser one without Web Locks. */
function withoutLocks(): void {
  Reflect.deleteProperty(navigator, "locks");
}

beforeEach(() => {
  vi.useFakeTimers({ now: start });
  locks = new FakeLocks();
  Object.defineProperty(navigator, "locks", { configurable: true, value: locks });
});

afterEach(() => {
  vi.useRealTimers();
  sessionStorage.clear();
  show(true);
  withoutLocks();
});

/** show makes the page shown or hidden, and tells it so. */
function show(visible: boolean): void {
  Object.defineProperty(document, "visibilityState", { configurable: true, value: visible ? "visible" : "hidden" });
  document.dispatchEvent(new Event("visibilitychange"));
}

/** pagehide tells the page the browser is leaving it, kept for the back-forward cache or not. */
function pagehide(persisted: boolean): void {
  window.dispatchEvent(Object.assign(new Event("pagehide"), { persisted }));
}

/** passed lets ms pass on the page's clock, and everything due by then happen. */
const passed = (ms = 0) => vi.advanceTimersByTimeAsync(ms);

/** FakeSocket is a WebSocket whose other end the test plays. */
class FakeSocket implements TerminalSocket {
  binaryType: BinaryType = "blob";
  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;
  /** at is when the page opened it, on the page's clock. */
  readonly at = Date.now() - start;
  /** closed says the page closed it. */
  closed = false;
  private readonly sent: (string | Uint8Array)[] = [];

  send(data: string | Uint8Array<ArrayBuffer>): void {
    if (!this.closed) {
      this.sent.push(typeof data === "string" ? data : data.slice());
    }
  }

  close(): void {
    this.closed = true;
  }

  /** opened is the koko server taking the upgrade. */
  opened(): void {
    this.onopen?.(new Event("open"));
  }

  /** answer is the koko server's answer to the first message. */
  answer(answer: object): void {
    this.onmessage?.({ data: JSON.stringify(answer) } as MessageEvent);
  }

  /** frames is the Gateway's end of the stream sending frames. */
  frames(...frames: Frame[]): void {
    for (const frame of frames) {
      this.onmessage?.({ data: encodeFrame(frame).buffer } as MessageEvent);
    }
  }

  /** records is the Gateway sending records, in one frame. */
  records(...records: TerminalRecord[]): void {
    this.frames({ kind: "data", ack: 0, data: Uint8Array.from(records.flatMap((record) => [...encodeRecord(record)])) });
  }

  /** drop is the WebSocket closing under the page. */
  drop(): void {
    this.onclose?.({} as CloseEvent);
  }

  /** first is the first message the page sent. */
  get first(): unknown {
    return typeof this.sent[0] === "string" ? JSON.parse(this.sent[0]) : this.sent[0];
  }

  /** framesSent is the frames the page sent after its first message. */
  get framesSent(): Frame[] {
    const reader = new FrameReader();
    const frames: Frame[] = [];
    for (const message of this.sent.slice(1)) {
      expect(message, "a message after the first is binary").toBeInstanceOf(Uint8Array);
      reader.push(message as Uint8Array);
      for (let frame = reader.next(); frame; frame = reader.next()) {
        frames.push(frame);
      }
    }
    return frames;
  }

  /** dataSent is the stream's bytes the page sent on this WebSocket. */
  get dataSent(): Uint8Array {
    return Uint8Array.from(this.framesSent.flatMap((frame) => (frame.kind === "data" ? [...frame.data] : [])));
  }

  /** recordsSent names the records the page sent on this WebSocket. */
  get recordsSent(): string[] {
    const reader = new RecordReader();
    reader.push(this.dataSent);
    const records: string[] = [];
    for (let record = reader.next(); record; record = reader.next()) {
      records.push(
        record.type === "data"
          ? `data ${text(record.data)}`
          : record.type === "resize"
            ? `resize ${record.columns}x${record.rows}`
            : record.type,
      );
    }
    return records;
  }
}

/** terminals is one document's terminals, with the WebSockets it opened. */
function terminals(): { terminals: Terminals; sockets: FakeSocket[] } {
  const sockets: FakeSocket[] = [];
  return {
    sockets,
    terminals: new Terminals(() => {
      const socket = new FakeSocket();
      sockets.push(socket);
      return socket;
    }),
  };
}

/** newTab is the terminals of a document in a new browser tab, whose sessionStorage keeps nothing. */
function newTab() {
  sessionStorage.clear();
  return terminals();
}

/** opening opens a terminal to web1 and returns it before the koko server has answered. */
async function opening(on = newTab()) {
  const states: ConnectionState[] = [];
  const output: string[] = [];
  const connection = on.terminals.open(
    "web1",
    { columns: 120, rows: 36 },
    { onState: (state) => states.push(state), onOutput: (data) => void output.push(text(data)) },
  );
  await passed();
  return { ...on, connection, states, output, state: () => states.at(-1) };
}

/** connected opens a terminal to web1 whose shell has started. */
async function connected(on = newTab()) {
  const page = await opening(on);
  const socket = page.sockets.at(-1) as FakeSocket;
  socket.opened();
  socket.answer({ tunnel: id });
  await passed();
  return { ...page, socket };
}

/** latest is the WebSocket the page opened last. */
const latest = (sockets: FakeSocket[]) => sockets.at(-1) as FakeSocket;

test("an open names the Host and the size, and its answer starts the stream", async () => {
  const { connection, sockets, states } = await opening();
  expect(connection.state).toEqual({ status: "connecting" });
  expect(sockets).toHaveLength(1);
  const socket = latest(sockets);
  expect(socket.first, "nothing is sent before the WebSocket is open").toBeUndefined();

  connection.resize(100, 30); // the emulator's grid, known before the WebSocket opened
  socket.opened();
  expect(socket.binaryType).toBe("arraybuffer");
  expect(socket.first).toEqual({ protocol: 1, open: "web1", cols: 100, rows: 30 });

  connection.resize(90, 20); // after the open went
  socket.answer({ tunnel: id });
  await passed();
  expect(states).toEqual([{ status: "connected" }]);
  expect(connection.sessionId).toBe(id);
  expect(kept()).toEqual([id]);
  expect(socket.recordsSent, "the size that changed after the open").toEqual(["resize 90x20"]);
});

test("each refusal is shown with its code and the koko server's words, and nothing more is tried", async () => {
  const refusals = {
    reload_page: "",
    signin_required: "",
    cookie_required: "",
    revoked: "",
    unknown_host: "no Host named web1.",
    host_registered: "web1 is Registered, and koko does not reach it yet.",
    relay_registered: "the Relay of web1 is not Managed.",
    access_refused: "access to web1 was refused.",
    ssh_session_limit: "this Sign-in already has 16 SSH Sessions open, the most koko allows; this SSH Session did not start.",
    host_unreachable: "web1 is unreachable.",
    host_key_mismatch: "the host key of web1 is not the pinned one.",
    login_refused: "web1 refused the koko key.",
    host_connection_failed: "the connection to web1 failed.",
    shell_refused: "web1 refused a shell.",
  };
  for (const [code, message] of Object.entries(refusals)) {
    const { connection, sockets, states } = await opening();
    const socket = latest(sockets);
    socket.opened();
    socket.answer(message === "" ? { refused: code } : { refused: code, message });
    await passed(timing.grace);
    expect(states, code).toEqual([{ status: "refused", code, message }]);
    expect(socket.closed, code).toBe(true);
    expect(sockets, `${code}: no further attempt`).toHaveLength(1);
    expect(connection.sessionId, code).toBe("");
    expect(kept(), code).toEqual([]);
  }
});

test("an open the koko server never answers is unreachable", async () => {
  const noAnswer: [string, (socket: FakeSocket) => void | Promise<void>][] = [
    ["the WebSocket closed before it opened", (socket) => socket.drop()],
    ["the WebSocket closed before the answer", (socket) => (socket.opened(), socket.drop())],
    ["the WebSocket never opened", () => passed(timing.connect)],
    ["an answer that is no JSON", (socket) => (socket.opened(), socket.onmessage?.({ data: "{" } as MessageEvent))],
    ["an answer that is binary", (socket) => (socket.opened(), socket.frames({ kind: "eof", ack: 0 }))],
    ["an answer of neither kind", (socket) => (socket.opened(), socket.answer({ ended: true }))],
  ];
  for (const [what, act] of noAnswer) {
    const { sockets, states } = await opening();
    await act(latest(sockets));
    await passed(timing.grace);
    expect(states, what).toEqual([{ status: "unreachable" }]);
    expect(sockets, `${what}: no further attempt`).toHaveLength(1);
  }
  // The Gateway dials the Host before it answers, so an open waits for it.
  const { sockets, states } = await opening();
  latest(sockets).opened();
  await passed(60_000);
  expect(states, "an open still waiting after a minute").toEqual([]);

  const failing = new Terminals(() => {
    throw new Error("no WebSocket here");
  });
  const told: ConnectionState[] = [];
  failing.open("web1", { columns: 80, rows: 24 }, { onState: (state) => told.push(state), onOutput: () => {} });
  await passed();
  expect(told, "a browser that opens no WebSocket").toEqual([{ status: "unreachable" }]);
});

test("the shell's output reaches the page in order, and what the page sends reaches the shell as records", async () => {
  const { connection, socket, output } = await connected();
  socket.records({ type: "data", data: bytes("$ ") }, { type: "data", data: bytes("ls\r\n") });
  await passed();
  expect(output).toEqual(["$ ", "ls\r\n"]);

  connection.send(bytes("ls\r"));
  connection.send(new Uint8Array(0));
  connection.resize(80, 24);
  connection.resize(0, 5000); // an emulator not yet laid out, or absurdly large
  expect(socket.recordsSent).toEqual(["data ls\r", "resize 80x24", "resize 1x4096"]);

  const pasted = new Uint8Array(40_000).fill(0x61);
  connection.send(pasted);
  const records = socket.recordsSent.slice(3);
  expect(records.map((record) => record.length - "data ".length), "a paste cut at a record's most").toEqual([32_768, 7_232]);
});

test("the page reads on only once it has written the output into the emulator", async () => {
  const on = terminals();
  const written: (() => void)[] = [];
  const output: string[] = [];
  on.terminals.open(
    "web1",
    { columns: 80, rows: 24 },
    {
      onState: () => {},
      onOutput: (data) => {
        output.push(text(data));
        return new Promise<void>((resolve) => written.push(resolve));
      },
    },
  );
  await passed();
  const socket = latest(on.sockets);
  socket.opened();
  socket.answer({ tunnel: id });
  const chunk = (fill: string): TerminalRecord => ({ type: "data", data: bytes(fill.repeat(30_000)) });
  socket.records(chunk("a"));
  socket.records(chunk("b"));
  socket.records(chunk("c"));
  await passed();
  expect(output.map((data) => data[0]), "output given while the first is being written").toEqual(["a"]);
  expect(socket.framesSent, "what the page acknowledged, the first frame read and not yet a frame's worth").toEqual([]);
  written[0]?.();
  await passed();
  expect(output.map((data) => data[0])).toEqual(["a", "b"]);
  expect(socket.framesSent.map((frame) => frame.ack), "acknowledged once two frames were read").toEqual([60_008]);
});

test("the shell's end is shown as its exit status, its signal, or the koko server's notice", async () => {
  const endings: [string, (socket: FakeSocket) => void, ConnectionState][] = [
    [
      "an exit status",
      (socket) => {
        socket.records({ type: "data", data: bytes("logout\r\n") }, { type: "exitStatus", status: 3 });
        socket.frames({ kind: "close", ack: 0, reason: "" });
      },
      { status: "ended", ending: { kind: "exitStatus", status: 3 } },
    ],
    [
      "a signal",
      (socket) => {
        socket.records({ type: "exitSignal", signal: "KILL" });
        socket.frames({ kind: "close", ack: 0, reason: "" });
      },
      { status: "ended", ending: { kind: "exitSignal", signal: "KILL" } },
    ],
    [
      "a notice",
      (socket) => {
        socket.records({ type: "data", data: bytes("\r\nkoko: the koko server is restarting; this SSH Session has ended.\r\n") });
        socket.frames({ kind: "close", ack: 0, reason: "the koko server is restarting; this SSH Session has ended." });
        socket.drop(); // the Gateway closes the WebSocket behind its close frame
      },
      { status: "ended", ending: { kind: "notice", notice: "the koko server is restarting; this SSH Session has ended." } },
    ],
  ];
  for (const [what, act, want] of endings) {
    const { socket, sockets, states, output } = await connected();
    act(socket);
    await passed(timing.grace);
    expect(states.at(-1), what).toEqual(want);
    expect(states, what).toHaveLength(2);
    expect(output.join("").length > 0 || what === "a signal", `${what}: the output before the end`).toBe(true);
    expect(socket.closed, what).toBe(true);
    expect(sockets, `${what}: no Resume of an SSH Session that ended`).toHaveLength(1);
    expect(kept(), `${what}: its id is no longer kept`).toEqual([]);
  }
});

test("bytes that are not the protocol end the SSH Session, telling the koko server why", async () => {
  const broken: [string, (socket: FakeSocket) => void][] = [
    ["an unknown record", (socket) => socket.frames({ kind: "data", ack: 0, data: Uint8Array.of(0x04, 0x00) })],
    ["a resize from the koko server", (socket) => socket.records({ type: "resize", columns: 80, rows: 24 })],
    ["a frame acknowledging what was never sent", (socket) => socket.frames({ kind: "data", ack: 9, data: new Uint8Array(0) })],
  ];
  for (const [what, act] of broken) {
    const { socket, states } = await connected();
    act(socket);
    await passed();
    expect(states.at(-1), what).toMatchObject({ status: "ended", ending: { kind: "protocol" } });
    expect(socket.framesSent.at(-1), what).toMatchObject({ kind: "close", reason: expect.stringContaining("malformed") });
    expect(socket.closed, what).toBe(true);
  }
});

test("the page probes after 15 s without sending, and takes 45 s of silence for a dead WebSocket", async () => {
  const { connection, socket, sockets, states } = await connected();
  await passed(timing.probe - 1);
  expect(socket.framesSent).toEqual([]);
  await passed(1);
  expect(socket.framesSent, "the probe").toEqual([{ kind: "data", ack: 0, data: new Uint8Array(0) }]);

  // What the page sends puts its next probe off; what it hears keeps the
  // WebSocket alive, and only that.
  await passed(5_000); // 20 s
  connection.send(bytes("x"));
  socket.frames({ kind: "data", ack: 0, data: new Uint8Array(0) }); // the Gateway's own probe
  await passed(timing.probe - 1);
  expect(socket.framesSent).toHaveLength(2);
  await passed(1); // 35 s
  expect(socket.framesSent).toHaveLength(3);
  await passed(timing.dead - timing.probe - 1); // 1 ms short of 45 s after the Gateway's probe
  expect(states).toEqual([{ status: "connected" }]);
  await passed(1);
  expect(states.at(-1)).toEqual({ status: "reconnecting", since: start + 20_000 + timing.dead });
  expect(socket.closed).toBe(true);
  expect(latest(sockets)).not.toBe(socket);
});

test("an idle terminal whose koko server probes stays connected", async () => {
  const { socket, sockets, states } = await connected();
  for (let i = 0; i < 20; i++) {
    await passed(timing.probe);
    socket.frames({ kind: "data", ack: 0, data: new Uint8Array(0) });
  }
  expect(states).toEqual([{ status: "connected" }]);
  expect(sockets).toHaveLength(1);
  expect(socket.framesSent, "the page's probes").toHaveLength(20);
});

test("a page shown again, or back online, takes a WebSocket silent for 15 s for dead at once", async () => {
  const wakes: [string, () => void][] = [
    ["visibilitychange", () => show(true)],
    ["pageshow", () => window.dispatchEvent(new Event("pageshow"))],
    ["online", () => window.dispatchEvent(new Event("online"))],
  ];
  for (const [what, wake] of wakes) {
    const { socket, sockets, states } = await connected();
    await passed(timing.probe - 1);
    wake();
    expect(states, `${what}, 1 ms short of 15 s of silence`).toEqual([{ status: "connected" }]);
    await passed(1);
    wake();
    await passed();
    expect(states.at(-1), what).toEqual({ status: "reconnecting", since: Date.now() });
    expect(socket.closed, what).toBe(true);
    latest(sockets).opened();
    expect(latest(sockets).first, what).toEqual({ protocol: 1, resume: id, offset: 0 });
  }
  // Hidden, a page is not woken: the browser tells it of a change it cannot use.
  const { states } = await connected();
  await passed(timing.probe);
  show(false);
  expect(states).toEqual([{ status: "connected" }]);
});

/** dropped is a connected terminal whose WebSocket has just closed, and its first try to Resume. */
async function dropped() {
  const page = await connected();
  page.socket.records({ type: "data", data: bytes("0123456789") });
  await passed();
  page.socket.drop();
  await passed();
  return page;
}

test("a dropped WebSocket is resumed at once, then after 250 ms doubling to 5 s", async () => {
  const { socket, sockets, states } = await dropped();
  expect(states.at(-1)).toEqual({ status: "reconnecting", since: start });
  expect(socket.closed).toBe(true);
  for (let i = 0; i < 9; i++) {
    const retry = latest(sockets);
    retry.opened();
    expect(retry.first, "the offset is what the page has read: the record's two bytes and its ten").toEqual({ protocol: 1, resume: id, offset: 12 });
    retry.drop();
    expect(retry.closed).toBe(true);
    await vi.advanceTimersToNextTimerAsync(); // the wait before the next try
  }
  const waits = sockets.slice(1).map((retry, i, retries) => retry.at - (retries[i - 1]?.at ?? retry.at));
  expect(waits).toEqual([0, 250, 500, 1000, 2000, 4000, 5000, 5000, 5000, 5000]);
  expect(states).toHaveLength(2);
});

test("a try that gets no answer is given up after its bound, and the next follows", async () => {
  const { sockets } = await dropped();
  const unopened = latest(sockets);
  await passed(timing.connect - 1);
  expect(unopened.closed, "a WebSocket not yet open, 1 ms short of its bound").toBe(false);
  await passed(1);
  expect(unopened.closed).toBe(true);
  await passed(timing.reconnectFirst);
  const unanswered = latest(sockets);
  expect(unanswered).not.toBe(unopened);
  unanswered.opened();
  await passed(timing.answer - 1);
  expect(unanswered.closed, "a resume not yet answered, 1 ms short of its bound").toBe(false);
  await passed(1);
  expect(unanswered.closed).toBe(true);
  await passed(2 * timing.reconnectFirst);
  expect(latest(sockets)).not.toBe(unanswered);
});

test("a hidden page does not try, and tries at once when it is shown", async () => {
  const { socket, sockets, states } = await connected();
  show(false);
  socket.drop();
  await passed(60_000);
  expect(states.at(-1)).toEqual({ status: "reconnecting", since: start });
  expect(sockets, "tries while hidden").toHaveLength(1);
  show(true);
  expect(sockets, "a try at once when shown").toHaveLength(2);

  // Hidden between tries, it stops; Try now and a wake skip the backoff.
  latest(sockets).drop();
  show(false);
  await passed(60_000);
  expect(sockets).toHaveLength(2);
  show(true);
  expect(sockets).toHaveLength(3);
  for (let i = 0; i < 6; i++) {
    latest(sockets).drop();
    await passed(timing.reconnectMax);
  }
  latest(sockets).drop(); // the next try is 5 s away
  const before = sockets.length;
  window.dispatchEvent(new Event("online"));
  expect(sockets, "a try at once when back online").toHaveLength(before + 1);
});

test("the person's Try now skips the backoff", async () => {
  const { connection, sockets } = await dropped();
  for (let i = 0; i < 6; i++) {
    latest(sockets).drop();
    await passed(timing.reconnectMax);
  }
  latest(sockets).drop();
  const before = sockets.length;
  connection.tryNow();
  expect(sockets).toHaveLength(before + 1);
  connection.tryNow();
  expect(sockets, "a second Try now while the first is under way").toHaveLength(before + 1);
});

test("the page gives up 15 minutes after the loss", async () => {
  const { sockets, states } = await dropped();
  const answerNothing = setInterval(() => latest(sockets).drop(), 1_000);
  await passed(timing.grace - 1);
  expect(states.at(-1), "1 ms short of the grace").toEqual({ status: "reconnecting", since: start });
  await passed(1);
  clearInterval(answerNothing);
  expect(states.at(-1)).toEqual({ status: "ended", ending: { kind: "lost", at: start, message: "" } });
  expect(kept()).toEqual([]);
  const tried = sockets.length;
  show(true);
  await passed(timing.grace);
  expect(sockets, "tries after giving up").toHaveLength(tried);

  // A hidden page gives up too, without a try.
  const hidden = await connected();
  show(false);
  const lost = Date.now();
  hidden.socket.drop();
  await passed(timing.grace - 1);
  expect(hidden.states.at(-1)).toEqual({ status: "reconnecting", since: lost });
  await passed(1);
  expect(hidden.states.at(-1)).toEqual({ status: "ended", ending: { kind: "lost", at: lost, message: "" } });
  expect(hidden.sockets).toHaveLength(1);
});

test("a refused resume ends the SSH Session with the Gateway's reason, and nothing more is tried", async () => {
  const { sockets, states } = await dropped();
  const retry = latest(sockets);
  retry.opened();
  retry.answer({ refused: "resume_refused", message: "web1 closed the connection" });
  await passed(timing.grace);
  expect(states.at(-1)).toEqual({ status: "ended", ending: { kind: "lost", at: start, message: "web1 closed the connection" } });
  expect(retry.closed).toBe(true);
  expect(sockets).toHaveLength(2);
  expect(kept()).toEqual([]);

  // An id the Gateway holds nothing of is refused without a reason.
  const unknown = await dropped();
  latest(unknown.sockets).opened();
  latest(unknown.sockets).answer({ refused: "resume_refused", message: "" });
  expect(unknown.states.at(-1)).toMatchObject({ status: "ended", ending: { kind: "lost", message: "" } });

  // The shell's exit, read before the loss, says more than the refusal.
  const exited = await connected();
  exited.socket.records({ type: "exitStatus", status: 0 });
  await passed();
  exited.socket.drop(); // before the close frame
  latest(exited.sockets).opened();
  latest(exited.sockets).answer({ refused: "resume_refused", message: "web1 closed the connection" });
  expect(exited.states.at(-1)).toEqual({ status: "ended", ending: { kind: "exitStatus", status: 0 } });

  // The web front's own refusal, as of a Sign-in that ended meanwhile.
  const signedOut = await dropped();
  latest(signedOut.sockets).opened();
  latest(signedOut.sockets).answer({ refused: "signin_required" });
  await passed(timing.grace);
  expect(signedOut.states.at(-1)).toEqual({ status: "refused", code: "signin_required", message: "" });
  expect(signedOut.sockets).toHaveLength(2);
});

test("a Resume carries on byte for byte: keys typed meanwhile go once, and the size goes again", async () => {
  const { connection, socket, sockets, states, output } = await connected();
  connection.send(bytes("abc")); // a record of five bytes, of which the Gateway reads two before the drop
  socket.records({ type: "data", data: bytes("seen") });
  await passed();
  socket.frames({ kind: "data", ack: 0, data: encodeRecord({ type: "data", data: bytes("unseen") }).slice(0, 4) });
  socket.drop();
  await passed();
  connection.send(bytes("typed meanwhile"));
  connection.resize(100, 40);

  const next = latest(sockets);
  next.opened();
  expect(next.first).toEqual({ protocol: 1, resume: id, offset: 10 }); // the whole record read, and half of the next
  next.answer({ offset: 2 });
  await passed();
  expect(states.at(-1)).toEqual({ status: "connected" });
  expect([...next.dataSent.slice(0, 3)], "resent from the Gateway's offset, inside the record").toEqual([...bytes("abc")]);
  const rest = new FakeSocket();
  rest.send("");
  rest.send(Uint8Array.from(encodeFrame({ kind: "data", ack: 0, data: next.dataSent.slice(3) })));
  expect(rest.recordsSent).toEqual(["data typed meanwhile", "resize 100x40", "resize 100x40"]);

  // The Gateway resends from the page's offset, and the output carries on.
  next.frames({ kind: "data", ack: 0, data: encodeRecord({ type: "data", data: bytes("unseen") }).slice(4) });
  await passed();
  expect(output).toEqual(["seen", "unseen"]);

  // The liveness watch is on the new WebSocket.
  await passed(timing.dead);
  expect(states.at(-1)).toMatchObject({ status: "reconnecting" });
});

test("a resume answered with an offset the page cannot serve ends the SSH Session", async () => {
  const { sockets, states } = await dropped();
  latest(sockets).opened();
  latest(sockets).answer({ offset: 99 });
  expect(states.at(-1)).toMatchObject({ status: "ended", ending: { kind: "protocol", message: expect.stringContaining("offset") } });
  expect(latest(sockets).framesSent).toMatchObject([{ kind: "close" }]);
  expect(latest(sockets).closed).toBe(true);
});

test("the person ending the SSH Session sends the stream's close, then closes the WebSocket", async () => {
  const { connection, socket, sockets, states } = await connected();
  connection.send(bytes("exit\r"));
  connection.end();
  expect(socket.framesSent.at(-1)).toEqual({ kind: "close", ack: 0, reason: "" });
  expect(socket.recordsSent, "what was typed before goes before the close").toEqual(["data exit\r"]);
  expect(socket.closed).toBe(true);
  expect(states.at(-1)).toEqual({ status: "ended", ending: { kind: "closed" } });
  expect(kept()).toEqual([]);
  await passed(timing.grace);
  expect(sockets).toHaveLength(1);
  expect(states).toHaveLength(2);

  // Before the koko server answered, there is no SSH Session to close yet.
  const early = await opening();
  latest(early.sockets).opened();
  early.connection.end();
  expect(latest(early.sockets).closed).toBe(true);
  expect(early.states).toEqual([{ status: "ended", ending: { kind: "closed" } }]);
});

test("ending an SSH Session while reconnecting asks the koko server to end it", async () => {
  const { connection, sockets, states } = await dropped();
  const retry = latest(sockets);
  connection.end();
  expect(states.at(-1)).toEqual({ status: "ended", ending: { kind: "closed" } });
  expect(retry.closed, "the try to Resume under way").toBe(true);
  const ending = latest(sockets);
  expect(ending).not.toBe(retry);
  ending.opened();
  expect(ending.first).toEqual({ protocol: 1, end: id });
  expect(kept(), "kept until the koko server has ended it").toEqual([id]);
  ending.answer({ ended: true });
  expect(kept()).toEqual([]);
  expect(ending.closed).toBe(true);
});

test("a page the browser leaves closes its SSH Sessions, and keeps their ids for the next page", async () => {
  const { socket, states } = await connected();
  pagehide(true); // kept for the back-forward cache
  expect(states).toEqual([{ status: "connected" }]);
  expect(socket.closed).toBe(false);

  pagehide(false);
  expect(socket.framesSent.at(-1)).toEqual({ kind: "close", ack: 0, reason: "" });
  expect(socket.closed).toBe(true);
  expect(kept(), "the close may never leave the browser").toEqual([id]);
});

test("a new page ends the SSH Sessions the page before it left, then opens its own", async () => {
  // The page before: two terminals, and a reload.
  const before = newTab();
  const [first, second] = ["1".repeat(32), "2".repeat(32)];
  for (const tunnel of [first, second]) {
    await opening(before);
    latest(before.sockets).opened();
    latest(before.sockets).answer({ tunnel });
  }
  pagehide(false);
  expect(kept()).toEqual([first, second]);
  await passed(); // the browser lets the leaving document's locks go

  const after = terminals();
  const page = await opening(after);
  await opening(after);
  expect(after.sockets, "one WebSocket for each kept id, and none yet for an open").toHaveLength(2);
  for (const socket of after.sockets) {
    socket.opened();
  }
  expect(after.sockets.map((socket) => socket.first)).toEqual([
    { protocol: 1, end: first },
    { protocol: 1, end: second },
  ]);
  after.sockets[0]?.answer({ ended: true });
  await passed(1_000);
  expect(after.sockets, "an open before every kept id is ended").toHaveLength(2);
  expect(kept()).toEqual([second]);
  after.sockets[1]?.answer({ ended: true });
  await passed();
  expect(after.sockets.slice(0, 2).every((socket) => socket.closed)).toBe(true);
  expect(after.sockets, "both terminals open once the old SSH Sessions are ended").toHaveLength(4);
  after.sockets[2]?.opened();
  expect(after.sockets[2]?.first).toEqual({ protocol: 1, open: "web1", cols: 120, rows: 36 });
  after.sockets[2]?.answer({ tunnel: id });
  await passed();
  expect(page.state()).toEqual({ status: "connected" });
  expect(kept()).toEqual([id]);

  // A terminal opened later ends nothing: the ids kept now are this page's.
  await opening(after);
  latest(after.sockets).opened();
  expect(latest(after.sockets).first).toMatchObject({ open: "web1" });
});

test("a kept id the koko server does not answer for is kept, and the page opens all the same", async () => {
  sessionStorage.setItem(keptKey, JSON.stringify(["1".repeat(32), "2".repeat(32), "3".repeat(32)]));
  const { sockets, states } = await opening(terminals());
  expect(sockets).toHaveLength(3);
  sockets[0]?.opened(); // and never answered
  sockets[1]?.drop();
  sockets[2]?.opened();
  sockets[2]?.answer({ refused: "signin_required" }); // this page can end nothing more for it
  await passed(timing.connect);
  expect(sockets[0]?.closed).toBe(true);
  expect(sockets, "the open, once each end was answered or given up").toHaveLength(4);
  expect(kept()).toEqual(["1".repeat(32), "2".repeat(32)]);
  latest(sockets).opened();
  latest(sockets).answer({ tunnel: id });
  await passed();
  expect(states).toEqual([{ status: "connected" }]);

  // What sessionStorage holds that is no list of ids is none.
  sessionStorage.setItem(keptKey, "{");
  const unreadable = await opening(terminals());
  latest(unreadable.sockets).opened();
  expect(latest(unreadable.sockets).first).toMatchObject({ open: "web1" });
});

test("a document holds a Web Lock for each SSH Session it holds, until the SSH Session ends", async () => {
  const { connection } = await connected();
  expect([...locks.held]).toEqual([`koko.terminal.${id}`]);
  connection.end();
  await passed();
  expect([...locks.held]).toEqual([]);
});

test("a browser tab duplicated from a live one leaves that one's SSH Sessions alone, and opens its own", async () => {
  // The browser tab duplicated: the copy starts with its sessionStorage,
  // which keeps the id of an SSH Session the original still holds.
  const original = await connected();
  expect(kept()).toEqual([id]);

  const copy = terminals();
  const page = await opening(copy);
  expect(copy.sockets, "no end for the original's SSH Session").toHaveLength(1);
  latest(copy.sockets).opened();
  expect(latest(copy.sockets).first).toEqual({ protocol: 1, open: "web1", cols: 120, rows: 36 });
  expect(kept(), "the copy forgets the original's id").toEqual([]);
  const own = "f".repeat(32);
  latest(copy.sockets).answer({ tunnel: own });
  await passed();
  expect(page.state()).toEqual({ status: "connected" });
  expect(kept()).toEqual([own]);

  expect(original.socket.closed).toBe(false);
  expect(original.socket.framesSent).toEqual([]);
  expect(original.state()).toEqual({ status: "connected" });

  // Once the original lets its SSH Session go, as a browser tab closed whose
  // close may never have arrived, the copy makes sure it is ended.
  const sockets = copy.sockets.length;
  original.connection.end();
  await passed();
  expect(copy.sockets).toHaveLength(sockets + 1);
  latest(copy.sockets).opened();
  expect(latest(copy.sockets).first).toEqual({ protocol: 1, end: id });
});

test("a document that cannot tell whose the kept ids are ends none of them and forgets them", async () => {
  withoutLocks();
  sessionStorage.setItem(keptKey, JSON.stringify(["1".repeat(32)]));
  const { sockets } = await opening(terminals());
  expect(sockets).toHaveLength(1);
  latest(sockets).opened();
  expect(latest(sockets).first).toMatchObject({ open: "web1" });
  expect(kept()).toEqual([]);
});

test("a reload whose earlier document still held its locks when asked ends its SSH Sessions once they are let go", async () => {
  await connected();
  pagehide(false); // the next document asks before the browser lets this one's locks go

  const after = terminals();
  await opening(after);
  const firsts = after.sockets.map((socket) => {
    socket.opened();
    return socket.first;
  });
  expect(firsts).toEqual([
    { protocol: 1, open: "web1", cols: 120, rows: 36 },
    { protocol: 1, end: id },
  ]);
});
