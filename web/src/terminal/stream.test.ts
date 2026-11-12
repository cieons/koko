import { expect, test } from "vitest";

import framesGolden from "../../../internal/shared/resumable/testdata/frames.golden?raw";
import terminalGolden from "../../../internal/shared/wire/testdata/terminal.golden?raw";
import { readUvarint, uvarint } from "./bytes";
import { encodeRecord, MalformedRecord, RecordReader, type TerminalRecord, terminalProtocol } from "./records";
import { encodeFrame, type Frame, FrameReader, maxBuffer, maxData, Stream, StreamError, type StreamSocket } from "./stream";

// The resumable stream's TypeScript end, against the Go end's golden files
// and over in-memory sockets, as internal/shared/resumable's tests run the
// Go end over in-memory channels.

const text = (bytes: Uint8Array) => new TextDecoder().decode(bytes);
const bytes = (s: string) => new TextEncoder().encode(s);
const hex = (b: Uint8Array) => [...b].map((byte) => byte.toString(16).padStart(2, "0")).join(" ");
const unhex = (s: string) => Uint8Array.from(s.split(" "), (byte) => Number.parseInt(byte, 16));

/** section is the lines of the golden file's section of that name. */
function section(golden: string, name: string): string[] {
  const lines = golden.split("\n");
  const start = lines.indexOf(`== ${name}`);
  expect(start, `the golden file's section ${name}`).toBeGreaterThanOrEqual(0);
  const rest = lines.slice(start + 1);
  const end = rest.findIndex((line) => line.startsWith("== "));
  return (end < 0 ? rest : rest.slice(0, end)).filter((line) => line !== "");
}

/** decodeFrame reads one frame from b, or says it is malformed. */
function decodeFrame(b: Uint8Array): Frame | "malformed" {
  const reader = new FrameReader();
  reader.push(b);
  try {
    const frame = reader.next();
    expect(frame, `a whole frame in ${hex(b)}`).not.toBeNull();
    return frame as Frame;
  } catch (err) {
    expect(err).toMatchObject({ kind: "malformed" });
    return "malformed";
  }
}

// The frames of tunnel@koko revision 1, which the terminal's protocol
// carries: a change to the Go end's frames changes frames.golden, and
// fails here until this end matches.
test("the frames are those the Go end records", () => {
  const lines = section(framesGolden, "tunnel@koko 1");
  expect(lines.length).toBeGreaterThan(0);
  for (const line of lines) {
    let m: RegExpExecArray | null;
    if ((m = /^data, ack (\d+), "(.*)": (.+)$/.exec(line))) {
      const frame: Frame = { kind: "data", ack: Number(m[1]), data: bytes(m[2] ?? "") };
      expect(hex(encodeFrame(frame)), line).toBe(m[3]);
      const read = decodeFrame(unhex(m[3] ?? ""));
      expect(read, line).toMatchObject({ kind: "data", ack: frame.ack });
      expect(read !== "malformed" && read.kind === "data" && text(read.data), line).toBe(m[2]);
    } else if ((m = /^eof, ack (\d+): (.+)$/.exec(line))) {
      const frame: Frame = { kind: "eof", ack: Number(m[1]) };
      expect(hex(encodeFrame(frame)), line).toBe(m[2]);
      expect(decodeFrame(unhex(m[2] ?? "")), line).toEqual(frame);
    } else if ((m = /^close, ack (\d+), "(.*)": (.+)$/.exec(line))) {
      const frame: Frame = { kind: "close", ack: Number(m[1]), reason: m[2] ?? "" };
      expect(hex(encodeFrame(frame)), line).toBe(m[3]);
      expect(decodeFrame(unhex(m[3] ?? "")), line).toEqual(frame);
    } else if ((m = /^resume answer, offset (\d+): (.+)$/.exec(line))) {
      // The terminal carries each end's offset in its first messages' JSON;
      // the uvarint is the one every frame's fields are written in.
      expect(hex(Uint8Array.from(uvarint(Number(m[1])))), line).toBe(m[2]);
      expect(readUvarint(unhex(m[2] ?? ""), 0), line).toMatchObject({ value: Number(m[1]) });
    } else if ((m = /^reads size (\d+) as: (\w+)$/.exec(line))) {
      const read = decodeFrame(Uint8Array.from([0, ...uvarint(Number(m[1])), 0]));
      expect(read === "malformed" ? read : read.kind, line).toBe(m[2]);
    } else {
      expect.unreachable(`frames.golden has a line this test does not know: ${line}`);
    }
  }
});

/** described names a record as terminal.golden does. */
function described(record: TerminalRecord): string {
  switch (record.type) {
    case "data":
      return `data ${JSON.stringify(text(record.data))}`;
    case "resize":
      return `resize ${record.columns}x${record.rows}`;
    case "exitStatus":
      return `exit status ${record.status}`;
    case "exitSignal":
      return `exit signal ${record.signal}`;
  }
}

test("the terminal records are those the Go end records", () => {
  const lines = section(terminalGolden, `terminal ${terminalProtocol}`);
  expect(lines.length).toBeGreaterThan(0);
  expect(lines.length % 2, "each record's line is followed by what it is read as").toBe(0);
  for (let i = 0; i < lines.length; i += 2) {
    const line = lines[i] ?? "";
    const colon = line.lastIndexOf(": ");
    const [name, encoded] = [line.slice(0, colon), line.slice(colon + 2)];
    const readAs = /^ {2}read as: (.+)$/.exec(lines[i + 1] ?? "")?.[1];
    expect(readAs, `what ${line} is read as`).toBeDefined();

    const reader = new RecordReader();
    reader.push(unhex(encoded));
    if (readAs === "malformed") {
      expect(() => reader.next(), line).toThrow(MalformedRecord);
      continue;
    }
    const record = reader.next();
    expect(record, line).not.toBeNull();
    expect(described(record as TerminalRecord), line).toBe(readAs);
    expect(readAs, line).toBe(name);
    expect(hex(encodeRecord(record as TerminalRecord)), line).toBe(encoded);
    expect(reader.next(), `${line}: nothing after the record`).toBeNull();
  }
});

test("a record the decoder would refuse is not encoded", () => {
  for (const record of [
    { type: "resize", columns: 0, rows: 24 },
    { type: "resize", columns: 80, rows: 4097 },
    { type: "resize", columns: 80.5, rows: 24 },
    { type: "exitStatus", status: 2 ** 32 },
    { type: "data", data: new Uint8Array(maxData + 1) },
  ] satisfies TerminalRecord[]) {
    expect(() => encodeRecord(record), described(record).slice(0, 40)).toThrow(MalformedRecord);
  }
});

test("a record that spans the stream's reads is read once it is whole", () => {
  const reader = new RecordReader();
  const encoded = [...encodeRecord({ type: "data", data: bytes("hello") }), ...encodeRecord({ type: "exitStatus", status: 3 })];
  const read: string[] = [];
  for (const byte of encoded) {
    reader.push(Uint8Array.of(byte));
    for (let record = reader.next(); record; record = reader.next()) {
      read.push(described(record));
    }
  }
  expect(read).toEqual(['data "hello"', "exit status 3"]);
});

/** random is a seeded generator (mulberry32) of numbers in [0, 1). */
function random(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 2 ** 32;
  };
}

/**
 * Link is an in-memory socket of two ends, cut after budget bytes: a send
 * that passes the budget delivers what fits, mid-frame as it may be, and
 * fails, as every send after it does. What it carries arrives later, as a
 * WebSocket's messages do, in pieces of random sizes when given a
 * generator, since a frame may span messages.
 */
class Link {
  readonly page: End;
  readonly gateway: End;
  /** cut resolves once the link is cut. */
  readonly cut: Promise<void>;
  isCut = false;
  private markCut: () => void = () => {};
  private left: number;
  private readonly rng?: () => number;

  constructor(left = Number.POSITIVE_INFINITY, rng?: () => number) {
    this.left = left;
    this.rng = rng;
    this.cut = new Promise((resolve) => {
      this.markCut = resolve;
    });
    this.page = new End(this);
    this.gateway = new End(this);
    this.page.peer = this.gateway;
    this.gateway.peer = this.page;
  }

  carry(to: End, data: Uint8Array): void {
    if (this.isCut) {
      throw new Error("the link is cut");
    }
    const fits = Math.min(data.length, this.left);
    this.left -= fits;
    for (let at = 0; at < fits; ) {
      const size = this.rng ? 1 + Math.floor(this.rng() * 2 * maxData) : fits;
      const piece = data.slice(at, Math.min(at + size, fits));
      at += piece.length;
      queueMicrotask(() => to.onmessage?.({ data: piece.buffer } as MessageEvent));
    }
    if (fits < data.length) {
      this.sever();
      throw new Error("the link is cut");
    }
  }

  sever(): void {
    this.isCut = true;
    this.markCut();
  }
}

class End implements StreamSocket {
  onmessage: ((event: MessageEvent) => void) | null = null;
  peer!: End;
  /** sent is every message this end sent, whether or not it arrived. */
  readonly sent: Uint8Array[] = [];
  private readonly link: Link;

  constructor(link: Link) {
    this.link = link;
  }

  send(data: Uint8Array<ArrayBuffer>): void {
    this.sent.push(data.slice());
    this.link.carry(this.peer, data);
  }
}

/** open starts both ends of a new SSH Session's stream on one link. */
function open(link = new Link()): { page: Stream; gateway: Stream; link: Link } {
  return { page: new Stream(link.page), gateway: new Stream(link.gateway), link };
}

/**
 * resume carries both ends onto a new link, as the page and the Gateway
 * do: each gives its received offset, in the first messages' JSON, and
 * resumes from the other's.
 */
function resume(page: Stream, gateway: Stream, link = new Link()): Link {
  const pageReceived = page.detach();
  const gatewayReceived = gateway.detach();
  gateway.resume(link.gateway, pageReceived);
  page.resume(link.page, gatewayReceived);
  return link;
}

/** settled lets everything already on its way arrive. */
const settled = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

/** readN reads exactly n bytes from s. */
async function readN(s: Stream, n: number): Promise<Uint8Array> {
  const got = new Uint8Array(n);
  for (let at = 0; at < n; ) {
    const part = await s.read(n - at);
    if (part === null) {
      throw new Error(`the stream ended after ${at} bytes of ${n}`);
    }
    got.set(part, at);
    at += part.length;
  }
  return got;
}

/** readAll reads s up to the other end's half-close, or to why the stream ended. */
async function readAll(s: Stream): Promise<{ got: string; err: StreamError | null }> {
  const parts: number[] = [];
  try {
    for (let part = await s.read(); part !== null; part = await s.read()) {
      parts.push(...part);
    }
  } catch (err) {
    return { got: text(Uint8Array.from(parts)), err: err as StreamError };
  }
  return { got: text(Uint8Array.from(parts)), err: null };
}

/** raw starts a stream on a socket whose other end the test speaks frames on. */
function raw(): { stream: Stream; link: Link; peer: End; say: (...frames: (Frame | number[])[]) => Promise<void> } {
  const link = new Link();
  const stream = new Stream(link.page);
  const say = async (...frames: (Frame | number[])[]) => {
    for (const frame of frames) {
      link.gateway.send(Array.isArray(frame) ? Uint8Array.from(frame) : encodeFrame(frame));
    }
    await settled();
  };
  return { stream, link, peer: link.gateway, say };
}

/** framesOf decodes the frames in the messages an end sent. */
function framesOf(messages: Uint8Array[]): Frame[] {
  const reader = new FrameReader();
  const frames: Frame[] = [];
  for (const message of messages) {
    reader.push(message);
    for (let frame = reader.next(); frame; frame = reader.next()) {
      frames.push(frame.kind === "data" ? { ...frame, data: frame.data.slice() } : frame);
    }
  }
  return frames;
}

test("bytes flow both ways", async () => {
  const { page, gateway } = open();
  await Promise.all([page.write(bytes("ping")), gateway.write(bytes("pong"))]);
  expect(text(await readN(gateway, 4))).toBe("ping");
  expect(text(await readN(page, 4))).toBe("pong");
});

test("a frame that spans messages is read once it is whole", async () => {
  const { stream, peer } = raw();
  const frame = encodeFrame({ kind: "data", ack: 0, data: bytes("hello") });
  for (const byte of frame) {
    peer.send(Uint8Array.of(byte));
  }
  expect(text(await readN(stream, 5))).toBe("hello");
});

test("closeWrite ends one direction", async () => {
  const { page, gateway } = open();
  await page.write(bytes("bye"));
  await page.closeWrite();
  expect(await readAll(gateway)).toEqual({ got: "bye", err: null });
  expect(await gateway.read(), "a read after the end").toBeNull();
  await expect(page.write(bytes("more"))).rejects.toMatchObject({ kind: "writeAfterCloseWrite" });
  expect(page.error, "a write after closeWrite ends nothing").toBeNull();

  await gateway.write(bytes("still open"));
  expect(text(await readN(page, 10)), "the other direction after a closeWrite").toBe("still open");
});

test("a half-close survives a resume", async () => {
  const { page, gateway, link } = open(new Link(1)); // the data's frame is cut, the EOF's never sent
  await page.write(bytes("bye"));
  await page.closeWrite();
  await link.cut;
  resume(page, gateway);
  expect(await readAll(gateway)).toEqual({ got: "bye", err: null });

  // The EOF takes a position, so an end that read it does not get it again.
  resume(page, gateway);
  await gateway.write(bytes("ok"));
  expect(text(await readN(page, 2))).toBe("ok");
  expect(gateway.error ?? page.error).toBeNull();

  // An EOF that arrived unread is dropped with its socket, and sent again.
  await gateway.closeWrite();
  await settled();
  resume(page, gateway);
  expect(await readAll(page)).toEqual({ got: "", err: null });
  expect(gateway.error ?? page.error).toBeNull();
});

test("a close carries its reason after the data", async () => {
  const { page, gateway, link } = open();
  await page.write(bytes("last words"));
  page.close("the person ended it");
  expect(framesOf(link.page.sent).at(-1)).toEqual({ kind: "close", ack: 0, reason: "the person ended it" });

  const { got, err } = await readAll(gateway);
  expect(got).toBe("last words");
  expect(err).toMatchObject({ kind: "peerClosed", reason: "the person ended it" });
  expect(await gateway.ended).toBe(err);
  await expect(gateway.write(bytes("x"))).rejects.toMatchObject({ kind: "peerClosed" });
  await expect(page.read()).rejects.toMatchObject({ kind: "closed" });
  await expect(page.write(bytes("x"))).rejects.toMatchObject({ kind: "closed" });
  expect(page.error).toMatchObject({ kind: "closed" });
});

test("after a half-close, a read says the end and only the error tells of the close", async () => {
  const { page, gateway } = open();
  await gateway.closeWrite();
  gateway.close("done");
  expect(await readAll(page)).toEqual({ got: "", err: null });
  expect(await page.read()).toBeNull();
  expect(await page.ended).toMatchObject({ kind: "peerClosed", reason: "done" });
});

test("a close frame is never replayed: a stream closed while detached sends nothing", async () => {
  const { page, gateway } = open();
  page.detach();
  page.close("too late to say");
  const link = new Link();
  expect(() => page.resume(link.page, gateway.detach())).toThrow(expect.objectContaining({ kind: "closed" }));
  expect(link.page.sent).toEqual([]);
});

test("a close's reason is cut to the most a frame carries, at a character's edge", async () => {
  const { page, link } = open();
  page.close("好".repeat(maxData)); // three bytes each
  const frame = framesOf(link.page.sent).at(-1);
  expect(frame).toMatchObject({ kind: "close" });
  const reason = frame?.kind === "close" ? frame.reason : "";
  expect(reason).toBe("好".repeat(Math.floor(maxData / 3)));
});

test("a full buffer holds the writer back and loses nothing", async () => {
  const { page, gateway } = open();
  const fill = random(1);
  const payload = Uint8Array.from({ length: maxBuffer + 1 }, () => Math.floor(fill() * 256));

  await page.write(payload.subarray(0, maxBuffer)); // the other end does not read
  let wrote = false;
  const last = page.write(payload.subarray(maxBuffer)).then(() => {
    wrote = true;
  });
  let halfClosed = false;
  const closed = page.closeWrite().then(() => {
    halfClosed = true;
  });
  await settled();
  expect(wrote, "a write past a full buffer returned before the other end read").toBe(false);
  expect(halfClosed, "a closeWrite past a full buffer returned before the other end read").toBe(false);

  resume(page, gateway); // what the Gateway held unread is dropped and resent
  const got = await readN(gateway, payload.length);
  expect(firstDifference(got, payload)).toBe(-1);
  await last;
  await closed;
  expect(await gateway.read()).toBeNull();
});

test("a closeWrite waits for room in a full buffer too", async () => {
  const { page, gateway } = open();
  await page.write(new Uint8Array(maxBuffer));
  let halfClosed = false;
  const closed = page.closeWrite().then(() => {
    halfClosed = true;
  });
  await settled();
  expect(halfClosed, "a closeWrite on a full buffer returned before the other end read").toBe(false);
  await readN(gateway, maxBuffer);
  await closed;
  expect(await gateway.read()).toBeNull();
});

/** firstDifference is where a and b first differ, or -1 when they are equal. */
function firstDifference(a: Uint8Array, b: Uint8Array): number {
  for (let i = 0; i < Math.max(a.length, b.length); i++) {
    if (a[i] !== b[i]) {
      return i;
    }
  }
  return -1;
}

/**
 * transfer writes a payload into one end in chunks of random sizes and
 * half-closes it, and reads the other end to its end in reads of random
 * sizes.
 */
async function transfer(rng: () => number, from: Stream, to: Stream, want: Uint8Array): Promise<Uint8Array> {
  const size = () => 1 + Math.floor(rng() * 2 * maxData);
  const write = async () => {
    for (let at = 0; at < want.length; ) {
      const chunk = want.subarray(at, at + size());
      await from.write(chunk);
      at += chunk.length;
    }
    await from.closeWrite();
  };
  const read = async () => {
    const got = new Uint8Array(want.length + 1);
    let at = 0;
    for (let part = await to.read(size()); part !== null; part = await to.read(size())) {
      got.set(part.subarray(0, got.length - at), at);
      at += part.length;
    }
    return got.subarray(0, at);
  };
  const [got] = await Promise.all([read(), write()]);
  return got;
}

// A few MiB each way, more than the replay buffers hold, over links cut at
// random bytes: mid-frame, and mid-resume, when the Gateway has taken the
// resume and its answer never reaches the page. Some resumes start while
// both ends are still on a working link, so the last attach wins. Every
// byte must come out once, in order, with the end after it.
test("bytes survive any sequence of cuts and resumes", async () => {
  const seed = Number(import.meta.env.VITE_STREAM_SEED) || Date.now() >>> 0;
  console.log(`stream.test.ts: seed ${seed} (set VITE_STREAM_SEED to run it again)`);
  for (let round = 0; round < 4; round++) {
    const rng = random(seed + round);
    const upto = (n: number) => Math.floor(rng() * n);
    const { page, gateway, link } = open(new Link(upto(256 << 10), rng));
    const payload = () => Uint8Array.from({ length: (5 << 19) + upto(3 << 19) }, () => upto(256));
    const [up, down] = [payload(), payload()];
    let finished = false;
    const done = Promise.all([transfer(rng, page, gateway, up), transfer(rng, gateway, page, down)]).then((got) => {
      finished = true;
      return got;
    });

    let current = link;
    let resumes = 0;
    let answered = true;
    for (let i = 0; i < 40 && !finished; i++) {
      if (answered && upto(4) > 0) {
        await Promise.race([current.cut, done]); // else resume at once, on a working link
      } else {
        await settled();
      }
      if (finished) {
        break;
      }
      current.sever(); // the old WebSocket is closed before a new one opens
      current = new Link(upto(8) === 0 ? upto(16) : upto(256 << 10), rng);
      const pageReceived = page.detach();
      const gatewayReceived = gateway.detach();
      gateway.resume(current.gateway, pageReceived);
      // One resume in eight, the answer is lost: the Gateway is on a socket
      // the page never uses.
      answered = upto(8) > 0;
      if (answered) {
        page.resume(current.page, gatewayReceived);
        resumes++;
      }
    }
    if (!finished) {
      current.sever();
      resume(page, gateway, new Link(Number.POSITIVE_INFINITY, rng));
    }
    const [gotUp, gotDown] = await done;
    const where = `seed ${seed}, round ${round}, ${resumes} resumes`;
    expect(page.error ?? gateway.error, where).toBeNull();
    expect(gotUp.length, `page to Gateway, ${where}`).toBe(up.length);
    expect(firstDifference(gotUp, up), `page to Gateway, first different at, ${where}`).toBe(-1);
    expect(gotDown.length, `Gateway to page, ${where}`).toBe(down.length);
    expect(firstDifference(gotDown, down), `Gateway to page, first different at, ${where}`).toBe(-1);
  }
});

test("the last attach wins: the socket a stream left is neither sent on nor heard", async () => {
  const { page, gateway, link } = open();
  const next = resume(page, gateway);
  const before = link.page.sent.length;
  link.gateway.send(encodeFrame({ kind: "data", ack: 0, data: bytes("stale") }));
  await page.write(bytes("fresh"));
  expect(text(await readN(gateway, 5))).toBe("fresh");
  expect(link.page.sent.length, "frames sent on the old socket after the resume").toBe(before);
  await gateway.write(bytes("reply"));
  expect(text(await readN(page, 5)), "what the page reads after a frame on its old socket").toBe("reply");
  expect(next.isCut).toBe(false);
});

test("a probe is one acknowledgement-only frame, and none while detached", async () => {
  const { stream, peer, link } = raw();
  stream.probe();
  expect(link.page.sent.map(hex)).toEqual(["00 00"]);

  stream.detach();
  stream.probe();
  const next = new Link();
  stream.resume(next.page, 0);
  await settled();
  expect(next.page.sent, "a probe asked for while detached, sent after the resume").toEqual([]);
  expect(peer.sent).toEqual([]);

  // Nor is a probe owed on a socket whose sends fail carried onto the next.
  next.sever();
  await stream.write(bytes("x"));
  stream.probe();
  const last = new Link();
  stream.resume(last.page, 1); // the byte arrived all the same
  expect(last.page.sent, "a probe owed on a failed socket, sent after the resume").toEqual([]);
});

test("an acknowledgement goes alone once a frame's worth has been read", async () => {
  const { stream, link, say } = raw();
  await say({ kind: "data", ack: 0, data: new Uint8Array(maxData) });
  expect(link.page.sent, "before the page read anything").toEqual([]);
  await readN(stream, maxData);
  expect(framesOf(link.page.sent)).toEqual([{ kind: "data", ack: maxData, data: new Uint8Array(0) }]);
});

test("an offset outside the replay buffer ends the stream, and tells the other end", async () => {
  for (const [what, offset] of [
    ["more than this end sent", 3],
    ["what this end let go of", 0],
    ["no offset at all", 1.5],
  ] as const) {
    const { stream, say } = raw();
    await stream.write(bytes("hi"));
    await say({ kind: "data", ack: 1, data: new Uint8Array(0) }); // the first byte is acknowledged and let go
    stream.detach();
    const next = new Link();
    expect(() => stream.resume(next.page, offset), what).toThrow(expect.objectContaining({ kind: "offset" }));
    expect(framesOf(next.page.sent), what).toMatchObject([{ kind: "close", reason: expect.stringContaining("offset") }]);
    expect(await stream.ended, what).toMatchObject({ kind: "offset" });
    await expect(stream.read(), what).rejects.toMatchObject({ kind: "offset" });
    await expect(stream.write(bytes("x")), what).rejects.toMatchObject({ kind: "offset" });
  }
});

test("a malformed frame, or one the stream's state forbids, ends the stream and tells the other end", async () => {
  const eof: Frame = { kind: "eof", ack: 0 };
  const cases: [string, (Frame | number[] | string)[]][] = [
    ["a size that is no frame type", [[0, ...uvarint(maxData + 3)]]],
    ["a uvarint past 64 bits", [[0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f]]],
    ["a reason longer than a frame carries", [[0, ...uvarint(maxData + 2), ...uvarint(maxData + 1)]]],
    ["an acknowledgement of positions never sent", [{ kind: "data", ack: 1, data: new Uint8Array(0) }]],
    ["data after the EOF", [eof, { kind: "data", ack: 0, data: bytes("x") }]],
    ["an EOF after the EOF", [eof, eof]],
    [
      "more unacknowledged than a buffer holds",
      [
        ...Array.from({ length: maxBuffer / maxData }, (): Frame => ({ kind: "data", ack: 0, data: new Uint8Array(maxData) })),
        { kind: "data", ack: 0, data: bytes("x") },
      ],
    ],
    ["a text message", ["text"]],
  ];
  for (const [what, frames] of cases) {
    const { stream, link, peer } = raw();
    for (const frame of frames) {
      if (typeof frame === "string") {
        link.page.onmessage?.({ data: frame } as MessageEvent);
      } else {
        peer.send(Array.isArray(frame) ? Uint8Array.from(frame) : encodeFrame(frame));
      }
    }
    expect(await stream.ended, what).toMatchObject({ kind: "malformed" });
    expect(framesOf(link.page.sent).at(-1), what).toMatchObject({ kind: "close", reason: expect.stringContaining("malformed frame") });
    await expect(stream.write(bytes("x")), what).rejects.toMatchObject({ kind: "malformed" });
    // The stream is off the socket: nothing more is sent or heard on it.
    const sent = link.page.sent.length;
    peer.send(encodeFrame({ kind: "data", ack: 0, data: bytes("more") }));
    stream.probe();
    await settled();
    expect(link.page.sent.length, what).toBe(sent);
  }
});

test("a failed send still reads the close frame that arrived", async () => {
  const { page, gateway, link } = open();
  await gateway.write(bytes("last output"));
  gateway.close("the koko server is restarting");
  link.sever(); // the Gateway closed the WebSocket behind its close frame
  await page.write(bytes("typed too late")); // fails on the socket, and is kept
  expect(await readAll(page)).toMatchObject({
    got: "last output",
    err: { kind: "peerClosed", reason: "the koko server is restarting" },
  });
});
