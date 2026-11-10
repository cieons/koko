import { createContext, type ReactNode, useContext, useEffect, useState } from "react";

import { ApiError, DroppedError } from "../client";
import { catalogues, type Language, languages, type MessageKey } from "../i18n/catalogues";

// Language: the web UI follows the browser's preferred languages until the
// top bar's Language menu picks one, which the browser then remembers.
// Names, keys, values and Notes are never translated: they reach a message
// only as its parameters, shown as stored.

/** storageKey is where the browser remembers the language the Language menu picked. */
const storageKey = "koko.language";

/**
 * chooseLanguage picks the first of the browser's preferred languages that
 * the web UI ships: Chinese of any region gives Simplified Chinese, and
 * English when none is shipped.
 */
export function chooseLanguage(preferred: readonly string[]): Language {
  for (const tag of preferred) {
    const primary = tag.toLowerCase().split("-")[0];
    if (primary === "zh") {
      return "zh-CN";
    }
    if (primary === "en") {
      return "en";
    }
  }
  return "en";
}

/** remembered is the language the Language menu picked, unless none or unreadable. */
function remembered(): Language | undefined {
  try {
    const stored = localStorage.getItem(storageKey);
    return languages.find((l) => l === stored);
  } catch {
    return undefined; // storage the browser refuses remembers nothing
  }
}

/** Translate is a message of the current language, with its parameters. */
export type Translate = (key: MessageKey, params?: Readonly<Record<string, string | number>>) => string;

interface LanguageState {
  language: Language;
  setLanguage: (language: Language) => void;
  t: Translate;
}

const LanguageContext = createContext<LanguageState | undefined>(undefined);

/** LanguageProvider holds the language of everything inside it. */
export function LanguageProvider({ children }: { children: ReactNode }) {
  const [language, setChosen] = useState<Language>(() => remembered() ?? chooseLanguage(navigator.languages));
  useEffect(() => {
    document.documentElement.lang = language;
  }, [language]);
  const setLanguage = (next: Language) => {
    try {
      localStorage.setItem(storageKey, next);
    } catch {
      // The choice still applies to this page when the browser cannot store it.
    }
    setChosen(next);
  };
  const t: Translate = (key, params = {}) =>
    catalogues[language][key].replace(/\{(\w+)\}/g, (placeholder, name: string) =>
      name in params ? String(params[name]) : placeholder,
    );
  return <LanguageContext value={{ language, setLanguage, t }}>{children}</LanguageContext>;
}

function useLanguageState(): LanguageState {
  const state = useContext(LanguageContext);
  if (!state) {
    throw new Error("a component that shows messages is outside the shell's LanguageProvider");
  }
  return state;
}

/** useLanguage is the current language. */
export function useLanguage(): Language {
  return useLanguageState().language;
}

/** useT is the message function of the current language. */
export function useT(): Translate {
  return useLanguageState().t;
}

/** errorMessage is the message for a failed call in the current language. */
export function errorMessage(t: Translate, error: unknown): string {
  if (error instanceof ApiError) {
    const key = `error.${error.code}`;
    return key in catalogues.en ? t(key as MessageKey) : t("shell.error.unexpected");
  }
  if (error instanceof DroppedError) {
    return t("shell.error.dropped");
  }
  return t("shell.error.unexpected");
}

/** useLanguageChoice is the language, and a function that chooses another, which the browser remembers. */
export function useLanguageChoice(): [Language, (language: Language) => void] {
  const { language, setLanguage } = useLanguageState();
  return [language, setLanguage];
}
