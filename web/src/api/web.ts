// Code generated from internal/shared/api/web.go by make generate; DO NOT EDIT.

import type { Op } from "./api";

/** WebStartRequest starts a run of koko web. */
export type WebStartRequest = Record<string, never>;

/** WebStartResponse is the run's one-time token. */
export interface WebStartResponse {
  /**
   * Token is the one-time token koko web puts in the address it prints,
   * http://localhost:<port>/?t=<token>. The web front trades it for a
   * cookie once, within 5 minutes of its issue on the koko server's
   * clock, on a web@koko channel of the caller's Device Session, or, for
   * the Local caller, of the connection that started the run.
   */
  token: string;
}

/**
 * WebStart issues a one-time token for a run of koko web. A Device
 * Session's token, and the cookie it is traded for, work on a web@koko
 * channel of any of its Device Connections, and the cookie lasts until
 * the Device Session is Revoked. Over the Unix socket the run is the
 * caller's connection: the token and the cookie work only on its
 * web@koko channels, and end with it.
 */
export const WebStart: Op<WebStartRequest, WebStartResponse> = {
  name: "web.start",
  path: "/v1/web.start",
  write: false,
};

/**
 * CodeCookieRequired: a request on a web@koko channel came without a
 * valid cookie of this koko web; open the address koko web prints.
 */
export const CodeCookieRequired = "cookie_required";
