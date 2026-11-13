/**
 * timing is the web terminal's timings, on the page's clock: the Workspace
 * socket's, each attachment's and the uploads'. Each but connect, answer,
 * still and retryFor is the koko server's or the device's of the same
 * purpose (internal/shared/timing), which the spec has the page share.
 */
export const timing = {
  /** probe is how long the page goes without sending on a WebSocket before it sends a probe (ServerProbe). */
  probe: 15_000,
  /** dead is how long it goes without a byte from the koko server before it takes the WebSocket for dead (ServerDead). */
  dead: 45_000,
  /** reconnectFirst is the wait after the first failed try to reconnect; it doubles up to reconnectMax. */
  reconnectFirst: 250,
  reconnectMax: 5_000,
  /** connect bounds how long one WebSocket takes to open (DeviceConnectAttempt). */
  connect: 10_000,
  /** answer bounds how long the answer to a first message takes, once the WebSocket is open (ResumeExchange). */
  answer: 5_000,
  /** still is how long the terminal's box stays the same size before the page tells the koko server of it. */
  still: 100,
  /** retryFor is how long a chunk or a finish of an upload whose connection failed is sent again. */
  retryFor: 15 * 60_000,
} as const;
