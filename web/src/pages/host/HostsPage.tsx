import { Button, SearchField, Tabs } from "@heroui/react";
import { Plus } from "lucide-react";
import { lazy, Suspense, useState } from "react";

import { type AttrKey, AttrList, type AttrListResponse } from "../../api/attr";
import { type Host, HostList, type HostListResponse } from "../../api/host";
import { type Service, ServiceList, type ServiceListResponse } from "../../api/service";
import { type Space, SpaceList, type SpaceListResponse } from "../../api/space";
import { type Read, useRead } from "../../shell/data";
import { useCount, useT } from "../../shell/i18n";
import { type RememberedList, ResetView, type RememberedView, useRememberedView } from "../../shell/rememberedView";
import { Choice, listSearchClass, SearchChoice } from "../../ui/fields";
import { Page } from "../../ui/Page";
import { usePhone } from "../../ui/phone";
import { EmptyState } from "../../ui/states";
import {
  chooseSpace,
  chooseState,
  type ListPage,
  readListPage,
  selected,
  type StateChoice,
  stateChoice,
  stateTerm,
  switchTab,
  type Tab,
  writeListPage,
} from "./controls";
import { Filter, TermChips } from "./Filter";
import { useHostSelectMode } from "./HostBatch";
import { HostsList } from "./HostsList";
import { keyLabel } from "./keys";
import { columnsOf, defaults, fieldsOf, InPanel, type ListProps } from "./ListParts";
import { ColumnsButton } from "./RecordTable";
import { browserTimeZone } from "./reldate";
import { ServicesList } from "./ServicesList";
import { type Clock, keyTypes, type View } from "./view";

// Add Host's form brings the calendar with it, so it loads when it is first opened.
const AddHost = lazy(() => import("./HostForm").then((m) => ({ default: m.AddHost })));

/**
 * HostsPage is /hosts (docs/web-design.md, "Hosts"): the Hosts the caller
 * can see, as `koko host ls` lists them, grouped by Space, or, with
 * view=services, their Services, as `koko service ls` lists them; a search,
 * the Space and state selects, Filter, Group by, sorting by a header and
 * the columns button over them, each kept in the URL in koko's grammar,
 * which the browser remembers for each view (useRememberedView), with
 * Reset view; Select, which starts the Hosts view's Select mode
 * (useHostSelectMode); and Add Host.
 */
export function HostsPage() {
  const t = useT();
  const count = useCount();
  const view = useRememberedView(listOf);
  const page = readListPage(view.params, defaults);
  const setPage = (next: ListPage, replace = false) => view.setParams(writeListPage(next, defaults), { replace });
  const attrs = useRead(AttrList, {});
  const spaces = useRead(SpaceList, {});
  // Every Host recorded, Retired ones too: the page is empty only with none, and counts those in use.
  const hosts = useRead(HostList, { all: true });
  const services = useRead(ServiceList, {});
  const [adding, setAdding] = useState(false);
  const recorded = hosts.data?.hosts;
  const inUse = recorded?.filter((h) => h.state !== "retired");
  const add = (
    <Button onPress={() => setAdding(true)}>
      <Plus aria-hidden className="size-4" />
      {t("host.add.open")}
    </Button>
  );
  const lead =
    inUse && services.data
      ? t("host.list.lead", {
          hosts: count("host.count.hosts", inUse.length),
          spaces: count("host.count.spaces", new Set(inUse.map((h) => h.space)).size),
          services: count("host.count.services", services.data.services.length),
        })
      : undefined;
  const asDefault = page.q === "" && page.space === "" && page.list.filters.length === 0 && !page.list.all;
  return (
    <Page title={t("host.list.title")} lead={lead} actions={add}>
      {recorded?.length === 0 && asDefault ? (
        <EmptyState title={t("host.list.empty")} lead={t("host.list.emptyLead")} action={add} />
      ) : (
        <InPanel read={joined(attrs, spaces, hosts, services)}>
          {([a, s, h, v]) => (
            <ListArea
              page={page}
              setPage={setPage}
              remembered={view}
              attrs={a.attributes}
              spaces={s.spaces}
              hosts={h.hosts.filter((host) => host.state !== "retired")}
              recorded={h.hosts}
              services={v.services}
            />
          )}
        </InPanel>
      )}
      {adding && (
        <Suspense>
          <AddHost
            spaces={spaces.data?.spaces}
            hosts={inUse}
            attrs={attrs.data?.attributes}
            onClose={() => setAdding(false)}
          />
        </Suspense>
      )}
    </Page>
  );
}

/**
 * listOf names the list a query is a view of: the Services view with
 * view=services, which the browser remembers on its own, and otherwise the
 * Hosts view.
 */
function listOf(params: URLSearchParams): RememberedList {
  return params.get("view") === "services"
    ? { list: "services", base: "view=services" }
    : { list: "hosts", base: "" };
}

type Answers = [AttrListResponse, SpaceListResponse, HostListResponse, ServiceListResponse];

/** joined is the page's four reads as one: their answers once each has one, the first failure, and a reload of each. */
function joined(
  ...reads: [Read<Answers[0]>, Read<Answers[1]>, Read<Answers[2]>, Read<Answers[3]>]
): Read<Answers> {
  const [a, s, h, v] = reads;
  return {
    data: a.data && s.data && h.data && v.data ? [a.data, s.data, h.data, v.data] : undefined,
    error: reads.find((r) => r.error !== undefined)?.error,
    reload: () => {
      for (const r of reads) {
        r.reload();
      }
    },
  };
}

/**
 * viewTabClass makes a view tab as wide as its label and count, on one line:
 * HeroUI sizes a tab to the whole list, which splits the list among the
 * tabs whatever their labels.
 */
const viewTabClass = "w-auto whitespace-nowrap";

/** ListArea is the row of controls, the chips of the view's terms and the list of the view chosen. */
function ListArea({
  page,
  setPage,
  remembered,
  attrs,
  spaces,
  hosts,
  recorded,
  services,
}: {
  page: ListPage;
  setPage: (next: ListPage, replace?: boolean) => void;
  remembered: RememberedView;
  attrs: readonly AttrKey[];
  spaces: readonly Space[];
  hosts: readonly Host[];
  /** recorded are every Host, Retired ones too, which a selection may hold. */
  recorded: readonly Host[];
  services: readonly Service[];
}) {
  const t = useT();
  const phone = usePhone();
  const { tab, list: view } = page;
  const fields = fieldsOf[tab];
  const types = keyTypes(fields, attrs);
  const clock: Clock = { now: new Date(), timeZone: browserTimeZone() };
  const setView = (next: View) => setPage({ ...page, list: next });
  const { mode, select, overlays } = useHostSelectMode({ recorded, spaces, attrs, clock });
  const names = spaces.map((s) => s.name);
  const spaceSelect = names.length > 1;
  // The terms the Space and state selects show, in the Hosts view; every other term is a chip.
  const spaceShown = tab === "hosts" && spaceSelect ? selected(view.filters, "space", (v) => names.includes(v)) : undefined;
  const stateShown = tab === "hosts" ? stateTerm(view) : undefined;
  const chips = view.filters
    .map((written, index) => ({ written, index }))
    .filter(({ index }) => index !== spaceShown?.index && index !== stateShown?.index);
  const space = tab === "hosts" ? spaceShown?.value : page.space || undefined;
  const { fixed, unsorted } = columnsOf[tab];
  const columnKeys = [
    ...new Set([...Object.keys(fields), ...unsorted, ...attrs.map((a) => a.key)].filter((k) => !fixed.includes(k))),
  ];
  const showColumn = (key: string, show: boolean) =>
    setView(
      show
        ? { ...view, columns: [...view.columns.filter((c) => c !== key), key] }
        : {
            ...view,
            columns: view.columns.filter((c) => c !== key),
            // A column hidden no longer sorts the list.
            sort: view.sort.filter((s) => s !== key && s !== `${key}.desc`),
          },
    );
  const listProps: ListProps = {
    page,
    setView,
    types,
    clock,
    q: page.q.trim().toLocaleLowerCase(),
    spaces,
    hosts,
    services,
    clear: remembered.reset,
    mode,
  };
  return (
    <>
      <Tabs
        selectedKey={tab}
        onSelectionChange={(k) => setPage(switchTab(page, k as Tab, defaults, spaceShown?.value))}
        className="gap-0"
      >
        <div className="mb-5 flex flex-wrap items-center gap-2.5">
          {/* The tabs keep their width in the row, which wraps instead. */}
          <Tabs.ListContainer className="shrink-0">
            <Tabs.List aria-label={t("host.list.views")}>
              <Tabs.Tab id="hosts" className={viewTabClass}>
                {t("host.list.title")}
                <span className="t-num ml-1.5 text-muted">{hosts.length}</span>
                <Tabs.Indicator />
              </Tabs.Tab>
              <Tabs.Tab id="services" className={viewTabClass}>
                {t("host.services.title")}
                <span className="t-num ml-1.5 text-muted">{services.length}</span>
                <Tabs.Indicator />
              </Tabs.Tab>
            </Tabs.List>
          </Tabs.ListContainer>
          <SearchField
            aria-label={t(tab === "hosts" ? "host.search" : "host.services.search")}
            value={page.q}
            onChange={(q) => setPage({ ...page, q }, true)}
            className={listSearchClass}
            fullWidth
          >
            <SearchField.Group>
              <SearchField.SearchIcon />
              <SearchField.Input placeholder={t(tab === "hosts" ? "host.search.hint" : "host.services.search.hint")} />
              <SearchField.ClearButton aria-label={t("host.search.clear")} />
            </SearchField.Group>
          </SearchField>
          {spaceSelect && (
            <SearchChoice
              label={t("host.key.space")}
              hideLabel
              value={space ?? "*"}
              onChange={(chosen) => {
                const name = chosen === null || chosen === "*" ? undefined : chosen;
                if (tab === "hosts") {
                  setView(chooseSpace(view, name));
                } else {
                  setPage({ ...page, space: name ?? "" });
                }
              }}
              options={[{ id: "*", label: t("host.list.allSpaces") }, ...names.map((n) => ({ id: n, label: n }))]}
              className="min-w-[min(9rem,100%)] flex-1 md:w-44 md:flex-none"
            />
          )}
          {tab === "hosts" && (
            <Choice
              label={t("host.key.state")}
              hideLabel
              value={stateChoice(view)}
              onChange={(choice) => setView(chooseState(view, choice as StateChoice))}
              options={[
                { id: "inUse", label: t("host.list.inUse") },
                { id: "managed", label: t("host.state.managed") },
                { id: "registered", label: t("host.state.registered") },
                { id: "retired", label: t("host.state.retired") },
                { id: "all", label: t("host.list.withRetired") },
              ]}
              // No narrower than the state it shows, so its label stays on one line.
              className="min-w-max flex-1 md:w-48 md:flex-none"
            />
          )}
          <Filter
            fields={fields}
            attrs={attrs}
            types={types}
            clock={clock}
            onAdd={(term) => setView({ ...view, filters: [...view.filters, term] })}
          />
          {tab === "hosts" && (
            <SearchChoice
              label={t("host.group")}
              inlineLabel
              value={page.group === "" ? "*" : page.group}
              onChange={(chosen) => setPage({ ...page, group: chosen === null || chosen === "*" ? "" : chosen })}
              options={[
                { id: "*", label: t("host.group.none") },
                // The keys as Filter offers them, and a key the URL holds that attr.list does not answer.
                ...[...new Set([...attrs.map((a) => a.key), page.group].filter((k) => k !== ""))].map((k) => ({
                  id: k,
                  label: k,
                })),
              ]}
              className="min-w-[min(12rem,100%)] flex-1 md:w-56 md:flex-none"
            />
          )}
          {!phone && (
            <ColumnsButton keys={columnKeys} shown={view.columns} labelOf={(k) => keyLabel(t, k)} onChange={showColumn} />
          )}
          {tab === "hosts" && select}
          <ResetView view={remembered} />
        </div>
        <TermChips
          terms={chips}
          types={types}
          clock={clock}
          onRemove={(index) => setView({ ...view, filters: view.filters.filter((_, i) => i !== index) })}
        />
        <Tabs.Panel id="hosts" className="p-0">
          <HostsList {...listProps} />
        </Tabs.Panel>
        <Tabs.Panel id="services" className="p-0">
          <ServicesList {...listProps} />
        </Tabs.Panel>
      </Tabs>
      {/* Outside the Tabs, whose children React Aria also renders into its own hidden collection: a dialog there would open twice. */}
      {tab === "hosts" && overlays}
    </>
  );
}
