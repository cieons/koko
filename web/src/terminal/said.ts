import { CodeRevoked } from "../api/device";
import { CodeHostUnreachable, CodeLoginRefused } from "../api/enroll";
import { CodeUnknownHost } from "../api/host";
import { CodeCookieRequired, CodeSignInRequired } from "../api/web";
import type { MessageKey } from "../i18n/catalogues";
import type { Translate } from "../shell/i18n";
import type { Ended } from "./workspaceSocket";

// What the page says of a tab's refusals and endings, in its own words
// (docs/web-design.md, "Tabs and their states"): the pane's covers and
// cards, and an open's refusal, which the frame says.

/**
 * restartNotice is the Gateway's notice when the koko server stops for a
 * restart (gateway.go, errStopping), which the page says in its own words;
 * it must match the Gateway's byte for byte.
 */
export const restartNotice = "the koko server is restarting; this SSH Session has ended.";

/**
 * refusals are the open's refusals (the Gateway's, spec #169) the page says
 * in its own words, by their codes; any other is said generally.
 */
const refusals: Readonly<Record<string, MessageKey>> = {
  [CodeUnknownHost]: "terminal.refused.unknown_host",
  host_registered: "terminal.refused.host_registered",
  relay_registered: "terminal.refused.relay_registered",
  access_refused: "terminal.refused.access_refused",
  ssh_session_limit: "terminal.refused.ssh_session_limit",
  [CodeHostUnreachable]: "terminal.refused.host_unreachable",
  host_key_mismatch: "terminal.refused.host_key_mismatch",
  [CodeLoginRefused]: "terminal.refused.login_refused",
  host_connection_failed: "terminal.refused.host_connection_failed",
  shell_refused: "terminal.refused.shell_refused",
};

/** shellRefusals are the web front's refusals the shell already has words for. */
const shellRefusals: Readonly<Record<string, MessageKey>> = {
  [CodeCookieRequired]: "error.cookie_required",
  [CodeRevoked]: "error.revoked",
  [CodeSignInRequired]: "error.signin_required",
};

/**
 * Said is what a state says: a line in the page's words, and beneath it the
 * koko server's own, as it wrote them, when they add to it: they may begin
 * with a Host's name, which keeps its case.
 */
export interface Said {
  line: string;
  detail?: string;
}

/** refusalSaid says why an open, a reopen or an attach was refused. */
export function refusalSaid(t: Translate, host: string, code: string, message: string): Said {
  const shell = shellRefusals[code];
  if (shell !== undefined) {
    return { line: t(shell) };
  }
  // An unknown name's words are the same for every Host the caller cannot see, and add nothing.
  const detail = message === "" || code === CodeUnknownHost ? undefined : message;
  return { line: t(refusals[code] ?? "terminal.refused.other", { host }), detail };
}

/** endingSaid says why a tab's SSH Session ended, as the Workspace gives it. */
export function endingSaid(t: Translate, host: string, ended: Ended | undefined): Said {
  if (ended?.exitStatus !== undefined) {
    return { line: t("terminal.ended.status", { host, status: ended.exitStatus }) };
  }
  if (ended?.exitSignal !== undefined) {
    return { line: t("terminal.ended.signal", { host, signal: ended.exitSignal }) };
  }
  const notice = ended?.notice ?? "";
  if (notice === restartNotice) {
    return { line: t("terminal.ended.restart") };
  }
  return { line: t("terminal.ended.notice", { host }), detail: notice === "" ? undefined : notice };
}
