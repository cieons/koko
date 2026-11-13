import { encodeRecord, RecordReader, type TerminalRecord } from "../terminal/records";
import { encodeFrame, type Frame, FrameReader } from "../terminal/stream";

// A stand-in for the browser's WebSocket, to the Workspace socket and to
// /terminal/socket, whose other end a test plays.

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

  /** messages is the text messages the page sent, parsed: the Workspace socket's. */
  get messages(): unknown[] {
    return this.sent.flatMap((message) => (typeof message === "string" ? [JSON.parse(message)] : []));
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

/** isWorkspace says a fake WebSocket is the Workspace socket, not an attachment's. */
const isWorkspace = (socket: FakeWebSocket) => socket.url.endsWith("/terminal/workspace");

/** attachments are the WebSockets the page opened to /terminal/socket, in order. */
export function attachments(): FakeWebSocket[] {
  return FakeWebSocket.sockets.filter((socket) => !isWorkspace(socket));
}

/** workspaceSockets are the Workspace sockets the page opened, in order. */
export function workspaceSockets(): FakeWebSocket[] {
  return FakeWebSocket.sockets.filter(isWorkspace);
}

/** WireTab is a tab of the Workspace message, as the koko server writes it. */
export interface WireTab {
  id: string;
  host: string;
  label: string;
  state: "starting" | "running" | "ended" | "refused" | "restarted";
  session?: string;
  ended?: { reason: string; notice?: string; exit_status?: number; exit_signal?: string };
  refused?: { code: string; message: string };
}

/** running is a tab whose shell runs, its SSH Session's id made from its own. */
export function running(id: string, host: string, label = host): WireTab {
  return { id, host, label, state: "running", session: `5e55${id}`.padEnd(32, "0") };
}

/** Request is a request the page sent on the Workspace socket. */
export type Request = { request: number } & Record<string, unknown>;

/** requestsOn is the requests the page sent on a Workspace socket, in order. */
export function requestsOn(socket: FakeWebSocket | undefined): Request[] {
  return (socket?.messages ?? []).filter((message): message is Request => typeof (message as Request).request === "number");
}
