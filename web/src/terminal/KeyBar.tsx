import { Keyboard } from "lucide-react";
import type { MouseEvent, PointerEvent, ReactNode } from "react";

import type { MessageKey } from "../i18n/catalogues";
import { useT } from "../shell/i18n";
import { pressKey, runtime, type Sticky, tapModifier, useWorkspace } from "./workspace";

// The key bar (docs/web-design.md, "The terminal", "On a phone"): one row of
// keys a phone's soft keyboard hides, above it, or at the foot of the screen
// while it is closed. A key never takes the focus, so the soft keyboard
// stays as it is.

/** keys are the bar's keys in order: a KeyboardEvent key, what the key shows, and its accessible name. */
const keys: readonly { key: string; face: string; name: MessageKey }[] = [
  { key: "Escape", face: "Esc", name: "terminal.key.escape" },
  { key: "Tab", face: "Tab", name: "terminal.key.tab" },
  { key: "ctrl", face: "Ctrl", name: "terminal.key.control" },
  { key: "alt", face: "Alt", name: "terminal.key.alt" },
  { key: "ArrowLeft", face: "←", name: "terminal.key.left" },
  { key: "ArrowUp", face: "↑", name: "terminal.key.up" },
  { key: "ArrowDown", face: "↓", name: "terminal.key.down" },
  { key: "ArrowRight", face: "→", name: "terminal.key.right" },
  { key: "|", face: "|", name: "terminal.key.pipe" },
  { key: "/", face: "/", name: "terminal.key.slash" },
  { key: "-", face: "-", name: "terminal.key.dash" },
  { key: "~", face: "~", name: "terminal.key.tilde" },
  { key: "Home", face: "Home", name: "terminal.key.home" },
  { key: "End", face: "End", name: "terminal.key.end" },
  { key: "PageUp", face: "PgUp", name: "terminal.key.pageUp" },
  { key: "PageDown", face: "PgDn", name: "terminal.key.pageDown" },
];

/** fills are a key's look: raised, a modifier applied to the next key in the accent, and one held in the text colour. */
const fills: Record<Sticky, string> = {
  off: "island text-foreground",
  once: "bg-accent text-accent-foreground",
  held: "bg-foreground text-background",
};

/** keepFocus stops a press from moving the focus, so the soft keyboard stays open or closed as it was. */
export const keepFocus = {
  onPointerDown: (event: PointerEvent) => event.preventDefault(),
  onMouseDown: (event: MouseEvent) => event.preventDefault(),
};

/** Key is one key of the bar: a button that never takes the focus. */
function Key(props: { label: string; sticky?: Sticky; onPress: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      tabIndex={-1}
      aria-label={props.label}
      aria-pressed={props.sticky === undefined ? undefined : props.sticky !== "off"}
      {...keepFocus}
      onClick={props.onPress}
      className={`key t-code flex h-11 min-w-11 shrink-0 items-center justify-center rounded-xl px-3 text-[0.9375rem] font-medium select-none active:translate-y-px active:opacity-80 ${fills[props.sticky ?? "off"]}`}
    >
      {props.children}
    </button>
  );
}

/**
 * KeyBar is the row of keys acting on a tab, which scrolls sideways when
 * the keys do not fit, fading at its end. Each key is pressed through the
 * tab's emulator (pressKey); Control and Alt are sticky (tapModifier); the
 * keyboard key opens or closes the soft keyboard. The row is one Tab stop,
 * so a keyboard can scroll it.
 */
export function KeyBar({ tab }: { tab: number }) {
  const t = useT();
  const ws = useWorkspace();
  const toggleKeyboard = () => {
    const emulator = runtime(tab).emulator;
    if (emulator?.hasFocus()) {
      emulator.blur();
    } else {
      emulator?.focus();
    }
  };
  return (
    <div
      role="toolbar"
      tabIndex={0}
      aria-label={t("terminal.keys")}
      {...keepFocus}
      className="end-fade flex min-w-0 flex-1 gap-1.5 overflow-x-auto rounded-xl py-0.5 pe-6 outline-none focus-visible:ring-2 focus-visible:ring-focus [scrollbar-width:none]"
    >
      {keys.map(({ key, face, name }) =>
        key === "ctrl" || key === "alt" ? (
          <Key key={key} label={t(name)} sticky={ws[key]} onPress={() => tapModifier(key)}>
            {face}
          </Key>
        ) : (
          <Key key={key} label={t(name)} onPress={() => pressKey(tab, key)}>
            {face}
          </Key>
        ),
      )}
      <Key label={t("terminal.key.keyboard")} onPress={toggleKeyboard}>
        <Keyboard aria-hidden className="size-[1.125rem]" />
      </Key>
    </div>
  );
}
