import { createContext, type ReactNode, useContext, useEffect, useState } from "react";

// Light and dark: the web UI follows the system's mode until a choice of
// Light or Dark overrides it, which the browser then remembers, as it
// remembers the language. The mode in effect, light or dark, is written to
// <html> as its class and its data-theme, which the theme's tokens and
// HeroUI's follow (theme/theme.css).

/** modes are the choices of colour mode: Light, Dark, or the system's, the default. */
export const modes = ["light", "dark", "system"] as const;

/** Mode is a choice of colour mode: Light, Dark, or the system's. */
export type Mode = (typeof modes)[number];

/** storageKey is where the browser remembers the mode chosen. */
const storageKey = "koko.theme";

/** systemDark is the query of the system's colour scheme. */
const systemDark = "(prefers-color-scheme: dark)";

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
 * applyMode writes the mode in effect for mode to <html>: its class and its
 * data-theme, light or dark, the system's for System.
 */
function applyMode(mode: Mode): void {
  const dark = mode === "dark" || (mode === "system" && window.matchMedia(systemDark).matches);
  const scheme = dark ? "dark" : "light";
  const html = document.documentElement;
  html.classList.toggle("dark", dark);
  html.classList.toggle("light", !dark);
  html.dataset.theme = scheme;
}

/**
 * applyStoredMode writes the stored mode to <html>, for main.tsx to call
 * before React's first render, which runs in a later task: so no frame
 * between shows the other mode. index.html may run no inline script that
 * could do it sooner.
 */
export function applyStoredMode(): void {
  applyMode(storedMode());
}

const ModeContext = createContext<[Mode, (mode: Mode) => void] | undefined>(undefined);

/**
 * ModeProvider holds the mode and keeps <html> in it: on each choice, and,
 * for System, whenever the system's mode changes.
 */
export function ModeProvider({ children }: { children: ReactNode }) {
  const [mode, setChosen] = useState(storedMode);
  useEffect(() => {
    applyMode(mode);
    if (mode !== "system") {
      return;
    }
    const query = window.matchMedia(systemDark);
    const follow = () => applyMode("system");
    query.addEventListener("change", follow);
    return () => query.removeEventListener("change", follow);
  }, [mode]);
  const setMode = (next: Mode) => {
    try {
      localStorage.setItem(storageKey, next);
    } catch {
      // The choice still applies to this page when the browser cannot store it.
    }
    setChosen(next);
  };
  return <ModeContext value={[mode, setMode]}>{children}</ModeContext>;
}

/** useMode is the mode chosen, and a function that chooses another, which the browser remembers. */
export function useMode(): [Mode, (mode: Mode) => void] {
  const state = useContext(ModeContext);
  if (!state) {
    throw new Error("a component that reads the mode is outside the shell's ModeProvider");
  }
  return state;
}
