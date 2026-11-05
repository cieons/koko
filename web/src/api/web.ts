// Code generated from internal/shared/api/web.go by make generate; DO NOT EDIT.

import type { Op } from "./api";

/** WebStartRequest starts a run of koko web. */
export type WebStartRequest = Record<string, never>;

/** WebStartResponse is the run's one-time token. */
export interface WebStartResponse {
  /**
   * Token is the one-time token koko web puts in the address it prints,
   * http://localhost:<port>/?t=<token>. The web front trades it for a
   * cookie once, on a web@koko channel of the connection that started the
   * run, within 5 minutes of its issue on the koko server's clock.
   */
  token: string;
}

/**
 * WebStart starts a run of koko web on the caller's connection and
 * issues its one-time token. Over the Unix socket the run is that
 * connection: the token and the cookie it is traded for work only on its
 * web@koko channels, and end with it. It answers only the Local caller
 * until a Device Session can run koko web.
 */
export const WebStart: Op<WebStartRequest, WebStartResponse> = {
  name: "web.start",
  path: "/v1/web.start",
  write: false,
};

/**
 * CodeCookieRequired: a request on a web@koko channel came without a
 * valid cookie of this run of koko web; open the address koko web
 * prints.
 */
export const CodeCookieRequired = "cookie_required";
