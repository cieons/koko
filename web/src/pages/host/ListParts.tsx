import { Badge } from "@astryxdesign/core/Badge";
import { Banner } from "@astryxdesign/core/Banner";
import { BottomSheet } from "@astryxdesign/core/BottomSheet";
import { Button } from "@astryxdesign/core/Button";
import { useMediaQuery } from "@astryxdesign/core/hooks";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, Layout, LayoutContent, LayoutHeader, LayoutPanel, Section, StackItem, VStack } from "@astryxdesign/core/Layout";
import { List, ListItem } from "@astryxdesign/core/List";
import { Popover } from "@astryxdesign/core/Popover";
import { PowerSearch, type PowerSearchFilter } from "@astryxdesign/core/PowerSearch";
import { ResizeHandle, useResizable } from "@astryxdesign/core/Resizable";
import {
  proportional,
  Table,
  type TableColumn,
  type TablePlugin,
  type TableSortState,
  useTableSortable,
  useTableStickyColumns,
} from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Columns3, Plus, X } from "lucide-react";
import { type MouseEvent, type ReactNode, useMemo, useState } from "react";

import type { AttrKey } from "../../api/attr";
import { phone } from "../../shell/AdaptiveDialog";
import { useT } from "../../shell/i18n";
import { filterConfig, termOf, tokenOf } from "./filter";
import {
  type Clock,
  type Columns,
  type Fields,
  type KeyTypes,
  type ReadTerm,
  readSortKey,
  readWritten,
  sortBy,
  type View,
} from "./view";

// What the Hosts list and the Services list share, on Astryx's table-filter
// pattern: the page's frame with its detail pane, the PowerSearch filter bar
// over the view's terms, the sortable headers, the column options and the
// table itself. Each is written over a list's view, which the page's URL
// keeps, rather than over Hosts.

/** ViewProps are what a part of a list gets to show its view and change it. */
export interface ViewProps {
  view: View;
  /** setView moves the page to another view, a new history entry. */
  setView: (next: View) => void;
}

/** Detail is what a list's detail pane shows: the record selected, by its label, and how it closes. */
export interface Detail {
  label: string;
  content: ReactNode;
  onClose: () => void;
}

/**
 * ListLayout is a list page's frame, table-filter's: a header with the
 * title, the count of records listed, the page's actions and the toolbar,
 * the content under it, and the record selected in a resizable pane beside
 * it, or in a bottom sheet on a phone.
 */
export function ListLayout({
  title,
  count,
  actions,
  toolbar,
  detail,
  children,
}: {
  title: string;
  count?: number;
  actions?: ReactNode;
  toolbar?: ReactNode;
  detail?: Detail;
  children: ReactNode;
}) {
  const t = useT();
  const isPhone = useMediaQuery(phone);
  const width = useResizable({ defaultSize: 380, minSize: 320, maxSize: 560 });
  return (
    <>
      <Layout
        height="fill"
        padding={0}
        className="koko-list"
        header={
          <LayoutHeader hasDivider label={t("host.list.controls", { list: title })}>
            <Section variant="transparent" padding={4}>
              <VStack gap={4}>
                <HStack gap={3} vAlign="center">
                  <StackItem size="fill">
                    <HStack gap={2} vAlign="center">
                      <Heading level={1}>{title}</Heading>
                      {count !== undefined && <Badge variant="neutral" label={String(count)} />}
                    </HStack>
                  </StackItem>
                  {actions}
                </HStack>
                {toolbar}
              </VStack>
            </Section>
          </LayoutHeader>
        }
        content={
          <LayoutContent padding={4} label={title}>
            {children}
          </LayoutContent>
        }
        end={
          !isPhone && detail ? (
            <>
              <ResizeHandle resizable={width.props} isReversed isAlwaysVisible={false} label={t("host.detail.resize")} />
              <LayoutPanel resizable={width.props} hasDivider padding={0} role="region" label={detail.label}>
                {detail.content}
              </LayoutPanel>
            </>
          ) : undefined
        }
      />
      {isPhone && (
        <BottomSheet
          isOpen={detail !== undefined}
          onOpenChange={(open) => open || detail?.onClose()}
          label={detail?.label ?? title}
          height="tall"
          padding={0}
        >
          {detail?.content}
        </BottomSheet>
      )}
    </>
  );
}

/** Toolbar lays out a list's filter bar, filling the row, and its other controls after it. */
export function Toolbar({ filter, children }: { filter: ReactNode; children: ReactNode }) {
  return (
    <HStack gap={3} vAlign="center" wrap="wrap">
      <StackItem size="fill" className="koko-list-filter">
        {filter}
      </StackItem>
      <HStack gap={3} vAlign="center">
        {children}
      </HStack>
    </HStack>
  );
}

/** tokenKey identifies a token by what it holds, so a token PowerSearch hands back unchanged keeps its term as written. */
function tokenKey(token: PowerSearchFilter): string {
  return JSON.stringify([token.field, token.operator, token.value]);
}

/**
 * FilterBar is a list's filter: a PowerSearch token for each of the view's
 * terms that reads, each the term the URL holds, and any term added or
 * changed there written into the URL as the CLI writes it. A token handed
 * back unchanged keeps its term as written, so 10.50 stays 10.50; a term
 * that does not read stays in the URL after them, for the page to say so.
 * A value the key does not take is refused, saying so, and the URL kept.
 */
export function FilterBar({
  label,
  fields,
  attrs,
  types,
  clock,
  terms,
  view,
  setView,
  count,
}: ViewProps & {
  /** label names the filter, such as Filter Hosts. */
  label: string;
  fields: Fields;
  attrs: readonly AttrKey[];
  types: KeyTypes;
  clock: Clock;
  terms: readonly ReadTerm[];
  /** count is how many records the view lists, once known. */
  count?: number;
}) {
  const t = useT();
  // A refusal is of a change to the terms as they were; once they change
  // otherwise, as by Back, it no longer applies.
  const [refusal, setRefusal] = useState<{ term: string; of: string }>();
  const current = view.filters.join("\n");
  const refused = refusal?.of === current ? refusal.term : undefined;
  const tokens = terms.flatMap(({ written, term, filter }) => {
    const type = term && filter && types(term.key);
    return type ? [{ written, token: tokenOf(term, type) }] : [];
  });
  const unread = terms.filter((r) => !tokens.some((k) => k.written === r.written)).map((r) => r.written);
  const config = filterConfig(
    t,
    label,
    fields,
    attrs,
    types,
    tokens.map((k) => k.token.field),
  );
  const change = (next: readonly PowerSearchFilter[]) => {
    const kept = new Set<number>();
    const written = next.map((token) => {
      const at = tokens.findIndex((k, i) => !kept.has(i) && tokenKey(k.token) === tokenKey(token));
      if (at >= 0) {
        kept.add(at);
        return { term: tokens[at]?.written ?? "", isNew: false };
      }
      return { term: termOf(token), isNew: true };
    });
    const wrong = written.find((w) => w.isNew && readWritten(w.term, types, clock).filter === undefined);
    setRefusal(wrong && { term: wrong.term, of: current });
    if (!wrong) {
      setView({ ...view, filters: [...written.map((w) => w.term), ...unread] });
    }
  };
  return (
    <PowerSearch
      config={config}
      filters={tokens.map((k) => k.token)}
      onChange={change}
      label={label}
      placeholder={t("host.filter.placeholder")}
      resultCount={count}
      status={refused === undefined ? undefined : { type: "error", message: t("host.filter.refused", { term: refused }) }}
    />
  );
}

/**
 * useSortPlugin is the Table plugin that sorts a list by its headers, into
 * the view: a click on a header sorts by its key alone, or turns it round
 * when the list is sorted by it alone, and a Shift-click adds the key as a
 * further one, or turns it round where it is, like repeated --sort (sortBy).
 */
function useSortPlugin<T extends Record<string, unknown>>({ view, setView }: ViewProps): TablePlugin<T> {
  const sort: TableSortState = view.sort
    .map(readSortKey)
    .map((k) => ({ sortKey: k.key, direction: k.desc ? "descending" : "ascending" }));
  return useTableSortable<T>({
    sort,
    allowUnsortedState: false,
    isMultiSortEnabled: true,
    // Astryx answers with the sort it would make: a click's has one key, a
    // Shift-click's the key it added or turned among the others.
    onSortChange: (next) => {
      const further = next.length > 1;
      const clicked = further
        ? next.find((e, i) => sort[i]?.sortKey !== e.sortKey || sort[i]?.direction !== e.direction)
        : next[0];
      if (clicked) {
        setView({ ...view, sort: sortBy(view.sort, clicked.sortKey, further) });
      }
    },
  });
}

/** Row is a record as a table's row. */
interface Row<R> extends Record<string, unknown> {
  id: string;
  record: R;
}

/**
 * RecordTable lists records in the view's columns, after the list's fixed
 * ones, which stay in view as the table scrolls sideways. Headers are the
 * keys, as stored, each sorting the list but the unsorted columns'. A click
 * on a row selects it; the first column's link selects it too, and opens
 * the record's page in a new tab when clicked so.
 */
export function RecordTable<R>({
  label,
  records,
  idOf,
  columns,
  types,
  view,
  setView,
  cell,
  selected,
  onSelect,
}: ViewProps & {
  label: string;
  records: readonly R[];
  idOf: (record: R) => string;
  columns: Columns;
  types: KeyTypes;
  /** cell is what a record shows in the column of key. */
  cell: (record: R, key: string) => ReactNode;
  selected?: string;
  onSelect: (id: string) => void;
}) {
  const rows = useMemo(() => records.map((record): Row<R> => ({ id: idOf(record), record })), [records, idOf]);
  const sortable = useSortPlugin<Row<R>>({ view, setView });
  const sticky = useTableStickyColumns<Row<R>>({ startKeys: [...columns.fixed] });
  const selection = useMemo<TablePlugin<Row<R>>>(
    () => ({
      transformBodyRow: (props, row) => ({
        ...props,
        htmlProps: {
          ...props.htmlProps,
          className: [props.htmlProps.className, "koko-row", row.id === selected && "koko-row-selected"]
            .filter(Boolean)
            .join(" "),
          "aria-current": row.id === selected ? true : undefined,
          onClick: (event: MouseEvent<HTMLTableRowElement>) => {
            if (!(event.target as HTMLElement).closest("a, button, input")) {
              onSelect(row.id);
            }
          },
        },
      }),
    }),
    [selected, onSelect],
  );
  const tableColumns = [...columns.fixed, ...view.columns].map(
    (key, i): TableColumn<Row<R>> => ({
      key,
      header: key,
      width: proportional(i === 0 ? 2 : 1, { minWidth: i < columns.fixed.length ? 140 : 110 }),
      sortable: !columns.unsorted.includes(key) && types(key) !== undefined,
      renderCell: (row) => cell(row.record, key),
    }),
  );
  return (
    <Table<Row<R>>
      aria-label={label}
      data={rows}
      columns={tableColumns}
      idKey="id"
      dividers="rows"
      hasHover
      verticalAlign="top"
      plugins={{ sortable, sticky, selection }}
    />
  );
}

/**
 * selectingLink is the onClick of a link to a record's page in a list's
 * table: a plain click selects the record, opening its detail pane, and a
 * modified one opens the page as the browser would.
 */
export function selectingLink(select: () => void) {
  return (event: MouseEvent) => {
    if (event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey) {
      event.preventDefault();
      select();
    }
  };
}

/**
 * ColumnOptions shows or hides each column of keys, after the list's fixed
 * columns: those shown, in order, and those not, each with a button. A
 * column shown goes last, and a column hidden no longer sorts the list. A
 * popover beside its button, or a bottom sheet on a phone.
 */
export function ColumnOptions({ view, setView, keys }: ViewProps & { keys: readonly string[] }) {
  const t = useT();
  const isPhone = useMediaQuery(phone);
  const [isOpen, setOpen] = useState(false);
  const hidden = keys.filter((k) => !view.columns.includes(k));
  const hide = (key: string): View => ({
    ...view,
    columns: view.columns.filter((c) => c !== key),
    sort: view.sort.filter((s) => readSortKey(s).key !== key),
  });
  const body = (
    <VStack gap={4} padding={isPhone ? 4 : 0}>
      <List density="compact" header={<Text type="label">{t("host.columns.shown")}</Text>}>
        {view.columns.map((key) => (
          <ListItem
            key={key}
            label={key}
            endContent={
              <IconButton
                label={t("host.columns.hide", { key })}
                variant="ghost"
                size="sm"
                icon={<Icon icon={X} size="sm" />}
                onClick={() => setView(hide(key))}
              />
            }
          />
        ))}
      </List>
      <List density="compact" header={<Text type="label">{t("host.columns.hidden")}</Text>}>
        {hidden.map((key) => (
          <ListItem
            key={key}
            label={key}
            endContent={
              <IconButton
                label={t("host.columns.show", { key })}
                variant="ghost"
                size="sm"
                icon={<Icon icon={Plus} size="sm" />}
                onClick={() => setView({ ...view, columns: [...view.columns, key] })}
              />
            }
          />
        ))}
      </List>
    </VStack>
  );
  const trigger = (
    <Button
      label={t("host.columns")}
      variant="secondary"
      icon={<Icon icon={Columns3} size="sm" />}
      onClick={isPhone ? () => setOpen(true) : undefined}
    />
  );
  return isPhone ? (
    <>
      {trigger}
      <BottomSheet isOpen={isOpen} onOpenChange={setOpen} label={t("host.columns")} height="tall" padding={0}>
        {body}
      </BottomSheet>
    </>
  ) : (
    <Popover placement="below" alignment="end" width={320} label={t("host.columns")} content={body}>
      {trigger}
    </Popover>
  );
}

/**
 * ViewFault says which parts of the URL's view the list does not take, in
 * place of the list, with a link back to the list's default view.
 */
export function ViewFault({ message, reset }: { message: string; reset: string }) {
  const t = useT();
  return (
    <Banner
      status="error"
      role="alert"
      title={message}
      endContent={<Button variant="secondary" label={t("host.view.reset")} href={reset} />}
    />
  );
}
