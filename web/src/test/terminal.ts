import { onTestFinished } from "vitest";

import { encodeRecord, RecordReader, type TerminalRecord } from "../terminal/records";
import { encodeFrame, type Frame, FrameReader } from "../terminal/stream";

// Stand-ins for what the web terminal uses of the browser: Web Locks, which
// tell a duplicated browser tab's SSH Sessions from a reload's, and the
// WebSocket to /terminal/socket, whose other end a test plays.

/**
 * FakeLocks is the browser's Web Locks, which every document a test plays
 * shares, as the documents of one browser do: a lock asked for while held
 * is granted once it is let go.
 */
export class FakeLocks {
  readonly held = new Set<string>();
  /** queues holds, for each lock asked for, when its last request lets it go. */
  private readonly queues = new Map<string, Promise<void>>();

  /**
   * request runs callback once the lock is free, and holds it until
   * callback's promise settles; a request whose signal aborts before then
   * is refused at once, and its callback never runs.
   */
  request(name: string, ...args: [() => Promise<void>] | [{ signal?: AbortSignal }, () => Promise<void>]): Promise<void> {
    const [options, callback] = args.length === 1 ? [{}, args[0]] : args;
    const { signal } = options;
    let aborted = false;
    const refused = new Promise<never>((_, reject) => {
      signal?.addEventListener("abort", () => {
        aborted = true;
        reject(new DOMException("The request was aborted.", "AbortError"));
      });
    });
    const granted = (this.queues.get(name) ?? Promise.resolve()).then(() => {
      if (aborted) {
        return;
      }
      this.held.add(name);
      return callback().finally(() => this.held.delete(name));
    });
    this.queues.set(
      name,
      granted.catch(() => {}),
    );
    return signal ? Promise.race([granted, refused]) : granted;
  }

  async query(): Promise<LockManagerSnapshot> {
    return { held: [...this.held].map((name) => ({ name, mode: "exclusive" })), pending: [] };
  }
}

/** useFakeLocks gives the browser Web Locks for one test, and is them. */
export function useFakeLocks(): FakeLocks {
  const locks = new FakeLocks();
  Object.defineProperty(navigator, "locks", { configurable: true, value: locks });
  onTestFinished(() => {
    Reflect.deleteProperty(navigator, "locks");
  });
  return locks;
}

/**
 * FakeWebSocket stands in for the browser's WebSocket (vi.stubGlobal): each
 * one the page opens is in sockets, and the test answers as the koko server.
 */
export class FakeWebSocket {
  static sockets: FakeWebSocket[] = [];
  binaryType: BinaryType = "blob";
  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;
  closed = false;
  readonly url: string;
  private readonly sent: (string | Uint8Array)[] = [];

  constructor(url: string) {
    this.url = url;
    FakeWebSocket.sockets.push(this);
  }

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

  /** output is the shell's output reaching the page. */
  output(text: string): void {
    this.records({ type: "data", data: new TextEncoder().encode(text) });
  }

  /** drop is the WebSocket closing under the page. */
  drop(): void {
    this.onclose?.({} as CloseEvent);
  }

  /** first is the first message the page sent, parsed. */
  get first(): unknown {
    return typeof this.sent[0] === "string" ? JSON.parse(this.sent[0]) : undefined;
  }

  /** framesSent is the frames the page sent after its first message. */
  get framesSent(): Frame[] {
    const reader = new FrameReader();
    const frames: Frame[] = [];
    for (const message of this.sent.slice(1)) {
      if (message instanceof Uint8Array) {
        reader.push(message);
        for (let frame = reader.next(); frame; frame = reader.next()) {
          frames.push(frame);
        }
      }
    }
    return frames;
  }

  /** typed is the text of the data records the page sent after its first message: what was typed, pasted or pressed. */
  get typed(): string {
    const reader = new RecordReader();
    reader.push(Uint8Array.from(this.framesSent.flatMap((frame) => (frame.kind === "data" ? [...frame.data] : []))));
    const decoder = new TextDecoder();
    let text = "";
    for (let record = reader.next(); record; record = reader.next()) {
      if (record.type === "data") {
        text += decoder.decode(record.data);
      }
    }
    return text;
  }
}
