import { Theme } from "@astryxdesign/core/theme";
import { neutralTheme } from "@astryxdesign/theme-neutral/built";
import { createContext, type ReactNode, useContext, useState } from "react";

// Light and dark: the web UI follows the system's mode until a choice of
// Light or Dark overrides it, which the browser then remembers, as it
// remembers the language. Astryx's Theme applies the mode, writing it to
// <html data-theme> (absent for System), and its tokens follow.

/** modes are the choices of colour mode, System the default. */
export const modes = ["light", "dark", "system"] as const;

/** Mode is a choice of colour mode: Light, Dark, or the system's. */
export type Mode = (typeof modes)[number];

/** storageKey is where the browser remembers the mode chosen. */
const storageKey = "koko.theme";

/** storedMode is the mode the browser remembers, System when none or unreadable. */
export function storedMode(): Mode {
  try {
    const stored = localStorage.getItem(storageKey);
    return modes.find((m) => m === stored) ?? "system";
  } catch {
    return "system"; // storage the browser refuses remembers nothing
  }
}

/**
 * applyStoredMode writes the stored mode to <html data-theme> as Theme
 * does, for main.tsx to call before React's first render, which runs in a
 * later task: so no frame between shows the system's mode for a stored
 * Light or Dark. index.html may run no inline script that could do it
 * sooner.
 */
export function applyStoredMode(): void {
  const mode = storedMode();
  if (mode === "system") {
    document.documentElement.removeAttribute("data-theme");
  } else {
    document.documentElement.setAttribute("data-theme", mode);
  }
}

const ModeContext = createContext<[Mode, (mode: Mode) => void] | undefined>(undefined);

/** ModeProvider holds the mode, and themes everything inside it in Astryx's Neutral theme. */
export function ModeProvider({ children }: { children: ReactNode }) {
  const [mode, setChosen] = useState(storedMode);
  const setMode = (next: Mode) => {
    try {
      localStorage.setItem(storageKey, next);
    } catch {
      // The choice still applies to this page when the browser cannot store it.
    }
    setChosen(next);
  };
  return (
    <ModeContext value={[mode, setMode]}>
      <Theme theme={neutralTheme} mode={mode}>
        {children}
      </Theme>
    </ModeContext>
  );
}

/** useMode is the mode chosen, and a function that chooses another, which the browser remembers. */
export function useMode(): [Mode, (mode: Mode) => void] {
  const state = useContext(ModeContext);
  if (!state) {
    throw new Error("a component that reads the mode is outside the shell's ModeProvider");
  }
  return state;
}
