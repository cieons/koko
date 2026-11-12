import { Button, Checkbox, Popover } from "@heroui/react";
import { ArrowDown, ArrowUp, Columns3 } from "lucide-react";
import { type MouseEvent, type ReactNode, useId } from "react";

import { useT } from "../../shell/i18n";
import { usePhone } from "../../ui/phone";
import type { Ticked } from "../batch/selection";
import { Tick } from "../batch/Tick";
import { readSortKey } from "./view";

// The table a list's records lie in, on a panel: a header row whose
// headers sort the list, a click by that key alone and a Shift-click as a
// further key, and a row per record that is one link to it. On a phone each
// record is a row of lines instead. And the columns button, which ticks the
// columns shown. In a Select mode (#134) each row has a checkbox before it
// and toggles on a click, and is no link.

/** linkClass is a row's link, whose box covers its row, so the whole row opens the record and is one Tab stop. */
export const linkClass =
  "t-name text-foreground no-underline outline-none after:absolute after:inset-0 data-[focus-visible]:after:ring-2 data-[focus-visible]:after:ring-focus data-[focus-visible]:after:ring-inset";

/**
 * GivesWay is when a column gives way as its table narrows, by the
 * table's own width (a container query), not the window's: first under
 * 64rem, second under 52rem.
 */
export type GivesWay = "first" | "second";

/** givesWayClass hides a column's header and cells below its width, written out whole for Tailwind to find. */
const givesWayClass: Readonly<Record<GivesWay, string>> = {
  first: "@max-[64rem]:hidden",
  second: "@max-[52rem]:hidden",
};

/** Column is one column of a record table after its first. */
export interface Column<R> {
  key: string;
  label: string;
  /** sortable makes its header a button that sorts by key. */
  sortable: boolean;
  cell: (record: R) => ReactNode;
  /** givesWay, when set, hides the column while the table is narrower than its width; unset, it always shows. */
  givesWay?: GivesWay;
}

/**
 * hiddenWhenNarrow is the class that hides a column's header and cells
 * while its table is narrow, or none: a column the list sorts by always
 * shows, so the order on screen is never by a column out of sight.
 */
function hiddenWhenNarrow<R>(column: Column<R>, sort: readonly string[]): string {
  if (column.givesWay === undefined || sort.some((s) => readSortKey(s).key === column.key)) {
    return "";
  }
  return givesWayClass[column.givesWay];
}

/** RecordTableProps are a table's. */
export interface RecordTableProps<R> {
  /** label names the table, such as its Space. */
  label: string;
  records: readonly R[];
  idOf: (record: R) => string;
  /** first is the column that names each record, a link that covers its row. */
  first: Column<R>;
  columns: readonly Column<R>[];
  /** sort are the view's sort keys, as written. */
  sort: readonly string[];
  /** onSort sorts by key, alone or, with further, as a further key. */
  onSort: (key: string, further: boolean) => void;
  /** phoneRow is a record as a phone shows it: one link of a few lines. */
  phoneRow: (record: R) => ReactNode;
  /** ticks, in a Select mode, puts a checkbox before each row, which a click anywhere on the row toggles, and one for them all in the header row. */
  ticks?: RowTicks<R>;
}

/** RowTicks are a table's checkboxes in a Select mode: one a row, and the header's for every row. */
export interface RowTicks<R> {
  ticked: (record: R) => boolean;
  /** onTick hears a row ticked or unticked, and whether Shift was held, which ticks a range. */
  onTick: (record: R, on: boolean, shift: boolean) => void;
  /** labelOf names a row's checkbox, such as "Select web1". */
  labelOf: (record: R) => string;
  all: Ticked;
  onAll: (on: boolean) => void;
  /** allLabel names the header's checkbox, such as "Select the 14 Hosts of ops". */
  allLabel: string;
}

/**
 * RecordTable is a list's records on a panel: a table on a desktop, rows of
 * lines on a phone. The whole row is the link to its record; its first
 * cell's link covers it, so it is one Tab stop. With ticks, a row is no
 * link: a click anywhere on it ticks it, and its checkbox is its Tab stop.
 */
export function RecordTable<R>({
  label,
  records,
  idOf,
  first,
  columns,
  sort,
  onSort,
  phoneRow,
  ticks,
}: RecordTableProps<R>) {
  const phone = usePhone();
  if (phone) {
    return (
      <ul aria-label={label}>
        {records.map((r) => (
          <li key={idOf(r)} className="border-b border-line last:border-0">
            {phoneRow(r)}
          </li>
        ))}
      </ul>
    );
  }
  return (
    // A container its columns that give way key on; past what they free, the table scrolls inside its panel.
    <div className="@container overflow-x-auto">
      <table aria-label={label} className="w-full border-separate border-spacing-0">
        <thead>
          <tr>
            {ticks && (
              <th scope="col" className="h-10 w-11 border-b border-line py-0 pr-0 pl-2">
                <Tick label={ticks.allLabel} ticked={ticks.all} onChange={ticks.onAll} className="-my-1" />
              </th>
            )}
            <Header column={first} sort={sort} onSort={onSort} flush={ticks !== undefined} />
            {columns.map((c) => (
              <Header key={c.key} column={c} sort={sort} onSort={onSort} />
            ))}
          </tr>
        </thead>
        <tbody>
          {records.map((r) => {
            const ticked = ticks?.ticked(r) === true;
            return (
              <tr
                key={idOf(r)}
                onClick={ticks ? (event) => ticks.onTick(r, !ticked, event.shiftKey) : undefined}
                className={`group relative ${ticked ? "bg-accent-soft" : "hover:bg-surface-secondary"} ${ticks ? "cursor-pointer select-none" : ""}`}
              >
                {ticks && (
                  <td className="w-11 border-b border-line py-0 pr-0 pl-2 group-last:border-0">
                    <Tick label={ticks.labelOf(r)} ticked={ticked} onChange={(on, shift) => ticks.onTick(r, on, shift)} />
                  </td>
                )}
                <th
                  scope="row"
                  className={`h-15 max-w-80 border-b border-line text-left font-normal whitespace-nowrap group-last:border-0 ${ticks ? "pr-5 pl-2" : "px-5"}`}
                >
                  {first.cell(r)}
                </th>
                {columns.map((c) => (
                  <td
                    key={c.key}
                    className={`max-w-72 truncate border-b border-line px-5 group-last:border-0 ${hiddenWhenNarrow(c, sort)}`}
                  >
                    {c.cell(r)}
                  </td>
                ))}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

/**
 * Header is a column's header: a button that sorts by its key where it
 * sorts, showing the direction and, when the list sorts by more than one
 * key, its rank, which a screen reader hears as the button's description.
 * The first key's direction is the header's aria-sort.
 */
function Header<R>({
  column,
  sort,
  onSort,
  flush = false,
}: {
  column: Column<R>;
  sort: readonly string[];
  onSort: (key: string, further: boolean) => void;
  /** flush sets the header against a checkbox column before it. */
  flush?: boolean;
}) {
  const t = useT();
  const id = useId();
  const keys = sort.map(readSortKey);
  const rank = keys.findIndex((k) => k.key === column.key);
  const key = keys[rank];
  const direction = key?.desc === true ? "descending" : "ascending";
  const className = `h-10 border-b border-line text-left font-normal whitespace-nowrap ${flush ? "pr-5 pl-2" : "px-5"} ${hiddenWhenNarrow(column, sort)}`;
  if (!column.sortable) {
    return (
      <th scope="col" className={`${className} t-label`}>
        {column.label}
      </th>
    );
  }
  const Arrow = key?.desc === true ? ArrowDown : ArrowUp;
  const ranked = key !== undefined && sort.length > 1;
  return (
    <th scope="col" aria-sort={rank === 0 ? direction : undefined} className={className}>
      <button
        type="button"
        aria-describedby={ranked ? id : undefined}
        onClick={(event: MouseEvent) => onSort(column.key, event.shiftKey)}
        className={`t-label inline-flex items-center gap-1 rounded outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-focus ${key ? "font-medium text-foreground" : ""}`}
      >
        {column.label}
        {key && <Arrow aria-hidden className="size-3.5" />}
        {ranked && (
          <span aria-hidden className="t-num text-xs">
            {rank + 1}
          </span>
        )}
      </button>
      {ranked && (
        <span id={id} hidden>
          {t(`host.sort.${direction}`, { rank: rank + 1 })}
        </span>
      )}
    </th>
  );
}

/**
 * ColumnsButton opens the columns a list may show, each a checkbox: one
 * ticked shows, going last; one unticked no longer shows.
 */
export function ColumnsButton({
  keys,
  shown,
  labelOf,
  onChange,
}: {
  /** keys are every column the list may show after its fixed ones, in the order offered. */
  keys: readonly string[];
  shown: readonly string[];
  labelOf: (key: string) => string;
  onChange: (key: string, show: boolean) => void;
}) {
  const t = useT();
  return (
    <Popover>
      <Button variant="secondary" isIconOnly aria-label={t("host.columns")}>
        <Columns3 aria-hidden className="size-4" />
      </Button>
      <Popover.Content placement="bottom end" className="w-64">
        <Popover.Dialog aria-label={t("host.columns")} className="flex max-h-[min(28rem,70dvh)] flex-col gap-3 overflow-y-auto p-4">
          <Popover.Heading className="t-heading">{t("host.columns")}</Popover.Heading>
          {keys.map((k) => (
            <Checkbox key={k} isSelected={shown.includes(k)} onChange={(on) => onChange(k, on)}>
              <Checkbox.Content>
                <Checkbox.Control>
                  <Checkbox.Indicator />
                </Checkbox.Control>
                {labelOf(k)}
              </Checkbox.Content>
            </Checkbox>
          ))}
        </Popover.Dialog>
      </Popover.Content>
    </Popover>
  );
}
