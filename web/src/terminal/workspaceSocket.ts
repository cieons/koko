import { terminalProtocol } from "./records";
import { timing } from "./timing";

// The page's end of the Workspace socket, /terminal/workspace (spec #213,
// "The Workspace socket"): the User's Workspace, sent whole by the koko
// server after every change, and the requests that change it, each
// answered by its number. JSON text messages both ways. The rail lists
// the Workspace on every page, so this module is in the first load.

/** TabState is a Workspace tab's state, as the koko server gives it. */
export type TabState = "starting" | "running" | "ended" | "refused" | "restarted";

/**
 * Ended is how a tab's SSH Session ended: the reason the log gives, the
 * Gateway's notice, which the page says, and the shell's exit, if it exited.
 */
export interface Ended {
  reason: string;
  notice?: string;
  exitStatus?: number;
  exitSignal?: string;
}

/** Refusal is a refusal's code, which the page can test, and the koko server's words. */
export interface Refusal {
  code: string;
  message: string;
}

/** WorkspaceTab is one tab of the Workspace, as the koko server gives it. */
export interface WorkspaceTab {
  /** id names the tab for its life, across the SSH Sessions it holds. */
  id: string;
  /** host is the Host's name now, or the tab's label for a Host the caller can no longer see. */
  host: string;
  label: string;
  /** session is its SSH Session's id, which the page attaches to and the Files panel's calls name. */
  session?: string;
  state: TabState;
  ended?: Ended;
  refused?: Refusal;
}

/** Request is what the page asks of the Workspace. */
export type Request =
  | { open: string; cols: number; rows: number }
  | { reopen: string; cols: number; rows: number }
  | { close: string };

/**
 * Answer is a request's answer: the tab opened or reopened, a close done, a
 * refusal, or lost, for an open sent on a WebSocket that dropped before its
 * answer, which the page does not send again since it may have been taken.
 */
export type Answer = { kind: "tab"; tab: string } | { kind: "done" } | ({ kind: "refused" } & Refusal) | { kind: "lost" };

/** SocketState is what the page shows of its Workspace socket. */
export type SocketState =
  | { status: "connecting" }
  | { status: "open" }
  /** The WebSocket was lost, and the page reconnects; stopping says the koko server said it was restarting. */
  | { status: "reconnecting"; stopping: boolean }
  /** The koko server refused the page at its first message, such as signin_required or reload_page; nothing more is tried. */
  | ({ status: "refused" } & Refusal);

/** WorkspaceHandlers is what the socket tells its page. */
export interface WorkspaceHandlers {
  /** onWorkspace is each Workspace the koko server sends, whole, which replaces the tabs the page holds. */
  onWorkspace(tabs: WorkspaceTab[]): void;
  onState(state: SocketState): void;
}

/** TextSocket is the part of a WebSocket the Workspace socket uses. */
export interface TextSocket {
  onopen: ((event: Event) => void) | null;
  onmessage: ((event: MessageEvent) => void) | null;
  onclose: ((event: CloseEvent) => void) | null;
  send(data: string): void;
  close(): void;
}

/** dialWorkspace opens a WebSocket to the Workspace socket of the page's own origin. */
function dialWorkspace(): TextSocket {
  const scheme = window.location.protocol === "https:" ? "wss:" : "ws:";
  return new WebSocket(`${scheme}//${window.location.host}/terminal/workspace`);
}

const tabStates: ReadonlySet<string> = new Set<TabState>(["starting", "running", "ended", "refused", "restarted"]);

/** text is a field's string, if it is one. */
const text = (value: unknown): string | undefined => (typeof value === "string" ? value : undefined);

/** refusalIn is a refusal's code and words, from an object holding them, if it does. */
function refusalIn(value: unknown, code: string): Refusal | undefined {
  if (typeof value !== "object" || value === null) {
    return undefined;
  }
  const fields = value as Record<string, unknown>;
  const found = text(fields[code]);
  return found === undefined ? undefined : { code: found, message: text(fields.message) ?? "" };
}

/** tabOf reads one tab of a Workspace message, or undefined for one that is no tab. */
function tabOf(value: unknown): WorkspaceTab | undefined {
  if (typeof value !== "object" || value === null) {
    return undefined;
  }
  const fields = value as Record<string, unknown>;
  const [id, host, label, state] = [text(fields.id), text(fields.host), text(fields.label), text(fields.state)];
  if (id === undefined || host === undefined || label === undefined || state === undefined || !tabStates.has(state)) {
    return undefined;
  }
  const tab: WorkspaceTab = { id, host, label, state: state as TabState };
  const session = text(fields.session);
  if (session !== undefined && session !== "") {
    tab.session = session;
  }
  if (typeof fields.ended === "object" && fields.ended !== null) {
    const ended = fields.ended as Record<string, unknown>;
    tab.ended = { reason: text(ended.reason) ?? "" };
    const notice = text(ended.notice);
    if (notice !== undefined && notice !== "") {
      tab.ended.notice = notice;
    }
    if (typeof ended.exit_status === "number") {
      tab.ended.exitStatus = ended.exit_status;
    }
    const signal = text(ended.exit_signal);
    if (signal !== undefined && signal !== "") {
      tab.ended.exitSignal = signal;
    }
  }
  const refused = refusalIn(fields.refused, "code");
  if (refused) {
    tab.refused = refused;
  }
  return tab;
}

/** Pending is a request not yet answered: sent says it is on the WebSocket now open. */
interface Pending {
  request: Request;
  resolve: (answer: Answer) => void;
  sent: boolean;
}

/**
 * WorkspaceSocket is the page's one Workspace socket, from the frame's
 * first showing until signing out. It reconnects after a drop, at once and
 * then after the Resume's backoff, and gets the Workspace whole again.
 * Requests wait for the Workspace's first message; a close or a reopen on a
 * WebSocket that dropped is sent again on the next, since the koko server
 * answers both the same when they come twice, and an open is given up.
 */
export class WorkspaceSocket {
  private readonly dial: () => TextSocket;
  private readonly handlers: WorkspaceHandlers;
  private current: SocketState = { status: "connecting" };
  private socket: TextSocket | null = null;
  /** ready says the WebSocket now open has brought the Workspace, so requests may go on it. */
  private ready = false;
  /** stopping says the koko server said it was restarting, until the next Workspace. */
  private stopping = false;
  private ended = false;
  private nextRequest = 1;
  private readonly pending = new Map<number, Pending>();

  // Liveness, as an attachment's: when the page last sent and last heard.
  private lastSent = 0;
  private lastReceived = 0;
  private liveness: ReturnType<typeof setTimeout> | undefined;
  /** bound gives up a WebSocket that does not open, or whose first answer does not come, in time. */
  private bound: ReturnType<typeof setTimeout> | undefined;
  private wait = 0;
  private retryTimer: ReturnType<typeof setTimeout> | undefined;

  constructor(handlers: WorkspaceHandlers, dial: () => TextSocket = dialWorkspace) {
    this.dial = dial;
    this.handlers = handlers;
    document.addEventListener("visibilitychange", this.wake);
    window.addEventListener("pageshow", this.wake);
    window.addEventListener("online", this.wake);
    this.connect();
  }

  /** state is the socket's state now. */
  get state(): SocketState {
    return this.current;
  }

  /** request asks the Workspace for a change, and is its answer. */
  request(request: Request): Promise<Answer> {
    if (this.ended || this.current.status === "refused") {
      return Promise.resolve({ kind: "lost" });
    }
    const n = this.nextRequest++;
    return new Promise((resolve) => {
      this.pending.set(n, { request, resolve, sent: false });
      this.flush();
    });
  }

  /** close leaves the Workspace, as on signing out: the WebSocket goes, and so does every request waiting. */
  close(): void {
    if (this.ended) {
      return;
    }
    this.ended = true;
    clearTimeout(this.liveness);
    clearTimeout(this.bound);
    clearTimeout(this.retryTimer);
    document.removeEventListener("visibilitychange", this.wake);
    window.removeEventListener("pageshow", this.wake);
    window.removeEventListener("online", this.wake);
    this.discard();
    this.giveUpPending(() => true);
  }

  private setState(state: SocketState): void {
    this.current = state;
    this.handlers.onState(state);
  }

  private send(message: object): void {
    this.lastSent = Date.now();
    this.socket?.send(JSON.stringify(message));
  }

  /** flush sends the requests not yet on the WebSocket, once its Workspace has come. */
  private flush(): void {
    if (!this.ready) {
      return;
    }
    for (const [n, each] of this.pending) {
      if (!each.sent) {
        each.sent = true;
        this.send({ request: n, ...each.request });
      }
    }
  }

  private giveUpPending(which: (each: Pending) => boolean): void {
    for (const [n, each] of this.pending) {
      if (which(each)) {
        this.pending.delete(n);
        each.resolve({ kind: "lost" });
      }
    }
  }

  /** discard closes the WebSocket, if any, and hears no more from it. */
  private discard(): void {
    const socket = this.socket;
    this.socket = null;
    this.ready = false;
    if (socket) {
      socket.onopen = socket.onmessage = socket.onclose = null;
      socket.close();
    }
  }

  private connect(): void {
    clearTimeout(this.retryTimer);
    let socket: TextSocket;
    try {
      socket = this.dial();
    } catch {
      // A page that cannot open a WebSocket at all tries again, as after a drop.
      this.retryTimer = setTimeout(() => this.lose(), 0);
      return;
    }
    this.socket = socket;
    this.bound = setTimeout(() => this.lose(), timing.connect);
    socket.onopen = () => {
      clearTimeout(this.bound);
      this.bound = setTimeout(() => this.lose(), timing.answer);
      this.lastReceived = Date.now();
      this.send({ protocol: terminalProtocol });
    };
    socket.onmessage = (event) => this.receive(event.data);
    socket.onclose = () => this.lose();
  }

  private receive(data: unknown): void {
    this.lastReceived = Date.now();
    let message: unknown = null;
    try {
      message = typeof data === "string" ? JSON.parse(data) : null;
    } catch {
      // No JSON is no message.
    }
    if (typeof message !== "object" || message === null || Array.isArray(message)) {
      this.lose();
      return;
    }
    const fields = message as Record<string, unknown>;
    if (!this.ready) {
      clearTimeout(this.bound);
      const refusal = refusalIn(fields, "refused");
      if (refusal) {
        this.discard();
        this.close();
        this.setState({ status: "refused", ...refusal });
        return;
      }
      if (!Array.isArray(fields.tabs)) {
        this.lose();
        return;
      }
      this.ready = true;
      this.wait = 0;
      this.stopping = false;
      this.setState({ status: "open" });
      this.watch();
    }
    if (Array.isArray(fields.tabs)) {
      this.handlers.onWorkspace(fields.tabs.flatMap((each) => tabOf(each) ?? []));
      this.flush();
    } else if (typeof fields.request === "number") {
      this.answer(fields.request, fields);
    } else if (typeof fields.stopping === "string") {
      this.stopping = true;
    }
    // {} is the koko server's probe: hearing it is all it is for.
  }

  private answer(n: number, fields: Record<string, unknown>): void {
    const each = this.pending.get(n);
    if (!each) {
      return;
    }
    this.pending.delete(n);
    const refusal = refusalIn(fields, "refused");
    const tab = text(fields.tab);
    each.resolve(refusal ? { kind: "refused", ...refusal } : tab !== undefined ? { kind: "tab", tab } : { kind: "done" });
  }

  /**
   * watch is the liveness watch: a probe, {}, after probe without sending,
   * and the WebSocket taken for dead after dead without a message. It reads
   * the wall clock, which moves on while a phone sleeps.
   */
  private watch(): void {
    clearTimeout(this.liveness);
    const now = Date.now();
    if (now - this.lastReceived >= timing.dead) {
      this.lose();
      return;
    }
    if (now - this.lastSent >= timing.probe) {
      this.send({});
    }
    const next = Math.min(this.lastSent + timing.probe, this.lastReceived + timing.dead);
    this.liveness = setTimeout(() => this.watch(), next - now);
  }

  /**
   * lose takes the page off a WebSocket that closed, failed or is taken for
   * dead, and reconnects: at once, then after reconnectFirst doubling to
   * reconnectMax; while the page is hidden it waits to be shown.
   */
  private lose(): void {
    if (this.ended) {
      return;
    }
    clearTimeout(this.liveness);
    clearTimeout(this.bound);
    clearTimeout(this.retryTimer);
    this.discard();
    this.giveUpPending((each) => each.sent && "open" in each.request);
    for (const each of this.pending.values()) {
      each.sent = false;
    }
    if (this.current.status !== "reconnecting" || this.current.stopping !== this.stopping) {
      this.setState({ status: "reconnecting", stopping: this.stopping });
    }
    if (document.visibilityState === "hidden") {
      return;
    }
    const wait = this.wait;
    this.wait = wait === 0 ? timing.reconnectFirst : Math.min(2 * wait, timing.reconnectMax);
    this.retryTimer = setTimeout(() => this.connect(), wait);
  }

  /**
   * wake is the page shown again, or back online: a WebSocket silent for
   * probe is taken for dead at once, and a reconnect waiting is tried now.
   */
  private readonly wake = (): void => {
    if (document.visibilityState === "hidden" || this.ended) {
      return;
    }
    if (this.ready && Date.now() - this.lastReceived >= timing.probe) {
      this.lose();
    } else if (this.current.status === "reconnecting" && this.socket === null) {
      this.wait = 0;
      this.connect();
    }
  };
}
