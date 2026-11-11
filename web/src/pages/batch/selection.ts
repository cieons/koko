import { useCallback, useMemo, useRef, useState } from "react";

// The records ticked on a list page in its Select mode, by id, kept while
// the list reloads and its view changes (#134).

/** Selection is the records ticked on a list page. */
export interface Selection {
  has: (id: string) => boolean;
  /**
   * toggle ticks or unticks id. With order, the ids as the list shows them,
   * it does the same to every record between the one toggled last and id,
   * as a Shift-click does.
   */
  toggle: (id: string, on: boolean, order?: readonly string[]) => void;
  /** setMany ticks or unticks each of ids, as a group's checkbox does. */
  setMany: (ids: readonly string[], on: boolean) => void;
  clear: () => void;
}

/** useSelection holds a list page's selection; its functions stay the same from one render to the next. */
export function useSelection(): Selection {
  const [ids, setIds] = useState<ReadonlySet<string>>(new Set());
  const anchor = useRef<string>(undefined);
  const setMany = useCallback((changed: readonly string[], on: boolean) => {
    setIds((now) => {
      const next = new Set(now);
      for (const id of changed) {
        if (on) {
          next.add(id);
        } else {
          next.delete(id);
        }
      }
      return next;
    });
  }, []);
  const toggle = useCallback(
    (id: string, on: boolean, order?: readonly string[]) => {
      const from = order && anchor.current !== undefined ? order.indexOf(anchor.current) : -1;
      const to = order ? order.indexOf(id) : -1;
      anchor.current = id;
      setMany(order && from >= 0 && to >= 0 ? order.slice(Math.min(from, to), Math.max(from, to) + 1) : [id], on);
    },
    [setMany],
  );
  const clear = useCallback(() => {
    anchor.current = undefined;
    setIds((now) => (now.size === 0 ? now : new Set()));
  }, []);
  return useMemo(
    () => ({ has: (id: string) => ids.has(id), toggle, setMany, clear }),
    [ids, toggle, setMany, clear],
  );
}

/** Ticked says how many of a group's records are ticked: all, some or none. */
export type Ticked = "all" | "some" | "none";

/** tickedOf says how many of ids the selection holds. */
export function tickedOf(selection: Selection, ids: readonly string[]): Ticked {
  const count = ids.filter((id) => selection.has(id)).length;
  return count === 0 ? "none" : count === ids.length ? "all" : "some";
}
