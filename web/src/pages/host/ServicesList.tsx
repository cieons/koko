import { Link } from "react-aria-components";

import { type Service, ServiceList } from "../../api/service";
import { useRead } from "../../shell/data";
import { type Translate, useCount, useT } from "../../shell/i18n";
import { EmptyState, NoMatch } from "../../ui/states";
import { keyLabel } from "./keys";
import { columnsOf, InPanel, type ListProps, Missing, ShownCount, sorter, ViewFault } from "./ListParts";
import { hostPathOf, servicePath } from "./record";
import { type Column, linkClass, RecordTable } from "./RecordTable";
import { matches, serviceValues } from "./search";
import { type ListQuery, readQuery } from "./view";

// The Services view: the Services service.list answers for the view's
// terms and sort keys, never a Retired Host's, narrowed to a Space in the
// browser by each Service's Host, since service.list takes no space key,
// and the search applied; one panel.

/**
 * ServicesList reads the view in the URL as `koko service ls` reads its
 * arguments, and lists what service.list answers for it; a view the list
 * does not take is named in place of the list, and nothing is called. A
 * Service has no Retired state, so all=1 is none of its view.
 */
export function ServicesList(props: ListProps) {
  const t = useT();
  const { query, faults } = readQuery({ ...props.page.list, all: false }, props.types, props.clock, columnsOf.services);
  if (faults.length > 0) {
    return (
      <ViewFault message={t("host.services.view.invalid", { parts: faults.join(", ") })} reset="/hosts?view=services" />
    );
  }
  return <ServicesPanel {...props} query={query} />;
}

function ServicesPanel(props: ListProps & { query: ListQuery }) {
  const read = useRead(ServiceList, props.query);
  return <InPanel read={read}>{({ services }) => <Rows {...props} listed={services} />}</InPanel>;
}

/** Rows are the Services listed that are in the view's Space and match the search, on one panel. */
function Rows({ page, setView, q, hosts, services, clear, listed }: ListProps & { listed: readonly Service[] }) {
  const t = useT();
  const count = useCount();
  if (services.length === 0) {
    return <EmptyState title={t("host.services.empty")} lead={t("host.services.emptyLead")} />;
  }
  const spaceOf = new Map(hosts.map((h) => [h.name, h.space]));
  const shown = listed.filter(
    (s) => (page.space === "" || spaceOf.get(s.host) === page.space) && matches(serviceValues(s), q),
  );
  if (shown.length === 0) {
    return <NoMatch message={t("host.services.noMatch")} clear={t("host.list.clear")} onClear={clear} />;
  }
  const narrowed = q !== "" || page.space !== "" || page.list.filters.length > 0;
  const first: Column<Service> = {
    key: "name",
    label: t("host.key.name"),
    sortable: true,
    cell: (s) => (
      <Link href={servicePath(s.host, s.name)} className={linkClass}>
        {s.name}
      </Link>
    ),
  };
  const host: Column<Service> = {
    key: "host",
    label: t("host.key.host"),
    sortable: true,
    // The Host's own link lies above the row's, so the row opens the Service and the Host cell the Host.
    cell: (s) => (
      <Link
        href={hostPathOf(s.host)}
        className="relative z-10 rounded text-ink-2 underline-offset-2 outline-none hover:underline data-[focus-visible]:ring-2 data-[focus-visible]:ring-focus"
      >
        {s.host}
      </Link>
    ),
  };
  return (
    <>
      {narrowed && <ShownCount>{count("host.services.shown", listed.length, { shown: shown.length })}</ShownCount>}
      <div className="sheet overflow-hidden">
        <RecordTable
          label={t("host.services.title")}
          records={shown}
          idOf={(s) => s.id}
          first={first}
          columns={[host, ...page.list.columns.map((key) => serviceColumn(t, key))]}
          sort={page.list.sort}
          onSort={sorter(page.list, setView)}
          phoneRow={(s) => <PhoneRow service={s} />}
        />
      </div>
    </>
  );
}

/** Ports are a Service's ports in the code face, or that it listens on none. */
function Ports({ ports }: { ports: readonly string[] }) {
  const t = useT();
  return ports.length === 0 ? (
    <span className="t-label">{t("host.services.noPorts")}</span>
  ) : (
    <span className="t-code text-ink-2">{ports.join(", ")}</span>
  );
}

/** PhoneRow is a Service on a phone: one link, its name and Host, then its ports. */
function PhoneRow({ service }: { service: Service }) {
  const t = useT();
  return (
    <Link
      href={servicePath(service.host, service.name)}
      className="row-link flex flex-col gap-1 rounded-none px-4 py-3.5 text-foreground no-underline"
    >
      <span className="flex items-baseline justify-between gap-3">
        <span className="t-name truncate">{service.name}</span>
        <span className="truncate t-label">{t("host.services.on", { host: service.host })}</span>
      </span>
      <Ports ports={service.ports} />
    </Link>
  );
}

/**
 * serviceColumn is the Services view's column of key: its ports, which it
 * neither filters nor sorts on, or a field's or an Attribute's value, as
 * stored.
 */
function serviceColumn(t: Translate, key: string): Column<Service> {
  if (key === "ports") {
    return { key, label: keyLabel(t, key), sortable: false, cell: (s) => <Ports ports={s.ports} /> };
  }
  const cell = (s: Service) => {
    const value = key === "id" ? s.id : s.attributes[key];
    return value === undefined ? <Missing /> : <span className="text-ink-2">{value}</span>;
  };
  return { key, label: keyLabel(t, key), sortable: true, cell };
}
