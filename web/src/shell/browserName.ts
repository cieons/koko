import type { SignIn } from "../api/signin";
import type { Translate } from "./i18n";

/**
 * browserName names a Sign-in's browser in the reader's language, as the
 * koko server's English label does (api.ClientLabel): the browser on its
 * system, the browser alone without a system, or "Browser" for one koko
 * does not recognise.
 */
export function browserName(t: Translate, { browser, system }: Pick<SignIn, "browser" | "system">): string {
  if (browser === undefined) {
    return t("shell.browser.unknown");
  }
  return system === undefined ? browser : t("shell.browser.on", { browser, system });
}
