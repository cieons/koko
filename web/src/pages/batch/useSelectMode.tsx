import { Button } from "@heroui/react";
import { Check, ListChecks, type LucideIcon } from "lucide-react";
import { type ReactNode, useCallback, useState } from "react";

import { useT } from "../../shell/i18n";
import { BatchBar } from "./BatchBar";
import { type Selection, useSelection } from "./selection";

// What a list page holds of its Select mode (#134), whatever it lists:
// whether the mode is on, the selection, which records its view shows, and
// the action whose dialog is open.

/** BatchAction is one action a batch takes, as the bar lists it. */
export interface BatchAction<A extends string> {
  id: A;
  label: string;
  icon: LucideIcon;
  /** danger marks an action that takes something away. */
  danger?: boolean;
}

/** SelectMode is what a list reads and tells of the page's Select mode. */
export interface SelectMode {
  /** selecting says the mode is on: rows carry a checkbox and toggle, and are no links. */
  selecting: boolean;
  selection: Selection;
  /** report tells which records the view shows, in its order, and whether the search or filters narrow it. */
  report: (ids: readonly string[], narrowed: boolean) => void;
}

/**
 * useSelectMode holds a list page's Select mode: the list's own part
 * (mode), the Select button of the controls row (select), and the bar and
 * the open dialog (overlays). The selection is kept while the list reloads
 * and its view changes, so it can be gathered across several searches; a
 * record that is gone has left it. A batch that was applied ends the mode;
 * one given up leaves the selection as it was.
 */
export function useSelectMode<R, A extends string>({
  records,
  idOf,
  actions,
  barLabel,
  dialog,
}: {
  /** records are every record a selection may hold, shown by the view or not. */
  records: readonly R[];
  idOf: (record: R) => string;
  actions: readonly BatchAction<A>[];
  /** barLabel names the bar, such as "Actions on the selected Hosts". */
  barLabel: string;
  /** dialog is the dialog of action for records; its onClose hears whether the batch was applied. */
  dialog: (action: A, records: readonly R[], onClose: (applied: boolean) => void) => ReactNode;
}): { mode: SelectMode; select: ReactNode; overlays: ReactNode } {
  const t = useT();
  const selection = useSelection();
  const [selecting, setSelecting] = useState(false);
  const [shown, setShown] = useState<{ ids: readonly string[]; narrowed: boolean }>({ ids: [], narrowed: false });
  const [acting, setActing] = useState<{ action: A; records: readonly R[] }>();
  const report = useCallback((ids: readonly string[], narrowed: boolean) => setShown({ ids, narrowed }), []);

  const selected = records.filter((r) => selection.has(idOf(r)));
  const hidden = selected.filter((r) => !shown.ids.includes(idOf(r))).length;
  const unticked = shown.ids.some((id) => !selection.has(id));

  const leave = () => {
    selection.clear();
    setSelecting(false);
  };

  const select = (
    <Button
      variant={selecting ? "primary" : "secondary"}
      onPress={() => (selecting ? leave() : setSelecting(true))}
      className="shrink-0"
    >
      {selecting ? <Check aria-hidden className="size-4" /> : <ListChecks aria-hidden className="size-4" />}
      {t(selecting ? "batch.done" : "batch.select")}
    </Button>
  );

  const overlays = (
    <>
      {selecting && (
        <>
          {/* Room for the bar under the last row. */}
          <div aria-hidden className="h-16" />
          <BatchBar
            label={barLabel}
            summary={t("batch.selected", { count: selected.length })}
            hidden={hidden > 0 ? t("batch.hidden", { count: hidden }) : undefined}
            selectAll={
              unticked
                ? t(shown.narrowed ? "batch.selectAll.matching" : "batch.selectAll", { count: shown.ids.length })
                : undefined
            }
            onSelectAll={() => selection.setMany(shown.ids, true)}
            actions={actions.map((a) => ({ ...a, onPress: () => setActing({ action: a.id, records: selected }) }))}
            disabled={selected.length === 0}
            done={t("batch.done")}
            onDone={leave}
          />
        </>
      )}
      {acting &&
        dialog(acting.action, acting.records, (applied) => {
          setActing(undefined);
          if (applied) {
            leave();
          }
        })}
    </>
  );

  return { mode: { selecting, selection, report }, select, overlays };
}
