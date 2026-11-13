import { encodeRecord, MalformedRecord, maxRecord, RecordReader, type RGB, type TerminalRecord, terminalProtocol } from "./records";
import { Stream, StreamError, type StreamSocket } from "./stream";
import { timing } from "./timing";
import type { Refusal } from "./workspaceSocket";

// A page's attachment to one SSH Session of its Workspace (spec #213, "The
// terminal socket and attaching"): the first messages on /terminal/socket
// and their answers, the resumable stream of terminal records after them,
// liveness, and resuming the attachment after a drop. Attaching changes
// nothing in the SSH Session: the shell goes on with no page attached. It
// has no UI: a page shows its state and gives it keys.

/**
 * maxGrid is the most columns and rows a pty has, the koko server's
 * vt.MaxCols by vt.MaxRows: a page whose box holds more says the cap, and
 * draws the terminal at it with a margin.
 */
export const maxGrid = { columns: 500, rows: 200 } as const;

/** TerminalSocket is the part of a WebSocket an attachment uses. */
export interface TerminalSocket {
  binaryType: BinaryType;
  onopen: ((event: Event) => void) | null;
  onmessage: ((event: MessageEvent) => void) | null;
  onclose: ((event: CloseEvent) => void) | null;
  send(data: string | Uint8Array<ArrayBuffer>): void;
  close(): void;
}

/** dialSocket opens a WebSocket to the terminal endpoint of the page's own origin. */
export function dialSocket(): TerminalSocket {
  const scheme = window.location.protocol === "https:" ? "wss:" : "ws:";
  return new WebSocket(`${scheme}//${window.location.host}/terminal/socket`);
}

/** Size is a terminal's size, in character cells. */
export interface Size {
  columns: number;
  rows: number;
}

/** Colors are the theme's foreground and background, which the koko server answers OSC 10 and 11 with. */
export interface Colors {
  foreground: RGB;
  background: RGB;
}

/** AttachmentState is what a tab shows of its page's attachment. */
export type AttachmentState =
  | { status: "attaching" }
  | { status: "attached" }
  /** The WebSocket was lost at since (Date.now), and the page is resuming; keys typed are kept. */
  | { status: "reconnecting"; since: number }
  /**
   * The koko server refused the page, with a code it can test: the web
   * front's, such as signin_required or reload_page. Nothing more is tried.
   */
  | ({ status: "refused" } & Refusal)
  /** The SSH Session is no longer there to attach to: the Workspace tells how it ended. */
  | { status: "gone" }
  /** The koko server closed the attachment, saying notice: the SSH Session ended, or this page was detached. */
  | { status: "detached"; notice: string }
  /** A ninth page attached and detached this one, the least recently active: it attaches again only on the person's word. */
  | { status: "displaced" }
  /** The two ends disagreed about the protocol, which is a bug in koko. */
  | { status: "protocol"; message: string };

/** Handlers is what an attachment tells its page. */
export interface Handlers {
  /** onState is called at each change of state. */
  onState(state: AttachmentState): void;
  /**
   * onOutput is given the shell's output, in order. The attachment waits
   * for the promise it returns, if any, before it reads on, so an emulator
   * that falls behind slows the stream instead of filling memory.
   */
  onOutput(bytes: Uint8Array): void | Promise<void>;
  /** onReset clears the emulator, its history too, to the pty's size; the snapshot follows as output. */
  onReset(size: Size): void;
  /** onActive says whether this page is the SSH Session's active page, whose box the pty takes. */
  onActive(active: boolean): void;
}

/** side brings a number of columns or rows into 1 to most. */
const side = (cells: number, most: number) => Math.min(Math.max(Math.trunc(cells) || 1, 1), most);

/** gridOf brings a box's size into what a pty may have. */
export function gridOf(size: Size): Size {
  return { columns: side(size.columns, maxGrid.columns), rows: side(size.rows, maxGrid.rows) };
}

/**
 * Answered is the koko server's answer to a first message, with the
 * WebSocket it came on, whose handlers are then the caller's to set; or
 * null for no answer: the WebSocket failed first, and is closed.
 */
type Answered = { answer: Record<string, unknown>; socket: TerminalSocket } | null;

/**
 * ask opens a WebSocket, sends one first message, made once the WebSocket
 * is open, and gives the answer, within timing.answer of the open. The
 * returned function gives the WebSocket up, and answered is then not
 * called.
 */
function ask(dial: () => TerminalSocket, message: () => object, answered: (answered: Answered) => void): () => void {
  let socket: TerminalSocket | null = null;
  let timer: ReturnType<typeof setTimeout> | undefined;
  /** release takes the WebSocket out of ask's hands, once. */
  const release = (): TerminalSocket | null => {
    clearTimeout(timer);
    const released = socket;
    socket = null;
    if (released) {
      released.onopen = released.onmessage = released.onclose = null;
    }
    return released;
  };
  const fail = () => {
    release()?.close();
    answered(null);
  };
  try {
    socket = dial();
  } catch {
    // A page that cannot open a WebSocket at all learns it later, as it
    // learns of a WebSocket that fails.
    timer = setTimeout(() => answered(null), 0);
    return () => clearTimeout(timer);
  }
  socket.binaryType = "arraybuffer";
  timer = setTimeout(fail, timing.connect);
  socket.onopen = () => {
    clearTimeout(timer);
    timer = setTimeout(fail, timing.answer);
    socket?.send(JSON.stringify({ protocol: terminalProtocol, ...message() }));
  };
  socket.onmessage = (event) => {
    let answer: unknown = null;
    try {
      answer = typeof event.data === "string" ? JSON.parse(event.data) : null;
    } catch {
      // No JSON is no answer.
    }
    if (typeof answer !== "object" || answer === null || Array.isArray(answer)) {
      fail();
      return;
    }
    const released = release();
    if (released) {
      answered({ answer: answer as Record<string, unknown>, socket: released });
    }
  };
  socket.onclose = fail;
  return () => {
    release()?.close();
  };
}

/** refusalOf is an answer's refusal, if it is one: its code and the koko server's words. */
function refusalOf(answer: Record<string, unknown>): Refusal | null {
  if (typeof answer.refused !== "string") {
    return null;
  }
  return { code: answer.refused, message: typeof answer.message === "string" ? answer.message : "" };
}

/** goneCodes are the refusals of an attach that say the SSH Session is no longer there. */
const goneCodes: ReadonlySet<string> = new Set(["unknown_tunnel", "ssh_session_ended"]);

/**
 * Attachment is one page's attachment to one SSH Session: attached at once,
 * resumed after a drop, and attached again with a new snapshot once the
 * Gateway has let the old one go (resume_refused). It ends when the koko
 * server closes it, or when the page detaches.
 */
export class Attachment {
  private readonly dial: () => TerminalSocket;
  private readonly session: string;
  private readonly handlers: Handlers;
  private current: AttachmentState = { status: "attaching" };
  private size: Size;
  private colors: Colors | undefined;
  /** wantsActive says a person acted while no stream reached the Gateway: it is told once one does. */
  private wantsActive = false;
  /** giveUp gives up the WebSocket of a first message not yet answered. */
  private giveUp: (() => void) | null = null;
  private socket: TerminalSocket | null = null;
  private stream: Stream | null = null;
  private id = "";
  /** displaced says the Gateway said this page was detached for a ninth, before closing the stream. */
  private displaced = false;

  // Liveness: when the page last sent, and last heard, on the WebSocket.
  private lastSent = 0;
  private lastReceived = 0;
  private liveness: ReturnType<typeof setTimeout> | undefined;

  // Reconnecting: when the WebSocket was lost, the wait before the next
  // try, and whether a try is under way.
  private lostAt = 0;
  private wait = 0;
  private trying = false;
  private retryTimer: ReturnType<typeof setTimeout> | undefined;

  constructor(session: string, size: Size, colors: Colors | undefined, handlers: Handlers, dial: () => TerminalSocket = dialSocket) {
    this.dial = dial;
    this.session = session;
    this.handlers = handlers;
    this.size = gridOf(size);
    this.colors = colors;
    document.addEventListener("visibilitychange", this.wake);
    window.addEventListener("pageshow", this.wake);
    window.addEventListener("online", this.wake);
    this.attach();
  }

  /** state is the attachment's state now. */
  get state(): AttachmentState {
    return this.current;
  }

  /**
   * send sends bytes typed or pasted to the shell, which makes this page
   * the active one. While reconnecting they are kept, and sent once on the
   * resume; while attaching, and once the attachment is over, they go
   * nowhere.
   */
  send(bytes: Uint8Array): void {
    for (let at = 0; at < bytes.length; at += maxRecord) {
      this.write({ type: "data", data: bytes.subarray(at, at + maxRecord) });
    }
  }

  /** resize tells the koko server the page's box, in cells, which the pty takes while this page is active. */
  resize(size: Size): void {
    this.size = gridOf(size);
    this.write({ type: "resize", ...this.size });
  }

  /**
   * active tells the koko server a person acted in this page, which makes
   * it the active page. The caller sends it only for a person's act in a
   * visible document; one while no stream reaches the Gateway is told once
   * one does.
   */
  active(): void {
    if (!this.writes) {
      this.wantsActive = this.live;
      return;
    }
    this.write({ type: "active" });
  }

  /** setColors tells the koko server the theme's colours, now and on every attach. */
  setColors(colors: Colors): void {
    this.colors = colors;
    this.write({ type: "colors", ...colors });
  }

  /** tryNow tries to resume at once, for a person who does not wait out the backoff. */
  tryNow(): void {
    this.wait = 0;
    this.retry();
  }

  /**
   * detach leaves the SSH Session, as a page does that closes its tab,
   * reloads or signs out: the stream's close, then the WebSocket's. The
   * SSH Session goes on.
   */
  detach(): void {
    if (this.writes) {
      this.stream?.close();
    }
    this.settle(null);
  }

  /** live says whether the attachment may still carry the shell: it is in no final state. */
  private get live(): boolean {
    const status = this.current.status;
    return status === "attaching" || status === "attached" || status === "reconnecting";
  }

  /** writes says whether a record written now reaches the Gateway, at once or once resumed. */
  private get writes(): boolean {
    return this.stream !== null && (this.current.status === "attached" || this.current.status === "reconnecting");
  }

  private setState(state: AttachmentState): void {
    this.current = state;
    this.handlers.onState(state);
  }

  /** settle brings the attachment to a final state, or with null to none the page hears of, and gives up what it held. */
  private settle(state: AttachmentState | null): void {
    if (!this.live) {
      return;
    }
    clearTimeout(this.liveness);
    clearTimeout(this.retryTimer);
    document.removeEventListener("visibilitychange", this.wake);
    window.removeEventListener("pageshow", this.wake);
    window.removeEventListener("online", this.wake);
    this.giveUp?.();
    this.giveUp = null;
    this.discard();
    if (state) {
      this.setState(state);
    } else {
      this.current = { status: "gone" };
    }
  }

  /** discard closes the stream's WebSocket, if any, and hears no more from it. */
  private discard(): void {
    const socket = this.socket;
    this.socket = null;
    if (socket) {
      socket.onmessage = socket.onclose = null;
      socket.close();
    }
  }

  private write(record: TerminalRecord): void {
    if (this.writes) {
      // A write an ended stream refuses is told of by the stream's end.
      this.stream?.write(encodeRecord(record)).catch(() => {});
    }
  }

  /** attach sends the attach, at the box's size, and starts the stream it answers with. */
  private attach(): void {
    this.trying = true;
    this.giveUp = ask(
      this.dial,
      () => ({ attach: this.session, cols: this.size.columns, rows: this.size.rows }),
      (answered) => {
        this.giveUp = null;
        this.trying = false;
        const id = answered?.answer.attachment;
        if (answered && typeof id === "string") {
          this.start(id, answered.socket);
          return;
        }
        answered?.socket.close();
        const refusal = answered && refusalOf(answered.answer);
        if (refusal) {
          this.settle(goneCodes.has(refusal.code) ? { status: "gone" } : { status: "refused", ...refusal });
          return;
        }
        this.backOff();
      },
    );
  }

  /** start makes the stream of a new attachment, and tells the koko server the theme's colours and a person's act. */
  private start(id: string, socket: TerminalSocket): void {
    this.id = id;
    const stream = new Stream(this.carry(socket));
    this.stream = stream;
    this.watch();
    this.setState({ status: "attached" });
    if (this.colors) {
      this.write({ type: "colors", ...this.colors });
    }
    this.sayActive();
    void this.relay(stream);
  }

  /** sayActive tells the koko server of a person's act that came while no stream reached it. */
  private sayActive(): void {
    if (this.wantsActive) {
      this.wantsActive = false;
      this.write({ type: "active" });
    }
  }

  /**
   * carry makes socket the stream's, noting when the page sends and hears
   * on it, for the liveness watch.
   */
  private carry(socket: TerminalSocket): StreamSocket {
    const carried: StreamSocket = {
      send: (data) => {
        this.lastSent = Date.now();
        socket.send(data);
      },
      onmessage: null,
    };
    socket.onmessage = (event) => {
      this.lastReceived = Date.now();
      carried.onmessage?.(event);
    };
    socket.onclose = () => this.lose();
    this.socket = socket;
    this.lastSent = this.lastReceived = Date.now();
    return carried;
  }

  /**
   * watch is the liveness watch, as the Gateway's: a page can neither send
   * nor see WebSocket pings, so the page sends an acknowledgement-only
   * frame after probe without sending, and takes the WebSocket for dead
   * after dead without a byte. It reads the wall clock, which moves on
   * while a phone sleeps, as a monotonic clock may not.
   */
  private watch(): void {
    clearTimeout(this.liveness);
    const now = Date.now();
    if (now - this.lastReceived >= timing.dead) {
      this.lose();
      return;
    }
    if (now - this.lastSent >= timing.probe) {
      this.lastSent = now; // even should the probe not go, as on a failed send
      this.stream?.probe();
    }
    const next = Math.min(this.lastSent + timing.probe, this.lastReceived + timing.dead);
    this.liveness = setTimeout(() => this.watch(), next - now);
  }

  /** relay gives the page the stream's records, and ends the attachment as the stream ends. */
  private async relay(stream: Stream): Promise<void> {
    const records = new RecordReader();
    try {
      for (let bytes = await stream.read(); bytes !== null; bytes = await stream.read()) {
        records.push(bytes);
        for (let record = records.next(); record; record = records.next()) {
          if (this.stream !== stream) {
            return;
          }
          if (record.type === "data") {
            await this.handlers.onOutput(record.data);
          } else if (record.type === "reset") {
            this.handlers.onReset({ columns: record.columns, rows: record.rows });
          } else if (record.type === "active" && record.active !== undefined) {
            this.handlers.onActive(record.active);
          } else if (record.type === "displaced") {
            this.displaced = true;
          } else if (record.type !== "exitStatus" && record.type !== "exitSignal") {
            // The shell's exit comes in the Workspace too, which the page shows.
            throw new MalformedRecord(`a ${record.type} record, which only the page sends`);
          }
        }
      }
    } catch (err) {
      if (err instanceof MalformedRecord) {
        stream.close(err.message);
        this.settle({ status: "protocol", message: err.message });
        return;
      }
      if (!(err instanceof StreamError)) {
        throw err;
      }
    }
    if (this.stream !== stream) {
      return;
    }
    const err = await stream.ended;
    if (this.displaced) {
      this.settle({ status: "displaced" });
    } else if (err.kind === "peerClosed") {
      this.settle({ status: "detached", notice: err.reason });
    } else if (err.kind !== "closed") {
      this.settle({ status: "protocol", message: err.message });
    }
  }

  /**
   * lose takes the stream off a WebSocket that closed or is taken for
   * dead, and starts resuming. A stream that has ended is not resumed: its
   * end is on its way to the page through relay.
   */
  private lose(): void {
    if (this.current.status !== "attached" || !this.stream || this.stream.error) {
      return;
    }
    clearTimeout(this.liveness);
    this.stream.detach();
    this.discard();
    this.lostAt = Date.now();
    this.wait = 0;
    this.setState({ status: "reconnecting", since: this.lostAt });
    this.retry();
  }

  /** backOff waits before the next try: reconnectFirst, doubling up to reconnectMax. */
  private backOff(): void {
    this.wait = this.wait === 0 ? timing.reconnectFirst : Math.min(2 * this.wait, timing.reconnectMax);
    this.retryTimer = setTimeout(() => this.retry(), this.wait);
  }

  /**
   * retry tries again on a new WebSocket, unless a try is under way: to
   * resume the attachment, or to attach while there is none. While the page
   * is hidden it does not try: wake does when the page is shown.
   */
  private retry(): void {
    if (!this.live || this.trying || this.current.status === "attached") {
      return;
    }
    clearTimeout(this.retryTimer);
    if (document.visibilityState === "hidden") {
      return;
    }
    const stream = this.stream;
    if (!stream) {
      this.attach();
      return;
    }
    this.trying = true;
    this.giveUp = ask(
      this.dial,
      () => ({ resume: this.id, offset: stream.detach() }),
      (answered) => {
        this.giveUp = null;
        this.trying = false;
        const offset = answered?.answer.offset;
        if (answered && typeof offset === "number") {
          try {
            stream.resume(this.carry(answered.socket), offset);
          } catch (err) {
            this.settle({ status: "protocol", message: err instanceof Error ? err.message : String(err) });
            return;
          }
          this.watch();
          this.setState({ status: "attached" });
          // A resize may have been lost unacknowledged, so the size goes again.
          this.write({ type: "resize", ...this.size });
          this.sayActive();
          return;
        }
        answered?.socket.close();
        const refusal = answered && refusalOf(answered.answer);
        if (refusal?.code === "resume_refused") {
          // The Gateway let the attachment go: a new one brings the screen as it is now.
          this.stream = null;
          this.wait = 0;
          this.attach();
          return;
        }
        if (refusal) {
          this.settle({ status: "refused", ...refusal });
          return;
        }
        this.backOff();
      },
    );
  }

  /**
   * wake is the page shown again, or back online: a WebSocket that has
   * said nothing for probe is taken for dead at once, so a phone out of a
   * pocket does not wait out dead; and a try waiting is made at once.
   */
  private readonly wake = (): void => {
    if (document.visibilityState === "hidden") {
      return;
    }
    if (this.current.status === "attached" && Date.now() - this.lastReceived >= timing.probe) {
      this.lose();
    } else {
      this.tryNow();
    }
  };
}
