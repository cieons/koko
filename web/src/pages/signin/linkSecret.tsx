import { type ComponentType, createContext, useContext, useEffect, useState } from "react";

import { wrappingPage } from "../../shell/lazyPage";
import { navigate, type PageProps } from "../../shell/router";

/**
 * LinkSecret is the secret of the Password Link Set a Password was opened
 * with: undefined until read, and "" when the URL held none.
 */
const LinkSecret = createContext<string | undefined>(undefined);

/** useLinkSecret is the Password Link's secret the route took from the URL. */
export function useLinkSecret(): string | undefined {
  return useContext(LinkSecret);
}

/**
 * takingLinkSecret is Set a Password's route: it takes the Password Link's
 * secret out of the URL's fragment at once, replacing the URL without it,
 * before the page's own chunk has loaded, so the secret leaves the address
 * bar and the history; and again when another link is opened on the page,
 * which changes the fragment alone. The page reads it with useLinkSecret.
 * It is part of the first load, and small.
 */
export function takingLinkSecret(Page: ComponentType<PageProps>): ComponentType<PageProps> {
  return wrappingPage(function TakingLinkSecret(props: PageProps) {
    const [secret, setSecret] = useState<string>();
    useEffect(() => {
      const take = () => {
        const { hash, pathname, search } = window.location;
        if (hash.length > 1) {
          navigate(pathname + search, { replace: true });
          setSecret(hash.slice(1));
        } else {
          setSecret((was) => was ?? ""); // a second run finds the fragment gone, and keeps what the first took
        }
      };
      take();
      window.addEventListener("hashchange", take);
      return () => window.removeEventListener("hashchange", take);
    }, []);
    return (
      <LinkSecret value={secret}>
        <Page {...props} />
      </LinkSecret>
    );
  }, Page);
}
