import { ServerShow } from "../../api/server";
import { useRead } from "../../shell/data";
import { errorMessage, type Translate } from "../../shell/i18n";

/** WebURL is whether the Web URL is set, which a Sign-in Link needs to name an address, and its host, which passkeys are made for. */
export interface WebURL {
  /** set is true once server.show answers a Web URL, false while it answers none or has not answered. */
  set: boolean;
  /**
   * reason says why a Sign-in Link cannot be issued: the Web URL unset, or
   * server.show failing; undefined while the Web URL is set or unknown yet.
   */
  reason: ((t: Translate) => string) | undefined;
  /**
   * host is the Web URL's host without its port, a passkey's relying party:
   * null while the Web URL is unset, undefined until server.show answers.
   */
  host: string | null | undefined;
}

/**
 * useWebURL reads from server.show, which answers every caller, whether
 * the Web URL is set, for Invite's In a browser and Issue Sign-in Link,
 * which stay disabled until it is, saying why; and its host, beside which
 * a passkey made for another host says so.
 */
export function useWebURL(): WebURL {
  const server = useRead(ServerShow, {});
  const url = server.data?.server.web_url;
  if (server.error !== undefined && url === undefined) {
    const { error } = server;
    return { set: false, reason: (t) => errorMessage(t, error), host: undefined };
  }
  if (url === null) {
    return { set: false, reason: (t) => t("signin.webURLUnset"), host: null };
  }
  return { set: typeof url === "string", reason: undefined, host: url === undefined ? undefined : hostOf(url) };
}

/** hostOf is a URL's host without its port, as a passkey's relying party names it; undefined for one that does not parse. */
function hostOf(url: string): string | undefined {
  try {
    return new URL(url).hostname;
  } catch {
    return undefined;
  }
}
