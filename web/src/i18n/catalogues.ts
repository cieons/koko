// The message catalogues: every string the web UI shows, one catalogue per
// language. Each area keeps its messages in a file of its own per language,
// en/<area>.ts and zh-CN/<area>.ts, and adds one line for each to these
// lists. The Simplified Chinese file changes with its English twin, key for
// key; catalogues.test.ts checks that, and that every registered API error
// code has a message in its area's file.

import enBatch from "./en/batch";
import enDevice from "./en/device";
import enEnroll from "./en/enroll";
import enErrors from "./en/errors";
import enFiles from "./en/files";
import enHost from "./en/host";
import enSecret from "./en/secret";
import enServer from "./en/server";
import enService from "./en/service";
import enShell from "./en/shell";
import enSignin from "./en/signin";
import enSpace from "./en/space";
import enTerminal from "./en/terminal";
import enUser from "./en/user";
import enWeb from "./en/web";
import zhBatch from "./zh-CN/batch";
import zhDevice from "./zh-CN/device";
import zhEnroll from "./zh-CN/enroll";
import zhErrors from "./zh-CN/errors";
import zhFiles from "./zh-CN/files";
import zhHost from "./zh-CN/host";
import zhSecret from "./zh-CN/secret";
import zhServer from "./zh-CN/server";
import zhService from "./zh-CN/service";
import zhShell from "./zh-CN/shell";
import zhSignin from "./zh-CN/signin";
import zhSpace from "./zh-CN/space";
import zhTerminal from "./zh-CN/terminal";
import zhUser from "./zh-CN/user";
import zhWeb from "./zh-CN/web";

const en = {
  ...enShell,
  ...enErrors,
  ...enDevice,
  ...enHost,
  ...enSecret,
  ...enServer,
  ...enSpace,
  ...enUser,
  ...enWeb,
  ...enEnroll,
  ...enService,
  ...enSignin,
  ...enBatch,
  ...enFiles,
  ...enTerminal,
};

/** MessageKey is the key of a message, such as "shell.language". */
export type MessageKey = keyof typeof en;

/** Catalogue holds every message of one language, by key. */
export type Catalogue = Record<MessageKey, string>;

const zhCN: Catalogue = {
  ...zhShell,
  ...zhErrors,
  ...zhDevice,
  ...zhHost,
  ...zhSecret,
  ...zhServer,
  ...zhSpace,
  ...zhUser,
  ...zhWeb,
  ...zhEnroll,
  ...zhService,
  ...zhSignin,
  ...zhBatch,
  ...zhFiles,
  ...zhTerminal,
};

/** The languages the web UI ships, English first: the default. */
export const languages = ["en", "zh-CN"] as const;

/** Language is a language the web UI ships, as its BCP 47 tag. */
export type Language = (typeof languages)[number];

export const catalogues: Record<Language, Catalogue> = { en, "zh-CN": zhCN };
