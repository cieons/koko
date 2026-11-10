import { ToggleButton } from "@heroui/react";
import type { ReactNode } from "react";

import { languages } from "../../i18n/catalogues";
import { useLanguageChoice, useT } from "../../shell/i18n";
import { useDocumentTitle } from "../../ui/Page";

/** DoorProps are what a page outside the frame shows in its panel. */
export interface DoorProps {
  /** title is the panel's title, which names the browser's tab too. */
  title: string;
  /** lead is the line under the title. */
  lead?: ReactNode;
  children: ReactNode;
}

/**
 * Door is the layout of a page outside the frame, Sign in and Set a
 * Password (docs/web-design.md, "Sign in and Set a Password"): koko's
 * wordmark, one panel 26rem wide with the page's title and content, and the
 * two languages as buttons, which the browser keeps as the top bar's
 * Language menu does.
 */
export function Door({ title, lead, children }: DoorProps) {
  const t = useT();
  const [language, setLanguage] = useLanguageChoice();
  useDocumentTitle(title);
  return (
    <div className="flex min-h-dvh flex-col items-center px-3 pt-[12dvh] pb-10">
      <header className="mb-8">
        <p className="text-4xl leading-none font-semibold tracking-[-0.04em] select-none">{t("shell.brand")}</p>
      </header>
      <main className="sheet w-full max-w-[26rem] p-6 sm:p-8">
        <h1 className="t-title">{title}</h1>
        {lead !== undefined && <p className="mt-1.5 text-muted">{lead}</p>}
        <div className="mt-6">{children}</div>
      </main>
      <nav aria-label={t("shell.language")} className="mt-6 flex items-center gap-1">
        {languages.map((l) => (
          <ToggleButton
            key={l}
            lang={l}
            variant="ghost"
            isSelected={language === l}
            onChange={(on) => on && setLanguage(l)}
            className="rounded-full text-muted data-[selected=true]:bg-default data-[selected=true]:text-foreground"
          >
            {t(`shell.language.${l}`)}
          </ToggleButton>
        ))}
      </nav>
    </div>
  );
}
