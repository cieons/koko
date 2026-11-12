// Code generated from internal/shared/api/files.go by make generate; DO NOT EDIT.

import type { AuthEndpoint } from "./signin";

/**
 * FilesList lists the directory a path names on the Host, following
 * links, or the Host Account's home for an empty path.
 */
export const FilesList: AuthEndpoint<FilesListRequest, FilesListResponse> = {
  path: "/terminal/files/list",
};

/**
 * FilesListRequest names a directory on the Host of the web terminal's SSH
 * Session Tunnel, its id.
 */
export interface FilesListRequest {
  tunnel: string;
  /** Path is absolute, or empty for the Host Account's home. */
  path: string;
}

/** FilesListResponse is a directory on the Host. */
export interface FilesListResponse {
  /**
   * Path is the directory's path as the Host resolved it, links, "."
   * and ".." followed.
   */
  path: string;
  /**
   * Entries are at most MaxListed of its entries, directories first,
   * then by name, byte for byte; "." and ".." are not among them.
   */
  entries: FileEntry[];
  /** More counts the entries past MaxListed, which are not listed. */
  more: number;
}

/** FileEntry is one entry of a directory on the Host. */
export interface FileEntry {
  name: string;
  /**
   * Kind is "file", "dir", "link" (a symbolic link, not followed) or
   * "other", such as a FIFO or a socket.
   */
  kind: string;
  /** Size is its size in bytes, as the Host gives it. */
  size: number;
  /** Modified is when it last changed, RFC 3339 in UTC. */
  modified: string;
}

/**
 * CodeUnknownTunnel: a files call named no live web terminal SSH
 * Session of this caller.
 */
export const CodeUnknownTunnel = "unknown_tunnel";

/**
 * CodeSSHSessionEnded: the web terminal's SSH Session ended, which
 * ends every transfer of it.
 */
export const CodeSSHSessionEnded = "ssh_session_ended";

/**
 * CodeSFTPUnavailable: the Host offers no SFTP, which koko moves files
 * through.
 */
export const CodeSFTPUnavailable = "sftp_unavailable";

/**
 * CodeHostRefused: the Host refused a listing, a read or a write; the
 * message is the Host's.
 */
export const CodeHostRefused = "host_refused";
