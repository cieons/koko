import { Button } from "@heroui/react";
import { Layers, Plus, SquareTerminal, X } from "lucide-react";
import { type KeyboardEvent, useEffect, useLayoutEffect, useRef } from "react";
import { Link, Tab, TabList, Tabs } from "react-aria-components";

import type { Host } from "../api/host";
import { CodeSignInRequired } from "../api/web";
import { useT } from "../shell/i18n";
import { navigate, signInPage, signInPath } from "../shell/router";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { notify } from "../ui/notify";
import { useManagedHosts } from "./hosts";
import { chord, goToTerminal, openSwitcher } from "./keys";
import { stateWord, TabMark } from "./TabMark";
import {
  askClose,
  cancelClose,
  closeTab,
  maxTabs,
  saidClosedElsewhere,
  saidAtLimit,
  setTabListId,
  showTab,
  tabStatus,
  type Tab as TerminalTab,
  terminalPath,
  useWorkspace,
  workspace,
  workspaceQuery,
} from "./workspace";

// The terminal in the frame (docs/web-design.md, "The frame"): the rail's
// Terminal section on every page of a desktop, and the terminal's button at
// the start of a phone's bar. Both read the workspace's state, so they are
// in the first load; the workspace itself loads with the first terminal.

/** Kbd is a chord shown beside the control it works, for the eye alone: the control's name says it too. */
export function Kbd({ children }: { children: string }) {
  return (
    <kbd aria-hidden className="rounded-md bg-default px-1.5 py-0.5 font-mono text-[0.6875rem] font-medium text-muted">
      {children}
    </kbd>
  );
}

/** shortPath is a directory short enough for a tab's line: its last two parts. */
export function shortPath(path: string): string {
  const parts = path.split("/").filter(Boolean);
  return parts.length <= 2 ? path : `…/${parts.slice(-2).join("/")}`;
}

/** spaceOf is the Space of the Managed Host of that name, once host.list has answered. */
export function spaceOf(hosts: readonly Host[] | undefined, name: string): string | undefined {
  return hosts?.find((host) => host.name === name)?.space;
}

/** TabLine is a tab's second line: its state when it is not Connected, else its directory, or its Space. */
export function TabLine({ tab, space }: { tab: TerminalTab; space: string | undefined }) {
  const t = useT();
  const status = tabStatus(tab);
  if (status !== "connected") {
    // The mark beside it says the state to a screen reader already.
    return (
      <span
        aria-hidden
        className={`truncate text-xs font-medium ${status === "refused" ? "text-danger" : "text-ink-2"}`}
      >
        {stateWord(t, status)}
      </span>
    );
  }
  const line = tab.cwd === undefined ? space : shortPath(tab.cwd);
  return line === undefined ? null : <span className="truncate font-mono text-xs text-muted">{line}</span>;
}

/**
 * RailTabs is the rail's tab list, React Aria's Tabs, vertical: the arrow
 * keys move between tabs, and on the terminal show each; Delete or
 * Backspace closes the focused tab, asking first while its shell runs; a
 * pointer closes a tab on its mark, which is no button inside the tab. On
 * another page no tab is raised, and choosing one goes to the terminal.
 * Each tab's panel is in the workspace, named after this list's id, which
 * the list publishes as it is shown.
 */
function RailTabs({ open, onTerminal, hosts }: { open: boolean; onTerminal: boolean; hosts: readonly Host[] | undefined }) {
  const t = useT();
  const { tabs, active } = useWorkspace();
  const list = useRef<HTMLDivElement>(null);
  // Whether the tab chosen last was chosen with the keys, which leave the focus in the list.
  const byKeys = useRef(false);
  useLayoutEffect(() => {
    setTabListId(list.current?.id || undefined);
    return () => setTabListId(undefined);
  }, []);

  const choose = (id: string) => {
    showTab(id, { focus: !byKeys.current, person: true });
    if (!onTerminal) {
      goToTerminal();
    }
  };
  const closeFocused = (event: KeyboardEvent) => {
    if (event.key !== "Delete" && event.key !== "Backspace") {
      return;
    }
    const id = (event.target as HTMLElement).closest("[data-tab]")?.getAttribute("data-tab");
    if (id) {
      event.preventDefault();
      askClose(id);
    }
  };
  return (
    <div
      className="flex min-h-0 flex-col"
      onKeyDownCapture={() => {
        byKeys.current = true;
      }}
      onPointerDownCapture={() => {
        byKeys.current = false;
      }}
      onKeyDown={closeFocused}
    >
      <Tabs
        orientation="vertical"
        selectedKey={active}
        onSelectionChange={(key) => choose(String(key))}
        keyboardActivation={onTerminal ? "automatic" : "manual"}
        className="flex min-h-0 flex-col"
      >
        <TabList
          ref={list}
          aria-label={t("terminal.tabs")}
          className="-mx-1 flex min-h-0 flex-col gap-1 overflow-y-auto px-1 py-1 [scrollbar-width:thin]"
        >
          {tabs.map((tab) => {
            const selected = tab.id === active;
            const raised = selected && onTerminal;
            return (
              <Tab
                key={tab.id}
                id={tab.id}
                data-tab={tab.id}
                className={`group relative flex shrink-0 cursor-default items-center rounded-2xl text-start outline-none transition-colors data-[focus-visible]:ring-2 data-[focus-visible]:ring-focus ${
                  raised ? "island" : "hover:bg-surface/60"
                } ${open ? "min-h-14 gap-3 py-2 ps-3.5 pe-1.5" : "size-14 flex-col justify-center gap-1.5 self-center"}`}
              >
                <TabMark status={tabStatus(tab)} word={false} />
                {open ? (
                  <>
                    <span className="flex min-w-0 flex-1 flex-col">
                      <span
                        className={`truncate text-[0.9375rem] tracking-[-0.01em] ${selected ? "font-semibold" : "font-medium text-ink-2"}`}
                      >
                        {tab.label}
                      </span>
                      <TabLine tab={tab} space={spaceOf(hosts, tab.host)} />
                    </span>
                    <span
                      aria-hidden
                      title={t("terminal.tab.closeNamed", { tab: tab.label })}
                      data-close
                      onPointerDown={(event) => event.stopPropagation()}
                      onMouseDown={(event) => event.stopPropagation()}
                      onClick={(event) => {
                        event.stopPropagation();
                        askClose(tab.id);
                      }}
                      className={`flex size-7 shrink-0 items-center justify-center rounded-full text-muted hover:bg-surface-tertiary hover:text-foreground ${
                        raised ? "" : "opacity-0 group-hover:opacity-100"
                      }`}
                    >
                      <X className="size-3.5" />
                    </span>
                  </>
                ) : (
                  <span
                    title={tab.label}
                    className={`max-w-12 truncate font-mono text-[0.6875rem] ${selected ? "font-semibold" : "text-ink-2"}`}
                  >
                    {tab.label}
                  </span>
                )}
              </Tab>
            );
          })}
        </TabList>
      </Tabs>
    </div>
  );
}

/**
 * TerminalSection is the rail's Terminal section, between the places and
 * the foot: Terminal, a link to the workspace with the count while tabs
 * are open; New tab, which opens the switcher from any page; then the open
 * tabs, or "Open terminal" with none; and at 16 tabs, why New tab is
 * disabled, which the folded rail says to a screen reader alone. On the
 * terminal, the way out with the keyboard follows it, in the folded rail
 * for a screen reader alone.
 */
export function TerminalSection({ open, onTerminal }: { open: boolean; onTerminal: boolean }) {
  const t = useT();
  const { tabs } = useWorkspace();
  const hosts = useManagedHosts(tabs.length > 0);
  const full = tabs.length >= maxTabs;
  return (
    <>
      <div
        role="group"
        aria-label={t("terminal.title")}
        className="flex min-h-40 flex-1 flex-col gap-1.5 border-t border-line pt-4"
      >
        <div className={`flex shrink-0 items-center ${open ? "gap-1" : "flex-col gap-1.5"}`}>
          <Link
            href={terminalPath}
            aria-current={onTerminal ? "page" : undefined}
            aria-label={open ? undefined : tabs.length > 0 ? t("terminal.place.count", { count: tabs.length }) : t("terminal.title")}
            className={`relative flex h-10 shrink-0 items-center gap-3 rounded-full text-sm no-underline outline-none transition-colors hover:bg-surface/60 hover:text-foreground data-[focus-visible]:ring-2 data-[focus-visible]:ring-focus ${
              onTerminal ? "font-semibold text-foreground" : "font-medium text-ink-2"
            } ${open ? "min-w-0 flex-1 px-3.5" : "w-11 justify-center self-center"}`}
          >
            <SquareTerminal aria-hidden className="size-[1.125rem] shrink-0" />
            {open && <span className="flex-1">{t("terminal.title")}</span>}
            {tabs.length > 0 && (
              <span
                aria-hidden={!open}
                className={`t-num rounded-full bg-default px-1.5 text-xs font-semibold text-foreground ${open ? "" : "absolute -end-1 -top-1"}`}
              >
                {open ? t("terminal.rail.count", { count: tabs.length, max: maxTabs }) : tabs.length}
              </span>
            )}
          </Link>
          <Button
            variant="ghost"
            isIconOnly
            size="sm"
            isDisabled={full}
            aria-label={t("terminal.tab.newKeys", { keys: chord("K") })}
            onPress={openSwitcher}
            className={open ? "size-9 rounded-full" : "size-11 rounded-full"}
          >
            <Plus aria-hidden className="size-[1.125rem]" />
          </Button>
        </div>
        {tabs.length > 0 ? (
          <RailTabs open={open} onTerminal={onTerminal} hosts={hosts} />
        ) : (
          open && (
            <button
              type="button"
              onClick={openSwitcher}
              className="t-label flex items-center justify-between gap-2 rounded-xl px-3.5 py-2 text-start outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-focus"
            >
              {t("terminal.open")}
              <Kbd>{chord("K")}</Kbd>
            </button>
          )
        )}
        {full && <p className={open ? "t-label px-2" : "sr-only"}>{t("terminal.tab.limit", { max: maxTabs })}</p>}
      </div>
      {onTerminal && tabs.length > 0 && (
        <p className={open ? "t-label shrink-0 px-2.5 text-xs [@media(max-height:640px)]:sr-only" : "sr-only"}>
          {t("terminal.leave")}
        </p>
      )}
    </>
  );
}

/**
 * PhoneTerminalButton starts a phone's bar at the foot, where the
 * terminal's key bar starts: while tabs are open it is filled in the text
 * colour and shows the active tab's name and the count; with none, a
 * raised Terminal. Either goes to the terminal.
 */
export function PhoneTerminalButton() {
  const t = useT();
  const { tabs, active } = useWorkspace();
  const tab = tabs.find((each) => each.id === active);
  return (
    <Link
      href={terminalPath + workspaceQuery(workspace())}
      className={`flex h-12 max-w-[40%] shrink-0 items-center gap-2 rounded-xl ps-3 pe-2.5 no-underline outline-none data-[focus-visible]:ring-2 data-[focus-visible]:ring-focus ${
        tab ? "bg-foreground text-background" : "island text-foreground"
      }`}
    >
      {tab ? (
        <Layers aria-hidden className="size-4 shrink-0 opacity-70" />
      ) : (
        <SquareTerminal aria-hidden className="size-[1.125rem] shrink-0" />
      )}
      <span className="truncate text-[0.9375rem] font-semibold">{tab ? tab.label : t("terminal.title")}</span>
      {tab && (
        <>
          <span aria-hidden className="t-num rounded-full bg-background/20 px-1.5 text-xs font-semibold">
            {tabs.length}
          </span>
          <span className="sr-only">{`, ${t("terminal.place.count", { count: tabs.length })}`}</span>
        </>
      )}
    </Link>
  );
}

/** CloseQuestion asks before a tab whose shell runs is closed, wherever its closing was asked for: it ends the shell everywhere. */
export function CloseQuestion() {
  const t = useT();
  const { tabs, closing } = useWorkspace();
  const tab = tabs.find((each) => each.id === closing);
  return tab ? (
    <ConfirmDialog
      title={t("terminal.close.title", { tab: tab.label })}
      consequence={t("terminal.close.consequence")}
      confirm={t("terminal.tab.close")}
      onConfirm={() => closeTab(tab.id)}
      onCancel={cancelClose}
    />
  ) : null;
}

/**
 * WorkspaceNotices says what the Workspace came to that has no place of
 * its own, on whatever page the frame shows: an open past the limit, the
 * tab shown closed by another page, and a Workspace socket
 * refused for want of a Sign-in, which goes to Sign in and back.
 */
export function WorkspaceNotices() {
  const t = useT();
  const { atLimit, closedElsewhere, socket } = useWorkspace();
  useEffect(() => {
    if (atLimit) {
      notify(t("terminal.tab.limit", { max: maxTabs }));
      saidAtLimit();
    }
  }, [atLimit, t]);
  useEffect(() => {
    if (closedElsewhere !== undefined) {
      notify(t("terminal.closedElsewhere", { tab: closedElsewhere }));
      saidClosedElsewhere();
    }
  }, [closedElsewhere, t]);
  const signIn = socket.status === "refused" && socket.code === CodeSignInRequired;
  useEffect(() => {
    if (signIn && window.location.pathname !== signInPage) {
      navigate(signInPath(window.location.pathname + window.location.search), { replace: true });
    }
  }, [signIn]);
  return null;
}
