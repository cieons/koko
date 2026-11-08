import "./host.css";

import { useId, useState } from "react";

import { type AttrKey, AttrList } from "../../api/attr";
import { type Host, HostAdd, HostList, type NewHost } from "../../api/host";
import { SpaceList } from "../../api/space";
import { useCaller } from "../../shell/caller";
import { useRead, useWrite } from "../../shell/data";
import { ErrorMessage, type Translate, useT } from "../../shell/i18n";
import { Link, navigate, useSearchParams } from "../../shell/router";
import { FilterBuilder } from "./FilterBuilder";
import { ColumnPicker, SortHeader, Terms, type ViewProps } from "./ListControls";
import { hostPath, validPort } from "./record";
import { ActionForm, Field } from "./RecordForm";
import { browserTimeZone } from "./reldate";
import {
  type Clock,
  type Columns,
  type Fields,
  keyTypes,
  type ListQuery,
  readQuery,
  type ReadTerm,
  readView,
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
  relay: { type: "text" },
  state: { type: "enum", values: ["registered", "managed", "retired"] },
  host_key: { type: "text" },
};

/**
 * defaultColumns are the columns the list shows after the name unless its
 * URL names others; price shows with its currency.
 */
const defaultColumns = ["space", "state", "address", "provider", "region", "price", "expires_at"];

/** columns are the list's columns besides its keys: the name, always first. */
const columns: Columns = { fixed: ["name"], unsorted: [] };

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
      <AddHost />
    </>
  );
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
  const { query, terms, faults } = readQuery(view, types, clock, columns);
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
      <ColumnPicker
        view={view}
        setView={setView}
        keys={[...Object.keys(hostFields), ...attrs.map((a) => a.key)].filter((k) => !columns.fixed.includes(k))}
      />
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

/** HostTable lists the Hosts host.list answers for query, in the view's columns. */
function HostTable({ view, setView, query, titleId }: ViewProps & { query: ListQuery; titleId: string }) {
  const t = useT();
  const list = useRead(HostList, query);
  return (
    <>
      {list.error !== undefined && <ErrorMessage error={list.error} />}
      {list.data && (
        <table aria-labelledby={titleId}>
          <thead>
            <tr>
              {[...columns.fixed, ...view.columns].map((key) => (
                <SortHeader key={key} view={view} setView={setView} column={key} />
              ))}
            </tr>
          </thead>
          <tbody>
            {list.data.hosts.map((host) => (
              <tr key={host.id}>
                <td>
                  <Link to={hostPath(host)}>{host.name}</Link>
                </td>
                {view.columns.map((key) => (
                  <td key={key}>{cell(t, host, key)}</td>
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
 * price with its currency, its Relay's name, or that it has one the caller
 * cannot see, and nothing for a key it lacks.
 */
function cell(t: Translate, host: Host, key: string): string {
  if (key === "relay") {
    return host.relay === null ? "" : (host.relay.name ?? t("host.relay.unseen"));
  }
  if (key === "price" && host.attributes.price !== undefined) {
    return [host.attributes.price, host.attributes.currency].filter((v) => v !== undefined).join(" ");
  }
  const value = Object.hasOwn(hostFields, key) ? host[key as keyof Host] : host.attributes[key];
  return typeof value === "string" || typeof value === "number" ? String(value) : "";
}

/**
 * AddHost records a Host, in the caller's Personal Space unless they choose
 * another Space they can act in, sending only the fields given; it never
 * asks, and the page moves to the Host, whose page then sets its
 * Attributes and Note.
 */
function AddHost() {
  const t = useT();
  const [open, setOpen] = useState(false);
  return open ? (
    <AddHostForm onCancel={() => setOpen(false)} />
  ) : (
    <button type="button" onClick={() => setOpen(true)}>
      {t("host.add.title")}
    </button>
  );
}

/** AddHostForm is the form AddHost opens. */
function AddHostForm({ onCancel }: { onCancel: () => void }) {
  const t = useT();
  const caller = useCaller();
  const spaces = useRead(SpaceList, {});
  const [values, setValues] = useState({
    name: "",
    space: caller.user.name,
    address: "",
    port: "",
    account: "",
    relay: "",
  });
  const add = useWrite(HostAdd);
  const field = (key: keyof typeof values) => (
    <Field name={key} value={values[key]} onChange={(value) => setValues((v) => ({ ...v, [key]: value }))} />
  );
  const { name, space, address, port, account, relay } = values;
  const ready = name !== "" && address !== "" && account !== "" && (port === "" || validPort(port)) && !add.pending;
  const submit = () => {
    const host: NewHost = {
      name,
      space,
      address,
      ...(port !== "" && { port: Number(port) }),
      account,
      ...(relay !== "" && { relay }),
    };
    void add.submit({ hosts: [host] }).then((added) => {
      const [recorded] = added?.hosts ?? [];
      if (recorded) {
        navigate(hostPath(recorded));
      }
    });
  };
  return (
    <ActionForm
      title={t("host.add.title")}
      submit={t("host.add.submit")}
      ready={ready}
      error={add.error ?? spaces.error}
      onSubmit={submit}
      onCancel={onCancel}
    >
      {field("name")}
      <label>
        space{" "}
        <select value={space} onChange={(e) => setValues((v) => ({ ...v, space: e.target.value }))}>
          {(spaces.data?.spaces.map((s) => s.name) ?? [space]).map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
      </label>
      {field("address")}
      {field("port")}
      {field("account")}
      {field("relay")}
    </ActionForm>
  );
}
