import { bytesOf, readUvarint, Unread, uvarint } from "./bytes";

// The resumable stream's TypeScript end: the byte stream beneath a web
// terminal's SSH Session, which outlives the WebSocket it runs on. It is a
// port of resumable.Stream (internal/shared/resumable), whose frames, offsets
// and failures are in docs/subsystems/resumable-stream.md; both ends are
// pinned by testdata/frames.golden, which stream.test.ts reads.

/** maxData is the most data one frame carries. */
export const maxData = 32 << 10;

/**
 * maxBuffer is the most positions an end keeps for the other to
 * acknowledge, per direction; a full buffer holds the writer back.
 */
export const maxBuffer = 2 << 20;

/** ackThreshold is how much an end delivers before it sends an acknowledgement alone. */
const ackThreshold = maxData;

/** A frame's size field is a data length up to maxData, or one of these frame types. */
const sizeEOF = maxData + 1;
const sizeClose = maxData + 2;

/**
 * StreamError is why a stream ended, or why a call on it failed:
 *
 * - `closed`: this end closed the stream;
 * - `peerClosed`: the other end closed it, giving reason, perhaps empty;
 * - `offset`: the other end's received offset is outside this end's replay
 *   buffer, gone or never sent;
 * - `malformed`: the other end sent bytes that are no frame, or a frame its
 *   state forbids;
 * - `writeAfterCloseWrite`: a write after closeWrite, which ends nothing.
 */
export class StreamError extends Error {
  readonly kind: "closed" | "peerClosed" | "offset" | "malformed" | "writeAfterCloseWrite";
  /** reason is what the other end's close gave. */
  readonly reason: string;

  constructor(kind: StreamError["kind"], message: string, reason = "") {
    super(message);
    this.name = "StreamError";
    this.kind = kind;
    this.reason = reason;
  }
}

function malformed(what: string): StreamError {
  return new StreamError("malformed", `malformed frame: ${what}`);
}

/** Frame is one frame: its acknowledgement, and what it carries. */
export type Frame =
  /** Data, or with none an acknowledgement alone. */
  | { kind: "data"; ack: number; data: Uint8Array }
  | { kind: "eof"; ack: number }
  /** A close, whose reason is at most maxData bytes of UTF-8. */
  | { kind: "close"; ack: number; reason: string };

/** encodeFrame encodes one frame. */
export function encodeFrame(frame: Frame): Uint8Array<ArrayBuffer> {
  switch (frame.kind) {
    case "data":
      return bytesOf([...uvarint(frame.ack), ...uvarint(frame.data.length)], frame.data);
    case "eof":
      return bytesOf([...uvarint(frame.ack), ...uvarint(sizeEOF)]);
    case "close": {
      const reason = new TextEncoder().encode(frame.reason);
      return bytesOf([...uvarint(frame.ack), ...uvarint(sizeClose), ...uvarint(reason.length)], reason);
    }
  }
}

/** FrameReader decodes the frames in the bytes pushed into it. */
export class FrameReader {
  private readonly unread = new Unread();

  /** push adds the channel's next bytes, which the caller no longer changes. */
  push(bytes: Uint8Array): void {
    this.unread.push(bytes);
  }

  /**
   * next returns the next whole frame, or null until more bytes come. A
   * broken frame fails with a StreamError of kind malformed.
   */
  next(): Frame | null {
    const bytes = this.unread.rest;
    const field = (at: number, what: string) => {
      const read = readUvarint(bytes, at);
      if (read === "overflow") {
        throw malformed(`${what}: a uvarint past 64 bits`);
      }
      return read;
    };
    const ack = field(0, "an acknowledgement");
    if (ack === "short") {
      return null;
    }
    const size = field(ack.next, "a size");
    if (size === "short") {
      return null;
    }
    if (size.value === sizeEOF) {
      this.unread.skip(size.next);
      return { kind: "eof", ack: ack.value };
    }
    let length = size;
    if (size.value === sizeClose) {
      const reason = field(size.next, "a reason's length");
      if (reason === "short") {
        return null;
      }
      if (reason.value > maxData) {
        throw malformed(`a reason of ${reason.value} bytes, more than ${maxData}`);
      }
      length = reason;
    } else if (size.value > maxData) {
      throw malformed(`size ${size.value}, neither data of at most ${maxData} bytes nor a frame type`);
    }
    const end = length.next + length.value;
    if (bytes.length < end) {
      return null;
    }
    const body = bytes.subarray(length.next, end);
    this.unread.skip(end);
    if (size.value === sizeClose) {
      return { kind: "close", ack: ack.value, reason: new TextDecoder().decode(body) };
    }
    return { kind: "data", ack: ack.value, data: body };
  }
}

/**
 * StreamSocket is what a stream runs on: a WebSocket in binary mode, or
 * anything that sends bytes in order and hands those that arrive, in order,
 * to onmessage, as an ArrayBuffer or a view of one. A send that throws is
 * taken as the socket having failed.
 */
export interface StreamSocket {
  send(data: Uint8Array<ArrayBuffer>): void;
  onmessage: ((event: MessageEvent) => void) | null;
}

/** Attachment is the socket a stream runs on. */
interface Attachment {
  socket: StreamSocket;
  frames: FrameReader;
  /** sendFailed tells that a send failed, after which nothing more is sent on the socket. */
  sendFailed: boolean;
}

/** ReplayBuffer holds what this end wrote and the other has not acknowledged. */
class ReplayBuffer {
  private bytes = new Uint8Array(0);
  private start = 0;
  private end = 0;

  get length(): number {
    return this.end - this.start;
  }

  append(more: Uint8Array): void {
    if (this.end + more.length > this.bytes.length) {
      const held = this.bytes.subarray(this.start, this.end);
      const needed = held.length + more.length;
      if (needed > this.bytes.length) {
        const grown = new Uint8Array(Math.min(maxBuffer, Math.max(needed, 2 * this.bytes.length)));
        grown.set(held);
        this.bytes = grown;
      } else {
        this.bytes.copyWithin(0, this.start, this.end);
      }
      this.start = 0;
      this.end = held.length;
    }
    this.bytes.set(more, this.end);
    this.end += more.length;
  }

  /** slice is the length bytes from offset on. */
  slice(offset: number, length: number): Uint8Array {
    return this.bytes.subarray(this.start + offset, this.start + offset + length);
  }

  /** drop lets go of the first n bytes, and of the array once nothing is left. */
  drop(n: number): void {
    this.start += n;
    if (this.start === this.end) {
      this.bytes = new Uint8Array(0);
      this.start = this.end = 0;
    }
  }
}

/** Writer is a write waiting for room in the replay buffer, or with no data a closeWrite. */
interface Writer {
  data: Uint8Array | null;
  resolve: () => void;
  reject: (err: StreamError) => void;
}

/** Reader is a read waiting for bytes. */
interface Reader {
  max: number;
  resolve: (bytes: Uint8Array | null) => void;
  reject: (err: StreamError) => void;
}

/**
 * Stream is the page's end of a resumable byte stream. Where the Go end
 * blocks, this end's calls return promises; where it owns and closes its
 * channel, this end only stops using its socket, which its caller closes.
 * It has no timers: liveness, the backoff and the grace are its caller's.
 */
export class Stream {
  private att: Attachment | null = null;
  private err: StreamError | null = null;
  /** A close frame carrying reason is due after the data. */
  private owesClose = false;
  private reason = "";
  /** probe asked for a frame on att, and none has gone since. */
  private owesProbe = false;
  private pumping = false;

  // This end's writes, by position (a byte each, then one for the EOF):
  // acknowledged up to outAcked, sent on the current socket up to outSent,
  // written up to outEnd. out holds the data from outAcked on.
  private readonly out = new ReplayBuffer();
  private outAcked = 0;
  private outSent = 0;
  private outEnd = 0;
  /** outEnd's last position is the EOF. */
  private outEOF = false;
  private readonly writers: Writer[] = [];

  // The other end's writes: read has delivered up to inDelivered, this end
  // has acknowledged up to inAcked, and in holds what arrived beyond.
  private in: Uint8Array[] = [];
  private inLength = 0;
  private inEOF: "none" | "received" | "delivered" = "none";
  private inDelivered = 0;
  private inAcked = 0;
  private readonly readers: Reader[] = [];

  /**
   * ended resolves with why the stream ended, once it has. A read may still
   * return what arrived before the other end's close.
   */
  readonly ended: Promise<StreamError>;
  private resolveEnded: (err: StreamError) => void = () => {};

  /**
   * A new SSH Session's stream starts on its first socket. Both ends start
   * at position 0, so nothing is exchanged.
   */
  constructor(socket: StreamSocket) {
    this.ended = new Promise((resolve) => {
      this.resolveEnded = resolve;
    });
    this.attach(socket);
  }

  /** error is why the stream ended, or null while it lives. */
  get error(): StreamError | null {
    return this.err;
  }

  /**
   * detach takes the stream off its socket, if any, and returns its
   * received offset: what the resume message carries, unchanged until the
   * next resume.
   */
  detach(): number {
    this.drop(this.att);
    return this.inDelivered;
  }

  /**
   * probe asks for a frame on the stream's socket, for a caller that
   * watches the socket's liveness: an acknowledgement alone when nothing
   * else is owed. While the stream is detached it sends nothing, then or
   * later.
   */
  probe(): void {
    if (this.att) {
      this.owesProbe = true;
      this.pump();
    }
  }

  /**
   * resume carries the stream onto socket, off its current one (the last
   * attach wins), once the Gateway accepted a resume carrying detach's
   * offset and answered with peerReceived, its own. It resends from there.
   * An ended stream throws why it ended; an offset outside the replay
   * buffer ends it, telling the other end why on socket, and throws.
   */
  resume(socket: StreamSocket, peerReceived: number): void {
    this.drop(this.att);
    if (this.err) {
      throw this.err;
    }
    if (!Number.isSafeInteger(peerReceived) || peerReceived < this.outAcked || peerReceived > this.outSent) {
      const err = new StreamError(
        "offset",
        "received offset outside the replay buffer: " +
          `the other end has received ${peerReceived}, and this end holds ${this.outAcked} to ${this.outSent}`,
      );
      this.end(err);
      try {
        socket.send(encodeFrame({ kind: "close", ack: 0, reason: err.message }));
      } catch {
        // The socket failed; the other end learns nothing more on it.
      }
      throw err;
    }
    this.free(peerReceived);
    this.outSent = peerReceived;
    this.inAcked = this.inDelivered;
    this.attach(socket);
    this.admit();
  }

  /**
   * read resolves with what the other end wrote next, at most max bytes,
   * then with null once it has half-closed. After the other end's close it
   * still gives what came before, then rejects with why the stream ended.
   * What a read has returned is what this end acknowledges, so a reader
   * that falls behind holds the other end's writes back.
   */
  read(max = Number.POSITIVE_INFINITY): Promise<Uint8Array | null> {
    return new Promise((resolve, reject) => {
      this.readers.push({ max, resolve, reject });
      this.serve();
      this.pump();
    });
  }

  /**
   * write resolves once bytes, which the caller does not change meanwhile,
   * are in the replay buffer, and waits while the buffer is full. Writes
   * go out in the order they were made.
   */
  write(bytes: Uint8Array): Promise<void> {
    return new Promise((resolve, reject) => {
      this.writers.push({ data: bytes, resolve, reject });
      this.admit();
    });
  }

  /**
   * closeWrite half-closes the stream: the other end reads what came
   * before, then the end. It waits while the buffer is full.
   */
  closeWrite(): Promise<void> {
    return new Promise((resolve, reject) => {
      this.writers.push({ data: null, resolve, reject });
      this.admit();
    });
  }

  /**
   * close ends the stream for good. On a socket it sends what is unsent,
   * then a close frame carrying reason (cut to maxData bytes), and comes
   * off the socket, which the caller then closes. Detached, it sends
   * nothing: a close frame is never replayed.
   */
  close(reason = ""): void {
    this.closeWith(new StreamError("closed", "resumable stream closed"), cut(reason));
    this.pump();
  }

  /** end ends the stream for err, unless it has ended, and says whether it did. */
  private end(err: StreamError): boolean {
    if (this.err) {
      return false;
    }
    this.err = err;
    this.resolveEnded(err);
    for (const writer of this.writers.splice(0)) {
      writer.reject(err);
    }
    this.serve();
    return true;
  }

  /** closeWith ends the stream for err and owes the other end a close frame carrying reason. */
  private closeWith(err: StreamError, reason: string): void {
    if (this.end(err)) {
      this.owesClose = true;
      this.reason = reason;
    }
  }

  /** free lets go of what the other end has acknowledged, up to upTo. */
  private free(upTo: number): void {
    this.out.drop(Math.min(upTo - this.outAcked, this.out.length));
    this.outAcked = upTo;
  }

  /** attach starts the stream on socket. */
  private attach(socket: StreamSocket): void {
    const a: Attachment = { socket, frames: new FrameReader(), sendFailed: false };
    this.att = a;
    socket.onmessage = (event) => this.receive(a, event.data);
    this.pump();
  }

  /**
   * drop takes the stream off a, if still on it, and while the stream
   * lives drops what arrived unread, which the other end resends.
   */
  private drop(a: Attachment | null): void {
    if (!a || this.att !== a) {
      return;
    }
    this.att = null;
    this.owesProbe = false;
    a.socket.onmessage = null;
    if (!this.err) {
      this.in = [];
      this.inLength = 0;
      if (this.inEOF === "received") {
        this.inEOF = "none";
      }
    }
  }

  /** admit takes the waiting writes into the replay buffer, as far as it has room. */
  private admit(): void {
    for (let w = this.writers[0]; w; w = this.writers[0]) {
      if (this.err) {
        w.reject(this.err);
      } else if (w.data === null) {
        if (!this.outEOF) {
          if (this.outEnd - this.outAcked >= maxBuffer) {
            break;
          }
          this.outEOF = true;
          this.outEnd++;
        }
        w.resolve();
      } else if (this.outEOF) {
        w.reject(new StreamError("writeAfterCloseWrite", "resumable stream: write after closeWrite"));
      } else {
        const taken = Math.min(maxBuffer - (this.outEnd - this.outAcked), w.data.length);
        this.out.append(w.data.subarray(0, taken));
        this.outEnd += taken;
        w.data = w.data.subarray(taken);
        if (w.data.length > 0) {
          break;
        }
        w.resolve();
      }
      this.writers.shift();
    }
    this.pump();
  }

  /** serve gives the waiting reads what has arrived. */
  private serve(): void {
    for (let r = this.readers[0]; r; r = this.readers[0]) {
      const head = this.in[0];
      if (this.err && this.err.kind !== "peerClosed") {
        r.reject(this.err);
      } else if (head) {
        if (head.length > r.max) {
          this.in[0] = head.subarray(r.max);
        } else {
          this.in.shift();
        }
        const bytes = head.subarray(0, r.max);
        this.inLength -= bytes.length;
        this.inDelivered += bytes.length;
        r.resolve(bytes);
      } else if (this.inEOF !== "none") {
        if (this.inEOF === "received") {
          this.inEOF = "delivered";
          this.inDelivered++;
        }
        r.resolve(null);
      } else if (this.err) {
        r.reject(this.err);
      } else {
        return;
      }
      this.readers.shift();
    }
  }

  /** owesFrame says whether this end has a frame to send. */
  private owesFrame(): boolean {
    return (
      this.outSent < this.outEnd ||
      this.owesClose ||
      this.owesProbe ||
      this.inDelivered - this.inAcked >= ackThreshold
    );
  }

  /**
   * pump sends frames on the stream's socket for as long as it owes one. A
   * send that fails stops the sending but leaves the stream on the socket:
   * the other end may have sent frames, its close frame among them, before
   * the socket failed, and they still arrive. The caller takes the stream
   * off once the socket has closed.
   */
  private pump(): void {
    if (this.pumping) {
      return; // a socket that answers inside send; the loop below goes on
    }
    this.pumping = true;
    try {
      for (let a = this.att; a && a === this.att && !a.sendFailed && this.owesFrame(); ) {
        const frame = this.nextFrame();
        try {
          a.socket.send(encodeFrame(frame));
        } catch {
          a.sendFailed = true;
        }
        if (frame.kind === "close") {
          this.drop(a);
        }
      }
    } finally {
      this.pumping = false;
    }
  }

  /**
   * nextFrame is the next frame to send: unsent data, then the EOF, then a
   * close frame owed, or else an acknowledgement alone. Whichever it is,
   * it is the frame a probe asked for.
   */
  private nextFrame(): Frame {
    const ack = this.inDelivered - this.inAcked;
    this.inAcked = this.inDelivered;
    this.owesProbe = false;
    const dataEnd = this.outEnd - (this.outEOF ? 1 : 0);
    if (this.outSent < dataEnd) {
      const length = Math.min(maxData, dataEnd - this.outSent);
      const data = this.out.slice(this.outSent - this.outAcked, length);
      this.outSent += length;
      return { kind: "data", ack, data };
    }
    if (this.outSent < this.outEnd) {
      this.outSent++;
      return { kind: "eof", ack };
    }
    if (this.owesClose) {
      this.owesClose = false;
      return { kind: "close", ack, reason: this.reason };
    }
    return { kind: "data", ack, data: new Uint8Array(0) };
  }

  /** receive takes a message from a's socket, which hands it over only while the stream is on it. */
  private receive(a: Attachment, message: unknown): void {
    try {
      if (message instanceof ArrayBuffer) {
        a.frames.push(new Uint8Array(message));
      } else if (ArrayBuffer.isView(message)) {
        a.frames.push(new Uint8Array(message.buffer, message.byteOffset, message.byteLength));
      } else {
        throw malformed("a message that is not binary");
      }
      for (let frame = a.frames.next(); frame; frame = this.att === a ? a.frames.next() : null) {
        this.apply(frame);
      }
    } catch (err) {
      if (!(err instanceof StreamError)) {
        throw err;
      }
      this.stop(a, err);
    }
    this.serve();
    this.admit();
  }

  /**
   * stop handles what stopped the receiving on a: the other end's close,
   * or a malformed frame, which the other end is told of in a close frame,
   * pump's next, unless a send has failed. Either ends the stream and
   * takes it off the socket.
   */
  private stop(a: Attachment, err: StreamError): void {
    if (err.kind === "peerClosed") {
      this.end(err);
      this.drop(a);
      return;
    }
    this.closeWith(err, err.message);
    if (a.sendFailed) {
      this.drop(a);
    }
  }

  /** apply applies one frame from the other end, unless its state forbids it. */
  private apply(frame: Frame): void {
    const unacked = this.outSent - this.outAcked;
    if (frame.ack > unacked) {
      throw malformed(`it acknowledges ${frame.ack} bytes, of ${unacked} sent and unacknowledged`);
    }
    this.free(this.outAcked + frame.ack);
    if (frame.kind === "close") {
      const why = frame.reason === "" ? "" : `: ${frame.reason}`;
      throw new StreamError("peerClosed", `the other end closed the resumable stream${why}`, frame.reason);
    }
    const length = frame.kind === "eof" ? 1 : frame.data.length;
    if (this.inEOF !== "none" && length > 0) {
      throw malformed("data or an EOF after the EOF");
    }
    const held = this.inDelivered + this.inLength + length - this.inAcked;
    if (held > maxBuffer) {
      throw malformed(`${held} bytes unacknowledged, more than the ${maxBuffer} a buffer holds`);
    }
    if (frame.kind === "eof") {
      this.inEOF = "received";
    } else if (length > 0) {
      this.in.push(frame.data);
      this.inLength += length;
    }
  }
}

/** cut cuts reason to at most maxData bytes of UTF-8, at a character's edge. */
function cut(reason: string): string {
  const bytes = new TextEncoder().encode(reason);
  if (bytes.length <= maxData) {
    return reason;
  }
  let end = maxData;
  while (end > 0 && ((bytes[end] ?? 0) & 0xc0) === 0x80) {
    end--;
  }
  return new TextDecoder().decode(bytes.subarray(0, end));
}
