import { Button, Checkbox, Popover } from "@heroui/react";
import { ArrowDown, ArrowUp, Columns3 } from "lucide-react";
import { type MouseEvent, type ReactNode, useId } from "react";

import { useT } from "../../shell/i18n";
import { usePhone } from "../../ui/phone";
import { readSortKey } from "./view";

// The table a list's records lie in, on a panel: a header row whose
// headers sort the list, a click by that key alone and a Shift-click as a
// further key, and a row per record that is one link to it. On a phone each
// record is a row of lines instead. And the columns button, which ticks the
// columns shown.

/** linkClass is a row's link, whose box covers its row, so the whole row opens the record and is one Tab stop. */
export const linkClass =
  "t-name text-foreground no-underline outline-none after:absolute after:inset-0 data-[focus-visible]:after:ring-2 data-[focus-visible]:after:ring-focus data-[focus-visible]:after:ring-inset";

/** Column is one column of a record table after its first. */
export interface Column<R> {
  key: string;
  label: string;
  /** sortable makes its header a button that sorts by key. */
  sortable: boolean;
  cell: (record: R) => ReactNode;
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
}

/**
 * RecordTable is a list's records on a panel: a table on a desktop, rows of
 * lines on a phone. The whole row is the link to its record; its first
 * cell's link covers it, so it is one Tab stop.
 */
export function RecordTable<R>({ label, records, idOf, first, columns, sort, onSort, phoneRow }: RecordTableProps<R>) {
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
    <div className="overflow-x-auto">
      <table aria-label={label} className="w-full border-separate border-spacing-0">
        <thead>
          <tr>
            <Header column={first} sort={sort} onSort={onSort} />
            {columns.map((c) => (
              <Header key={c.key} column={c} sort={sort} onSort={onSort} />
            ))}
          </tr>
        </thead>
        <tbody>
          {records.map((r) => (
            <tr key={idOf(r)} className="group relative hover:bg-surface-secondary">
              <th
                scope="row"
                className="h-15 max-w-80 border-b border-line px-5 text-left font-normal whitespace-nowrap group-last:border-0"
              >
                {first.cell(r)}
              </th>
              {columns.map((c) => (
                <td key={c.key} className="max-w-72 truncate border-b border-line px-5 group-last:border-0">
                  {c.cell(r)}
                </td>
              ))}
            </tr>
          ))}
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
}: {
  column: Column<R>;
  sort: readonly string[];
  onSort: (key: string, further: boolean) => void;
}) {
  const t = useT();
  const id = useId();
  const keys = sort.map(readSortKey);
  const rank = keys.findIndex((k) => k.key === column.key);
  const key = keys[rank];
  const direction = key?.desc === true ? "descending" : "ascending";
  const className = "h-10 border-b border-line px-5 text-left font-normal whitespace-nowrap";
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
