import { Button } from "@heroui/react";
import { Check, type LucideIcon } from "lucide-react";

import { usePhone } from "../../ui/phone";

// The bar of a Select mode (#134): how many records are ticked, the way to
// tick every record the view shows, the actions, and Done.

/** BarAction is one action of the bar. */
export interface BarAction {
  id: string;
  label: string;
  icon: LucideIcon;
  /** danger marks an action that takes something away, set apart after the others. */
  danger?: boolean;
  onPress: () => void;
}

/** BatchBarProps are what the bar shows. */
export interface BatchBarProps {
  /** label names the bar, such as "Actions on the selected Hosts". */
  label: string;
  /** summary says how many records are ticked, such as "12 selected". */
  summary: string;
  /** hidden says how many of them the view does not show, such as "3 not in this view", if any. */
  hidden?: string;
  /** selectAll labels the button that ticks every record the view shows, such as "Select all 42"; left out once all are ticked. */
  selectAll?: string;
  onSelectAll: () => void;
  actions: readonly BarAction[];
  /** disabled says no record is ticked yet, as when the mode has just begun. */
  disabled: boolean;
  /** done labels the button that leaves the mode. */
  done: string;
  onDone: () => void;
}

/**
 * BatchBar floats at the foot of the page while a Select mode is on: an
 * island, as the phone's bar of places is, which it covers on a phone. It
 * never takes the focus as it appears; its count is announced politely.
 */
export function BatchBar(props: BatchBarProps) {
  const phone = usePhone();
  const action = (a: BarAction) => (
    <Button
      key={a.id}
      variant={phone ? "ghost" : "tertiary"}
      isDisabled={props.disabled}
      onPress={a.onPress}
      className={`${phone ? "h-auto min-h-12 min-w-0 flex-1 flex-col gap-0.5 rounded-2xl px-1 py-1.5 text-xs" : ""} ${a.danger === true && !props.disabled ? "text-danger" : ""}`}
    >
      <a.icon aria-hidden className="size-4 shrink-0" />
      <span className="truncate">{a.label}</span>
    </Button>
  );
  const count = (
    <p role="status" className="min-w-0 px-2 leading-tight">
      <span className="t-name t-num whitespace-nowrap">{props.summary}</span>
      {props.hidden !== undefined && <span className="t-label block truncate md:ml-2 md:inline">{props.hidden}</span>}
    </p>
  );
  const selectAll = props.selectAll !== undefined && (
    <Button variant="ghost" size="sm" className="shrink-0 text-accent" onPress={props.onSelectAll}>
      {props.selectAll}
    </Button>
  );
  if (phone) {
    return (
      <div
        role="region"
        aria-label={props.label}
        className="fixed inset-x-0 bottom-0 z-40 flex justify-center px-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]"
      >
        <div className="flex w-full max-w-md flex-col gap-1 rounded-3xl bg-overlay p-1.5 shadow-overlay">
          <div className="flex min-h-10 items-center justify-between gap-1 pl-1">
            {count}
            <div className="flex shrink-0 items-center">
              {selectAll}
              <Button variant="ghost" size="sm" className="shrink-0 font-semibold" onPress={props.onDone}>
                {props.done}
              </Button>
            </div>
          </div>
          <div className="flex items-stretch gap-1 border-t border-line pt-1">{props.actions.map(action)}</div>
        </div>
      </div>
    );
  }
  const plain = props.actions.filter((a) => a.danger !== true);
  const danger = props.actions.filter((a) => a.danger === true);
  const rule = <span aria-hidden className="mx-1.5 h-6 w-px shrink-0 bg-border" />;
  return (
    <div
      role="region"
      aria-label={props.label}
      className="pointer-events-none fixed inset-x-0 bottom-6 z-40 flex justify-center px-6"
    >
      <div className="pointer-events-auto flex max-w-full items-center gap-1 rounded-full bg-overlay py-2 pr-2 pl-3 shadow-overlay">
        {count}
        {selectAll}
        {rule}
        {plain.map(action)}
        {danger.length > 0 && rule}
        {danger.map(action)}
        {rule}
        <Button variant="ghost" onPress={props.onDone}>
          <Check aria-hidden className="size-4" />
          {props.done}
        </Button>
      </div>
    </div>
  );
}
