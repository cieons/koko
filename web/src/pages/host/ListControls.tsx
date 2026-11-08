import type { MouseEvent } from "react";

import { useT } from "../../shell/i18n";
import { type ReadTerm, readSortKey, sortBy, type View } from "./view";

// The controls a list of records shares, written over its view rather than
// over Hosts: the filter terms in words, the column picker and the sortable
// column headers. The Hosts list and the Services list use them alike.

/** ViewProps are what a part of a list gets to show its view and change it. */
export interface ViewProps {
  view: View;
  /** setView moves the page to another view, a new history entry. */
  setView: (next: View) => void;
}

/**
 * Terms are the view's filter terms, each in words, such as price > 10,
 * with a button that takes it away; a relative date shows the date it
 * stands for today. A term that does not read shows as written.
 */
export function Terms({ view, setView, terms }: ViewProps & { terms: readonly ReadTerm[] }) {
  const t = useT();
  if (terms.length === 0) {
    return null;
  }
  return (
    <ul className="terms" aria-label={t("host.filter.terms")}>
      {terms.map(({ written, term, filter }, i) => {
        const shown = term
          ? [term.key, t(`host.filter.op.${term.op}`), term.value].filter((part) => part !== undefined).join(" ")
          : written;
        const resolved = term?.value !== undefined && filter?.value !== undefined && String(filter.value) !== term.value;
        return (
          <li key={`${i}:${written}`}>
            <code>{shown}</code>
            {resolved && <span className="tag"> ({filter?.value})</span>}
            <button
              type="button"
              aria-label={t("host.filter.remove", { term: shown })}
              onClick={() => setView({ ...view, filters: view.filters.filter((_, j) => j !== i) })}
            >
              ×
            </button>
          </li>
        );
      })}
    </ul>
  );
}

/**
 * ColumnPicker shows or hides a column for each of keys, after the columns
 * shown; a column added goes last, and a column hidden no longer sorts the
 * list.
 */
export function ColumnPicker({ view, setView, keys }: ViewProps & { keys: readonly string[] }) {
  const t = useT();
  const hide = (key: string): View => ({
    ...view,
    columns: view.columns.filter((c) => c !== key),
    sort: view.sort.filter((s) => readSortKey(s).key !== key),
  });
  return (
    <fieldset className="columns">
      <legend>{t("host.columns")}</legend>
      {[...new Set([...view.columns, ...keys])].map((key) => (
        <label key={key}>
          <input
            type="checkbox"
            name={key}
            checked={view.columns.includes(key)}
            onChange={(e) => setView(e.target.checked ? { ...view, columns: [...view.columns, key] } : hide(key))}
          />{" "}
          {key}
        </label>
      ))}
    </fieldset>
  );
}

/**
 * SortHeader is the header of the column of key: a click sorts by it alone,
 * a Shift-click adds it as a further key, and it shows where it sorts.
 */
export function SortHeader({ view, setView, column }: ViewProps & { column: string }) {
  const t = useT();
  const sort = view.sort.map(readSortKey);
  const at = sort.findIndex((k) => k.key === column);
  const by = sort[at];
  const click = (e: MouseEvent<HTMLButtonElement>) => setView({ ...view, sort: sortBy(view.sort, column, e.shiftKey) });
  return (
    <th aria-sort={at === 0 ? (by?.desc ? "descending" : "ascending") : undefined}>
      <button type="button" className="sort" onClick={click} title={t("host.sort.hint")}>
        {column}
        {by && (
          <span aria-hidden="true">
            {" "}
            {by.desc ? "▼" : "▲"}
            {sort.length > 1 ? at + 1 : ""}
          </span>
        )}
      </button>
    </th>
  );
}
