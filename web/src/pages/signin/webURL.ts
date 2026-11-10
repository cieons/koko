import { ServerShow } from "../../api/server";
import { useRead } from "../../shell/data";
import { errorMessage, type Translate } from "../../shell/i18n";

/** WebURL is whether the Web URL is set, which a Password Link needs to name an address. */
export interface WebURL {
  /** set is true once server.show answers a Web URL, false while it answers none or has not answered. */
  set: boolean;
  /**
   * reason says why a Password Link cannot be issued: the Web URL unset, or
   * server.show failing; undefined while the Web URL is set or unknown yet.
   */
  reason: ((t: Translate) => string) | undefined;
}

/**
 * useWebURL reads from server.show, which answers every caller, whether
 * the Web URL is set, for Invite's In a browser and Issue Password Link,
 * which stay disabled until it is, saying why.
 */
export function useWebURL(): WebURL {
  const server = useRead(ServerShow, {});
  const url = server.data?.server.web_url;
  if (server.error !== undefined && url === undefined) {
    const { error } = server;
    return { set: false, reason: (t) => errorMessage(t, error) };
  }
  if (url === null) {
    return { set: false, reason: (t) => t("signin.webURLUnset") };
  }
  return { set: typeof url === "string", reason: undefined };
}
