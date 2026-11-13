import { Button, Spinner } from "@heroui/react";
import { Clipboard as ClipboardIcon } from "lucide-react";
import { type ReactNode, useEffect, useLayoutEffect, useRef, useState } from "react";

import { CodeSignInRequired } from "../api/web";
import { useCount, useT } from "../shell/i18n";
import { navigate, signInPage, signInPath } from "../shell/router";
import { writeClipboard } from "../ui/copy";
import { AlertLine, loadingDelay } from "../ui/states";
import { Attachment, gridOf, type Size } from "./connection";
import { Emulator, type EmulatorHandle, type Metrics } from "./emulator/Emulator";
import { endingSaid, refusalSaid, restartNotice, type Said } from "./said";
import { TabMark } from "./TabMark";
import { themeColors, watchTheme } from "./themeColors";
import { timing } from "./timing";
import {
  askClose,
  boxSize,
  panelId,
  patchTab,
  personActed,
  reopenTab,
  runtime,
  setBox,
  type Tab,
  takeTyped,
  useWorkspace,
} from "./workspace";

// A tab's pane: its terminal, and over it the tab's state (docs/web-design.md,
// "Tabs and their states"). No state moves the layout: a tab with nothing to
// show is covered; one whose terminal stays has cards at its top end.

/** maxAttachments is the most pages attached to one SSH Session (the Gateway's maxAttachments). */
const maxAttachments = 8;

/** focusReports are what wterm sends as the terminal gains and loses the focus, for a program that asks. */
const focusReports = new Set(["\x1b[I", "\x1b[O"]);

/**
 * Notice is a card at the terminal's top end, over it, which stays readable
 * beside and under it. Its mark is for the eye: its words say the state.
 */
function Notice({ mark, children }: { mark: ReactNode; children: ReactNode }) {
  return (
    <div
      role="status"
      className="pointer-events-auto flex flex-wrap items-center gap-x-4 gap-y-3 rounded-2xl bg-overlay py-3 ps-4 pe-3 text-sm shadow-overlay"
    >
      <span aria-hidden className="flex h-5 items-center self-start">
        {mark}
      </span>
      {children}
    </div>
  );
}

/** Saying is a state's line, and the koko server's words beneath it in the code face. */
function Saying({ said }: { said: Said }) {
  return (
    <div className="min-w-0 flex-1 basis-56">
      <p>{said.line}</p>
      {said.detail !== undefined && <p className="t-code mt-1 text-xs break-words text-muted">{said.detail}</p>}
    </div>
  );
}

/** Waiting stands over an empty terminal while its SSH Session starts or the page attaches, with a spinner past 300 ms. */
export function Waiting({ line }: { line: string }) {
  const [slow, setSlow] = useState(false);
  useEffect(() => {
    const timer = setTimeout(() => setSlow(true), loadingDelay);
    return () => clearTimeout(timer);
  }, []);
  return (
    <div role="status" className="flex w-full max-w-lg items-center gap-3 text-ink-2">
      <span className="flex size-5 items-center justify-center">{slow && <Spinner aria-hidden size="sm" color="current" />}</span>
      <p className="text-[1.0625rem] font-medium">{line}</p>
    </div>
  );
}

/** Cover stands in place of a terminal that has nothing to show: why, and what to do. */
export function Cover({ said, children }: { said: Said; children: ReactNode }) {
  return (
    <div className="flex w-full max-w-lg flex-col items-start gap-4">
      <AlertLine>
        {said.line}
        {said.detail !== undefined && <span className="t-code mt-1 block text-xs break-words">{said.detail}</span>}
      </AlertLine>
      <div className="flex flex-wrap gap-2">{children}</div>
    </div>
  );
}

/** ClipboardNotice asks before a program's request writes the clipboard (OSC 52); a newer request replaces it. */
function ClipboardNotice({ tab, text }: { tab: Tab; text: string }) {
  const t = useT();
  const count = useCount();
  const [failed, setFailed] = useState(false);
  const [first] = text.split("\n");
  const answer = () => patchTab(tab.id, { clipboard: undefined });
  return (
    <Notice mark={<ClipboardIcon aria-hidden className="size-4 text-muted" />}>
      <div className="min-w-0 flex-1 basis-56">
        <p>{count("terminal.clipboard.ask", [...text].length, { host: tab.host })}</p>
        <p className="t-code mt-1 truncate text-xs text-muted">{first}</p>
        {failed && <p className="mt-1 text-danger">{t("terminal.clipboard.failed")}</p>}
      </div>
      <div className="flex gap-2">
        <Button size="sm" variant="secondary" onPress={() => writeClipboard(text).then(answer, () => setFailed(true))}>
          {t("terminal.clipboard.copy")}
        </Button>
        <Button size="sm" variant="tertiary" onPress={answer}>
          {t("terminal.clipboard.dismiss")}
        </Button>
      </div>
    </Notice>
  );
}

/** directoryOf is the directory an OSC 7 URI names, when it names one on this Host or on no host. */
function directoryOf(uri: string, host: string): string | undefined {
  try {
    const url = new URL(uri);
    if (url.protocol === "file:" && (url.hostname === "" || url.hostname === host)) {
      return decodeURIComponent(url.pathname);
    }
  } catch {
    // A directory that is no URI is none the page can name.
  }
  return undefined;
}

/** cellsOf is how many cells a box of width by height pixels holds, or undefined while it cannot be told. */
function cellsOf(box: { width: number; height: number } | undefined, metrics: Metrics | null | undefined): Size | undefined {
  if (!box || !metrics || box.width <= 0 || box.height <= 0) {
    return undefined;
  }
  return gridOf({
    columns: Math.floor((box.width - metrics.padX) / metrics.cellWidth),
    rows: Math.floor((box.height - metrics.padY) / metrics.cellHeight),
  });
}

/** PaneProps are a tab, whether it is the one shown and whether the workspace is in view. */
export interface PaneProps {
  tab: Tab;
  active: boolean;
  shown: boolean;
}

/**
 * Pane is one tab's terminal. It attaches to the tab's SSH Session once
 * the tab is first shown, and stays attached while another tab or another
 * page is shown, its painting paused, so switching back draws at once.
 * The terminal is drawn at the pty's size, which the koko server sets by
 * the reset record: scaled down to fit a box too small, and with a margin
 * in the terminal's background in a box too large. The page's own box goes
 * to the koko server once it has been still for a moment.
 */
export function Pane({ tab, active, shown }: PaneProps) {
  const t = useT();
  const { fontSize, announce, tabListId, socket } = useWorkspace();
  const emulator = useRef<EmulatorHandle | null>(null);
  const box = useRef<HTMLDivElement>(null);
  const [ready, setReady] = useState(false);
  const [seen, setSeen] = useState(false);
  /** attachments counts this page's attachments to the SSH Session: Show here and Try again make the next. */
  const [attachments, setAttachments] = useState(0);
  /** grid is the pty's size, from the last reset. */
  const [grid, setGrid] = useState<Size>();
  const [boxPx, setBoxPx] = useState<{ width: number; height: number }>();
  const [metrics, setMetrics] = useState<Metrics | null>(null);
  const { id, host, session, attachment } = tab;
  const running = tab.state === "running";

  // The page attaches to a tab once it shows it, and stays attached.
  useEffect(() => {
    if (active && shown) {
      setSeen(true);
    }
  }, [active, shown]);

  // The box: measured as it changes, in pixels, and in cells against the emulator's.
  useLayoutEffect(() => {
    const element = box.current;
    if (!element) {
      return;
    }
    const measure = () => setBoxPx({ width: element.clientWidth, height: element.clientHeight });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    window.addEventListener("resize", measure);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, []);
  useEffect(() => {
    if (ready) {
      setMetrics(emulator.current?.metrics() ?? null);
    }
  }, [ready, fontSize, boxPx]);
  const cells = cellsOf(boxPx, metrics);

  // The box goes to the koko server, and becomes the size tabs open at, once it has been still.
  const columns = cells?.columns;
  const rows = cells?.rows;
  useEffect(() => {
    if (columns === undefined || rows === undefined) {
      return;
    }
    const timer = setTimeout(() => {
      if (active) {
        setBox({ columns, rows });
      }
      runtime(id).attachment?.resize({ columns, rows });
    }, timing.still);
    return () => clearTimeout(timer);
  }, [columns, rows, active, id]);

  // The attachment: made once the tab has been shown and its shell runs, and again on Show here.
  useEffect(() => {
    if (!ready || !seen || !running || session === undefined) {
      return;
    }
    const rt = runtime(id);
    // The box as it is now: the measure above may not have come round yet.
    const element = box.current;
    const now = element && cellsOf({ width: element.clientWidth, height: element.clientHeight }, emulator.current?.metrics());
    const made = new Attachment(session, now ?? boxSize(), themeColors(), {
      onState: (next) => patchTab(id, { attachment: next }),
      onOutput: (bytes) => emulator.current?.write(bytes),
      onReset: (size) => {
        emulator.current?.reset(size.columns, size.rows);
        setGrid(size);
      },
      onActive: (on) => patchTab(id, on ? { activePage: true } : { activePage: false, clipboard: undefined }),
    });
    rt.attachment = made;
    patchTab(id, { attachment: made.state });
    if (rt.acted) {
      rt.acted = false;
      made.active();
    }
    const unwatch = watchTheme(() => {
      const colors = themeColors();
      if (colors) {
        made.setColors(colors);
      }
    });
    return () => {
      unwatch();
      made.detach();
      if (rt.attachment === made) {
        rt.attachment = undefined;
      }
    };
  }, [ready, seen, running, session, attachments, id]);

  // A refusal for want of a Sign-in takes the page to Sign in, which comes back here.
  const signIn = attachment?.status === "refused" && attachment.code === CodeSignInRequired;
  useEffect(() => {
    if (signIn && window.location.pathname !== signInPage) {
      navigate(signInPath(window.location.pathname + window.location.search), { replace: true });
    }
  }, [signIn]);

  /** attachAgain makes a new attachment on the person's word, which makes this page the active one. */
  const attachAgain = () => {
    if (document.visibilityState === "visible") {
      runtime(id).acted = true;
    }
    setAttachments((n) => n + 1);
  };
  const close = () => askClose(id);
  const reload = (
    <Button variant="primary" onPress={() => window.location.reload()}>
      {t("terminal.reload.action")}
    </Button>
  );
  const closeButton = (
    <Button variant="tertiary" onPress={close}>
      {t("terminal.tab.close")}
    </Button>
  );

  let cover: ReactNode = null;
  if (tab.failed === true) {
    cover = <Cover said={{ line: t("terminal.emulatorFailed") }}>{reload}</Cover>;
  } else if (running && attachment?.status === "refused" && attachment.code === "reload_page") {
    cover = <Cover said={{ line: t("terminal.reload.message") }}>{reload}</Cover>;
  } else if (running && attachment?.status === "refused") {
    cover = (
      <Cover said={refusalSaid(t, host, attachment.code, attachment.message)}>
        <Button variant="primary" onPress={attachAgain}>
          {t("terminal.retry")}
        </Button>
        {closeButton}
      </Cover>
    );
  } else if (running && attachment?.status === "protocol") {
    cover = <Cover said={{ line: t("terminal.ended.protocol"), detail: attachment.message }}>{reload}</Cover>;
  } else if (running && attachment?.status === "displaced") {
    cover = (
      <Cover said={{ line: t("terminal.displaced", { count: maxAttachments }) }}>
        <Button variant="primary" onPress={attachAgain}>
          {t("terminal.displaced.show")}
        </Button>
      </Cover>
    );
  } else if (
    running &&
    ((socket.status === "reconnecting" && socket.stopping) || (attachment?.status === "detached" && attachment.notice === restartNotice))
  ) {
    // The koko server is restarting: the tab comes back restarted once the page reconnects.
    cover = <Waiting line={t("terminal.ended.restart")} />;
  } else if (tab.state === "restarted") {
    cover = <Waiting line={t("terminal.restarted")} />;
  } else if (tab.state === "starting" || (running && attachment?.status !== "attached" && attachment?.status !== "reconnecting")) {
    // Attaching, or an attachment the koko server let go while the Workspace still says the shell runs: its word comes.
    cover = <Waiting line={t("terminal.connecting", { host })} />;
  } else if (tab.state === "refused") {
    const refused = tab.refused ?? { code: "", message: "" };
    cover = (
      <Cover said={refusalSaid(t, host, refused.code, refused.message)}>
        <Button variant="primary" onPress={() => reopenTab(id)}>
          {t("terminal.retry")}
        </Button>
        {closeButton}
      </Cover>
    );
  }

  const reconnecting = running && attachment?.status === "reconnecting";
  const clipboard = tab.activePage ? tab.clipboard : undefined;
  const cards = cover === null && (reconnecting || tab.state === "ended" || clipboard !== undefined);

  // The terminal at the pty's size: scaled down to fit a box too small, at its top start in one too large.
  const natural = grid && metrics && { width: grid.columns * metrics.cellWidth + metrics.padX, height: grid.rows * metrics.cellHeight + metrics.padY };
  const scale =
    natural && boxPx && boxPx.width > 0 && boxPx.height > 0
      ? Math.min(1, boxPx.width / natural.width, boxPx.height / natural.height)
      : 1;

  return (
    <div
      role="tabpanel"
      id={panelId(id, tabListId)}
      aria-label={tab.label}
      className={`absolute inset-0 flex flex-col ${active ? "" : "invisible"}`}
      inert={!active}
    >
      <div
        ref={box}
        className={`relative min-h-0 flex-1 overflow-hidden transition-opacity ${reconnecting ? "opacity-55" : ""}`}
        onPointerDown={() => personActed(id)}
      >
        <div
          data-terminal-grid
          className={natural ? "absolute top-0 left-0 flex origin-top-left" : "flex size-full"}
          style={natural ? { width: natural.width, height: natural.height, transform: scale < 1 ? `scale(${scale})` : undefined } : undefined}
        >
          <Emulator
            ref={(handle) => {
              emulator.current = handle;
              if (handle) {
                runtime(id).emulator = handle;
              }
            }}
            label={t("terminal.label", { tab: tab.label })}
            fontSize={fontSize}
            paused={!active || !shown}
            announce={announce}
            onReady={() => {
              setReady(true);
              patchTab(id, { ready: true });
            }}
            onInput={(bytes) => {
              // A program's focus reports go from the active page alone.
              if (!tab.activePage && focusReports.has(String.fromCharCode(...bytes))) {
                return;
              }
              // A character typed while the key bar's Control or Alt is on is pressed with it instead.
              if (!takeTyped(id, bytes)) {
                runtime(id).attachment?.send(bytes);
              }
            }}
            onClipboardWrite={(text) => {
              // A copy is offered in the active page alone.
              if (tab.activePage) {
                patchTab(id, { clipboard: text === "" ? undefined : text });
              }
            }}
            onWorkingDirectory={(uri) => {
              const cwd = directoryOf(uri, host);
              if (cwd !== undefined) {
                patchTab(id, { cwd });
              }
            }}
            onTitle={() => {}}
            onError={() => patchTab(id, { failed: true })}
          />
        </div>
      </div>
      {cover !== null && <div className="absolute inset-0 flex items-center justify-center bg-[var(--term-surface)] p-6">{cover}</div>}
      {cards && (
        <div className="pointer-events-none absolute inset-x-2 top-2 z-10 flex flex-col items-stretch gap-2 md:start-auto md:end-3 md:top-3 md:w-[min(34rem,calc(100%-1.5rem))]">
          {reconnecting && (
            <Notice mark={<TabMark status="reconnecting" word={false} />}>
              <p className="min-w-0 flex-1 basis-56">{t("terminal.reconnecting")}</p>
              <Button size="sm" variant="secondary" onPress={() => runtime(id).attachment?.tryNow()}>
                {t("terminal.reconnecting.now")}
              </Button>
            </Notice>
          )}
          {tab.state === "ended" && (
            <Notice mark={<TabMark status="ended" word={false} />}>
              <Saying said={endingSaid(t, host, tab.ended)} />
              <div className="flex gap-2">
                <Button size="sm" variant="primary" onPress={() => reopenTab(id)}>
                  {t("terminal.ended.new")}
                </Button>
                <Button size="sm" variant="tertiary" onPress={close}>
                  {t("terminal.tab.close")}
                </Button>
              </div>
            </Notice>
          )}
          {clipboard !== undefined && <ClipboardNotice tab={tab} text={clipboard} />}
        </div>
      )}
    </div>
  );
}
