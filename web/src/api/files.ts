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
 * FilesUpload starts an upload into a directory on the Host: a hidden
 * partial file beside its name, which FilesFinish renames into place.
 * A name that exists is refused with CodeFileExists unless the request
 * replaces it, and a fifth upload of one SSH Session with
 * CodeTooManyUploads.
 */
export const FilesUpload: AuthEndpoint<FilesUploadRequest, FilesUploadResponse> = {
  path: "/terminal/files/upload",
};

/**
 * FilesFinish renames an upload's partial file onto its name once every
 * byte has come, and answers the same again when sent again.
 */
export const FilesFinish: AuthEndpoint<FilesFinishRequest, FilesFinishResponse> = {
  path: "/terminal/files/finish",
};

/** FilesCancel cancels an upload, removing its partial file. */
export const FilesCancel: AuthEndpoint<FilesCancelRequest, FilesCancelResponse> = {
  path: "/terminal/files/cancel",
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
 * FilesUploadRequest starts an upload of a file of Size bytes, named Name,
 * into the directory Dir on the Host of the web terminal's SSH Session
 * Tunnel.
 */
export interface FilesUploadRequest {
  tunnel: string;
  /** Dir is absolute. */
  dir: string;
  /**
   * Name is the file's name in Dir: not empty, "." or "..", and without
   * "/".
   */
  name: string;
  size: number;
  /** Replace replaces a file of that name, which is otherwise refused. */
  replace?: boolean;
}

/** FilesUploadResponse names the upload started. */
export interface FilesUploadResponse {
  /** Upload is the upload's id, which its chunks, finish and cancel name. */
  upload: string;
}

/** FilesFinishRequest names the upload to finish. */
export interface FilesFinishRequest {
  upload: string;
}

/** FilesFinishResponse is the file an upload made on the Host. */
export interface FilesFinishResponse {
  /** Path is the file's absolute path. */
  path: string;
  size: number;
}

/** FilesCancelRequest names the upload to cancel. */
export interface FilesCancelRequest {
  upload: string;
}

/** FilesCancelResponse answers a cancel. */
export type FilesCancelResponse = Record<string, never>;

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

/**
 * CodeFileExists: an upload's name exists in its directory and the
 * upload does not replace it.
 */
export const CodeFileExists = "file_exists";

/**
 * CodeBadOffset: a chunk would leave a gap in its upload, or an upload
 * was finished before every byte had come.
 */
export const CodeBadOffset = "bad_offset";

/** CodeUnknownUpload: no live upload of this caller has the id. */
export const CodeUnknownUpload = "unknown_upload";

/**
 * CodeTooManyUploads: the SSH Session already runs MaxUploads uploads;
 * the page queues the rest.
 */
export const CodeTooManyUploads = "too_many_uploads";
