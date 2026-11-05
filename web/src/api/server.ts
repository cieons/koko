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
}

/** ServerSetResponse is the koko server's deployment as server.set left it. */
export interface ServerSetResponse {
  server: Server;
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
 * Server Address, for the Join Codes issued afterwards. It answers only
 * the Local caller, over the Unix socket, and never a browser tab of
 * koko web there, so no device and no browser tab can move where
 * devices connect. A Server Address a Join Code cannot carry is refused
 * with CodeInvalidAddress.
 */
export const ServerSet: Op<ServerSetRequest, ServerSetResponse> = {
  name: "server.set",
  path: "/v1/server.set",
  write: true,
};

/**
 * CodeAlreadyInitialised: the koko server is initialised already, so its
 * Owner and its keys stay as they are.
 */
export const CodeAlreadyInitialised = "already_initialised";

/** CodeInvalidAddress: a Server Address is not host or host:port. */
export const CodeInvalidAddress = "invalid_address";
