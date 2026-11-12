import { useSyncExternalStore } from "react";

import { FilesCancel, FilesDownload, FilesFinish, FilesList, type FilesListResponse, FilesUpload } from "../api/files";
import { ApiError, post, postBytes } from "../client";
import { timing } from "./connection";

// The Files panel's calls, the web front's own endpoints under
// /terminal/files/ (P1 spec: a terminal in the web UI, "Files over SFTP"),
// and the uploads of every tab, which live for the document's life beside
// the workspace's tabs: closing the panel or showing another tab stops
// none of them.

/** chunkSize is the size of an upload's chunk: the most a chunk takes (api.MaxFilesChunk). */
export const chunkSize = 512 << 10;

/** maxRunning is how many uploads one SSH Session runs at once (api.MaxUploads); the page queues the rest. */
export const maxRunning = 4;

/**
 * chunkPath takes an upload's chunks (api.FilesChunkPath). A chunk is no
 * JSON, so its endpoint is not among the generated ones.
 */
const chunkPath = "/terminal/files/chunk";

/** Kind is what an entry of a directory is: a link is not followed. */
export type Kind = "file" | "dir" | "link" | "other";

/** list lists a directory of an SSH Session's Host; an empty path is the Host Account's home. */
export function list(tunnel: string, path: string): Promise<FilesListResponse> {
  return post(FilesList, { tunnel, path });
}

/** downloadPath is where a download's token is spent: its GET answers the file's bytes. */
export function downloadPath(token: string): string {
  return `${FilesDownload.path}/${token}`;
}

/**
 * download has the browser save a file of an SSH Session's Host, with its
 * own progress and Cancel: it asks for a token, then follows a link to the
 * token's GET, which answers the file as an attachment, so the page stays
 * where it is and the file never sits in its memory.
 */
export async function download(tunnel: string, path: string): Promise<void> {
  const { token } = await post(FilesDownload, { tunnel, path });
  const link = document.createElement("a");
  link.href = downloadPath(token);
  link.download = "";
  link.hidden = true;
  document.body.append(link);
  link.click();
  link.remove();
}

/** UploadState is where an upload is: queued, sending, or over. */
export type UploadState = "waiting" | "running" | "done" | "cancelled" | "failed";

/** Upload is one file being uploaded into a directory of an SSH Session's Host, as the panel shows it. */
export interface Upload {
  /** id names the upload for the document's life. */
  id: number;
  /** tunnel is the SSH Session's id. */
  tunnel: string;
  host: string;
  dir: string;
  name: string;
  size: number;
  /** sent counts the bytes the koko server has received. */
  sent: number;
  state: UploadState;
  /** retrying says a call failed with its connection and is being sent again. */
  retrying: boolean;
  /** error is why a failed upload failed. */
  error?: unknown;
}

/** Target is the directory of an SSH Session's Host an upload goes into. */
export interface Target {
  tunnel: string;
  host: string;
  dir: string;
}

/** Job is what the page keeps of an upload to send it: the file, and the koko server's id for it once started. */
interface Job {
  file: File;
  replace: boolean;
  server?: string;
  cancelled: boolean;
  /** cancelling is the cancel sent to the koko server, which the upload's run waits for before its place is taken. */
  cancelling?: Promise<unknown>;
  /** wake ends the wait before a call is sent again, as a cancel does. */
  wake?: () => void;
}

let uploads: readonly Upload[] = [];
const jobs = new Map<number, Job>();
const listeners = new Set<() => void>();
const finishedListeners = new Set<(upload: Upload) => void>();
let nextId = 1;

function publish(next: readonly Upload[]): void {
  uploads = next;
  for (const listener of listeners) {
    listener();
  }
}

function patch(id: number, change: Partial<Upload>): void {
  publish(uploads.map((upload) => (upload.id === id ? { ...upload, ...change } : upload)));
}

/** useUploads is the uploads of every tab, in the order they were started, followed as they change. */
export function useUploads(): readonly Upload[] {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    () => uploads,
  );
}

/** onFinished has listener hear each upload that has finished, until the returned function is called. */
export function onFinished(listener: (upload: Upload) => void): () => void {
  finishedListeners.add(listener);
  return () => finishedListeners.delete(listener);
}

/**
 * startUpload queues a file for a directory of an SSH Session's Host, as a
 * file of that name there is replaced only with replace, which the page
 * asks about first.
 */
export function startUpload(target: Target, file: File, replace: boolean): void {
  const id = nextId++;
  jobs.set(id, { file, replace, cancelled: false });
  publish([...uploads, { id, ...target, name: file.name, size: file.size, sent: 0, state: "waiting", retrying: false }]);
  pump();
}

/**
 * runs holds the SSH Session of each upload whose run is under way: a
 * cancelled one keeps its place until the koko server has its cancel.
 */
const runs = new Map<number, string>();

/** pump starts the waiting uploads, in order, while their SSH Session runs fewer than maxRunning. */
function pump(): void {
  for (const next of uploads) {
    const job = jobs.get(next.id);
    const running = [...runs.values()].filter((tunnel) => tunnel === next.tunnel).length;
    if (next.state === "waiting" && job && running < maxRunning) {
      runs.set(next.id, next.tunnel);
      patch(next.id, { state: "running" });
      void run(next, job).finally(() => {
        runs.delete(next.id);
        pump();
      });
    }
  }
}

/** Cancelled ends an upload's run once it has been cancelled. */
class Cancelled extends Error {}

/**
 * run sends one upload: it starts it, sends its chunks in order, each again
 * from where the koko server says it has come to, and finishes it.
 */
async function run(upload: Upload, job: Job): Promise<void> {
  const { id } = upload;
  const { file } = job;
  try {
    const started = await post(FilesUpload, {
      tunnel: upload.tunnel,
      dir: upload.dir,
      name: upload.name,
      size: file.size,
      replace: job.replace,
    });
    job.server = started.upload;
    if (job.cancelled) {
      throw new Cancelled();
    }
    for (let offset = 0; offset < file.size; ) {
      const at = offset;
      const { received } = await sent(id, job, () =>
        postBytes<{ received: number }>(`${chunkPath}?upload=${started.upload}&offset=${at}`, file.slice(at, at + chunkSize)),
      );
      offset = received;
      patch(id, { sent: received });
    }
    await sent(id, job, () => post(FilesFinish, { upload: started.upload }));
    patch(id, { state: "done", sent: file.size });
    const done = uploads.find((each) => each.id === id);
    for (const listener of finishedListeners) {
      if (done) {
        listener(done);
      }
    }
  } catch (error) {
    if (!job.cancelled) {
      patch(id, { state: "failed", retrying: false, error });
    } else if (job.server !== undefined) {
      // A cancel takes the partial file away, also one started as it came,
      // before the next upload of its SSH Session takes the place.
      job.cancelling ??= cancelOnServer(job.server);
      await job.cancelling;
    }
  }
}

/**
 * sent sends a chunk or a finish, which the koko server answers the same
 * when it comes twice: one whose connection failed is sent again with a
 * Resume's backoff, for up to the grace a Detached SSH Session is held. A
 * refusal ends the upload, and so does a cancel.
 */
async function sent<T>(id: number, job: Job, send: () => Promise<T>): Promise<T> {
  const since = Date.now();
  for (let wait: number = timing.reconnectFirst; ; wait = Math.min(2 * wait, timing.reconnectMax)) {
    try {
      const answer = await send();
      if (job.cancelled) {
        throw new Cancelled();
      }
      if (uploads.find((each) => each.id === id)?.retrying) {
        patch(id, { retrying: false });
      }
      return answer;
    } catch (error) {
      if (job.cancelled) {
        throw new Cancelled();
      }
      if (error instanceof ApiError || Date.now() - since >= timing.grace) {
        throw error;
      }
    }
    patch(id, { retrying: true });
    await new Promise<void>((resolve) => {
      const timer = setTimeout(resolve, wait);
      job.wake = () => {
        clearTimeout(timer);
        resolve();
      };
    });
    job.wake = undefined;
    if (job.cancelled) {
      throw new Cancelled();
    }
  }
}

function cancelOnServer(server: string): Promise<unknown> {
  return post(FilesCancel, { upload: server }).catch(() => undefined);
}

/** cancelUpload stops an upload that is waiting or running; the koko server removes its partial file. */
export function cancelUpload(id: number): void {
  const job = jobs.get(id);
  const upload = uploads.find((each) => each.id === id);
  if (!job || !upload || (upload.state !== "waiting" && upload.state !== "running")) {
    return;
  }
  job.cancelled = true;
  job.wake?.();
  patch(id, { state: "cancelled", retrying: false });
  if (job.server !== undefined) {
    job.cancelling = cancelOnServer(job.server);
  }
}

/** dismissUpload takes away the row of an upload that is over. */
export function dismissUpload(id: number): void {
  jobs.delete(id);
  publish(uploads.filter((upload) => upload.id !== id));
}

/**
 * resetUploads stops and forgets every upload, as for a new document: the
 * workspace going with the shell's frame, as on signing out, whose SSH
 * Sessions end and take the partial files with them.
 */
export function resetUploads(): void {
  for (const job of jobs.values()) {
    job.cancelled = true;
    job.wake?.();
  }
  jobs.clear();
  runs.clear();
  publish([]);
}
