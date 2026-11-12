import { useEffect } from "react";
import { Link } from "react-aria-components";

import { type Host, HostList } from "../../api/host";
import type { Service } from "../../api/service";
import { useRead } from "../../shell/data";
import { type Translate, useCount, useT } from "../../shell/i18n";
import { usePhone } from "../../ui/phone";
import { NoMatch } from "../../ui/states";
import { StateMark } from "../../ui/StateMark";
import { tickedOf } from "../batch/selection";
import { Tick } from "../batch/Tick";
import { groupBy } from "./group";
import { keyLabel } from "./keys";
import { columnsOf, givesWayOf, InPanel, type ListProps, Missing, ShownCount, sorter, ViewFault } from "./ListParts";
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
 * else by name. In the Select mode (#134) each row has a checkbox and
 * toggles in place of opening its Host, and each group has a checkbox for
 * its rows: a table's header has its own, and the Space's heading has one
 * where Group by splits the Space or, on a phone, where there is no table.
 */
function Groups({
  page,
  setView,
  types,
  q,
  spaces,
  services,
  clear,
  mode,
  listed,
}: ListProps & { listed: readonly Host[] }) {
  const t = useT();
  const count = useCount();
  const phone = usePhone();
  const { selecting, selection, report } = mode;
  const ofHost = byHost(services);
  const servicesOf = (h: Host) => ofHost.get(h.state === "retired" ? h.id : h.name) ?? [];
  const shown = listed.filter((h) => matches(hostValues(h, servicesOf(h)), q));
  const narrowed = q !== "" || page.list.filters.length > 0;
  const grouped = page.group !== "";
  // Group by's subheadings: a Space's Hosts by their value of its key, in the order host.list sorts its type in.
  const byValue = (hosts: readonly Host[]) =>
    groupBy(hosts, (h) => h.attributes[page.group], types(page.group) ?? { type: "text" });
  const sections = [...new Set(shown.map((h) => h.space))].sort().map((space) => {
    const hosts = shown.filter((h) => h.space === space);
    const parts = grouped
      ? byValue(hosts).map(({ value, records }) => ({
          key: value === undefined ? "" : String(value),
          name: value === undefined ? t("host.group.lacks", { key: page.group }) : String(value),
          records,
        }))
      : [];
    return { space, hosts, parts };
  });
  // The Hosts in the order the page shows them, which a Shift-click's range and "Select all" follow.
  const order = sections.flatMap((s) => (grouped ? s.parts.flatMap((p) => p.records) : s.hosts)).map((h) => h.id);
  const orderKey = order.join(",");
  useEffect(() => {
    report(orderKey === "" ? [] : orderKey.split(","), narrowed);
  }, [report, orderKey, narrowed]);

  if (shown.length === 0) {
    return <NoMatch message={t("host.list.noMatch")} clear={t("host.list.clear")} onClear={clear} />;
  }
  const personal = new Set(spaces.filter((s) => s.personal).map((s) => s.name));
  const columns = page.list.columns.map((key) => hostColumn(t, key, page.list.columns));
  const first: Column<Host> = {
    key: "name",
    label: t("host.key.name"),
    sortable: true,
    cell: (h) => <HostName host={h} services={servicesOf(h)} link={!selecting} />,
  };
  const ids = (hosts: readonly Host[]) => hosts.map((h) => h.id);
  const tickRow = (h: Host, on: boolean, shift: boolean) => selection.toggle(h.id, on, shift ? order : undefined);
  const groupLabel = (hosts: readonly Host[], group: string) =>
    t("batch.tickGroup", { things: count("host.count.hosts", hosts.length), group });
  const groupTick = (hosts: readonly Host[], group: string) => (
    <Tick
      label={groupLabel(hosts, group)}
      ticked={tickedOf(selection, ids(hosts))}
      onChange={(on) => selection.setMany(ids(hosts), on)}
      // In line with the rows' checkboxes: a table's on a desktop, a list's on a phone.
      className="-my-3 md:ml-1"
    />
  );
  return (
    <>
      {narrowed && <ShownCount>{count("host.list.shown", listed.length, { shown: shown.length })}</ShownCount>}
      <div className="flex flex-col gap-6">
        {sections.map(({ space, hosts, parts }) => {
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
                phoneRow={(h) => (
                  <PhoneRow
                    host={h}
                    tick={selecting ? { ticked: selection.has(h.id), onTick: (on) => tickRow(h, on, false) } : undefined}
                  />
                )}
                ticks={
                  selecting
                    ? {
                        ticked: (h) => selection.has(h.id),
                        onTick: tickRow,
                        labelOf: (h) => t("batch.tick", { name: h.name }),
                        all: tickedOf(selection, ids(records)),
                        onAll: (on) => selection.setMany(ids(records), on),
                        allLabel: groupLabel(records, label),
                      }
                    : undefined
                }
              />
            </div>
          );
          return (
            <section key={space} aria-labelledby={id}>
              <h2 className="t-heading mb-2 flex min-h-6 items-center gap-2 px-1">
                {selecting && (grouped || phone) && groupTick(hosts, space)}
                <span id={id}>{space}</span>
                <span className="t-num font-normal text-muted">{hosts.length}</span>
                {personal.has(space) && <span className="t-label font-normal">{t("host.list.personal")}</span>}
              </h2>
              {!grouped ? (
                panel(space, hosts)
              ) : (
                <div className="flex flex-col gap-4">
                  {parts.map(({ key, name, records }) => {
                    const label = t("host.group.table", { space, group: name });
                    return (
                      <div key={key}>
                        <h3 className="t-label mb-1.5 flex min-h-6 items-center gap-2 px-1 text-ink-2">
                          {selecting && phone && groupTick(records, label)}
                          {name}
                          <span className="t-num text-muted">{records.length}</span>
                        </h3>
                        {panel(label, records)}
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

/** HostName is a row's first cell: the link to the Host, or its name alone where the row toggles, and its Services' names after it. */
function HostName({ host, services, link }: { host: Host; services: readonly Service[]; link: boolean }) {
  return (
    <>
      {link ? (
        <Link href={hostPath(host)} className={linkClass}>
          {host.name}
        </Link>
      ) : (
        <span className="t-name">{host.name}</span>
      )}
      {services.length > 0 && (
        <span className="ml-3 t-label">{services.map((s) => s.name).join(", ")}</span>
      )}
    </>
  );
}

/**
 * PhoneRow is a Host on a phone: one link of three lines, its name and
 * state, its address, and its provider, region and expiry. With tick, in
 * the Select mode, the row is no link: a checkbox before the lines, and a
 * tap anywhere toggles it.
 */
function PhoneRow({ host, tick }: { host: Host; tick?: { ticked: boolean; onTick: (on: boolean) => void } }) {
  const t = useT();
  const a = host.attributes;
  const where = [a.provider, a.region].filter((v) => v !== undefined).join(" · ");
  const lines = (
    <>
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
    </>
  );
  if (tick) {
    return (
      <div
        onClick={() => tick.onTick(!tick.ticked)}
        className={`flex cursor-pointer items-center select-none ${tick.ticked ? "bg-accent-soft" : ""}`}
      >
        <Tick label={t("batch.tick", { name: host.name })} ticked={tick.ticked} onChange={tick.onTick} className="ml-1" />
        <span className="flex min-w-0 flex-1 flex-col gap-1.5 py-3.5 pr-4 pl-0.5">{lines}</span>
      </div>
    );
  }
  return (
    <Link
      href={hostPath(host)}
      className="row-link flex flex-col gap-1.5 rounded-none px-4 py-3.5 text-foreground no-underline"
    >
      {lines}
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
  return { key, label, sortable: true, cell, givesWay: givesWayOf[key] };
}
