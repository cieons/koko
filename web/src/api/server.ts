// Code generated from internal/shared/api/server.go by make generate; DO NOT EDIT.

import type { Op } from "./api";

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
}

/**
 * ServerInit initialises a fresh koko server: it makes the koko key and
 * the host key, sealed under the Root Key, creates the Owner with their
 * Personal Space, and stores the Server Address. It answers only the
 * Local caller, and is the one operation that answers before
 * initialisation; once the koko server is initialised it is refused with
 * CodeAlreadyInitialised.
 */
export const ServerInit: Op<ServerInitRequest, ServerInitResponse> = {
  name: "server.init",
  path: "/v1/server.init",
  write: true,
};
