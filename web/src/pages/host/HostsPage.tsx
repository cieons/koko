import "./host.css";

import { type MouseEvent, useId } from "react";

import { type AttrKey, AttrList } from "../../api/attr";
import { type Host, HostList } from "../../api/host";
import { useRead } from "../../shell/data";
import { ErrorMessage, useT } from "../../shell/i18n";
import { Link, useSearchParams } from "../../shell/router";
import { FilterBuilder } from "./FilterBuilder";
import { browserTimeZone } from "./reldate";
import {
  type Clock,
  type Fields,
  keyTypes,
  type ListQuery,
  readQuery,
  type ReadTerm,
  readSortKey,
  readView,
  sortBy,
  type View,
  writeView,
} from "./view";

/**
 * hostFields are the fields host.list filters and sorts on, with their
 * types, as api.HostListFields has them; every other key is an Attribute
 * key.
 */
export const hostFields: Fields = {
  id: { type: "text" },
  name: { type: "text" },
  space: { type: "text" },
  address: { type: "text" },
  port: { type: "number" },
  account: { type: "text" },
  state: { type: "enum", values: ["registered", "managed", "retired"] },
  host_key: { type: "text" },
};

/**
 * defaultColumns are the columns the list shows after the name unless its
 * URL names others; price shows with its currency.
 */
const defaultColumns = ["space", "state", "address", "provider", "region", "price", "expires_at"];

/**
 * hostPath is the path of a Host's page: by its name, or by its id once it
 * is Retired, since a new Host may hold its name.
 */
export function hostPath(host: Host): string {
  return `/hosts/${encodeURIComponent(host.state === "retired" ? host.id : host.name)}`;
}

/**
 * HostsPage lists the Hosts the caller can see, as `koko host ls` does,
 * through a view its URL keeps, so a bookmark is a saved view: the filter
 * terms built in three steps, the sort keys a click or a Shift-click on a
 * column header sets, the columns the picker chooses, and the Retired Hosts
 * when asked for (all=1, as --all). Relative dates stay as written in the
 * URL and are resolved in the browser's time zone on each load.
 */
export function HostsPage() {
  const t = useT();
  const titleId = useId();
  const [params, setParams] = useSearchParams();
  const attrs = useRead(AttrList, {});
  const view = readView(params, defaultColumns);
  const setView = (next: View) => setParams(writeView(next, defaultColumns));

  return (
    <>
      <h1 id={titleId}>{t("host.list.title")}</h1>
      {attrs.error !== undefined && <ErrorMessage error={attrs.error} />}
      {attrs.data && <HostsView view={view} setView={setView} attrs={attrs.data.attributes} titleId={titleId} />}
    </>
  );
}

/** ViewProps are what a part of the page gets to show the view and change it. */
interface ViewProps {
  view: View;
  /** setView moves the page to another view, a new history entry. */
  setView: (next: View) => void;
}

/**
 * asksForRetired tells whether a term keeps only Retired Hosts, which
 * host.list answers only with all, so the page sends all with it.
 */
function asksForRetired({ filter }: ReadTerm): boolean {
  return filter?.key === "state" && filter.op === "eq" && filter.value === "retired";
}

/** HostsView is the list and its controls, once the Attribute keys and their types are known. */
function HostsView({ view, setView, attrs, titleId }: ViewProps & { attrs: readonly AttrKey[]; titleId: string }) {
  const t = useT();
  const types = keyTypes(hostFields, attrs);
  const clock: Clock = { now: new Date(), timeZone: browserTimeZone() };
  const { query, terms, faults } = readQuery(view, types, clock);
  const retiredAsked = terms.some(asksForRetired);
  if (retiredAsked) {
    query.all = true;
  }

  return (
    <>
      <FilterBuilder
        fields={hostFields}
        attrs={attrs}
        types={types}
        clock={clock}
        onAdd={(term) => setView({ ...view, filters: [...view.filters, term] })}
      />
      <Terms view={view} setView={setView} terms={terms} />
      <label>
        <input
          type="checkbox"
          checked={query.all === true}
          disabled={retiredAsked}
          onChange={(e) => setView({ ...view, all: e.target.checked })}
        />{" "}
        {t("host.list.showRetired")}
      </label>
      <ColumnPicker view={view} setView={setView} attrs={attrs} />
      {faults.length > 0 ? (
        <p className="error" role="alert">
          {t("host.view.invalid", { parts: faults.join(", ") })} <Link to="/hosts">{t("host.view.reset")}</Link>
        </p>
      ) : (
        <HostTable view={view} setView={setView} query={query} titleId={titleId} />
      )}
    </>
  );
}

/**
 * Terms are the view's filter terms, each in words, such as price > 10,
 * with a button that takes it away; a relative date shows the date it
 * stands for today. A term that does not read shows as written.
 */
function Terms({ view, setView, terms }: ViewProps & { terms: readonly ReadTerm[] }) {
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
 * ColumnPicker shows or hides a column for each field and each Attribute
 * key, a Preset or any key in use; a column added goes last, and a column
 * hidden no longer sorts the list.
 */
function ColumnPicker({ view, setView, attrs }: ViewProps & { attrs: readonly AttrKey[] }) {
  const t = useT();
  const keys = new Set([...view.columns, ...Object.keys(hostFields), ...attrs.map((a) => a.key)]);
  keys.delete("name");
  const hide = (key: string): View => ({
    ...view,
    columns: view.columns.filter((c) => c !== key),
    sort: view.sort.filter((s) => readSortKey(s).key !== key),
  });
  return (
    <fieldset className="columns">
      <legend>{t("host.columns")}</legend>
      {[...keys].map((key) => (
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

/** HostTable lists the Hosts host.list answers for query, in the view's columns. */
function HostTable({ view, setView, query, titleId }: ViewProps & { query: ListQuery; titleId: string }) {
  const t = useT();
  const list = useRead(HostList, query);
  const sort = view.sort.map(readSortKey);
  const columns = ["name", ...view.columns];

  const header = (key: string) => {
    const at = sort.findIndex((k) => k.key === key);
    const by = sort[at];
    const click = (e: MouseEvent<HTMLButtonElement>) => setView({ ...view, sort: sortBy(view.sort, key, e.shiftKey) });
    return (
      <th key={key} aria-sort={at === 0 ? (by?.desc ? "descending" : "ascending") : undefined}>
        <button type="button" className="sort" onClick={click} title={t("host.sort.hint")}>
          {key}
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
  };

  return (
    <>
      {list.error !== undefined && <ErrorMessage error={list.error} />}
      {list.data && (
        <table aria-labelledby={titleId}>
          <thead>
            <tr>{columns.map(header)}</tr>
          </thead>
          <tbody>
            {list.data.hosts.map((host) => (
              <tr key={host.id}>
                <td>
                  <Link to={hostPath(host)}>{host.name}</Link>
                </td>
                {view.columns.map((key) => (
                  <td key={key}>{cell(host, key)}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {list.data?.hosts.length === 0 && <p>{t("host.list.none")}</p>}
    </>
  );
}

/**
 * cell is a Host's value of key, as stored: a field's or an Attribute's,
 * price with its currency, and nothing for a key it lacks.
 */
function cell(host: Host, key: string): string {
  if (key === "price" && host.attributes.price !== undefined) {
    return [host.attributes.price, host.attributes.currency].filter((v) => v !== undefined).join(" ");
  }
  const value = Object.hasOwn(hostFields, key) ? host[key as keyof Host] : host.attributes[key];
  return typeof value === "string" || typeof value === "number" ? String(value) : "";
}
