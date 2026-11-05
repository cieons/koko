# The resumable stream

The byte stream beneath each Tunnel's SSH Session, which outlives the channel
it runs on ([ADR 0021](../adr/0021-an-ssh-session-survives-a-network-drop-by-resuming-its-tunnel.md)).
One implementation, [`resumable.Stream`](../../internal/shared/resumable/resumable.go),
serves both ends: the device's, in `koko tunnel` and `koko exec`, and the
Gateway's. It knows nothing of SSH, Tunnels or Device Connections: it runs on
any `io.ReadWriteCloser`, owns that channel and closes it when it comes off.

## The interface

| Call | Who | What it does |
| --- | --- | --- |
| `New(ch)` | both | Starts a new Tunnel's stream on its first channel. Both ends start at position 0, so nothing is exchanged. |
| `Read`, `Write` | both | `Write` returns once the bytes are in the replay buffer, and blocks while it is full. |
| `CloseWrite` | both | Half-closes: the other end reads what came before, then `io.EOF`. |
| `CloseWithReason(r)`, `Close` | both | Ends the stream for good: sends what is unsent, then a close frame carrying `r`, and returns once the channel took them or failed. On a stalled channel that is when the connection's liveness check fails it, or when `Detach` is called. |
| `Err()` | both | Why the stream ended, or nil while it lives. |
| `Detached()` | both | A channel closed once the stream is off its latest channel. |
| `Detach()` | device | Takes the stream off its channel and returns its received offset, for the resume payload. |
| `Resume(ch, sent)` | device | Attaches `ch` after the Gateway accepted a resume whose payload carried `sent`. |
| `AcceptResume(ch, peer)` | Gateway | Attaches `ch`, given the device's offset `peer` from the resume payload. The last attach wins: the stream leaves its current channel. |

The stream has no timers. The 5 s resume exchange and the grace belong to its
callers, who close the channel to abort an exchange or `Close` the stream.

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
| The channel fails, or `Detach` or a newer attach takes the stream off it | Detached. `Write` keeps filling the buffer; `Read` waits. The old channel is closed in a goroutine of its own, since closing an SSH channel can block on a stalled connection. |
| A resume exchange fails on the channel | Stays Detached, to resume on another channel. |
| The other end's offset is outside the buffer | Ends with `ErrOffset`, and tells the other end in a close frame. |
| A malformed frame or resume answer, or a frame its state forbids (an acknowledgement of unsent positions, data after the EOF, more than 2 MiB unacknowledged) | Ends with `ErrMalformed`, and tells the other end in a close frame. |
| The other end's close frame | Ends with `*PeerClosedError` carrying the reason. `Read` first returns what arrived before it; once the other end has half-closed, `Read` returns `io.EOF` and only `Err` tells of the close. |
| `CloseWithReason` | Ends with `ErrClosed`, after sending what was unsent and the close frame. An attach in progress fails. |

## Verify

`go test ./internal/shared/resumable` runs the stream over in-memory channels
in `testing/synctest` bubbles. It moves a few MiB each way over channels cut
at random bytes, mid-frame and mid-exchange, through overlapping attaches, and
logs the seed it used. Other tests pin the frame bytes above, and check that a
full buffer blocks without loss, that a half-close survives a resume, and each
failure in the table.
