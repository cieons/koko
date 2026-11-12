import { Button, ProgressBar } from "@heroui/react";
import { ArrowUp, File as FileIcon, Folder, FolderSymlink, RefreshCw, Upload as UploadIcon, X } from "lucide-react";
import { type DragEvent, type ReactNode, useCallback, useEffect, useId, useRef, useState } from "react";

import { CodeHostRefused, CodeNotAFile, CodeSFTPUnavailable, CodeSSHSessionEnded, CodeUnknownTunnel, type FilesListResponse } from "../api/files";
import { ApiError } from "../client";
import { errorMessage, type Translate, useCount, useLanguage, useT } from "../shell/i18n";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { SidePanel } from "../ui/SidePanel";
import { AlertLine, Loading } from "../ui/states";
import {
  cancelUpload,
  dismissUpload,
  download,
  type Kind,
  list,
  onFinished,
  startUpload,
  type Upload,
  useUploads,
} from "./files";
import type { Tab } from "./workspace";

// The Files panel (docs/web-design.md, "The terminal"): a directory of the
// active tab's Host over its SSH Session, opening where its shell last
// was, or else at the Host Account's home; and the uploads of every tab.

/** Dropped is files dropped on a tab's terminal, to upload into the directory its shell is in. */
export interface Dropped {
  /** tab is the key of the tab they were dropped on. */
  tab: number;
  files: readonly File[];
}

/** sessionWide are the refusals that hold for every directory of the SSH Session, so home is not tried after them. */
const sessionWide = new Set<string>([CodeSFTPUnavailable, CodeSSHSessionEnded, CodeUnknownTunnel]);

/** formatSize is a size in bytes in binary units, as the browser's language writes numbers. */
export function formatSize(bytes: number, language: string): string {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  const digits = unit === 0 || value >= 100 ? 0 : 1;
  return `${new Intl.NumberFormat(language, { maximumFractionDigits: digits }).format(value)} ${units[unit]}`;
}

/** parentOf is the directory above an absolute path: "/" above "/" too. */
function parentOf(path: string): string {
  const up = path.replace(/\/+[^/]*\/*$/, "");
  return up === "" ? "/" : up;
}

/** joinPath names an entry of a directory. */
function joinPath(dir: string, name: string): string {
  return dir.endsWith("/") ? dir + name : `${dir}/${name}`;
}

/** Said is a refusal in the page's words, and beneath them the Host's own when it refused. */
function Said({ error }: { error: unknown }) {
  const t = useT();
  const detail = error instanceof ApiError && error.code === CodeHostRefused && error.message !== "" ? error.message : undefined;
  return (
    <span className="min-w-0">
      {errorMessage(t, error)}
      {detail !== undefined && <span className="t-code mt-1 block text-xs break-words">{detail}</span>}
    </span>
  );
}

/** outcome says where an upload is, in words: a failed one in the Host's own too, when it refused. */
function outcome(t: Translate, language: string, upload: Upload): ReactNode {
  switch (upload.state) {
    case "waiting":
      return t("terminal.files.upload.waiting");
    case "done":
      return t("terminal.files.upload.done");
    case "cancelled":
      return t("terminal.files.upload.cancelled");
    case "failed":
      return <Said error={upload.error} />;
    case "running":
      return upload.retrying
        ? t("terminal.files.upload.retrying")
        : t("terminal.files.upload.progress", {
            sent: formatSize(upload.sent, language),
            size: formatSize(upload.size, language),
          });
  }
}

/** UploadRow is one upload: its name, its Host and directory, its progress and Cancel, or its outcome until dismissed. */
function UploadRow({ upload }: { upload: Upload }) {
  const t = useT();
  const language = useLanguage();
  const live = upload.state === "waiting" || upload.state === "running";
  return (
    <li className="flex items-start gap-3 py-3">
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <p className="truncate text-sm font-medium">{upload.name}</p>
        <p className="t-label truncate">{t("terminal.files.upload.to", { host: upload.host, dir: upload.dir })}</p>
        {upload.state === "running" && (
          <ProgressBar aria-label={upload.name} value={upload.sent} maxValue={Math.max(upload.size, 1)} size="sm">
            <ProgressBar.Track>
              <ProgressBar.Fill />
            </ProgressBar.Track>
          </ProgressBar>
        )}
        <p className={`t-label t-num ${upload.state === "failed" ? "text-danger" : ""}`}>{outcome(t, language, upload)}</p>
      </div>
      <Button
        variant="ghost"
        isIconOnly
        size="sm"
        className="shrink-0 rounded-full"
        aria-label={t(live ? "terminal.files.upload.cancel" : "terminal.files.upload.dismiss", { name: upload.name })}
        onPress={() => (live ? cancelUpload(upload.id) : dismissUpload(upload.id))}
      >
        <X aria-hidden className="size-4" />
      </Button>
    </li>
  );
}

/** Uploads lists the uploads of every tab, while there are any. */
function Uploads() {
  const t = useT();
  const heading = useId();
  const uploads = useUploads();
  if (uploads.length === 0) {
    return null;
  }
  return (
    <section aria-labelledby={heading} className="flex flex-col border-t border-line pt-4">
      <h3 id={heading} className="t-heading">
        {t("terminal.files.uploads")}
      </h3>
      <ul className="divide-y divide-line">
        {uploads.map((upload) => (
          <UploadRow key={upload.id} upload={upload} />
        ))}
      </ul>
    </section>
  );
}

const icons = { dir: Folder, link: FolderSymlink, file: FileIcon, other: FileIcon } as const;

/** Asked is a file whose name exists in the directory it goes into, waiting for the answer to whether it replaces it. */
interface Asked {
  file: File;
  dir: string;
}

/** DirectoryProps are the tab whose Host is listed, its SSH Session, and files dropped on its terminal. */
interface DirectoryProps {
  tab: Tab;
  tunnel: string;
  dropped: Dropped | undefined;
  onTaken: () => void;
}

/**
 * Directory lists one directory of the tab's Host at a time, from where its
 * shell is, and takes uploads into it: a file whose name is listed there is
 * asked about first.
 */
function Directory({ tab, tunnel, dropped, onTaken }: DirectoryProps) {
  const t = useT();
  const count = useCount();
  const language = useLanguage();
  const [path, setPath] = useState(tab.cwd ?? "");
  const [listing, setListing] = useState<FilesListResponse>();
  const [error, setError] = useState<unknown>();
  const [asking, setAsking] = useState<readonly Asked[]>([]);
  const picker = useRef<HTMLInputElement>(null);
  const sequence = useRef(0);
  const shownPath = useRef(path);
  shownPath.current = listing?.path ?? path;

  /**
   * load lists the directory at a path, in place of what is shown, or
   * under it while refreshing; with fallback, a path that fails is left
   * for home. It is the listing, once it is the last asked for.
   */
  const load = useCallback(
    async (at: string, { fallback = false, refresh = false } = {}): Promise<FilesListResponse | undefined> => {
      const mine = ++sequence.current;
      setError(undefined);
      if (!refresh) {
        setPath(at);
        setListing(undefined);
      }
      try {
        let answer: FilesListResponse;
        try {
          answer = await list(tunnel, at);
        } catch (failed) {
          if (!fallback || at === "" || (failed instanceof ApiError && sessionWide.has(failed.code))) {
            throw failed;
          }
          answer = await list(tunnel, "");
        }
        if (mine !== sequence.current) {
          return undefined;
        }
        setPath(answer.path);
        setListing(answer);
        return answer;
      } catch (failed) {
        if (mine === sequence.current) {
          setError(failed);
        }
        return undefined;
      }
    },
    [tunnel],
  );

  /** queue uploads files into a listed directory, asking first about each whose name it holds. */
  const queue = useCallback(
    (files: Iterable<File>, into: FilesListResponse) => {
      const names = new Set(into.entries.map((entry) => entry.name));
      const ask: Asked[] = [];
      for (const file of files) {
        if (names.has(file.name)) {
          ask.push({ file, dir: into.path });
        } else {
          startUpload({ tunnel, host: tab.host, dir: into.path }, file, false);
        }
      }
      if (ask.length > 0) {
        setAsking((before) => [...before, ...ask]);
      }
    },
    [tunnel, tab.host],
  );

  // The panel opens where the shell is, and files dropped on the terminal
  // go there: the panel lists it first, so a name it holds is asked about.
  const opened = useRef(false);
  const taken = useRef<Dropped>(undefined);
  useEffect(() => {
    if (dropped !== undefined && taken.current !== dropped) {
      taken.current = dropped;
      opened.current = true;
      onTaken();
      void load(tab.cwd ?? "", { fallback: true }).then((answer) => answer && queue(dropped.files, answer));
    } else if (!opened.current) {
      opened.current = true;
      void load(tab.cwd ?? "", { fallback: true });
    }
  }, [dropped, onTaken, load, queue, tab.cwd]);

  // An upload finished into the directory shown shows in it.
  useEffect(
    () =>
      onFinished((upload) => {
        if (upload.tunnel === tunnel && upload.dir === shownPath.current) {
          void load(shownPath.current, { refresh: true });
        }
      }),
    [tunnel, load],
  );

  if (error instanceof ApiError && error.code === CodeSFTPUnavailable) {
    return (
      <div role="alert" className="flex flex-col gap-1">
        <p className="t-name">{t("terminal.files.unavailable", { host: tab.host })}</p>
        <p className="text-muted">{t("terminal.files.unavailableLead")}</p>
      </div>
    );
  }

  const open = (name: string, kind: Kind) => {
    const full = joinPath(shownPath.current, name);
    if (kind === "dir") {
      void load(full);
    } else if (kind === "file") {
      download(tunnel, full).catch(setError);
    } else if (kind === "link") {
      // A link to a file downloads it, and one to a directory is entered.
      download(tunnel, full).catch((failed: unknown) => {
        if (failed instanceof ApiError && failed.code === CodeNotAFile) {
          void load(full);
        } else {
          setError(failed);
        }
      });
    }
  };
  const [question] = asking;
  const answered = () => setAsking((before) => before.slice(1));
  const collator = new Intl.Collator(language, { numeric: true });
  const entries = [...(listing?.entries ?? [])].sort(
    (a, b) => Number(b.kind === "dir") - Number(a.kind === "dir") || collator.compare(a.name, b.name),
  );
  const date = new Intl.DateTimeFormat(language, { dateStyle: "medium", timeStyle: "short" });
  const current = listing?.path ?? path;

  return (
    <>
      <div className="flex flex-col gap-2">
        <p title={current} className="t-code rounded-xl bg-default px-3 py-2 text-[0.8125rem] break-all">
          <span className="sr-only">{t("terminal.cwd")} </span>
          {current === "" ? "~" : current}
        </p>
        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            isIconOnly
            size="sm"
            className="rounded-full"
            aria-label={t("terminal.files.up")}
            isDisabled={listing === undefined || current === "/"}
            onPress={() => void load(parentOf(current))}
          >
            <ArrowUp aria-hidden className="size-4" />
          </Button>
          <Button
            variant="ghost"
            isIconOnly
            size="sm"
            className="rounded-full"
            aria-label={t("terminal.files.refresh")}
            onPress={() => void load(current, { refresh: listing !== undefined })}
          >
            <RefreshCw aria-hidden className="size-4" />
          </Button>
          <span className="flex-1" />
          <Button variant="primary" size="sm" isDisabled={listing === undefined} onPress={() => picker.current?.click()}>
            <UploadIcon aria-hidden className="size-4" />
            {t("terminal.files.upload")}
          </Button>
          <input
            ref={picker}
            type="file"
            multiple
            hidden
            onChange={(event) => {
              if (listing !== undefined && event.target.files) {
                queue(event.target.files, listing);
              }
              event.target.value = "";
            }}
          />
        </div>
      </div>

      {error !== undefined && (
        <div className="flex flex-col items-start gap-3">
          <AlertLine>
            <Said error={error} />
          </AlertLine>
          {listing === undefined && (
            <Button variant="secondary" size="sm" onPress={() => void load(current, { fallback: current === (tab.cwd ?? "") })}>
              {t("shell.retry")}
            </Button>
          )}
        </div>
      )}

      {listing === undefined
        ? error === undefined && <Loading />
        : (
          <div className="-mx-2">
            {entries.length === 0 && <p className="px-2 text-muted">{t("terminal.files.empty")}</p>}
            <ul aria-label={t("terminal.files.entries", { dir: listing.path })}>
              {entries.map((entry) => {
                const kind = entry.kind as Kind;
                const Icon = icons[kind] ?? FileIcon;
                const enters = kind === "dir" || kind === "link";
                const what = kind === "file" ? formatSize(entry.size, language) : t(`terminal.files.kind.${kind === "dir" || kind === "link" ? kind : "other"}`);
                return (
                  <li key={entry.name}>
                    <button
                      type="button"
                      disabled={kind === "other"}
                      aria-label={t(enters ? "terminal.files.open" : "terminal.files.download", { name: entry.name })}
                      onClick={() => open(entry.name, kind)}
                      className="row-link flex w-full items-center gap-3 px-2 py-2 text-start disabled:opacity-60"
                    >
                      <Icon aria-hidden className={`size-[1.125rem] shrink-0 ${enters ? "text-link" : "text-muted"}`} />
                      <span className="flex min-w-0 flex-1 flex-col">
                        <span className="truncate">{entry.name}</span>
                        <span className="t-label t-num truncate">
                          {what} · {date.format(new Date(entry.modified))}
                        </span>
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
            {listing.more > 0 && <p className="t-label px-2 pt-2">{count("terminal.files.more", listing.more)}</p>}
          </div>
        )}

      {question && (
        <ConfirmDialog
          title={t("terminal.files.replace.title", { name: question.file.name, dir: question.dir })}
          consequence={t("terminal.files.replace.consequence", { host: tab.host })}
          confirm={t("terminal.files.replace.confirm")}
          onConfirm={() => {
            startUpload({ tunnel, host: tab.host, dir: question.dir }, question.file, true);
            answered();
          }}
          onCancel={answered}
        />
      )}
    </>
  );
}

/** FilesBodyProps are the tab whose Host the panel lists, and files dropped on its terminal, which it takes once. */
export interface FilesBodyProps {
  tab: Tab;
  dropped?: Dropped;
  onTaken: () => void;
}

/**
 * FilesBody is the panel's content: the directory, over the tab's SSH
 * Session while it has one, or why not; then the uploads of every tab.
 */
function FilesBody({ tab, dropped, onTaken }: FilesBodyProps) {
  const t = useT();
  const { status } = tab.state;
  let directory;
  if (status === "ended") {
    directory = <p className="text-muted">{t("terminal.files.ended")}</p>;
  } else if (tab.session !== undefined) {
    directory = <Directory key={tab.session} tab={tab} tunnel={tab.session} dropped={dropped} onTaken={onTaken} />;
  } else if (status === "connecting") {
    directory = <Loading />;
  } else {
    directory = <p className="text-muted">{t("terminal.files.noSession")}</p>;
  }
  return (
    <>
      {directory}
      <Uploads />
    </>
  );
}

/** FilesPanelProps are the tab whose Host the panel lists, files dropped on its terminal, and its close. */
export interface FilesPanelProps extends FilesBodyProps {
  onClose: () => void;
}

/**
 * FilesColumn is the Files panel on a desktop, docked in the terminal's
 * island at its end edge: the terminal narrows for it, so the listing and
 * the shell are seen together.
 */
export function FilesColumn({ tab, dropped, onTaken, onClose }: FilesPanelProps) {
  const t = useT();
  return (
    <aside
      aria-label={t("terminal.files")}
      className="flex w-[clamp(20rem,28vw,26rem)] shrink-0 flex-col border-s border-line bg-surface"
    >
      <div className="flex h-14 shrink-0 items-center gap-2 border-b border-line ps-5 pe-3">
        <h2 className="t-title min-w-0 flex-1 truncate">
          {t("terminal.files")} <span className="text-muted">· {t("terminal.files.on", { host: tab.label })}</span>
        </h2>
        <Button variant="ghost" isIconOnly size="sm" className="rounded-full" aria-label={t("terminal.files.close")} onPress={onClose}>
          <X aria-hidden className="size-4" />
        </Button>
      </div>
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
        <FilesBody key={tab.key} tab={tab} dropped={dropped} onTaken={onTaken} />
      </div>
    </aside>
  );
}

/** FilesSheet is the Files panel on a phone: a side panel from the bottom. */
export function FilesSheet({ tab, dropped, onTaken, onClose }: FilesPanelProps) {
  const t = useT();
  return (
    <SidePanel title={t("terminal.files.title", { host: tab.label })} onClose={onClose}>
      <FilesBody key={tab.key} tab={tab} dropped={dropped} onTaken={onTaken} />
    </SidePanel>
  );
}

/**
 * useFileDrop lets files dropped on the terminal be uploaded into the
 * directory its shell is in, while on: dragging them over shows where they
 * go, and dropping hands them to onDrop.
 */
export function useFileDrop(on: boolean, onDrop: (files: File[]) => void) {
  const [over, setOver] = useState(false);
  const depth = useRef(0);
  const carriesFiles = (event: DragEvent) => event.dataTransfer.types.includes("Files");
  if (!on) {
    return { over: false, handlers: {} };
  }
  return {
    over,
    handlers: {
      onDragEnter: (event: DragEvent) => {
        if (carriesFiles(event)) {
          event.preventDefault();
          depth.current++;
          setOver(true);
        }
      },
      onDragOver: (event: DragEvent) => {
        if (carriesFiles(event)) {
          event.preventDefault();
          event.dataTransfer.dropEffect = "copy";
        }
      },
      onDragLeave: () => {
        depth.current = Math.max(0, depth.current - 1);
        if (depth.current === 0) {
          setOver(false);
        }
      },
      onDrop: (event: DragEvent) => {
        if (carriesFiles(event)) {
          event.preventDefault();
          depth.current = 0;
          setOver(false);
          onDrop([...event.dataTransfer.files]);
        }
      },
    },
  };
}

/** DropCover says, over the terminal, where files dropped on it go: a dashed accent edge round it. */
export function DropCover({ tab }: { tab: Tab }) {
  const t = useT();
  return (
    <div className="pointer-events-none absolute inset-3 z-20 flex items-center justify-center rounded-2xl border-2 border-dashed border-accent bg-[var(--term-surface)]/85">
      <p className="t-title max-w-md px-6 text-center">
        {tab.cwd === undefined
          ? t("terminal.drop.home", { host: tab.host })
          : t("terminal.drop", { host: tab.host, dir: tab.cwd })}
      </p>
    </div>
  );
}
