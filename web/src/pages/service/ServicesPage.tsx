import "../host/host.css";

import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Icon } from "@astryxdesign/core/Icon";
import { Link } from "@astryxdesign/core/Link";
import { Spinner } from "@astryxdesign/core/Spinner";
import { SearchX } from "lucide-react";
import { type ReactNode, useCallback, useState } from "react";

import { type AttrKey, AttrList } from "../../api/attr";
import { type Service, ServiceList, ServiceShow } from "../../api/service";
import { useRead } from "../../shell/data";
import { formatDate } from "../../shell/format";
import { ErrorMessage, useT } from "../../shell/i18n";
import { useSearchParams } from "../../shell/router";
import {
  ColumnOptions,
  FilterBar,
  ListLayout,
  RecordTable,
  selectingLink,
  Toolbar,
  ViewFault,
  type ViewProps,
} from "../host/ListParts";
import { hostPathOf, qualified, servicePath } from "../host/record";
import { Attributes, DetailPane, FieldList, Note } from "../host/RecordView";
import { browserTimeZone } from "../host/reldate";
import {
  type Clock,
  type Columns,
  type Fields,
  keyTypes,
  type KeyTypes,
  type ListQuery,
  readQuery,
  readView,
  type View,
  writeView,
} from "../host/view";

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
 * `koko service ls` does, on the Hosts list's pattern, through a view its
 * URL keeps: the PowerSearch tokens, the sort keys a click or a Shift-click
 * on a column header sets, and the columns the column options choose. A
 * Retired Host's Services never show here. A Service selected opens in the
 * detail pane, which opens its page.
 */
export function ServicesPage() {
  const t = useT();
  const attrs = useRead(AttrList, {});
  return attrs.data ? (
    <ServicesView attrs={attrs.data.attributes} />
  ) : (
    <ListLayout title={t("service.list.title")}>
      {attrs.error !== undefined ? <ErrorMessage error={attrs.error} /> : <Spinner label={t("shell.loading")} />}
    </ListLayout>
  );
}

/** ServicesView is the list and its controls, once the Attribute keys and their types are known. */
function ServicesView({ attrs }: { attrs: readonly AttrKey[] }) {
  const t = useT();
  const [params, setParams] = useSearchParams();
  const view = readView(params, defaultColumns);
  const setView = (next: View) => setParams(writeView(next, defaultColumns));
  const types = keyTypes(serviceFields, attrs);
  const clock: Clock = { now: new Date(), timeZone: browserTimeZone() };
  // A Service has no Retired state, so the view's all is none of the list's.
  const { query, terms, faults } = readQuery({ ...view, all: false }, types, clock, columns);
  const toolbar = (count?: number) => (
    <Toolbar
      filter={
        <FilterBar
          label={t("service.filter.label")}
          fields={serviceFields}
          attrs={attrs}
          types={types}
          clock={clock}
          terms={terms}
          view={view}
          setView={setView}
          count={count}
        />
      }
    >
      <ColumnOptions
        view={view}
        setView={setView}
        keys={[...Object.keys(serviceFields), ...columns.unsorted, ...attrs.map((a) => a.key)].filter(
          (k) => !columns.fixed.includes(k),
        )}
      />
    </Toolbar>
  );
  return faults.length > 0 ? (
    <ListLayout title={t("service.list.title")} toolbar={toolbar()}>
      <ViewFault message={t("service.view.invalid", { parts: faults.join(", ") })} reset="/services" />
    </ListLayout>
  ) : (
    <ServiceTable view={view} setView={setView} query={query} types={types} toolbar={toolbar} />
  );
}

/** ServiceTable lists the Services service.list answers for query, in the view's columns, with the one selected beside them. */
function ServiceTable({
  view,
  setView,
  query,
  types,
  toolbar,
}: ViewProps & { query: ListQuery; types: KeyTypes; toolbar: (count?: number) => ReactNode }) {
  const t = useT();
  const list = useRead(ServiceList, query);
  const [selected, setSelected] = useState<string>();
  const services = list.data?.services;
  const open = services?.find((s) => s.id === selected);
  const close = useCallback(() => setSelected(undefined), []);
  return (
    <ListLayout
      title={t("service.list.title")}
      count={services?.length}
      toolbar={toolbar(services?.length)}
      detail={
        open && {
          label: t("service.detail.label", { name: qualified(open) }),
          content: <ServiceDetail key={open.id} service={open} onClose={close} />,
          onClose: close,
        }
      }
    >
      {list.error !== undefined && <ErrorMessage error={list.error} />}
      {services &&
        (services.length === 0 ? (
          <EmptyState icon={<Icon icon={SearchX} />} title={t("service.list.none")} headingLevel={2} />
        ) : (
          <RecordTable
            label={t("service.list.title")}
            records={services}
            idOf={serviceId}
            columns={columns}
            types={types}
            view={view}
            setView={setView}
            selected={selected}
            onSelect={setSelected}
            cell={(service, key) =>
              key === "host" ? (
                <Link href={hostPathOf(service.host)}>{service.host}</Link>
              ) : key === "name" ? (
                <Link
                  href={servicePath(service.host, service.name)}
                  onClick={selectingLink(() => setSelected(service.id))}
                >
                  {service.name}
                </Link>
              ) : (
                serviceCell(service, key)
              )
            }
          />
        ))}
    </ListLayout>
  );
}

const serviceId = (service: Service) => service.id;

/**
 * serviceCell is a Service's value of key, as stored: its id, its ports, or an
 * Attribute's, and nothing for a key it lacks.
 */
function serviceCell(service: Service, key: string): string {
  if (key === "id") {
    return service.id;
  }
  if (key === "ports") {
    return service.ports.join(", ");
  }
  const value = service.attributes[key];
  return value === undefined ? "" : String(value);
}

/**
 * ServiceDetail is the detail pane of a Service selected in the list: what
 * service.show answers, as on its page, and a button that opens the page.
 */
function ServiceDetail({ service, onClose }: { service: Service; onClose: () => void }) {
  const t = useT();
  const shown = useRead(ServiceShow, { host: service.host, name: service.name });
  const current = shown.data?.service ?? service;
  return (
    <DetailPane
      name={qualified(current)}
      about={current.id}
      href={servicePath(current.host, current.name)}
      onClose={onClose}
      sections={[
        <FieldList
          label={t("host.detail.fields")}
          fields={[
            ["ports", current.ports.join(", ")],
            ["updated_at", formatDate(current.updated_at)],
          ]}
        />,
        <Attributes attributes={current.attributes} />,
        shown.error !== undefined ? (
          <ErrorMessage error={shown.error} />
        ) : shown.data === undefined ? (
          <Spinner label={t("shell.loading")} />
        ) : (
          <Note note={current.note} />
        ),
      ]}
    />
  );
}
