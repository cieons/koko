import { Button } from "@heroui/react";
import { Layers, Plus, SquareTerminal, X } from "lucide-react";
import { type PointerEvent, useRef } from "react";
import { Link } from "react-aria-components";

import type { Host } from "../api/host";
import type { MessageKey } from "../i18n/catalogues";
import { useT } from "../shell/i18n";
import type { Route } from "../shell/router";
import { SidePanel } from "../ui/SidePanel";
import { keepFocus } from "./KeyBar";
import { spaceOf, TabLine } from "./RailSection";
import { stateWord, TabMark } from "./TabMark";
import { askClose, maxTabs, setSwitcher, showTab, stepTab, tabStatus, useWorkspace } from "./workspace";

// The phone's way among the tabs (docs/web-design.md, "The terminal", "On a
// phone"): the tab button that starts the key bar's row, and the sheet of
// tabs it opens.

/** swipe is how far, in CSS pixels, a pointer moves across the tab button to show another tab. */
export const swipe = 36;

/**
 * TabButton starts the key bar's row: the active tab's name and the count
 * of tabs, filled in the text colour, or, with none open, a raised
 * Terminal. A tap opens the sheet of tabs; a swipe across it, more than 36
 * pixels, shows the next tab (leftwards) or the one before (rightwards),
 * without opening the sheet. A press moves no focus, so the soft keyboard
 * stays as it was until the sheet opens.
 */
export function TabButton({ onPress }: { onPress: () => void }) {
  const t = useT();
  const { tabs, active } = useWorkspace();
  const tab = tabs.find((each) => each.id === active);
  const start = useRef<number | undefined>(undefined);
  const swiped = useRef(false);
  const down = (event: PointerEvent) => {
    keepFocus.onPointerDown(event);
    start.current = event.clientX;
    swiped.current = false;
  };
  const up = (event: PointerEvent) => {
    if (start.current === undefined) {
      return;
    }
    const moved = event.clientX - start.current;
    start.current = undefined;
    if (Math.abs(moved) > swipe && tabs.length > 1) {
      swiped.current = true;
      stepTab(moved < 0 ? 1 : -1);
    }
  };
  return (
    <button
      type="button"
      aria-haspopup="dialog"
      aria-label={t("terminal.tab.button", { tab: tab?.label ?? t("terminal.title"), count: tabs.length })}
      onPointerDown={down}
      onPointerUp={up}
      onPointerCancel={() => {
        start.current = undefined;
      }}
      onMouseDown={keepFocus.onMouseDown}
      onClick={() => {
        if (!swiped.current) {
          onPress();
        }
        swiped.current = false;
      }}
      className={`tab-key flex h-11 max-w-[42%] shrink-0 touch-pan-y items-center gap-2 rounded-xl ps-3 pe-2 outline-none select-none focus-visible:ring-2 focus-visible:ring-focus active:opacity-80 ${
        tab ? "bg-foreground text-background" : "island text-foreground"
      }`}
    >
      {tab ? (
        <Layers aria-hidden className="size-4 shrink-0 opacity-70" />
      ) : (
        <SquareTerminal aria-hidden className="size-[1.125rem] shrink-0" />
      )}
      <span className="truncate text-[0.9375rem] font-semibold">{tab?.label ?? t("terminal.title")}</span>
      {tab && <span className="t-num rounded-full bg-background/20 px-1.5 text-xs font-semibold">{tabs.length}</span>}
    </button>
  );
}

/**
 * TabSheet is the phone's sheet of tabs, a side panel from the bottom: the
 * places first, as tiles, so one thumb gets back to them; then the tabs,
 * each shown by a tap and closed by its close button, asking first while
 * its shell runs; then New tab, which opens the switcher, or at 16 tabs
 * why there is none.
 */
export function TabSheet(props: { places: readonly Route[]; hosts: readonly Host[] | undefined; onClose: () => void }) {
  const t = useT();
  const { tabs, active } = useWorkspace();
  const { places, hosts, onClose } = props;
  return (
    <SidePanel title={t("terminal.tab.sheet")} onClose={onClose}>
      {places.length > 0 && (
        <nav aria-label={t("shell.nav")} className="grid grid-cols-3 gap-2">
          {places.map((place) => {
            const Icon = place.icon;
            return (
              <Link
                key={place.path}
                href={place.path}
                onPress={onClose}
                className="island flex min-h-16 flex-col items-center justify-center gap-1 rounded-2xl text-sm font-medium text-foreground no-underline outline-none data-[focus-visible]:ring-2 data-[focus-visible]:ring-focus"
              >
                {Icon && <Icon aria-hidden className="size-[1.125rem] text-ink-2" />}
                <span className="max-w-full truncate px-1">{t(place.nav as MessageKey)}</span>
              </Link>
            );
          })}
        </nav>
      )}
      {tabs.length > 0 && (
        <ul aria-label={t("terminal.tabs")} className="flex flex-col gap-1">
          {tabs.map((tab) => (
            <li key={tab.id} className={`flex items-center rounded-2xl ${tab.id === active ? "bg-default" : ""}`}>
              <button
                type="button"
                aria-current={tab.id === active ? "true" : undefined}
                onClick={() => {
                  showTab(tab.id, { focus: true, person: true });
                  onClose();
                }}
                className="flex min-h-14 min-w-0 flex-1 items-center gap-3 rounded-2xl py-2 ps-4 text-start outline-none focus-visible:ring-2 focus-visible:ring-focus"
              >
                {/* The row is named by the tab first, then its state. */}
                <span aria-hidden className="flex">
                  <TabMark status={tabStatus(tab)} word={false} />
                </span>
                <span className="flex min-w-0 flex-col">
                  <span className="t-name truncate">{tab.label}</span>
                  <span className="sr-only">{`, ${stateWord(t, tabStatus(tab))}`}</span>
                  <TabLine tab={tab} space={spaceOf(hosts, tab.host)} />
                </span>
              </button>
              <Button
                variant="ghost"
                isIconOnly
                className="me-1 rounded-full"
                aria-label={t("terminal.tab.closeNamed", { tab: tab.label })}
                onPress={() => askClose(tab.id)}
              >
                <X aria-hidden className="size-[1.125rem]" />
              </Button>
            </li>
          ))}
        </ul>
      )}
      {tabs.length >= maxTabs ? (
        <p className="t-label">{t("terminal.tab.limit", { max: maxTabs })}</p>
      ) : (
        <Button
          variant="primary"
          className="w-full"
          onPress={() => {
            onClose();
            setSwitcher(true);
          }}
        >
          <Plus aria-hidden className="size-4" />
          {t("terminal.tab.new")}
        </Button>
      )}
    </SidePanel>
  );
}
