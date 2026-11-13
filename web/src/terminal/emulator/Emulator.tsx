import "@wterm/dom/src/terminal.css";
import "./emulator.css";

import { GhosttyCore } from "@wterm/ghostty";
import wasmPath from "@wterm/ghostty/ghostty-vt.wasm?url";
import { Terminal, type TerminalHandle, type WTerm } from "@wterm/react";
import { type Ref, useEffect, useImperativeHandle, useRef, useState } from "react";

// The emulator: wterm on its ghostty core (ADR 0054). This directory alone
// imports @wterm/* (static.test.ts); the page sees the Emulator below and
// nothing of wterm, so replacing wterm or its core changes this directory
// alone. Its colours are the theme's --term-* tokens (emulator.css), which
// follow Light, Dark or System, and wterm reads them back when a program
// asks for the terminal's colours (OSC 10 and 11).

/** Modifiers are the modifier keys held with a key the page presses. */
export interface Modifiers {
  ctrl: boolean;
  alt: boolean;
}

/** Metrics are a terminal's cell and the padding around its grid, in CSS pixels. */
export interface Metrics {
  cellWidth: number;
  cellHeight: number;
  /** padX and padY are the padding on both sides together, across and down. */
  padX: number;
  padY: number;
}

/** EmulatorHandle is what the page calls on an emulator. */
export interface EmulatorHandle {
  /**
   * write gives the emulator the shell's output. What wterm answers to a
   * query in it goes nowhere: the koko server answers the shell's queries.
   */
  write(bytes: Uint8Array): void;
  /** reset clears the screen and its history, and sets the grid to cols by rows, as a snapshot is drawn on. */
  reset(cols: number, rows: number): void;
  /** metrics is the size of a cell and the grid's padding, once the emulator has loaded and laid out. */
  metrics(): Metrics | null;
  focus(): void;
  /** blur takes the focus off the terminal, which closes a phone's soft keyboard. */
  blur(): void;
  /** hasFocus tells whether the terminal has the focus, as it has while a phone's soft keyboard is open. */
  hasFocus(): boolean;
  /**
   * pressKey sends a key as the terminal's current modes ask, such as
   * application cursor keys, through onInput. name is a KeyboardEvent key:
   * "Escape", "ArrowUp", "a".
   */
  pressKey(name: string, modifiers: Modifiers): void;
  /** selection is the text selected in the terminal, or "" when none is. */
  selection(): string;
}

/** fontSizes are the text sizes the terminal offers, in pixels, each with its row height in emulator.css. */
export const fontSizes = [12, 13, 14, 16, 18] as const;

/** FontSize is one of the text sizes. */
export type FontSize = (typeof fontSizes)[number];

/** EmulatorProps are what the page gives an emulator. */
export interface EmulatorProps {
  ref?: Ref<EmulatorHandle>;
  /** label names the terminal for a screen reader. */
  label: string;
  fontSize: FontSize;
  /** paused suspends painting, for a terminal not shown; its output is still taken in. */
  paused: boolean;
  /** announce has output announced politely to a screen reader while the terminal has the focus. */
  announce: boolean;
  /** onReady says the emulator has loaded and laid out its grid. */
  onReady: () => void;
  /** onInput gives the bytes typed, pasted or pressed, and wterm's focus reports; never its replies to queries. */
  onInput: (bytes: Uint8Array) => void;
  /** onClipboardWrite is a program's request to copy text (OSC 52), which the page asks about first. */
  onClipboardWrite: (text: string) => void;
  /** onWorkingDirectory is the directory the shell reported (OSC 7), as its file: URI. */
  onWorkingDirectory: (uri: string) => void;
  onTitle: (title: string) => void;
  /** onError says the emulator could not load, as when its WebAssembly does not. */
  onError: (error: unknown) => void;
}

/** scrollbackLines is the history a terminal keeps. */
const scrollbackLines = 5000;

/** clearAll is RIS, then ED 3: the screen, its modes and its history cleared, for a snapshot to draw on. */
const clearAll = "\x1bc\x1b[3J";

/** Measured is wterm's own measure of a cell, which WTerm 0.5.10 keeps private: it measures afresh, as after a new text size. */
interface Measured {
  _measureCharSize?: () => { charWidth: number; rowHeight: number } | null;
}

/** KeyEncoder is wterm's InputHandler as far as pressKey uses it. */
interface KeyEncoder {
  encodeKey?: (key: { key: string; ctrlKey: boolean; altKey: boolean; shiftKey: boolean; metaKey: boolean }) => string | null;
}

const encoder = new TextEncoder();

/**
 * Emulator is one terminal: wterm's <Terminal> on a ghostty core of its own.
 * The page compiles the core's WebAssembly once, and each terminal
 * instantiates it, with 5,000 lines of scrollback and Kitty graphics off.
 * Its grid is set by reset alone, never fitted to its element: the pty's
 * size is the koko server's. It stays mounted for as long as its tab is
 * open: unmounting it ends its screen and frees its core.
 *
 * wterm hands its replies to the shell's queries to onData, as typed keys:
 * in 0.5.10 within write, the core's through its drain and the window's
 * size after it (wterm.test.ts). The emulator drops what wterm gives while
 * write runs, since the koko server answers the queries once, however many
 * pages are attached.
 */
export function Emulator(props: EmulatorProps) {
  const { ref, fontSize, paused, announce, label } = props;
  const [core, setCore] = useState<GhosttyCore>();
  const terminal = useRef<TerminalHandle>(null);
  const callbacks = useRef(props);
  callbacks.current = props;
  /** writing says write is running, whose replies from wterm go nowhere. */
  const writing = useRef(false);

  useEffect(() => {
    let gone = false;
    let loaded: GhosttyCore | undefined;
    GhosttyCore.load({ wasmPath, scrollbackLines, imageStorageLimit: 0 }).then(
      (each) => {
        loaded = each;
        if (gone) {
          each.dispose();
        } else {
          setCore(each);
        }
      },
      (error: unknown) => gone || callbacks.current.onError(error),
    );
    return () => {
      gone = true;
      loaded?.dispose();
    };
  }, []);

  useImperativeHandle(ref, () => {
    const instance = (): WTerm | null => terminal.current?.instance ?? null;
    /** written writes into wterm, dropping what it gives meanwhile. */
    const written = (data: string | Uint8Array) => {
      writing.current = true;
      try {
        terminal.current?.write(data);
      } finally {
        writing.current = false;
      }
    };
    return {
      write: written,
      reset: (cols, rows) => {
        const wt = instance();
        if (wt && (wt.cols !== cols || wt.rows !== rows)) {
          wt.resize(cols, rows);
        }
        written(clearAll);
      },
      metrics: () => {
        const wt = instance();
        const measured = (wt as unknown as Measured | null)?._measureCharSize?.();
        if (!wt || !measured || measured.charWidth <= 0 || measured.rowHeight <= 0) {
          return null;
        }
        const element = wt.element;
        const style = getComputedStyle(element);
        const px = (value: string) => Number.parseFloat(value) || 0;
        const bordersX = px(style.borderLeftWidth) + px(style.borderRightWidth);
        // A terminal with history scrolls, and a scrollbar that takes room is counted as padding, as wterm's own fit does.
        const scrollbar = Math.max(0, element.offsetWidth - element.clientWidth - bordersX);
        return {
          cellWidth: measured.charWidth,
          cellHeight: measured.rowHeight,
          padX: px(style.paddingLeft) + px(style.paddingRight) + bordersX + scrollbar,
          padY: px(style.paddingTop) + px(style.paddingBottom) + px(style.borderTopWidth) + px(style.borderBottomWidth),
        };
      },
      focus: () => terminal.current?.focus(),
      blur: () => {
        const focused = document.activeElement;
        if (focused instanceof HTMLElement && instance()?.element.contains(focused)) {
          focused.blur();
        }
      },
      hasFocus: () => instance()?.element.contains(document.activeElement) === true,
      selection: () => instance()?.getSelectionText() ?? "",
      pressKey: (name, modifiers) => {
        const wt = instance();
        if (!wt) {
          return;
        }
        const key = { key: name, ctrlKey: modifiers.ctrl, altKey: modifiers.alt, shiftKey: false, metaKey: false };
        // wterm 0.5.10 encodes a key as the modes ask in InputHandler.encodeKey,
        // which WTerm keeps in a private field; without it, a KeyboardEvent
        // dispatched to its input target is encoded the same way.
        const input = (wt as unknown as { input?: KeyEncoder }).input;
        if (typeof input?.encodeKey === "function") {
          const sequence = input.encodeKey(key);
          if (sequence !== null) {
            callbacks.current.onInput(encoder.encode(sequence));
          }
          return;
        }
        wt.element
          .querySelector("textarea")
          ?.dispatchEvent(new KeyboardEvent("keydown", { ...key, bubbles: true, cancelable: true }));
      },
    };
  }, []);

  if (!core) {
    return null;
  }
  return (
    <Terminal
      ref={terminal}
      core={core}
      autoResize={false}
      renderingPaused={paused}
      announceOutput={announce}
      className={`koko-term koko-term-${fontSize} size-full`}
      aria-label={label}
      onReady={(wt) => {
        // wterm fixes the element's height to the first grid's; the page sizes it.
        wt.element.style.height = "";
        callbacks.current.onReady();
      }}
      onData={(data) => writing.current || callbacks.current.onInput(encoder.encode(data))}
      onBinary={(bytes) => writing.current || callbacks.current.onInput(bytes)}
      onClipboardWrite={(text) => callbacks.current.onClipboardWrite(text)}
      onWorkingDirectory={(uri) => callbacks.current.onWorkingDirectory(uri)}
      onTitle={(title) => callbacks.current.onTitle(title)}
      onError={(error) => callbacks.current.onError(error)}
    />
  );
}
