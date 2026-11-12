import { forgetSession, keepSessions, keptSessions } from "./kept";
import { encodeRecord, MalformedRecord, maxRecord, maxSide, RecordReader, type TerminalRecord, terminalProtocol } from "./records";
import { Stream, StreamError, type StreamSocket } from "./stream";

// The page's end of a web terminal's SSH Session: the first messages on
// /terminal/socket and their answers, the resumable stream of terminal
// records after them, liveness, and the Resume after a drop (P1 spec: a
// terminal in the web UI, "The terminal endpoint"). It has no UI: a page
// shows its state and gives it keys.

/**
 * timing is the page's timings, on the page's clock. Each but connect and
 * answer is the koko server's or the device's of the same purpose
 * (internal/shared/timing), which the spec has the page share.
 */
export const timing = {
  /** probe is how long the page goes without sending before it sends a probe (ServerProbe). */
  probe: 15_000,
  /** dead is how long it goes without a byte from the koko server before it takes the WebSocket for dead (ServerDead). */
  dead: 45_000,
  /** reconnectFirst is the wait after the first failed try to Resume; it doubles up to reconnectMax. */
  reconnectFirst: 250,
  reconnectMax: 5_000,
  /** grace is how long the Gateway holds a Detached SSH Session, and so how long the page tries (BrowserGrace). */
  grace: 15 * 60_000,
  /** connect bounds how long one WebSocket takes to open (DeviceConnectAttempt). */
  connect: 10_000,
  /** answer bounds how long the answer to a resume or an end takes, once the WebSocket is open (ResumeExchange). */
  answer: 5_000,
} as const;

/** TerminalSocket is the part of a WebSocket the terminal uses. */
export interface TerminalSocket {
  binaryType: BinaryType;
  onopen: ((event: Event) => void) | null;
  onmessage: ((event: MessageEvent) => void) | null;
  onclose: ((event: CloseEvent) => void) | null;
  send(data: string | Uint8Array<ArrayBuffer>): void;
  close(): void;
}

/** dialSocket opens a WebSocket to the terminal endpoint of the page's own origin. */
function dialSocket(): TerminalSocket {
  const scheme = window.location.protocol === "https:" ? "wss:" : "ws:";
  return new WebSocket(`${scheme}//${window.location.host}/terminal/socket`);
}

/** Size is a terminal's size, in character cells. */
export interface Size {
  columns: number;
  rows: number;
}

/** Ending is why an SSH Session ended. */
export type Ending =
  /** The shell exited. */
  | { kind: "exitStatus"; status: number }
  /** A signal, such as KILL, ended the shell. */
  | { kind: "exitSignal"; signal: string }
  /** The koko server closed it, saying notice: the Gateway's notice without "koko: ", perhaps empty. */
  | { kind: "notice"; notice: string }
  /** The person ended it from the page. */
  | { kind: "closed" }
  /**
   * The WebSocket was lost at the time at (Date.now), and the SSH Session
   * could not be resumed: message is the Gateway's reason, or empty when it
   * gave none or the page gave up at the grace.
   */
  | { kind: "lost"; at: number; message: string }
  /** The two ends disagreed about the protocol, which is a bug in koko. */
  | { kind: "protocol"; message: string };

/** ConnectionState is what a terminal's tab shows of its connection. */
export type ConnectionState =
  | { status: "connecting" }
  | { status: "connected" }
  /** The WebSocket was lost at since (Date.now), and the page is resuming; keys typed are kept. */
  | { status: "reconnecting"; since: number }
  /**
   * The koko server refused, with a code the page can test and the
   * Gateway's words: an open's refusal, such as unknown_host, or the web
   * front's, such as signin_required or reload_page, at an open or a
   * resume. The page makes no further attempt.
   */
  | { status: "refused"; code: string; message: string }
  /** The open got no answer: the WebSocket failed first. */
  | { status: "unreachable" }
  | { status: "ended"; ending: Ending };

/** Handlers is what a connection tells its page. */
export interface Handlers {
  /** onState is called at each change of state. */
  onState(state: ConnectionState): void;
  /**
   * onOutput is given the shell's output, in order. The connection waits
   * for the promise it returns, if any, before it reads on, so an emulator
   * that falls behind slows the shell instead of filling memory.
   */
  onOutput(bytes: Uint8Array): void | Promise<void>;
}

// A browser tab duplicated from this one starts with a copy of its
// sessionStorage, and so with the ids of SSH Sessions this document still
// holds. So a document holds a Web Lock named after each SSH Session it
// holds, for as long as it does; the browser lets the locks go with the
// document. A new document ends only the kept ids no other document holds:
// a reload's or a restored tab's, whose document has gone. The ids another
// document holds are that document's, and the copy forgets them.

/** lockPrefix begins the name of the Web Lock a document holds for each SSH Session it holds. */
const lockPrefix = "koko.terminal.";

/** locks is the browser's Web Locks, if it has them: every browser the web UI supports does, in a secure context. */
function locks(): LockManager | undefined {
  return "locks" in navigator ? navigator.locks : undefined;
}

/**
 * holdSession tells this browser's other documents that this one holds the
 * SSH Session of that id, until the function it returns is called or the
 * document goes.
 */
function holdSession(id: string): () => void {
  let release = () => {};
  const held = new Promise<void>((resolve) => {
    release = resolve;
  });
  locks()
    ?.request(lockPrefix + id, () => held)
    .catch(() => {});
  return release;
}

/**
 * heldElsewhere is the ids of the SSH Sessions other documents of this
 * browser hold, or null when the browser cannot tell.
 */
async function heldElsewhere(): Promise<Set<string> | null> {
  const manager = locks();
  if (!manager) {
    return null;
  }
  try {
    const { held = [] } = await manager.query();
    return new Set(held.flatMap(({ name = "" }) => (name.startsWith(lockPrefix) ? [name.slice(lockPrefix.length)] : [])));
  } catch {
    return null;
  }
}

/**
 * endLeft ends the SSH Sessions an earlier document of this browser tab
 * left, and forgets the kept ids it may not end: those another document
 * holds, and every one when the browser cannot tell whose they are, which
 * the Gateway then ends at the grace.
 */
async function endLeft(dial: () => TerminalSocket): Promise<void> {
  const kept = keptSessions();
  if (kept.length === 0) {
    return;
  }
  const elsewhere = await heldElsewhere();
  const ours = elsewhere === null ? [] : kept.filter((id) => !elsewhere.has(id));
  keepSessions((ids) => ids.filter((id) => ours.includes(id)));
  await Promise.all(ours.map((id) => endSession(dial, id)));
}

/**
 * Answered is the koko server's answer to a first message, with the
 * WebSocket it came on, whose handlers are then the caller's to set; or
 * null for no answer: the WebSocket failed first, and is closed.
 */
type Answered = { answer: Record<string, unknown>; socket: TerminalSocket } | null;

/**
 * ask opens a WebSocket, sends one first message, made once the WebSocket
 * is open, and gives the answer. answerWithin bounds the wait for it; an
 * open has no bound, since the Gateway dials the Host before it answers.
 * The returned function gives the WebSocket up, and answered is then not
 * called.
 */
function ask(
  dial: () => TerminalSocket,
  message: () => object,
  answerWithin: number | null,
  answered: (answered: Answered) => void,
): () => void {
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
    if (answerWithin !== null) {
      timer = setTimeout(fail, answerWithin);
    }
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
function refusalOf(answer: Record<string, unknown>): { code: string; message: string } | null {
  if (typeof answer.refused !== "string") {
    return null;
  }
  return { code: answer.refused, message: typeof answer.message === "string" ? answer.message : "" };
}

/**
 * endSession asks the koko server to end the SSH Session of that id, as a
 * page that no longer holds its stream does, and forgets the id once the
 * koko server has answered: ended, or refused, after which this page can do
 * no more for it. Without an answer the id is kept for a later page.
 */
function endSession(dial: () => TerminalSocket, id: string): Promise<void> {
  return new Promise((resolve) => {
    ask(
      dial,
      () => ({ end: id }),
      timing.answer,
      (answered) => {
        if (answered) {
          forgetSession(id);
          answered.socket.close();
        }
        resolve();
      },
    );
  });
}

/**
 * Terminals opens the web terminal's SSH Sessions for one document. Before
 * its first, it ends the SSH Sessions an earlier document of this browser
 * tab left: a reload, or a tab the browser discarded and restored, lost
 * their streams, and sessionStorage kept their ids, so they stop counting
 * against the caller's limit before the tabs open again. A browser tab
 * duplicated from a live one leaves that one's SSH Sessions alone.
 */
export class Terminals {
  private readonly dial: () => TerminalSocket;
  private earlierEnded: Promise<void> | undefined;

  constructor(dial: () => TerminalSocket = dialSocket) {
    this.dial = dial;
  }

  /**
   * endEarlier ends the SSH Sessions an earlier document of this browser
   * tab left, once; the first open waits for it, and a page may start it
   * sooner, as one that has no terminal to open yet.
   */
  endEarlier(): Promise<void> {
    this.earlierEnded ??= endLeft(this.dial);
    return this.earlierEnded;
  }

  /**
   * open opens an SSH Session to the Host of that name, with a pty of that
   * size, and returns its connection, which starts as connecting.
   */
  open(host: string, size: Size, handlers: Handlers): Connection {
    return new Connection(this.dial, this.endEarlier(), host, size, handlers);
  }
}

/** side brings a number of columns or rows into what a resize carries. */
const side = (cells: number) => Math.min(Math.max(Math.trunc(cells) || 1, 1), maxSide);

/**
 * Connection is one SSH Session of a web terminal, from its open to its
 * end: Terminals.open makes it. A new shell in the same tab is a new
 * Connection.
 */
export class Connection {
  private readonly dial: () => TerminalSocket;
  private readonly host: string;
  private readonly handlers: Handlers;
  private current: ConnectionState = { status: "connecting" };
  private size: Size;
  /** giveUp gives up the WebSocket of a first message not yet answered. */
  private giveUp: (() => void) | null = null;
  private socket: TerminalSocket | null = null;
  private stream: Stream | null = null;
  private id = "";
  /** release lets go of the Web Lock that says this document holds the SSH Session. */
  private release = () => {};
  /** exit is the shell's end, once its record has come. */
  private exit: Ending | null = null;

  // Liveness: when the page last sent, and last heard, on the WebSocket.
  private lastSent = 0;
  private lastReceived = 0;
  private liveness: ReturnType<typeof setTimeout> | undefined;

  // Resuming: when the WebSocket was lost, the wait before the next try,
  // and whether a try is under way.
  private lostAt = 0;
  private wait = 0;
  private trying = false;
  private retryTimer: ReturnType<typeof setTimeout> | undefined;

  constructor(dial: () => TerminalSocket, earlierEnded: Promise<void>, host: string, size: Size, handlers: Handlers) {
    this.dial = dial;
    this.host = host;
    this.handlers = handlers;
    this.size = { columns: side(size.columns), rows: side(size.rows) };
    document.addEventListener("visibilitychange", this.wake);
    window.addEventListener("pageshow", this.wake);
    window.addEventListener("online", this.wake);
    window.addEventListener("pagehide", this.leave);
    void earlierEnded.then(() => this.open());
  }

  /** state is the connection's state now. */
  get state(): ConnectionState {
    return this.current;
  }

  /** sessionId is the SSH Session's id, which the Files panel's calls name; empty until connected. */
  get sessionId(): string {
    return this.id;
  }

  /**
   * send sends bytes typed or pasted to the shell. While reconnecting they
   * are kept, and sent once on the Resume; before the shell has started
   * and after it has ended they go nowhere.
   */
  send(bytes: Uint8Array): void {
    for (let at = 0; at < bytes.length; at += maxRecord) {
      this.write({ type: "data", data: bytes.subarray(at, at + maxRecord) });
    }
  }

  /** resize tells the shell the terminal's size, at the open if it has not gone yet. */
  resize(columns: number, rows: number): void {
    this.size = { columns: side(columns), rows: side(rows) };
    this.write({ type: "resize", ...this.size });
  }

  /**
   * end ends the SSH Session on the person's word: the stream's close
   * frame, then the WebSocket's close. While reconnecting, where no stream
   * reaches the Gateway, it asks the koko server to end it instead.
   */
  end(): void {
    if (this.current.status === "connected") {
      this.stream?.close();
      forgetSession(this.id);
    } else if (this.current.status === "reconnecting") {
      void endSession(this.dial, this.id);
    } else if (this.current.status !== "connecting") {
      return;
    }
    this.settle({ status: "ended", ending: { kind: "closed" } });
  }

  /** tryNow tries to Resume at once, for a person who does not wait out the backoff. */
  tryNow(): void {
    this.wait = 0;
    this.retry();
  }

  /** live says whether the connection may still carry the shell: it is in no final state. */
  private get live(): boolean {
    const status = this.current.status;
    return status === "connecting" || status === "connected" || status === "reconnecting";
  }

  private setState(state: ConnectionState): void {
    this.current = state;
    this.handlers.onState(state);
  }

  /** settle brings the connection to a final state, and gives up what it held. */
  private settle(state: ConnectionState): void {
    if (!this.live) {
      return;
    }
    clearTimeout(this.liveness);
    clearTimeout(this.retryTimer);
    document.removeEventListener("visibilitychange", this.wake);
    window.removeEventListener("pageshow", this.wake);
    window.removeEventListener("online", this.wake);
    window.removeEventListener("pagehide", this.leave);
    this.giveUp?.();
    this.giveUp = null;
    this.discard();
    this.release();
    this.setState(state);
  }

  /** finish ends the SSH Session for ending, or for the shell's end when its record came first. */
  private finish(ending: Ending): void {
    if (this.live) {
      forgetSession(this.id);
      this.settle({ status: "ended", ending: this.exit ?? ending });
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
    if (this.stream && this.live) {
      // A write an ended stream refuses is told of by the stream's end.
      this.stream.write(encodeRecord(record)).catch(() => {});
    }
  }

  /** open sends the open, once the earlier document's SSH Sessions are ended. */
  private open(): void {
    if (this.current.status !== "connecting") {
      return;
    }
    let sent = this.size;
    this.giveUp = ask(
      this.dial,
      () => {
        sent = this.size;
        return { open: this.host, cols: sent.columns, rows: sent.rows };
      },
      null,
      (answered) => {
        this.giveUp = null;
        const id = answered?.answer.tunnel;
        if (!answered || typeof id !== "string") {
          answered?.socket.close();
          const refusal = answered && refusalOf(answered.answer);
          this.settle(refusal ? { status: "refused", ...refusal } : { status: "unreachable" });
          return;
        }
        this.id = id;
        keepSessions((ids) => [...ids, id]);
        this.release = holdSession(id);
        const stream = new Stream(this.carry(answered.socket));
        this.stream = stream;
        this.watch();
        this.setState({ status: "connected" });
        if (sent !== this.size) {
          this.write({ type: "resize", ...this.size });
        }
        void this.relay(stream);
      },
    );
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

  /** relay gives the page the shell's output, and ends the connection as the stream ends. */
  private async relay(stream: Stream): Promise<void> {
    const records = new RecordReader();
    try {
      for (let bytes = await stream.read(); bytes !== null; bytes = await stream.read()) {
        records.push(bytes);
        for (let record = records.next(); record; record = records.next()) {
          if (record.type === "data") {
            await this.handlers.onOutput(record.data);
          } else if (record.type === "exitStatus") {
            this.exit = { kind: "exitStatus", status: record.status };
          } else if (record.type === "exitSignal") {
            this.exit = { kind: "exitSignal", signal: record.signal };
          } else {
            throw new MalformedRecord("a resize, which only the page sends");
          }
        }
      }
    } catch (err) {
      if (err instanceof MalformedRecord) {
        stream.close(err.message);
        this.finish({ kind: "protocol", message: err.message });
        return;
      }
      if (!(err instanceof StreamError)) {
        throw err;
      }
    }
    const err = await stream.ended;
    if (err.kind === "peerClosed") {
      this.finish({ kind: "notice", notice: err.reason });
    } else if (err.kind !== "closed") {
      this.finish({ kind: "protocol", message: err.message });
    }
  }

  /**
   * lose takes the stream off a WebSocket that closed or is taken for
   * dead, and starts resuming. A stream that has ended is not resumed: its
   * end is on its way to the page through relay.
   */
  private lose(): void {
    if (this.current.status !== "connected" || !this.stream || this.stream.error) {
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

  /**
   * retry tries to Resume on a new WebSocket, unless a try is under way.
   * It gives up at the grace, counted from the loss, and while the page is
   * hidden it does not try: wake does when the page is shown.
   */
  private retry(): void {
    const stream = this.stream;
    if (this.current.status !== "reconnecting" || this.trying || !stream) {
      return;
    }
    clearTimeout(this.retryTimer);
    const left = this.lostAt + timing.grace - Date.now();
    if (left <= 0) {
      this.finish({ kind: "lost", at: this.lostAt, message: "" });
      return;
    }
    if (document.visibilityState === "hidden") {
      this.retryTimer = setTimeout(() => this.retry(), left);
      return;
    }
    this.trying = true;
    this.giveUp = ask(
      this.dial,
      () => ({ resume: this.id, offset: stream.detach() }),
      timing.answer,
      (answered) => {
        this.giveUp = null;
        this.trying = false;
        const offset = answered?.answer.offset;
        if (answered && typeof offset === "number") {
          try {
            stream.resume(this.carry(answered.socket), offset);
          } catch (err) {
            this.finish({ kind: "protocol", message: err instanceof Error ? err.message : String(err) });
            return;
          }
          this.watch();
          this.setState({ status: "connected" });
          // A resize may have been lost unacknowledged, so the size goes again.
          this.write({ type: "resize", ...this.size });
          return;
        }
        answered?.socket.close();
        const refusal = answered && refusalOf(answered.answer);
        if (refusal?.code === "resume_refused") {
          this.finish({ kind: "lost", at: this.lostAt, message: refusal.message });
          return;
        }
        if (refusal) {
          this.settle({ status: "refused", ...refusal });
          return;
        }
        this.wait = this.wait === 0 ? timing.reconnectFirst : Math.min(2 * this.wait, timing.reconnectMax);
        this.retryTimer = setTimeout(() => this.retry(), Math.min(this.wait, this.lostAt + timing.grace - Date.now()));
      },
    );
  }

  /**
   * wake is the page shown again, or back online: a WebSocket that has
   * said nothing for probe is taken for dead at once, so a phone out of a
   * pocket does not wait out dead; and a Resume waiting is tried at once.
   */
  private readonly wake = (): void => {
    if (document.visibilityState === "hidden") {
      return;
    }
    if (this.current.status === "connected" && Date.now() - this.lastReceived >= timing.probe) {
      this.lose();
    } else {
      this.tryNow();
    }
  };

  /**
   * leave is the browser leaving the page for good, not keeping it for the
   * back-forward cache: the page sends the stream's close, at best effort,
   * since a browser may stop the page before the bytes leave. The id stays
   * kept, so that the next document ends the SSH Session if the close
   * never arrived.
   */
  private readonly leave = (event: PageTransitionEvent): void => {
    if (!event.persisted) {
      this.stream?.close();
      this.settle({ status: "ended", ending: { kind: "closed" } });
    }
  };
}
