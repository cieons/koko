import { Button } from "@heroui/react";
import { RotateCcw } from "lucide-react";
import { useLayoutEffect, useMemo } from "react";

import { useCaller } from "./caller";
import { useT } from "./i18n";
import { navigate, type NavigateOptions, useLocation, useSearchParams } from "./router";

// A list page keeps its whole view in its URL's query, so a bookmark is a
// saved view; the browser also remembers the last view each list showed,
// for each User, so a list opened with no view in its URL, from the top
// bar, a new tab or a link, shows it again (docs/web-design.md, "A list
// remembers its view").

/** RememberedList names the list a query is a view of, and the query of that list's default view. */
export interface RememberedList {
  /** list names the list where the browser remembers its view, such as hosts. */
  list: string;
  /** base is the query of the list's default view: "", or "view=services" for the Services view. */
  base: string;
}

/** RememberedView is a list page's view, and the ways to change it. */
export interface RememberedView {
  /** params is the view's query: the URL's, or the one remembered while the URL is being replaced by it. */
  params: URLSearchParams;
  /** setParams shows the view next, remembering it, as the router's setter does. */
  setParams: (next: URLSearchParams, options?: NavigateOptions) => void;
  /** changed says the view is not its list's default. */
  changed: boolean;
  /** reset shows the list's default view and forgets the one remembered, as a new history entry. */
  reset: () => void;
}

/**
 * shownMark marks, in a history entry's state, an entry whose URL holds no
 * view that the page has shown as the default view: Back to it, or a reload
 * of it, shows the default again, rather than the view remembered since,
 * which it leaves remembered.
 */
const shownMark = "kokoDefaultView";

/** storageKey is where the browser remembers the view of list for the User whose id is user. */
function storageKey(user: string, list: string): string {
  return `koko.view.${user}.${list}`;
}

/** recalled is the query remembered at key, or undefined for none or storage the browser refuses. */
function recalled(key: string): string | undefined {
  try {
    return localStorage.getItem(key) ?? undefined;
  } catch {
    return undefined;
  }
}

/** remember keeps query at key, or forgets what is there for undefined. */
function remember(key: string, query: string | undefined): void {
  try {
    if (query === undefined) {
      localStorage.removeItem(key);
    } else {
      localStorage.setItem(key, query);
    }
  } catch {
    // A browser that cannot store the view still shows it; it is only not kept.
  }
}

/** entryState is the history entry's state, as an object: the router's own entries hold none. */
function entryState(): Record<string, unknown> {
  const state: unknown = window.history.state;
  return typeof state === "object" && state !== null && !Array.isArray(state) ? (state as Record<string, unknown>) : {};
}

/** shownAsDefault says the history entry is one the page has shown as the default view. */
function shownAsDefault(): boolean {
  return shownMark in entryState();
}

/**
 * useRememberedView is a list page's view, its URL's query, kept in the
 * browser for the caller and the list listOf names for a query. A URL with
 * a view shows it and is remembered; a URL with none, the list's base,
 * shows the view remembered and is replaced by it, unless the page has
 * shown that history entry as the default before, as Back to it does.
 */
export function useRememberedView(listOf: (params: URLSearchParams) => RememberedList): RememberedView {
  const { pathname } = useLocation();
  const [shown, setSearch] = useSearchParams();
  const user = useCaller().user.id;
  const { list, base } = listOf(shown);
  const key = storageKey(user, list);
  const query = shown.toString();
  const bare = query === base;
  const kept = bare && !shownAsDefault() ? recalled(key) : undefined;
  const restored = kept === base ? undefined : kept;
  const params = useMemo(() => (restored === undefined ? shown : new URLSearchParams(restored)), [shown, restored]);
  useLayoutEffect(() => {
    if (restored !== undefined) {
      navigate(`${pathname}?${restored}`, { replace: true });
    } else if (bare) {
      window.history.replaceState({ ...entryState(), [shownMark]: true }, "");
    } else {
      remember(key, query);
    }
  }, [pathname, key, query, bare, restored]);
  const setParams = (next: URLSearchParams, options?: NavigateOptions) => {
    // The list's default view chosen is forgotten first, so it is not taken
    // for a URL with no view to restore. Another list's, such as the other
    // tab's with nothing carried over, restores that list's own view.
    if (next.toString() === base && listOf(next).list === list) {
      remember(key, undefined);
    }
    setSearch(next, options);
  };
  return {
    params,
    setParams,
    changed: params.toString() !== base,
    reset: () => setParams(new URLSearchParams(base)),
  };
}

/**
 * ResetView is the button that shows a list's default view and forgets the
 * one remembered. It stays in the row, unavailable at the default view, so
 * the row keeps its layout as the view changes.
 */
export function ResetView({ view }: { view: RememberedView }) {
  const t = useT();
  return (
    <Button variant="secondary" isDisabled={!view.changed} onPress={view.reset}>
      <RotateCcw aria-hidden className="size-4" />
      {t("shell.view.reset")}
    </Button>
  );
}
