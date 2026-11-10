import { readTerm, readView, type View, writeTerm, writeView } from "./view";

// The Hosts page's view as its URL holds it, in koko's grammar (filter,
// sort, columns, all=1) and the page's own parameters: the search (q), the
// Services view (view=services) and, in that view, its Space (space=), since
// service.list takes no space key. The Space and state selects read and
// write filter terms, as docs/web-design.md's table under "The URL keeps
// koko's grammar" gives them.

/** Tab is the view the page lists: its Hosts, or their Services. */
export type Tab = "hosts" | "services";

/** ListPage is the page's whole view. */
export interface ListPage {
  tab: Tab;
  /** q is the search, applied in the browser. */
  q: string;
  /** space is the Services view's Space, by name, or "" for every Space. */
  space: string;
  /** list is the list's view: its terms, sort keys, columns and all=1. */
  list: View;
}

/** Defaults are each view's default columns, which the URL leaves out. */
export type Defaults = Readonly<Record<Tab, readonly string[]>>;

/** readListPage reads the page's view from its URL's query. */
export function readListPage(params: URLSearchParams, defaults: Defaults): ListPage {
  const tab: Tab = params.get("view") === "services" ? "services" : "hosts";
  return {
    tab,
    q: params.get("q") ?? "",
    space: tab === "services" ? (params.get("space") ?? "") : "",
    list: readView(params, defaults[tab]),
  };
}

/**
 * writeListPage writes the page's view as its URL's query, leaving out what
 * is as by default: the list's view as koko's grammar writes it, then the
 * Services view's Space, the search and the view.
 */
export function writeListPage(page: ListPage, defaults: Defaults): URLSearchParams {
  const params = writeView(page.list, defaults[page.tab]);
  if (page.tab === "services" && page.space !== "") {
    params.set("space", page.space);
  }
  if (page.q !== "") {
    params.set("q", page.q);
  }
  if (page.tab === "services") {
    params.set("view", "services");
  }
  return params;
}

/** emptyView is a list's view with no term, no sort key, its default columns and no all. */
function emptyView(columns: readonly string[]): View {
  return { filters: [], sort: [], columns: [...columns], all: false };
}

/**
 * switchTab is the page's view after its other tab is chosen: the search
 * stays, and so does the Space, as a space term in the Hosts view and as
 * space= in the Services view; the other view's terms, sort keys and
 * columns are dropped.
 */
export function switchTab(page: ListPage, tab: Tab, defaults: Defaults, hostsSpace: string | undefined): ListPage {
  if (tab === page.tab) {
    return page;
  }
  if (tab === "services") {
    return { tab, q: page.q, space: hostsSpace ?? "", list: emptyView(defaults.services) };
  }
  const list = emptyView(defaults.hosts);
  if (page.space !== "") {
    list.filters.push(writeTerm("space", "eq", page.space));
  }
  return { tab, q: page.q, space: "", list };
}

/** SelectedTerm is the term a select shows: its value, and its index among the view's terms. */
export interface SelectedTerm {
  value: string;
  index: number;
}

/**
 * selected is the value of the one term of key the list's select shows,
 * with its index among the terms: a term key=value, the only term of its
 * key, whose value the select offers. Any other term of the key, or a
 * second one, leaves the select on its first option and shows as a chip.
 */
export function selected(
  filters: readonly string[],
  key: string,
  offers: (value: string) => boolean,
): SelectedTerm | undefined {
  const ofKey = filters.flatMap((written, index) => {
    const term = readTerm(written);
    return term?.key === key ? [{ term, index }] : [];
  });
  const [only] = ofKey;
  if (ofKey.length !== 1 || only === undefined || only.term.op !== "eq" || only.term.value === undefined) {
    return undefined;
  }
  return offers(only.term.value) ? { value: only.term.value, index: only.index } : undefined;
}

/** withoutKey is the terms without any term of key. */
function withoutKey(filters: readonly string[], key: string): string[] {
  return filters.filter((written) => readTerm(written)?.key !== key);
}

/**
 * chooseSpace is the Hosts view after the Space select chose space, or
 * every Space for undefined: the select sets the space key alone, so any
 * other space term goes, and "All Spaces" leaves none.
 */
export function chooseSpace(view: View, space: string | undefined): View {
  const filters = withoutKey(view.filters, "space");
  if (space !== undefined) {
    filters.push(writeTerm("space", "eq", space));
  }
  return { ...view, filters };
}

/** StateChoice is an option of the state select. */
export type StateChoice = "inUse" | "managed" | "registered" | "retired" | "all";

/** states are the states the state select offers by name. */
const states = ["managed", "registered", "retired"];

/** stateTerm is the term the state select shows, with its index among the terms, if any. */
export function stateTerm(view: View): SelectedTerm | undefined {
  return selected(view.filters, "state", (v) => states.includes(v));
}

/**
 * stateChoice is the state select's option for a view: the state of its one
 * state term; with none, All, with Retired under all=1, else In use. A term
 * the select cannot show leaves it on In use, its first option.
 */
export function stateChoice(view: View): StateChoice {
  const shown = stateTerm(view);
  if (shown) {
    return shown.value as StateChoice;
  }
  const other = view.filters.some((written) => readTerm(written)?.key === "state");
  return !other && view.all ? "all" : "inUse";
}

/**
 * chooseState is the Hosts view after the state select chose choice: In
 * use, no state term and no all; Managed or Registered, a state term of
 * that value; Retired, a state term retired and all=1; All, with Retired,
 * all=1 and no state term. The select sets the state key alone.
 */
export function chooseState(view: View, choice: StateChoice): View {
  const filters = withoutKey(view.filters, "state");
  if (choice === "managed" || choice === "registered" || choice === "retired") {
    filters.push(writeTerm("state", "eq", choice));
  }
  return { ...view, filters, all: choice === "retired" || choice === "all" };
}
