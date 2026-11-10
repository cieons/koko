import { I18nProvider, RouterProvider, Toast } from "@heroui/react";
import type { ReactNode } from "react";

import type { Language } from "../i18n/catalogues";
import { LanguageProvider, useLanguage } from "./i18n";
import { ModeProvider } from "./mode";
import { navigate, type NavigateOptions } from "./router";

declare module "react-aria-components" {
  interface RouterConfig {
    routerOptions: NavigateOptions;
  }
}

/** locales are React Aria's locale for each of koko's languages, the two the build keeps (vite.config.ts). */
const locales: Record<Language, string> = { en: "en-US", "zh-CN": "zh-CN" };

/**
 * Providers are what every page of the web UI renders inside, in main.tsx
 * and in the page tests alike: koko's language; the mode, Light, Dark or
 * System, kept on <html>; React Aria's own strings, such as a dialog's close
 * label, its dates and numbers, in koko's language; koko's router behind
 * every link HeroUI renders; and the toasts, bottom end.
 */
export function Providers({ children }: { children: ReactNode }) {
  return (
    <LanguageProvider>
      <ModeProvider>
        <AriaLocale>
          <RouterProvider navigate={(to, options) => navigate(to, options)}>
            {children}
            <Toast.Provider placement="bottom end" />
          </RouterProvider>
        </AriaLocale>
      </ModeProvider>
    </LanguageProvider>
  );
}

function AriaLocale({ children }: { children: ReactNode }) {
  return <I18nProvider locale={locales[useLanguage()]}>{children}</I18nProvider>;
}
