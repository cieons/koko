import "./host.css";

import { Button } from "@astryxdesign/core/Button";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Icon } from "@astryxdesign/core/Icon";
import { Link } from "@astryxdesign/core/Link";
import { List, ListItem } from "@astryxdesign/core/List";
import { Spinner } from "@astryxdesign/core/Spinner";
import { Switch } from "@astryxdesign/core/Switch";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Plus, SearchX } from "lucide-react";
import { type ReactNode, useCallback, useState } from "react";

import { type AttrKey, AttrList } from "../../api/attr";
import { type Host, HostAdd, HostList, HostShow, type NewHost } from "../../api/host";
import type { Service } from "../../api/service";
import { SpaceList } from "../../api/space";
import { FormDialog } from "../../shell/AdaptiveDialog";
import { useCaller } from "../../shell/caller";
import { useRead, useWrite } from "../../shell/data";
import { formatDate } from "../../shell/format";
import { ErrorMessage, type Translate, useT } from "../../shell/i18n";
import { navigate, useSearchParams } from "../../shell/router";
import {
  ColumnOptions,
  FilterBar,
  ListLayout,
  RecordTable,
  selectingLink,
  Toolbar,
  ViewFault,
  type ViewProps,
} from "./ListParts";
import { hostFields, hostPath, servicePath, validPort } from "./record";
import { Field, SpaceField } from "./RecordForm";
import { Attributes, DetailPane, FieldList, Note, StateBadge } from "./RecordView";
import { browserTimeZone } from "./reldate";
import {
  type Clock,
  type Columns,
  keyTypes,
  type ListQuery,
  readQuery,
  type ReadTerm,
  readView,
  type View,
  writeView,
} from "./view";

/**
 * defaultColumns are the columns the list shows after the name unless its
 * URL names others; price shows with its currency.
 */
const defaultColumns = ["space", "state", "address", "provider", "region", "price", "expires_at"];

/** columns are the list's columns besides its keys: the name, always first. */
const columns: Columns = { fixed: ["name"], unsorted: [] };

/**
 * HostsPage lists the Hosts the caller can see, as `koko host ls` does, on
 * Astryx's table-filter pattern, through a view its URL keeps, so a
 * bookmark is a saved view: the PowerSearch tokens, each a filter term as
 * the CLI writes it, the sort keys a click or a Shift-click on a column
 * header sets, the columns the column options choose, and the Retired
 * Hosts when asked for (all=1, as --all). Relative dates stay as written in
 * the URL and are resolved in the browser's time zone on each load. A Host
 * selected opens in the detail pane, which opens its page; Add Host records
 * one.
 */
export function HostsPage() {
  const t = useT();
  const attrs = useRead(AttrList, {});
  const [adding, setAdding] = useState(false);
  const add = (
    <Button
      label={t("host.add.open")}
      variant="primary"
      icon={<Icon icon={Plus} size="sm" />}
      onClick={() => setAdding(true)}
    />
  );
  return (
    <>
      {attrs.data ? (
        <HostsView attrs={attrs.data.attributes} actions={add} />
      ) : (
        <ListLayout title={t("host.list.title")} actions={add}>
          {attrs.error !== undefined ? <ErrorMessage error={attrs.error} /> : <Spinner label={t("shell.loading")} />}
        </ListLayout>
      )}
      {adding && <AddHost onClose={() => setAdding(false)} />}
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
function HostsView({ attrs, actions }: { attrs: readonly AttrKey[]; actions: ReactNode }) {
  const t = useT();
  const [params, setParams] = useSearchParams();
  const view = readView(params, defaultColumns);
  const setView = (next: View) => setParams(writeView(next, defaultColumns));
  const types = keyTypes(hostFields, attrs);
  const clock: Clock = { now: new Date(), timeZone: browserTimeZone() };
  const { query, terms, faults } = readQuery(view, types, clock, columns);
  const retiredAsked = terms.some(asksForRetired);
  if (retiredAsked) {
    query.all = true;
  }
  const toolbar = (count?: number) => (
    <Toolbar
      filter={
        <FilterBar
          label={t("host.filter.label")}
          fields={hostFields}
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
      <Switch
        label={t("host.list.showRetired")}
        value={query.all === true}
        isDisabled={retiredAsked}
        disabledMessage={retiredAsked ? t("host.list.retiredAsked") : undefined}
        onChange={(all) => setView({ ...view, all })}
      />
      <ColumnOptions
        view={view}
        setView={setView}
        keys={[...Object.keys(hostFields), ...attrs.map((a) => a.key)].filter((k) => !columns.fixed.includes(k))}
      />
    </Toolbar>
  );
  return faults.length > 0 ? (
    <ListLayout title={t("host.list.title")} actions={actions} toolbar={toolbar()}>
      <ViewFault message={t("host.view.invalid", { parts: faults.join(", ") })} reset="/hosts" />
    </ListLayout>
  ) : (
    <HostTable view={view} setView={setView} query={query} types={types} actions={actions} toolbar={toolbar} />
  );
}

/** HostTable lists the Hosts host.list answers for query, in the view's columns, with the one selected beside them. */
function HostTable({
  view,
  setView,
  query,
  types,
  actions,
  toolbar,
}: ViewProps & {
  query: ListQuery;
  types: ReturnType<typeof keyTypes>;
  actions: ReactNode;
  toolbar: (count?: number) => ReactNode;
}) {
  const t = useT();
  const list = useRead(HostList, query);
  const [selected, setSelected] = useState<string>();
  const hosts = list.data?.hosts;
  const open = hosts?.find((h) => h.id === selected);
  const close = useCallback(() => setSelected(undefined), []);
  return (
    <ListLayout
      title={t("host.list.title")}
      count={hosts?.length}
      actions={actions}
      toolbar={toolbar(hosts?.length)}
      detail={
        open && {
          label: t("host.detail.label", { name: open.name }),
          content: <HostDetail key={open.id} host={open} onClose={close} />,
          onClose: close,
        }
      }
    >
      {list.error !== undefined && <ErrorMessage error={list.error} />}
      {hosts &&
        (hosts.length === 0 ? (
          <EmptyState icon={<Icon icon={SearchX} />} title={t("host.list.none")} headingLevel={2} />
        ) : (
          <RecordTable
            label={t("host.list.title")}
            records={hosts}
            idOf={hostId}
            columns={columns}
            types={types}
            view={view}
            setView={setView}
            selected={selected}
            onSelect={setSelected}
            cell={(host, key) =>
              key === "name" ? (
                <Link href={hostPath(host)} onClick={selectingLink(() => setSelected(host.id))}>
                  {host.name}
                </Link>
              ) : key === "state" ? (
                <StateBadge state={host.state} />
              ) : (
                hostCell(t, host, key)
              )
            }
          />
        ))}
    </ListLayout>
  );
}

const hostId = (host: Host) => host.id;

/**
 * hostCell is a Host's value of key, as stored: a field's or an Attribute's,
 * price with its currency, its Relay's name, or that it has one the caller
 * cannot see, and nothing for a key it lacks.
 */
function hostCell(t: Translate, host: Host, key: string): string {
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
 * HostDetail is the detail pane of a Host selected in the list: what
 * host.show answers, as on its page, and a button that opens the page.
 */
function HostDetail({ host, onClose }: { host: Host; onClose: () => void }) {
  const t = useT();
  const shown = useRead(HostShow, { host: host.id });
  const current = shown.data?.host ?? host;
  const services = shown.data?.services;
  return (
    <DetailPane
      name={current.name}
      badge={<StateBadge state={current.state} />}
      about={t("host.detail.about", { space: current.space, id: current.id })}
      href={hostPath(current)}
      onClose={onClose}
      sections={[
        <FieldList
          label={t("host.detail.fields")}
          fields={[
            ["address", current.address],
            ["port", current.port],
            ["account", current.account],
            ["relay", current.relay === null ? t("host.relay.none") : (current.relay.name ?? t("host.relay.unseen"))],
            ["host_key", current.host_key ?? t("host.hostKey.none")],
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
        <PaneServices services={services} />,
      ]}
    />
  );
}

/** PaneServices lists a Host's Services in its detail pane, each linking to its page. */
function PaneServices({ services }: { services: readonly Service[] | undefined }) {
  const t = useT();
  return (
    <>
      <Heading level={2}>{t("host.services")}</Heading>
      {services === undefined ? null : services.length === 0 ? (
        <Text type="body" color="secondary">
          {t("host.services.none")}
        </Text>
      ) : (
        <List density="compact" edge="outset">
          {services.map((s) => (
            <ListItem key={s.id} label={s.name} description={s.ports.length === 0 ? t("host.services.noPorts") : s.ports.join(", ")} href={servicePath(s.host, s.name)} />
          ))}
        </List>
      )}
    </>
  );
}

/**
 * AddHost records a Host, in the caller's Personal Space unless they choose
 * another Space they can act in, sending only the fields given; it never
 * asks, and the page moves to the Host, whose page then sets its
 * Attributes and Note.
 */
function AddHost({ onClose }: { onClose: () => void }) {
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
  const set = (key: keyof typeof values) => (value: string) => setValues((v) => ({ ...v, [key]: value }));
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
    <FormDialog
      title={t("host.add.title")}
      submit={t("host.add.submit")}
      ready={ready}
      error={add.error ?? spaces.error}
      onSubmit={submit}
      onClose={onClose}
    >
      <Field label="name" value={name} onChange={set("name")} />
      <SpaceField
        label="space"
        spaces={spaces.data?.spaces.map((s) => s.name) ?? [space]}
        value={space}
        onChange={set("space")}
      />
      <Field label="address" value={address} onChange={set("address")} />
      <Field label="port" value={port} onChange={set("port")} inputMode="numeric" />
      <Field label="account" value={account} onChange={set("account")} />
      <Field label="relay" value={relay} onChange={set("relay")} />
    </FormDialog>
  );
}
