import { useEffect } from "react";

import { navigate } from "../shell/router";
import { setSwitcher, stepTab, terminalPath, workspace, workspaceQuery } from "./workspace";

// The chords that reach the terminal workspace (docs/web-design.md, "The
// keys that reach the workspace"): taken on the window before the terminal
// sees them, so the shell never receives them. The browsers bind none.

/** mac tells whether the keyboard is a Mac's, whose chords are written as glyphs. */
const mac = /Mac|iPhone|iPad/.test(navigator.platform);

/** chord writes Control and Shift with a key as the platform writes them: "Ctrl+Shift+K", or "⌃⇧K" on a Mac. */
export function chord(key: string): string {
  return mac ? `⌃⇧${key}` : `Ctrl+Shift+${key}`;
}

/** goToTerminal goes to the workspace, with its tabs in the URL, from whatever page. */
export function goToTerminal(): void {
  if (window.location.pathname !== terminalPath) {
    navigate(terminalPath + workspaceQuery(workspace()));
  }
}

/**
 * openSwitcher opens the switcher on the terminal, going there first from
 * another page. Then it opens once the visit has settled, so that the
 * frame's moving the focus to the new page, if it does, goes before the
 * switcher takes it.
 */
export function openSwitcher(): void {
  if (window.location.pathname === terminalPath) {
    setSwitcher(true);
    return;
  }
  goToTerminal();
  setTimeout(() => setSwitcher(true), 0);
}

/**
 * useTerminalKeys takes the workspace's chords on every page of the frame:
 * Control+Shift+K opens the switcher, going to the terminal first; on the
 * terminal, Control+Shift+← and → show the tab before and after, unless
 * the switcher is open, whose field takes them to select words.
 */
export function useTerminalKeys(onTerminal: boolean): void {
  useEffect(() => {
    const take = (event: KeyboardEvent) => {
      if (!event.ctrlKey || !event.shiftKey || event.altKey || event.metaKey) {
        return;
      }
      const arrows = onTerminal && !workspace().switcher;
      const action =
        event.code === "KeyK"
          ? openSwitcher
          : arrows && event.key === "ArrowLeft"
            ? () => stepTab(-1)
            : arrows && event.key === "ArrowRight"
              ? () => stepTab(1)
              : undefined;
      if (action) {
        event.preventDefault();
        event.stopPropagation();
        action();
      }
    };
    window.addEventListener("keydown", take, { capture: true });
    return () => window.removeEventListener("keydown", take, { capture: true });
  }, [onTerminal]);
}
