import { afterEach, beforeEach, expect, test, vi } from "vitest";

import { Attachment, type AttachmentState, type Colors, type Size, type TerminalSocket } from "./connection";
import { encodeRecord, RecordReader, type TerminalRecord } from "./records";
import { encodeFrame, type Frame, FrameReader } from "./stream";
import { timing } from "./timing";
import { personActed, resetWorkspace, runtime } from "./workspace";

// A page's attachment to one SSH Session of its Workspace, against a fake
// WebSocket the test plays the koko server on, and on a fake clock.

const text = (bytes: Uint8Array) => new TextDecoder().decode(bytes);
const bytes = (s: string) => new TextEncoder().encode(s);

const start = Date.parse("2026-11-13T12:00:00Z");
const session = "0123456789abcdef0123456789abcdef";
const attachment = "fedcba9876543210fedcba9876543210";
const colors: Colors = { foreground: [0x1d, 0x1f, 0x27], background: [0xff, 0xff, 0xff] };

beforeEach(() => {
  vi.useFakeTimers({ now: start });
});

afterEach(() => {
  vi.useRealTimers();
  show(true);
  resetWorkspace();
});

/** show makes the page shown or hidden, and tells it so. */
function show(visible: boolean): void {
  Object.defineProperty(document, "visibilityState", { configurable: true, value: visible ? "visible" : "hidden" });
  document.dispatchEvent(new Event("visibilitychange"));
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
    return named(reader);
  }
}

/** named names each record a reader holds, as the tests compare them. */
function named(reader: RecordReader): string[] {
  const records: string[] = [];
  for (let record = reader.next(); record; record = reader.next()) {
    switch (record.type) {
      case "data":
        records.push(`data ${text(record.data)}`);
        break;
      case "resize":
        records.push(`resize ${record.columns}x${record.rows}`);
        break;
      case "colors":
        records.push(`colors ${record.foreground.join(",")} ${record.background.join(",")}`);
        break;
      default:
        records.push(record.type);
    }
  }
  return records;
}

/** attaching attaches to the SSH Session and returns the attachment before the koko server has answered. */
async function attaching(size: Size = { columns: 120, rows: 36 }, withColors?: Colors) {
  const sockets: FakeSocket[] = [];
  const states: AttachmentState[] = [];
  const output: string[] = [];
  const resets: Size[] = [];
  const actives: boolean[] = [];
  const attached = new Attachment(
    session,
    size,
    withColors,
    {
      onState: (state) => states.push(state),
      onOutput: (data) => void output.push(text(data)),
      onReset: (grid) => resets.push(grid),
      onActive: (active) => actives.push(active),
    },
    () => {
      const socket = new FakeSocket();
      sockets.push(socket);
      return socket;
    },
  );
  await passed();
  return { attachment: attached, sockets, states, output, resets, actives };
}

/** attached is an attachment the koko server has taken. */
async function attached(withColors?: Colors) {
  const page = await attaching(undefined, withColors);
  const socket = page.sockets.at(-1) as FakeSocket;
  socket.opened();
  socket.answer({ attachment });
  await passed();
  return { ...page, socket };
}

/** latest is the WebSocket the page opened last. */
const latest = (sockets: FakeSocket[]) => sockets.at(-1) as FakeSocket;

test("an attach names the SSH Session and the box, and its answer starts the stream with the theme's colours", async () => {
  const { attachment: page, sockets, states } = await attaching({ columns: 100, rows: 30 }, colors);
  expect(page.state).toEqual({ status: "attaching" });
  const socket = latest(sockets);
  expect(socket.first, "nothing is sent before the WebSocket is open").toBeUndefined();
  page.resize({ columns: 90, rows: 20 }); // the box, which changed before the WebSocket opened
  socket.opened();
  expect(socket.binaryType).toBe("arraybuffer");
  expect(socket.first).toEqual({ protocol: 2, attach: session, cols: 90, rows: 20 });
  socket.answer({ attachment });
  await passed();
  expect(states).toEqual([{ status: "attached" }]);
  expect(socket.recordsSent, "the colours, and no active: attaching is no person's act").toEqual(["colors 29,31,39 255,255,255"]);

  page.setColors({ foreground: [1, 2, 3], background: [4, 5, 6] });
  expect(socket.recordsSent.at(-1), "the theme changed").toBe("colors 1,2,3 4,5,6");
});

test("a box past the koko server's cap is held to it", async () => {
  const { sockets } = await attaching({ columns: 900, rows: 0 });
  latest(sockets).opened();
  expect(latest(sockets).first).toEqual({ protocol: 2, attach: session, cols: 500, rows: 1 });
});

test("an SSH Session that is gone, and each refusal of the web front, end the attachment, and nothing more is tried", async () => {
  const refusals: [string, string, AttachmentState][] = [
    ["unknown_tunnel", "", { status: "gone" }],
    ["ssh_session_ended", "", { status: "gone" }],
    ["reload_page", "", { status: "refused", code: "reload_page", message: "" }],
    ["signin_required", "", { status: "refused", code: "signin_required", message: "" }],
    ["cookie_required", "", { status: "refused", code: "cookie_required", message: "" }],
    ["revoked", "this device was revoked.", { status: "refused", code: "revoked", message: "this device was revoked." }],
  ];
  for (const [code, message, want] of refusals) {
    const { sockets, states } = await attaching();
    const socket = latest(sockets);
    socket.opened();
    socket.answer(message === "" ? { refused: code } : { refused: code, message });
    await passed(60_000);
    expect(states, code).toEqual([want]);
    expect(socket.closed, code).toBe(true);
    expect(sockets, `${code}: no further attempt`).toHaveLength(1);
  }
});

test("an attach the koko server does not answer is tried again, after 250 ms doubling", async () => {
  const { sockets, states } = await attaching();
  latest(sockets).drop();
  await passed();
  expect(sockets).toHaveLength(1);
  await passed(timing.reconnectFirst);
  expect(sockets).toHaveLength(2);
  latest(sockets).opened();
  latest(sockets).answer({ neither: true });
  await passed(2 * timing.reconnectFirst);
  expect(sockets).toHaveLength(3);
  latest(sockets).opened();
  expect(latest(sockets).first).toEqual({ protocol: 2, attach: session, cols: 120, rows: 36 });
  latest(sockets).answer({ attachment });
  await passed();
  expect(states).toEqual([{ status: "attached" }]);
});

test("reset clears the emulator to the pty's size before the snapshot, and the active record says whose box it is", async () => {
  const { socket, output, resets, actives } = await attached();
  socket.records(
    { type: "reset", columns: 80, rows: 24 },
    { type: "data", data: bytes("snapshot") },
    { type: "active", active: false },
    { type: "data", data: bytes("live") },
  );
  await passed();
  expect(resets).toEqual([{ columns: 80, rows: 24 }]);
  expect(output).toEqual(["snapshot", "live"]);
  expect(actives).toEqual([false]);
  socket.records({ type: "active", active: true }, { type: "reset", columns: 200, rows: 50 }, { type: "data", data: bytes("again") });
  await passed();
  expect(actives).toEqual([false, true]);
  expect(resets.at(-1)).toEqual({ columns: 200, rows: 50 });
  expect(output.at(-1)).toBe("again");
});

test("the shell's output reaches the page in order, and what the page sends reaches the shell as records", async () => {
  const { attachment: page, socket, output } = await attached();
  socket.records({ type: "data", data: bytes("$ ") }, { type: "data", data: bytes("ls\r\n") });
  await passed();
  expect(output).toEqual(["$ ", "ls\r\n"]);

  page.send(bytes("ls\r"));
  page.send(new Uint8Array(0));
  page.resize({ columns: 80, rows: 24 });
  page.active();
  expect(socket.recordsSent).toEqual(["data ls\r", "resize 80x24", "active"]);

  const pasted = new Uint8Array(40_000).fill(0x61);
  page.send(pasted);
  const records = socket.recordsSent.slice(3);
  expect(records.map((record) => record.length - "data ".length), "a paste cut at a record's most").toEqual([32_768, 7_232]);
});

test("the page reads on only once it has written the output into the emulator", async () => {
  const sockets: FakeSocket[] = [];
  const written: (() => void)[] = [];
  const output: string[] = [];
  new Attachment(
    session,
    { columns: 80, rows: 24 },
    undefined,
    {
      onState: () => {},
      onOutput: (data) => {
        output.push(text(data));
        return new Promise<void>((resolve) => written.push(resolve));
      },
      onReset: () => {},
      onActive: () => {},
    },
    () => {
      const socket = new FakeSocket();
      sockets.push(socket);
      return socket;
    },
  );
  await passed();
  const socket = latest(sockets);
  socket.opened();
  socket.answer({ attachment });
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

test("the koko server closing the attachment ends it with its notice, and nothing is resumed", async () => {
  const { socket, sockets, states } = await attached();
  socket.records({ type: "data", data: bytes("logout\r\n") }, { type: "exitStatus", status: 0 });
  socket.frames({ kind: "close", ack: 0, reason: "the koko server is restarting; this SSH Session has ended." });
  socket.drop(); // the Gateway closes the WebSocket behind its close frame
  await passed(60_000);
  expect(states.at(-1)).toEqual({ status: "detached", notice: "the koko server is restarting; this SSH Session has ended." });
  expect(socket.closed).toBe(true);
  expect(sockets).toHaveLength(1);
});

test("a page displaced by a ninth is told so, and attaches again only when asked", async () => {
  const { socket, sockets, states } = await attached();
  socket.records({ type: "active", active: false }, { type: "displaced" });
  socket.frames({ kind: "close", ack: 0, reason: "" });
  socket.drop();
  await passed(60_000);
  expect(states.at(-1)).toEqual({ status: "displaced" });
  expect(sockets, "no attach of its own, which would displace another").toHaveLength(1);
});

test("bytes that are not the protocol end the attachment, telling the koko server why", async () => {
  const broken: [string, (socket: FakeSocket) => void][] = [
    ["an unknown record", (socket) => socket.frames({ kind: "data", ack: 0, data: Uint8Array.of(0x7f, 0x00) })],
    ["a resize from the koko server", (socket) => socket.records({ type: "resize", columns: 80, rows: 24 })],
    ["the page's own active", (socket) => socket.records({ type: "active" })],
    ["colours from the koko server", (socket) => socket.records({ type: "colors", ...colors })],
    ["a frame acknowledging what was never sent", (socket) => socket.frames({ kind: "data", ack: 9, data: new Uint8Array(0) })],
  ];
  for (const [what, act] of broken) {
    const { socket, states } = await attached();
    act(socket);
    await passed();
    expect(states.at(-1), what).toMatchObject({ status: "protocol" });
    expect(socket.framesSent.at(-1), what).toMatchObject({ kind: "close", reason: expect.stringContaining("malformed") });
    expect(socket.closed, what).toBe(true);
  }
});

test("the page probes after 15 s without sending, and takes 45 s of silence for a dead WebSocket", async () => {
  const { attachment: page, socket, sockets, states } = await attached();
  await passed(timing.probe - 1);
  expect(socket.framesSent).toEqual([]);
  await passed(1);
  expect(socket.framesSent, "the probe").toEqual([{ kind: "data", ack: 0, data: new Uint8Array(0) }]);

  // What the page sends puts its next probe off; what it hears keeps the
  // WebSocket alive, and only that.
  await passed(5_000); // 20 s
  page.send(bytes("x"));
  socket.frames({ kind: "data", ack: 0, data: new Uint8Array(0) }); // the Gateway's own probe
  await passed(timing.probe - 1);
  expect(socket.framesSent).toHaveLength(2);
  await passed(1); // 35 s
  expect(socket.framesSent).toHaveLength(3);
  await passed(timing.dead - timing.probe - 1); // 1 ms short of 45 s after the Gateway's probe
  expect(states).toEqual([{ status: "attached" }]);
  await passed(1);
  expect(states.at(-1)).toEqual({ status: "reconnecting", since: start + 20_000 + timing.dead });
  expect(socket.closed).toBe(true);
  expect(latest(sockets)).not.toBe(socket);
});

test("a page shown again, or back online, takes a WebSocket silent for 15 s for dead at once", async () => {
  const wakes: [string, () => void][] = [
    ["visibilitychange", () => show(true)],
    ["pageshow", () => window.dispatchEvent(new Event("pageshow"))],
    ["online", () => window.dispatchEvent(new Event("online"))],
  ];
  for (const [what, wake] of wakes) {
    const { socket, sockets, states, attachment: page } = await attached();
    await passed(timing.probe - 1);
    wake();
    expect(states, `${what}, 1 ms short of 15 s of silence`).toEqual([{ status: "attached" }]);
    await passed(1);
    wake();
    await passed();
    expect(states.at(-1), what).toEqual({ status: "reconnecting", since: Date.now() });
    expect(socket.closed, what).toBe(true);
    latest(sockets).opened();
    expect(latest(sockets).first, what).toEqual({ protocol: 2, resume: attachment, offset: 0 });
    page.detach();
  }
});

/** dropped is an attachment whose WebSocket has just closed, and its first try to resume. */
async function dropped() {
  const page = await attached();
  page.socket.records({ type: "data", data: bytes("0123456789") });
  await passed();
  page.socket.drop();
  await passed();
  return page;
}

test("a dropped WebSocket is resumed at once, then after 250 ms doubling to 5 s, for as long as it takes", async () => {
  const { socket, sockets, states } = await dropped();
  expect(states.at(-1)).toEqual({ status: "reconnecting", since: start });
  expect(socket.closed).toBe(true);
  for (let i = 0; i < 9; i++) {
    const retry = latest(sockets);
    retry.opened();
    expect(retry.first, "the offset is what the page has read: the record's two bytes and its ten").toEqual({ protocol: 2, resume: attachment, offset: 12 });
    retry.drop();
    expect(retry.closed).toBe(true);
    await vi.advanceTimersToNextTimerAsync(); // the wait before the next try
  }
  const waits = sockets.slice(1).map((retry, i, retries) => retry.at - (retries[i - 1]?.at ?? retry.at));
  expect(waits).toEqual([0, 250, 500, 1000, 2000, 4000, 5000, 5000, 5000, 5000]);
  await passed(60 * 60_000);
  expect(states, "no grace on the page: the Gateway says when the attachment is gone").toHaveLength(2);
});

test("a hidden page does not try, and tries at once when it is shown; Try now skips the backoff", async () => {
  const { attachment: page, socket, sockets, states } = await attached();
  show(false);
  socket.drop();
  await passed(60_000);
  expect(states.at(-1)).toEqual({ status: "reconnecting", since: start });
  expect(sockets, "tries while hidden").toHaveLength(1);
  show(true);
  expect(sockets, "a try at once when shown").toHaveLength(2);
  for (let i = 0; i < 6; i++) {
    latest(sockets).drop();
    await passed(timing.reconnectMax);
  }
  latest(sockets).drop();
  const before = sockets.length;
  page.tryNow();
  expect(sockets).toHaveLength(before + 1);
  page.tryNow();
  expect(sockets, "a second Try now while the first is under way").toHaveLength(before + 1);
});

test("a resume carries on byte for byte: keys typed meanwhile go once, the size goes again, and no active", async () => {
  const { attachment: page, socket, sockets, states, output } = await attached();
  page.send(bytes("abc")); // a record of five bytes, of which the Gateway reads two before the drop
  socket.records({ type: "data", data: bytes("seen") });
  await passed();
  socket.frames({ kind: "data", ack: 0, data: encodeRecord({ type: "data", data: bytes("unseen") }).slice(0, 4) });
  socket.drop();
  await passed();
  page.send(bytes("typed meanwhile"));
  page.resize({ columns: 100, rows: 40 });

  const next = latest(sockets);
  next.opened();
  expect(next.first).toEqual({ protocol: 2, resume: attachment, offset: 10 }); // the whole record read, and half of the next
  next.answer({ offset: 2 });
  await passed();
  expect(states.at(-1)).toEqual({ status: "attached" });
  expect([...next.dataSent.slice(0, 3)], "resent from the Gateway's offset, inside the record").toEqual([...bytes("abc")]);
  const rest = new RecordReader();
  rest.push(next.dataSent.slice(3));
  expect(named(rest)).toEqual(["data typed meanwhile", "resize 100x40", "resize 100x40"]);

  // The Gateway resends from the page's offset, and the output carries on.
  next.frames({ kind: "data", ack: 0, data: encodeRecord({ type: "data", data: bytes("unseen") }).slice(4) });
  await passed();
  expect(output).toEqual(["seen", "unseen"]);
});

test("a resume the Gateway refuses attaches again, and the new snapshot draws the screen", async () => {
  const { sockets, states, resets, output } = await dropped();
  const retry = latest(sockets);
  retry.opened();
  retry.answer({ refused: "resume_refused", message: "" });
  await passed();
  const again = latest(sockets);
  expect(again).not.toBe(retry);
  again.opened();
  expect(again.first).toEqual({ protocol: 2, attach: session, cols: 120, rows: 36 });
  expect(states.at(-1), "still reconnecting while it attaches again").toMatchObject({ status: "reconnecting" });
  again.answer({ attachment: "11111111111111111111111111111111" });
  await passed();
  again.records({ type: "reset", columns: 120, rows: 36 }, { type: "data", data: bytes("the screen now") });
  await passed();
  expect(states.at(-1)).toEqual({ status: "attached" });
  expect(resets).toEqual([{ columns: 120, rows: 36 }]);
  expect(output.at(-1)).toBe("the screen now");
  expect(again.recordsSent, "no active on attaching again").toEqual([]);

  // Its next drop resumes the new attachment.
  again.drop();
  await passed();
  latest(sockets).opened();
  expect(latest(sockets).first).toMatchObject({ resume: "11111111111111111111111111111111" });
});

test("a person's act while reconnecting is told once the attachment is resumed", async () => {
  const { attachment: page, sockets } = await dropped();
  page.active();
  const next = latest(sockets);
  next.opened();
  next.answer({ offset: 0 });
  await passed();
  expect(next.recordsSent).toEqual(["active", "resize 120x36"]);
});

test("active is sent only for a person's act in a visible page", async () => {
  const { attachment: page, socket } = await attached();
  runtime("tab1").attachment = page;
  show(false);
  personActed("tab1");
  expect(socket.recordsSent, "a hidden page").toEqual([]);
  show(true);
  personActed("tab1");
  expect(socket.recordsSent).toEqual(["active"]);
});

test("detaching sends the stream's close, then closes the WebSocket, and tries nothing more", async () => {
  const { attachment: page, socket, sockets, states } = await attached();
  page.detach();
  expect(socket.framesSent).toEqual([{ kind: "close", ack: 0, reason: "" }]);
  expect(socket.closed).toBe(true);
  await passed(60_000);
  expect(sockets).toHaveLength(1);
  expect(states).toEqual([{ status: "attached" }]);
});
