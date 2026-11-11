// Code generated from internal/shared/api/server.go by make generate; DO NOT EDIT.

import type { Op } from "./api";
import type { JoinCode } from "./device";

/**
 * ServerInitRequest initialises a fresh koko server: it names the Owner and
 * sets the Server Address.
 */
export interface ServerInitRequest {
  /**
   * Owner is the Owner's User name. It has no default, because a User's
   * name never changes.
   */
  owner: string;
  /**
   * Address is the Server Address, host or host:port (package serveraddr).
   * Without a port, its port is the koko server's listen port.
   */
  address: string;
}

/** ServerInitResponse is what initialisation set and made. */
export interface ServerInitResponse {
  /** Address is the Server Address, as the request gave it. */
  address: string;
  /**
   * HostKeyFingerprint is the SHA-256 fingerprint of the koko server's host
   * key, as OpenSSH prints it: "SHA256:" and 43 characters of base64.
   */
  host_key_fingerprint: string;
  /**
   * JoinCode is the Owner's first Join Code, issued over the Unix socket
   * as device.add issues one there, for the Owner's first device.
   */
  join_code: JoinCode;
}

/**
 * Server is the koko server's deployment, as server.show and server.set
 * return it. It names the Root Key's file and id, never the key (ADR 0018).
 */
export interface Server {
  /**
   * Address is the Server Address, as stored: without a port, its port is
   * ListenPort.
   */
  address: string;
  /** ListenPort is the TCP port the koko server listens on. */
  listen_port: number;
  /**
   * HostKeyFingerprint is the SHA-256 fingerprint of the host key devices
   * pin from their Join Codes, as OpenSSH prints it.
   */
  host_key_fingerprint: string;
  /**
   * HostKey is that host key itself, written as in authorized_keys
   * without a comment ("ssh-ed25519 AAAA..."): the Gateway presents it
   * inside every Tunnel, so koko ssh setup writes it into koko's
   * known_hosts on the koko server's own machine, where nothing joins.
   */
  host_key: string;
  /**
   * Version is the koko server's version, as `koko version` names its
   * own: a release such as v0.3.0, a pseudo-version, or "(devel)".
   */
  version: string;
  /**
   * RootKeyFile is the Root Key's file as the Owner keeps it: under
   * systemd, the file the koko unit hands the service, /etc/koko/root.key.
   */
  root_key_file: string;
  /**
   * RootKeyID is the id the Root Key's file names, so the Owner can tell
   * which copy fits.
   */
  root_key_id: string;
  /** DataDir is the data directory, which holds koko.db. */
  data_dir: string;
  /**
   * WebURL is the Web URL, the https:// origin the Owner's reverse proxy
   * serves the web UI at, which every request on the web listener is
   * checked against; null while it is not set.
   */
  web_url: string | null;
  /**
   * WebListen is the address the web listener listens on, plain HTTP for
   * the reverse proxy alone; null while it is off, as it is unless kokod
   * run is given --web-listen.
   */
  web_listen: string | null;
  /**
   * UpdateCheck is the koko server's check for a new koko release (ADR
   * 0048): its setting and what the checks found.
   */
  update_check: UpdateCheck;
}

/**
 * UpdateCheck is the koko server's check for a new koko release: a random
 * 2 to 5 minutes after it starts, then 6 hours after the last check of any
 * kind, while it is on, and at once when the Owner asks (server.check_update).
 * What it found is kept in memory, so it is null again after a restart.
 */
export interface UpdateCheck {
  /**
   * Enabled is the update_check setting: on, the koko server checks by
   * itself; off, only when the Owner asks.
   */
  enabled: boolean;
  /**
   * CheckedAt is when the last check ended, RFC 3339 in UTC; null before
   * any, and once the setting is turned off, until the Owner checks.
   */
  checked_at: string | null;
  /**
   * Latest is the tag of koko's latest release, as the last check that
   * succeeded found it, such as v0.5.0; null while unknown.
   */
  latest: string | null;
  /**
   * Error is why the last check failed; null when it succeeded or none
   * was made.
   */
  error: string | null;
}

/** ServerShowRequest asks for the koko server's deployment. */
export type ServerShowRequest = Record<string, never>;

/** ServerShowResponse is the koko server's deployment. */
export interface ServerShowResponse {
  server: Server;
}

/** ServerSetRequest changes the deployment settings it names, and only those. */
export interface ServerSetRequest {
  /**
   * Address, when given, is the new Server Address, host or host:port
   * (package serveraddr). Join Codes issued before keep the address they
   * carry.
   */
  address?: string;
  /**
   * WebURL, when given, is the new Web URL, an https:// origin (package
   * weburl), checked on the web listener's next request.
   */
  web_url?: string;
  /**
   * UpdateCheck, when given, turns the koko server's check for a new
   * release on or off.
   */
  update_check?: boolean;
  /**
   * Unset clears these settings: only "web_url", which turns the web
   * listener's answers into a page naming koko server set web_url=.
   */
  unset?: string[];
}

/** ServerSetResponse is the koko server's deployment as server.set left it. */
export interface ServerSetResponse {
  server: Server;
}

/**
 * ServerCheckUpdateRequest asks the koko server to check for a new koko
 * release at once.
 */
export type ServerCheckUpdateRequest = Record<string, never>;

/**
 * ServerCheckUpdateResponse is what the check found, and the koko server's
 * version to set it against.
 */
export interface ServerCheckUpdateResponse {
  update_check: UpdateCheck;
  /** Version is the koko server's version, as server.show names it. */
  version: string;
}

/**
 * ServerInit initialises a fresh koko server: it makes the koko key and
 * the host key, sealed under the Root Key, creates the Owner with their
 * Personal Space, stores the Server Address, and issues the Owner's first
 * Join Code. It answers only the Local caller, and is the one operation
 * that answers before initialisation; once the koko server is
 * initialised it is refused with CodeAlreadyInitialised.
 */
export const ServerInit: Op<ServerInitRequest, ServerInitResponse> = {
  name: "server.init",
  path: "/v1/server.init",
  write: true,
};

/**
 * ServerShow returns the koko server's deployment: its Server Address,
 * listen port, host key, version, Root Key's file and id, and data
 * directory.
 */
export const ServerShow: Op<ServerShowRequest, ServerShowResponse> = {
  name: "server.show",
  path: "/v1/server.show",
  write: false,
};

/**
 * ServerSet changes the deployment settings the request names: the
 * Server Address, for the Join Codes issued afterwards, and the Web URL,
 * for the web listener's next request. It answers only
 * the Local caller, over the Unix socket, and never a browser tab of
 * koko web there, so no device and no browser tab can move where
 * devices connect. A Server Address a Join Code cannot carry is refused
 * with CodeInvalidAddress, and a Web URL that is not an https:// origin
 * with CodeInvalidWebURL.
 */
export const ServerSet: Op<ServerSetRequest, ServerSetResponse> = {
  name: "server.set",
  path: "/v1/server.set",
  write: true,
};

/**
 * ServerCheckUpdate checks for a new koko release at once, with the
 * update_check setting on or off, sharing a check already in flight. It
 * is for the Owner only, the Local caller included, and changes no
 * record. A check that fails is refused with CodeUpdateCheckFailed, and
 * server.show's update_check names it too.
 */
export const ServerCheckUpdate: Op<ServerCheckUpdateRequest, ServerCheckUpdateResponse> = {
  name: "server.check_update",
  path: "/v1/server.check_update",
  write: false,
};

/**
 * CodeAlreadyInitialised: the koko server is initialised already, so its
 * Owner and its keys stay as they are.
 */
export const CodeAlreadyInitialised = "already_initialised";

/** CodeInvalidAddress: a Server Address is not host or host:port. */
export const CodeInvalidAddress = "invalid_address";

/**
 * CodeInvalidWebURL: a Web URL is not an https:// origin,
 * https://<host>[:<port>] with no path.
 */
export const CodeInvalidWebURL = "invalid_web_url";

/**
 * CodeUpdateCheckFailed: the koko server could not find koko's latest
 * release; the message names what failed.
 */
export const CodeUpdateCheckFailed = "update_check_failed";
