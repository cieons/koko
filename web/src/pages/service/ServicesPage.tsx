import "../host/host.css";

import { useId } from "react";

import { type AttrKey, AttrList } from "../../api/attr";
import { type Service, ServiceList } from "../../api/service";
import { useRead } from "../../shell/data";
import { ErrorMessage, useT } from "../../shell/i18n";
import { Link, useSearchParams } from "../../shell/router";
import { FilterBuilder } from "../host/FilterBuilder";
import { ColumnPicker, SortHeader, Terms, type ViewProps } from "../host/ListControls";
import { browserTimeZone } from "../host/reldate";
import {
  type Clock,
  type Columns,
  type Fields,
  keyTypes,
  type ListQuery,
  readQuery,
  readView,
  type View,
  writeView,
} from "../host/view";
import { hostPathOf, servicePath } from "../host/record";

/**
 * serviceFields are the fields service.list filters and sorts on, with
 * their types, as api.ServiceListFields has them; every other key is an
 * Attribute key.
 */
const serviceFields: Fields = {
  id: { type: "text" },
  name: { type: "text" },
  host: { type: "text" },
};

/**
 * columns are the list's columns besides its keys: the Host and the name,
 * always first, and the ports, which it neither filters nor sorts on.
 */
const columns: Columns = { fixed: ["host", "name"], unsorted: ["ports"] };

/** defaultColumns are the columns the list shows after the Host and the name unless its URL names others. */
const defaultColumns = ["ports"];

/**
 * ServicesPage lists the Services on the Hosts the caller can see, as
 * `koko service ls` does, through a view its URL keeps, as the Hosts list
 * does: the filter terms built in three steps, the sort keys a click or a
 * Shift-click on a column header sets, and the columns the picker chooses.
 * A Retired Host's Services never show here.
 */
export function ServicesPage() {
  const t = useT();
  const titleId = useId();
  const [params, setParams] = useSearchParams();
  const attrs = useRead(AttrList, {});
  const view = readView(params, defaultColumns);
  const setView = (next: View) => setParams(writeView(next, defaultColumns));

  return (
    <>
      <h1 id={titleId}>{t("service.list.title")}</h1>
      {attrs.error !== undefined && <ErrorMessage error={attrs.error} />}
      {attrs.data && <ServicesView view={view} setView={setView} attrs={attrs.data.attributes} titleId={titleId} />}
    </>
  );
}

/** ServicesView is the list and its controls, once the Attribute keys and their types are known. */
function ServicesView({ view, setView, attrs, titleId }: ViewProps & { attrs: readonly AttrKey[]; titleId: string }) {
  const t = useT();
  const types = keyTypes(serviceFields, attrs);
  const clock: Clock = { now: new Date(), timeZone: browserTimeZone() };
  // A Service has no Retired state, so the view's all is none of the list's.
  const { query, terms, faults } = readQuery({ ...view, all: false }, types, clock, columns);

  return (
    <>
      <FilterBuilder
        fields={serviceFields}
        attrs={attrs}
        types={types}
        clock={clock}
        onAdd={(term) => setView({ ...view, filters: [...view.filters, term] })}
      />
      <Terms view={view} setView={setView} terms={terms} />
      <ColumnPicker
        view={view}
        setView={setView}
        keys={[...Object.keys(serviceFields), ...columns.unsorted, ...attrs.map((a) => a.key)].filter(
          (k) => !columns.fixed.includes(k),
        )}
      />
      {faults.length > 0 ? (
        <p className="error" role="alert">
          {t("service.view.invalid", { parts: faults.join(", ") })}{" "}
          <Link to="/services">{t("service.view.reset")}</Link>
        </p>
      ) : (
        <ServiceTable view={view} setView={setView} query={query} titleId={titleId} />
      )}
    </>
  );
}

/** ServiceTable lists the Services service.list answers for query, in the view's columns. */
function ServiceTable({ view, setView, query, titleId }: ViewProps & { query: ListQuery; titleId: string }) {
  const t = useT();
  const list = useRead(ServiceList, query);
  return (
    <>
      {list.error !== undefined && <ErrorMessage error={list.error} />}
      {list.data && (
        <table aria-labelledby={titleId}>
          <thead>
            <tr>
              {[...columns.fixed, ...view.columns].map((key) =>
                columns.unsorted.includes(key) ? (
                  <th key={key}>{key}</th>
                ) : (
                  <SortHeader key={key} view={view} setView={setView} column={key} />
                ),
              )}
            </tr>
          </thead>
          <tbody>
            {list.data.services.map((service) => (
              <tr key={service.id}>
                <td>
                  <Link to={hostPathOf(service.host)}>{service.host}</Link>
                </td>
                <td>
                  <Link to={servicePath(service.host, service.name)}>{service.name}</Link>
                </td>
                {view.columns.map((key) => (
                  <td key={key}>{cell(service, key)}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {list.data?.services.length === 0 && <p>{t("service.list.none")}</p>}
    </>
  );
}

/**
 * cell is a Service's value of key, as stored: its id, its ports, or an
 * Attribute's, and nothing for a key it lacks.
 */
function cell(service: Service, key: string): string {
  if (key === "id") {
    return service.id;
  }
  if (key === "ports") {
    return service.ports.join(", ");
  }
  const value = service.attributes[key];
  return value === undefined ? "" : String(value);
}
