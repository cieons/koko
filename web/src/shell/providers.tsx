import { InternationalizationProvider } from "@astryxdesign/core/i18n";
import { LinkProvider } from "@astryxdesign/core/Link";
import zhCN from "@astryxdesign/core/locales/zh-CN.generated.js";
import type { ReactNode } from "react";

import { LanguageProvider, useLanguage } from "./i18n";
import { ModeProvider } from "./mode";
import { Link } from "./router";

/** astryxMessages are Astryx's own catalogues of the languages koko ships beyond its built-in English. */
const astryxMessages = { "zh-CN": zhCN };

/**
 * Providers are what every page of the web UI renders inside: koko's
 * language; Astryx's Neutral theme in the mode chosen; Astryx's own strings
 * (a dialog's close label, a table's empty state) in koko's language; and
 * koko's router behind every link an Astryx component renders.
 */
export function Providers({ children }: { children: ReactNode }) {
  return (
    <LanguageProvider>
      <ModeProvider>
        <AstryxStrings>
          <LinkProvider component={Link}>{children}</LinkProvider>
        </AstryxStrings>
      </ModeProvider>
    </LanguageProvider>
  );
}

function AstryxStrings({ children }: { children: ReactNode }) {
  return (
    <InternationalizationProvider locale={useLanguage()} messages={astryxMessages}>
      {children}
    </InternationalizationProvider>
  );
}
