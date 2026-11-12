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

/** EmulatorHandle is what the page calls on an emulator. */
export interface EmulatorHandle {
  /** write gives the emulator the shell's output. */
  write(bytes: Uint8Array): void;
  focus(): void;
  /** fit sizes the grid to its element again; onResize follows when the grid changes. */
  fit(): void;
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
  /** onReady says the emulator has loaded and laid out its grid of cols by rows. */
  onReady: (cols: number, rows: number) => void;
  /** onInput gives the bytes typed, pasted or pressed. */
  onInput: (bytes: Uint8Array) => void;
  /** onResize gives the grid's new size. */
  onResize: (cols: number, rows: number) => void;
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

/** KeyEncoder is wterm's InputHandler as far as pressKey uses it. */
interface KeyEncoder {
  encodeKey?: (key: { key: string; ctrlKey: boolean; altKey: boolean; shiftKey: boolean; metaKey: boolean }) => string | null;
}

const encoder = new TextEncoder();

/**
 * Emulator is one terminal: wterm's <Terminal> on a ghostty core of its own.
 * The page compiles the core's WebAssembly once, and each terminal
 * instantiates it, with 5,000 lines of scrollback and Kitty graphics off.
 * It stays mounted for as long as its tab is open: unmounting it ends its
 * screen and frees its core.
 */
export function Emulator(props: EmulatorProps) {
  const { ref, fontSize, paused, announce, label } = props;
  const [core, setCore] = useState<GhosttyCore>();
  const terminal = useRef<TerminalHandle>(null);
  const callbacks = useRef(props);
  callbacks.current = props;

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
    return {
      write: (bytes) => terminal.current?.write(bytes),
      focus: () => terminal.current?.focus(),
      fit: () => instance()?.fit(),
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

  // A new text size changes the cells, so the grid is fitted again.
  useEffect(() => {
    terminal.current?.instance?.fit();
  }, [fontSize]);

  if (!core) {
    return null;
  }
  return (
    <Terminal
      ref={terminal}
      core={core}
      autoResize
      renderingPaused={paused}
      announceOutput={announce}
      className={`koko-term koko-term-${fontSize} min-w-0 flex-1`}
      aria-label={label}
      onReady={(wt) => callbacks.current.onReady(wt.cols, wt.rows)}
      onData={(data) => callbacks.current.onInput(encoder.encode(data))}
      onBinary={(bytes) => callbacks.current.onInput(bytes)}
      onResize={(cols, rows) => callbacks.current.onResize(cols, rows)}
      onClipboardWrite={(text) => callbacks.current.onClipboardWrite(text)}
      onWorkingDirectory={(uri) => callbacks.current.onWorkingDirectory(uri)}
      onTitle={(title) => callbacks.current.onTitle(title)}
      onError={(error) => callbacks.current.onError(error)}
    />
  );
}
