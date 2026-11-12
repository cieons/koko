import { Button } from "@heroui/react";
import { ChevronRight, Folder, FolderOpen } from "lucide-react";
import { type ReactNode, type RefObject, useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { Link } from "react-aria-components";

import type { Host } from "../api/host";
import { hostPathOf } from "../pages/host/record";
import { useT } from "../shell/i18n";
import { navigate, type Route } from "../shell/router";
import { writeClipboard } from "../ui/copy";
import { notify } from "../ui/notify";
import { useDocumentTitle } from "../ui/Page";
import { useCoarsePointer, usePhone, useSideways } from "../ui/phone";
import { type RowAction, RowMenu } from "../ui/RowMenu";
import { Terminals } from "./connection";
import { fontSizes } from "./emulator/Emulator";
import { resetUploads } from "./files";
import { DropCover, type Dropped, FilesColumn, FilesSheet, useFileDrop } from "./FilesPanel";
import { useManagedHosts } from "./hosts";
import { KeyBar } from "./KeyBar";
import { chord, openSwitcher } from "./keys";
import { Pane } from "./Pane";
import { EndQuestion, Kbd, shortPath, spaceOf } from "./RailSection";
import { Switcher } from "./Switcher";
import { TabButton, TabSheet } from "./TabSheet";
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
              href={hostPathOf(tab.host)}
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
 * PhoneHead is the phone's row over the terminal, under the safe area: the
 * wordmark, a link back to Hosts; the active tab's state mark, with its
 * word when it is not Connected, and the directory's last two parts; then
 * Files and More as icon buttons.
 */
function PhoneHead(props: { tab: Tab | undefined; onFiles: () => void; more: RowAction[] }) {
  const t = useT();
  const { tab } = props;
  return (
    <div className="box-content flex h-12 shrink-0 items-center gap-2 border-b border-line bg-background ps-4 pe-1 pt-[env(safe-area-inset-top)]">
      <Link
        href="/"
        className="me-1 rounded-md text-lg leading-none font-semibold tracking-[-0.04em] text-foreground no-underline outline-none select-none data-[focus-visible]:ring-2 data-[focus-visible]:ring-focus"
      >
        {t("shell.brand")}
        <span className="sr-only">{`, ${t("terminal.back")}`}</span>
      </Link>
      <div className="flex min-w-0 flex-1 items-center gap-2.5">
        {tab && <TabMark status={tab.state.status} word={tab.state.status !== "connected"} />}
        {tab?.state.status === "connected" && tab.cwd !== undefined && (
          <span title={tab.cwd} className="t-code min-w-0 truncate text-[0.75rem] text-muted">
            <span className="sr-only">{t("terminal.cwd")} </span>
            {shortPath(tab.cwd)}
          </span>
        )}
      </div>
      <PhoneActions {...props} />
    </div>
  );
}

/** PhoneActions are Files and More as icon buttons: at the end of the phone's head, or of the key bar's row held sideways. */
function PhoneActions(props: { tab: Tab | undefined; onFiles: () => void; more: RowAction[] }) {
  const t = useT();
  return (
    <span className="flex shrink-0 items-center">
      <Button
        variant="ghost"
        isIconOnly
        className="rounded-full"
        aria-label={t("terminal.files")}
        isDisabled={!props.tab}
        onPress={props.onFiles}
      >
        <FolderOpen aria-hidden className="size-[1.125rem]" />
      </Button>
      <RowMenu label={t("terminal.more")} actions={props.more} />
    </span>
  );
}

/**
 * useVisualViewport keeps a phone's workspace in the visual viewport, the
 * part of the page the soft keyboard leaves in view, so the key bar sits on
 * the keyboard and the terminal fills what is above it.
 */
function useVisualViewport(layer: RefObject<HTMLDivElement | null>, on: boolean): void {
  useLayoutEffect(() => {
    const element = layer.current;
    const viewport = window.visualViewport;
    if (!element || !viewport || !on) {
      return;
    }
    const place = () => {
      element.style.top = `${viewport.offsetTop}px`;
      element.style.height = `${viewport.height}px`;
    };
    place();
    viewport.addEventListener("resize", place);
    viewport.addEventListener("scroll", place);
    return () => {
      viewport.removeEventListener("resize", place);
      viewport.removeEventListener("scroll", place);
      element.style.top = "";
      element.style.height = "";
    };
  }, [layer, on]);
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
  /** places are the places the caller may see, which a phone's sheet of tabs leads back to. */
  places: readonly Route[];
}

/**
 * Workspace is the terminal workspace beside the rail: one island with the
 * active tab's head and every tab's terminal under it, only the active one
 * shown, and the Files column docked at its end; on a touch screen the key
 * bar at its foot. On a phone, and a phone held sideways, it fills the
 * visual viewport in place of the frame: a row over the terminal (none
 * held sideways), and under it the key bar's row, starting with the tab
 * button. Off the terminal's page it stays mounted, hidden and inert, its
 * terminals' painting paused. Every form keeps each terminal at one place
 * in the tree, so none is mounted again as the form changes.
 */
export function Workspace({ shown, notice, places }: WorkspaceProps) {
  const t = useT();
  const ws = useWorkspace();
  const hosts = useManagedHosts();
  const sideways = useSideways();
  const compact = usePhone() || sideways;
  const coarse = useCoarsePointer();
  const layer = useRef<HTMLDivElement>(null);
  const [files, setFiles] = useState(false);
  const [sheet, setSheet] = useState(false);
  const [dropped, setDropped] = useState<Dropped>();
  const [ending, setEnding] = useState<Tab>();
  const tab = ws.tabs.find((each) => each.key === ws.active);
  const taken = useCallback(() => setDropped(undefined), []);
  // Files dropped on a terminal whose shell runs go into the directory it is in, through the Files panel.
  const drop = useFileDrop(shown && tab?.session !== undefined && shellLives(tab), (dropping) => {
    if (tab && dropping.length > 0) {
      setDropped({ tab: tab.key, files: dropping });
      setFiles(true);
    }
  });
  const panel = tab && {
    tab,
    dropped: dropped?.tab === tab.key ? dropped : undefined,
    onTaken: taken,
    onClose: () => setFiles(false),
  };
  const more = useMore(tab, setEnding);
  // This document's SSH Sessions, which end first what the document before a reload left.
  const [terminals] = useState(() => new Terminals());

  // A document that kept SSH Sessions from before a reload ends them now, with or without a tab to open.
  useEffect(() => {
    void terminals.endEarlier();
  }, [terminals]);

  // The uploads go with the workspace, as on signing out, so the next Sign-in sees none of them.
  useEffect(() => resetUploads, []);

  // The URL lists the tabs, so a reload or a bookmark opens the same Hosts again.
  const query = workspaceQuery(ws);
  useEffect(() => {
    if (shown && window.location.search !== query) {
      navigate(terminalPath + query, { replace: true });
    }
  }, [shown, query]);

  useVisualViewport(layer, compact && shown);
  // The sheet of tabs is the phone's: a window widening past it leaves it closed.
  const sheetOpen = sheet && compact && shown;

  // The terminal shown takes the focus when asked (a tab chosen by a
  // pointer, the switcher or a chord), and when the workspace is shown with
  // the focus nowhere: each once its emulator is ready and no overlay is
  // open, since an emulator still loading has nothing to focus. On a touch
  // screen it never takes the focus by itself, which would open the soft
  // keyboard: a tap on the terminal or the keyboard key does.
  const overlay = ws.switcher || ending !== undefined || ws.closing !== undefined || (files && compact) || sheetOpen;
  const ready = tab?.ready === true;
  const answered = useRef(0);
  useEffect(() => {
    if (!shown || overlay || !ready || coarse || ws.active === undefined) {
      return;
    }
    const focused = document.activeElement;
    if (ws.focusRequest !== answered.current || focused === null || focused === document.body) {
      answered.current = ws.focusRequest;
      runtime(ws.active).emulator?.focus();
    }
  }, [shown, overlay, ready, coarse, ws.active, ws.focusRequest]);

  const actions = { tab, onFiles: () => setFiles(true), more };
  return (
    <div
      ref={layer}
      inert={!shown}
      className={`${
        compact
          ? "fixed inset-x-0 top-0 z-20 flex h-dvh flex-col bg-[var(--term-surface)] ps-[env(safe-area-inset-left)] pe-[env(safe-area-inset-right)]"
          : "fixed inset-y-0 end-0 start-[var(--rail-w)] z-20 flex flex-col gap-3 p-3 ps-0"
      } ${shown ? "" : "invisible"}`}
    >
      {shown && <Title tab={tab} />}
      {compact && !sideways && <PhoneHead {...actions} />}
      {notice}
      <section
        aria-label={t("terminal.title")}
        className={compact ? "flex min-h-0 flex-1 overflow-hidden" : "island flex min-h-0 flex-1 overflow-hidden rounded-2xl"}
      >
        <div className="flex min-w-0 flex-1 flex-col">
          {!compact && (
            <Head tab={tab} space={tab && spaceOf(hosts, tab.host)} files={files} onFiles={() => setFiles(!files)} more={more} />
          )}
          <div className="relative min-h-0 flex-1 overflow-hidden bg-[var(--term-surface)]" {...drop.handlers}>
            {ws.tabs.map((each) => (
              <Pane key={each.key} tab={each} active={each.key === ws.active} shown={shown} terminals={terminals} />
            ))}
            {ws.tabs.length === 0 && <EmptyWorkspace hosts={hosts} />}
            {drop.over && tab && <DropCover tab={tab} />}
          </div>
          {compact ? (
            <div className="flex shrink-0 items-center gap-1.5 border-t border-line bg-background px-2 pt-1.5 pb-[max(0.375rem,env(safe-area-inset-bottom))]">
              <TabButton onPress={() => setSheet(true)} />
              {coarse && tab ? <KeyBar tab={tab.key} /> : <span className="flex-1" />}
              {sideways && <PhoneActions {...actions} />}
            </div>
          ) : (
            coarse &&
            tab && (
              <div className="flex shrink-0 border-t border-line px-2 py-1.5">
                <KeyBar tab={tab.key} />
              </div>
            )
          )}
        </div>
        {files && panel && !compact && <FilesColumn {...panel} />}
      </section>
      {files && panel && compact && shown && <FilesSheet {...panel} />}
      {sheetOpen && <TabSheet places={places} hosts={hosts} onClose={() => setSheet(false)} />}
      {shown && ws.switcher && <Switcher hosts={hosts} />}
      {ending && (
        <EndQuestion
          tab={ending}
          onEnd={() => {
            runtime(ending.key).end?.();
            setEnding(undefined);
          }}
          onCancel={() => setEnding(undefined)}
        />
      )}
    </div>
  );
}
