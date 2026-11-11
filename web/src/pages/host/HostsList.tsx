import { Link } from "react-aria-components";

import { type Host, HostList } from "../../api/host";
import type { Service } from "../../api/service";
import { useRead } from "../../shell/data";
import { type Translate, useCount, useT } from "../../shell/i18n";
import { NoMatch } from "../../ui/states";
import { StateMark } from "../../ui/StateMark";
import { groupBy } from "./group";
import { keyLabel } from "./keys";
import { columnsOf, InPanel, type ListProps, Missing, ShownCount, sorter, ViewFault } from "./ListParts";
import { hostFields, hostPath } from "./record";
import { type Column, linkClass, RecordTable } from "./RecordTable";
import { byHost, hostValues, matches } from "./search";
import { type ListQuery, readQuery, type ReadTerm, validAttrKey } from "./view";

// The Hosts view: the Hosts host.list answers for the view's terms and sort
// keys, the search applied, grouped by Space, a panel each, or, with Group
// by, a panel per value of an Attribute key within each Space.

/**
 * asksForRetired tells whether a term keeps only Retired Hosts, which
 * host.list answers only with all, so the page sends all with it.
 */
function asksForRetired({ filter }: ReadTerm): boolean {
  return filter?.key === "state" && filter.op === "eq" && filter.value === "retired";
}

/**
 * HostsList reads the view in the URL as `koko host ls` reads its
 * arguments, and lists what host.list answers for it; a view the list does
 * not take is named in place of the list, and nothing is called.
 */
export function HostsList(props: ListProps) {
  const t = useT();
  const { query, terms, faults } = readQuery(props.page.list, props.types, props.clock, columnsOf.hosts);
  if (terms.some(asksForRetired)) {
    query.all = true;
  }
  // Group by takes an Attribute key alone, not a field such as state.
  const { group } = props.page;
  if (group !== "" && !validAttrKey(group)) {
    faults.push(group);
  }
  if (faults.length > 0) {
    return <ViewFault message={t("host.view.invalid", { parts: faults.join(", ") })} reset={props.clear} />;
  }
  return <HostGroups {...props} query={query} />;
}

function HostGroups(props: ListProps & { query: ListQuery }) {
  const read = useRead(HostList, { ...props.query, with_note: true });
  return <InPanel read={read}>{({ hosts }) => <Groups {...props} listed={hosts} />}</InPanel>;
}

/**
 * Groups are the Hosts listed that match the search, a section per Space,
 * in the order of the Spaces' names, and in it a panel, or, with Group by,
 * a subheading and a panel per value of its key (byValue); within a panel
 * the Hosts keep the order host.list answered them in, the view's sort or
 * else by name.
 */
function Groups({ page, setView, types, q, spaces, services, clear, listed }: ListProps & { listed: readonly Host[] }) {
  const t = useT();
  const count = useCount();
  const ofHost = byHost(services);
  const servicesOf = (h: Host) => ofHost.get(h.state === "retired" ? h.id : h.name) ?? [];
  const shown = listed.filter((h) => matches(hostValues(h, servicesOf(h)), q));
  const narrowed = q !== "" || page.list.filters.length > 0;
  if (shown.length === 0) {
    return <NoMatch message={t("host.list.noMatch")} clear={t("host.list.clear")} onClear={clear} />;
  }
  const personal = new Set(spaces.filter((s) => s.personal).map((s) => s.name));
  const groups = [...new Set(shown.map((h) => h.space))].sort();
  const columns = page.list.columns.map((key) => hostColumn(t, key, page.list.columns));
  // Group by's subheadings: a Space's Hosts by their value of its key, in the order host.list sorts its type in.
  const byValue = (hosts: readonly Host[]) =>
    groupBy(hosts, (h) => h.attributes[page.group], types(page.group) ?? { type: "text" });
  const first: Column<Host> = {
    key: "name",
    label: t("host.key.name"),
    sortable: true,
    cell: (h) => <HostName host={h} services={servicesOf(h)} />,
  };
  return (
    <>
      {narrowed && <ShownCount>{count("host.list.shown", listed.length, { shown: shown.length })}</ShownCount>}
      <div className="flex flex-col gap-6">
        {groups.map((space) => {
          const hosts = shown.filter((h) => h.space === space);
          const id = `space-${space}`;
          const panel = (label: string, records: readonly Host[]) => (
            <div className="sheet overflow-hidden">
              <RecordTable
                label={label}
                records={records}
                idOf={(h) => h.id}
                first={first}
                columns={columns}
                sort={page.list.sort}
                onSort={sorter(page.list, setView)}
                phoneRow={(h) => <PhoneRow host={h} />}
              />
            </div>
          );
          return (
            <section key={space} aria-labelledby={id}>
              <h2 id={id} className="t-heading mb-2 flex items-baseline gap-2 px-1">
                {space}
                <span className="t-num font-normal text-muted">{hosts.length}</span>
                {personal.has(space) && <span className="t-label font-normal">{t("host.list.personal")}</span>}
              </h2>
              {page.group === "" ? (
                panel(space, hosts)
              ) : (
                <div className="flex flex-col gap-4">
                  {byValue(hosts).map(({ value, records }) => {
                    const name = value === undefined ? t("host.group.lacks", { key: page.group }) : String(value);
                    return (
                      <div key={value === undefined ? "" : String(value)}>
                        <h3 className="t-label mb-1.5 flex items-baseline gap-2 px-1 text-ink-2">
                          {name}
                          <span className="t-num text-muted">{records.length}</span>
                        </h3>
                        {panel(t("host.group.table", { space, group: name }), records)}
                      </div>
                    );
                  })}
                </div>
              )}
            </section>
          );
        })}
      </div>
    </>
  );
}

/** HostName is a row's first cell: the link to the Host, and its Services' names after it. */
function HostName({ host, services }: { host: Host; services: readonly Service[] }) {
  return (
    <>
      <Link href={hostPath(host)} className={linkClass}>
        {host.name}
      </Link>
      {services.length > 0 && (
        <span className="ml-3 t-label">{services.map((s) => s.name).join(", ")}</span>
      )}
    </>
  );
}

/** PhoneRow is a Host on a phone: one link of three lines, its name and state, its address, and its provider, region and expiry. */
function PhoneRow({ host }: { host: Host }) {
  const a = host.attributes;
  const where = [a.provider, a.region].filter((v) => v !== undefined).join(" · ");
  return (
    <Link
      href={hostPath(host)}
      className="row-link flex flex-col gap-1.5 rounded-none px-4 py-3.5 text-foreground no-underline"
    >
      <span className="flex items-center justify-between gap-3">
        <span className="t-name truncate">{host.name}</span>
        <StateMark state={host.state} />
      </span>
      <span className="t-code truncate text-ink-2">{address(host, [])}</span>
      {(where !== "" || a.expires_at !== undefined) && (
        <span className="flex items-baseline justify-between gap-3 t-label">
          <span className="truncate">{where}</span>
          {a.expires_at !== undefined && <span className="t-num shrink-0">{a.expires_at}</span>}
        </span>
      )}
    </Link>
  );
}

/** address is a Host's address, with :port when its port is not 22 and no column of its own shows it. */
function address(host: Host, columns: readonly string[]): string {
  return host.port !== 22 && !columns.includes("port") ? `${host.address}:${host.port}` : host.address;
}

/** valueOf is a Host's value of key, a field's or an Attribute's, as stored, or undefined when it lacks one. */
function valueOf(host: Host, key: string): string | number | undefined {
  if (Object.hasOwn(hostFields, key)) {
    const value = host[key as keyof Host];
    return typeof value === "string" || typeof value === "number" ? value : undefined;
  }
  return host.attributes[key];
}

/** machine are the keys whose values are machine values, set in the code face. */
const machine = new Set(["id", "address", "port", "account", "host_key"]);

/**
 * hostColumn is the Hosts view's column of key. Provider shows its region
 * after it, or the region alone with no provider, and Price its currency and billing cycle, unless a column of
 * their own shows them; State shows its mark; a Relay its name, or that the
 * caller cannot see it. Every other value shows as stored.
 */
function hostColumn(t: Translate, key: string, columns: readonly string[]): Column<Host> {
  const own = (k: string) => columns.includes(k);
  const label = key === "provider" && !own("region") ? t("host.column.providerRegion") : keyLabel(t, key);
  const cell = (h: Host) => {
    const a = h.attributes;
    switch (key) {
      case "provider": {
        const region = own("region") ? undefined : a.region;
        if (a.provider === undefined) {
          // The region alone, as the cell's value, with no separator before it.
          return region === undefined ? <Missing /> : <span className="text-ink-2">{region}</span>;
        }
        return (
          <span className="text-ink-2">
            {a.provider}
            {region !== undefined && <span className="text-muted"> · {region}</span>}
          </span>
        );
      }
      case "price":
        return a.price === undefined ? (
          <Missing />
        ) : (
          <span className="t-num">
            {[a.price, own("currency") ? undefined : a.currency].filter((v) => v !== undefined).join(" ")}
            {!own("billing_cycle") && a.billing_cycle !== undefined && (
              <span className="t-label"> {a.billing_cycle}</span>
            )}
          </span>
        );
      case "address":
        return <span className="t-code text-ink-2">{address(h, columns)}</span>;
      case "state":
        return <StateMark state={h.state} />;
      case "relay":
        return h.relay === null ? (
          <Missing />
        ) : (
          <span className="text-ink-2">{h.relay.name ?? t("host.relay.unseen")}</span>
        );
      default: {
        const value = valueOf(h, key);
        return value === undefined ? (
          <Missing />
        ) : (
          <span className={`${machine.has(key) ? "t-code" : "t-num"} text-ink-2`}>{value}</span>
        );
      }
    }
  };
  return { key, label, sortable: true, cell };
}
