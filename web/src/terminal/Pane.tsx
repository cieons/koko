import { Button, Spinner } from "@heroui/react";
import { Clipboard as ClipboardIcon } from "lucide-react";
import { type ReactNode, useEffect, useRef, useState } from "react";

import { CodeRevoked } from "../api/device";
import { CodeHostUnreachable, CodeLoginRefused } from "../api/enroll";
import { CodeUnknownHost } from "../api/host";
import { CodeCookieRequired, CodeSignInRequired } from "../api/web";
import type { MessageKey } from "../i18n/catalogues";
import { type Translate, useCount, useLanguage, useT } from "../shell/i18n";
import { navigate, signInPage, signInPath } from "../shell/router";
import { writeClipboard } from "../ui/copy";
import { AlertLine, loadingDelay } from "../ui/states";
import type { Connection, Ending, Terminals } from "./connection";
import { Emulator, type EmulatorHandle } from "./emulator/Emulator";
import { TabMark } from "./TabMark";
import { closeTab, panelId, patchTab, reopen, runtime, type Tab, useWorkspace } from "./workspace";

// A tab's pane: its terminal, and over it the tab's state (docs/web-design.md,
// "Tabs and their states"). No state moves the layout: a tab with nothing to
// show is covered; one whose terminal stays has cards at its top end.

/**
 * restartNotice is the Gateway's notice when the koko server stops for a
 * restart (gateway.go, errStopping), which the page says in its own words;
 * it must match the Gateway's byte for byte.
 */
const restartNotice = "the koko server is restarting; this SSH Session has ended.";

/**
 * refusals are the open's refusals (the Gateway's, spec #169) the page says
 * in its own words, by their codes; any other is said generally.
 */
const refusals: Readonly<Record<string, MessageKey>> = {
  [CodeUnknownHost]: "terminal.refused.unknown_host",
  host_registered: "terminal.refused.host_registered",
  relay_registered: "terminal.refused.relay_registered",
  access_refused: "terminal.refused.access_refused",
  ssh_session_limit: "terminal.refused.ssh_session_limit",
  [CodeHostUnreachable]: "terminal.refused.host_unreachable",
  host_key_mismatch: "terminal.refused.host_key_mismatch",
  [CodeLoginRefused]: "terminal.refused.login_refused",
  host_connection_failed: "terminal.refused.host_connection_failed",
  shell_refused: "terminal.refused.shell_refused",
};

/** shellRefusals are the web front's refusals the shell already has words for. */
const shellRefusals: Readonly<Record<string, MessageKey>> = {
  [CodeCookieRequired]: "error.cookie_required",
  [CodeRevoked]: "error.revoked",
  [CodeSignInRequired]: "error.signin_required",
};

/**
 * Said is what a state says: a line in the page's words, and beneath it the
 * koko server's own, as it wrote them, when they add to it: they may begin
 * with a Host's name, which keeps its case.
 */
interface Said {
  line: string;
  detail?: string;
}

/** refusalSaid says why an open or a resume was refused. */
function refusalSaid(t: Translate, host: string, code: string, message: string): Said {
  const shell = shellRefusals[code];
  if (shell !== undefined) {
    return { line: t(shell) };
  }
  // An unknown name's words are the same for every Host the caller cannot see, and add nothing.
  const detail = message === "" || code === CodeUnknownHost ? undefined : message;
  return { line: t(refusals[code] ?? "terminal.refused.other", { host }), detail };
}

/** endingSaid says why an SSH Session ended. */
function endingSaid(t: Translate, language: string, host: string, ending: Ending): Said {
  switch (ending.kind) {
    case "exitStatus":
      return { line: t("terminal.ended.status", { host, status: ending.status }) };
    case "exitSignal":
      return { line: t("terminal.ended.signal", { host, signal: ending.signal }) };
    case "notice":
      if (ending.notice === restartNotice) {
        return { line: t("terminal.ended.restart") };
      }
      return { line: t("terminal.ended.notice", { host }), detail: ending.notice === "" ? undefined : ending.notice };
    case "closed":
      return { line: t("terminal.ended.closed", { host }) };
    case "lost": {
      const time = new Intl.DateTimeFormat(language, { hour: "2-digit", minute: "2-digit" }).format(ending.at);
      return { line: t("terminal.ended.lost", { host, time }), detail: ending.message === "" ? undefined : ending.message };
    }
    case "protocol":
      return { line: t("terminal.ended.protocol"), detail: ending.message };
  }
}

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

/** Connecting stands over an empty terminal while its SSH Session opens, with a spinner past 300 ms. */
function Connecting({ host }: { host: string }) {
  const t = useT();
  const [slow, setSlow] = useState(false);
  useEffect(() => {
    const timer = setTimeout(() => setSlow(true), loadingDelay);
    return () => clearTimeout(timer);
  }, []);
  return (
    <div role="status" className="flex w-full max-w-lg items-center gap-3 text-ink-2">
      <span className="flex size-5 items-center justify-center">{slow && <Spinner aria-hidden size="sm" color="current" />}</span>
      <p className="text-[1.0625rem] font-medium">{t("terminal.connecting", { host })}</p>
    </div>
  );
}

/** Cover stands in place of a terminal that has nothing to show: why, and what to do. */
function Cover({ said, children }: { said: Said; children: ReactNode }) {
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
  const answer = () => patchTab(tab.key, { clipboard: undefined });
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

/** PaneProps are a tab, whether it is the one shown and whether the workspace is in view, and the document's Terminals. */
export interface PaneProps {
  tab: Tab;
  active: boolean;
  shown: boolean;
  terminals: Terminals;
}

/**
 * Pane is one tab's terminal. It stays mounted while another tab or
 * another page is shown, its painting paused, so its SSH Session carries
 * on; it opens the tab's SSH Session once its emulator has laid out its
 * grid, and a new one each time the tab is opened again.
 */
export function Pane({ tab, active, shown, terminals }: PaneProps) {
  const t = useT();
  const language = useLanguage();
  const { fontSize, announce, tabListId } = useWorkspace();
  const emulator = useRef<EmulatorHandle | null>(null);
  const connection = useRef<Connection | null>(null);
  const [grid, setGrid] = useState<{ cols: number; rows: number }>();
  const { key, host, state } = tab;

  // The tab's SSH Session: opened once the grid is known, at its size, and
  // again on each reopen; ended with the pane, as when the frame goes on
  // signing out.
  const gridNow = useRef(grid);
  gridNow.current = grid;
  const ready = grid !== undefined;
  useEffect(() => {
    const size = gridNow.current;
    if (!size) {
      return;
    }
    // A connection tells its state only after open returns, so its id is there to read.
    const opened: Connection = terminals.open(
      host,
      { columns: size.cols, rows: size.rows },
      {
        onState: (next) => patchTab(key, { state: next, session: opened.sessionId || undefined }),
        onOutput: (bytes) => emulator.current?.write(bytes),
      },
    );
    connection.current = opened;
    runtime(key).end = () => opened.end();
    return () => {
      opened.end();
      if (connection.current === opened) {
        connection.current = null;
      }
    };
  }, [ready, tab.opening, terminals, host, key]);

  // A refusal for want of a Sign-in, at an open or a resume, takes the page to Sign in, which comes back here.
  const signIn = state.status === "refused" && state.code === CodeSignInRequired;
  useEffect(() => {
    if (signIn && window.location.pathname !== signInPage) {
      navigate(signInPath(window.location.pathname + window.location.search), { replace: true });
    }
  }, [signIn]);

  const covered = state.status === "connecting" || state.status === "refused" || state.status === "unreachable" || tab.failed === true;
  const retry = () => reopen(key);
  const close = () => closeTab(key);
  let cover: ReactNode = null;
  if (tab.failed === true) {
    cover = (
      <Cover said={{ line: t("terminal.emulatorFailed") }}>
        <Button variant="primary" onPress={() => window.location.reload()}>
          {t("terminal.reload.action")}
        </Button>
      </Cover>
    );
  } else if (state.status === "connecting") {
    cover = <Connecting host={host} />;
  } else if (state.status === "refused" && state.code === "reload_page") {
    cover = (
      <Cover said={{ line: t("terminal.reload.message") }}>
        <Button variant="primary" onPress={() => window.location.reload()}>
          {t("terminal.reload.action")}
        </Button>
      </Cover>
    );
  } else if (state.status === "refused" || state.status === "unreachable") {
    const said =
      state.status === "refused" ? refusalSaid(t, host, state.code, state.message) : { line: t("terminal.unreachable") };
    cover = (
      <Cover said={said}>
        <Button variant="primary" onPress={retry}>
          {t("terminal.retry")}
        </Button>
        <Button variant="tertiary" onPress={close}>
          {t("terminal.tab.close")}
        </Button>
      </Cover>
    );
  }

  return (
    <div
      role="tabpanel"
      id={panelId(key, tabListId)}
      aria-label={tab.label}
      className={`absolute inset-0 flex flex-col ${active ? "" : "invisible"}`}
      inert={!active}
    >
      <div className={`flex min-h-0 flex-1 transition-opacity ${state.status === "reconnecting" ? "opacity-55" : ""}`}>
        <Emulator
          ref={(handle) => {
            emulator.current = handle;
            if (handle) {
              runtime(key).emulator = handle;
            }
          }}
          label={t("terminal.label", { tab: tab.label })}
          fontSize={fontSize}
          paused={!active || !shown}
          announce={announce}
          onReady={(cols, rows) => {
            setGrid({ cols, rows });
            patchTab(key, { ready: true });
          }}
          onInput={(bytes) => connection.current?.send(bytes)}
          onResize={(cols, rows) => {
            setGrid({ cols, rows });
            connection.current?.resize(cols, rows);
          }}
          onClipboardWrite={(text) => patchTab(key, { clipboard: text === "" ? undefined : text })}
          onWorkingDirectory={(uri) => {
            const cwd = directoryOf(uri, host);
            if (cwd !== undefined) {
              patchTab(key, { cwd });
            }
          }}
          onTitle={() => {}}
          onError={() => patchTab(key, { failed: true })}
        />
      </div>
      {covered && (
        <div className="absolute inset-0 flex items-center justify-center bg-[var(--term-surface)] p-6">{cover}</div>
      )}
      {!covered && (state.status !== "connected" || tab.clipboard !== undefined) && (
        <div className="pointer-events-none absolute inset-x-2 top-2 z-10 flex flex-col items-stretch gap-2 md:start-auto md:end-3 md:top-3 md:w-[min(34rem,calc(100%-1.5rem))]">
          {state.status === "reconnecting" && (
            <Notice mark={<TabMark status="reconnecting" word={false} />}>
              <p className="min-w-0 flex-1 basis-56">{t("terminal.reconnecting")}</p>
              <Button size="sm" variant="secondary" onPress={() => connection.current?.tryNow()}>
                {t("terminal.reconnecting.now")}
              </Button>
            </Notice>
          )}
          {state.status === "ended" && (
            <Notice mark={<TabMark status="ended" word={false} />}>
              <Saying said={endingSaid(t, language, host, state.ending)} />
              <div className="flex gap-2">
                <Button size="sm" variant="primary" onPress={retry}>
                  {t("terminal.ended.new")}
                </Button>
                <Button size="sm" variant="tertiary" onPress={close}>
                  {t("terminal.tab.close")}
                </Button>
              </div>
            </Notice>
          )}
          {tab.clipboard !== undefined && <ClipboardNotice tab={tab} text={tab.clipboard} />}
        </div>
      )}
    </div>
  );
}

