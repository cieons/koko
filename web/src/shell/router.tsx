import { type ComponentType, type MouseEvent, type ReactNode, useMemo, useSyncExternalStore } from "react";

import type { MessageKey } from "../i18n/catalogues";

// The router: each page is a path, and a page keeps its state (filters,
// sorting, columns) in the URL's query, so a bookmark is a saved view. It
// uses the browser's history alone; the web front serves index.html for every
// path outside /v1/.

/** PageProps are what the router gives a page. */
export interface PageProps {
  /** params holds the path's :name segments, decoded, by name. */
  params: Readonly<Record<string, string>>;
}

/** Route is one page of the web UI. */
export interface Route {
  /**
   * path is the page's path, such as /spaces/:name; a :name segment matches
   * any one segment.
   */
  path: string;
  page: ComponentType<PageProps>;
  /** nav, when set, links the page from the header under this message. */
  nav?: MessageKey;
  /** ownerOnly hides the page, and its link, from a Member. */
  ownerOnly?: boolean;
}

/** Location is where the browser is, and which visit of it this is. */
export interface Location {
  pathname: string;
  /** search is the query, with its "?", or "". */
  search: string;
  /**
   * visit counts the history entries the browser moved to (a link, Back,
   * Forward), so a page reloads its data on each, even to the URL it is on.
   * A replace refines the entry it is on and is not a new visit.
   */
  visit: number;
}

let visit = 0;
let snapshot: Location | undefined;
const listeners = new Set<() => void>();

function changed(): void {
  for (const listener of listeners) {
    listener();
  }
}

function visited(): void {
  visit++;
  changed();
}

function subscribe(listener: () => void): () => void {
  if (listeners.size === 0) {
    window.addEventListener("popstate", visited);
  }
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0) {
      window.removeEventListener("popstate", visited);
    }
  };
}

function current(): Location {
  const { pathname, search } = window.location;
  if (snapshot?.pathname !== pathname || snapshot.search !== search || snapshot.visit !== visit) {
    snapshot = { pathname, search, visit };
  }
  return snapshot;
}

/** useLocation is where the browser is, updated on every navigation. */
export function useLocation(): Location {
  return useSyncExternalStore(subscribe, current);
}

/** NavigateOptions are how navigate moves. */
export interface NavigateOptions {
  /**
   * replace takes the place of the current history entry, as a redirect or
   * a page's state refined in place does, and is not a new visit.
   */
  replace?: boolean;
}

/** navigate moves the web UI to the path to, with its query. */
export function navigate(to: string, options: NavigateOptions = {}): void {
  if (options.replace) {
    window.history.replaceState(null, "", to);
    changed();
  } else {
    window.history.pushState(null, "", to);
    visited();
  }
}

/**
 * useSearchParams is the page's query, the page state it keeps in the URL,
 * and a function that navigates to the same page with another query.
 */
export function useSearchParams(): [URLSearchParams, (next: URLSearchParams, options?: NavigateOptions) => void] {
  const { pathname, search } = useLocation();
  const params = useMemo(() => new URLSearchParams(search), [search]);
  const set = (next: URLSearchParams, options?: NavigateOptions) => {
    const query = next.toString();
    navigate(query === "" ? pathname : `${pathname}?${query}`, options);
  };
  return [params, set];
}

/**
 * useQueryFlag is a yes-or-no page state kept in the URL's query as
 * name=1, such as ?all=1 for a list that shows its Removed or Revoked
 * records too, and a function that navigates to the page with it set or not.
 */
export function useQueryFlag(name: string): [boolean, (on: boolean) => void] {
  const [params, setParams] = useSearchParams();
  const set = (on: boolean) => {
    const next = new URLSearchParams(params);
    if (on) {
      next.set(name, "1");
    } else {
      next.delete(name);
    }
    setParams(next);
  };
  return [params.get(name) === "1", set];
}

/** Link is a link to a page of the web UI, followed without a page load. */
export function Link({ to, children }: { to: string; children: ReactNode }) {
  const { pathname } = useLocation();
  const follow = (event: MouseEvent<HTMLAnchorElement>) => {
    // A modified click, or another button, opens the link as the browser
    // would, in a new tab or window.
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
      return;
    }
    event.preventDefault();
    navigate(to);
  };
  return (
    <a href={to} onClick={follow} aria-current={pathname === to ? "page" : undefined}>
      {children}
    </a>
  );
}

/** matchRoute finds the route of pathname, with the params of its path. */
export function matchRoute(
  routes: readonly Route[],
  pathname: string,
): { route: Route; params: Record<string, string> } | undefined {
  const segments = pathname.split("/").filter((s) => s !== "");
  for (const route of routes) {
    const pattern = route.path.split("/").filter((s) => s !== "");
    if (pattern.length !== segments.length) {
      continue;
    }
    const params: Record<string, string> = {};
    const matches = pattern.every((p, i) => {
      const segment = segments[i] ?? "";
      if (!p.startsWith(":")) {
        return p === segment;
      }
      try {
        params[p.slice(1)] = decodeURIComponent(segment);
        return true;
      } catch {
        return false; // a segment that does not decode names no page
      }
    });
    if (matches) {
      return { route, params };
    }
  }
  return undefined;
}
