import { Button } from "@heroui/react";
import { PanelLeftClose, PanelLeftOpen } from "lucide-react";
import { type RefObject, useCallback, useEffect, useRef, useState } from "react";
import { Link } from "react-aria-components";

import type { MessageKey } from "../i18n/catalogues";
import { PhoneTerminalButton, TerminalSection } from "../terminal/RailSection";
import { terminalPath } from "../terminal/workspace";
import { useMedia, usePhone } from "../ui/phone";
import type { Caller } from "./caller";
import { AccountPanel, LanguageChoice, ModeChoice } from "./controls";
import { useT } from "./i18n";
import { type Route, useLocation } from "./router";

// The frame's navigation (docs/web-design.md, "The frame"). On a desktop,
// one rail down the start edge: the wordmark and the fold button, the
// places, and at its foot the account, Language and Appearance. It folds to
// icons, and under 1280 pixels it is folded and opens over the page for a
// moment. On a phone it gives way to a top row and a bar of places at the
// foot. The terminal's section goes between the places and the foot, outside
// the places' navigation, and its button first in the phone's bar.

/** wideQuery is a window wide enough for the rail to stand open beside the page: 1280 pixels. */
const wideQuery = "(width >= 80rem)";

/** foldedKey is where the browser keeps that the rail was folded on a wide window. */
const foldedKey = "koko.rail";

/**
 * RailState is how the rail shows: open beside the page, folded to icons
 * beside it, or open over the page, which it leaves where it was.
 */
export type RailState = "open" | "folded" | "over";

/** RailControl is the rail's state, and what changes it. */
export interface RailControl {
  state: RailState;
  /** toggle folds or opens the rail: for good on a wide window, over the page on a narrower one. */
  toggle: () => void;
  /** close ends the rail over the page, as Escape and a click beside it do, and gives the focus back to the fold button. */
  close: () => void;
  /** dismiss ends the rail over the page and leaves the focus to the caller, as the skip link moves it to the page. */
  dismiss: () => void;
  foldButton: RefObject<HTMLButtonElement | null>;
}

/** storedFolded tells whether the browser keeps the rail folded; storage the browser refuses remembers nothing. */
function storedFolded(): boolean {
  try {
    return localStorage.getItem(foldedKey) === "folded";
  } catch {
    return false;
  }
}

/** storeFolded keeps the choice where the browser can, and the choice still holds on this page where it cannot. */
function storeFolded(folded: boolean): void {
  try {
    if (folded) {
      localStorage.setItem(foldedKey, "folded");
    } else {
      localStorage.removeItem(foldedKey);
    }
  } catch {
    // Storage the browser refuses remembers nothing.
  }
}

/**
 * useRail is the rail's state. From 1280 pixels wide it is open unless the
 * person folded it, which the browser keeps; under that it is folded, and
 * the fold button opens it over the page until a place is chosen (any
 * visit), Escape, or a click beside it, each giving the focus back to the
 * fold button.
 */
export function useRail(): RailControl {
  const wide = useMedia(wideQuery);
  const phone = usePhone();
  const [folded, setFolded] = useState(storedFolded);
  const [over, setOver] = useState(false);
  const { visit } = useLocation();
  const foldButton = useRef<HTMLButtonElement>(null);

  const dismiss = useCallback(() => setOver(false), []);
  const close = useCallback(() => {
    setOver(false);
    foldButton.current?.focus();
  }, []);
  // A visit, as a place chosen, closes the rail over the page as Escape does, the focus back on the
  // fold button. It runs on a visit alone, not as the rail opens, and reads whether the rail lay
  // over the page in the render the visit brought.
  useEffect(() => {
    if (over) {
      close();
    }
  }, [visit]);
  // The window crossing an edge ends it too, leaving the focus where it is.
  useEffect(() => setOver(false), [wide, phone]);
  useEffect(() => {
    if (!over) {
      return;
    }
    // A menu or panel open in the rail takes its own Escape first, and stops it there.
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        close();
      }
    };
    window.addEventListener("keydown", escape);
    return () => window.removeEventListener("keydown", escape);
  }, [over, close]);

  const toggle = () => {
    if (!wide) {
      setOver((was) => !was);
      return;
    }
    storeFolded(!folded);
    setFolded(!folded);
  };
  const state: RailState = wide ? (folded ? "folded" : "open") : over ? "over" : "folded";
  return { state, toggle, close, dismiss, foldButton };
}

/** inPlace tells whether a path is the place's or beneath it, as a Host's page is beneath Hosts. */
function inPlace(pathname: string, place: Route): boolean {
  return pathname === place.path || pathname.startsWith(`${place.path}/`);
}

/** Wordmark is koko's name, a link to /, which goes to the first place. */
function Wordmark({ className = "" }: { className?: string }) {
  const t = useT();
  return (
    <Link
      href="/"
      className={`rounded-md text-xl leading-none font-semibold tracking-[-0.04em] text-foreground no-underline outline-none select-none data-[focus-visible]:ring-2 data-[focus-visible]:ring-focus ${className}`}
    >
      {t("shell.brand")}
    </Link>
  );
}

/**
 * Rail is the frame on a desktop: the banner down the start edge, holding
 * the places the caller may see, the one the URL is in raised and marked
 * (aria-current), the terminal's section once user.current has answered,
 * and at its foot the account, Language and Appearance. Open it names each place beside its
 * icon; folded it shows the icons alone, each named by its place. Over the
 * page it lies on the backdrop, which a click closes.
 */
export function Rail({ places, caller, rail }: { places: readonly Route[]; caller: Caller | undefined; rail: RailControl }) {
  const t = useT();
  const { pathname } = useLocation();
  const open = rail.state !== "folded";
  return (
    <>
      {rail.state === "over" && <div aria-hidden className="fixed inset-0 z-[45] bg-[var(--backdrop)]" onClick={rail.close} />}
      <header
        className={`rail fixed inset-y-0 start-0 flex flex-col gap-5 overflow-y-auto py-3 [scrollbar-width:none] ${
          open ? "w-[var(--rail-open)] px-3" : "w-[var(--rail-folded)] px-2"
        } ${rail.state === "over" ? "z-50 rounded-e-[1.5rem] bg-overlay shadow-overlay" : "z-30 bg-background"}`}
      >
        <div className={`flex shrink-0 items-center ${open ? "h-10 justify-between ps-2.5" : "flex-col gap-2 pt-1"}`}>
          <Wordmark />
          <Button
            ref={rail.foldButton}
            variant="ghost"
            isIconOnly
            size="sm"
            className="rounded-full text-muted"
            aria-label={t(open ? "shell.rail.collapse" : "shell.rail.expand")}
            aria-expanded={open}
            onPress={rail.toggle}
          >
            {open ? <PanelLeftClose aria-hidden className="size-4" /> : <PanelLeftOpen aria-hidden className="size-4" />}
          </Button>
        </div>
        {places.length > 0 && (
          <nav aria-label={t("shell.nav")} className="flex shrink-0 flex-col gap-1">
            {places.map((r) => {
              const Icon = r.icon;
              const current = inPlace(pathname, r);
              const name = t(r.nav as MessageKey);
              return (
                <Link
                  key={r.path}
                  href={r.path}
                  aria-label={open || !Icon ? undefined : name}
                  aria-current={current ? "page" : undefined}
                  className={`place flex shrink-0 items-center gap-3 rounded-full text-sm font-medium no-underline outline-none transition-colors data-[focus-visible]:ring-2 data-[focus-visible]:ring-focus ${
                    current ? "" : "text-ink-2 hover:bg-surface/60 hover:text-foreground"
                  } ${open ? "h-10 px-3.5" : "size-11 justify-center self-center"}`}
                >
                  {Icon && <Icon aria-hidden className="size-[1.125rem] shrink-0" />}
                  {(open || !Icon) && name}
                </Link>
              );
            })}
          </nav>
        )}
        {caller && <TerminalSection open={open} onTerminal={pathname === terminalPath} />}
        <div
          className={`mt-auto flex shrink-0 items-center gap-0.5 border-t border-line pt-3 ${open ? "" : "flex-col"}`}
        >
          {caller && <AccountPanel caller={caller} full={open} placement="top start" />}
          <LanguageChoice placement="top start" />
          <ModeChoice placement="top start" />
        </div>
      </header>
    </>
  );
}

/**
 * PhoneTop is the frame's top row on a phone, sticky: the wordmark, then
 * Language, Appearance and, once user.current has answered, the account,
 * each as an icon.
 */
export function PhoneTop({ caller }: { caller: Caller | undefined }) {
  return (
    <header className="sticky top-0 z-30 box-content flex h-14 items-center gap-1 border-b border-line bg-background/85 ps-4 pe-2 pt-[env(safe-area-inset-top)] backdrop-blur-xl">
      <Wordmark className="me-auto" />
      <LanguageChoice placement="bottom end" />
      <ModeChoice placement="bottom end" />
      {caller && <AccountPanel caller={caller} full={false} placement="bottom end" />}
    </header>
  );
}

/**
 * PhoneBar is the bar at the foot of a phone's frame: the terminal's
 * button first, then each place the caller may see as its icon over its
 * name, the one the URL is in raised and marked (aria-current), above the
 * safe area.
 */
export function PhoneBar({ places }: { places: readonly Route[] }) {
  const t = useT();
  const { pathname } = useLocation();
  return (
    <nav
      aria-label={t("shell.nav")}
      className="fixed inset-x-0 bottom-0 z-30 border-t border-line bg-background/90 px-2 pt-1.5 pb-[max(0.375rem,env(safe-area-inset-bottom))] backdrop-blur-xl"
    >
      <div className="mx-auto flex max-w-md items-stretch gap-1.5">
        <PhoneTerminalButton />
        {places.map((r) => {
          const Icon = r.icon;
          const current = inPlace(pathname, r);
          return (
            <Link
              key={r.path}
              href={r.path}
              aria-current={current ? "page" : undefined}
              className={`place flex h-12 min-w-0 flex-1 flex-col items-center justify-center gap-0.5 rounded-xl text-xs font-medium no-underline outline-none data-[focus-visible]:ring-2 data-[focus-visible]:ring-focus ${
                current ? "" : "text-muted"
              }`}
            >
              {Icon && <Icon aria-hidden className="size-[1.125rem]" />}
              <span className="max-w-full truncate px-1">{t(r.nav as MessageKey)}</span>
            </Link>
          );
        })}
      </div>
    </nav>
  );
}
