# The resumable stream

The byte stream beneath each Tunnel's SSH Session, which outlives the channel
it runs on ([ADR 0021](../adr/0021-an-ssh-session-survives-a-network-drop-by-resuming-its-tunnel.md)).
One implementation, [`resumable.Stream`](../../internal/shared/resumable/resumable.go),
serves both ends: the device's, in `koko tunnel` and `koko exec`, and the
Gateway's. It knows nothing of SSH, Tunnels or Device Connections: it runs on
any `io.ReadWriteCloser`, owns that channel and closes it when it comes off.

A web terminal's page runs a second implementation, [the TypeScript
end](#the-typescript-end), against the Gateway's Go end.

## The interface

| Call | Who | What it does |
| --- | --- | --- |
| `New(ch)` | both | Starts a new Tunnel's stream on its first channel. Both ends start at position 0, so nothing is exchanged. |
| `Read`, `Write` | both | `Write` returns once the bytes are in the replay buffer, and blocks while it is full. |
| `CloseWrite` | both | Half-closes: the other end reads what came before, then `io.EOF`. |
| `CloseWithReason(r)`, `Close` | both | Ends the stream for good: sends what is unsent, then a close frame carrying `r`, and returns once the channel took them or failed. On a stalled channel that is when the connection's liveness check fails it, or when `Detach` is called. Once a write on the channel has failed, nothing more is sent, and it returns once the channel's reads have failed too. |
| `Err()` | both | Why the stream ended, or nil while it lives. |
| `Probe()` | both | Asks for a frame on the current channel, for a caller that watches the channel's liveness: an acknowledgement alone (`00 00` when nothing is owed), unless a frame is on its way anyway. It does not wait for the frame, and sends nothing while Detached, then or after a resume. |
| `Detached()` | both | A channel closed once the stream is off its latest channel. |
| `Detach()` | device | Takes the stream off its channel and returns its received offset, for the resume payload. |
| `Resume(ch, sent)` | device | Attaches `ch` after the Gateway accepted a resume whose payload carried `sent`. |
| `AcceptResume(ch, peer)` | Gateway | Attaches `ch`, given the device's offset `peer` from the resume payload. The last attach wins: the stream leaves its current channel. |

The stream has no timers. The 5 s resume exchange and the grace belong to its
callers, who close the channel to abort an exchange or `Close` the stream. So
does liveness where the channel has none of its own, as a web terminal's
WebSocket: its caller calls `Probe` after a silence of its own choosing, and
the other end takes the frame as any acknowledgement.

## On the wire

Each direction is a sequence of positions: one per byte written, and one for
the EOF. A frame is a uvarint acknowledgement, the number of positions the
sender has read since its last one, then a uvarint size:

| Size | Frame | Then |
| --- | --- | --- |
| 0 to 32768 | data (0: an acknowledgement alone) | that many bytes |
| 32769 | EOF | nothing |
| 32770 | close | a uvarint length of at most 32768, then the reason in UTF-8 |

Any other size is malformed. For example, `00 02 68 69` is the data `hi`,
`00 81 80 02` an EOF, and `00 82 80 02 03 62 79 65` a close saying `bye`. An
EOF frame takes its position and is acknowledged and replayed like data, so a
half-close survives a resume. A close frame is never replayed: one lost with
its channel is lost, and the Gateway keeps a refused resume's reason instead.

On a resume, the device's received offset travels in the `tunnel@koko` open
payload. The Gateway answers with its own as one uvarint, the first bytes it
writes on the new channel, and both ends then resend from the offset the
other gave.

The frames and the resume answer are covered by `tunnel@koko`'s revision
([API](api.md#revisions), [ADR 0049](../adr/0049-the-cli-and-the-koko-server-work-together-within-a-window-of-versions.md)):
revision 1 is the layout above. A change to it, or a new frame type, comes
with a raise of `wire.TunnelRevision`; the stream then speaks the revision
its Tunnel opened at, any that some koko in the window speaks.

## Acknowledgements and the replay buffer

- **What an end acknowledges** is what its `Read` has returned, not what has
  merely arrived. So the other end's cap also bounds what this end holds
  unread.
- **When.** Acknowledgements ride on data frames; an end sends one alone once
  32 KiB are owed.
- **The received offset** is the same count. On losing its channel, an end
  drops what arrived beyond it, and the other end resends it.
- **The cap.** An end keeps what it has written and the other has not
  acknowledged: at most 2 MiB of positions, grown on demand and let go when
  empty. A full buffer blocks `Write` and `CloseWrite`, and drops nothing.

## When things go wrong

| Event | The stream |
| --- | --- |
| The channel fails, or `Detach` or a newer attach takes the stream off it | Detached. `Write` keeps filling the buffer; `Read` waits. The old channel is closed in a goroutine of its own, since closing an SSH channel can block on a stalled connection. A failed write stops only the sending: the stream comes off the channel once its reads fail too, after what arrived before, such as the other end's close frame, which an SSH channel the other end has closed still holds. |
| A resume exchange fails on the channel | Stays Detached, to resume on another channel. |
| The other end's offset is outside the buffer | Ends with `ErrOffset`, and tells the other end in a close frame. |
| A malformed frame or resume answer, or a frame its state forbids (an acknowledgement of unsent positions, data after the EOF, more than 2 MiB unacknowledged) | Ends with `ErrMalformed`, and tells the other end in a close frame, unless a write on the channel has failed. |
| The other end's close frame | Ends with `*PeerClosedError` carrying the reason. `Read` first returns what arrived before it; once the other end has half-closed, `Read` returns `io.EOF` and only `Err` tells of the close. |
| `CloseWithReason` | Ends with `ErrClosed`, after sending what was unsent and the close frame. An attach in progress fails. |

## The TypeScript end

A web terminal's page speaks the stream to the Gateway over a WebSocket
([ADR 0053](../adr/0053-a-browser-opens-ssh-sessions-through-a-websocket-to-the-gateway.md)),
so the web UI has an end of its own:
[`Stream`](../../web/src/terminal/stream.ts), a port of `resumable.Stream`'s
behaviour. The frames, the acknowledgements, the 2 MiB replay buffer and
each row of the table above are the same. What differs is what a page's
single thread asks for:

| Go | TypeScript | Why |
| --- | --- | --- |
| `New(ch)` on an `io.ReadWriteCloser` | `new Stream(socket)` on anything with `send` and `onmessage` | A WebSocket's messages arrive whole; a frame may span them. A `send` that throws is the socket's failure. |
| `Read`, `Write` and `CloseWrite` block | `read`, `write` and `closeWrite` return promises | `read` resolves with the next bytes, or `null` for the other end's half-close; `write` resolves once its bytes are in the replay buffer. `ended` resolves with why the stream ended. |
| `Resume`, `AcceptResume` | `resume(socket, peerReceived)` | The terminal carries each end's offset in its first messages' JSON, so nothing is exchanged on the stream itself. |
| The stream closes its channel | The caller closes the socket | The stream only stops using a socket it leaves. After a failed send it stays on the socket, to read what arrived before, until its caller, told of the WebSocket's close, calls `detach`. |
| `CloseWithReason` waits for the channel | `close(reason)` returns at once | A WebSocket's `send` never blocks, so the close frame is handed over before `close` returns. |

Its caller is [`connection.ts`](../../web/src/terminal/connection.ts), the
page's end of a terminal's SSH Session: the first messages, the terminal
records ([`records.ts`](../../web/src/terminal/records.ts)), liveness, the
backoff and the grace.

## Verify

`cd web && node_modules/.bin/vitest run src/terminal` runs the TypeScript
end. `stream.test.ts` reads `frames.golden` (the `tunnel@koko 1` section)
and `wire/testdata/terminal.golden` from the repository and encodes and
decodes each line, so a change to the Go end's frames or records that the
TypeScript end does not match fails there. It also moves a few MiB each way
over in-memory sockets cut at random bytes, logging its seed, and checks the
full buffer, the half-close, the close frame and each failure in the table.

`go test ./internal/shared/resumable` runs the stream over in-memory channels
in `testing/synctest` bubbles. It moves a few MiB each way over channels cut
at random bytes, mid-frame and mid-exchange, through overlapping attaches, and
logs the seed it used. Other tests pin the frame bytes above, and check that a
full buffer blocks without loss, that a half-close survives a resume, that a
probe is one acknowledgement-only frame and none while Detached, and each
failure in the table. `TestTheFramesAreRecordedByRevision` records the
frames and the resume answer of each revision of `tunnel@koko` in
`testdata/frames.golden`, a section a revision, which never changes once
written ([`internal/harness/frozen`](../../internal/harness/frozen/frozen.go)).
