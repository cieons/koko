import { Button } from "@heroui/react";
import { ChevronRight, Folder, FolderOpen, X } from "lucide-react";
import { type ReactNode, useEffect, useState } from "react";
import { Link } from "react-aria-components";

import type { Host } from "../api/host";
import { useT } from "../shell/i18n";
import { navigate } from "../shell/router";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { writeClipboard } from "../ui/copy";
import { notify } from "../ui/notify";
import { useDocumentTitle } from "../ui/Page";
import { type RowAction, RowMenu } from "../ui/RowMenu";
import { Terminals } from "./connection";
import { fontSizes } from "./emulator/Emulator";
import { useManagedHosts } from "./hosts";
import { chord, openSwitcher } from "./keys";
import { Pane } from "./Pane";
import { Kbd, spaceOf } from "./RailSection";
import { Switcher } from "./Switcher";
import { TabMark } from "./TabMark";
import {
  addTab,
  runtime,
  setAnnounce,
  setFontSize,
  shellLives,
  type Tab,
  terminalPath,
  useWorkspace,
  workspaceQuery,
} from "./workspace";

// The terminal workspace (docs/web-design.md, "The terminal"): a chunk of
// its own, loaded with the first terminal, which the shell's frame keeps
// mounted for the document's life, so its SSH Sessions and their screens
// stay while the person visits other pages. Its state is workspace.ts's.

/** Title names the browser's tab after the active tab's Host: "web1 · Terminal · koko". */
function Title({ tab }: { tab: Tab | undefined }) {
  const t = useT();
  useDocumentTitle(tab ? t("terminal.documentTitle", { host: tab.host }) : t("terminal.title"));
  return null;
}

/** useMore is More's actions on the active tab; End SSH Session asks first, through onEnd. */
function useMore(tab: Tab | undefined, onEnd: (tab: Tab) => void): RowAction[] {
  const t = useT();
  const { fontSize, announce } = useWorkspace();
  const size = fontSizes.indexOf(fontSize);
  const copy = () => {
    const text = tab ? (runtime(tab.key).emulator?.selection() ?? "") : "";
    if (text === "") {
      notify(t("terminal.more.nothingSelected"));
      return;
    }
    writeClipboard(text).then(
      () => notify(t("terminal.more.copied")),
      () => notify(t("terminal.clipboard.failed")),
    );
  };
  return [
    {
      id: "larger",
      label: t("terminal.more.larger"),
      isDisabled: size >= fontSizes.length - 1,
      onAction: () => setFontSize(fontSizes[size + 1] ?? fontSize),
    },
    {
      id: "smaller",
      label: t("terminal.more.smaller"),
      isDisabled: size <= 0,
      onAction: () => setFontSize(fontSizes[size - 1] ?? fontSize),
    },
    { id: "copy", label: t("terminal.more.copy"), isDisabled: !tab, onAction: copy },
    {
      id: "announce",
      label: t(announce ? "terminal.more.announceOff" : "terminal.more.announce"),
      onAction: () => setAnnounce(!announce),
    },
    {
      id: "end",
      label: t("terminal.more.end"),
      danger: true,
      isDisabled: !tab || !shellLives(tab),
      onAction: () => tab && onEnd(tab),
    },
  ];
}

/** Head is the island's head: the active tab's Space and name, its state, its directory, Files and More. */
function Head(props: { tab: Tab | undefined; space: string | undefined; files: boolean; onFiles: () => void; more: RowAction[] }) {
  const t = useT();
  const { tab, space, files } = props;
  return (
    <div className="flex h-14 shrink-0 items-center gap-4 border-b border-line bg-surface ps-5 pe-3">
      {tab ? (
        <div className="flex min-w-0 flex-1 items-center gap-4">
          <nav aria-label={t("host.page.breadcrumb")} className="flex min-w-0 shrink items-center gap-1.5 text-sm text-muted">
            {space !== undefined && (
              <>
                <span className="hidden truncate lg:inline">{space}</span>
                <ChevronRight aria-hidden className="hidden size-3.5 shrink-0 lg:block" />
              </>
            )}
            <Link
              href={`/hosts/${encodeURIComponent(tab.host)}`}
              className="truncate text-[1.0625rem] font-semibold tracking-[-0.01em] text-foreground no-underline outline-none hover:underline data-[focus-visible]:ring-2 data-[focus-visible]:ring-focus"
            >
              {tab.label}
            </Link>
          </nav>
          <TabMark status={tab.state.status} />
          {tab.cwd !== undefined && (
            <span title={tab.cwd} className="hidden min-w-0 items-center gap-1.5 rounded-lg bg-default px-2 py-1 xl:flex">
              <Folder aria-hidden className="size-3.5 shrink-0 text-muted" />
              <span className="sr-only">{t("terminal.cwd")}</span>
              <span className="t-code truncate text-[0.75rem]">{tab.cwd}</span>
            </span>
          )}
        </div>
      ) : (
        <div className="flex-1" />
      )}
      <div className="flex shrink-0 items-center gap-1.5">
        <Button
          variant="secondary"
          size="sm"
          isDisabled={!tab}
          aria-pressed={files}
          onPress={props.onFiles}
          className={`h-9 rounded-full px-3.5 ${files ? "bg-foreground text-background" : ""}`}
        >
          <FolderOpen aria-hidden className="size-4" />
          {t("terminal.files")}
        </Button>
        <RowMenu label={t("terminal.more")} actions={props.more} />
      </div>
    </div>
  );
}

/**
 * FilesColumn is where the Files panel docks, inside the island at its end
 * edge, the terminal narrowing for it: its head names the tab's Host, and
 * its listing is the Files panel's (#179).
 */
function FilesColumn({ tab, onClose }: { tab: Tab; onClose: () => void }) {
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
    </aside>
  );
}

/** EmptyWorkspace is the workspace with no tab: what a terminal is, up to eight Managed Hosts to open one to, and the switcher. */
function EmptyWorkspace({ hosts }: { hosts: readonly Host[] | undefined }) {
  const t = useT();
  return (
    <div className="flex h-full flex-col items-start justify-center gap-6 overflow-y-auto p-6 md:p-12">
      <div className="flex flex-col gap-2">
        <h1 className="t-display">{t("terminal.empty.title")}</h1>
        <p className="max-w-[60ch] text-muted">{t("terminal.empty.lead")}</p>
      </div>
      {hosts && hosts.length > 0 && (
        <ul className="grid w-full max-w-3xl grid-cols-[repeat(auto-fill,minmax(11rem,1fr))] gap-2">
          {hosts.slice(0, 8).map((host) => (
            <li key={host.name}>
              <button
                type="button"
                onClick={() => addTab(host.name)}
                className="island flex w-full flex-col items-start rounded-2xl px-4 py-3 text-start outline-none hover:bg-surface-secondary focus-visible:ring-2 focus-visible:ring-focus"
              >
                <span className="t-name max-w-full truncate">{host.name}</span>
                <span className="t-label">{host.space}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
      <button
        type="button"
        onClick={openSwitcher}
        className="t-label flex items-center gap-2 rounded-md underline-offset-4 outline-none hover:underline focus-visible:ring-2 focus-visible:ring-focus"
      >
        {t("terminal.empty.all")}
        <span className="hidden md:inline">
          <Kbd>{chord("K")}</Kbd>
        </span>
      </button>
    </div>
  );
}

/** WorkspaceProps are what the frame gives the workspace. */
export interface WorkspaceProps {
  /** shown says the frame is on the terminal's page; elsewhere the workspace is kept, hidden. */
  shown: boolean;
  /** notice is the frame's notice, which on the terminal heads the workspace. */
  notice: ReactNode;
}

/**
 * Workspace is the terminal workspace beside the rail: one island with the
 * active tab's head and every tab's terminal under it, only the active one
 * shown, and the Files column docked at its end. Off the terminal's page it
 * stays mounted, hidden and inert, its terminals' painting paused.
 */
export function Workspace({ shown, notice }: WorkspaceProps) {
  const t = useT();
  const ws = useWorkspace();
  const hosts = useManagedHosts();
  const [files, setFiles] = useState(false);
  const [ending, setEnding] = useState<Tab>();
  const tab = ws.tabs.find((each) => each.key === ws.active);
  const more = useMore(tab, setEnding);
  // This document's SSH Sessions, which end first what the document before a reload left.
  const [terminals] = useState(() => new Terminals());

  // A document that kept SSH Sessions from before a reload ends them now, with or without a tab to open.
  useEffect(() => {
    void terminals.endEarlier();
  }, [terminals]);

  // The URL lists the tabs, so a reload or a bookmark opens the same Hosts again.
  const query = workspaceQuery(ws);
  useEffect(() => {
    if (shown && window.location.search !== query) {
      navigate(terminalPath + query, { replace: true });
    }
  }, [shown, query]);

  // The terminal shown takes the focus when asked: a tab chosen by a pointer, the switcher or a chord, or the
  // workspace shown with the focus nowhere.
  const overlay = ws.switcher || ending !== undefined || ws.closing !== undefined;
  useEffect(() => {
    if (shown && !overlay && ws.active !== undefined) {
      runtime(ws.active).emulator?.focus();
    }
  }, [ws.focusRequest]);
  useEffect(() => {
    const focused = document.activeElement;
    if (shown && !overlay && ws.active !== undefined && (focused === null || focused === document.body)) {
      runtime(ws.active).emulator?.focus();
    }
  }, [shown]);

  return (
    <div
      inert={!shown}
      className={`fixed inset-y-0 end-0 start-[var(--rail-w)] z-20 flex flex-col gap-3 p-3 ps-0 max-md:top-[calc(3.5rem+1px+env(safe-area-inset-top))] max-md:bottom-[calc(3.8125rem+env(safe-area-inset-bottom))] max-md:p-0 ${
        shown ? "" : "invisible"
      }`}
    >
      {shown && <Title tab={tab} />}
      {notice}
      <section
        aria-label={t("terminal.title")}
        className="island flex min-h-0 flex-1 overflow-hidden rounded-2xl max-md:rounded-none"
      >
        <div className="flex min-w-0 flex-1 flex-col">
          <Head tab={tab} space={tab && spaceOf(hosts, tab.host)} files={files} onFiles={() => setFiles(!files)} more={more} />
          <div className="relative min-h-0 flex-1 overflow-hidden bg-[var(--term-surface)]">
            {ws.tabs.map((each) => (
              <Pane key={each.key} tab={each} active={each.key === ws.active} shown={shown} terminals={terminals} />
            ))}
            {ws.tabs.length === 0 && <EmptyWorkspace hosts={hosts} />}
          </div>
        </div>
        {files && tab && <FilesColumn tab={tab} onClose={() => setFiles(false)} />}
      </section>
      {shown && ws.switcher && <Switcher hosts={hosts} />}
      {ending && (
        <ConfirmDialog
          title={t("terminal.end.title", { host: ending.label })}
          consequence={t("terminal.end.consequence", { host: ending.host })}
          confirm={t("terminal.end.confirm")}
          onConfirm={() => {
            runtime(ending.key).end?.();
            setEnding(undefined);
          }}
          onCancel={() => setEnding(undefined)}
        />
      )}
    </div>
  );
}
