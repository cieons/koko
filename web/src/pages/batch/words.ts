import type { MessageKey } from "../../i18n/catalogues";
import type { Count, Translate } from "../../shell/i18n";
import type { Words } from "./BatchDialog";

/**
 * Base is where an action's messages of how its batch ended are, such as
 * batch.host.move: the keys under it are done (counted), none and rest
 * (counted).
 */
export type Base = {
  [K in MessageKey]: K extends `${infer B}.none` ? (B extends `batch.${string}` ? B : never) : never;
}[MessageKey];

/** wordsOf are the words of how an action's batch ended, from its messages, with params such as the Space moved to. */
export function wordsOf(t: Translate, count: Count, base: Base, params: Readonly<Record<string, string>> = {}): Words {
  return {
    done: (n) => count(`${base}.done`, n, params),
    none: t(`${base}.none`, params),
    rest: (n) => count(`${base}.rest`, n, params),
  };
}
