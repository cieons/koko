import { Button } from "@heroui/react";
import type { ReactNode } from "react";

import type { Host } from "../../api/host";
import type { Service } from "../../api/service";
import type { Space } from "../../api/space";
import type { Read } from "../../shell/data";
import { useT } from "../../shell/i18n";
import { Sheet } from "../../ui/Page";
import { AlertLine, Loaded } from "../../ui/states";
import type { SelectMode } from "../batch/useSelectMode";
import type { Defaults, ListPage, Tab } from "./controls";
import { hostFields, serviceFields } from "./record";
import { type Clock, type Columns, type Fields, type KeyTypes, sortBy, type View } from "./view";

// What the Hosts page's two views share: their columns, what each list is
// drawn from, the panel for a URL the list does not take, and the line that
// says how many records the search and filters left.

/**
 * defaults are each view's columns after its fixed ones unless the URL
 * names others: the Hosts view's Provider and region, Address, Price,
 * Expires and State (docs/web-design.md, "Hosts"), and the Services view's
 * ports.
 */
export const defaults: Defaults = {
  hosts: ["provider", "address", "price", "expires_at", "state"],
  services: ["ports"],
};

/**
 * columnsOf are each view's columns besides its keys: those it always shows
 * first, which no columns= may name (a Host's name; a Service's name and
 * Host), and those it may show but neither filters nor sorts on (a
 * Service's ports).
 */
export const columnsOf: Readonly<Record<Tab, Columns>> = {
  hosts: { fixed: ["name"], unsorted: [] },
  services: { fixed: ["name", "host"], unsorted: ["ports"] },
};

/** fieldsOf are each view's fields, which its list filters and sorts on. */
export const fieldsOf: Readonly<Record<Tab, Fields>> = { hosts: hostFields, services: serviceFields };

/** ListProps are what each view's list is drawn from. */
export interface ListProps {
  page: ListPage;
  setView: (view: View) => void;
  types: KeyTypes;
  clock: Clock;
  /** q is the search, lowercased, or "" for none. */
  q: string;
  spaces: readonly Space[];
  /** hosts are the Hosts not Retired, as host.list answers with no term. */
  hosts: readonly Host[];
  /** services are every Service, as service.list answers with no term. */
  services: readonly Service[];
  /** clear goes to the view's default, clearing the search and filters, and forgets the view remembered. */
  clear: () => void;
  /** mode is the Hosts view's Select mode (#134). */
  mode: SelectMode;
}

/** sorter is what a header's click does to the view: sortBy, as repeated --sort. */
export function sorter(view: View, setView: (view: View) => void): (key: string, further: boolean) => void {
  return (key, further) => setView({ ...view, sort: sortBy(view.sort, key, further) });
}

/**
 * ViewFault is the panel shown in place of a list whose URL holds a part
 * the list does not take: it names each, and offers the default view.
 */
export function ViewFault({ message, reset }: { message: string; reset: () => void }) {
  const t = useT();
  return (
    <Sheet className="flex flex-col items-start gap-3">
      <AlertLine>{message}</AlertLine>
      <Button variant="secondary" onPress={reset}>
        {t("host.view.reset")}
      </Button>
    </Sheet>
  );
}

/** ShownCount is the line, announced politely, that says how many of a view's records the search and filters left. */
export function ShownCount({ children }: { children: ReactNode }) {
  return (
    <p role="status" className="t-label mb-3 px-1">
      {children}
    </p>
  );
}

/**
 * InPanel shows a read's answer through children once it has one; until
 * then, and when it failed, its state on a panel of its own.
 */
export function InPanel<T>({ read, children }: { read: Read<T>; children: (data: T) => ReactNode }) {
  return read.data === undefined ? (
    <Sheet list>
      <Loaded read={read}>{() => null}</Loaded>
    </Sheet>
  ) : (
    <Loaded read={read}>{children}</Loaded>
  );
}

/** Missing is a cell's mark for a value the record lacks, which a screen reader reads as an empty cell. */
export function Missing() {
  return (
    <span aria-hidden className="text-muted">
      –
    </span>
  );
}
